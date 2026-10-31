package git

import (
	"bytes"
	"context"
	"io"

	"github.com/rm4n0s/errors"
)

// ObjectName is the name git knows a version of a file by (cat-file's "<object>"): the index version of path for ref "" (and "~", "index",
// which the diff of a file changed in the work tree reads), a stage of a conflicted file for "~1", "~2" and "~3" (":1:path"), and the
// version in a commit for any other ref (HEAD, a branch, a tag, a hash): "<ref>:<path>". The path is relative to the work tree, with
// slashes.
//
// Ported from VS Code: extensions/git/src/fileSystemProvider.ts (sanitizeRef), extensions/git/src/git.ts (Repository.buffer).
func ObjectName(ref, path string) string {
	switch {
	case ref == "" || ref == "~" || ref == "index":
		return ":" + path
	case len(ref) == 2 && ref[0] == '~' && ref[1] >= '1' && ref[1] <= '3':
		return ":" + ref[1:] + ":" + path
	}
	return ref + ":" + path
}

// ReadVersion reads a version of a file (ObjectName gives its name) with "git cat-file blob". found is false when git has no such
// version: the file is not in the index or the commit, the commit is unknown, or the path exists on disk but not there. A file larger than
// MaxBlobSize is not read: the error has the tag GitContentTooLarge. The content is raw, with no textconv and no filter.
func (r *Repo) ReadVersion(ctx context.Context, ref, path string) (data []byte, found bool, err error) {
	data, err = r.ReadBlob(ctx, ObjectName(ref, path))
	if err != nil {
		if appErr, ok := errors.FromError(err); ok && (appErr.Tag == "CantOpenResource" || appErr.Tag == "WrongCase") {
			return nil, false, nil
		}
		return nil, false, err
	}
	return data, true, nil
}

// DiffPath returns the text of "git diff" (or "git diff --cached" for cached) for one path, which is what the diff of a submodule
// shows: the "Subproject commit" lines of the old and the new commit. The output is bounded by MaxBlobSize.
//
// Ported from VS Code: extensions/git/src/git.ts (Repository.diffWithHEAD, diffIndexWithHEAD).
func (r *Repo) DiffPath(ctx context.Context, cached bool, path string) (string, error) {
	args := []string{"diff"}
	if cached {
		args = append(args, "--cached")
	}
	var out string
	err := r.stream(ctx, ScopeIndex|ScopeWorkTree|ScopeRefs, Command{Args: args, Paths: []string{path}}, func(stdout io.Reader) error {
		data, err := io.ReadAll(io.LimitReader(stdout, MaxBlobSize+1))
		if err != nil {
			return err
		}
		if len(data) > MaxBlobSize {
			return errors.New("GitContentTooLarge", "the diff is too large to show", "path", path, "limit", MaxBlobSize)
		}
		out = string(data)
		return nil
	})
	return out, err
}

// LooksBinary tells whether file content is not text: git's own test, a NUL byte in the first 8,000 bytes.
func LooksBinary(data []byte) bool {
	if len(data) > 8000 {
		data = data[:8000]
	}
	return bytes.IndexByte(data, 0) >= 0
}
