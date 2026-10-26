package exthost

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/core/lsp"
	"github.com/rm4n0s/emetos/internal/core/lsp/lsptest"
)

// Completion through a language server (step 12.5): the list, the resolve of one item, and the command of an item that goes
// back to the server that sent it.

func completionScript() lsptest.Script {
	return lsptest.Script{
		Capabilities: raw(`{"textDocumentSync":2,"completionProvider":{"triggerCharacters":["."],"resolveProvider":true}}`),
		Responses: map[string]lsptest.Response{
			lsp.MethodCompletion: {Result: raw(`{"isIncomplete":false,"items":[{"label":"Println","kind":3,"data":{"id":7},"command":{"title":"After","command":"fake.after","arguments":["x"]}}]}`)},
			"completionItem/resolve": {Result: raw(`{"label":"Println","detail":"func(a ...any)","documentation":{"kind":"markdown","value":"prints"},` +
				`"additionalTextEdits":[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}},"newText":"import \"fmt\"\n"}]}`)},
			"workspace/executeCommand": {Result: raw(`null`)},
		},
	}
}

func TestCompletionResolveAndCommandGoThroughTheServer(t *testing.T) {
	st := newServerTest(t, completionScript(), nil)
	tb := st.startTab("one")
	d := st.goDoc(tb, "a.go", "package a\n")
	st.waitRunning(tb)
	c := st.controller(tb)
	f := tb.scope.Features()
	doc := langfeatures.Document{URI: d.URI(), LanguageID: "go", Version: d.Version(), Text: d.Snapshot()}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if got := f.CompletionTriggers(doc); len(got) != 1 || got[0] != "." {
		t.Errorf("trigger characters = %v", got)
	}
	list, errs := f.Completion(ctx, doc, langfeatures.Position{Line: 0, Col: 9}, langfeatures.CompletionTrigger{Kind: langfeatures.TriggerInvoked})
	if len(errs) != 0 || len(list.Items) != 1 {
		t.Fatalf("completion = %+v, %v", list, errs)
	}
	item := list.Items[0]
	if item.Command == nil || item.Command.ID != "fake.after" || item.Command.Owner == "" {
		t.Fatalf("the command of the item = %+v: it must name the server", item.Command)
	}
	if !f.CanResolveCompletion(item) {
		t.Fatal("the item cannot be resolved")
	}
	resolved, err := f.ResolveCompletion(ctx, doc, item)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Detail != "func(a ...any)" || resolved.Documentation == nil || resolved.Documentation.Value != "prints" || len(resolved.AdditionalEdits) != 1 {
		t.Errorf("resolved = %+v", resolved)
	}
	found, err := tb.scope.ExecuteServerCommand(ctx, *item.Command)
	if err != nil || !found {
		t.Fatalf("ExecuteServerCommand = %v, %v", found, err)
	}
	if n := countMethod(t, c, "workspace/executeCommand"); n != 1 {
		t.Errorf("%d executeCommand requests, want 1", n)
	}
	for _, m := range traffic(t, c) {
		if m.Method == "completionItem/resolve" && m.FromClient && !strings.Contains(string(m.Body), `"id":7`) {
			t.Errorf("the resolve did not send the data of the item: %s", m.Body)
		}
	}
	// a command of nobody's server is not found, so that the caller runs it as a command of the editor
	other := *item.Command
	other.Owner = "nobody"
	if found, err := tb.scope.ExecuteServerCommand(ctx, other); found || err != nil {
		t.Errorf("a command of an unknown server = %v, %v", found, err)
	}
}
