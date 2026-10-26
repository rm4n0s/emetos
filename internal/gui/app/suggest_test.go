package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/core/settings"
)

// Completion in the window (step 12.5): the words of the open documents are a provider of the tab, and the command of an item
// that no server owns is a command of the window.

func TestWordsOfTheOpenDocumentsAreAProviderOfTheWindow(t *testing.T) {
	w := newExtensionWorkspace(t, extensionProcess(t), "", "go.mod")
	write := func(name, text string) *document.Document {
		path := filepath.Join(w.Folder, name)
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		d, err := w.Documents.OpenFile(context.Background(), path, document.NewOptions())
		if err != nil {
			t.Fatal(err)
		}
		d.SetLanguageID("go")
		return d
	}
	a := write("a.go", "package a\n\nfunc alphabet() {}\nvar al\n")
	write("b.go", "package b\n\nfunc alphaBeta() {}\n")
	f := w.Extensions().Features()
	doc := langfeatures.Document{URI: a.URI(), LanguageID: "go", Version: a.Version(), Text: a.Snapshot()}
	wordsOf := func() []string {
		list, errs := f.Completion(context.Background(), doc, langfeatures.Position{Line: 3, Col: 6}, langfeatures.CompletionTrigger{Kind: langfeatures.TriggerInvoked})
		if len(errs) != 0 {
			t.Fatalf("errors: %+v", errs)
		}
		var out []string
		for _, it := range list.Items {
			out = append(out, it.Label)
		}
		slices.Sort(out)
		return out
	}
	// the default is the words of the documents of the same language
	if got := wordsOf(); !slices.Contains(got, "alphabet") || !slices.Contains(got, "alphaBeta") {
		t.Errorf("the words of the open documents = %v", got)
	}
	if err := w.Settings.Update(context.Background(), "editor.wordBasedSuggestions", "currentDocument", settings.TargetUser); err != nil {
		t.Fatal(err)
	}
	if got := wordsOf(); !slices.Contains(got, "alphabet") || slices.Contains(got, "alphaBeta") {
		t.Errorf("the words of the current document = %v", got)
	}
	if err := w.Settings.Update(context.Background(), "editor.wordBasedSuggestions", "off", settings.TargetUser); err != nil {
		t.Fatal(err)
	}
	if got := wordsOf(); len(got) != 0 {
		t.Errorf("the words with the setting off = %v", got)
	}
}

func TestTheCommandOfAnItemNoServerOwnsIsACommandOfTheWindow(t *testing.T) {
	w := newTestWorkspace(t, "")
	var got []any
	if err := w.Commands.Register(command.Meta{ID: "test.afterCompletion", Title: "Test"}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Commands.SetHandler("test.afterCompletion", func(_ command.Context, arg any) error { got = append(got, arg); return nil }); err != nil {
		t.Fatal(err)
	}
	host := w.suggestHost()
	cmd := langfeatures.Command{ID: "test.afterCompletion", Arguments: []json.RawMessage{json.RawMessage(`{"a":1}`), json.RawMessage(`2`)}}
	if err := host.Execute(context.Background(), cmd); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].(map[string]any)["a"] != float64(1) {
		t.Errorf("the handler got %v: the first argument of the item", got)
	}
	if err := host.Execute(context.Background(), langfeatures.Command{ID: "test.nobody"}); err == nil {
		t.Error("an unknown command is not an error")
	}
	if host.Shared == nil || host.Ctx == nil {
		t.Errorf("the host is incomplete: %+v", host)
	}
	if w.suggestHost().Shared != host.Shared {
		t.Error("the editors of the window do not share the state of the widget")
	}
}
