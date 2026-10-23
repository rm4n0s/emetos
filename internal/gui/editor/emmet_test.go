package editor

import (
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
)

func emmetVM(t *testing.T, lang, text string, tweak func(*EmmetOptions), sels ...buffer.Selection) *ViewModel {
	t.Helper()
	doc := document.New(document.FileURI("/test."+lang), text, document.NewOptions())
	doc.SetLanguageID(lang)
	o := testOptions()
	o.Emmet = EmmetOptions{TriggerExpansionOnTab: true, IncludeLanguages: map[string]any{}, ExcludeLanguages: []any{"markdown"}, Variables: map[string]any{}, SyntaxProfiles: map[string]any{}}
	if tweak != nil {
		tweak(&o.Emmet)
	}
	vm := NewViewModel(doc, o, nil)
	if len(sels) > 0 {
		vm.Cursors().SetSelections(sels)
	}
	return vm
}

const expand = "editor.emmet.action.expandAbbreviation"

func TestExpandEmmetAbbreviation(t *testing.T) {
	cases := []struct {
		name  string
		lang  string
		text  string
		sels  []buffer.Selection
		tweak func(*EmmetOptions)
		want  string
		sel   buffer.Selection
	}{
		{"markup", "html", "ul>li", []buffer.Selection{cur(0, 5)}, nil, "<ul>\n    <li></li>\n</ul>", cur(1, 8)},
		{"after text", "html", "  div.a", []buffer.Selection{cur(0, 7)}, nil, "  <div class=\"a\"></div>", cur(0, 17)},
		{"stylesheet", "css", "  m10", []buffer.Selection{cur(0, 5)}, nil, "  margin: 10px;", cur(0, 15)},
		{"indented lines take the indentation of the line", "html", "    ul>li", []buffer.Selection{cur(0, 9)}, nil, "    <ul>\n        <li></li>\n    </ul>", cur(1, 12)},
		{"included language", "mylang", "p", []buffer.Selection{cur(0, 1)}, func(o *EmmetOptions) { o.IncludeLanguages = map[string]any{"mylang": "html"} }, "<p></p>", cur(0, 3)},
		{"a selection is the abbreviation", "html", "x p y", []buffer.Selection{sel(0, 2, 0, 3)}, nil, "x <p></p> y", cur(0, 5)},
		{"every cursor", "html", "p\np", []buffer.Selection{cur(0, 1), cur(1, 1)}, nil, "<p></p>\n<p></p>", cur(0, 3)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vm := emmetVM(t, tc.lang, tc.text, tc.tweak, tc.sels...)
			if err := vm.RunCommand(expand, nil); err != nil {
				t.Fatal(err)
			}
			if got := text(vm); got != tc.want {
				t.Errorf("text %q, want %q", got, tc.want)
			}
			if got := vm.Cursors().Selections(); got[0] != tc.sel {
				t.Errorf("cursor %v, want %v", got[0], tc.sel)
			}
		})
	}
}

func TestExpandEmmetWithTabStops(t *testing.T) {
	vm := emmetVM(t, "html", "a", nil, cur(0, 1))
	if err := vm.RunCommand(expand, nil); err != nil {
		t.Fatal(err)
	}
	if got := text(vm); got != `<a href=""></a>` {
		t.Fatalf("text %q", got)
	}
	if !vm.SnippetState().InSnippet {
		t.Error("the expansion has tab stops: a snippet session is running")
	}
	if got := vm.Cursors().Selections(); got[0] != cur(0, 9) {
		t.Errorf("the first tab stop is in the href: %v", got)
	}
}

func TestExpandEmmetFallsBackToTab(t *testing.T) {
	cases := []struct {
		name  string
		lang  string
		text  string
		tweak func(*EmmetOptions)
		want  string
	}{
		{"nothing before the cursor", "html", "x ", nil, "x   "},
		{"language without Emmet", "go", "ul>li", nil, "ul>li   "},
		{"excluded language", "markdown", "ul>li", nil, "ul>li   "},
		{"nothing without the setting", "go", "ul>li", func(o *EmmetOptions) { o.TriggerExpansionOnTab = false }, "ul>li"},
		{"a stylesheet colon", "css", "margin:", nil, "margin: "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vm := emmetVM(t, tc.lang, tc.text, tc.tweak, cur(0, len(tc.text)))
			if err := vm.RunCommand(expand, nil); err != nil {
				t.Fatal(err)
			}
			if got := text(vm); got != tc.want {
				t.Errorf("text %q, want %q", got, tc.want)
			}
		})
	}
}

func TestExpandEmmetFailure(t *testing.T) {
	vm := emmetVM(t, "html", "a", func(o *EmmetOptions) { o.Variables = []any{1} }, cur(0, 1))
	err := vm.RunCommand(expand, nil)
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute("VariablesInvalid") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
	if text(vm) != "a" {
		t.Errorf("the text changed: %q", text(vm))
	}
}

func TestExpandEmmetInReadOnlyDocument(t *testing.T) {
	doc := document.New(document.FileURI("/t.html"), "p", document.Options{ReadOnly: true})
	doc.SetLanguageID("html")
	o := testOptions()
	o.Emmet = EmmetOptions{IncludeLanguages: map[string]any{}, ExcludeLanguages: []any{}, Variables: map[string]any{}, SyntaxProfiles: map[string]any{}}
	vm := NewViewModel(doc, o, nil)
	vm.Cursors().SetSelections([]buffer.Selection{cur(0, 1)})
	err := vm.RunCommand(expand, nil)
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("DocumentReadOnly") {
		t.Errorf("expected DocumentReadOnly, got %v", err)
	}
}
