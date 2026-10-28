package search

import (
	"bytes"
	"context"
	"io"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/fileio"
	"github.com/rm4n0s/emetos/internal/core/glob"
)

// The defaults of Options.
const (
	// DefaultMaxResults is search.maxResults.
	DefaultMaxResults = 20000
	// DefaultMaxFileSize is the largest file that is searched: 64 MiB. A larger file is skipped (Summary.TooLarge).
	DefaultMaxFileSize = 64 << 20
	// MaxWorkers is the most goroutines that read and match files.
	MaxWorkers = 8
	// binaryWindow is how many bytes of a file are looked at for a NUL.
	binaryWindow = 8 << 10
)

// Options say where to search and what to leave out.
type Options struct {
	// Folder is the absolute path of the folder to search.
	Folder string
	// Include are the glob patterns a file must match (ParsePatterns of the "files to include" box); none is every file.
	Include []string
	// Exclude are the glob patterns of the "files to exclude" box (ParsePatterns), and ExcludeSettings those of
	// files.exclude and search.exclude together (glob.ExpressionFromSetting); a folder or file that matches is left out.
	Exclude         []string
	ExcludeSettings glob.Expression
	// UseIgnoreFiles (search.useIgnoreFiles) leaves out what .gitignore and .ignore files and .git/info/exclude say, in the
	// folders that are searched. UseParentIgnoreFiles (search.useParentIgnoreFiles) adds the ignore files of the parent
	// folders of Folder, and GlobalIgnoreFile (search.useGlobalIgnoreFiles) the user's global ignore file when it is not
	// empty. Both need UseIgnoreFiles.
	UseIgnoreFiles       bool
	UseParentIgnoreFiles bool
	GlobalIgnoreFile     string
	// FollowSymlinks (search.followSymlinks) follows links to folders and files; a link that makes a loop is not followed.
	FollowSymlinks bool
	// Encoding is the id of files.encoding; empty is utf8. A byte order mark decides first.
	Encoding string
	// MaxResults is the most matches to report (DefaultMaxResults when not positive); the search stops when it is reached
	// and says so in Summary.LimitHit.
	MaxResults int
	// MaxFileSize is the largest file to search (DefaultMaxFileSize when not positive).
	MaxFileSize int64
	// Files, when it is not nil, are the files to search, as paths relative to Folder with forward slashes: the file index of
	// the workspace when the walk has finished and its settings are those of this search. The folder is not walked then, and
	// ExcludeSettings, the ignore files and FollowSymlinks are not applied (the list has them applied already). Include and
	// Exclude still are.
	Files []string
	// OnlyPaths, when it is not nil, are the absolute paths of the only files to search ("Search Only in Open Editors");
	// the folder is not walked, and the ignore files are not applied.
	OnlyPaths []string
	// Unsaved are the texts of the editors that have unsaved changes, by absolute path: they are searched instead of the
	// files on disk. A path that is under Folder and has no file on disk is searched too.
	Unsaved map[string]string
	// Workers is the number of goroutines that read and match files: the number of CPUs, at most MaxWorkers, when not
	// positive.
	Workers int
}

// Summary says how a search went.
type Summary struct {
	// Files is the number of files with matches and Matches the number of matches.
	Files, Matches int
	// LimitHit is true when the search stopped at Options.MaxResults.
	LimitHit bool
	// Searched is the number of files that were read.
	Searched int
	// Binary, TooLarge and Unreadable count the files that were left out because they look binary, are larger than
	// Options.MaxFileSize, or could not be read or decoded; Folders counts the folders that could not be read.
	Binary, TooLarge, Unreadable, Folders int
}

// Run searches the folder and calls onFile with the matches of each file that has some. The calls are made one at a time
// (never at once) from the goroutines of the search, so the function may keep state without a lock but must not block for
// long; the files come in no fixed order. Run returns when the search is over: all the files are read, the limit is hit, or
// ctx is done. A search that ctx ended returns the summary so far and an error with the tag SearchCancelled; a query that
// does not compile returns the error of internal/core/regex or SearchPatternEmpty, and a folder that cannot be read SearchFolderUnreadable.
// No goroutine of the search is left running when Run returns.
func Run(ctx context.Context, q Query, o Options, onFile func(FileResult)) (Summary, error) {
	m, err := compile(q)
	if err != nil {
		return Summary{}, err
	}
	if o.OnlyPaths == nil && o.Files == nil {
		if _, err := os.Stat(o.Folder); err != nil {
			return Summary{}, errors.NewErr("SearchFolderUnreadable", err).SetMetadata("folder", o.Folder)
		}
	}
	o.Folder = filepath.Clean(o.Folder)
	maxResults := int64(o.MaxResults)
	if maxResults <= 0 {
		maxResults = DefaultMaxResults
	}
	if o.MaxFileSize <= 0 {
		o.MaxFileSize = DefaultMaxFileSize
	}
	workers := o.Workers
	if workers <= 0 {
		workers = min(runtime.NumCPU(), MaxWorkers)
	}
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()

	e := &engine{ctx: sctx, o: &o, m: m, max: maxResults, onFile: onFile, cancel: cancel}
	files := make(chan candidate, 4*workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.work(files)
		}()
	}
	e.list(files)
	close(files)
	wg.Wait()

	sum := Summary{
		Files: int(e.files.Load()), Matches: int(min(e.matches.Load(), maxResults)), LimitHit: e.limit.Load(),
		Searched: int(e.searched.Load()), Binary: int(e.binary.Load()), TooLarge: int(e.tooLarge.Load()),
		Unreadable: int(e.unreadable.Load()), Folders: e.folders,
	}
	if err := ctx.Err(); err != nil {
		return sum, errors.NewErr("SearchCancelled", err)
	}
	return sum, nil
}

// engine is the state of one search.
type engine struct {
	ctx    context.Context
	cancel context.CancelFunc
	o      *Options
	m      *matcher
	max    int64

	mu     sync.Mutex // serializes onFile
	onFile func(FileResult)

	files, matches, searched, binary, tooLarge, unreadable atomic.Int64
	limit                                                  atomic.Bool
	folders                                                int
}

// list sends the files to search to the workers.
func (e *engine) list(out chan<- candidate) {
	o := e.o
	f := newFilters(o)
	send := func(c candidate) bool {
		select {
		case out <- c:
			return true
		case <-e.ctx.Done():
			return false
		}
	}
	switch {
	case o.OnlyPaths != nil:
		for _, abs := range slices.Sorted(slices.Values(o.OnlyPaths)) {
			rel, ok := relative(o.Folder, abs)
			if !ok {
				rel = filepath.ToSlash(abs)
			}
			if f.excluded(rel, baseName(rel), nil) || !f.included(rel, baseName(rel)) {
				continue
			}
			if !send(candidate{rel: rel, abs: abs}) {
				return
			}
		}
		return
	case o.Files != nil:
		for _, rel := range o.Files {
			name := baseName(rel)
			if f.excluded(rel, name, nil) || !f.included(rel, name) {
				continue
			}
			if !send(candidate{rel: rel, abs: filepath.Join(o.Folder, filepath.FromSlash(rel))}) {
				return
			}
		}
		return
	}
	w := &walker{ctx: e.ctx, opts: o, f: f, root: o.Folder, rootReal: o.Folder, igDirs: map[string]bool{}, out: out}
	if real, err := filepath.EvalSymlinks(o.Folder); err == nil {
		w.rootReal = real
	}
	w.ig = glob.NewIgnore(glob.IgnoreOptions{})
	if len(o.Unsaved) > 0 {
		w.seen = map[string]bool{}
	}
	w.run()
	e.folders = w.skipped
	// The editors that have unsaved changes and no file on disk yet.
	for _, abs := range slices.Sorted(maps.Keys(o.Unsaved)) {
		if e.ctx.Err() != nil {
			return
		}
		rel, ok := relative(o.Folder, abs)
		if !ok || w.seen[abs] {
			continue
		}
		if _, err := os.Lstat(abs); err == nil {
			continue
		}
		name := baseName(rel)
		if f.excluded(rel, name, nil) || !f.included(rel, name) {
			continue
		}
		if !send(candidate{rel: rel, abs: abs}) {
			return
		}
	}
}

// relative is the path of abs relative to the folder, with forward slashes.
func relative(folder, abs string) (string, bool) {
	rel, ok := strings.CutPrefix(abs, strings.TrimSuffix(folder, string(filepath.Separator))+string(filepath.Separator))
	if !ok || rel == "" {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

func baseName(rel string) string { return rel[strings.LastIndexByte(rel, '/')+1:] }

// work reads and matches the files it is sent.
func (e *engine) work(in <-chan candidate) {
	var buf []byte
	var sc scratch
	for c := range in {
		if e.ctx.Err() != nil {
			continue // drain, so that the lister never waits for a worker that stopped
		}
		var data []byte
		if text, ok := e.o.Unsaved[c.abs]; ok {
			data = []byte(text)
		} else {
			var ok bool
			if data, buf, ok = e.read(c.abs, buf); !ok {
				continue
			}
		}
		e.searched.Add(1)
		data = normalize(data)
		e.match(c.rel, data, &sc)
	}
}

// read reads a file into the buffer and decodes it to UTF-8; ok is false for a file that is left out. It returns the buffer,
// which may have grown, for the next file.
func (e *engine) read(abs string, buf []byte) (data, kept []byte, ok bool) {
	f, err := os.Open(abs)
	if err != nil {
		e.unreadable.Add(1)
		return nil, buf, false
	}
	defer func() { _ = f.Close() }() // read only
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		e.unreadable.Add(1)
		return nil, buf, false
	}
	size := info.Size()
	if size > e.o.MaxFileSize {
		e.tooLarge.Add(1)
		return nil, buf, false
	}
	if int64(cap(buf)) < size+1 {
		buf = make([]byte, size+1)
	}
	n, err := io.ReadFull(f, buf[:size])
	if err != nil && err != io.ErrUnexpectedEOF {
		if size > 0 {
			e.unreadable.Add(1)
			return nil, buf, false
		}
	}
	data = buf[:n]
	// A byte order mark says UTF-8 or UTF-16; without one, a NUL in the first bytes means the file is binary.
	enc := e.o.Encoding
	switch {
	case bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}):
		data, enc = data[3:], ""
	case bytes.HasPrefix(data, []byte{0xFF, 0xFE}):
		data, enc = data[2:], fileio.UTF16LE
	case bytes.HasPrefix(data, []byte{0xFE, 0xFF}):
		data, enc = data[2:], fileio.UTF16BE
	default:
		if enc != fileio.UTF16LE && enc != fileio.UTF16BE && bytes.IndexByte(data[:min(len(data), binaryWindow)], 0) >= 0 {
			e.binary.Add(1)
			return nil, buf, false
		}
	}
	if enc != "" && enc != fileio.UTF8 && enc != fileio.UTF8BOM {
		text, err := fileio.Decode(enc, data)
		if err != nil {
			e.unreadable.Add(1)
			return nil, buf, false
		}
		return []byte(text), buf, true
	}
	return data, buf, true
}

// normalize makes the bytes valid UTF-8, as the editor reads the file: each invalid byte is U+FFFD.
func normalize(data []byte) []byte {
	if utf8.Valid(data) {
		return data
	}
	var out bytes.Buffer
	out.Grow(len(data) + 16)
	for len(data) > 0 {
		r, w := utf8.DecodeRune(data)
		out.WriteRune(r)
		data = data[w:]
	}
	return out.Bytes()
}

// match finds the matches in a text and reports them.
func (e *engine) match(rel string, data []byte, sc *scratch) {
	m := e.m
	if m.multiline && bytes.Contains(data, []byte("\r\n")) {
		data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	}
	loc := locator{data: data}
	var matches []Match
	err := m.scan(data, sc, func(s, end int) bool {
		if e.ctx.Err() != nil {
			return false
		}
		if e.matches.Add(1) > e.max {
			e.limit.Store(true)
			e.cancel()
			return false
		}
		matches = append(matches, loc.match(s, end))
		return true
	})
	if err != nil {
		e.unreadable.Add(1)
	}
	if len(matches) == 0 {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.files.Add(1)
	e.onFile(FileResult{Path: rel, Matches: matches})
}
