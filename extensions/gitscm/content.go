package gitscm

import (
	"context"
	"path"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/git"
)

// The documents of the versions of files (SPEC §10.30 "Diffs of changes"): the extension provides the text of every
// emetos-virtual:/git/... URI (uri.go). The text is made off the UI thread by git cat-file in the repository the id names; a version
// that git does not have is an empty text, a file that looks binary is refused with the notice of the editor, and a file over 16 MiB is
// refused with its own message.
//
// Ported from VS Code: extensions/git/src/fileSystemProvider.ts (readFile, sanitizeRef)

// contentRepos are the opened repositories by id, which the provider reads from any goroutine (the repositories themselves belong to the
// UI thread).
type contentRepos struct {
	mu   sync.Mutex
	byID map[string]*git.Repo
}

func (c *contentRepos) set(id string, repo *git.Repo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.byID == nil {
		c.byID = map[string]*git.Repo{}
	}
	c.byID[id] = repo
}

func (c *contentRepos) remove(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.byID, id)
}

func (c *contentRepos) get(id string) *git.Repo {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.byID[id]
}

// reSafeRef is what a ref of a URI may be made of; a ref that starts with "-" would be read by git as an option.
var reSafeRef = regexp.MustCompile(`^[A-Za-z0-9._/~^@{}+-]*$`)

// provideContent registers the provider of the git: documents. A scope that cannot show documents has no diffs; the extension goes on
// without them.
func (x *gitscm) provideContent() {
	reg, err := x.api.Documents().ProvideContent(extension.ContentProvider{
		Scheme: ContentScheme,
		Label: func(uri string) string {
			g, ok := parseGitURI(uri)
			if !ok {
				return ""
			}
			return path.Base(g.Path)
		},
		Language: func(uri string) string {
			if g, ok := parseGitURI(uri); ok && g.isSubmodule() {
				lang, _ := x.api.Languages().Resolve("diff")
				return lang
			}
			return ""
		},
		Text: x.contentText,
	})
	if err != nil {
		// a host without a workbench (the tests of the languages make one) shows no documents: nothing to warn about
		if appErr, ok := errors.FromError(err); !ok || appErr.Tag != "ExtensionDocumentUnavailable" {
			x.log.Warn("the documents of the versions of files cannot be provided", "err", err)
		}
		return
	}
	x.content.reg = reg
}

// contentText is the text of the document of a URI. It runs on a goroutine of the window.
func (x *gitscm) contentText(ctx context.Context, uri string) (string, error) {
	g, ok := parseGitURI(uri)
	if !ok {
		return "", errors.New("GitContentURIInvalid", "this is not the URI of a version of a file", "uri", uri)
	}
	if !reSafeRef.MatchString(g.Ref) || strings.HasPrefix(g.Ref, "-") {
		return "", errors.New("GitContentRefInvalid", "the ref of this version is not a name git accepts here", "uri", uri)
	}
	repo := x.content.repos.get(g.RepoID)
	if repo == nil {
		return "", errors.New("GitContentRepositoryGone", "the repository of this version is not open", "uri", uri)
	}
	if g.isSubmodule() {
		return repo.DiffPath(ctx, g.Ref == refSubIndex, strings.TrimSuffix(g.Path, ".diff"))
	}
	data, found, err := repo.ReadVersion(ctx, g.Ref, g.Path)
	if err != nil {
		return "", err
	}
	if !found {
		return "", nil
	}
	if git.LooksBinary(data) {
		return "", errors.New("ContentBinary", "the file looks binary", "uri", uri)
	}
	return decodeText(data), nil
}

// decodeText turns the bytes of a version into text: UTF-8 with the byte order mark taken off and the bytes that are not UTF-8 shown as
// the replacement character.
func decodeText(data []byte) string {
	data = []byte(strings.TrimPrefix(string(data), "\xef\xbb\xbf"))
	if utf8.Valid(data) {
		return string(data)
	}
	return strings.ToValidUTF8(string(data), "�")
}

// contentState is what the extension keeps for the provided documents.
type contentState struct {
	repos contentRepos
	reg   extension.ContentRegistration
}

// contentChanged tells the window that the open documents of a repository may have changed: those of the index and the stages always,
// those of a commit only when HEAD moved (headMoved).
func (r *repository) contentChanged(headMoved bool) {
	x := r.m.x
	if x.content.reg == nil {
		return
	}
	prefix := extension.ContentURI(ContentScheme, r.id+"/")
	var uris []string
	for _, uri := range x.api.Documents().Open() {
		g, ok := parseGitURI(uri)
		if !ok || !strings.HasPrefix(uri, prefix) {
			continue
		}
		if g.Ref == refHEAD && !headMoved {
			continue
		}
		uris = append(uris, uri)
	}
	if len(uris) > 0 {
		x.content.reg.Changed(uris...)
	}
}
