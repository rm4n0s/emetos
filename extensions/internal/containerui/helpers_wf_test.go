package containerui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// mustWrite writes a file, with the folders it needs.
func mustWrite(t testing.TB, p, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// noFolderAPI is the API of the rig with a scope that has no folder.
type noFolderAPI struct{ *fakeapi.API }

// Scope implements extension.API.
func (a noFolderAPI) Scope() extension.ScopeInfo {
	s := a.API.Scope()
	s.Folder = ""
	return s
}
