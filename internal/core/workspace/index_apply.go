package workspace

import (
	"context"
	"io/fs"
	"path"
	"path/filepath"
	"strings"
)

// isIgnoreFileName tells whether a file name is one of the ignore files that decide what is listed.
func isIgnoreFileName(name string) bool { return name == ".gitignore" || name == ".ignore" }

// Apply updates the list with the events of the watcher: a created file is added, a deleted one
// removed, a renamed one moved, and a created or renamed folder is walked; paths that the exclude
// patterns or the ignore files leave out are not added. A change of an ignore file, or an event that
// says that events were lost below the folder, makes the list again. Events for paths outside the
// folder are ignored. It reads a little of the file system (one stat for each created path) and
// starts a walk in the background for each folder that appeared; the list may miss a file that was
// created and deleted in a moment.
func (x *FileIndex) Apply(ctx context.Context, events []Event) {
	x.mu.RLock()
	ws, closed := x.cur, x.closed
	x.mu.RUnlock()
	if ws == nil || closed || ctx.Err() != nil {
		return
	}
	x.mu.Lock()
	x.ensureSetLocked()
	x.mu.Unlock()
	var (
		rebuild  bool
		exact    = map[string]struct{}{}
		prefixes []string
		adds     []string
		scans    []string
	)
	remove := func(p string, isDir bool) {
		rel, ok := x.rel(p)
		if !ok {
			if p == x.root {
				rebuild = true
			}
			return
		}
		if isIgnoreFileName(path.Base(rel)) {
			rebuild = true
			return
		}
		if ws.hasIgnoreBelow(rel) {
			rebuild = true // the rules of a folder that is gone cannot be taken back
		}
		x.mu.RLock()
		_, listed := x.set[rel]
		x.mu.RUnlock()
		if listed {
			exact[rel] = struct{}{}
		}
		// a folder, or a path that is not listed and that nothing leaves out (it may have been a folder)
		if isDir || (!listed && !x.excludedPath(ws, rel, false)) {
			prefixes = append(prefixes, rel+"/")
		}
	}
	add := func(p string) {
		rel, ok := x.rel(p)
		if !ok {
			return
		}
		if isIgnoreFileName(path.Base(rel)) {
			rebuild = true
			return
		}
		if x.excludedPath(ws, rel, false) {
			return
		}
		info, err := x.fsys.Lstat(p)
		if err != nil {
			return
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			if !ws.cfg.FollowSymlinks {
				return
			}
			if info, err = x.fsys.Stat(p); err != nil {
				return
			}
		}
		switch {
		case info.IsDir():
			if x.excludedPath(ws, rel, true) {
				return
			}
			if ws.hasIgnoreBelow(rel) {
				rebuild = true
			}
			scans = append(scans, rel)
		case info.Mode().IsRegular():
			adds = append(adds, rel)
		}
	}
	for _, e := range events {
		switch e.Kind {
		case EventCreated:
			add(e.Path)
		case EventDeleted:
			remove(e.Path, e.IsDir)
		case EventRenamed:
			remove(e.OldPath, e.IsDir)
			add(e.Path)
		case EventChanged:
			if isIgnoreFileName(filepath.Base(e.Path)) {
				if _, ok := x.rel(e.Path); ok {
					rebuild = true
				}
			}
		case EventRescan:
			rel, ok := x.rel(e.Path)
			switch {
			case !ok && e.Path == x.root, ok && rel == "":
				rebuild = true
			case ok:
				remove(e.Path, true)
				add(e.Path)
			}
		}
	}
	if rebuild {
		x.Rebuild()
		return
	}
	if len(exact) > 0 || len(prefixes) > 0 {
		x.removeFiles(exact, prefixes)
	}
	if len(adds) > 0 {
		x.mu.Lock()
		if x.cur == ws {
			x.ensureSetLocked()
			x.appendLocked(adds)
		}
		v := x.version
		x.mu.Unlock()
		x.fire(IndexChanged, v)
	}
	for _, rel := range scans {
		x.scan(ws, rel)
	}
}

// removeFiles drops the files that are listed in exact or below one of the prefixes (which end with
// "/"), making a new list so that the snapshots that exist keep theirs.
func (x *FileIndex) removeFiles(exact map[string]struct{}, prefixes []string) {
	prefixSet := make(map[string]struct{}, len(prefixes))
	for _, p := range prefixes {
		prefixSet[strings.TrimSuffix(p, "/")] = struct{}{}
	}
	x.mu.Lock()
	kept := make([]string, 0, len(x.files))
	removed := false
	for _, f := range x.files {
		drop := false
		if _, ok := exact[f]; ok {
			drop = true
		} else if len(prefixSet) > 0 {
			for i := 0; i < len(f); i++ {
				if f[i] == '/' {
					if _, ok := prefixSet[f[:i]]; ok {
						drop = true
						break
					}
				}
			}
		}
		if drop {
			removed = true
			if x.set != nil {
				delete(x.set, f)
			}
			continue
		}
		kept = append(kept, f)
	}
	if removed {
		x.files = kept
		x.version++
	}
	v := x.version
	x.mu.Unlock()
	if removed {
		x.fire(IndexChanged, v)
	}
}

// scan walks a folder that appeared, in the background.
func (x *FileIndex) scan(ws *walkState, rel string) {
	real := x.abs(rel)
	if r, err := filepath.EvalSymlinks(real); err == nil {
		real = r
	}
	x.mu.Lock()
	if x.cur != ws || x.closed {
		x.mu.Unlock()
		return
	}
	x.wg.Add(1)
	x.mu.Unlock()
	go func() {
		defer x.wg.Done()
		x.walk(ws, []dirNode{{rel: rel, real: real, parent: -1}})
	}()
}

// excludedPath tells whether a path anywhere in the folder is left out: it, or a folder above it,
// matches the exclude patterns or is ignored by the ignore files.
func (x *FileIndex) excludedPath(ws *walkState, rel string, isDir bool) bool {
	segments := strings.Split(rel, "/")
	prefix := ""
	hasSibling := func(name string) bool {
		_, err := x.fsys.Lstat(x.abs(path.Join(path.Dir(prefix), name)))
		return err == nil
	}
	for i, seg := range segments {
		if i == 0 {
			prefix = seg
		} else {
			prefix += "/" + seg
		}
		last := i == len(segments)-1
		if seg == ".git" {
			return true
		}
		if !ws.match.IsEmpty() {
			if _, hit := ws.match.Match(prefix, seg, hasSibling); hit {
				return true
			}
		}
		if ws.cfg.UseIgnoreFiles {
			dir := isDir || !last
			ws.igMu.RLock()
			ignored, _ := ws.ig.Check(prefix, dir)
			parents := ws.parents
			ws.igMu.RUnlock()
			if ignored {
				return true
			}
			for _, p := range parents {
				if p.ig.Ignored(p.prefix+prefix, dir) {
					return true
				}
			}
		}
	}
	return false
}

// hasIgnoreBelow tells whether an ignore file was read in the folder rel or below it.
func (ws *walkState) hasIgnoreBelow(rel string) bool {
	ws.igMu.RLock()
	defer ws.igMu.RUnlock()
	for _, dir := range ws.layerDirs {
		if dir == rel || strings.HasPrefix(dir, rel+"/") {
			return true
		}
	}
	return false
}
