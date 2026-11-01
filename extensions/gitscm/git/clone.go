package git

import (
	"context"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/rm4n0s/errors"
)

// maxCloneFolders is how many folders Clone tries for a name that is taken ("repo", "repo-1", ... "repo-19"), as VS Code does.
const maxCloneFolders = 20

// CloneOptions are the options of Runner.Clone.
type CloneOptions struct {
	// Recursive clones the submodules too ("--recursive").
	Recursive bool
	// Ref checks out that branch or tag ("--branch").
	Ref string
	// Bare makes a bare clone.
	Bare bool
	// TargetName, when it is not empty, is the name of the folder made in the parent folder; it is used as it is, and a folder of that
	// name that has files in it fails the clone with GitCloneTargetExists. When it is empty the folder is named after the repository,
	// and a name that is taken gets the next free "-1", "-2"... (VS Code's).
	TargetName string
	// Progress, when set, gets the progress of the transfer (--progress).
	Progress func(Progress)
}

// CloneFolderName is the name of the folder a clone of the URL gets: the last element of its path without ".git" (the part after the
// last colon of an scp-like address), unescaped, or "repository" when nothing usable is left.
//
// Ported from VS Code: extensions/git/src/git.ts (Git.clone: baseFolderName)
func CloneFolderName(source string) string {
	name := strings.TrimSpace(source)
	if unescaped, err := url.PathUnescape(name); err == nil {
		name = unescaped
	}
	name = strings.TrimRight(name, "/\\")
	if i := strings.LastIndexAny(name, "/\\"); i >= 0 {
		name = name[i+1:]
	} else if i := strings.LastIndex(name, ":"); i >= 0 {
		name = name[i+1:] // host:repo.git
	}
	name = strings.TrimSuffix(name, ".git")
	if name == "" || name == "." || name == ".." || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "repository"
	}
	return name
}

// Clone clones the repository at source into a new folder of parentDir and returns the path of the folder. The folder is named after the
// repository (CloneOptions.TargetName), parentDir is made when it is missing, and the transfer is reported to o.Progress. It is a write
// command and not a background one: git may ask for a password through the askpass program of the Config, and the error has the tag
// AuthenticationFailed when none was given, RepositoryNotFound, CantAccessRemote and the other tags of the generic rules. The command runs
// outside every repository (a parentDir inside a repository that was not trusted is fine: git reads none of its files).
//
// A clone that fails or is cancelled leaves no folder: the one this call made is removed, with what git put in it. A folder that
// existed (an empty one named by TargetName) is emptied. A source that is a path is read as git would from parentDir ("~" is the home
// folder of the Config's environment).
//
// Ported from VS Code: extensions/git/src/git.ts (Git.clone)
func (r *Runner) Clone(ctx context.Context, source, parentDir string, o CloneOptions) (string, error) {
	source = strings.TrimSpace(source)
	if msg := ValidateRemoteURL(source); msg != "" {
		return "", errors.New("GitCloneURLInvalid", msg, "url", MaskURLs(source))
	}
	if o.Ref != "" {
		if err := refName(o.Ref); err != nil {
			return "", err
		}
	}
	if !filepath.IsAbs(parentDir) {
		return "", errors.New("GitCloneParentInvalid", "the folder to clone into is an absolute path", "parent", parentDir)
	}
	parentDir = filepath.Clean(parentDir)
	if o.TargetName != "" && (o.TargetName != filepath.Base(o.TargetName) || o.TargetName == "." || o.TargetName == "..") {
		return "", errors.New("GitCloneTargetInvalid", "the name of the folder of a clone is a name and not a path", "name", o.TargetName)
	}

	name := o.TargetName
	folder := ""
	existed := false
	if name != "" {
		folder = filepath.Join(parentDir, name)
		if _, err := os.Lstat(folder); err == nil {
			existed = true
			empty, err := emptyFolder(folder)
			if err != nil || !empty {
				return "", errors.New("GitCloneTargetExists", "the folder already exists and is not an empty directory", "path", folder)
			}
		}
	} else {
		base := CloneFolderName(source)
		name = base
		folder = filepath.Join(parentDir, name)
		for count := 1; count < maxCloneFolders; count++ {
			if _, err := os.Lstat(folder); err != nil {
				break
			}
			name = base + "-" + strconv.Itoa(count)
			folder = filepath.Join(parentDir, name)
		}
		if _, err := os.Lstat(folder); err == nil {
			return "", errors.New("GitCloneTargetExists", "the folder already exists and is not an empty directory", "path", folder)
		}
	}
	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		return "", errors.NewErr("GitCloneParentFailed", err).SetMetadata("parent", parentDir)
	}

	args := []string{"clone"}
	if o.Progress != nil {
		args = append(args, "--progress")
	}
	if o.Recursive {
		args = append(args, "--recursive")
	}
	if o.Bare {
		args = append(args, "--bare")
	}
	if o.Ref != "" {
		args = append(args, "--branch", o.Ref)
	}
	args = append(args, "--", r.localSource(source, parentDir), folder)
	_, err := r.Run(ctx, Command{Args: args, Progress: o.Progress})
	if err != nil {
		removePartialClone(folder, existed)
		return "", err
	}
	return folder, nil
}

// localSource makes the source a path git can read from outside parentDir: "~/x" is under the home folder of the environment of git,
// and a relative path is read from parentDir (what VS Code's working directory does). A URL and an scp-like address are left alone.
func (r *Runner) localSource(source, parentDir string) string {
	switch {
	case strings.Contains(source, "://"):
		return source
	case source == "~" || strings.HasPrefix(source, "~/"):
		if home := r.homeDir(); home != "" {
			return filepath.Join(home, strings.TrimPrefix(source, "~"))
		}
		return source
	case filepath.IsAbs(source):
		return source
	case reScpLike.MatchString(source) && !strings.HasPrefix(source, "./") && !strings.HasPrefix(source, "../"):
		return source
	}
	return filepath.Join(parentDir, source)
}

// homeDir is the HOME of the environment the Runner starts git with.
func (r *Runner) homeDir() string {
	env := r.cfg.Env
	if env == nil {
		env = os.Environ()
	}
	for i := len(env) - 1; i >= 0; i-- {
		if v, ok := strings.CutPrefix(env[i], "HOME="); ok {
			return v
		}
	}
	return ""
}

// emptyFolder says whether the folder has no entries.
func emptyFolder(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()
	names, err := f.Readdirnames(1)
	if errors.Is(err, io.EOF) {
		return true, nil
	}
	return len(names) == 0, err
}

// removePartialClone removes what a clone that failed or was stopped left in its folder: the folder when the clone made it, its entries
// when it was an empty folder before. Nothing else is touched, and a failure to remove is not an error of the clone (the clone's own
// failure is the one the caller needs).
func removePartialClone(folder string, existed bool) {
	if !existed {
		_ = os.RemoveAll(folder)
		return
	}
	entries, err := os.ReadDir(folder)
	if err != nil {
		return
	}
	for _, e := range entries {
		_ = os.RemoveAll(filepath.Join(folder, e.Name()))
	}
}
