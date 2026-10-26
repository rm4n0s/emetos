package langfeatures_test

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	. "github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/core/lsp"
	"github.com/rm4n0s/emetos/internal/core/lsp/lsptest"
	"github.com/rm4n0s/emetos/internal/core/snippet"
)

func TestCompletionOfTriggerCharactersAsksTheProvidersThatHaveThem(t *testing.T) {
	calls := &callLog{}
	r := NewRegistry()
	r.RegisterCompletion("a", Selector{Languages: []string{"go"}}, &fakeProvider{name: "dot", calls: calls, items: []CompletionItem{{Label: "x"}}}, ".")
	r.RegisterCompletion("b", Selector{Languages: []string{"go"}}, &fakeProvider{name: "other", calls: calls, items: []CompletionItem{{Label: "y"}}}, ":")
	r.RegisterCompletion("c", Selector{}, &fakeProvider{name: "words", calls: calls, items: []CompletionItem{{Label: "w"}}})
	doc := docOf("go", "/a.go", "x\n")
	tests := []struct {
		name    string
		trigger CompletionTrigger
		query   CompletionQuery
		want    []string
		items   []string
	}{
		{"invoked asks the best group", CompletionTrigger{}, CompletionQuery{}, []string{"dot", "other"}, []string{"x", "y"}},
		{"a trigger character asks its providers", CompletionTrigger{Kind: TriggerCharacter, Character: "."}, CompletionQuery{}, []string{"dot"}, []string{"x"}},
		{"another trigger character", CompletionTrigger{Kind: TriggerCharacter, Character: ":"}, CompletionQuery{}, []string{"other"}, []string{"y"}},
		{"a character nobody has asks nobody", CompletionTrigger{Kind: TriggerCharacter, Character: ";"}, CompletionQuery{}, nil, nil},
		{"only the owners of the query", CompletionTrigger{Kind: TriggerIncomplete}, CompletionQuery{Owners: map[string]bool{"b": true}}, []string{"other"}, []string{"y"}},
		{"an owner nobody has falls to the words", CompletionTrigger{Kind: TriggerIncomplete}, CompletionQuery{Owners: map[string]bool{"c": true}}, []string{"words"}, []string{"w"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls.mu.Lock()
			calls.names = nil
			calls.mu.Unlock()
			list, errs := r.CompletionFor(context.Background(), doc, Position{}, tt.trigger, tt.query)
			if len(errs) != 0 {
				t.Fatalf("errors %v", errs)
			}
			calls.mu.Lock()
			got := slices.Clone(calls.names)
			calls.mu.Unlock()
			slices.Sort(got)
			want := slices.Clone(tt.want)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Errorf("asked %v, want %v", got, want)
			}
			var items []string
			for _, it := range list.Items {
				items = append(items, it.Label)
			}
			slices.Sort(items)
			if !slices.Equal(items, tt.items) {
				t.Errorf("items %v, want %v", items, tt.items)
			}
		})
	}
}

func TestCompletionMarksItemsOfIncompleteLists(t *testing.T) {
	r := NewRegistry()
	r.RegisterCompletion("a", Selector{}, &fakeProvider{items: []CompletionItem{{Label: "x"}}, incompl: true})
	r.RegisterCompletion("b", Selector{}, &fakeProvider{items: []CompletionItem{{Label: "y"}}})
	list, _ := r.Completion(context.Background(), docOf("go", "/a.go", "x\n"), Position{}, CompletionTrigger{})
	got := map[string]bool{}
	for _, it := range list.Items {
		got[it.Owner] = it.IncompleteList
	}
	if len(got) != 2 || !got["a"] || got["b"] {
		t.Errorf("incomplete marks %v, want a incomplete and b complete", got)
	}
	if !list.Incomplete {
		t.Error("the list is not incomplete")
	}
}

func TestHasCompletion(t *testing.T) {
	r := NewRegistry()
	doc := docOf("go", "/a.go", "x\n")
	if r.HasCompletion(doc) {
		t.Error("an empty registry has a provider")
	}
	remove := r.RegisterCompletion("a", Selector{Languages: []string{"rust"}}, &fakeProvider{})
	if r.HasCompletion(doc) {
		t.Error("a provider of another language matches")
	}
	r.RegisterCompletion("b", Selector{}, &fakeProvider{})
	if !r.HasCompletion(doc) {
		t.Error("a provider of every document does not match")
	}
	remove()
}

// ---- resolve, with the fake server ----

const completionCapabilities = `{"completionProvider":{"triggerCharacters":["."],"resolveProvider":true}}`

func TestServerProviderResolvesAnItem(t *testing.T) {
	ctx := context.Background()
	doc := docOf("go", "/work/a.go", emojiLine)
	c := connect(t, completionCapabilities, map[string]lsptest.Response{
		lsp.MethodCompletion: result(`{"isIncomplete":false,"itemDefaults":{"data":{"list":1}},"items":[
			{"label":"Println","kind":3,"command":{"title":"c","command":"gopls.after"}},
			{"label":"Own","kind":3,"data":{"own":2}}]}`),
		lsp.MethodCompletionResolve: result(`{"label":"Println","documentation":{"kind":"markdown","value":"prints"},"detail":"func(a ...any)",
			"additionalTextEdits":[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}},"newText":"import \"fmt\"\n"}],
			"insertText":"CHANGED"}`),
	})
	r := NewRegistry()
	c.sp.Register(r, "ext", Selector{Languages: []string{"go"}})
	list, errs := r.Completion(ctx, doc, Position{Line: 0, Col: 5}, CompletionTrigger{})
	if len(errs) != 0 || len(list.Items) != 2 {
		t.Fatalf("list %+v, %v", list, errs)
	}
	first := list.Items[0]
	if first.Command == nil || first.Command.Owner != "emetos.test.srv" {
		t.Errorf("the command of an item is of the server: %+v", first.Command)
	}
	if !r.CanResolveCompletion(first) {
		t.Fatal("the item cannot be resolved")
	}
	got, err := r.ResolveCompletion(ctx, doc, first)
	if err != nil {
		t.Fatal(err)
	}
	if got.Documentation == nil || got.Documentation.Value != "prints" || got.Detail != "func(a ...any)" || len(got.AdditionalEdits) != 1 {
		t.Errorf("resolved %+v", got)
	}
	if got.InsertText == "CHANGED" || got.Label != "Println" {
		t.Errorf("a resolve changed what the list gave: %+v", got)
	}
	if got.Command == nil || got.Command.ID != "gopls.after" {
		t.Errorf("the command of the list is lost: %+v", got.Command)
	}
	reqs := c.requests(lsp.MethodCompletionResolve)
	if len(reqs) != 1 {
		t.Fatalf("%d resolve requests", len(reqs))
	}
	var sent struct {
		Label string          `json:"label"`
		Data  json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(reqs[0], &sent); err != nil {
		t.Fatal(err)
	}
	if sent.Label != "Println" || string(sent.Data) != `{"list":1}` {
		t.Errorf("the item went back as %+v: it has the data of the defaults of its list", sent)
	}
	// an item with data of its own keeps it
	if _, err := r.ResolveCompletion(ctx, doc, list.Items[1]); err != nil {
		t.Fatal(err)
	}
	var second struct {
		Data json.RawMessage `json:"data"`
	}
	if reqs = c.requests(lsp.MethodCompletionResolve); len(reqs) != 2 || json.Unmarshal(reqs[1], &second) != nil || string(second.Data) != `{"own":2}` {
		t.Errorf("second resolve request %s", reqs)
	}
}

func TestServerProviderResolveNeedsTheCapability(t *testing.T) {
	c := connect(t, `{"completionProvider":{}}`, map[string]lsptest.Response{lsp.MethodCompletion: result(`[{"label":"a"}]`)})
	r := NewRegistry()
	c.sp.Register(r, "ext", Selector{})
	list, _ := r.Completion(context.Background(), docOf("go", "/a.go", "a\n"), Position{}, CompletionTrigger{})
	if len(list.Items) != 1 || r.CanResolveCompletion(list.Items[0]) {
		t.Fatal("an item can be resolved by a server that does not resolve")
	}
	got, err := r.ResolveCompletion(context.Background(), docOf("go", "/a.go", "a\n"), list.Items[0])
	if err != nil || got.Label != "a" || len(c.requests(lsp.MethodCompletionResolve)) != 0 {
		t.Errorf("got %+v, %v; resolve requests %d", got, err, len(c.requests(lsp.MethodCompletionResolve)))
	}
}

func TestResolveOfAnItemWithoutAResolver(t *testing.T) {
	r := NewRegistry()
	r.RegisterCompletion("a", Selector{}, &fakeProvider{items: []CompletionItem{{Label: "x"}}})
	doc := docOf("go", "/a.go", "x\n")
	list, _ := r.Completion(context.Background(), doc, Position{}, CompletionTrigger{})
	if r.CanResolveCompletion(list.Items[0]) {
		t.Error("an item of a provider that cannot resolve can be resolved")
	}
	got, err := r.ResolveCompletion(context.Background(), doc, list.Items[0])
	if err != nil || got.Label != "x" {
		t.Errorf("got %+v, %v", got, err)
	}
}

func TestServerProviderExecutesACommand(t *testing.T) {
	c := connect(t, `{"completionProvider":{},"executeCommandProvider":{"commands":["gopls.x"]}}`, map[string]lsptest.Response{
		lsp.MethodExecuteCommand: result(`null`),
	})
	arg := json.RawMessage(`{"a":1}`)
	if err := c.sp.ExecuteCommand(context.Background(), Command{ID: "gopls.x", Arguments: []json.RawMessage{arg}}); err != nil {
		t.Fatal(err)
	}
	reqs := c.requests(lsp.MethodExecuteCommand)
	if len(reqs) != 1 {
		t.Fatalf("%d requests", len(reqs))
	}
	var p lsp.ExecuteCommandParams
	if err := json.Unmarshal(reqs[0], &p); err != nil || p.Command != "gopls.x" || len(p.Arguments) != 1 || string(p.Arguments[0]) != `{"a":1}` {
		t.Errorf("params %s", reqs[0])
	}
}

func TestMergeResolvedCompletion(t *testing.T) {
	old := CompletionItem{Label: "a", Detail: "old", InsertText: "a", Command: &Command{ID: "x", Owner: "srv"}}
	tests := []struct {
		name     string
		resolved CompletionItem
		check    func(t *testing.T, got CompletionItem)
	}{
		{"empty resolve keeps all", CompletionItem{}, func(t *testing.T, got CompletionItem) {
			if got.Detail != "old" || got.Command.ID != "x" {
				t.Errorf("%+v", got)
			}
		}},
		{"detail and documentation", CompletionItem{Detail: "new", Documentation: &Markup{Value: "d"}}, func(t *testing.T, got CompletionItem) {
			if got.Detail != "new" || got.Documentation.Value != "d" {
				t.Errorf("%+v", got)
			}
		}},
		{"a command keeps the owner", CompletionItem{Command: &Command{ID: "y"}}, func(t *testing.T, got CompletionItem) {
			if got.Command.ID != "y" || got.Command.Owner != "srv" {
				t.Errorf("%+v", got.Command)
			}
		}},
		{"edits", CompletionItem{AdditionalEdits: []TextEdit{{Text: "i"}}}, func(t *testing.T, got CompletionItem) {
			if len(got.AdditionalEdits) != 1 {
				t.Errorf("%+v", got)
			}
		}},
		{"the text to insert is not changed", CompletionItem{InsertText: "other", Label: "b"}, func(t *testing.T, got CompletionItem) {
			if got.InsertText != "a" || got.Label != "a" {
				t.Errorf("%+v", got)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { tt.check(t, MergeResolvedCompletion(old, tt.resolved)) })
	}
}

// ---- words ----

func TestWordProvider(t *testing.T) {
	cur := docOf("go", "/a.go", "alpha beta alpha 42 gam\n")
	other := Document{URI: docOf("go", "/b.go", "").URI, LanguageID: "go", Version: 1, Text: buffer.New("delta alpha\nepsilon\n").Snapshot()}
	text := Document{URI: docOf("go", "/c.txt", "").URI, LanguageID: "plaintext", Version: 1, Text: buffer.New("zeta\n").Snapshot()}
	tests := []struct {
		name string
		mode string
		want []string
	}{
		{"off", WordsOff, nil},
		{"current document", WordsCurrentDocument, []string{"alpha", "beta"}},
		{"matching documents", WordsMatchingDocuments, []string{"alpha", "beta", "delta", "epsilon"}},
		{"off with inline suggestions has no inline suggestions here", WordsOffWithInline, []string{"alpha", "beta", "delta", "epsilon"}},
		{"all documents", WordsAllDocuments, []string{"alpha", "beta", "delta", "epsilon", "zeta"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &WordProvider{
				Mode:      func(Document) string { return tt.mode },
				Documents: func(context.Context) []Document { return []Document{cur, other, text} },
				WordAt:    wordsAt,
			}
			// the cursor is after "gam": that word is the one being typed, so it is not a suggestion
			list, err := p.ProvideCompletion(context.Background(), cur, Position{Line: 0, Col: 23}, CompletionTrigger{})
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, it := range list.Items {
				got = append(got, it.Label)
				if it.Kind != 1 {
					t.Errorf("kind of %q is %d, want the text kind", it.Label, it.Kind)
				}
			}
			slices.Sort(got)
			if !slices.Equal(got, tt.want) {
				t.Errorf("words %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWordProviderIsTheFallbackOfTheLanguageProviders(t *testing.T) {
	r := NewRegistry()
	wp := &WordProvider{Mode: func(Document) string { return WordsCurrentDocument }, WordAt: wordsAt}
	r.RegisterCompletion(WordOwner, Selector{}, wp)
	doc := docOf("go", "/a.go", "alpha beta\n")
	words := func() []string {
		list, _ := r.Completion(context.Background(), doc, Position{Line: 0, Col: 10}, CompletionTrigger{})
		var out []string
		for _, it := range list.Items {
			out = append(out, it.Label)
		}
		slices.Sort(out)
		return out
	}
	if got := words(); !slices.Equal(got, []string{"alpha"}) {
		t.Errorf("words with no other provider: %v", got) // beta is the word at the cursor
	}
	remove := r.RegisterCompletion("srv", Selector{Languages: []string{"go"}}, &fakeProvider{items: []CompletionItem{{Label: "Println"}}})
	if got := words(); !slices.Equal(got, []string{"Println"}) {
		t.Errorf("words with a provider of the language: %v, want only its items", got)
	}
	remove()
	r.RegisterCompletion("srv", Selector{Languages: []string{"go"}}, &fakeProvider{})
	if got := words(); !slices.Equal(got, []string{"alpha"}) {
		t.Errorf("words when the provider of the language has nothing: %v", got)
	}
}

// ---- snippets ----

func snip(prefix, name, desc, body, source string) *snippet.Snippet {
	return &snippet.Snippet{Prefix: prefix, PrefixLow: lower(prefix), Name: name, Description: desc, Body: body, Source: source}
}

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

func TestSnippetItems(t *testing.T) {
	snippets := []*snippet.Snippet{
		snip("for", "for loop", "a for loop", "for ${1:i} := range ${2:x} {\n\t$0\n}", "User Snippet"),
		snip("fmt.p", "print", "", "fmt.Println($0)", "User Snippet"),
		snip("func", "function", "", "func $1() {}", "Go"),
		snip("if", "if", "", "if $1 {}", "User Snippet"),
	}
	rg := func(a, b int) *Range { return &Range{Start: Position{Col: a}, End: Position{Col: b}} }
	tests := []struct {
		name    string
		line    string
		col     int
		trigger CompletionTrigger
		want    []string
		// insert/replace of the first item
		insert, replace *Range
	}{
		{"a prefix typed", "fo", 2, CompletionTrigger{}, []string{"for"}, rg(0, 2), rg(0, 2)},
		{"fuzzy: the typed characters are in the prefix in order", "fn", 2, CompletionTrigger{}, []string{"func"}, rg(0, 2), rg(0, 2)},
		{"several match, and the letter is in the prefix of if", "f", 1, CompletionTrigger{}, []string{"fmt.p", "for", "func", "if"}, rg(0, 1), rg(0, 1)},
		{"after other words the word is the anchor", "x := fo", 7, CompletionTrigger{}, []string{"for"}, rg(5, 7), rg(5, 7)},
		{"the rest of the prefix is after the cursor", "fo|r", 2, CompletionTrigger{}, []string{"for"}, rg(0, 2), rg(0, 3)},
		{"a prefix with a dot is matched from the text before the dot", "fmt.p", 5, CompletionTrigger{}, []string{"fmt.p"}, rg(0, 5), rg(0, 5)},
		{"a trigger character keeps the prefixes that start with it", "fmt.", 4, CompletionTrigger{Kind: TriggerCharacter, Character: "."}, nil, nil, nil},
		{"a trigger character that starts a prefix", "f", 1, CompletionTrigger{Kind: TriggerCharacter, Character: "f"}, []string{"fmt.p", "for", "func"}, rg(0, 1), rg(0, 1)},
		{"nothing matches", "zz", 2, CompletionTrigger{}, nil, nil, nil},
		{"after a space all the snippets", "x ", 2, CompletionTrigger{}, []string{"fmt.p", "for", "func", "if"}, rg(2, 2), rg(2, 2)},
		{"on an empty line all the snippets", "", 0, CompletionTrigger{}, []string{"fmt.p", "for", "func", "if"}, rg(0, 0), rg(0, 0)},
		{"after a space, not for a trigger character", "x ", 2, CompletionTrigger{Kind: TriggerCharacter, Character: " "}, nil, nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line, col := tt.line, tt.col
			if i := indexByte(line, '|'); i >= 0 {
				line = line[:i] + line[i+1:]
			}
			items := SnippetItems(snippets, line, Position{Line: 0, Col: col}, wordsAt, tt.trigger)
			var got []string
			for _, it := range items {
				got = append(got, it.Label)
				if it.Kind != SnippetKind || !it.Snippet || it.Owner != SnippetOwner || it.Documentation == nil {
					t.Errorf("item %+v is not a snippet row", it)
				}
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("snippets %v, want %v", got, tt.want)
			}
			if len(items) > 0 && tt.insert != nil {
				if *items[0].Insert != *tt.insert || *items[0].Replace != *tt.replace {
					t.Errorf("insert %+v replace %+v, want %+v %+v", *items[0].Insert, *items[0].Replace, *tt.insert, *tt.replace)
				}
			}
		})
	}
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func TestSnippetItemsDetails(t *testing.T) {
	items := SnippetItems([]*snippet.Snippet{
		snip("for", "for loop", "a for loop", "for $1 {}", "User Snippet"),
		snip("fn", "function", "", "func $1() {}", "Go"),
	}, "f", Position{Col: 1}, wordsAt, CompletionTrigger{})
	if len(items) != 2 {
		t.Fatalf("%d items", len(items))
	}
	byLabel := map[string]CompletionItem{}
	for _, it := range items {
		byLabel[it.Label] = it
	}
	f := byLabel["for"]
	if f.LabelDescription != "for loop" || f.Detail != "a for loop (User Snippet)" || f.SortText != "a-for" || f.InsertText != "for $1 {}" {
		t.Errorf("for = %+v", f)
	}
	if !f.Documentation.Markdown || f.Documentation.Value != "```\nfor  {}\n```" {
		t.Errorf("documentation %q: the code of the snippet without its tab stops", f.Documentation.Value)
	}
	g := byLabel["fn"]
	if g.Detail != "function (Go)" || g.SortText != "z-fn" {
		t.Errorf("fn = %+v: a snippet of a language sorts after the user's", g)
	}
}
