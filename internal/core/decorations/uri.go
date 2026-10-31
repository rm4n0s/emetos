package decorations

import (
	"net/url"
	"strings"
)

// FileURI is the URI of a file at an absolute path the way the documents of Emetos spell it ("file:///home/a b/c.go": the path is
// not percent-encoded). The service takes the escaped spelling too (an extension may build the URI with net/url), see splitURI.
func FileURI(path string) string { return "file://" + path }

// splitURI cuts a URI into the key of its root (scheme and authority, "file://") and the segments of its path, with the query and
// the fragment, if any, as a last segment. Empty segments are dropped, so "file:///a/b/" and "file:///a/b" are the same place, and the
// path of a URI without "//" ("foo:bar") is its path. The segments are compared unescaped, so "file:///a%20b" and "file:///a b" are
// the same place (a segment that is not a valid escape is taken as it is).
func splitURI(uri string) (root string, segs []string) {
	scheme, rest, ok := strings.Cut(uri, ":")
	if !ok {
		return "", splitPath(uri)
	}
	var authority string
	if after, hasAuthority := strings.CutPrefix(rest, "//"); hasAuthority {
		end := strings.IndexAny(after, "/?#")
		if end < 0 {
			end = len(after)
		}
		authority, rest = after[:end], after[end:]
	}
	root = scheme + "://" + authority
	tail := ""
	if i := strings.IndexAny(rest, "?#"); i >= 0 {
		rest, tail = rest[:i], rest[i:]
	}
	segs = splitPath(rest)
	for i, seg := range segs {
		if strings.IndexByte(seg, '%') >= 0 {
			if u, err := url.PathUnescape(seg); err == nil {
				segs[i] = u
			}
		}
	}
	if tail != "" {
		segs = append(segs, tail)
	}
	return root, segs
}

// splitPath splits a path at its slashes and drops the empty parts.
func splitPath(p string) []string {
	if p == "" {
		return nil
	}
	parts := strings.Split(p, "/")
	out := parts[:0]
	for _, s := range parts {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// keyOf is the key of a URI in the sets of an event: the root and the segments joined by a character no segment holds.
func keyOf(root string, segs []string) string {
	return root + "\x00" + strings.Join(segs, "\x00")
}
