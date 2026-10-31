package fakeapi

import (
	"slices"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/decorationstest"
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
