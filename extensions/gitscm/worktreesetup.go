package gitscm

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/gitscm/git"
)

// What is done for a worktree after it was made (SPEC §10.30 "Worktrees"): the ignored folders of git.worktreeSymlinkFolders become symbolic
// links to the folders of the main work tree, and the ignored files of git.worktreeIncludeFiles are copied. It runs on a goroutine, never
// fails the creation, and says what it did in a notification. The tasks that VS Code runs after a worktree is made ("worktree-created") are
// not built.
//
// Ported from VS Code: extensions/git/src/repository.ts (_setupWorktree, _symlinkWorktreeFolders, _getWorktreeSymlinkFolders,
// _copyWorktreeIncludeFiles, _getWorktreeIncludePaths)

// maxCopies is how many files and folders are copied at the same time.
const maxCopies = 15

// worktreeSetup is what the setup did.
type worktreeSetup struct {
	// linked and linkTotal count the folders linked and asked for; linkFailed the links that could not be made.
	linked, linkTotal, linkFailed int
	// copied and copyTotal count the paths copied and listed; copyFailed the copies that failed.
	copied, copyTotal, copyFailed int
}

// setupWorktree starts the setup of a worktree that was made for the repository, when a setting asks for one.
func (x *gitscm) setupWorktree(r *repository, worktree string) {
	links := git.SanitizeWorktreeIncludePatterns(read(x.api, "git.worktreeSymlinkFolders", []string(nil)))
	includes := git.SanitizeWorktreeIncludePatterns(read(x.api, "git.worktreeIncludeFiles", []string(nil)))
	if dropped := len(read(x.api, "git.worktreeSymlinkFolders", []string(nil))) - len(links); dropped > 0 {
		x.logLine("git.worktreeSymlinkFolders: ignoring " + strconv.Itoa(dropped) + " pattern(s) containing line breaks")
	}
	if dropped := len(read(x.api, "git.worktreeIncludeFiles", []string(nil))) - len(includes); dropped > 0 {
		x.logLine("git.worktreeIncludeFiles: ignoring " + strconv.Itoa(dropped) + " pattern(s) containing line breaks")
	}
	if (len(links) == 0 && len(includes) == 0) || r.repo == nil {
		return
	}
	repo, ctx, root := r.repo, r.ctx, r.root
	go func() {
		start := time.Now()
		var res worktreeSetup
		linked := x.symlinkWorktreeFolders(ctx, repo, root, worktree, links, &res)
		x.copyWorktreeIncludeFiles(ctx, repo, root, worktree, includes, linked, &res)
		x.logLine("worktree " + worktree + " set up in " + time.Since(start).Round(time.Millisecond).String())
		x.api.Post(func() {
			if x.stopped.Load() {
				return
			}
			x.reportWorktreeSetup(res)
		})
	}()
}

// reportWorktreeSetup says what the setup did: the failures as warnings (VS Code's texts), and the counts of what was done.
func (x *gitscm) reportWorktreeSetup(res worktreeSetup) {
	if res.linkFailed > 0 {
		x.api.Notifications().Warn("Failed to create " + strconv.Itoa(res.linkFailed) + " worktree folder symlink(s).")
	}
	if res.copyFailed > 0 {
		x.api.Notifications().Warn("Failed to copy " + strconv.Itoa(res.copyFailed) + " folder(s)/file(s) to the worktree.")
	}
	var done []string
	if res.linked > 0 {
		done = append(done, "linked "+strconv.Itoa(res.linked)+" of "+strconv.Itoa(res.linkTotal)+" folder(s)")
	}
	if res.copied > 0 {
		done = append(done, "copied "+strconv.Itoa(res.copied)+" of "+strconv.Itoa(res.copyTotal)+" folder(s)/file(s)")
	}
	if len(done) > 0 {
		x.api.Notifications().Info("Worktree set up: " + strings.Join(done, " and ") + ".")
	}
}

// symlinkWorktreeFolders links the folders git.worktreeSymlinkFolders names and returns the ones it linked. Whatever goes wrong is
// logged and leaves the setup to go on.
func (x *gitscm) symlinkWorktreeFolders(ctx context.Context, repo *git.Repo, root, worktree string, patterns []string, res *worktreeSetup) []string {
	if len(patterns) == 0 {
		return nil
	}
	folders, err := symlinkCandidates(ctx, repo, patterns)
	if err != nil {
		x.logLine("worktree: cannot find the folders to link: " + git.MaskURLs(describe(err)))
		return nil
	}
	res.linkTotal = len(folders)
	var linked []string
	for _, folder := range folders {
		if ctx.Err() != nil {
			break
		}
		status, err := git.CreateWorktreeSymlink(root, worktree, folder)
		switch {
		case err != nil:
			res.linkFailed++
			x.logLine("worktree: cannot link " + folder + ": " + describe(err))
		case status == git.SymlinkCreated:
			res.linked++
			linked = append(linked, folder)
		default:
			x.logLine("worktree: skipped " + folder + " (" + string(status) + ")")
		}
	}
	return linked
}

// symlinkCandidates finds the folders of the main work tree to link: ignored by git, matching the patterns, and holding no tracked file.
func symlinkCandidates(ctx context.Context, repo *git.Repo, patterns []string) ([]string, error) {
	var ignored, matched, wholly string
	var errs [3]error
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		ignored, errs[0] = repo.LsIgnored(ctx, git.LsIgnoredOptions{ExcludeStandard: true})
	}()
	go func() {
		defer wg.Done()
		matched, errs[1] = repo.LsIgnored(ctx, git.LsIgnoredOptions{ExcludePatterns: patterns})
	}()
	go func() {
		defer wg.Done()
		wholly, errs[2] = repo.LsIgnored(ctx, git.LsIgnoredOptions{ExcludeStandard: true, Directory: true})
	}()
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	candidates := git.WorktreeSymlinkFolderCandidates(ignored, matched)
	if len(candidates) == 0 {
		return nil, nil
	}
	slashed := make([]string, len(candidates))
	for i, c := range candidates {
		slashed[i] = c + "/"
	}
	ignoredDirs, err := repo.CheckIgnoreFolders(ctx, slashed)
	if err != nil {
		return nil, err
	}
	// the folders the patterns alone match, as a repository with only those patterns would say (VS Code makes a scratch repository for
	// it; Emetos reads them with its own matcher, which starts no process and needs no trust for a scratch folder)
	rules := parseIgnore(strings.Join(patterns, "\n"))
	var matchedDirs []string
	for _, c := range slashed {
		if patternsMatchFolder(rules, strings.TrimSuffix(c, "/")) {
			matchedDirs = append(matchedDirs, c)
		}
	}
	return git.FilterWorktreeSymlinkFolders(candidates, ignoredDirs, strings.Join(matchedDirs, "\x00")+"\x00", wholly), nil
}

// patternsMatchFolder says whether the rules ignore a folder: the folder or one above it matches and no later rule un-ignores it.
func patternsMatchFolder(rules []ignoreRule, folder string) bool {
	for i := 0; i < len(folder); i++ {
		if folder[i] == '/' {
			if hit, found := matchIgnore(rules, folder[:i], true); found && hit {
				return true
			}
		}
	}
	hit, found := matchIgnore(rules, folder, true)
	return found && hit
}

// copyWorktreeIncludeFiles copies the ignored files that git.worktreeIncludeFiles names, except under the folders that were linked.
func (x *gitscm) copyWorktreeIncludeFiles(ctx context.Context, repo *git.Repo, root, worktree string, patterns, linked []string, res *worktreeSetup) {
	if len(patterns) == 0 {
		return
	}
	paths, err := includePaths(ctx, repo, worktree, patterns, linked)
	if err != nil {
		x.logLine("worktree: cannot find the files to copy: " + git.MaskURLs(describe(err)))
		return
	}
	res.copyTotal = len(paths)
	var mu sync.Mutex
	var wg sync.WaitGroup
	slots := make(chan struct{}, maxCopies)
	for _, p := range paths {
		slots <- struct{}{}
		wg.Add(1)
		go func() {
			defer func() { <-slots; wg.Done() }()
			copied, err := copyIgnored(ctx, filepath.Join(root, filepath.FromSlash(p)), filepath.Join(worktree, filepath.FromSlash(p)))
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err != nil:
				res.copyFailed++
				x.logLine("worktree: cannot copy " + p + ": " + describe(err))
			case copied:
				res.copied++
			}
		}()
	}
	wg.Wait()
}

// includePaths lists the ignored paths to copy into the worktree.
func includePaths(ctx context.Context, repo *git.Repo, worktree string, patterns, linked []string) ([]string, error) {
	var ignored, directories, included, tracked string
	var errs [4]error
	var wg sync.WaitGroup
	wg.Add(4)
	go func() {
		defer wg.Done()
		ignored, errs[0] = repo.LsIgnored(ctx, git.LsIgnoredOptions{ExcludeStandard: true})
	}()
	go func() {
		defer wg.Done()
		directories, errs[1] = repo.LsIgnored(ctx, git.LsIgnoredOptions{ExcludeStandard: true, Directory: true, NoEmptyDirectory: true})
	}()
	go func() {
		defer wg.Done()
		included, errs[2] = repo.LsIgnored(ctx, git.LsIgnoredOptions{ExcludePatterns: patterns})
	}()
	go func() { defer wg.Done(); tracked, errs[3] = repo.LsFilesIn(ctx, worktree) }()
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return git.ResolveWorktreeIncludePaths(ignored, included, directories, tracked, linked), nil
}

// copyIgnored copies a file, a link or a folder with what is in it, unless the target is there already (copied is then false). A link is
// copied as it is, not followed.
func copyIgnored(ctx context.Context, source, target string) (copied bool, err error) {
	if _, err := os.Lstat(target); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, errors.NewErr("GitWorktreeCopyFailed", err).SetMetadata("path", target, "step", "lstat")
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return false, errors.NewErr("GitWorktreeCopyFailed", err).SetMetadata("path", target, "step", "mkdir")
	}
	err = filepath.WalkDir(source, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		dst := filepath.Join(target, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			return os.MkdirAll(dst, info.Mode().Perm()|0o700)
		case info.Mode()&os.ModeSymlink != 0:
			to, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(to, dst)
		case info.Mode().IsRegular():
			return copyFile(path, dst, info.Mode().Perm())
		}
		return nil // sockets, devices and pipes are not copied
	})
	if err != nil {
		return false, errors.NewErr("GitWorktreeCopyFailed", err).SetMetadata("path", source, "step", "copy")
	}
	return true, nil
}

// copyFile copies the content of a regular file.
func copyFile(source, target string, mode fs.FileMode) (err error) {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }() // a file read only: closing cannot lose anything
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := out.Close(); err == nil {
			err = cerr
		}
	}()
	_, err = io.Copy(out, in)
	return err
}
