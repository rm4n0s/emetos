package git

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/rm4n0s/errors"
)

// Installation is a git program that was found and can be used.
type Installation struct {
	// Path is the program's path.
	Path string
	// Version is its version, at least MinVersion.
	Version Version
	// Raw is the output of "git --version".
	Raw string
}

// FindOptions tune Find.
type FindOptions struct {
	// Env is the environment of "git --version"; nil means the environment of the process. The PATH lookup uses the PATH of
	// the process.
	Env []string
	// Log receives a line for every candidate that was not usable (VS Code logs them to the Git channel); may be nil.
	Log func(line string)
}

// Find looks for git: the candidates in hints (the setting git.path: a string or an array, in order), then "git" on PATH. It
// runs "git --version" once for the candidate it takes, outside any repository, and returns the first one that is at least
// MinVersion (2.30). When some git was found but all are older the error has the tag GitTooOld with the path and the
// version of the first one; when none was found, GitNotFound with the candidates tried.
//
// Ported from VS Code: extensions/git/src/git.ts (findGit, findSpecificGit).
func Find(ctx context.Context, hints []string, opts FindOptions) (Installation, error) {
	candidates := append(append([]string(nil), hints...), "git")
	var tooOld *Installation
	var tried []string
	for _, c := range candidates {
		if c == "" {
			continue
		}
		path, err := exec.LookPath(c)
		if err != nil {
			tried = append(tried, c)
			if opts.Log != nil {
				opts.Log("Unable to find git: \"" + c + "\". Error: " + err.Error())
			}
			continue
		}
		inst, err := probe(ctx, path, opts.Env)
		if err != nil {
			tried = append(tried, c)
			if opts.Log != nil {
				opts.Log("Unable to use git: \"" + c + "\". Error: " + err.Error())
			}
			continue
		}
		if inst.Version.Compare(MinVersion) < 0 {
			if tooOld == nil {
				tooOld = &inst
			}
			continue
		}
		return inst, nil
	}
	if tooOld != nil {
		return Installation{}, errors.New("GitTooOld", "git "+tooOld.Version.String()+" is older than "+MinVersion.String(),
			"path", tooOld.Path, "version", tooOld.Version.String(), "minimum", MinVersion.String())
	}
	return Installation{}, errors.New("GitNotFound", "git installation not found", "candidates", tried)
}

// probe runs "git --version" and parses it.
func probe(ctx context.Context, path string, env []string) (Installation, error) {
	if env == nil {
		env = os.Environ()
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version")
	cmd.Dir = rootDir()
	cmd.Env = withVars(removeVars(env, "LC_ALL"), "GIT_TERMINAL_PROMPT=0", "GIT_PAGER=cat", "LC_MESSAGES=C", "LANGUAGE=en")
	out, err := cmd.Output()
	if err != nil {
		return Installation{}, errors.NewErr("GitVersionFailed", err).SetMetadata("path", path)
	}
	v, err := ParseVersion(string(out))
	if err != nil {
		return Installation{}, err
	}
	return Installation{Path: path, Version: v, Raw: trimSpace(string(out))}, nil
}

// rootDir is the working directory of the commands that need no repository: the root of the file system, which is a
// repository in no real setup.
func rootDir() string {
	return string(filepath.Separator)
}

// notExist says whether err means that a file or folder is missing.
func notExist(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, exec.ErrNotFound)
}
