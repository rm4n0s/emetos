package gitscm

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"

	"github.com/rm4n0s/emetos/extension"
)

// The URI of a version of a file (SPEC §10.30 "Diffs of changes"): emetos-virtual:/git/<repository id>/<ref>/<path>, the form of VS Code's
// git: URI ({path, ref, submoduleOf} in the query) for a document that an extension provides. The repository id is a hash of the root
// of the repository, so the same repository has the same URIs in every run; the path is relative to the root, with slashes. The
// ref is:
//
//	HEAD, a branch, a tag or a commit id   the version in that commit
//	"" (empty)                             the index version
//	~                                      the version a file changed in the work tree is compared with: the index version
//	~1, ~2, ~3                             a stage of a conflicted file (base, ours, theirs)
//	index, wt                              the diff of a submodule (git diff --cached, git diff), whose path ends in .diff
//
// Ported from VS Code: extensions/git/src/uri.ts (toGitUri, fromGitUri, GitUriParams)

// ContentScheme is the scheme of the documents of the extension, the one resourceScheme == git in the conditions of the menus reads.
const ContentScheme = "git"

// The refs of the URIs that are not commits.
const (
	refIndex      = ""
	refWorkTree   = "~"
	refBase       = "~1"
	refOurs       = "~2"
	refTheirs     = "~3"
	refHEAD       = "HEAD"
	refSubIndex   = "index"
	refSubWorkTre = "wt"
)

// gitURI is a parsed URI of a version of a file.
type gitURI struct {
	// RepoID is the id of the repository.
	RepoID string
	// Ref says which version (see above).
	Ref string
	// Path is the path of the file relative to the root of the repository, with slashes. For a submodule it ends in ".diff".
	Path string
}

// repoID is the id of a repository in the URIs: the first twelve hexadecimal digits of the SHA-256 of its root.
func repoID(root string) string {
	sum := sha256.Sum256([]byte(root))
	return hex.EncodeToString(sum[:])[:12]
}

// toGitURI is the URI of the version ref of the file at path (absolute) in the repository with this root and id.
func toGitURI(id, root, path, ref string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = path
	}
	return gitURI{RepoID: id, Ref: ref, Path: filepath.ToSlash(rel)}.String()
}

// toSubmoduleURI is the URI of the diff of a submodule, "index" for the staged one and "wt" for the one of the work tree; ".diff" is added
// to the path as VS Code does.
func toSubmoduleURI(id, root, path, ref string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = path
	}
	return gitURI{RepoID: id, Ref: ref, Path: filepath.ToSlash(rel) + ".diff"}.String()
}

// String is the URI.
func (g gitURI) String() string {
	return extension.ContentURI(ContentScheme, g.RepoID+"/"+g.Ref+"/"+g.Path)
}

// parseGitURI reads a URI that String made; false for any other.
func parseGitURI(uri string) (gitURI, bool) {
	scheme, rest, ok := extension.SplitContentURI(uri)
	if !ok || scheme != ContentScheme {
		return gitURI{}, false
	}
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) != 3 || parts[0] == "" || parts[2] == "" {
		return gitURI{}, false
	}
	return gitURI{RepoID: parts[0], Ref: parts[1], Path: parts[2]}, true
}

// isSubmodule says that the URI is the diff of a submodule.
func (g gitURI) isSubmodule() bool {
	return (g.Ref == refSubIndex || g.Ref == refSubWorkTre) && strings.HasSuffix(g.Path, ".diff")
}

// filePath is the path of the file the URI stands for, below root.
func (g gitURI) filePath(root string) string {
	p := g.Path
	if g.isSubmodule() {
		p = strings.TrimSuffix(p, ".diff")
	}
	return filepath.Join(root, filepath.FromSlash(p))
}
