package editor

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/core/lsp"
)

// fakeFeatures is the provider of code actions, formatting and rename the tests of step 12.8 drive.
type fakeFeatures struct {
	mu sync.Mutex
	// actions are the code actions, resolved holds the edit a resolvable action gets.
	actions  []langfeatures.CodeAction
	resolved *langfeatures.WorkspaceEdit
	requests []langfeatures.CodeActionRequest
	gate     chan struct{}
	// format answers formatting, ranged the range formatting (which is asked with the ranges).
	format  []langfeatures.TextEdit
	ranged  []langfeatures.TextEdit
	asked   []langfeatures.Range
	onType  []langfeatures.TextEdit
	trigger string
	// prepare and prepareErr answer prepareRename; rename the rename.
	prepare    *langfeatures.PrepareRename
	prepareErr error
	rename     *langfeatures.WorkspaceEdit
	names      []string
}

// with changes the answers under the lock: the requests of the light bulb run on goroutines of their own.
func (f *fakeFeatures) with(fn func(*fakeFeatures)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

func (f *fakeFeatures) hold(ctx context.Context) error {
	f.mu.Lock()
	gate := f.gate
	f.mu.Unlock()
	if gate == nil {
		return nil
	}
	select {
	case <-gate:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (f *fakeFeatures) ProvideCodeActions(ctx context.Context, _ langfeatures.Document, req langfeatures.CodeActionRequest) ([]langfeatures.CodeAction, error) {
	f.mu.Lock()
	f.requests = append(f.requests, req)
	f.mu.Unlock()
	if err := f.hold(ctx); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]langfeatures.CodeAction(nil), f.actions...), nil
}

func (f *fakeFeatures) ProvideFormatting(ctx context.Context, _ langfeatures.Document, _ langfeatures.FormattingOptions) ([]langfeatures.TextEdit, error) {
	if err := f.hold(ctx); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.format, nil
}

func (f *fakeFeatures) ProvideRangeFormatting(ctx context.Context, _ langfeatures.Document, r langfeatures.Range, _ langfeatures.FormattingOptions) ([]langfeatures.TextEdit, error) {
	f.mu.Lock()
	f.asked = append(f.asked, r)
	f.mu.Unlock()
	if err := f.hold(ctx); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ranged, nil
}

func (f *fakeFeatures) OnTypeTriggers() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return []string{f.trigger}
}

func (f *fakeFeatures) ProvideOnTypeFormatting(ctx context.Context, _ langfeatures.Document, _ langfeatures.Position, _ string, _ langfeatures.FormattingOptions) ([]langfeatures.TextEdit, error) {
	if err := f.hold(ctx); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.onType, nil
}

func (f *fakeFeatures) CanResolveAction(a langfeatures.CodeAction) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return a.Edit == nil && f.resolved != nil
}

func (f *fakeFeatures) ResolveCodeAction(_ context.Context, a langfeatures.CodeAction) (langfeatures.CodeAction, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a.Edit = f.resolved
	return a, nil
}

func (f *fakeFeatures) PrepareRename(ctx context.Context, _ langfeatures.Document, _ langfeatures.Position) (*langfeatures.PrepareRename, error) {
	if err := f.hold(ctx); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.prepare, f.prepareErr
}

func (f *fakeFeatures) ProvideRename(ctx context.Context, _ langfeatures.Document, _ langfeatures.Position, name string) (*langfeatures.WorkspaceEdit, error) {
	f.mu.Lock()
	f.names = append(f.names, name)
	f.mu.Unlock()
	if err := f.hold(ctx); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rename, nil
}

// featureHost is the langfeatures.EditHost of the tests: the documents by their URI, and no file operations.
type featureHost struct {
	vm   *ViewModel
	docs map[document.URI]*document.Document
}

func (h *featureHost) Document(uri document.URI) (*document.Document, bool) {
	d, ok := h.docs[uri]
	return d, ok
}

func (h *featureHost) OpenForEdit(context.Context, string) (*document.Document, error) {
	return nil, errors.New("TestNoOpen", "the test host opens no files")
}

func (h *featureHost) CreateFile(context.Context, string, langfeatures.FileOptions) error {
	return errors.New("TestNoFiles", "the test host makes no files")
}

func (h *featureHost) RenameFile(context.Context, string, string, langfeatures.FileOptions) error {
	return errors.New("TestNoFiles", "the test host moves no files")
}

func (h *featureHost) DeleteFile(context.Context, string, langfeatures.FileOptions) error {
	return errors.New("TestNoFiles", "the test host deletes no files")
}

// BeginEdit keeps the cursors of the editor in their places while the edit is made, as the window does.
func (h *featureHost) BeginEdit() func() { return h.vm.TrackCursors() }

// featureRig is an editor with the fake provider, a second document the edits can reach and a record of what the host was asked.
type featureRig struct {
	t        *testing.T
	vm       *ViewModel
	reg      *langfeatures.Registry
	p        *fakeFeatures
	other    *document.Document
	posts    chan func()
	executed []langfeatures.Command
	reported []error
	logged   []string
}

const otherProgram = "package main\n\nfunc other() { Greet() }\n"

func newFeatureRig(t *testing.T, tweak func(*Options)) *featureRig {
	t.Helper()
	opts := goOptions()
	opts.CursorBlinking = "solid"
	opts.CodeActions = CodeActionOptions{Lightbulb: "onCode", ShowHeaders: true, IncludeNearbyQuickFixes: true}
	opts.RenamePreview = true
	if tweak != nil {
		tweak(&opts)
	}
	doc := newTestDoc(navProgram)
	doc.SetLanguageID("go")
	vm := NewViewModel(doc, opts, nil)
	r := &featureRig{t: t, vm: vm, reg: langfeatures.NewRegistry(), p: &fakeFeatures{}, posts: make(chan func(), 256)}
	r.other = document.New(otherFile, otherProgram, document.NewOptions())
	r.other.SetLanguageID("go")
	host := &featureHost{vm: vm, docs: map[document.URI]*document.Document{thisFile: doc, otherFile: r.other}}
	vm.SetPoster(func(fn func()) { r.posts <- fn })
	vm.SetSuggest(&SuggestHost{
		Ctx: t.Context(), Features: r.reg,
		Log:     func(msg string, _ error) { r.logged = append(r.logged, msg) },
		Report:  func(err error) { r.reported = append(r.reported, err) },
		Execute: func(_ context.Context, c langfeatures.Command) error { r.executed = append(r.executed, c); return nil },
		ApplyEdit: func(ctx context.Context, we *langfeatures.WorkspaceEdit) error {
			_, err := langfeatures.Apply(ctx, host, we)
			return err
		},
		KeyLabel: func(id string) string {
			if id == "editor.action.quickFix" {
				return "Ctrl+."
			}
			return ""
		},
		Texts: func(_ context.Context, uri document.URI) (lsp.Lines, error) {
			if d, ok := host.docs[uri]; ok {
				return d.Snapshot(), nil
			}
			return nil, errors.New("TestNoText", "no such document", "uri", uri.String())
		},
	})
	sel := langfeatures.Selector{Languages: []string{"go"}}
	r.reg.RegisterCodeActions("test", sel, r.p)
	r.reg.RegisterFormatting("test", sel, r.p)
	r.reg.RegisterRename("test", sel, r.p)
	r.reg.RegisterOnTypeFormatting("test", sel, r.p)
	t.Cleanup(vm.RenameCancel)
	t.Cleanup(vm.CodeActionHide)
	t.Cleanup(vm.PreviewDiscard)
	vm.SetFocused(true)
	cursorAt(vm, 5, 2)
	return r
}

// settle runs what is posted to the UI thread until nothing comes for a short while.
func (r *featureRig) settle() {
	for {
		select {
		case fn := <-r.posts:
			fn()
		case <-time.After(60 * time.Millisecond):
			return
		}
	}
}

func (r *featureRig) text() string { return r.vm.Document().Snapshot().String() }

// greetRename is the edit of a rename of Greet to Hello in both documents.
func greetRename(name string) *langfeatures.WorkspaceEdit {
	return &langfeatures.WorkspaceEdit{Changes: []langfeatures.Change{
		{URI: thisFile, Edits: []langfeatures.TextEdit{
			{Range: rg(2, 5, 2, 10), Text: name}, {Range: rg(5, 1, 5, 6), Text: name}, {Range: rg(6, 1, 6, 6), Text: name},
		}},
		{URI: otherFile, Edits: []langfeatures.TextEdit{{Range: rg(2, 15, 2, 20), Text: name}}},
	}}
}

func (r *featureRig) greetDiagnostic() {
	raw, _ := json.Marshal(map[string]any{"code": "unused", "data": 7})
	r.vm.SetDiagnostics([]Diagnostic{{Range: rg(5, 1, 5, 6), Severity: DiagnosticWarning, Message: "Greet is unused", Source: "test", Code: "unused", Data: raw}})
}

func TestLightBulbForADiagnosticOpensTheList(t *testing.T) {
	r := newFeatureRig(t, nil)
	r.p.with(func(f *fakeFeatures) {
		f.actions = []langfeatures.CodeAction{
			{Title: "Rename to Hello", Kind: langfeatures.KindQuickFix, Preferred: true, Edit: greetRename("Hello"), Command: &langfeatures.Command{ID: "test.after", Title: "After"}},
			{Title: "Extract function", Kind: langfeatures.KindRefactorExtract, Edit: greetRename("Extracted")},
		}
	})
	r.greetDiagnostic()
	r.settle()
	b := r.vm.LightBulb()
	if b == nil {
		t.Fatal("the light bulb does not show for the diagnostic")
	}
	if b.Icon != LightBulbAutoFix || !strings.Contains(b.Title, "Preferred Quick Fix") {
		t.Errorf("light bulb %+v, want the auto fix one", b)
	}
	// the request carried the diagnostic of the server as it sent it
	r.p.mu.Lock()
	got := r.p.requests
	r.p.mu.Unlock()
	found := false
	for _, req := range got {
		if len(req.Diagnostics) == 1 && req.Diagnostics[0].Code == "unused" && strings.Contains(string(req.Diagnostics[0].Raw), `"data":7`) {
			found = true
		}
	}
	if !found {
		t.Fatalf("the requests are %+v, want one with the diagnostic and its raw form", got)
	}
	r.vm.LightBulbClick()
	l := r.vm.CodeActionList()
	if l == nil || !r.vm.CodeActionMenuVisible() {
		t.Fatal("the list does not open")
	}
	var titles []string
	for _, it := range l.Items {
		titles = append(titles, it.Title)
	}
	want := []string{"Quick Fix", "Rename to Hello", "Extract", "Extract function"}
	if strings.Join(titles, "|") != strings.Join(want, "|") {
		t.Errorf("the list shows %v, want %v", titles, want)
	}
	if l.Items[l.Focus].Title != "Rename to Hello" {
		t.Errorf("the focus is on %q", l.Items[l.Focus].Title)
	}
}

func TestApplyingAnActionMakesTheEditsOfTwoFilesAndRunsTheCommand(t *testing.T) {
	r := newFeatureRig(t, nil)
	r.p.with(func(f *fakeFeatures) {
		f.actions = []langfeatures.CodeAction{
			{Title: "Rename to Hello", Kind: langfeatures.KindQuickFix, Edit: greetRename("Hello"), Command: &langfeatures.Command{ID: "test.after", Title: "After"}},
		}
	})
	r.vm.TriggerCodeActions(langfeatures.CodeActionTrigger{Type: langfeatures.TriggerInvokedAction, Source: langfeatures.SourceQuickFix, Filter: langfeatures.CodeActionFilter{Include: langfeatures.Kind(langfeatures.KindQuickFix)}}, "none")
	r.settle()
	if !r.vm.CodeActionMenuVisible() {
		t.Fatal("the list does not open")
	}
	r.vm.CodeActionMenuAccept(false)
	r.settle()
	if got := r.text(); strings.Count(got, "Hello") != 3 || strings.Contains(got, "Greet") {
		t.Errorf("text %q, want Greet renamed three times", got)
	}
	if got := r.other.Snapshot().String(); got != strings.ReplaceAll(otherProgram, "Greet", "Hello") {
		t.Errorf("the other file is %q", got)
	}
	if len(r.executed) != 1 || r.executed[0].ID != "test.after" {
		t.Errorf("commands run %v, want test.after after the edit", r.executed)
	}
	if r.vm.CodeActionMenuVisible() {
		t.Error("the list stays open after the action")
	}
	// one undo step in the file of the editor
	if err := r.vm.RunCommand("undo", nil); err != nil {
		t.Fatal(err)
	}
	if r.text() != navProgram {
		t.Errorf("one undo leaves %q", r.text())
	}
}

func TestAnActionWithoutAnEditIsResolvedFirst(t *testing.T) {
	r := newFeatureRig(t, nil)
	r.p.with(func(f *fakeFeatures) { f.resolved = greetRename("Resolved") })
	r.p.with(func(f *fakeFeatures) {
		f.actions = []langfeatures.CodeAction{{Title: "Rename lazily", Kind: langfeatures.KindQuickFix}}
	})
	r.vm.TriggerCodeActions(langfeatures.CodeActionTrigger{Type: langfeatures.TriggerInvokedAction, Source: langfeatures.SourceQuickFix, Filter: langfeatures.CodeActionFilter{Include: langfeatures.Kind(langfeatures.KindQuickFix)}, AutoApply: langfeatures.AutoApplyIfSingle}, "none")
	r.settle()
	if got := r.text(); strings.Count(got, "Resolved") != 3 {
		t.Errorf("text %q, want the resolved edit made (the only action applies without the list)", got)
	}
}

func TestNoActionsShowsTheMessage(t *testing.T) {
	r := newFeatureRig(t, nil)
	if err := r.vm.RunCommand("editor.action.quickFix", nil); err != nil {
		t.Fatal(err)
	}
	r.settle()
	h := r.vm.Hover()
	if h == nil || h.Parts[0].Contents[0].Value != "No code actions available" {
		t.Errorf("hover %+v, want the message", h)
	}
}

func TestStaleCodeActionAnswersAreDropped(t *testing.T) {
	r := newFeatureRig(t, nil)
	r.p.with(func(f *fakeFeatures) {
		f.actions = []langfeatures.CodeAction{{Title: "Fix", Kind: langfeatures.KindQuickFix, Edit: greetRename("Hello")}}
	})
	trig := langfeatures.CodeActionTrigger{Type: langfeatures.TriggerInvokedAction, Source: langfeatures.SourceQuickFix, Filter: langfeatures.CodeActionFilter{Include: langfeatures.Kind(langfeatures.KindQuickFix)}}

	// the text changes while the provider works
	r.p.with(func(f *fakeFeatures) { f.gate = make(chan struct{}) })
	r.vm.TriggerCodeActions(trig, "none")
	if err := r.vm.RunCommand("type", map[string]any{"text": "x"}); err != nil {
		t.Fatal(err)
	}
	close(r.p.gate)
	r.settle()
	if r.vm.CodeActionMenuVisible() {
		t.Error("the list shows an answer for the old text")
	}

	// a second request supersedes the first
	r.p.with(func(f *fakeFeatures) { f.gate = make(chan struct{}) })
	r.vm.TriggerCodeActions(trig, "none")
	r.vm.TriggerCodeActions(trig, "none")
	close(r.p.gate)
	r.settle()
	if !r.vm.CodeActionMenuVisible() {
		t.Fatal("the list does not show")
	}
	if n := len(r.vm.CodeActionList().Items); n != 2 {
		t.Errorf("the list has %d rows, want one header and one action (not two lists)", n)
	}

	// the cursor moves to another line while the light bulb asks
	r.vm.CodeActionHide()
	r.p.with(func(f *fakeFeatures) { f.gate = make(chan struct{}) })
	r.vm.codeActionsAutoTrigger()
	cursorAt(r.vm, 2, 1)
	close(r.p.gate)
	r.settle()
	if r.vm.LightBulb() != nil && r.vm.LightBulb().Pos.Line == 5 {
		t.Error("the light bulb shows on the line the cursor left")
	}
}

func TestTheLightBulbFollowsTheSettings(t *testing.T) {
	for _, tc := range []struct {
		name, setting string
		line, col     int
		want          bool
	}{
		{"onCode on a line with code", "onCode", 5, 2, true},
		{"onCode on an empty line", "onCode", 3, 0, false},
		{"on on an empty line", "on", 3, 0, true},
		{"off", "off", 5, 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newFeatureRig(t, func(o *Options) { o.CodeActions.Lightbulb = tc.setting })
			r.p.with(func(f *fakeFeatures) {
				f.actions = []langfeatures.CodeAction{{Title: "Fix", Kind: langfeatures.KindQuickFix, Edit: greetRename("Hello")}}
			})
			cursorAt(r.vm, tc.line, tc.col)
			r.settle()
			if got := r.vm.LightBulb() != nil; got != tc.want {
				t.Errorf("light bulb shows: %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFormatDocumentIsOneUndoStep(t *testing.T) {
	r := newFeatureRig(t, nil)
	r.p.with(func(f *fakeFeatures) {
		f.format = []langfeatures.TextEdit{
			{Range: rg(2, 0, 2, 4), Text: "func"},
			{Range: rg(4, 0, 4, 0), Text: "// main\n"},
			{Range: rg(5, 0, 5, 1), Text: "    "},
		}
	})
	cursorAt(r.vm, 5, 3)
	if !r.vm.FormatDocument() {
		t.Fatal("no request was made")
	}
	r.settle()
	want := "package main\n\nfunc Greet() {}\n\n// main\nfunc main() {\n    Greet()\n\tGreet()\n}\n"
	if got := r.text(); got != want {
		t.Fatalf("text %q, want %q", got, want)
	}
	if err := r.vm.RunCommand("undo", nil); err != nil {
		t.Fatal(err)
	}
	if got := r.text(); got != navProgram {
		t.Errorf("one undo leaves %q", got)
	}
}

func TestFormatSelectionAsksForTheSelectionAndForTheLineOfAnEmptyOne(t *testing.T) {
	r := newFeatureRig(t, nil)
	r.p.with(func(f *fakeFeatures) { f.ranged = []langfeatures.TextEdit{{Range: rg(5, 0, 5, 1), Text: "  "}} })
	selectText(r.vm, 5, 0, 6, 3)
	r.vm.FormatSelection()
	r.settle()
	cursorAt(r.vm, 2, 3)
	r.vm.FormatSelection()
	r.settle()
	r.p.mu.Lock()
	asked := r.p.asked
	r.p.mu.Unlock()
	if len(asked) != 2 || asked[0] != rg(5, 0, 6, 3) || asked[1] != rg(2, 0, 2, 15) {
		t.Errorf("ranges asked %v, want the selection and then the whole line", asked)
	}
	if !strings.Contains(r.text(), "  Greet()") {
		t.Errorf("the edit was not made: %q", r.text())
	}
}

func TestFormatAnswerForOldTextIsDropped(t *testing.T) {
	r := newFeatureRig(t, nil)
	r.p.with(func(f *fakeFeatures) { f.format = []langfeatures.TextEdit{{Range: rg(2, 0, 2, 4), Text: "FUNC"}} })
	r.p.with(func(f *fakeFeatures) { f.gate = make(chan struct{}) })
	r.vm.FormatDocument()
	if err := r.vm.RunCommand("type", map[string]any{"text": "x"}); err != nil {
		t.Fatal(err)
	}
	close(r.p.gate)
	r.settle()
	if strings.Contains(r.text(), "FUNC") {
		t.Errorf("an answer for the old text was made: %q", r.text())
	}
}

func TestFormatOnType(t *testing.T) {
	r := newFeatureRig(t, func(o *Options) { o.Format.OnType = true })
	r.p.with(func(f *fakeFeatures) { f.trigger = "}" })
	r.p.with(func(f *fakeFeatures) { f.onType = []langfeatures.TextEdit{{Range: rg(7, 0, 7, 0), Text: "// done\n"}} })
	cursorAt(r.vm, 7, 0)
	if err := r.vm.RunCommand("type", map[string]any{"text": "}"}); err != nil {
		t.Fatal(err)
	}
	r.settle()
	if !strings.Contains(r.text(), "// done\n}") {
		t.Errorf("text %q, want the on type edit", r.text())
	}

	// a change at or above the line of the cursor cancels the request
	r2 := newFeatureRig(t, func(o *Options) { o.Format.OnType = true })
	r2.p.with(func(f *fakeFeatures) { f.trigger = "}" })
	r2.p.with(func(f *fakeFeatures) { f.onType = []langfeatures.TextEdit{{Range: rg(7, 0, 7, 0), Text: "// done\n"}} })
	r2.p.with(func(f *fakeFeatures) { f.gate = make(chan struct{}) })
	cursorAt(r2.vm, 7, 0)
	if err := r2.vm.RunCommand("type", map[string]any{"text": "}"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r2.vm.Document().Edit([]document.Edit{{Range: rg(0, 0, 0, 0), Text: "// top\n"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	close(r2.p.gate)
	r2.settle()
	if strings.Contains(r2.text(), "// done") {
		t.Errorf("text %q, want the on type edit dropped", r2.text())
	}
}

func TestRenameRefusedByPrepare(t *testing.T) {
	r := newFeatureRig(t, nil)
	r.p.with(func(f *fakeFeatures) { f.prepareErr = errors.New("NotRenameable", "this element cannot be renamed") })
	if !r.vm.RenameStart() {
		t.Fatal("no request was made")
	}
	r.settle()
	if r.vm.RenameVisible() {
		t.Error("the box shows although the provider refused")
	}
	h := r.vm.Hover()
	if h == nil || h.Parts[0].Contents[0].Value != "The element can't be renamed." {
		t.Errorf("hover %+v, want the refusal", h)
	}
}

func TestRenameAppliedAcrossFiles(t *testing.T) {
	r := newFeatureRig(t, nil)
	r.p.with(func(f *fakeFeatures) {
		f.prepare = &langfeatures.PrepareRename{Range: rg(5, 1, 5, 6), Placeholder: "Greet"}
	})
	r.p.with(func(f *fakeFeatures) { f.rename = greetRename("Hello") })
	r.vm.RenameStart()
	r.settle()
	b := r.vm.RenameBox()
	if b == nil || b.Text != "Greet" || b.Range != rg(5, 1, 5, 6) {
		t.Fatalf("box %+v", b)
	}
	r.vm.RenameSetInput("Hello")
	r.vm.RenameAccept(false)
	r.settle()
	if got := r.text(); strings.Count(got, "Hello") != 3 {
		t.Errorf("text %q", got)
	}
	if got := r.other.Snapshot().String(); !strings.Contains(got, "Hello()") {
		t.Errorf("the other file is %q", got)
	}
	if r.vm.RenameVisible() {
		t.Error("the box stays")
	}
	// the cursor stays where it was
	if p := r.vm.Cursors().Primary().Position(); p != (buffer.Position{Line: 5, Col: 2}) {
		t.Errorf("cursor at %v", p)
	}
	r.p.mu.Lock()
	names := r.p.names
	r.p.mu.Unlock()
	if len(names) != 1 || names[0] != "Hello" {
		t.Errorf("names asked %v", names)
	}
}

func TestRenameWithTheWordWhenThereIsNoPrepare(t *testing.T) {
	r := newFeatureRig(t, nil)
	r.vm.RenameStart()
	r.settle()
	if b := r.vm.RenameBox(); b == nil || b.Text != "Greet" {
		t.Fatalf("box %+v, want the word at the cursor", b)
	}
	// the name left as it was renames nothing
	r.vm.RenameAccept(false)
	r.settle()
	r.p.mu.Lock()
	n := len(r.p.names)
	r.p.mu.Unlock()
	if n != 0 || r.vm.RenameVisible() {
		t.Errorf("the unchanged name asked for a rename (%d)", n)
	}
}

func TestRenamePreviewedAndPartlyAccepted(t *testing.T) {
	r := newFeatureRig(t, nil)
	r.p.with(func(f *fakeFeatures) { f.rename = greetRename("Hello") })
	r.vm.RenameStart()
	r.settle()
	r.vm.RenameSetInput("Hello")
	r.vm.RenameAccept(true)
	r.settle()
	p := r.vm.RefactorPreview()
	if p == nil {
		t.Fatal("the preview does not show")
	}
	if r.text() != navProgram {
		t.Error("the edit was made before the preview was accepted")
	}
	if len(p.Files) != 2 || len(p.Files[0].Changes) != 3 || len(p.Files[1].Changes) != 1 {
		t.Fatalf("files %+v", p.Files)
	}
	if c := p.Files[0].Changes[1]; c.Line != 6 || c.Old != "Greet" || c.New != "Hello" {
		t.Errorf("change %+v", c)
	}
	// leave the other file and the second use out
	r.vm.PreviewToggle(4) // the row of the file other.go (rows: file, 3 changes, file, change)
	r.vm.PreviewToggle(3) // the third change of the first file
	r.vm.PreviewApply()
	r.settle()
	got := r.text()
	if strings.Count(got, "Hello") != 2 || strings.Count(got, "Greet") != 1 {
		t.Errorf("text %q, want the definition and one use renamed", got)
	}
	if r.other.Snapshot().String() != otherProgram {
		t.Errorf("the unchecked file changed: %q", r.other.Snapshot().String())
	}
	if r.vm.RefactorPreview() != nil {
		t.Error("the preview stays")
	}
}

func TestRenamePreviewDiscarded(t *testing.T) {
	r := newFeatureRig(t, nil)
	r.p.with(func(f *fakeFeatures) { f.rename = greetRename("Hello") })
	r.vm.RenameStart()
	r.settle()
	r.vm.RenameSetInput("Hello")
	r.vm.RenameAccept(true)
	r.settle()
	r.vm.PreviewDiscard()
	r.settle()
	if r.vm.RefactorPreview() != nil || r.text() != navProgram {
		t.Error("the discard changed something")
	}
}

func TestRenameBoxClosesWhenTheCursorMoves(t *testing.T) {
	r := newFeatureRig(t, nil)
	r.vm.RenameStart()
	r.settle()
	if !r.vm.RenameVisible() {
		t.Fatal("no box")
	}
	cursorAt(r.vm, 2, 1)
	if r.vm.RenameVisible() {
		t.Error("the box stays after the cursor moved")
	}
}

func TestRenameAnswerForOldTextIsDropped(t *testing.T) {
	r := newFeatureRig(t, nil)
	r.p.with(func(f *fakeFeatures) { f.gate = make(chan struct{}) })
	r.vm.RenameStart()
	if err := r.vm.RunCommand("type", map[string]any{"text": "x"}); err != nil {
		t.Fatal(err)
	}
	close(r.p.gate)
	r.settle()
	if r.vm.RenameVisible() {
		t.Error("the box shows for a position of the old text")
	}
}
