package fakeapi

import (
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/scmtest"
)

// observe lists the source controls as the view would show them, for the conformance suite.
func observe(a *API) []scmtest.Source {
	var out []scmtest.Source
	for _, s := range a.SourceControls() {
		in := s.input
		src := scmtest.Source{
			Handle: s.Handle(), ID: s.ID(), Label: s.Label(), RootURI: s.RootURI(), ContextValue: s.ContextValue(),
			Count: s.Count(), BadgeCount: s.BadgeCount(), Status: s.StatusBarCommands(), Busy: s.Busy(), ActiveName: s.ActiveRepositoryName(),
			Accept: s.AcceptInputCommand(), Button: s.ActionButton(),
			Input: scmtest.Input{Value: in.Value(), Placeholder: in.Placeholder(), Visible: in.Visible(), Enabled: in.Enabled(), FocusRequests: in.FocusRequests()},
		}
		for _, g := range s.Groups() {
			src.Groups = append(src.Groups, scmtest.Group{ID: g.ID(), Label: g.Label(), HideWhenEmpty: g.HideWhenEmpty(), ContextValue: g.ContextValue(), Resources: g.Resources()})
		}
		out = append(out, src)
	}
	return out
}

// TestSourceControlConformance runs the suite that the host passes too (internal/gui/exthost/scm_test.go).
func TestSourceControlConformance(t *testing.T) {
	scmtest.Run(t, func(t *testing.T) *scmtest.Harness {
		a := New(t, manifest(), extension.ScopeInfo{Folder: t.TempDir()})
		return &scmtest.Harness{
			API:     a,
			Observe: func() []scmtest.Source { return observe(a) },
			Type:    func(handle, text string) { a.SourceControlByHandle(handle).input.Type(text) },
			Validate: func(handle, text string) *extension.Validation {
				return a.SourceControlByHandle(handle).input.Validate(text)
			},
			Pump:       func() { a.Pump() },
			Deactivate: a.Deactivate,
			Failed:     a.Failed,
		}
	})
}

// TestTheFakeSourceControlFailsLikeTheHost checks the routes of the fake's errors, which have the tags of the host's.
func TestTheFakeSourceControlFailsLikeTheHost(t *testing.T) {
	a := New(t, manifest(), extension.ScopeInfo{Folder: t.TempDir()})
	spec := extension.SourceControlSpec{ID: "git", Label: "Git", RootURI: "file:///work/a"}
	sc, err := a.SourceControl().Create(spec)
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.SourceControl().Create(spec)
	route(t, err, "scmAPI.Create.ExtensionSCMDuplicate")
	_, err = a.SourceControl().Create(extension.SourceControlSpec{ID: "git"})
	route(t, err, "scmAPI.Create.ExtensionSCMInvalid")
	if _, err = sc.CreateGroup(extension.ResourceGroupSpec{ID: "g", Label: "G"}); err != nil {
		t.Fatal(err)
	}
	_, err = sc.CreateGroup(extension.ResourceGroupSpec{ID: "g", Label: "G"})
	route(t, err, "FakeSource.CreateGroup.ExtensionSCMGroupDuplicate")
	_, err = sc.CreateGroup(extension.ResourceGroupSpec{})
	route(t, err, "FakeSource.CreateGroup.ExtensionSCMGroupInvalid")
	sc.Dispose()
	_, err = sc.CreateGroup(extension.ResourceGroupSpec{ID: "late", Label: "L"})
	route(t, err, "FakeSource.CreateGroup.ExtensionSCMDisposed")
	a.Deactivate()
	_, err = a.SourceControl().Create(spec)
	route(t, err, "scmAPI.Create.ExtensionDeactivated")
	_, err = sc.CreateGroup(extension.ResourceGroupSpec{ID: "late", Label: "L"})
	route(t, err, "FakeSource.CreateGroup.ExtensionDeactivated")
}

func TestTheFakeKeepsWhatTheViewNeeds(t *testing.T) {
	a := New(t, manifest(), extension.ScopeInfo{Folder: t.TempDir()})
	sc, _ := a.SourceControl().Create(extension.SourceControlSpec{ID: "git", Label: "Git"})
	g, _ := sc.CreateGroup(extension.ResourceGroupSpec{ID: "g", Label: "G"})
	g.SetResources([]extension.ResourceState{{URI: "file:///a"}, {URI: "file:///b"}})
	src := a.SourceControlByHandle(sc.Handle())
	if src == nil || src.Group("g") == nil || src.Group("nope") != nil || len(src.Group("g").Resources()) != 2 || a.SourceControlByHandle("nope") != nil {
		t.Errorf("lookups: %v", src)
	}
	if got := len(a.SourceControls()); got != 1 {
		t.Errorf("%d source controls", got)
	}
}
