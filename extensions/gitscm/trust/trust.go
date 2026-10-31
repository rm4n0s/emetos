// Package trust keeps the user's answers to the question "Do you trust the authors of this Git repository?" (SPEC §10.30
// "Trusting a repository", D37): the repositories and the parent folders that are trusted, in Emetos's own state file
// $XDG_STATE_HOME/emetos/git-trust.json, and the repositories the user declined for this session. It runs nothing and imports
// no Git client; the client asks it (through git.TrustedBy) before it starts a process.
//
// The file is {"version": 1, "trusted": [path...], "trustedParents": [path...]}, mode 0600 in a folder of mode 0700, written
// atomically. Paths are real paths (symbolic links resolved) and cleaned, so a link to a trusted repository is the same
// repository, and a parent folder covers the folders below it by whole path elements: "/work/a" covers "/work/a/b" and not
// "/work/ab". A file that cannot be used is ignored with one error (GitTrustFileUnreadable, GitTrustFileCorrupt,
// GitTrustVersionUnsupported) and the question is asked again; a write that fails keeps the answer for the session
// (StateFileWriteFailed, or GitTrustWriteRefused when the file was left alone).
package trust

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/internal/statefile"
)

// FileName is the name of the trust file in $XDG_STATE_HOME/emetos.
const FileName = "git-trust.json"

// Version is the version of the file this package reads and writes.
const Version = 1

// maxFileSize bounds the file: a hundred thousand paths of a few hundred bytes would be a different problem.
const maxFileSize = 4 << 20

// file is the shape of the trust file.
type file struct {
	Version        int      `json:"version"`
	Trusted        []string `json:"trusted"`
	TrustedParents []string `json:"trustedParents"`
}

// Kind says what an Entry trusts.
type Kind int

// The kinds of entry.
const (
	// Repository trusts one repository.
	Repository Kind = iota
	// Parent trusts every repository below a folder.
	Parent
)

// Entry is a trusted repository or parent folder.
type Entry struct {
	Kind Kind
	// Path is the real path.
	Path string
}

// Store holds the answers. It is safe for concurrent use: the client asks it from the goroutines that run git.
type Store struct {
	path string

	mu       sync.RWMutex
	trusted  map[string]bool
	parents  map[string]bool
	declined map[string]bool
	// session are the parent folders trusted for this run of Emetos only, never written (the dev knob EMETOS_DEV_GIT_TRUST).
	session map[string]bool
	// readOnly is set when the file is of a newer version or cannot be read: the answers stay in memory and the file is left as
	// it is.
	readOnly error
	// encode writes the file's JSON; json.MarshalIndent, replaced by a test that makes it fail.
	encode func(v any, prefix, indent string) ([]byte, error)
}

// Path is the trust file of the state folder (the folder that holds "emetos", $XDG_STATE_HOME/emetos): its path.
func Path(stateDir string) string { return filepath.Join(stateDir, FileName) }

// Open reads the trust file at path (use Path with statefile.DirFromEnv) and returns the store. A file that is missing is an
// empty store. A file that cannot be used gives an empty store too, and its error (GitTrustFileUnreadable, GitTrustFileCorrupt
// or GitTrustVersionUnsupported) is returned with it for the caller to log once; a store made so never overwrites a file it
// could not read or whose version it does not know.
func Open(path string) (*Store, error) {
	s := &Store{path: path, trusted: map[string]bool{}, parents: map[string]bool{}, declined: map[string]bool{}}
	data, err := statefile.Read(path, maxFileSize, statefile.Writable)
	if err != nil {
		if statefile.Missing(err) {
			return s, nil
		}
		failure := errors.New("GitTrustFileUnreadable", "the trust file cannot be read, so it is ignored and left as it is", "path", path, "cause", err.Error())
		s.readOnly = failure
		return s, failure
	}
	var f file
	if err := json.Unmarshal(data, &f); err != nil {
		// a corrupt file is replaced by the next answer: there is nothing in it to keep
		return s, errors.NewErr("GitTrustFileCorrupt", err).SetMetadata("path", path)
	}
	if f.Version != Version {
		failure := errors.New("GitTrustVersionUnsupported", "the trust file was written by another version of Emetos", "path", path, "version", f.Version, "supported", Version)
		s.readOnly = failure
		return s, failure
	}
	for _, p := range f.Trusted {
		if filepath.IsAbs(p) {
			s.trusted[filepath.Clean(p)] = true
		}
	}
	for _, p := range f.TrustedParents {
		if filepath.IsAbs(p) {
			s.parents[filepath.Clean(p)] = true
		}
	}
	return s, nil
}

// NewMemory makes a store that never touches the disk, for the tests of code that only asks.
func NewMemory() *Store {
	return &Store{trusted: map[string]bool{}, parents: map[string]bool{}, declined: map[string]bool{}}
}

// Canonical is the key of a path: absolute, cleaned, with symbolic links resolved when the path exists (and the resolved
// parent with the name appended when only the last element is missing).
func Canonical(path string) string {
	path = filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	if dir, err := filepath.EvalSymlinks(filepath.Dir(path)); err == nil {
		return filepath.Join(dir, filepath.Base(path))
	}
	return path
}

// IsTrusted reports whether the repository with this root is trusted: its real path was trusted, or a parent folder above it was.
// It implements git.TrustStore.
func (s *Store) IsTrusted(root string) bool {
	key := Canonical(root)
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.trusted[key] {
		return true
	}
	for p := range s.parents {
		if within(key, p) {
			return true
		}
	}
	for p := range s.session {
		if within(key, p) {
			return true
		}
	}
	return false
}

// TrustForSession trusts every repository below the folder until the store is dropped; nothing is written. It is how the dev knob
// EMETOS_DEV_GIT_TRUST of the proofs keeps the question off an image (debug and emetos_dev builds, SPEC §15.2).
func (s *Store) TrustForSession(dir string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session == nil {
		s.session = map[string]bool{}
	}
	s.session[Canonical(dir)] = true
}

// Why says how a repository came to be trusted: "repository" (its own path was trusted), "parent" (a parent folder was, and via is
// the folder), "session" (the dev knob), or "" when it is not trusted by path. A repository that belongs to a trusted one as a submodule or a
// linked worktree is not trusted by its path: the caller follows its parent.
func (s *Store) Why(root string) (kind, via string) {
	key := Canonical(root)
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.trusted[key] {
		return "repository", ""
	}
	for _, p := range sortedKeys(s.parents) {
		if within(key, p) {
			return "parent", p
		}
	}
	for _, p := range sortedKeys(s.session) {
		if within(key, p) {
			return "session", p
		}
	}
	return "", ""
}

// within reports whether path is dir or below it, by whole path elements.
func within(path, dir string) bool {
	if path == dir {
		return true
	}
	if dir == string(filepath.Separator) {
		return true
	}
	return strings.HasPrefix(path, dir+string(filepath.Separator))
}

// Trust trusts the repository with this root and writes the file. When the write fails the answer holds for the session and the
// error (StateFileWriteFailed, or GitTrustWriteRefused when the file was left alone) says so.
func (s *Store) Trust(root string) error {
	key := Canonical(root)
	s.mu.Lock()
	s.trusted[key] = true
	delete(s.declined, key)
	s.mu.Unlock()
	return s.save()
}

// TrustParent trusts every repository below the folder and writes the file; the failure is that of Trust.
func (s *Store) TrustParent(dir string) error {
	key := Canonical(dir)
	s.mu.Lock()
	s.parents[key] = true
	s.declined = map[string]bool{} // a repository the user declined is covered by the new answer
	s.mu.Unlock()
	return s.save()
}

// Remove takes an entry away (Manage Trusted Repositories) and writes the file; the failure is that of Trust. It reports whether
// the entry was there.
func (s *Store) Remove(e Entry) (bool, error) {
	s.mu.Lock()
	var found bool
	switch e.Kind {
	case Parent:
		found = s.parents[e.Path]
		delete(s.parents, e.Path)
	default:
		found = s.trusted[e.Path]
		delete(s.trusted, e.Path)
	}
	s.mu.Unlock()
	if !found {
		return false, nil
	}
	return true, s.save()
}

// Decline remembers that the user answered Don't Trust for this repository, for the session only.
func (s *Store) Decline(root string) {
	s.mu.Lock()
	s.declined[Canonical(root)] = true
	s.mu.Unlock()
}

// Undecline forgets that the user declined this repository in this session, so that the question can be asked again (the command
// Trust Repository).
func (s *Store) Undecline(root string) {
	s.mu.Lock()
	delete(s.declined, Canonical(root))
	s.mu.Unlock()
}

// Declined reports whether the user declined this repository in this session.
func (s *Store) Declined(root string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.declined[Canonical(root)]
}

// Entries lists what is trusted: the repositories, then the parent folders, each sorted.
func (s *Store) Entries() []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Entry
	for _, p := range sortedKeys(s.trusted) {
		out = append(out, Entry{Kind: Repository, Path: p})
	}
	for _, p := range sortedKeys(s.parents) {
		out = append(out, Entry{Kind: Parent, Path: p})
	}
	return out
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// ParentOf is the folder "Trust All Repositories in <parent>" offers for a repository: the folder that holds it.
func ParentOf(root string) string { return filepath.Dir(Canonical(root)) }

// save writes the file. The file is written from the memory of the process: another Emetos may have changed it since, and its
// change is lost (the answers are one per user action, not a database). A store with no path (NewMemory) writes nothing.
func (s *Store) save() error {
	if s.path == "" {
		return nil
	}
	s.mu.RLock()
	readOnly := s.readOnly
	f := file{Version: Version, Trusted: sortedKeys(s.trusted), TrustedParents: sortedKeys(s.parents)}
	s.mu.RUnlock()
	if readOnly != nil {
		return errors.New("GitTrustWriteRefused", "the trust file could not be used when it was read, so it is not overwritten; the answer holds for this session",
			"path", s.path, "cause", readOnly.Error())
	}
	encode := s.encode
	if encode == nil {
		encode = json.MarshalIndent
	}
	data, err := encode(f, "", "  ")
	if err != nil {
		return errors.NewErr("GitTrustEncodeFailed", err).SetMetadata("path", s.path)
	}
	// the error of the state file (StateFileWriteFailed) is returned as it is: the answer holds for the session
	return statefile.Write(s.path, append(data, '\n'))
}
