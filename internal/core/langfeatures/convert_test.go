package langfeatures_test

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	. "github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/core/lsp"
)

// decodeLSP reads JSON into a wire type.
func decodeLSP[T any](t *testing.T, in string) T {
	t.Helper()
	var v T
	if err := json.Unmarshal([]byte(in), &v); err != nil {
		t.Fatalf("decoding %s: %v", in, err)
	}
	return v
}

func rng(l1, c1, l2, c2 int) Range {
	return Range{Start: Position{Line: l1, Col: c1}, End: Position{Line: l2, Col: c2}}
}

// emoji is a character of 4 bytes in UTF-8 and 2 units in UTF-16: "x😀y" has y at byte 5 and UTF-16 offset 3.
const emojiLine = "x😀y = 1\n"

func TestHoverFromLSP(t *testing.T) {
	doc := docOf("go", "/work/a.go", emojiLine)
	h := decodeLSP[lsp.Hover](t, `{"contents":[{"kind":"markdown","value":"**y**"},"plain",{"language":"go","value":"var y"},{"kind":"plaintext","value":""}],"range":{"start":{"line":0,"character":3},"end":{"line":0,"character":4}}}`)
	got := HoverFromLSP(lsp.UTF16, doc, &h)
	if len(got.Contents) != 3 || !got.Contents[0].Markdown || got.Contents[2].Value != "```go\nvar y\n```" {
		t.Errorf("contents = %+v", got.Contents)
	}
	if got.Range == nil || *got.Range != rng(0, 5, 0, 6) {
		t.Errorf("range = %+v, want bytes 5..6", got.Range)
	}
	if HoverFromLSP(lsp.UTF16, doc, nil) != nil {
		t.Error("a nil hover became a hover")
	}
	// a server that counts bytes needs no conversion
	if got := HoverFromLSP(lsp.UTF8, doc, &lsp.Hover{Range: &lsp.Range{Start: lsp.Position{Character: 5}, End: lsp.Position{Character: 6}}}); *got.Range != rng(0, 5, 0, 6) {
		t.Errorf("utf-8 range = %+v", got.Range)
	}
}

func TestCompletionFromLSP(t *testing.T) {
	doc := docOf("go", "/work/a.go", emojiLine)
	tests := []struct {
		name  string
		in    string
		check func(t *testing.T, l CompletionList)
	}{
		{
			name: "a plain item",
			in:   `[{"label":"fmt","kind":9,"detail":"package","documentation":{"kind":"markdown","value":"docs"},"sortText":"a","filterText":"f","preselect":true,"deprecated":true,"commitCharacters":["."],"labelDetails":{"detail":" (pkg)","description":"std"}}]`,
			check: func(t *testing.T, l CompletionList) {
				it := l.Items[0]
				if it.Label != "fmt" || it.Kind != 9 || it.Detail != "package" || it.Documentation == nil || !it.Documentation.Markdown ||
					it.SortText != "a" || it.FilterText != "f" || !it.Preselect || !it.Deprecated || it.CommitCharacters[0] != "." ||
					it.LabelDetail != " (pkg)" || it.LabelDescription != "std" || it.Insert != nil || it.Snippet {
					t.Errorf("item = %+v", it)
				}
			},
		},
		{
			name: "a snippet with a text edit and additional edits and a command",
			in:   `{"items":[{"label":"Println","insertTextFormat":2,"textEdit":{"newText":"Println($0)","range":{"start":{"line":0,"character":3},"end":{"line":0,"character":4}}},"additionalTextEdits":[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}},"newText":"import \"fmt\"\n"}],"command":{"title":"t","command":"c","arguments":[1]},"tags":[1]}]}`,
			check: func(t *testing.T, l CompletionList) {
				it := l.Items[0]
				if !it.Snippet || it.InsertText != "Println($0)" || it.Insert == nil || *it.Insert != rng(0, 5, 0, 6) || it.Replace != nil {
					t.Errorf("item = %+v", it)
				}
				if len(it.AdditionalEdits) != 1 || it.AdditionalEdits[0].Text != "import \"fmt\"\n" || it.Command == nil || it.Command.ID != "c" || !it.Deprecated {
					t.Errorf("edits and command = %+v", it)
				}
			},
		},
		{
			name: "an insert and replace edit",
			in:   `[{"label":"y","textEdit":{"newText":"yy","insert":{"start":{"line":0,"character":3},"end":{"line":0,"character":3}},"replace":{"start":{"line":0,"character":3},"end":{"line":0,"character":4}}}}]`,
			check: func(t *testing.T, l CompletionList) {
				it := l.Items[0]
				if it.Insert == nil || it.Replace == nil || *it.Insert != rng(0, 5, 0, 5) || *it.Replace != rng(0, 5, 0, 6) || it.InsertText != "yy" {
					t.Errorf("item = %+v", it)
				}
			},
		},
		{
			name: "the defaults of the list",
			in:   `{"isIncomplete":true,"itemDefaults":{"commitCharacters":["("],"insertTextFormat":2,"editRange":{"start":{"line":0,"character":3},"end":{"line":0,"character":4}}},"items":[{"label":"a","textEditText":"a($1)"},{"label":"b","commitCharacters":[";"]}]}`,
			check: func(t *testing.T, l CompletionList) {
				if !l.Incomplete || len(l.Items) != 2 {
					t.Fatalf("list = %+v", l)
				}
				a, b := l.Items[0], l.Items[1]
				if !a.Snippet || a.InsertText != "a($1)" || a.Insert == nil || *a.Insert != rng(0, 5, 0, 6) || a.CommitCharacters[0] != "(" {
					t.Errorf("a = %+v", a)
				}
				if b.CommitCharacters[0] != ";" {
					t.Errorf("b = %+v", b)
				}
			},
		},
		{
			name: "default insert and replace ranges",
			in:   `{"itemDefaults":{"editRange":{"insert":{"start":{"line":0,"character":3},"end":{"line":0,"character":3}},"replace":{"start":{"line":0,"character":3},"end":{"line":0,"character":4}}}},"items":[{"label":"a"}]}`,
			check: func(t *testing.T, l CompletionList) {
				if it := l.Items[0]; it.Insert == nil || it.Replace == nil || it.Replace.End.Col != 6 {
					t.Errorf("item = %+v", it)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.check(t, CompletionFromLSP(lsp.UTF16, doc, decodeLSP[lsp.CompletionList](t, tt.in)))
		})
	}
}

func TestSignatureHelpFromLSP(t *testing.T) {
	in := `{"signatures":[{"label":"f(é int, y string)","documentation":"doc","parameters":[{"label":[2,7]},{"label":"y string","documentation":{"kind":"markdown","value":"why"}}],"activeParameter":1}],"activeSignature":0,"activeParameter":1}`
	h := decodeLSP[lsp.SignatureHelp](t, in)
	got := SignatureHelpFromLSP(&h)
	if got == nil || got.ActiveParameter != 1 || len(got.Signatures) != 1 {
		t.Fatalf("help = %+v", got)
	}
	sig := got.Signatures[0]
	if sig.Parameters[0].Label != "é int" || sig.Parameters[1].Label != "y string" || sig.Parameters[1].Documentation == nil || sig.ActiveParameter == nil || *sig.ActiveParameter != 1 {
		t.Errorf("signature = %+v", sig)
	}
	if SignatureHelpFromLSP(nil) != nil || SignatureHelpFromLSP(&lsp.SignatureHelp{}) != nil {
		t.Error("an empty signature help became one")
	}
}

func TestLocationsFromLSP(t *testing.T) {
	doc := docOf("go", "/work/a.go", emojiLine)
	// the other file has the emoji too, but only its text tells where y is
	other := buffer.New(emojiLine).Snapshot()
	reads := 0
	texts := func(_ context.Context, uri document.URI) (lsp.Lines, error) {
		reads++
		if uri.Path == "/work/b.go" {
			return other, nil
		}
		return nil, context.DeadlineExceeded
	}
	links := decodeLSP[lsp.LocationLinks](t, `[
		{"uri":"file:///work/a.go","range":{"start":{"line":0,"character":3},"end":{"line":0,"character":4}}},
		{"targetUri":"file:///work/b.go","targetRange":{"start":{"line":0,"character":3},"end":{"line":0,"character":4}},"targetSelectionRange":{"start":{"line":0,"character":3},"end":{"line":0,"character":4}},"originSelectionRange":{"start":{"line":0,"character":0},"end":{"line":0,"character":1}}},
		{"uri":"file:///work/c.go","range":{"start":{"line":0,"character":3},"end":{"line":0,"character":4}}},
		{"uri":"jar:file:///x.jar!/A.class","range":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}}}]`)
	got := LocationsFromLSP(context.Background(), lsp.UTF16, doc, texts, links)
	// the jar location is left out: the editor has no documents for it
	if len(got) != 3 {
		t.Fatalf("links = %+v", got)
	}
	if got[0].Target.URI.Path != "/work/a.go" || got[0].Target.Range != rng(0, 5, 0, 6) {
		t.Errorf("the document itself = %+v", got[0])
	}
	if got[1].Target.Range != rng(0, 5, 0, 6) || got[1].Origin == nil || *got[1].Origin != rng(0, 0, 0, 1) {
		t.Errorf("the other file = %+v", got[1])
	}
	// a file that cannot be read keeps the columns of the server
	if got[2].Target.Range != rng(0, 3, 0, 4) {
		t.Errorf("the unreadable file = %+v", got[2])
	}
	refs := ReferencesFromLSP(context.Background(), lsp.UTF16, doc, texts, []lsp.Location{
		{URI: "file:///work/b.go", Range: lsp.Range{Start: lsp.Position{Character: 3}, End: lsp.Position{Character: 4}}},
		{URI: "bad:thing"},
	})
	if len(refs) != 1 || refs[0].Range != rng(0, 5, 0, 6) {
		t.Errorf("references = %+v", refs)
	}
	// a nil source takes the columns as they are
	if nilSrc := LocationsFromLSP(context.Background(), lsp.UTF16, doc, nil, links[1:2]); nilSrc[0].Target.Range != rng(0, 3, 0, 4) {
		t.Errorf("without a source = %+v", nilSrc)
	}
}

func TestSymbolsHighlightsAndDiagnosticsFromLSP(t *testing.T) {
	doc := docOf("go", "/work/a.go", emojiLine)
	syms := decodeLSP[lsp.DocumentSymbols](t, `[{"name":"y","kind":13,"detail":"int","range":{"start":{"line":0,"character":0},"end":{"line":0,"character":8}},"selectionRange":{"start":{"line":0,"character":3},"end":{"line":0,"character":4}},"tags":[1],"children":[{"name":"c","kind":13,"range":{"start":{"line":0,"character":3},"end":{"line":0,"character":4}},"selectionRange":{"start":{"line":0,"character":3},"end":{"line":0,"character":4}}}]}]`)
	got := SymbolsFromLSP(lsp.UTF16, doc, syms)
	if len(got) != 1 || got[0].SelectionRange != rng(0, 5, 0, 6) || !got[0].Deprecated || len(got[0].Children) != 1 || got[0].Children[0].Range != rng(0, 5, 0, 6) {
		t.Errorf("symbols = %+v", got)
	}
	flat := decodeLSP[lsp.DocumentSymbols](t, `[{"name":"f","kind":12,"containerName":"pkg","location":{"uri":"file:///work/a.go","range":{"start":{"line":0,"character":0},"end":{"line":0,"character":1}}}}]`)
	if got := SymbolsFromLSP(lsp.UTF16, doc, flat); got[0].Detail != "pkg" {
		t.Errorf("flat symbols = %+v", got)
	}

	hl := HighlightsFromLSP(lsp.UTF16, doc, []lsp.DocumentHighlight{
		{Range: lsp.Range{Start: lsp.Position{Character: 3}, End: lsp.Position{Character: 4}}, Kind: lsp.DocumentHighlightWrite},
		{Kind: lsp.DocumentHighlightRead}, {},
	})
	if hl[0].Kind != HighlightWrite || hl[0].Range != rng(0, 5, 0, 6) || hl[1].Kind != HighlightRead || hl[2].Kind != HighlightText {
		t.Errorf("highlights = %+v", hl)
	}

	ds := decodeLSP[[]lsp.Diagnostic](t, `[
		{"range":{"start":{"line":0,"character":3},"end":{"line":0,"character":4}},"severity":2,"code":1234,"codeDescription":{"href":"https://x"},"source":"vet","message":"m","tags":[1,2]},
		{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":1}},"message":"no severity means error","code":"E1"},
		{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":1}},"severity":3,"message":"i"},
		{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":1}},"severity":4,"message":"h"}]`)
	got2 := DiagnosticsFromLSP(lsp.UTF16, doc, ds)
	if got2[0].Severity != SeverityWarning || got2[0].Code != "1234" || got2[0].CodeHref != "https://x" || got2[0].Source != "vet" ||
		!got2[0].Unnecessary || !got2[0].Deprecated || got2[0].Range != rng(0, 5, 0, 6) || len(got2[0].Raw) == 0 {
		t.Errorf("diagnostic 0 = %+v", got2[0])
	}
	if got2[1].Severity != SeverityError || got2[1].Code != "E1" || got2[2].Severity != SeverityInfo || got2[3].Severity != SeverityHint {
		t.Errorf("severities = %v %v %v", got2[1].Severity, got2[2].Severity, got2[3].Severity)
	}

	edits := TextEditsFromLSP(lsp.UTF16, doc, []lsp.TextEdit{{Range: lsp.Range{Start: lsp.Position{Character: 3}, End: lsp.Position{Character: 4}}, NewText: "z"}})
	if len(edits) != 1 || edits[0].Range != rng(0, 5, 0, 6) || edits[0].Text != "z" {
		t.Errorf("edits = %+v", edits)
	}
}

func TestCodeActionsFromLSP(t *testing.T) {
	doc := docOf("go", "/work/a.go", emojiLine)
	acts := decodeLSP[lsp.CodeActions](t, `[
		{"title":"Fix","kind":"quickfix","isPreferred":true,"diagnostics":[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":1}},"message":"d"}],"edit":{"changes":{"file:///work/a.go":[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":1}},"newText":"z"}]}}},
		{"title":"Off","disabled":{"reason":"not here"}},
		{"title":"Off too","disabled":{}},
		{"title":"Run","command":"gopls.tidy","arguments":[{"a":1}]}]`)
	got := CodeActionsFromLSP(lsp.UTF16, doc, "emetos.golang.gopls", acts)
	if len(got) != 4 || got[0].Edit == nil || !got[0].Preferred || len(got[0].Diagnostics) != 1 || got[0].Kind != "quickfix" {
		t.Fatalf("actions = %+v", got)
	}
	if got[1].Disabled != "not here" || got[2].Disabled != "disabled" {
		t.Errorf("disabled = %q %q", got[1].Disabled, got[2].Disabled)
	}
	if c := got[3].Command; c == nil || c.ID != "gopls.tidy" || c.Owner != "emetos.golang.gopls" || len(c.Arguments) != 1 {
		t.Errorf("command = %+v", got[3].Command)
	}
}

func TestSliceOfParameterLabelsCountsUTF16(t *testing.T) {
	// an emoji in the label takes two units, the offsets of the protocol count so
	h := decodeLSP[lsp.SignatureHelp](t, `{"signatures":[{"label":"f(😀 a, b)","parameters":[{"label":[2,6]},{"label":[8,9]},{"label":[40,50]},{"label":[3,3]}]}]}`)
	got := SignatureHelpFromLSP(&h).Signatures[0]
	var labels []string
	for _, p := range got.Parameters {
		labels = append(labels, p.Label)
	}
	if want := []string{"😀 a", "b", "", ""}; !slices.Equal(labels, want) {
		t.Errorf("labels = %q, want %q", labels, want)
	}
}
