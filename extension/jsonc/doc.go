// Package jsonc parses JSON with comments and edits it while keeping the comments (SPEC §11.1). It lives under extension/ with
// nothing of Emetos imported (the standard library and github.com/rm4n0s/errors only), so the editor's core and the extensions
// both use it: the HTTP client keeps its collections in files it writes this way (SPEC §10.33).
package jsonc
