package lsp

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/rm4n0s/errors"
)

func decode[T any](t *testing.T, in string) T {
	t.Helper()
	var v T
	if err := json.Unmarshal([]byte(in), &v); err != nil {
		t.Fatalf("Unmarshal(%s): %v", in, err)
	}
	return v
}

func TestHoverShapes(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []MarkupContent
	}{
		{"markup content", `{"contents":{"kind":"markdown","value":"**x**"}}`, []MarkupContent{{MarkupMarkdown, "**x**"}}},
		{"plain text markup", `{"contents":{"kind":"plaintext","value":"x"}}`, []MarkupContent{{MarkupPlainText, "x"}}},
		{"a marked string", `{"contents":"text"}`, []MarkupContent{{MarkupMarkdown, "text"}}},
		{"a marked string with a language", `{"contents":{"language":"go","value":"func f()"}}`, []MarkupContent{{MarkupMarkdown, "```go\nfunc f()\n```"}}},
		{"an array of marked strings", `{"contents":["a",{"language":"go","value":"b"}]}`, []MarkupContent{{MarkupMarkdown, "a"}, {MarkupMarkdown, "```go\nb\n```"}}},
		{"null contents", `{"contents":null}`, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := decode[Hover](t, tt.in)
			if !reflect.DeepEqual(h.Contents, tt.want) {
				t.Errorf("Contents = %+v, want %+v", h.Contents, tt.want)
			}
		})
	}
	h := decode[Hover](t, `{"contents":"x","range":{"start":{"line":1,"character":2},"end":{"line":1,"character":4}}}`)
	if h.Range == nil || h.Range.End.Character != 4 {
		t.Errorf("Range = %+v", h.Range)
	}
}

func TestLocationLinksShapes(t *testing.T) {
	loc := `{"uri":"file:///a.go","range":{"start":{"line":1,"character":0},"end":{"line":1,"character":3}}}`
	link := `{"targetUri":"file:///b.go","targetRange":{"start":{"line":2,"character":0},"end":{"line":9,"character":1}},"targetSelectionRange":{"start":{"line":2,"character":5},"end":{"line":2,"character":8}},"originSelectionRange":{"start":{"line":0,"character":0},"end":{"line":0,"character":1}}}`
	tests := []struct {
		name string
		in   string
		want int
		uri  string
	}{
		{"null", `null`, 0, ""},
		{"one location", loc, 1, "file:///a.go"},
		{"locations", `[` + loc + `,` + loc + `]`, 2, "file:///a.go"},
		{"location links", `[` + link + `]`, 1, "file:///b.go"},
		{"mixed", `[` + loc + `,` + link + `]`, 2, "file:///a.go"},
		{"empty", `[]`, 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decode[LocationLinks](t, tt.in)
			if len(got) != tt.want {
				t.Fatalf("len = %d, want %d", len(got), tt.want)
			}
			if tt.want > 0 && got[0].TargetURI != tt.uri {
				t.Errorf("first target = %s", got[0].TargetURI)
			}
		})
	}
	links := decode[LocationLinks](t, loc)
	if links[0].TargetSelectionRange != links[0].TargetRange || links[0].OriginSelectionRange != nil {
		t.Errorf("a location is a link whose ranges are its range: %+v", links[0])
	}
	if got := decode[LocationLinks](t, `[`+link+`]`); got[0].TargetSelectionRange.Start.Character != 5 || got[0].OriginSelectionRange == nil {
		t.Errorf("link = %+v", got[0])
	}
}

func TestCompletionShapes(t *testing.T) {
	l := decode[CompletionList](t, `[{"label":"a"},{"label":"b"}]`)
	if l.IsIncomplete || len(l.Items) != 2 {
		t.Errorf("array = %+v", l)
	}
	l = decode[CompletionList](t, `{"isIncomplete":true,"itemDefaults":{"commitCharacters":["."]},"items":[{"label":"x","kind":3,"documentation":"plain","labelDetails":{"detail":"()"},"textEdit":{"newText":"x","range":{"start":{"line":0,"character":1},"end":{"line":0,"character":2}}}}]}`)
	if !l.IsIncomplete || l.ItemDefaults == nil || l.ItemDefaults.CommitCharacters[0] != "." {
		t.Errorf("list = %+v", l)
	}
	item := l.Items[0]
	if item.Documentation == nil || item.Documentation.Kind != MarkupPlainText || item.Documentation.Value != "plain" {
		t.Errorf("documentation = %+v", item.Documentation)
	}
	if item.TextEdit == nil || item.TextEdit.Range == nil || item.TextEdit.Insert != nil {
		t.Errorf("textEdit = %+v", item.TextEdit)
	}
	if null := decode[CompletionList](t, `null`); len(null.Items) != 0 {
		t.Errorf("null = %+v", null)
	}
}

func TestCompletionItemRoundTrips(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"text edit", `{"label":"a","textEdit":{"range":{"start":{"line":0,"character":1},"end":{"line":0,"character":2}},"newText":"x"}}`},
		{"insert replace edit", `{"label":"a","textEdit":{"newText":"x","insert":{"start":{"line":0,"character":1},"end":{"line":0,"character":2}},"replace":{"start":{"line":0,"character":1},"end":{"line":0,"character":4}}}}`},
		{"data is kept", `{"label":"a","data":{"id":[1,2,3]},"additionalTextEdits":[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}},"newText":"import \"x\"\n"}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := decode[CompletionItem](t, tt.in)
			out, err := json.Marshal(item)
			if err != nil {
				t.Fatal(err)
			}
			var a, b any
			if err := json.Unmarshal([]byte(tt.in), &a); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(out, &b); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(a, b) {
				t.Errorf("round trip:\n in %s\nout %s", tt.in, out)
			}
		})
	}
	item := decode[CompletionItem](t, tests[1].in)
	if e := item.TextEdit; e.Range != nil || e.Insert == nil || e.Replace == nil || e.Replace.End.Character != 4 {
		t.Errorf("insert/replace = %+v", e)
	}
}

func TestSignatureHelpShapes(t *testing.T) {
	sh := decode[SignatureHelp](t, `{"signatures":[{"label":"f(a int, b string)","documentation":{"kind":"markdown","value":"doc"},"parameters":[{"label":[2,7]},{"label":"b string","documentation":"d"}]}],"activeSignature":0,"activeParameter":1}`)
	p := sh.Signatures[0].Parameters
	if p[0].Label.Offsets == nil || *p[0].Label.Offsets != [2]int{2, 7} || p[0].Label.Text != "" {
		t.Errorf("offset label = %+v", p[0].Label)
	}
	if p[1].Label.Text != "b string" || p[1].Label.Offsets != nil || p[1].Documentation.Value != "d" {
		t.Errorf("text label = %+v", p[1])
	}
	if sh.ActiveParameter == nil || *sh.ActiveParameter != 1 {
		t.Errorf("ActiveParameter = %v", sh.ActiveParameter)
	}
	out, err := json.Marshal(sh.Signatures[0].Parameters)
	if err != nil {
		t.Fatal(err)
	}
	if want := `[{"label":[2,7]},{"label":"b string","documentation":{"kind":"plaintext","value":"d"}}]`; string(out) != want {
		t.Errorf("marshal = %s", out)
	}
}

func TestDocumentSymbolsShapes(t *testing.T) {
	r := `{"start":{"line":0,"character":0},"end":{"line":3,"character":1}}`
	nested := decode[DocumentSymbols](t, `[{"name":"T","kind":23,"range":`+r+`,"selectionRange":`+r+`,"children":[{"name":"f","kind":8,"range":`+r+`,"selectionRange":`+r+`}]}]`)
	if len(nested) != 1 || len(nested[0].Children) != 1 || nested[0].Children[0].Name != "f" {
		t.Errorf("nested = %+v", nested)
	}
	flat := decode[DocumentSymbols](t, `[{"name":"F","kind":12,"deprecated":true,"containerName":"pkg","location":{"uri":"file:///a.go","range":`+r+`}}]`)
	if len(flat) != 1 || flat[0].Name != "F" || flat[0].ContainerName != "pkg" || !flat[0].Deprecated || flat[0].Range.End.Line != 3 || flat[0].SelectionRange != flat[0].Range {
		t.Errorf("flat = %+v", flat)
	}
	if got := decode[DocumentSymbols](t, `null`); len(got) != 0 {
		t.Errorf("null = %+v", got)
	}
	ws := decode[[]WorkspaceSymbol](t, `[{"name":"a","kind":12,"location":{"uri":"file:///a.go"}},{"name":"b","kind":12,"location":{"uri":"file:///b.go","range":`+r+`}}]`)
	if ws[0].Location.Range != nil || ws[1].Location.Range == nil {
		t.Errorf("workspace symbols = %+v", ws)
	}
}

func TestCodeActionsShapes(t *testing.T) {
	got := decode[CodeActions](t, `[
		{"title":"Run","command":"cmd.run","arguments":[1,"a"]},
		{"title":"Fix","kind":"quickfix","isPreferred":true,"edit":{"changes":{"file:///a.go":[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":1}},"newText":"x"}]}},"command":{"title":"After","command":"after"}},
		{"title":"Off","kind":"refactor","disabled":{"reason":"not here"}}]`)
	if len(got) != 3 {
		t.Fatalf("len = %d", len(got))
	}
	if got[0].Command == nil || got[0].Command.Command != "cmd.run" || got[0].Kind != "" || len(got[0].Command.Arguments) != 2 {
		t.Errorf("bare command = %+v", got[0])
	}
	if !got[1].IsPreferred || got[1].Edit == nil || len(got[1].Edit.Changes["file:///a.go"]) != 1 || got[1].Command.Command != "after" {
		t.Errorf("action = %+v", got[1])
	}
	if got[2].Disabled == nil || got[2].Disabled.Reason != "not here" {
		t.Errorf("disabled = %+v", got[2])
	}
	if null := decode[CodeActions](t, `null`); len(null) != 0 {
		t.Errorf("null = %+v", null)
	}
}

func TestWorkspaceEditDocumentChanges(t *testing.T) {
	in := `{"documentChanges":[
		{"textDocument":{"uri":"file:///a.go","version":3},"edits":[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":1}},"newText":"x","annotationId":"a1"}]},
		{"kind":"create","uri":"file:///new.go","options":{"overwrite":true}},
		{"kind":"rename","oldUri":"file:///a.go","newUri":"file:///b.go","options":{"ignoreIfExists":true}},
		{"kind":"delete","uri":"file:///old.go","options":{"recursive":true,"ignoreIfNotExists":true}}],
		"changeAnnotations":{"a1":{"label":"Rename","needsConfirmation":true}}}`
	we := decode[WorkspaceEdit](t, in)
	c := we.DocumentChanges
	if len(c) != 4 {
		t.Fatalf("changes = %+v", c)
	}
	if c[0].Kind != "" || c[0].TextDocument.URI != "file:///a.go" || *c[0].TextDocument.Version != 3 || c[0].Edits[0].AnnotationID != "a1" {
		t.Errorf("text document edit = %+v", c[0])
	}
	if c[1].Kind != ChangeCreateFile || c[1].URI != "file:///new.go" || !c[1].Overwrite {
		t.Errorf("create = %+v", c[1])
	}
	if c[2].Kind != ChangeRenameFile || c[2].OldURI != "file:///a.go" || c[2].NewURI != "file:///b.go" || !c[2].IgnoreIfExists {
		t.Errorf("rename = %+v", c[2])
	}
	if c[3].Kind != ChangeDeleteFile || !c[3].Recursive || !c[3].IgnoreIfNotExists {
		t.Errorf("delete = %+v", c[3])
	}
	if !we.ChangeAnnotations["a1"].NeedsConfirmation {
		t.Errorf("annotations = %+v", we.ChangeAnnotations)
	}
	out, err := json.Marshal(we)
	if err != nil {
		t.Fatal(err)
	}
	if again := decode[WorkspaceEdit](t, string(out)); !reflect.DeepEqual(again, we) {
		t.Errorf("round trip lost something:\n%s", out)
	}
}

func TestPrepareRenameShapes(t *testing.T) {
	rng := `{"start":{"line":1,"character":2},"end":{"line":1,"character":5}}`
	tests := []struct {
		name        string
		in          string
		hasRange    bool
		placeholder string
		defaultB    bool
	}{
		{"null", `null`, false, "", false},
		{"a range", rng, true, "", false},
		{"a range with a placeholder", `{"range":` + rng + `,"placeholder":"name"}`, true, "name", false},
		{"default behavior", `{"defaultBehavior":true}`, false, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := decode[PrepareRenameResult](t, tt.in)
			if (r.Range != nil) != tt.hasRange || r.Placeholder != tt.placeholder || r.DefaultBehavior != tt.defaultB {
				t.Errorf("result = %+v", r)
			}
			if tt.hasRange && r.Range.Start.Character != 2 {
				t.Errorf("range = %+v", r.Range)
			}
		})
	}
}

func TestInlayHints(t *testing.T) {
	hints := decode[[]InlayHint](t, `[
		{"position":{"line":1,"character":4},"label":"name:","kind":2,"paddingRight":true},
		{"position":{"line":2,"character":0},"label":[{"value":"int","tooltip":"the type","location":{"uri":"file:///a.go","range":{"start":{"line":0,"character":0},"end":{"line":0,"character":3}}}},{"value":"!"}]}]`)
	if hints[0].Label.String() != "name:" || hints[0].Kind != InlayHintKindParameter || !hints[0].PaddingRight {
		t.Errorf("hint 0 = %+v", hints[0])
	}
	if hints[1].Label.String() != "int!" || len(hints[1].Label.Parts) != 2 || hints[1].Label.Parts[0].Location == nil || hints[1].Label.Parts[0].Tooltip.Value != "the type" {
		t.Errorf("hint 1 = %+v", hints[1])
	}
	out, err := json.Marshal(hints)
	if err != nil {
		t.Fatal(err)
	}
	if again := decode[[]InlayHint](t, string(out)); !reflect.DeepEqual(again, hints) {
		t.Errorf("round trip lost something: %s", out)
	}
}

func TestSemanticTokensDeltaResult(t *testing.T) {
	full := decode[SemanticTokensDeltaResult](t, `{"resultId":"1","data":[0,0,3,1,0]}`)
	if full.Full == nil || full.Delta != nil || full.Full.ResultID != "1" || len(full.Full.Data) != 5 {
		t.Errorf("full = %+v", full)
	}
	delta := decode[SemanticTokensDeltaResult](t, `{"resultId":"2","edits":[{"start":5,"deleteCount":5,"data":[1,0,2,3,0]},{"start":0,"deleteCount":0}]}`)
	if delta.Delta == nil || delta.Full != nil || len(delta.Delta.Edits) != 2 || delta.Delta.Edits[0].Start != 5 || delta.Delta.Edits[1].Data != nil {
		t.Errorf("delta = %+v", delta)
	}
	if null := decode[SemanticTokensDeltaResult](t, `null`); null.Full != nil || null.Delta != nil {
		t.Errorf("null = %+v", null)
	}
}

func TestMarkupContentFromString(t *testing.T) {
	m := decode[MarkupContent](t, `"text"`)
	if m.Kind != MarkupPlainText || m.Value != "text" {
		t.Errorf("m = %+v", m)
	}
	m = decode[MarkupContent](t, `{"kind":"markdown","value":"**x**"}`)
	if m.Kind != MarkupMarkdown || m.Value != "**x**" {
		t.Errorf("m = %+v", m)
	}
}

func TestDiagnosticCodeText(t *testing.T) {
	tests := []struct{ in, want string }{
		{`{"message":"m","range":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}},"code":"E0308"}`, "E0308"},
		{`{"message":"m","range":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}},"code":2304}`, "2304"},
		{`{"message":"m","range":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}}}`, ""},
	}
	for _, tt := range tests {
		if got := decode[Diagnostic](t, tt.in).CodeText(); got != tt.want {
			t.Errorf("CodeText = %q, want %q", got, tt.want)
		}
	}
	// The code goes back the way it came.
	d := decode[Diagnostic](t, tests[1].in)
	out, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	if m["code"] != float64(2304) {
		t.Errorf("code = %v", m["code"])
	}
}

func TestProgress(t *testing.T) {
	for _, in := range []string{`{"token":"abc","value":{"kind":"begin","title":"Loading","percentage":0,"cancellable":true}}`, `{"token":7,"value":{"kind":"begin","title":"Loading"}}`} {
		p := decode[ProgressParams](t, in)
		w, ok := p.WorkDone()
		if !ok || w.Kind != ProgressBegin || w.Title != "Loading" {
			t.Errorf("WorkDone = %+v, %v", w, ok)
		}
		out, err := json.Marshal(p.Token)
		if err != nil {
			t.Fatal(err)
		}
		if string(out) != p.Token.Key() {
			t.Errorf("token marshals as %s, key %s", out, p.Token.Key())
		}
	}
	if p := decode[ProgressParams](t, `{"token":1,"value":{"items":[]}}`); func() bool { _, ok := p.WorkDone(); return ok }() {
		t.Errorf("a partial result is not a work done progress")
	}
	if tok := NewProgressToken("x"); tok.Key() != `"x"` {
		t.Errorf("token = %s", tok.Key())
	}
	if out, err := json.Marshal(ProgressToken{}); err != nil || string(out) != "null" {
		t.Errorf("zero token = %s, %v", out, err)
	}
}

func TestProviderAndSyncOptions(t *testing.T) {
	caps := decode[ServerCapabilities](t, `{
		"positionEncoding":"utf-8",
		"textDocumentSync":{"openClose":true,"change":2,"willSave":true,"save":{"includeText":true}},
		"hoverProvider":true,
		"definitionProvider":{"workDoneProgress":true},
		"renameProvider":{"prepareProvider":true},
		"codeActionProvider":{"codeActionKinds":["quickfix","source.organizeImports"],"resolveProvider":true},
		"referencesProvider":false,
		"completionProvider":{"triggerCharacters":["."],"resolveProvider":true},
		"semanticTokensProvider":{"legend":{"tokenTypes":["type"],"tokenModifiers":[]},"range":true,"full":{"delta":true}},
		"documentOnTypeFormattingProvider":{"firstTriggerCharacter":"}","moreTriggerCharacter":[";"]}}`)
	if enc, ok := caps.Encoding(); !ok || enc != UTF8 {
		t.Errorf("Encoding = %v, %v", enc, ok)
	}
	sync := caps.TextDocumentSync
	if !sync.OpenClose || sync.Change != SyncIncremental || !sync.WillSave || sync.WillSaveWaitUntil || !sync.Save || !sync.SaveIncludeText {
		t.Errorf("sync = %+v", sync)
	}
	if !caps.HoverProvider.Enabled || !caps.DefinitionProvider.Enabled || caps.ReferencesProvider.Enabled || caps.ImplementationProvider.Enabled {
		t.Errorf("providers: hover %v definition %v references %v implementation %v", caps.HoverProvider, caps.DefinitionProvider, caps.ReferencesProvider, caps.ImplementationProvider)
	}
	if !caps.RenameProvider.Bool("prepareProvider") || caps.RenameProvider.Bool("resolveProvider") || caps.HoverProvider.Bool("x") {
		t.Errorf("Bool: rename %+v", caps.RenameProvider)
	}
	if got := caps.CodeActionProvider.Strings("codeActionKinds"); !reflect.DeepEqual(got, []string{"quickfix", "source.organizeImports"}) || caps.CodeActionProvider.Strings("none") != nil {
		t.Errorf("Strings = %v", got)
	}
	if caps.CompletionProvider.TriggerCharacters[0] != "." || !caps.CompletionProvider.ResolveProvider {
		t.Errorf("completion = %+v", caps.CompletionProvider)
	}
	st := caps.SemanticTokensProvider
	if !st.Range.Enabled || !st.Full.Bool("delta") || st.Legend.TokenTypes[0] != "type" {
		t.Errorf("semantic tokens = %+v", st)
	}
	if o := caps.DocumentOnTypeFormattingProvider; o.FirstTriggerCharacter != "}" || o.MoreTriggerCharacter[0] != ";" {
		t.Errorf("on type = %+v", o)
	}

	// The number form of textDocumentSync, and no encoding.
	caps = decode[ServerCapabilities](t, `{"textDocumentSync":1}`)
	if s := caps.TextDocumentSync; !s.OpenClose || s.Change != SyncFull || s.Save {
		t.Errorf("number sync = %+v", s)
	}
	if enc, ok := caps.Encoding(); !ok || enc != UTF16 {
		t.Errorf("default Encoding = %v, %v", enc, ok)
	}
	if _, ok := decode[ServerCapabilities](t, `{"positionEncoding":"utf-7"}`).Encoding(); ok {
		t.Errorf("utf-7 is not an encoding")
	}
	if b, err := json.Marshal(Provider{Enabled: true}); err != nil || string(b) != "true" {
		t.Errorf("Provider marshals as %s, %v", b, err)
	}
	if b, err := json.Marshal(caps.HoverProvider); err != nil || string(b) != "false" {
		t.Errorf("zero Provider marshals as %s, %v", b, err)
	}
}

func TestDefaultClientCapabilities(t *testing.T) {
	raw, err := json.Marshal(DefaultClientCapabilities())
	if err != nil {
		t.Fatal(err)
	}
	var caps struct {
		General struct {
			PositionEncodings []string `json:"positionEncodings"`
		} `json:"general"`
		Window    map[string]any `json:"window"`
		Workspace struct {
			ApplyEdit     bool           `json:"applyEdit"`
			Configuration bool           `json:"configuration"`
			WorkspaceEdit map[string]any `json:"workspaceEdit"`
		} `json:"workspace"`
		TextDocument map[string]json.RawMessage `json:"textDocument"`
	}
	if err := json.Unmarshal(raw, &caps); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(caps.General.PositionEncodings, []string{"utf-8", "utf-16"}) {
		t.Errorf("positionEncodings = %v", caps.General.PositionEncodings)
	}
	if caps.Window["workDoneProgress"] != true || !caps.Workspace.ApplyEdit || !caps.Workspace.Configuration || caps.Workspace.WorkspaceEdit["documentChanges"] != true {
		t.Errorf("window/workspace = %+v %+v", caps.Window, caps.Workspace)
	}
	// Every feature area 3 builds is advertised, and what it does not build is not.
	for _, feature := range []string{
		"synchronization", "completion", "hover", "signatureHelp", "declaration", "definition", "typeDefinition",
		"implementation", "references", "documentHighlight", "documentSymbol", "codeAction", "codeLens", "formatting",
		"rangeFormatting", "onTypeFormatting", "rename", "publishDiagnostics", "diagnostic", "foldingRange",
		"selectionRange", "semanticTokens", "inlayHint",
	} {
		if _, ok := caps.TextDocument[feature]; !ok {
			t.Errorf("textDocument.%s is not advertised", feature)
		}
	}
	for _, feature := range []string{"linkedEditingRange", "documentColor", "documentLink", "callHierarchy", "typeHierarchy", "colorPresentation", "inlineValue", "moniker"} {
		if _, ok := caps.TextDocument[feature]; ok {
			t.Errorf("textDocument.%s is advertised although area 3 builds nothing for it", feature)
		}
	}
}

func TestTypeErrors(t *testing.T) {
	tests := []struct {
		name  string
		route string
		do    func() error
	}{
		{"location of a wrong type", "LocationLinks.UnmarshalJSON.LocationInvalid", func() error { return json.Unmarshal([]byte(`"x"`), new(LocationLinks)) }},
		{"location array element of a wrong type", "LocationLinks.UnmarshalJSON.LocationInvalid", func() error { return json.Unmarshal([]byte(`[1]`), new(LocationLinks)) }},
		{"location link of a wrong type", "LocationLinks.UnmarshalJSON.LocationInvalid", func() error { return json.Unmarshal([]byte(`[{"targetUri":1}]`), new(LocationLinks)) }},
		{"markup of a wrong type", "MarkupContent.UnmarshalJSON.MarkupInvalid", func() error { return json.Unmarshal([]byte(`5`), new(MarkupContent)) }},
		{"hover of a wrong type", "Hover.UnmarshalJSON.HoverInvalid", func() error { return json.Unmarshal([]byte(`{"contents":5}`), new(Hover)) }},
		{"hover with a bad range", "Hover.UnmarshalJSON.HoverInvalid", func() error { return json.Unmarshal([]byte(`{"contents":"x","range":5}`), new(Hover)) }},
		{"hover contents array of a wrong type", "Hover.UnmarshalJSON.HoverInvalid", func() error { return json.Unmarshal([]byte(`{"contents":[5]}`), new(Hover)) }},
		{"completion list of a wrong type", "CompletionList.UnmarshalJSON.CompletionInvalid", func() error { return json.Unmarshal([]byte(`"x"`), new(CompletionList)) }},
		{"completion items of a wrong type", "CompletionList.UnmarshalJSON.CompletionInvalid", func() error { return json.Unmarshal([]byte(`[5]`), new(CompletionList)) }},
		{"completion text edit of a wrong type", "CompletionTextEdit.UnmarshalJSON.CompletionInvalid", func() error { return json.Unmarshal([]byte(`5`), new(CompletionTextEdit)) }},
		{"parameter label of a wrong type", "ParameterLabel.UnmarshalJSON.SignatureInvalid", func() error { return json.Unmarshal([]byte(`5`), new(ParameterLabel)) }},
		{"parameter label offsets of a wrong type", "ParameterLabel.UnmarshalJSON.SignatureInvalid", func() error { return json.Unmarshal([]byte(`["a",1]`), new(ParameterLabel)) }},
		{"symbols of a wrong type", "DocumentSymbols.UnmarshalJSON.SymbolInvalid", func() error { return json.Unmarshal([]byte(`5`), new(DocumentSymbols)) }},
		{"symbol of a wrong type", "DocumentSymbols.UnmarshalJSON.SymbolInvalid", func() error { return json.Unmarshal([]byte(`[5]`), new(DocumentSymbols)) }},
		{"symbol information of a wrong type", "DocumentSymbols.UnmarshalJSON.SymbolInvalid", func() error {
			return json.Unmarshal([]byte(`[{"name":1,"location":{"uri":"x","range":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}}}}]`), new(DocumentSymbols))
		}},
		{"document symbol of a wrong type", "DocumentSymbols.UnmarshalJSON.SymbolInvalid", func() error { return json.Unmarshal([]byte(`[{"name":1}]`), new(DocumentSymbols)) }},
		{"code actions of a wrong type", "CodeActions.UnmarshalJSON.CodeActionInvalid", func() error { return json.Unmarshal([]byte(`5`), new(CodeActions)) }},
		{"code action of a wrong type", "CodeActions.UnmarshalJSON.CodeActionInvalid", func() error { return json.Unmarshal([]byte(`[5]`), new(CodeActions)) }},
		{"command of a wrong type", "CodeActions.UnmarshalJSON.CodeActionInvalid", func() error { return json.Unmarshal([]byte(`[{"command":"c","title":5}]`), new(CodeActions)) }},
		{"action of a wrong type", "CodeActions.UnmarshalJSON.CodeActionInvalid", func() error { return json.Unmarshal([]byte(`[{"title":5}]`), new(CodeActions)) }},
		{"document change of a wrong type", "DocumentChange.UnmarshalJSON.WorkspaceEditInvalid", func() error { return json.Unmarshal([]byte(`5`), new(DocumentChange)) }},
		{"document change of an unknown kind", "DocumentChange.UnmarshalJSON.WorkspaceEditInvalid", func() error { return json.Unmarshal([]byte(`{"kind":"copy"}`), new(DocumentChange)) }},
		{"prepare rename of a wrong type", "PrepareRenameResult.UnmarshalJSON.RenameInvalid", func() error { return json.Unmarshal([]byte(`5`), new(PrepareRenameResult)) }},
		{"inlay hint label of a wrong type", "InlayHintLabel.UnmarshalJSON.InlayHintInvalid", func() error { return json.Unmarshal([]byte(`5`), new(InlayHintLabel)) }},
		{"inlay hint label parts of a wrong type", "InlayHintLabel.UnmarshalJSON.InlayHintInvalid", func() error { return json.Unmarshal([]byte(`[5]`), new(InlayHintLabel)) }},
		{"semantic tokens of a wrong type", "SemanticTokensDeltaResult.UnmarshalJSON.SemanticTokensInvalid", func() error { return json.Unmarshal([]byte(`5`), new(SemanticTokensDeltaResult)) }},
		{"semantic tokens data of a wrong type", "SemanticTokensDeltaResult.UnmarshalJSON.SemanticTokensInvalid", func() error { return json.Unmarshal([]byte(`{"data":"x"}`), new(SemanticTokensDeltaResult)) }},
		{"semantic tokens edits of a wrong type", "SemanticTokensDeltaResult.UnmarshalJSON.SemanticTokensInvalid", func() error { return json.Unmarshal([]byte(`{"edits":5}`), new(SemanticTokensDeltaResult)) }},
		{"provider of a wrong type", "Provider.UnmarshalJSON.CapabilitiesInvalid", func() error { return json.Unmarshal([]byte(`5`), new(Provider)) }},
		{"sync options of a wrong type", "TextDocumentSyncOptions.UnmarshalJSON.CapabilitiesInvalid", func() error { return json.Unmarshal([]byte(`"x"`), new(TextDocumentSyncOptions)) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.do()
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T: %v", err, err)
			}
			if !appErr.HasRoute(tt.route) {
				t.Errorf("route = %s, want it to contain %s", appErr.Route(), tt.route)
			}
		})
	}
}
