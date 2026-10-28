package search

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/rm4n0s/emetos/internal/core/glob"
)

// maxIgnoreFileSize bounds the ignore files that are read: a larger one is a mistake.
const maxIgnoreFileSize = 1 << 20

// candidate is a file the walk found, for a worker to read.
type candidate struct {
	rel, abs string
}

// dirNode is a folder of the walk: its path relative to the root, its path with the links resolved, and the index of its
// parent in the list.
type dirNode struct {
	rel, real string
	parent    int
}

// parentIgnore is the ignore files of one parent folder of the searched folder.
type parentIgnore struct {
	ig     *glob.Ignore
	prefix string // the path of the searched folder relative to the parent, with a "/" at the end
}

// filters are the rules that decide which files are searched.
type filters struct {
	include *glob.ExprMatcher // nil: every file
	exclude *glob.ExprMatcher // files.exclude, search.exclude and the "files to exclude" box
}

func newFilters(o *Options) filters {
	var f filters
	if len(o.Include) > 0 {
		f.include = glob.ParseExpression(expression(o.Include), glob.Options{})
		if f.include.IsEmpty() {
			f.include = nil
		}
	}
	expr := append(glob.Expression(nil), o.ExcludeSettings...)
	expr = append(expr, expression(o.Exclude)...)
	f.exclude = glob.ParseExpression(expr, glob.Options{TrimForExclusions: true})
	return f
}

// included tells whether a file passes the include patterns.
func (f filters) included(rel, name string) bool {
	if f.include == nil {
		return true
	}
	_, ok := f.include.Match(rel, name, nil)
	return ok
}

// excluded tells whether an entry matches the exclude patterns.
func (f filters) excluded(rel, name string, hasSibling func(string) bool) bool {
	if f.exclude.IsEmpty() {
		return false
	}
	_, ok := f.exclude.Match(rel, name, hasSibling)
	return ok
}

// walker lists the files of the folder for the workers: breadth first, each folder sorted by name, with the exclude patterns
// and the ignore files applied as it goes.
type walker struct {
	ctx      context.Context
	opts     *Options
	f        filters
	root     string
	rootReal string
	ig       *glob.Ignore
	igDirs   map[string]bool
	parents  []parentIgnore
	seen     map[string]bool // the absolute paths of the files that were listed, when there are unsaved texts to match
	out      chan<- candidate
	skipped  int // folders that could not be read
}

func (w *walker) readIgnoreFile(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil || len(data) > maxIgnoreFileSize {
		return "", false
	}
	return string(data), true
}

// loadIgnoreSources reads the ignore files that are not in the walked folders: git's exclude file, the global ignore file
// and the files of the parent folders.
func (w *walker) loadIgnoreSources() {
	o := w.opts
	if !o.UseIgnoreFiles {
		return
	}
	if o.UseParentIgnoreFiles {
		prefix := filepath.Base(w.root) + "/"
		for dir := filepath.Dir(w.root); ; dir = filepath.Dir(dir) {
			ig := glob.NewIgnore(glob.IgnoreOptions{})
			found := false
			for _, name := range []string{".gitignore", ".ignore"} {
				if text, ok := w.readIgnoreFile(filepath.Join(dir, name)); ok {
					ig.AddFile("", text)
					found = true
				}
			}
			if found {
				w.parents = append(w.parents, parentIgnore{ig: ig, prefix: prefix})
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			prefix = filepath.Base(dir) + "/" + prefix
		}
	}
	if text, ok := w.readIgnoreFile(filepath.Join(w.root, ".git", "info", "exclude")); ok {
		w.ig.AddGlobal(text)
	}
	if o.GlobalIgnoreFile != "" {
		if text, ok := w.readIgnoreFile(o.GlobalIgnoreFile); ok {
			w.ig.AddGlobal(text)
		}
	}
}

// run walks the folder and sends the files to out. It returns when the walk is done or the context is.
func (w *walker) run() {
	w.loadIgnoreSources()
	nodes := []dirNode{{rel: "", real: w.rootReal, parent: -1}}
	for next := 0; next < len(nodes); next++ {
		if w.ctx.Err() != nil {
			return
		}
		n := nodes[next]
		abs := w.root
		if n.rel != "" {
			abs = filepath.Join(w.root, filepath.FromSlash(n.rel))
		}
		entries, err := os.ReadDir(abs)
		if err != nil {
			w.skipped++
			continue
		}
		var names map[string]struct{}
		hasSibling := func(name string) bool {
			if names == nil {
				names = make(map[string]struct{}, len(entries))
				for _, e := range entries {
					names[e.Name()] = struct{}{}
				}
			}
			_, ok := names[name]
			return ok
		}
		if w.opts.UseIgnoreFiles {
			w.readFolderIgnoreFiles(n.rel, abs, entries)
		}
		for _, e := range entries {
			name := e.Name()
			if name == ".git" {
				continue
			}
			rel := name
			if n.rel != "" {
				rel = n.rel + "/" + name
			}
			isDir, real, ok := w.classify(e, abs, next, nodes, name)
			if !ok {
				continue
			}
			if w.f.excluded(rel, name, hasSibling) || w.ignored(rel, isDir) {
				continue
			}
			if isDir {
				nodes = append(nodes, dirNode{rel: rel, real: real, parent: next})
				continue
			}
			if !w.f.included(rel, name) {
				continue
			}
			c := candidate{rel: rel, abs: filepath.Join(abs, name)}
			if w.seen != nil {
				w.seen[c.abs] = true
			}
			select {
			case w.out <- c:
			case <-w.ctx.Done():
				return
			}
		}
	}
}

// classify tells whether an entry is a folder to enter (with its path with the links resolved) or a file to search; ok is
// false for the entries that are neither (sockets, broken links, links when they are not followed, and folders that loop).
func (w *walker) classify(e fs.DirEntry, abs string, cur int, nodes []dirNode, name string) (isDir bool, real string, ok bool) {
	n := nodes[cur]
	typ := e.Type()
	if typ&fs.ModeSymlink == 0 {
		switch {
		case typ.IsDir():
			return true, n.real + "/" + name, true
		case typ.IsRegular():
			return false, "", true
		}
		return false, "", false
	}
	if !w.opts.FollowSymlinks {
		return false, "", false
	}
	path := filepath.Join(abs, name)
	info, err := os.Stat(path)
	if err != nil {
		return false, "", false // a broken link
	}
	if !info.IsDir() {
		return false, "", info.Mode().IsRegular()
	}
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false, "", false
	}
	// A link to the folder itself, to a parent of it or to a folder that contains the searched one loops.
	if isPathPrefix(target, w.rootReal) {
		return false, "", false
	}
	for i := cur; i >= 0; i = nodes[i].parent {
		if nodes[i].real == target {
			return false, "", false
		}
	}
	return true, target, true
}

// isPathPrefix tells whether dir is path or one of its parents.
func isPathPrefix(dir, path string) bool {
	return path == dir || strings.HasPrefix(path, strings.TrimSuffix(dir, "/")+"/")
}

// readFolderIgnoreFiles adds the .gitignore and .ignore of a folder to the rules, once.
func (w *walker) readFolderIgnoreFiles(rel, abs string, entries []fs.DirEntry) {
	if w.igDirs[rel] {
		return
	}
	w.igDirs[rel] = true
	for _, name := range []string{".gitignore", ".ignore"} {
		found := false
		for _, e := range entries {
			if e.Name() == name {
				found = true
				break
			}
		}
		if !found {
			continue
		}
		if text, ok := w.readIgnoreFile(filepath.Join(abs, name)); ok {
			w.ig.AddFile(rel, text)
		}
	}
}

// ignored tells whether the ignore files leave an entry out.
func (w *walker) ignored(rel string, isDir bool) bool {
	if !w.opts.UseIgnoreFiles {
		return false
	}
	if ignored, _ := w.ig.Check(rel, isDir); ignored {
		return true
	}
	for _, p := range w.parents {
		if p.ig.Ignored(p.prefix+rel, isDir) {
			return true
		}
	}
	return false
}
