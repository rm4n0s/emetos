package fakeapi

import (
	"context"
	"slices"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/decorationstest"
	"github.com/rm4n0s/emetos/extension/editordecorationstest"
	"github.com/rm4n0s/emetos/extension/terminalenvtest"
)

// TestDecorationsConformance runs the suite that the host passes too (internal/gui/exthost/decorations_test.go).
func TestDecorationsConformance(t *testing.T) {
	decorationstest.Run(t, func(t *testing.T) *decorationstest.Harness {
		a := New(t, manifest(), extension.ScopeInfo{Folder: t.TempDir()})
		seenFired := map[*FakeDecorations]int{}
		return &decorationstest.Harness{
			API: a,
			Ask: func(uri string) (decorationstest.Seen, bool) {
				d, ok := a.FileDecoration(uri)
				return decorationstest.Seen{Badge: d.Badge, Icon: d.Icon, Tooltip: d.Tooltip, Color: d.Color}, ok
			},
			Told: func(uri string) bool {
				hit := false
				for _, f := range a.FileDecorationProviders() {
					fired := f.Fired()
					for _, uris := range fired[seenFired[f]:] {
						hit = hit || slices.Contains(uris, uri)
					}
					seenFired[f] = len(fired)
				}
				return hit
			},
			Pump:       func() { a.Pump() },
			Deactivate: a.Deactivate,
			Failed:     a.Failed,
		}
	})
}

// TestTheFakeDecorationsFailLikeTheHost checks the routes of the fake's errors, which have the tags of the host's.
func TestTheFakeDecorationsFailLikeTheHost(t *testing.T) {
	a := New(t, manifest(), extension.ScopeInfo{Folder: t.TempDir()})
	_, err := a.Decorations().Register(nil)
	route(t, err, "decorationsAPI.Register.ExtensionDecorationsInvalid")
	a.Deactivate()
	_, err = a.Decorations().Register(nil)
	route(t, err, "decorationsAPI.Register.ExtensionDeactivated")
}

// TestEditorDecorationsConformance runs the suite that the host passes too (internal/gui/exthost/editordecorations_test.go).
func TestEditorDecorationsConformance(t *testing.T) {
	editordecorationstest.Run(t, func(t *testing.T) *editordecorationstest.Harness {
		m := manifest()
		m.Contributes.Commands = append(m.Contributes.Commands, extension.CommandSpec{ID: editordecorationstest.OwnCommand, Title: "Run"})
		a := New(t, m, extension.ScopeInfo{Folder: t.TempDir()})
		var ran []any
		if err := a.Commands().Handle(editordecorationstest.OwnCommand, func(_ context.Context, args any) error { ran = append(ran, args); return nil }); err != nil {
			t.Fatal(err)
		}
		return &editordecorationstest.Harness{
			API:        a,
			Seen:       a.Annotations,
			Follow:     a.FollowAnnotationLink,
			Ran:        func() []any { return ran },
			Pump:       func() { a.Pump() },
			Deactivate: a.Deactivate,
		}
	})
}

// TestTheFakeEditorDecorationsFailLikeTheHost checks the routes of the fake's errors, which have the tags of the host's.
func TestTheFakeEditorDecorationsFailLikeTheHost(t *testing.T) {
	a := New(t, manifest(), extension.ScopeInfo{Folder: t.TempDir()})
	route(t, a.EditorDecorations().Set("", "file:///a", nil), "editorDecorationsAPI.Set.ExtensionEditorDecorationsInvalid")
	a.Deactivate()
	route(t, a.EditorDecorations().Set("k", "file:///a", nil), "editorDecorationsAPI.Set.ExtensionDeactivated")
}

// TestTerminalEnvironmentConformance runs the suite that the host passes too (internal/gui/exthost/terminalenv_test.go).
func TestTerminalEnvironmentConformance(t *testing.T) {
	terminalenvtest.Run(t, func(t *testing.T) *terminalenvtest.Harness {
		m, m2 := manifest(), manifest()
		m.ID, m.Name, m2.ID, m2.Name = "emetos.envtest", "Ext one", "emetos.envother", "Ext two"
		a, b := New(t, m, extension.ScopeInfo{}), New(t, m2, extension.ScopeInfo{})
		shared := NewTerminalSources()
		a.ShareTerminalEnvironment(shared)
		b.ShareTerminalEnvironment(shared)
		return &terminalenvtest.Harness{
			API: a, ID: "emetos.envtest", Name: "Ext one", Second: b, SecondID: "emetos.envother", SecondName: "Ext two",
			Deactivate: a.Deactivate,
		}
	})
}

// TestTheFakeTerminalEnvironmentFailsLikeTheHost checks the routes of the fake's errors, which have the tags of the host's.
func TestTheFakeTerminalEnvironmentFailsLikeTheHost(t *testing.T) {
	a := New(t, manifest(), extension.ScopeInfo{})
	_, err := a.TerminalEnvironment().Contribute(nil)
	route(t, err, "terminalEnvironmentAPI.Contribute.ExtensionTerminalEnvironmentInvalid")
	a.Deactivate()
	_, err = a.TerminalEnvironment().Contribute(func() (map[string]string, string) { return nil, "" })
	route(t, err, "terminalEnvironmentAPI.Contribute.ExtensionDeactivated")
}

// TestAFakeSourceThatPanicsTakesTheFakeOff mirrors the host: a panic in a source fails the extension.
func TestAFakeSourceThatPanicsTakesTheFakeOff(t *testing.T) {
	a := New(t, manifest(), extension.ScopeInfo{})
	if _, err := a.TerminalEnvironment().Contribute(func() (map[string]string, string) { panic("boom") }); err != nil {
		t.Fatal(err)
	}
	if got := a.TerminalEnvironment().Contributions(); len(got) != 0 || !a.Failed() {
		t.Errorf("contributions %+v, failed %v", got, a.Failed())
	}
}
