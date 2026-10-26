package golang

import (
	"slices"
	"testing"

	"github.com/rm4n0s/emetos/extension"
)

// TestGoDeclaresGopls checks the declaration of the first language server (SPEC §9.12 "The first server").
func TestGoDeclaresGopls(t *testing.T) {
	m := New().Manifest()
	if err := extension.CheckManifest(m); err != nil {
		t.Fatal(err)
	}
	if len(m.Contributes.LanguageServers) != 1 {
		t.Fatalf("servers = %+v", m.Contributes.LanguageServers)
	}
	s := m.Contributes.LanguageServers[0]
	if s.ID != "gopls" || s.Program != "gopls" || s.PathSetting != "go.goplsPath" || s.ConfigurationSection != "go.gopls" || !slices.Contains(s.Languages, "go") || s.OnActivation {
		t.Errorf("server = %+v", s)
	}
	if got := extension.TraceSetting(m.ID, s); got != "emetos.golang.gopls.trace.server" {
		t.Errorf("trace setting = %s", got)
	}
}
