package gitscm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/git"
)

// The quick diff providers of a repository (SPEC §10.30 "Quick diff", step 24.11): the marks of the changes of a file in the editors, and
// the baseline of the format on save of modifications. As VS Code's git extension has them there are two: Git Local Changes (Working
// Tree), whose original is the version in the index, and Git Local Changes (Index), whose original is HEAD and which has one for the files
// of the Staged Changes group only. The host asks them off the UI thread, so what they read about the repository is a copy that the UI
// thread keeps for them (quickDiffState).
//
// Ported from VS Code: extensions/git/src/quickDiffProvider.ts (GitQuickDiffProvider, StagedResourceQuickDiffProvider) and
// repository.ts (where the two are set on the source control)

// Labels of the two providers, VS Code's.
const (
	quickDiffWorkingTreeLabel = "Git Local Changes (Working Tree)"
	quickDiffIndexLabel       = "Git Local Changes (Index)"
)

// quickDiffState is what the providers of a repository read from other goroutines: the paths of the groups the rules look at, and the
// signature of the state they were made from.
type quickDiffState struct {
	mu sync.Mutex
	// merge, untracked and staged are the absolute paths of the files in the Merge Changes group, the untracked files (those of the
	// Untracked Changes group and the Changes group's) and the files of the Staged Changes group.
	merge, untracked, staged map[string]bool
	// sig is the signature of the state of the repository the host was last told about: when it is the same after a refresh, nothing
	// the providers answer has changed.
	sig string
	// regs are the providers added to the source control; off says that they were taken away.
	regs []extension.QuickDiffRegistration
}

// addQuickDiff adds the two providers to the source control of the repository, once. A scope that has no editors (a host without a
// workbench) has no quick diff, and the extension goes on without it.
func (r *repository) addQuickDiff() {
	if r.disposed || len(r.qd.regs) > 0 {
		return
	}
	x := r.m.x
	for _, p := range []extension.QuickDiffProvider{
		{Label: quickDiffWorkingTreeLabel, Kind: extension.QuickDiffPrimary, Original: r.workingTreeOriginal},
		{Label: quickDiffIndexLabel, Kind: extension.QuickDiffSecondary, Original: r.indexOriginal},
	} {
		reg, err := r.src.AddQuickDiffProvider(p)
		if err != nil {
			if appErr, ok := errors.FromError(err); !ok || appErr.Tag != "ExtensionQuickDiffUnavailable" {
				x.log.Warn("cannot add a quick diff provider", "root", r.root, "label", p.Label, "err", err)
			}
			return
		}
		r.qd.regs = append(r.qd.regs, reg)
	}
}

// dropQuickDiff takes the providers away (the repository is not trusted any more, or was closed).
func (r *repository) dropQuickDiff() {
	r.qd.mu.Lock()
	regs := r.qd.regs
	r.qd.regs = nil
	r.qd.sig, r.qd.merge, r.qd.untracked, r.qd.staged = "", nil, nil, nil
	r.qd.mu.Unlock()
	for _, reg := range regs {
		reg.Dispose()
	}
}

// quickDiffChanged is called after a refresh found the state of the repository: when the commit of HEAD, the index, or the files of the
// groups the providers look at changed, the providers' sets are made again and the host is told to ask them again. A refresh that finds
// the same state costs the host nothing.
func (r *repository) quickDiffChanged() {
	r.qd.mu.Lock()
	if len(r.qd.regs) == 0 {
		r.qd.mu.Unlock()
		return
	}
	sig := r.quickDiffSignature()
	if sig == r.qd.sig {
		r.qd.mu.Unlock()
		return
	}
	r.qd.sig = sig
	r.qd.merge, r.qd.untracked, r.qd.staged = map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, res := range r.res.merge {
		r.qd.merge[res.path] = true
	}
	for _, res := range r.res.index {
		r.qd.staged[res.path] = true
	}
	for _, res := range r.res.untracked {
		r.qd.untracked[res.path] = true
	}
	for _, res := range r.res.working {
		if res.status == git.StatusUntracked {
			r.qd.untracked[res.path] = true
		}
	}
	r.qd.mu.Unlock()
	r.src.RefreshQuickDiff()
}

// quickDiffSignature identifies the state the providers answer from: the commit HEAD is at, the size and time of the index file (it
// changes with every stage, also of a second part of a file that is staged already), and the files and statuses of the groups.
func (r *repository) quickDiffSignature() string {
	h := sha256.New()
	h.Write([]byte(r.head.commit))
	h.Write([]byte{0})
	if st, err := os.Stat(filepath.Join(r.loc.GitDir, "index")); err == nil {
		h.Write([]byte(st.ModTime().UTC().String() + "|" + strconv.FormatInt(st.Size(), 10)))
	}
	for i, list := range [][]resource{r.res.merge, r.res.index, r.res.working, r.res.untracked} {
		h.Write([]byte{0, byte(i)})
		for _, res := range list {
			h.Write([]byte(res.path))
			h.Write([]byte{0, byte(res.status)})
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// quickDiffFile is the path of a file URI the providers answer for, and false for another URI, a symbolic link, a file inside the git
// directory and a file another open repository owns (the repository with the deepest root that holds a file is its repository).
func (r *repository) quickDiffFile(uri string) (string, bool) {
	path, ok := uriPath(uri)
	if !ok {
		return "", false
	}
	if git.Within(path, r.loc.GitDir) || git.Within(path, r.commonDir()) || git.Within(path, filepath.Join(r.root, ".git")) {
		return "", false
	}
	if st, err := os.Lstat(path); err != nil || st.Mode()&os.ModeSymlink != 0 {
		return "", false
	}
	if !git.Within(path, r.root) || r.m.x.ownerOf(path) != r.root {
		return "", false
	}
	return path, true
}

// workingTreeOriginal is the provider of Git Local Changes (Working Tree): the version in the index, for a tracked file that is not in
// conflict. An untracked file, and a file git ignores, has no version in the index; the file is looked for there (git ls-files) only after
// the cheaper rules, so a refresh of the index costs one process for each open file.
func (r *repository) workingTreeOriginal(ctx context.Context, uri string) (string, error) {
	path, ok := r.quickDiffFile(uri)
	if !ok {
		return "", nil
	}
	r.qd.mu.Lock()
	skip := r.qd.merge[path] || r.qd.untracked[path]
	repo := r.repo
	r.qd.mu.Unlock()
	if skip || repo == nil {
		return "", nil
	}
	entries, err := repo.LsFiles(ctx, path)
	if err != nil || len(entries) == 0 {
		return "", err
	}
	return toGitURI(r.id, r.root, path, refIndex), nil
}

// indexOriginal is the provider of Git Local Changes (Index): the version in HEAD, for a file of the Staged Changes group.
func (r *repository) indexOriginal(_ context.Context, uri string) (string, error) {
	path, ok := r.quickDiffFile(uri)
	if !ok {
		return "", nil
	}
	r.qd.mu.Lock()
	staged := r.qd.staged[path]
	r.qd.mu.Unlock()
	if !staged {
		return "", nil
	}
	return toGitURI(r.id, r.root, path, refHEAD), nil
}

// ownerRoots are the roots of the open repositories, which a provider reads from any goroutine to tell whether a file is its
// repository's.
type ownerRoots struct {
	mu    sync.Mutex
	roots []string
}

// set replaces the roots.
func (o *ownerRoots) set(roots []string) {
	o.mu.Lock()
	o.roots = roots
	o.mu.Unlock()
}

// ownerOf is the root of the open repository that holds a file: the deepest one.
func (x *gitscm) ownerOf(path string) string {
	x.owners.mu.Lock()
	defer x.owners.mu.Unlock()
	best := ""
	for _, root := range x.owners.roots {
		if git.Within(path, root) && len(root) > len(best) {
			best = root
		}
	}
	return best
}
