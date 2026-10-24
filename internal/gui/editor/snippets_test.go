package editor

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/snippet"
	"github.com/rm4n0s/emetos/internal/gui/quickinput"
)

func snippetVM(t *testing.T, text string, sels ...buffer.Selection) *ViewModel {
	t.Helper()
	vm := NewViewModel(newTestDoc(text), testOptions(), nil)
	if len(sels) > 0 {
		vm.Cursors().SetSelections(sels)
	}
	return vm
}

func insertSnippetText(t *testing.T, vm *ViewModel, body string) {
	t.Helper()
	if err := vm.RunCommand("editor.action.insertSnippet", map[string]any{"snippet": body}); err != nil {
		t.Fatal(err)
	}
}

func text(vm *ViewModel) string { return vm.Document().Text() }

func TestInsertSnippetCommandAndTabStops(t *testing.T) {
	vm := snippetVM(t, "")
	insertSnippetText(t, vm, "foo(${1:a}, ${2:b})$0")
	if got := text(vm); got != "foo(a, b)" {
		t.Fatalf("text %q", got)
	}
	if got := vm.Cursors().Selections(); len(got) != 1 || got[0] != sel(0, 4, 0, 5) {
		t.Fatalf("selection %v", got)
	}
	if st := vm.SnippetState(); !st.InSnippet || !st.HasNext || st.HasPrev {
		t.Fatalf("state %+v", st)
	}
	// the decorations of the tab stops are in the document
	if n := len(vm.Document().DecorationsOf(vm.owner(snippetOwner))); n != 3 {
		t.Errorf("%d decorations", n)
	}
	if got := len(vm.snippetPlaceholders(0, 0)); got != 3 {
		t.Errorf("%d placeholders in view", got)
	}

	if err := vm.RunCommand("jumpToNextSnippetPlaceholder", nil); err != nil {
		t.Fatal(err)
	}
	if got := vm.Cursors().Selections(); got[0] != sel(0, 7, 0, 8) {
		t.Errorf("second tab stop: %v", got)
	}
	if st := vm.SnippetState(); !st.HasPrev {
		t.Errorf("state %+v", st)
	}
	if err := vm.RunCommand("jumpToPrevSnippetPlaceholder", nil); err != nil {
		t.Fatal(err)
	}
	if got := vm.Cursors().Selections(); got[0] != sel(0, 4, 0, 5) {
		t.Errorf("back to the first: %v", got)
	}
	// typing replaces the placeholder
	if err := vm.RunCommand("type", map[string]any{"text": "xy"}); err != nil {
		t.Fatal(err)
	}
	if got := text(vm); got != "foo(xy, b)" {
		t.Errorf("typed: %q", got)
	}
	if !vm.SnippetState().InSnippet {
		t.Error("typing in a tab stop ended the snippet")
	}
	for range 2 {
		if err := vm.RunCommand("jumpToNextSnippetPlaceholder", nil); err != nil {
			t.Fatal(err)
		}
	}
	if vm.SnippetState().InSnippet {
		t.Error("the final tab stop did not end the snippet")
	}
	if got := vm.Cursors().Selections(); got[0] != cur(0, 10) {
		t.Errorf("the cursor is at the final tab stop: %v", got)
	}
	if len(vm.Document().DecorationsOf(vm.owner(snippetOwner))) != 0 {
		t.Error("decorations remain")
	}
}

func TestSnippetModeEnds(t *testing.T) {
	cases := []struct {
		name string
		do   func(t *testing.T, vm *ViewModel)
		want bool
	}{
		{"stays when the cursor moves inside", func(t *testing.T, vm *ViewModel) {
			if err := vm.RunCommand("cursorRight", nil); err != nil {
				t.Fatal(err)
			}
		}, true},
		{"ends when the cursor leaves the tab stops", func(t *testing.T, vm *ViewModel) {
			if err := vm.RunCommand("cursorBottom", nil); err != nil {
				t.Fatal(err)
			}
			if err := vm.RunCommand("cursorLeft", nil); err != nil {
				t.Fatal(err)
			}
		}, false},
		{"leaveSnippet", func(t *testing.T, vm *ViewModel) {
			if err := vm.RunCommand("leaveSnippet", nil); err != nil {
				t.Fatal(err)
			}
		}, false},
		{"acceptSnippet goes to the end", func(t *testing.T, vm *ViewModel) {
			if err := vm.RunCommand("acceptSnippet", nil); err != nil {
				t.Fatal(err)
			}
		}, false},
		{"undo to before the snippet", func(t *testing.T, vm *ViewModel) {
			if err := vm.RunCommand("undo", nil); err != nil {
				t.Fatal(err)
			}
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vm := snippetVM(t, "")
			insertSnippetText(t, vm, "<${1:tag}>$2</${1}>$0")
			tc.do(t, vm)
			if got := vm.SnippetState().InSnippet; got != tc.want {
				t.Errorf("in snippet: %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLeaveSnippetCollapsesTheCursors(t *testing.T) {
	vm := snippetVM(t, "a\nb", cur(0, 1), cur(1, 1))
	insertSnippetText(t, vm, "[${1:x}]$0")
	if n := len(vm.Cursors().Selections()); n != 2 {
		t.Fatalf("%d cursors", n)
	}
	if err := vm.RunCommand("leaveSnippet", nil); err != nil {
		t.Fatal(err)
	}
	if n := len(vm.Cursors().Selections()); n != 1 {
		t.Errorf("%d cursors after Escape", n)
	}
}

func TestMirroredTabStopsHaveACursorEach(t *testing.T) {
	vm := snippetVM(t, "")
	insertSnippetText(t, vm, "<${1:tag}>$2</${1}>$0")
	if n := len(vm.Cursors().Selections()); n != 2 {
		t.Fatalf("%d cursors", n)
	}
	if err := vm.RunCommand("type", map[string]any{"text": "div"}); err != nil {
		t.Fatal(err)
	}
	if got := text(vm); got != "<div></div>" {
		t.Errorf("text %q", got)
	}
}

func TestChoicePicker(t *testing.T) {
	vm := snippetVM(t, "")
	insertSnippetText(t, vm, "go ${1|one,two,three|} $0")
	if !vm.SnippetState().InChoice {
		t.Fatalf("state %+v", vm.SnippetState())
	}
	p := vm.SnippetPicker()
	if p == nil {
		t.Fatal("no picker")
	}
	var labels []string
	for _, it := range p.Items {
		labels = append(labels, it.Label)
	}
	if !slices.Equal(labels, []string{"one", "two", "three"}) {
		t.Fatalf("options %v", labels)
	}
	p.Accept(1)
	if vm.SnippetPicker() != nil {
		t.Error("the picker stays open")
	}
	if got := text(vm); got != "go two " {
		t.Errorf("text %q", got)
	}
	if got := vm.Cursors().Selections(); got[0] != cur(0, 6) {
		t.Errorf("cursor %v", got)
	}
	if !vm.SnippetState().InSnippet {
		t.Error("choosing ended the snippet")
	}

	// dismissing keeps the option that is there
	vm2 := snippetVM(t, "")
	insertSnippetText(t, vm2, "${1|a,b|}")
	vm2.SnippetPicker().Dismiss()
	if vm2.SnippetPicker() != nil || text(vm2) != "a" {
		t.Errorf("dismiss: %v %q", vm2.SnippetPicker(), text(vm2))
	}
}

func writeSnippetFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func snippetService(t *testing.T, files map[string]string) *snippet.Service {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		writeSnippetFile(t, dir, name, body)
	}
	svc := snippet.NewService(dir)
	if err := svc.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	return svc
}

func goDoc(text string) *ViewModel {
	return NewViewModel(newTestDoc(text), func() Options { o := testOptions(); o.TabCompletion = "onlySnippets"; return o }(), nil)
}

func TestTabCompletionInsertsTheSnippetBeforeTheCursor(t *testing.T) {
	svc := snippetService(t, map[string]string{
		"plain.json": `{}`,
		"test.json":  `{"for": {"prefix": "forr", "body": ["for ${1:i} := range ${2:n} {", "\t$0", "}"]}}`,
	})
	vm := goDoc("  forr")
	vm.Cursors().SetSelections([]buffer.Selection{cur(0, 6)})
	vm.SetSnippets(svc)
	vm.Document().SetLanguageID("test")
	vm.updateTabSnippets()
	if !vm.HasSnippetCompletions() {
		t.Fatal("no completion for the prefix")
	}
	if err := vm.RunCommand("insertSnippet", nil); err != nil {
		t.Fatal(err)
	}
	if got := text(vm); got != "  for i := range n {\n    \n  }" {
		t.Errorf("text %q", got)
	}
	if got := vm.Cursors().Selections(); got[0] != sel(0, 6, 0, 7) {
		t.Errorf("cursor %v", got)
	}
	if !vm.SnippetState().InSnippet {
		t.Error("not in snippet mode")
	}
	if vm.HasSnippetCompletions() {
		t.Error("completions remain inside a snippet")
	}
}

func TestTabCompletionOffers(t *testing.T) {
	svc := snippetService(t, map[string]string{"test.json": `{"a": {"prefix": "ab", "body": "AB"}, "b": {"prefix": "b", "body": "B"}}`})
	cases := []struct {
		name string
		opt  string
		text string
		sel  buffer.Selection
		want int
	}{
		{"one match", "onlySnippets", "ab", cur(0, 2), 2},
		{"after a space nothing", "onlySnippets", "ab ", cur(0, 3), 0},
		{"selection equals the prefix", "onlySnippets", "x ab", sel(0, 2, 0, 4), 1},
		{"selection longer than the prefix", "onlySnippets", "xab", sel(0, 0, 0, 3), 0},
		{"option off", "off", "ab", cur(0, 2), 0},
		{"option on needs the suggestions", "on", "ab", cur(0, 2), 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := testOptions()
			o.TabCompletion = tc.opt
			vm := NewViewModel(newTestDoc(tc.text), o, nil)
			vm.Document().SetLanguageID("test")
			vm.SetSnippets(svc)
			vm.Cursors().SetSelections([]buffer.Selection{tc.sel})
			if got := len(vm.snip.tab); got != tc.want {
				t.Errorf("%d snippets, want %d", got, tc.want)
			}
		})
	}
}

func TestTabCompletionWithSeveralSnippetsOpensAList(t *testing.T) {
	svc := snippetService(t, map[string]string{"test.json": `{"a": {"prefix": "ab", "body": "AB", "description": "d"}, "b": {"prefix": "b", "body": "B"}}`})
	vm := goDoc("ab")
	vm.Document().SetLanguageID("test")
	vm.SetSnippets(svc)
	vm.Cursors().SetSelections([]buffer.Selection{cur(0, 2)})
	if err := vm.RunCommand("insertSnippet", nil); err != nil {
		t.Fatal(err)
	}
	p := vm.SnippetPicker()
	if p == nil || len(p.Items) != 2 || p.Items[0].Label != "ab" || p.Items[0].Description != "d" {
		t.Fatalf("picker %+v", p)
	}
	p.Accept(1)
	if got := text(vm); got != "aB" {
		t.Errorf("text %q", got)
	}
}

func TestInsertSnippetPickerAndName(t *testing.T) {
	svc := snippetService(t, map[string]string{"test.json": `{"Hello": {"prefix": "hi", "body": "hello ${1:you}"}, "NoPrefix": {"body": "np"}}`})
	t.Run("by name", func(t *testing.T) {
		vm := snippetVM(t, "")
		vm.Document().SetLanguageID("test")
		vm.SetSnippets(svc)
		if err := vm.RunCommand("editor.action.insertSnippet", map[string]any{"name": "NoPrefix"}); err != nil {
			t.Fatal(err)
		}
		if got := text(vm); got != "np" {
			t.Errorf("text %q", got)
		}
	})
	t.Run("by name of another language", func(t *testing.T) {
		vm := snippetVM(t, "")
		vm.SetSnippets(svc)
		if err := vm.RunCommand("editor.action.insertSnippet", map[string]any{"name": "Hello", "langId": "test"}); err != nil {
			t.Fatal(err)
		}
		if got := text(vm); got != "hello you" {
			t.Errorf("text %q", got)
		}
	})
	t.Run("unknown name", func(t *testing.T) {
		vm := snippetVM(t, "")
		vm.Document().SetLanguageID("test")
		vm.SetSnippets(svc)
		if err := vm.RunCommand("editor.action.insertSnippet", map[string]any{"name": "Nope"}); err != nil {
			t.Fatal(err)
		}
		if text(vm) != "" || vm.SnippetPicker() != nil {
			t.Errorf("something was inserted: %q", text(vm))
		}
	})
	t.Run("picked", func(t *testing.T) {
		vm := snippetVM(t, "")
		vm.Document().SetLanguageID("test")
		vm.SetSnippets(svc)
		var got quickinput.PickOptions
		vm.SetQuickPick(func(o quickinput.PickOptions) { got = o })
		if err := vm.RunCommand("editor.action.insertSnippet", nil); err != nil {
			t.Fatal(err)
		}
		if got.Placeholder != "Select a snippet" || !got.MatchOnDetail {
			t.Fatalf("options %+v", got)
		}
		picks := map[string]quickinput.Item{}
		separators := 0
		for _, it := range got.Items {
			if it.Separator {
				separators++
				continue
			}
			picks[it.Label] = it
		}
		// the prefix is the label, else the name; the description, else the body, is the detail
		if len(picks) != 2 || picks["hi"].Detail != "hello ${1:you}" || picks["NoPrefix"].Detail != "np" || separators == 0 {
			t.Fatalf("items %+v", got.Items)
		}
		if vm.SnippetPicker() != nil {
			t.Errorf("the list at the caret is shown too")
		}
		picks["hi"].Accept(quickinput.KeyMods{})
		if got := text(vm); got != "hello you" {
			t.Errorf("text %q", got)
		}
	})
	t.Run("without a quick input", func(t *testing.T) {
		vm := snippetVM(t, "")
		vm.Document().SetLanguageID("test")
		vm.SetSnippets(svc)
		if err := vm.RunCommand("editor.action.insertSnippet", nil); err != nil {
			t.Fatal(err)
		}
		if text(vm) != "" || vm.SnippetPicker() != nil {
			t.Errorf("something happened: %q", text(vm))
		}
	})
	t.Run("no service", func(t *testing.T) {
		vm := snippetVM(t, "")
		if err := vm.RunCommand("editor.action.insertSnippet", nil); err != nil {
			t.Fatal(err)
		}
	})
}

func TestInsertSnippetInReadOnlyDocumentFails(t *testing.T) {
	doc := newTestDoc("x")
	doc.SetReadOnly(true)
	vm := NewViewModel(doc, testOptions(), nil)
	err := vm.RunCommand("editor.action.insertSnippet", map[string]any{"snippet": "y"})
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute("DocumentReadOnly") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}

type fakeClipboard struct {
	text    string
	err     error
	pending []func()
}

func (c *fakeClipboard) Write(string, func(error)) {}
func (c *fakeClipboard) Read(done func(string, error)) {
	c.pending = append(c.pending, func() { done(c.text, c.err) })
}
func (c *fakeClipboard) WritePrimary(string, func(error)) {}
func (c *fakeClipboard) ReadPrimary(func(string, error))  {}
func (c *fakeClipboard) flush() {
	p := c.pending
	c.pending = nil
	for _, f := range p {
		f()
	}
}

func TestSnippetThatNeedsTheClipboard(t *testing.T) {
	svc := snippetService(t, map[string]string{"test.json": `{"paste": {"prefix": "pp", "body": "[$CLIPBOARD]"}}`})
	setup := func(t *testing.T, clip *fakeClipboard, report func(error)) *ViewModel {
		vm := snippetVM(t, "")
		vm.Document().SetLanguageID("test")
		vm.SetSnippets(svc)
		vm.SetClipboard(clip, nil, report)
		return vm
	}
	run := func(vm *ViewModel) error {
		return vm.RunCommand("editor.action.insertSnippet", map[string]any{"name": "paste"})
	}
	t.Run("read first", func(t *testing.T) {
		clip := &fakeClipboard{text: "text"}
		vm := setup(t, clip, nil)
		if err := run(vm); err != nil {
			t.Fatal(err)
		}
		if text(vm) != "" {
			t.Fatal("inserted before the clipboard was read")
		}
		clip.flush()
		if got := text(vm); got != "[text]" {
			t.Errorf("text %q", got)
		}
	})
	t.Run("the editor changed meanwhile", func(t *testing.T) {
		clip := &fakeClipboard{text: "text"}
		vm := setup(t, clip, nil)
		if err := run(vm); err != nil {
			t.Fatal(err)
		}
		if err := vm.RunCommand("type", map[string]any{"text": "k"}); err != nil {
			t.Fatal(err)
		}
		clip.flush()
		if got := text(vm); got != "k" {
			t.Errorf("text %q", got)
		}
	})
	t.Run("no clipboard", func(t *testing.T) {
		vm := snippetVM(t, "")
		vm.Document().SetLanguageID("test")
		vm.SetSnippets(svc)
		if err := run(vm); err == nil {
			t.Error("expected an error")
		}
	})
	t.Run("read fails", func(t *testing.T) {
		clip := &fakeClipboard{err: os.ErrPermission}
		var reported error
		vm := setup(t, clip, func(err error) { reported = err })
		if err := run(vm); err != nil {
			t.Fatal(err)
		}
		clip.flush()
		appErr, ok := errors.FromError(reported)
		if !ok {
			t.Fatalf("expected *errors.Error, got %T", reported)
		}
		if !appErr.HasRoute("viewModel.insertSnippetValue.SnippetClipboardReadFailed") && !appErr.HasRoute("SnippetClipboardReadFailed") {
			t.Errorf("unexpected failure path: %s", appErr.Route())
		}
	})
}

func TestSnippetVariablesComeFromTheEditor(t *testing.T) {
	vm := NewViewModel(document.New(document.FileURI("/work/proj/src/main.go"), "line", document.NewOptions()), testOptions(), nil)
	vm.SetSnippetWorkspace(snippet.WorkspaceResolver{Name: "proj", Folder: "/work/proj"})
	vm.Cursors().SetSelections([]buffer.Selection{cur(0, 4)})
	insertSnippetText(t, vm, "$TM_FILENAME|$RELATIVE_FILEPATH|$WORKSPACE_NAME|$TM_CURRENT_LINE|$LINE_COMMENT")
	if got := text(vm); got != "linemain.go|src/main.go|proj|line|" {
		t.Errorf("text %q", got)
	}
}

func TestSnippetKeysAndCommandsAreRegistered(t *testing.T) {
	reg := command.DefaultRegistry()
	for _, id := range []string{"jumpToNextSnippetPlaceholder", "jumpToPrevSnippetPlaceholder", "leaveSnippet", "acceptSnippet", "insertSnippet", "editor.action.insertSnippet"} {
		if _, ok := reg.Lookup(id); !ok {
			t.Errorf("%s is not registered", id)
		}
	}
}
