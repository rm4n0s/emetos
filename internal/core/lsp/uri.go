package lsp

import (
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/errors"
)

// DocumentURI is the URI of a document as a server sees it: "file:///home/me/a%20b.go" for a file (the
// path is percent-encoded, unlike document.URI.String, which is an identity and not an address),
// "untitled:Untitled-1" for an unsaved document, and "emetos-virtual:/path" for generated content.
func DocumentURI(u document.URI) string {
	switch u.Scheme {
	case document.SchemeFile:
		return (&url.URL{Scheme: "file", Path: u.Path}).String()
	case document.SchemeUntitled:
		return "untitled:" + u.Title()
	}
	return u.Scheme + ":" + (&url.URL{Path: u.Path}).EscapedPath()
}

// ParseDocumentURI reads a URI that a server sent. It fails with the tags DocumentURIInvalid and
// DocumentURIUnsupported (a scheme the editor has no documents for, such as "jar:").
func ParseDocumentURI(s string) (document.URI, error) {
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
