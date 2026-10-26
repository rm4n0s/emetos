package langfeatures_test

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	. "github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/core/lsp"
	"github.com/rm4n0s/emetos/internal/core/lsp/lsptest"
)

const allCapabilities = `{
	"completionProvider":{"triggerCharacters":["."]},
	"hoverProvider":true,
	"signatureHelpProvider":{"triggerCharacters":["("],"retriggerCharacters":[","]},
	"definitionProvider":true,"declarationProvider":true,"typeDefinitionProvider":true,"implementationProvider":true,
	"referencesProvider":true,"documentHighlightProvider":true,"documentSymbolProvider":true,
	"codeActionProvider":true,"documentFormattingProvider":true,"documentRangeFormattingProvider":true,
	"renameProvider":{"prepareProvider":true}}`

// emojiRange is the range of y in "x😀y = 1" for a server that counts UTF-16 units.
const emojiRange = `{"start":{"line":0,"character":3},"end":{"line":0,"character":4}}`

// connected is a ServerProvider over a fake server, and the server.
type connected struct {
	sp *ServerProvider
	h  *lsptest.Harness
}

func connect(t *testing.T, caps string, responses map[string]lsptest.Response) *connected {
	t.Helper()
	h := lsptest.Start(t, lsptest.Options{Script: lsptest.Script{Capabilities: json.RawMessage(caps), Responses: responses}})
	return &connected{sp: &ServerProvider{Name: "emetos.test.srv", Client: func() Server { return h.Client }}, h: h}
}

func result(s string) lsptest.Response { return lsptest.Response{Result: json.RawMessage(s)} }

// requests are the params of the requests of a method the server got.
func (c *connected) requests(method string) []json.RawMessage {
	var out []json.RawMessage
	for _, m := range c.h.Traffic() {
		if m.FromClient && m.Method == method && m.Kind == "request" {
			var body struct {
				Params json.RawMessage `json:"params"`
			}
			_ = json.Unmarshal(m.Body, &body)
			out = append(out, body.Params)
		}
	}
	return out
}

func TestServerProviderAnswers(t *testing.T) {
	ctx := context.Background()
	doc := docOf("go", "/work/a.go", emojiLine)
	at := Position{Line: 0, Col: 5} // y
	c := connect(t, allCapabilities, map[string]lsptest.Response{
		lsp.MethodCompletion:        result(`[{"label":"Println","kind":3}]`),
		lsp.MethodHover:             result(`{"contents":{"kind":"markdown","value":"hi"},"range":` + emojiRange + `}`),
		lsp.MethodSignatureHelp:     result(`{"signatures":[{"label":"f(a)"}]}`),
		lsp.MethodDefinition:        result(`{"uri":"file:///work/a.go","range":` + emojiRange + `}`),
		lsp.MethodDeclaration:       result(`{"uri":"file:///work/decl.go","range":` + emojiRange + `}`),
		lsp.MethodTypeDefinition:    result(`[]`),
		lsp.MethodImplementation:    result(`null`),
		lsp.MethodReferences:        result(`[{"uri":"file:///work/a.go","range":` + emojiRange + `}]`),
		lsp.MethodDocumentHighlight: result(`[{"range":` + emojiRange + `,"kind":3}]`),
		lsp.MethodDocumentSymbol:    result(`[{"name":"y","kind":13,"range":` + emojiRange + `,"selectionRange":` + emojiRange + `}]`),
		lsp.MethodCodeAction:        result(`[{"title":"Fix","command":{"title":"Fix","command":"gopls.fix"}}]`),
		lsp.MethodFormatting:        result(`[{"range":` + emojiRange + `,"newText":"z"}]`),
		lsp.MethodRangeFormatting:   result(`[{"range":` + emojiRange + `,"newText":"w"}]`),
		lsp.MethodPrepareRename:     result(`{"range":` + emojiRange + `,"placeholder":"y"}`),
		lsp.MethodRename:            result(`{"changes":{"file:///work/a.go":[{"range":` + emojiRange + `,"newText":"z"}]}}`),
	})
	sp := c.sp

	list, err := sp.ProvideCompletion(ctx, doc, at, CompletionTrigger{Kind: TriggerCharacter, Character: "."})
	if err != nil || len(list.Items) != 1 || list.Items[0].Label != "Println" {
		t.Errorf("completion = %+v, %v", list, err)
	}
	var cp struct {
		Position lsp.Position `json:"position"`
		Context  struct {
			TriggerKind      int    `json:"triggerKind"`
			TriggerCharacter string `json:"triggerCharacter"`
		} `json:"context"`
	}
	if err := json.Unmarshal(c.requests(lsp.MethodCompletion)[0], &cp); err != nil {
		t.Fatal(err)
	}
	if cp.Position.Character != 3 || cp.Context.TriggerKind != lsp.TriggerCharacter || cp.Context.TriggerCharacter != "." {
		t.Errorf("completion params = %+v (the position is the UTF-16 offset of y)", cp)
	}

	if h, err := sp.ProvideHover(ctx, doc, at); err != nil || h == nil || h.Contents[0].Value != "hi" || *h.Range != rng(0, 5, 0, 6) {
		t.Errorf("hover = %+v, %v", h, err)
	}
	if h, err := sp.ProvideSignatureHelp(ctx, doc, at, SignatureContext{Character: "(", IsRetrigger: false}); err != nil || h == nil || h.Signatures[0].Label != "f(a)" {
		t.Errorf("signature help = %+v, %v", h, err)
	}
	for kind, want := range map[NavKind]string{NavDefinition: "/work/a.go", NavDeclaration: "/work/decl.go"} {
		links, err := sp.ProvideLocations(ctx, doc, at, kind)
		if err != nil || len(links) != 1 || links[0].Target.URI.Path != want {
			t.Errorf("locations %d = %+v, %v", kind, links, err)
		}
	}
	for _, kind := range []NavKind{NavTypeDefinition, NavImplementation} {
		if links, err := sp.ProvideLocations(ctx, doc, at, kind); err != nil || len(links) != 0 {
			t.Errorf("locations %d = %+v, %v", kind, links, err)
		}
	}
	if refs, err := sp.ProvideReferences(ctx, doc, at, true); err != nil || len(refs) != 1 || refs[0].Range != rng(0, 5, 0, 6) {
		t.Errorf("references = %+v, %v", refs, err)
	}
	if hl, err := sp.ProvideDocumentHighlights(ctx, doc, at); err != nil || len(hl) != 1 || hl[0].Kind != HighlightWrite {
		t.Errorf("highlights = %+v, %v", hl, err)
	}
	if syms, err := sp.ProvideDocumentSymbols(ctx, doc); err != nil || len(syms) != 1 || syms[0].SelectionRange != rng(0, 5, 0, 6) {
		t.Errorf("symbols = %+v, %v", syms, err)
	}
	acts, err := sp.ProvideCodeActions(ctx, doc, CodeActionRequest{Range: rng(0, 5, 0, 6), Diagnostics: []Diagnostic{{Range: rng(0, 5, 0, 6), Message: "m", Severity: SeverityWarning}}})
	if err != nil || len(acts) != 1 || acts[0].Command == nil || acts[0].Command.Owner != "emetos.test.srv" {
		t.Errorf("code actions = %+v, %v", acts, err)
	}
	var cap struct {
		Range   lsp.Range `json:"range"`
		Context struct {
			Diagnostics []lsp.Diagnostic `json:"diagnostics"`
		} `json:"context"`
	}
	if err := json.Unmarshal(c.requests(lsp.MethodCodeAction)[0], &cap); err != nil {
		t.Fatal(err)
	}
	if cap.Range.Start.Character != 3 || len(cap.Context.Diagnostics) != 1 || cap.Context.Diagnostics[0].Severity != lsp.SeverityWarning || cap.Context.Diagnostics[0].Range.End.Character != 4 {
		t.Errorf("code action params = %+v", cap)
	}
	if edits, err := sp.ProvideFormatting(ctx, doc, FormattingOptions{TabSize: 4}); err != nil || len(edits) != 1 || edits[0].Text != "z" || edits[0].Range != rng(0, 5, 0, 6) {
		t.Errorf("formatting = %+v, %v", edits, err)
	}
	if edits, err := sp.ProvideRangeFormatting(ctx, doc, rng(0, 0, 0, 6), FormattingOptions{}); err != nil || edits[0].Text != "w" {
		t.Errorf("range formatting = %+v, %v", edits, err)
	}
	if p, err := sp.PrepareRename(ctx, doc, at); err != nil || p == nil || p.Range != rng(0, 5, 0, 6) || p.Placeholder != "y" {
		t.Errorf("prepare rename = %+v, %v", p, err)
	}
	we, err := sp.ProvideRename(ctx, doc, at, "z")
	if err != nil || we == nil {
		t.Fatalf("rename = %+v, %v", we, err)
	}

	if tr := sp.CompletionTriggers(); !slices.Equal(tr, []string{"."}) {
		t.Errorf("completion triggers = %v", tr)
	}
	if tr := sp.SignatureTriggers(); !slices.Equal(tr, []string{"(", ","}) {
		t.Errorf("signature triggers = %v", tr)
	}
}

func TestServerProviderDoesNotAskForWhatTheServerLacks(t *testing.T) {
	ctx := context.Background()
	doc := docOf("go", "/work/a.go", emojiLine)
	c := connect(t, `{"hoverProvider":false}`, nil)
	sp := c.sp
	if h, err := sp.ProvideHover(ctx, doc, Position{}); h != nil || err != nil {
		t.Errorf("hover = %+v, %v", h, err)
	}
	if l, err := sp.ProvideCompletion(ctx, doc, Position{}, CompletionTrigger{}); len(l.Items) != 0 || err != nil {
		t.Errorf("completion = %+v, %v", l, err)
	}
	if r, err := sp.ProvideRename(ctx, doc, Position{}, "x"); r != nil || err != nil {
		t.Errorf("rename = %+v, %v", r, err)
	}
	if p, err := sp.PrepareRename(ctx, doc, Position{}); p != nil || err != nil {
		t.Errorf("prepare = %+v, %v", p, err)
	}
	if n := len(c.requests(lsp.MethodHover)) + len(c.requests(lsp.MethodCompletion)) + len(c.requests(lsp.MethodRename)); n != 0 {
		t.Errorf("%d requests were sent", n)
	}
	if sp.CompletionTriggers() != nil || sp.SignatureTriggers() != nil {
		t.Error("triggers without the capability")
	}
}

func TestServerProviderWithoutAClient(t *testing.T) {
	ctx := context.Background()
	doc := docOf("go", "/work/a.go", emojiLine)
	for _, sp := range []*ServerProvider{{Name: "n"}, {Name: "n", Client: func() Server { return nil }}} {
		if h, err := sp.ProvideHover(ctx, doc, Position{}); h != nil || err != nil {
			t.Errorf("hover = %+v, %v", h, err)
		}
		if l, err := sp.ProvideLocations(ctx, doc, Position{}, NavDefinition); len(l) != 0 || err != nil {
			t.Errorf("locations = %+v, %v", l, err)
		}
		if es, err := sp.ProvideFormatting(ctx, doc, FormattingOptions{}); len(es) != 0 || err != nil {
			t.Errorf("formatting = %+v, %v", es, err)
		}
	}
}

func TestServerProviderFeaturesFromRegistrations(t *testing.T) {
	// a server that registers its features dynamically offers them all the same
	ctx := context.Background()
	doc := docOf("go", "/work/a.go", emojiLine)
	h := lsptest.Start(t, lsptest.Options{Script: lsptest.Script{
		Responses: map[string]lsptest.Response{lsp.MethodHover: result(`{"contents":"hi"}`)},
		Send: []lsptest.Outgoing{{After: "initialized", Request: true, Method: lsp.MethodRegisterCapability,
			Params: json.RawMessage(`{"registrations":[{"id":"1","method":"textDocument/hover"}]}`)}},
	}})
	sp := &ServerProvider{Client: func() Server { return h.Client }}
	deadline := 0
	for len(h.Client.Registered(lsp.MethodHover)) == 0 {
		deadline++
		if deadline > 2000 {
			t.Fatal("the registration did not arrive")
		}
		sleepABit()
	}
	if got, err := sp.ProvideHover(ctx, doc, Position{}); err != nil || got == nil || got.Contents[0].Value != "hi" {
		t.Errorf("hover = %+v, %v", got, err)
	}
}

func TestServerProviderErrors(t *testing.T) {
	ctx := context.Background()
	doc := docOf("go", "/work/a.go", emojiLine)
	c := connect(t, allCapabilities, map[string]lsptest.Response{
		lsp.MethodHover:         {Error: &lsp.ResponseError{Code: -32603, Message: "boom"}},
		lsp.MethodPrepareRename: result(`null`),
	})
	_, err := c.sp.ProvideHover(ctx, doc, Position{})
	routeOf(t, err, "ServerError")
	_, err = c.sp.PrepareRename(ctx, doc, Position{})
	routeOf(t, err, "ServerProvider.PrepareRename.NotRenameable")
	// a server that cannot prepare leaves the word to the editor
	d := connect(t, `{"renameProvider":true}`, nil)
	if p, err := d.sp.PrepareRename(ctx, doc, Position{}); p != nil || err != nil {
		t.Errorf("prepare without prepareProvider = %+v, %v", p, err)
	}
}

func TestRegisterAddsEveryFeature(t *testing.T) {
	ctx := context.Background()
	doc := docOf("go", "/work/a.go", emojiLine)
	c := connect(t, allCapabilities, map[string]lsptest.Response{
		lsp.MethodHover:      result(`{"contents":"hi"}`),
		lsp.MethodDefinition: result(`{"uri":"file:///work/a.go","range":` + emojiRange + `}`),
		lsp.MethodFormatting: result(`[]`),
	})
	reg := NewRegistry()
	remove := c.sp.Register(reg, "emetos.test", Selector{Languages: []string{"go"}})
	if hs, errs := reg.Hover(ctx, doc, Position{}); len(hs) != 1 || len(errs) != 0 || hs[0].Owner != "emetos.test" {
		t.Errorf("hover = %+v %v", hs, errs)
	}
	if ls, _ := reg.Locations(ctx, doc, Position{}, NavDefinition); len(ls) != 1 {
		t.Errorf("locations = %+v", ls)
	}
	if _, _, ok := reg.PickFormatter(doc, "emetos.test"); !ok {
		t.Error("no formatter")
	}
	if tr := reg.CompletionTriggers(doc); !slices.Equal(tr, []string{"."}) {
		t.Errorf("completion triggers = %v", tr)
	}
	if hs, _ := reg.Hover(ctx, docOf("rust", "/work/a.rs", ""), Position{}); len(hs) != 0 {
		t.Errorf("a document of another language got %+v", hs)
	}
	remove()
	if hs, _ := reg.Hover(ctx, doc, Position{}); len(hs) != 0 {
		t.Errorf("hover after remove = %+v", hs)
	}
}

// sleepABit gives the connection a moment.
func sleepABit() { time.Sleep(5 * time.Millisecond) }

func TestSignatureHelpContextReachesTheServer(t *testing.T) {
	ctx := context.Background()
	doc := docOf("go", "/work/a.go", emojiLine)
	c := connect(t, allCapabilities, map[string]lsptest.Response{
		lsp.MethodSignatureHelp: result(`{"signatures":[{"label":"f(a, b)","parameters":[{"label":"a"},{"label":[5,6]}]},{"label":"f()"}],"activeSignature":0,"activeParameter":1}`),
	})
	first, err := c.sp.ProvideSignatureHelp(ctx, doc, Position{}, SignatureContext{Kind: SignatureInvoke})
	if err != nil || first == nil || len(first.Signatures) != 2 {
		t.Fatalf("help = %+v, %v", first, err)
	}
	// the user went to the second signature, then typed a comma: the server gets the help it sent with that signature active
	moved := *first
	moved.ActiveSignature = 1
	if _, err := c.sp.ProvideSignatureHelp(ctx, doc, Position{}, SignatureContext{Kind: SignatureTriggerCharacter, Character: ",", IsRetrigger: true, Active: &moved}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.sp.ProvideSignatureHelp(ctx, doc, Position{}, SignatureContext{Kind: SignatureContentChange, IsRetrigger: true, Active: first}); err != nil {
		t.Fatal(err)
	}
	reqs := c.requests(lsp.MethodSignatureHelp)
	if len(reqs) != 3 {
		t.Fatalf("%d requests", len(reqs))
	}
	var got [3]struct {
		Context struct {
			TriggerKind         int                `json:"triggerKind"`
			TriggerCharacter    string             `json:"triggerCharacter"`
			IsRetrigger         bool               `json:"isRetrigger"`
			ActiveSignatureHelp *lsp.SignatureHelp `json:"activeSignatureHelp"`
		} `json:"context"`
	}
	for i, r := range reqs {
		if err := json.Unmarshal(r, &got[i]); err != nil {
			t.Fatal(err)
		}
	}
	if k := got[0].Context; k.TriggerKind != lsp.SignatureHelpInvoked || k.IsRetrigger || k.ActiveSignatureHelp != nil || k.TriggerCharacter != "" {
		t.Errorf("invoked: %+v", k)
	}
	k := got[1].Context
	if k.TriggerKind != lsp.SignatureHelpTriggerChar || k.TriggerCharacter != "," || !k.IsRetrigger || k.ActiveSignatureHelp == nil ||
		len(k.ActiveSignatureHelp.Signatures) != 2 || *k.ActiveSignatureHelp.ActiveSignature != 1 {
		t.Errorf("character: %+v", k)
	}
	if p := k.ActiveSignatureHelp.Signatures[0].Parameters[1].Label; p.Offsets == nil || *p.Offsets != [2]int{5, 6} {
		t.Errorf("the offsets of the label did not come back as they were sent: %+v", p)
	}
	if k := got[2].Context; k.TriggerKind != lsp.SignatureHelpContentChange || !k.IsRetrigger || *k.ActiveSignatureHelp.ActiveSignature != 0 {
		t.Errorf("content change: %+v", k)
	}
	// a help that was made by another provider is not sent to the server
	other := &SignatureHelp{Signatures: []Signature{{Label: "g()"}}}
	if _, err := c.sp.ProvideSignatureHelp(ctx, doc, Position{}, SignatureContext{Kind: SignatureContentChange, IsRetrigger: true, Active: other}); err != nil {
		t.Fatal(err)
	}
	var last struct {
		Context struct {
			ActiveSignatureHelp *lsp.SignatureHelp `json:"activeSignatureHelp"`
		} `json:"context"`
	}
	reqs = c.requests(lsp.MethodSignatureHelp)
	if err := json.Unmarshal(reqs[len(reqs)-1], &last); err != nil || last.Context.ActiveSignatureHelp != nil {
		t.Errorf("active help of another provider = %+v, %v", last.Context.ActiveSignatureHelp, err)
	}
}

func TestServerProviderTellsItsSignatureCharacters(t *testing.T) {
	c := connect(t, allCapabilities, nil)
	triggers, retriggers := c.sp.SignatureCharacters()
	if !slices.Equal(triggers, []string{"("}) || !slices.Equal(retriggers, []string{","}) {
		t.Errorf("characters = %q, %q", triggers, retriggers)
	}
	none := &ServerProvider{Name: "x", Client: func() Server { return nil }}
	if tr, re := none.SignatureCharacters(); tr != nil || re != nil {
		t.Errorf("a provider without a server has characters %q %q", tr, re)
	}
}
