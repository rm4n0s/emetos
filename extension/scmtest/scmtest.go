package scmtest

import (
	"reflect"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// Source is a source control as the view would show it.
type Source struct {
	// Handle, ID, Label, RootURI and ContextValue are what the extension created it with (and the handle it was given).
	Handle, ID, Label, RootURI, ContextValue string
	// Count is the count the extension set, -1 when it set none; BadgeCount is what the badge of the repository shows.
	Count, BadgeCount int
	// Status are the status bar commands.
	Status []extension.StatusCommand
	// Busy is the progress bar, ActiveName the name set for the active repository (empty for none).
	Busy       bool
	ActiveName string
	// Accept is the accept input command, empty ID for none; Button the action button, nil for none.
	Accept extension.Command
	Button *extension.ActionButton
	// Input is the commit message box.
	Input Input
	// Groups are the groups in the order they were created.
	Groups []Group
}

// Input is the commit message box as the view would show it.
type Input struct {
	// Value, Placeholder, Visible and Enabled are what the box holds and shows; FocusRequests counts the asks for the focus.
	Value, Placeholder string
	Visible, Enabled   bool
	FocusRequests      int
}

// Group is a group of resources as the view would show it.
type Group struct {
	// ID, Label, HideWhenEmpty and ContextValue are the properties of the group.
	ID, Label     string
	HideWhenEmpty bool
	ContextValue  string
	// Resources are the resources, in the order they were given.
	Resources []extension.ResourceState
}

// Harness is what an implementation gives the suite.
type Harness struct {
	// API is the API of the extension under test, in an activation that is on.
	API extension.API
	// Observe lists the source controls that are shown, in the order they were created.
	Observe func() []Source
	// Type is the user typing in the input of the source control with the handle: the text becomes the text of the box.
	Type func(handle, text string)
	// Validate is the view asking for the validation message of a text in the input of the source control with the handle.
	Validate func(handle, text string) *extension.Validation
	// Pump lets the work posted to the UI thread run.
	Pump func()
	// Deactivate ends the activation, as closing the window tab does.
	Deactivate func()
	// Failed tells whether the implementation took the extension off because of a panic in one of its callbacks.
	Failed func() bool
}

// Run runs the suite: newHarness makes a fresh harness for each test.
func Run(t *testing.T, newHarness func(t *testing.T) *Harness) {
	t.Helper()
	tests := []struct {
		name string
		run  func(t *testing.T, h *Harness)
	}{
		{"a source control is created with what the spec says", testCreate},
		{"the id and the root tell a source control, and a duplicate is refused", testDuplicates},
		{"a source control without an id or a label is refused", testInvalid},
		{"the setters of a source control are seen by the view", testSetters},
		{"the input holds, changes and validates", testInput},
		{"groups and their resources", testGroups},
		{"a group is refused without an id or twice", testGroupErrors},
		{"disposing a source control removes it and frees its id", testDispose},
		{"the end of the activation removes everything and refuses new calls", testDeactivation},
		{"a panic in the validation function takes the extension off", testValidatePanics},
		{"a panic in an OnChange function takes the extension off", testChangePanics},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { tt.run(t, newHarness(t)) })
	}
}

// wantTag fails the test unless err is an error of Emetos with the tag.
func wantTag(t *testing.T, err error, tag string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected an *errors.Error with the tag %s, got %T (%v)", tag, err, err)
	}
	if appErr.Tag != tag {
		t.Errorf("tag = %s (%s), want %s", appErr.Tag, appErr.Route(), tag)
	}
}

func create(t *testing.T, h *Harness, spec extension.SourceControlSpec) extension.SourceControl {
	t.Helper()
	sc, err := h.API.SourceControl().Create(spec)
	if err != nil {
		t.Fatalf("Create(%+v): %v", spec, err)
	}
	return sc
}

func only(t *testing.T, h *Harness) Source {
	t.Helper()
	list := h.Observe()
	if len(list) != 1 {
		t.Fatalf("%d source controls shown, want 1: %+v", len(list), list)
	}
	return list[0]
}

func testCreate(t *testing.T, h *Harness) {
	sc := create(t, h, extension.SourceControlSpec{ID: "git", Label: "Git", RootURI: "file:///work/repo", ContextValue: "repository"})
	if sc.Handle() == "" {
		t.Error("no handle")
	}
	s := only(t, h)
	want := Source{
		Handle: sc.Handle(), ID: "git", Label: "Git", RootURI: "file:///work/repo", ContextValue: "repository", Count: -1,
		Input: Input{Visible: true, Enabled: true},
	}
	if !reflect.DeepEqual(s, want) {
		t.Errorf("source = %+v\nwant   %+v", s, want)
	}
	other := create(t, h, extension.SourceControlSpec{ID: "git", Label: "Git", RootURI: "file:///work/other"})
	if other.Handle() == sc.Handle() {
		t.Errorf("two source controls have the handle %q", sc.Handle())
	}
	if got := len(h.Observe()); got != 2 {
		t.Errorf("%d source controls shown, want 2", got)
	}
}

func testDuplicates(t *testing.T, h *Harness) {
	spec := extension.SourceControlSpec{ID: "git", Label: "Git", RootURI: "file:///work/a"}
	create(t, h, spec)
	_, err := h.API.SourceControl().Create(spec)
	wantTag(t, err, "ExtensionSCMDuplicate")
	// the same id for another root is another source control, and so is another id for the same root
	create(t, h, extension.SourceControlSpec{ID: "git", Label: "Git", RootURI: "file:///work/b"})
	create(t, h, extension.SourceControlSpec{ID: "svn", Label: "Subversion", RootURI: "file:///work/a"})
	create(t, h, extension.SourceControlSpec{ID: "git", Label: "Git"})
	_, err = h.API.SourceControl().Create(extension.SourceControlSpec{ID: "git", Label: "Other label"})
	wantTag(t, err, "ExtensionSCMDuplicate")
	if got := len(h.Observe()); got != 4 {
		t.Errorf("%d source controls shown, want 4", got)
	}
}

func testInvalid(t *testing.T, h *Harness) {
	for _, spec := range []extension.SourceControlSpec{{Label: "Git"}, {ID: "git"}, {ID: "  ", Label: "Git"}, {ID: "git", Label: "\t"}} {
		_, err := h.API.SourceControl().Create(spec)
		wantTag(t, err, "ExtensionSCMInvalid")
	}
	if got := len(h.Observe()); got != 0 {
		t.Errorf("%d source controls shown, want none", got)
	}
}

func testSetters(t *testing.T, h *Harness) {
	sc := create(t, h, extension.SourceControlSpec{ID: "git", Label: "Git", RootURI: "file:///work/repo"})
	sc.SetCount(7)
	sc.SetStatusBarCommands([]extension.StatusCommand{{Text: "$(git-branch) main", Tooltip: "main, Checkout Branch/Tag...", Command: "git.checkout"}, {Text: "$(sync)", Command: "git.sync", Args: "x"}})
	sc.SetBusy(true)
	sc.SetActiveRepositoryName("worktree")
	sc.SetAcceptInputCommand(extension.Command{ID: "git.commit", Title: "Commit"})
	button := &extension.ActionButton{
		Command: extension.Command{ID: "git.commit", Title: "$(check) Commit", Tooltip: "Commit"}, Enabled: true, Description: "Commit the staged changes",
		Secondary: [][]extension.Command{{{ID: "git.commitAmend", Title: "Commit (Amend)"}}, {{ID: "git.commitSigned", Title: "Commit (Signed Off)"}}},
	}
	sc.SetActionButton(button)
	button.Secondary[0][0].ID = "changed" // the view has its own copy
	s := only(t, h)
	if s.Count != 7 || s.BadgeCount != 7 || !s.Busy || s.ActiveName != "worktree" || s.Accept.ID != "git.commit" {
		t.Errorf("source = %+v", s)
	}
	if len(s.Status) != 2 || s.Status[0].Command != "git.checkout" || s.Status[1].Args != "x" || s.Status[0].Text != "$(git-branch) main" {
		t.Errorf("status = %+v", s.Status)
	}
	if s.Button == nil || !s.Button.Enabled || s.Button.Command.ID != "git.commit" || s.Button.Description != "Commit the staged changes" ||
		len(s.Button.Secondary) != 2 || s.Button.Secondary[0][0].ID != "git.commitAmend" || s.Button.Secondary[1][0].Title != "Commit (Signed Off)" {
		t.Errorf("button = %+v", s.Button)
	}
	sc.SetCount(-1)
	sc.SetBusy(false)
	sc.SetActiveRepositoryName("")
	sc.SetAcceptInputCommand(extension.Command{})
	sc.SetActionButton(nil)
	sc.SetStatusBarCommands(nil)
	s = only(t, h)
	if s.Count != -1 || s.Busy || s.ActiveName != "" || s.Accept.ID != "" || s.Button != nil || len(s.Status) != 0 {
		t.Errorf("source after clearing = %+v", s)
	}
}

func testInput(t *testing.T, h *Harness) {
	sc := create(t, h, extension.SourceControlSpec{ID: "git", Label: "Git", RootURI: "file:///work/repo"})
	in := sc.Input()
	var seen []string
	cancel := in.OnChange(func(v string) { seen = append(seen, v) })
	in.SetPlaceholder(`Message (Ctrl+Enter to commit on "main")`)
	in.SetVisible(false)
	in.SetEnabled(false)
	in.SetValue("from the extension")
	h.Type(sc.Handle(), "from the user")
	h.Pump()
	if in.Value() != "from the user" {
		t.Errorf("Value = %q", in.Value())
	}
	if want := []string{"from the extension", "from the user"}; !reflect.DeepEqual(seen, want) {
		t.Errorf("OnChange saw %q, want %q", seen, want)
	}
	s := only(t, h).Input
	if s.Value != "from the user" || s.Placeholder != `Message (Ctrl+Enter to commit on "main")` || s.Visible || s.Enabled {
		t.Errorf("input = %+v", s)
	}
	cancel()
	h.Type(sc.Handle(), "after cancel")
	h.Pump()
	if len(seen) != 2 {
		t.Errorf("OnChange called after cancel: %q", seen)
	}
	in.Focus()
	in.Focus()
	if got := only(t, h).Input.FocusRequests; got != 2 {
		t.Errorf("FocusRequests = %d, want 2", got)
	}
	// validation
	if v := h.Validate(sc.Handle(), "x"); v != nil {
		t.Errorf("validation without a check = %+v", v)
	}
	in.SetValidation(func(value string) *extension.Validation {
		switch {
		case value == "":
			return nil
		case len(value) > 5:
			return &extension.Validation{Message: "too long", Level: extension.ValidationWarning}
		}
		return &extension.Validation{Message: "short", Level: extension.ValidationInfo}
	})
	for _, c := range []struct {
		text string
		want *extension.Validation
	}{
		{"", nil},
		{"abc", &extension.Validation{Message: "short", Level: extension.ValidationInfo}},
		{"abcdefg", &extension.Validation{Message: "too long", Level: extension.ValidationWarning}},
	} {
		if got := h.Validate(sc.Handle(), c.text); !reflect.DeepEqual(got, c.want) {
			t.Errorf("validation of %q = %+v, want %+v", c.text, got, c.want)
		}
	}
	in.SetValidation(nil)
	if v := h.Validate(sc.Handle(), "abcdefg"); v != nil {
		t.Errorf("validation after removing the check = %+v", v)
	}
}

func testGroups(t *testing.T, h *Harness) {
	sc := create(t, h, extension.SourceControlSpec{ID: "git", Label: "Git", RootURI: "file:///work/repo"})
	merge, err := sc.CreateGroup(extension.ResourceGroupSpec{ID: "merge", Label: "Merge Changes", HideWhenEmpty: true, ContextValue: "merge"})
	if err != nil {
		t.Fatal(err)
	}
	wt, err := sc.CreateGroup(extension.ResourceGroupSpec{ID: "workingTree", Label: "Changes"})
	if err != nil {
		t.Fatal(err)
	}
	if merge.ID() != "merge" || wt.ID() != "workingTree" {
		t.Errorf("ids = %q, %q", merge.ID(), wt.ID())
	}
	resources := []extension.ResourceState{
		{
			URI: "file:///work/repo/src/loader.go", ContextValue: "modified", Command: &extension.Command{ID: "git.openChange", Title: "Open", Args: "file:///work/repo/src/loader.go"},
			Decorations:                    extension.ResourceDecorations{Icon: "diff-modified", Tooltip: "Modified", Color: "gitDecoration.modifiedResourceForeground"},
			MultiFileDiffEditorOriginalURI: "git:/src/loader.go", MultiFileDiffEditorModifiedURI: "file:///work/repo/src/loader.go",
		},
		{URI: "file:///work/repo/old.go", Decorations: extension.ResourceDecorations{Tooltip: "Deleted", StrikeThrough: true, Faded: true}},
	}
	wt.SetResources(resources)
	resources[0].URI = "changed" // the view has its own copy
	wt.SetLabel("Modified")
	merge.SetHideWhenEmpty(false)
	s := only(t, h)
	if len(s.Groups) != 2 {
		t.Fatalf("groups = %+v", s.Groups)
	}
	g0, g1 := s.Groups[0], s.Groups[1]
	if g0.ID != "merge" || g0.Label != "Merge Changes" || g0.HideWhenEmpty || g0.ContextValue != "merge" || len(g0.Resources) != 0 {
		t.Errorf("merge group = %+v", g0)
	}
	if g1.ID != "workingTree" || g1.Label != "Modified" || len(g1.Resources) != 2 {
		t.Fatalf("working tree group = %+v", g1)
	}
	if r := g1.Resources[0]; r.URI != "file:///work/repo/src/loader.go" || r.ContextValue != "modified" || r.Command == nil || r.Command.ID != "git.openChange" || r.Command.Args != "file:///work/repo/src/loader.go" ||
		r.Decorations.Color != "gitDecoration.modifiedResourceForeground" || r.Decorations.Icon != "diff-modified" || r.MultiFileDiffEditorOriginalURI != "git:/src/loader.go" || r.MultiFileDiffEditorModifiedURI == "" {
		t.Errorf("first resource = %+v", r)
	}
	if r := g1.Resources[1]; !r.Decorations.StrikeThrough || !r.Decorations.Faded || r.Decorations.Tooltip != "Deleted" || r.Command != nil {
		t.Errorf("second resource = %+v", r)
	}
	// the resources are replaced in one call
	wt.SetResources([]extension.ResourceState{{URI: "file:///work/repo/new.go"}})
	if got := only(t, h).Groups[1].Resources; len(got) != 1 || got[0].URI != "file:///work/repo/new.go" {
		t.Errorf("resources after SetResources = %+v", got)
	}
	if only(t, h).BadgeCount != 1 {
		t.Errorf("BadgeCount = %d, want the number of resources", only(t, h).BadgeCount)
	}
	wt.Dispose()
	wt.Dispose() // twice does nothing
	wt.SetResources([]extension.ResourceState{{URI: "file:///work/repo/late.go"}})
	if got := only(t, h).Groups; len(got) != 1 || got[0].ID != "merge" {
		t.Errorf("groups after Dispose = %+v", got)
	}
	// the id of a group that is gone can be used again
	if _, err := sc.CreateGroup(extension.ResourceGroupSpec{ID: "workingTree", Label: "Changes"}); err != nil {
		t.Errorf("CreateGroup after Dispose: %v", err)
	}
}

func testGroupErrors(t *testing.T, h *Harness) {
	sc := create(t, h, extension.SourceControlSpec{ID: "git", Label: "Git"})
	if _, err := sc.CreateGroup(extension.ResourceGroupSpec{ID: "index", Label: "Staged Changes"}); err != nil {
		t.Fatal(err)
	}
	_, err := sc.CreateGroup(extension.ResourceGroupSpec{ID: "index", Label: "Again"})
	wantTag(t, err, "ExtensionSCMGroupDuplicate")
	_, err = sc.CreateGroup(extension.ResourceGroupSpec{Label: "No id"})
	wantTag(t, err, "ExtensionSCMGroupInvalid")
	// the same id in another source control is fine
	other := create(t, h, extension.SourceControlSpec{ID: "git", Label: "Git", RootURI: "file:///work/b"})
	if _, err := other.CreateGroup(extension.ResourceGroupSpec{ID: "index", Label: "Staged Changes"}); err != nil {
		t.Errorf("a group of the same id in another source control: %v", err)
	}
	sc.Dispose()
	_, err = sc.CreateGroup(extension.ResourceGroupSpec{ID: "late", Label: "Late"})
	wantTag(t, err, "ExtensionSCMDisposed")
}

func testDispose(t *testing.T, h *Harness) {
	spec := extension.SourceControlSpec{ID: "git", Label: "Git", RootURI: "file:///work/a"}
	sc := create(t, h, spec)
	keep := create(t, h, extension.SourceControlSpec{ID: "git", Label: "Git", RootURI: "file:///work/b"})
	g, _ := sc.CreateGroup(extension.ResourceGroupSpec{ID: "g", Label: "G"})
	sc.Dispose()
	sc.Dispose() // twice does nothing
	sc.SetCount(5)
	sc.Input().SetValue("late")
	g.SetResources([]extension.ResourceState{{URI: "file:///work/a/x.go"}})
	list := h.Observe()
	if len(list) != 1 || list[0].Handle != keep.Handle() {
		t.Fatalf("source controls after Dispose = %+v", list)
	}
	again := create(t, h, spec) // the id and the root are free again
	if again.Handle() == sc.Handle() {
		t.Error("the handle of a disposed source control was given again")
	}
	if got := len(h.Observe()); got != 2 {
		t.Errorf("%d source controls shown, want 2", got)
	}
}

func testDeactivation(t *testing.T, h *Harness) {
	sc := create(t, h, extension.SourceControlSpec{ID: "git", Label: "Git", RootURI: "file:///work/a"})
	g, _ := sc.CreateGroup(extension.ResourceGroupSpec{ID: "g", Label: "G"})
	create(t, h, extension.SourceControlSpec{ID: "svn", Label: "Subversion"})
	var seen []string
	sc.Input().OnChange(func(v string) { seen = append(seen, v) })
	h.Deactivate()
	if got := h.Observe(); len(got) != 0 {
		t.Fatalf("%d source controls left after the activation ended", len(got))
	}
	_, err := h.API.SourceControl().Create(extension.SourceControlSpec{ID: "git", Label: "Git"})
	wantTag(t, err, "ExtensionDeactivated")
	_, err = sc.CreateGroup(extension.ResourceGroupSpec{ID: "late", Label: "Late"})
	wantTag(t, err, "ExtensionDeactivated")
	// the calls that return nothing do nothing
	sc.SetCount(3)
	sc.Input().SetValue("late")
	g.SetResources([]extension.ResourceState{{URI: "file:///work/a/x.go"}})
	h.Pump()
	if len(seen) != 0 {
		t.Errorf("OnChange called after the activation ended: %q", seen)
	}
	if got := h.Observe(); len(got) != 0 {
		t.Errorf("%d source controls after calls on a dead activation", len(got))
	}
	if h.Failed() {
		t.Error("the end of the activation is not a failure")
	}
}

func testValidatePanics(t *testing.T, h *Harness) {
	sc := create(t, h, extension.SourceControlSpec{ID: "git", Label: "Git"})
	sc.Input().SetValidation(func(string) *extension.Validation { panic("boom") })
	if v := h.Validate(sc.Handle(), "x"); v != nil {
		t.Errorf("a validation that panicked gave %+v", v)
	}
	h.Pump()
	if !h.Failed() {
		t.Fatal("a panic in the validation function did not take the extension off")
	}
	if got := h.Observe(); len(got) != 0 {
		t.Errorf("%d source controls left after the failure", len(got))
	}
	_, err := h.API.SourceControl().Create(extension.SourceControlSpec{ID: "git", Label: "Git"})
	wantTag(t, err, "ExtensionDeactivated")
}

func testChangePanics(t *testing.T, h *Harness) {
	sc := create(t, h, extension.SourceControlSpec{ID: "git", Label: "Git"})
	sc.Input().OnChange(func(string) { panic("boom") })
	h.Type(sc.Handle(), "x")
	h.Pump()
	if !h.Failed() {
		t.Fatal("a panic in an OnChange function did not take the extension off")
	}
	if got := h.Observe(); len(got) != 0 {
		t.Errorf("%d source controls left after the failure", len(got))
	}
}
