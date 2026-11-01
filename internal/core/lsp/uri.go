package lsp

import (
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/errors"
)

// providedSchemes are the URI schemes of documents that a server makes up and an extension provides the text of: jdt://contents/... are the
// class files of Eclipse JDT LS (SPEC §10.34 "Java") and csharp:/<project>/decompiled/<Symbol>.cs the decompiled and generated sources of
// csharp-ls (SPEC §10.34 "C#"). A location in one of them is the read-only document "emetos-virtual:/<scheme>/<rest>", whose provider
// (extension.ContentProvider with that scheme) asks the server for the text, and the other way round. The list is read-only.
var providedSchemes = []providedScheme{{name: "jdt", prefix: "jdt://"}, {name: "csharp", prefix: "csharp:/", noSlash: true}}

// providedScheme is a scheme of documents a server makes up: its name (the scheme of the provider) and the start of its URIs, which is
// where the rest begins (jdt:// has two slashes, csharp:/ has one: the path of a project follows it).
type providedScheme struct {
	name, prefix string
	// noSlash says that the rest does not start with a slash: csharp://x is no URI of csharp-ls (its URIs have one slash and then the path
	// of the project, which has none at its start here), so it is left to the editor to refuse.
	noSlash bool
}

// ProvidedPath is the path of the virtual document that stands for a document the server made up: for "jdt://contents/java.base/String.class?=x"
// it is "/jdt/contents/java.base/String.class?=x" (the rest of the URI as it is, query and escapes included, so that it goes back to the server
// unchanged). It reports false for a URI of any other scheme and for one with nothing after the prefix; csharp://x is not one of csharp-ls's
// URIs (its path is the absolute path of the project, "csharp:/home/me/app/App.csproj/decompiled/System.Console.cs").
func ProvidedPath(s string) (string, bool) {
	for _, scheme := range providedSchemes {
		if rest, ok := strings.CutPrefix(s, scheme.prefix); ok && rest != "" && (!scheme.noSlash || !strings.HasPrefix(rest, "/")) {
			return "/" + scheme.name + "/" + rest, true
		}
	}
	return "", false
}

// providedURI is the URI the server knows a virtual document by, the reverse of ProvidedPath; false for any other virtual path.
func providedURI(path string) (string, bool) {
	for _, scheme := range providedSchemes {
		if rest, ok := strings.CutPrefix(path, "/"+scheme.name+"/"); ok && rest != "" {
			return scheme.prefix + rest, true
		}
	}
	return "", false
}

// DocumentURI is the URI of a document as a server sees it: "file:///home/me/a%20b.go" for a file (the
// path is percent-encoded, unlike document.URI.String, which is an identity and not an address),
// "untitled:Untitled-1" for an unsaved document, and "emetos-virtual:/path" for generated content.
func DocumentURI(u document.URI) string {
	switch u.Scheme {
	case document.SchemeFile:
		return (&url.URL{Scheme: "file", Path: u.Path}).String()
	case document.SchemeUntitled:
		return "untitled:" + u.Title()
	case document.SchemeVirtual:
		if uri, ok := providedURI(u.Path); ok {
			return uri
		}
	}
	return u.Scheme + ":" + (&url.URL{Path: u.Path}).EscapedPath()
}

// ParseDocumentURI reads a URI that a server sent. It fails with the tags DocumentURIInvalid and
// DocumentURIUnsupported (a scheme the editor has no documents for, such as "jar:"; the schemes of ProvidedPath become virtual documents).
func ParseDocumentURI(s string) (document.URI, error) {
	if path, ok := ProvidedPath(s); ok {
		return document.VirtualURI(path), nil
	}
	u, err := url.Parse(s)
	if err != nil {
		return document.URI{}, errors.NewErr("DocumentURIInvalid", err).SetMetadata("uri", s)
	}
	switch u.Scheme {
	case document.SchemeFile:
		if (u.Host != "" && u.Host != "localhost") || !filepath.IsAbs(u.Path) {
			return document.URI{}, errors.New("DocumentURIInvalid", "a file URI has no host and an absolute path", "uri", s)
		}
		return document.FileURI(filepath.Clean(u.Path)), nil
	case document.SchemeUntitled:
		name := u.Opaque
		if name == "" {
			name = strings.TrimPrefix(u.Path, "/")
		}
		n, err := strconv.Atoi(strings.TrimPrefix(name, "Untitled-"))
		if err != nil || n < 1 {
			return document.URI{}, errors.New("DocumentURIInvalid", "an untitled URI is untitled:Untitled- followed by a number from 1", "uri", s)
		}
		return document.UntitledURI(n), nil
	case document.SchemeVirtual:
		return document.VirtualURI(u.Path), nil
	}
	return document.URI{}, errors.New("DocumentURIUnsupported", "the editor has no documents with this URI scheme", "uri", s, "scheme", u.Scheme)
}
