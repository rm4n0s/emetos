package langfeatures_test

import (
	"strconv"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	. "github.com/rm4n0s/emetos/internal/core/langfeatures"
)

// Tests ported from VS Code: src/vs/editor/contrib/suggest/test/browser/completionModel.test.ts. The positions of VS Code's tests
// are 1-based columns of characters; here the column is the byte column from 0, and the texts are ASCII, so a column of the
// test is one more than a column here.

func wordsAt(line string, col int) (int, int, bool) {
	isWord := func(b byte) bool {
		return b == '_' || b == '-' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
	}
	col = min(col, len(line))
	s, e := col, col
	for s > 0 && isWord(line[s-1]) {
		s--
	}
	for e < len(line) && isWord(line[e]) {
		e++
	}
	return s, e, e > s
}

var itemCount int

// suggestItem is VS Code's createSuggestItem: an item that overwrites overwriteBefore characters before the position (the
// 1-based column of VS Code), on its own provider.
func suggestItem(label string, overwriteBefore int, kind CompletionKind, incomplete bool, column int, sortText, filterText string) *SuggestItem {
	itemCount++
	pos := Position{Line: 0, Col: column - 1}
	it := CompletionItem{
		Label: label, SortText: sortText, FilterText: filterText, InsertText: label, Kind: kind,
		Insert:         &Range{Start: Position{Col: pos.Col - overwriteBefore}, End: pos},
		IncompleteList: incomplete, Owner: "p" + strconv.Itoa(itemCount),
	}
	req := SuggestRequest{Doc: docOf("go", "/a.go", "\n"), Pos: pos, WordAt: wordsAt}
	return NewSuggestItems(req, []CompletionItem{it}, ItemFilter{})[0]
}

func plain(label string, overwriteBefore int) *SuggestItem {
	return suggestItem(label, overwriteBefore, 10, false, 1, "", "")
}

func defaultModelOptions() SuggestModelOptions {
	return SuggestModelOptions{FilterGraceful: true, MatchOnWordStartOnly: true, Snippets: SnippetsInline}
}

func labelsOf(m *SuggestModel) []string {
	var out []string
	for _, it := range m.Items() {
		out = append(out, it.Label)
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSuggestModelFilteringIsCached(t *testing.T) {
	m := NewSuggestModel([]*SuggestItem{plain("foo", 3), plain("Foo", 3), plain("foo", 2)}, 0, LineContext{LeadingLine: "foo"}, defaultModelOptions())
	now := m.Items()
	if again := m.Items(); &now[0] != &again[0] {
		t.Error("the items are made again for the same context")
	}
	m.SetLineContext(LineContext{LeadingLine: "foo"})
	if again := m.Items(); &now[0] != &again[0] {
		t.Error("the same context filters again")
	}
	m.SetLineContext(LineContext{LeadingLine: "foo1", Delta: 1})
	if again := m.Items(); len(again) > 0 && &now[0] == &again[0] {
		t.Error("a different context did not filter again")
	}
}

func TestSuggestModelIncompleteOwners(t *testing.T) {
	m := NewSuggestModel([]*SuggestItem{plain("foo", 3), plain("foo", 2)}, 0, LineContext{LeadingLine: "foo"}, defaultModelOptions())
	if n := len(m.IncompleteOwners()); n != 0 {
		t.Errorf("%d incomplete owners in a complete model", n)
	}
	m = NewSuggestModel([]*SuggestItem{suggestItem("foo", 3, 10, true, 1, "", ""), plain("foo", 2)}, 0, LineContext{LeadingLine: "foo"}, defaultModelOptions())
	if n := len(m.IncompleteOwners()); n != 1 {
		t.Errorf("%d incomplete owners, want 1", n)
	}
	// Fuzzy matching of snippets stopped working with inline snippet suggestions #49895
	var items []*SuggestItem
	for i := 1; i <= 5; i++ {
		items = append(items, suggestItem("foobar"+strconv.Itoa(i), 1, 10, false, 2, "", ""))
	}
	items = append(items, suggestItem("foofoo1", 1, 10, true, 2, "", ""))
	m = NewSuggestModel(items, 1, LineContext{LeadingLine: "f"}, defaultModelOptions())
	if len(m.IncompleteOwners()) != 1 || len(m.Items()) != 6 {
		t.Errorf("%d incomplete owners and %d items, want 1 and 6", len(m.IncompleteOwners()), len(m.Items()))
	}
}

func TestSuggestModelProperCurrentWordWhenLengthIsZero(t *testing.T) { // #16380
	m := NewSuggestModel([]*SuggestItem{plain("    </div", 4), plain("a", 0), plain("p", 0), plain("    </tag", 4), plain("    XYZ", 4)},
		0, LineContext{LeadingLine: "   <"}, defaultModelOptions())
	if got, want := labelsOf(m), []string{"    </div", "    </tag", "a", "p"}; !equalStrings(got, want) {
		t.Errorf("items %q, want %q", got, want)
	}
}

func TestSuggestModelKeepsSnippetSortingWithPrefix(t *testing.T) { // #25495
	tests := []struct {
		name  string
		order SnippetOrder
		items []*SuggestItem
		want  []string
		// firstScoreLower: the first item's score is lower than the second's, so that only the snippet order put it first
		firstScoreLower bool
	}{
		{"top", SnippetsTop, []*SuggestItem{
			suggestItem("Snippet1", 1, SnippetKind, false, 1, "", ""), suggestItem("tnippet2", 1, SnippetKind, false, 1, "", ""),
			suggestItem("semver", 1, 10, false, 1, "", "")}, []string{"Snippet1", "semver"}, true},
		{"bottom", SnippetsBottom, []*SuggestItem{
			suggestItem("snippet1", 1, SnippetKind, false, 1, "", ""), suggestItem("tnippet2", 1, SnippetKind, false, 1, "", ""),
			suggestItem("Semver", 1, 10, false, 1, "", "")}, []string{"Semver", "snippet1"}, true},
		{"inline", SnippetsInline, []*SuggestItem{
			suggestItem("snippet1", 1, SnippetKind, false, 1, "", ""), suggestItem("tnippet2", 1, SnippetKind, false, 1, "", ""),
			suggestItem("Semver", 1, 10, false, 1, "", "")}, []string{"snippet1", "Semver"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := defaultModelOptions()
			opts.Snippets = tt.order
			m := NewSuggestModel(tt.items, 0, LineContext{LeadingLine: "s"}, opts)
			got := m.Items()
			if len(got) != 2 || got[0].Label != tt.want[0] || got[1].Label != tt.want[1] {
				t.Fatalf("items %q, want %q", labelsOf(m), tt.want)
			}
			if lower := got[0].Score.Score < got[1].Score.Score; lower != tt.firstScoreLower && tt.order != SnippetsInline {
				t.Errorf("first score %d, second %d: the snippet was not promoted or demoted by the order", got[0].Score.Score, got[1].Score.Score)
			}
			if tt.order == SnippetsInline && got[0].Score.Score <= got[1].Score.Score {
				t.Errorf("inline: first score %d is not above the second %d", got[0].Score.Score, got[1].Score.Score)
			}
		})
	}
}

func TestSuggestModelFilterTextIsNotIgnored(t *testing.T) { // #26874
	item1 := suggestItem("Map - java.util", 1, 10, false, 1, "", "Map")
	item2 := suggestItem("Map - java.util", 1, 10, false, 1, "", "")
	m := NewSuggestModel([]*SuggestItem{item1, item2}, 0, LineContext{LeadingLine: "M"}, defaultModelOptions())
	if len(m.Items()) != 2 {
		t.Fatalf("%d items, want 2", len(m.Items()))
	}
	m.SetLineContext(LineContext{LeadingLine: "Map ", Delta: 3})
	if len(m.Items()) != 1 {
		t.Errorf("%d items after \"Map \", want 1: the filter text must match the word", len(m.Items()))
	}
}

func TestSuggestModelObeysSortText(t *testing.T) { // #26096
	item1 := suggestItem("<- groups", 2, 10, false, 3, "00002", "  groups")
	item2 := suggestItem("source", 0, 10, false, 3, "00001", "source")
	items := []*SuggestItem{item1, item2}
	SortSuggestItems(items, SnippetsInline)
	m := NewSuggestModel(items, 2, LineContext{LeadingLine: "  "}, defaultModelOptions())
	if got, want := labelsOf(m), []string{"source", "<- groups"}; !equalStrings(got, want) {
		t.Errorf("items %q, want %q", got, want)
	}
}

func TestSortSuggestItemsWithLabelDetails(t *testing.T) { // #153026
	zzz := suggestItem("ZZZ", 0, 24, false, 1, "", "")
	aaa := suggestItem("AAA", 0, 24, false, 1, "", "")
	iii := suggestItem("III", 0, 24, false, 1, "", "")
	items := []*SuggestItem{zzz, aaa, iii}
	SortSuggestItems(items, SnippetsInline)
	if items[0] != aaa || items[1] != iii || items[2] != zzz {
		t.Errorf("order %s %s %s", items[0].Label, items[1].Label, items[2].Label)
	}
}

func TestSortSuggestItemsUsesKindOfVSCodeLast(t *testing.T) {
	// the same sort text and label: the kind of VS Code's own numbering decides (method before function before text)
	text, fn, method := suggestItem("a", 0, 1, false, 1, "", ""), suggestItem("a", 0, 3, false, 1, "", ""), suggestItem("a", 0, 2, false, 1, "", "")
	items := []*SuggestItem{text, fn, method}
	SortSuggestItems(items, SnippetsInline)
	if items[0] != method || items[1] != fn || items[2] != text {
		t.Errorf("order %d %d %d", items[0].Kind, items[1].Kind, items[2].Kind)
	}
}

func TestSuggestModelScoresOnlyTheFilteredItemsWhenTypingMore(t *testing.T) {
	m := NewSuggestModel([]*SuggestItem{plain("console", 0), plain("co_new", 0), plain("bar", 0), plain("car", 0), plain("foo", 0)},
		0, LineContext{}, defaultModelOptions())
	steps := []struct {
		ctx  LineContext
		want int
	}{
		{LineContext{}, 5},
		// narrow down once
		{LineContext{LeadingLine: "c", Delta: 1}, 3},
		// the query gets longer: narrow down the narrowed set
		{LineContext{LeadingLine: "cn", Delta: 2}, 2},
		// the query gets shorter: filter everything
		{LineContext{}, 5},
	}
	for _, st := range steps {
		m.SetLineContext(st.ctx)
		if n := len(m.Items()); n != st.want {
			t.Errorf("context %+v: %d items, want %d", st.ctx, n, st.want)
		}
	}
}

func TestSuggestModelMatchingIsRelaxed(t *testing.T) { // #15419
	m := NewSuggestModel([]*SuggestItem{plain("result", 0), plain("replyToUser", 0), plain("randomLolut", 0), plain("car", 0), plain("foo", 0)},
		0, LineContext{}, defaultModelOptions())
	m.SetLineContext(LineContext{LeadingLine: "rlut", Delta: 4})
	if got, want := labelsOf(m), []string{"result", "replyToUser", "randomLolut"}; !equalStrings(got, want) {
		t.Errorf("items %q, want %q", got, want)
	}
}

func TestSuggestModelEmmetAtTheTop(t *testing.T) { // #39518
	m := NewSuggestModel([]*SuggestItem{plain("from", 0), plain("form", 0), plain("form:get", 0), plain("testForeignMeasure", 0), plain("fooRoom", 0)},
		0, LineContext{}, defaultModelOptions())
	m.SetLineContext(LineContext{LeadingLine: "form", Delta: 4})
	got := labelsOf(m)
	if len(got) != 5 || got[0] != "form" || got[1] != "form:get" || got[2] != "from" {
		t.Errorf("items %q", got)
	}
}

func TestSuggestModelGracefulFilteringIsAnOption(t *testing.T) {
	for _, graceful := range []bool{true, false} {
		opts := defaultModelOptions()
		opts.FilterGraceful = graceful
		m := NewSuggestModel([]*SuggestItem{plain("result", 0)}, 0, LineContext{}, opts)
		m.SetLineContext(LineContext{LeadingLine: "rlut", Delta: 4})
		if got := len(m.Items()) == 1; got != graceful {
			t.Errorf("graceful %v: rlut matches result = %v", graceful, got)
		}
	}
}

func TestSuggestModelMatchOnWordStartOnly(t *testing.T) {
	for _, startOnly := range []bool{true, false} {
		opts := defaultModelOptions()
		opts.MatchOnWordStartOnly = startOnly
		m := NewSuggestModel([]*SuggestItem{plain("description", 0)}, 0, LineContext{}, opts)
		m.SetLineContext(LineContext{LeadingLine: "c", Delta: 1})
		if got := len(m.Items()) == 1; got != !startOnly {
			t.Errorf("matchOnWordStartOnly %v: c matches description = %v", startOnly, got)
		}
	}
}

func TestNewSuggestItemsRangesAndValidation(t *testing.T) {
	doc := docOf("go", "/a.go", "fmt.Pri(x)\nsecond\n")
	req := SuggestRequest{Doc: doc, Pos: Position{Line: 0, Col: 7}, WordAt: wordsAt} // after Pri
	rg := func(l1, c1, l2, c2 int) *Range {
		return &Range{Start: Position{Line: l1, Col: c1}, End: Position{Line: l2, Col: c2}}
	}
	tests := []struct {
		name                         string
		item                         CompletionItem
		start, insertEnd, replaceEnd int
		invalid                      bool
		sortText                     string
	}{
		{"no range is the word", CompletionItem{Label: "Println"}, 4, 7, 7, false, "Println"},
		{"a plain edit", CompletionItem{Label: "Println", Insert: rg(0, 4, 0, 7)}, 4, 7, 7, false, "Println"},
		{"insert and replace", CompletionItem{Label: "Println", Insert: rg(0, 4, 0, 7), Replace: rg(0, 4, 0, 10)}, 4, 7, 10, false, "Println"},
		{"a sort text is kept", CompletionItem{Label: "Println", SortText: "01"}, 4, 7, 7, false, "01"},
		{"an empty label", CompletionItem{Label: ""}, 4, 7, 7, true, ""},
		{"an edit over two lines", CompletionItem{Label: "x", Insert: rg(0, 4, 1, 2)}, 4, 2, 2, true, "x"},
		{"an edit on another line", CompletionItem{Label: "x", Insert: rg(1, 0, 1, 2)}, 0, 2, 2, true, "x"},
		{"insert and replace that start apart", CompletionItem{Label: "x", Insert: rg(0, 4, 0, 7), Replace: rg(0, 5, 0, 7)}, 4, 7, 7, true, "x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewSuggestItems(req, []CompletionItem{tt.item}, ItemFilter{})[0]
			if got.EditStart != tt.start || got.InsertEnd != tt.insertEnd || got.ReplaceEnd != tt.replaceEnd || got.Invalid != tt.invalid || got.SortText != tt.sortText {
				t.Errorf("start %d insert end %d replace end %d invalid %v sort %q; want %d %d %d %v %q",
					got.EditStart, got.InsertEnd, got.ReplaceEnd, got.Invalid, got.SortText, tt.start, tt.insertEnd, tt.replaceEnd, tt.invalid, tt.sortText)
			}
		})
	}
}

func TestNewSuggestItemsFilters(t *testing.T) {
	doc := docOf("go", "/a.go", "x\n")
	req := SuggestRequest{Doc: doc, Pos: Position{}, WordAt: wordsAt}
	items := []CompletionItem{
		{Label: "m", Kind: 2}, {Label: "f", Kind: 3}, {Label: "old", Kind: 3, Deprecated: true}, {Label: "s", Kind: SnippetKind},
	}
	tests := []struct {
		name   string
		filter ItemFilter
		want   []string
	}{
		{"nothing hidden", ItemFilter{}, []string{"m", "f", "old", "s"}},
		{"a kind hidden", ItemFilter{HiddenKinds: map[CompletionKind]bool{2: true}}, []string{"f", "old", "s"}},
		{"snippets hidden", ItemFilter{HiddenKinds: map[CompletionKind]bool{SnippetKind: true}}, []string{"m", "f", "old"}},
		{"deprecated hidden", ItemFilter{HideDeprecated: true}, []string{"m", "f", "s"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			for _, it := range NewSuggestItems(req, items, tt.filter) {
				got = append(got, it.Label)
			}
			if !equalStrings(got, tt.want) {
				t.Errorf("items %q, want %q", got, tt.want)
			}
		})
	}
}

// ---- the memory of the accepted suggestions ----

func scored(label string, score int, preselect bool, kind CompletionKind) *SuggestItem {
	it := suggestItem(label, 0, kind, false, 1, "", "")
	it.Score.Score = score
	it.Preselect = preselect
	return it
}

func TestSuggestMemorySelect(t *testing.T) {
	items := func() []*SuggestItem {
		return []*SuggestItem{scored("Alpha", 10, false, 6), scored("Beta", 10, false, 6), scored("Gamma", 10, true, 6), scored("Delta", 5, true, 6)}
	}
	tests := []struct {
		name       string
		mode       MemoryMode
		memorize   func(m *SuggestMemory, items []*SuggestItem)
		word       string
		afterSpace bool
		want       int
	}{
		{"first: the preselected among the best", MemoryFirst, nil, "", false, 2},
		{"first: a preselected item with a worse score does not count", MemoryFirst, func(*SuggestMemory, []*SuggestItem) {}, "x", false, 2},
		{"recently used: nothing used, the preselected", MemoryRecentlyUsed, nil, "", false, 2},
		{"recently used: a preselected item wins (VS Code returns at it)", MemoryRecentlyUsed, func(m *SuggestMemory, it []*SuggestItem) {
			m.Memorize(MemoryRecentlyUsed, "go", "", it[1])
		}, "", false, 2},
		{"recently used: after white space it is the first mode", MemoryRecentlyUsed, func(m *SuggestMemory, it []*SuggestItem) {
			m.Memorize(MemoryRecentlyUsed, "go", "", it[1])
		}, "", true, 2},
		{"by prefix: the item accepted for the prefix", MemoryRecentlyUsedByPrefix, func(m *SuggestMemory, it []*SuggestItem) {
			m.Memorize(MemoryRecentlyUsedByPrefix, "go", "al", it[0])
		}, "al", false, 0},
		{"by prefix: the item accepted for a shorter prefix", MemoryRecentlyUsedByPrefix, func(m *SuggestMemory, it []*SuggestItem) {
			m.Memorize(MemoryRecentlyUsedByPrefix, "go", "al", it[0])
		}, "alp", false, 0},
		{"by prefix: another prefix", MemoryRecentlyUsedByPrefix, func(m *SuggestMemory, it []*SuggestItem) {
			m.Memorize(MemoryRecentlyUsedByPrefix, "go", "be", it[1])
		}, "al", false, 2},
		{"by prefix: no word", MemoryRecentlyUsedByPrefix, func(m *SuggestMemory, it []*SuggestItem) {
			m.Memorize(MemoryRecentlyUsedByPrefix, "go", "", it[1])
		}, "", false, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, its := NewSuggestMemory(), items()
			if tt.memorize != nil {
				tt.memorize(m, its)
			}
			if got := m.Select(tt.mode, "go", tt.word, tt.afterSpace, its); got != tt.want {
				t.Errorf("selected %d, want %d", got, tt.want)
			}
		})
	}
}

func TestSuggestMemoryRecentlyUsedWins(t *testing.T) {
	items := []*SuggestItem{scored("Alpha", 10, false, 6), scored("Beta", 10, false, 6), scored("Gamma", 10, false, 6), scored("Delta", 5, false, 6)}
	m := NewSuggestMemory()
	m.Memorize(MemoryRecentlyUsed, "go", "", items[1])
	m.Memorize(MemoryRecentlyUsed, "go", "", items[2])
	if got := m.Select(MemoryRecentlyUsed, "go", "", false, items); got != 2 {
		t.Errorf("selected %d, want 2 (the item used last)", got)
	}
	m.Memorize(MemoryRecentlyUsed, "go", "", items[1])
	if got := m.Select(MemoryRecentlyUsed, "go", "", false, items); got != 1 {
		t.Errorf("selected %d, want 1 (used last now)", got)
	}
	// an item that is not among the best matches does not count
	m.Memorize(MemoryRecentlyUsed, "go", "", items[3])
	if got := m.Select(MemoryRecentlyUsed, "go", "", false, items); got != 1 {
		t.Errorf("selected %d, want 1: Delta is not a best match", got)
	}
}

func TestSuggestMemoryIsPerLanguageAndKind(t *testing.T) {
	m := NewSuggestMemory()
	items := []*SuggestItem{scored("Alpha", 10, false, 6), scored("Beta", 10, false, 6)}
	m.Memorize(MemoryRecentlyUsed, "go", "", items[1])
	if got := m.Select(MemoryRecentlyUsed, "rust", "", false, items); got != 0 {
		t.Errorf("another language selected %d, want 0", got)
	}
	items[1].Kind = 3 // the same label of another kind is another suggestion
	if got := m.Select(MemoryRecentlyUsed, "go", "", false, items); got != 0 {
		t.Errorf("another kind selected %d, want 0", got)
	}
}

func TestSuggestMemoryForgetsTheOldest(t *testing.T) {
	m := NewSuggestMemory()
	for i := 0; i < 400; i++ {
		m.Memorize(MemoryRecentlyUsed, "go", "", scored("item"+strconv.Itoa(i), 10, false, 6))
	}
	first := []*SuggestItem{scored("zzz", 10, false, 6), scored("item0", 10, false, 6)}
	if got := m.Select(MemoryRecentlyUsed, "go", "", false, first); got != 0 {
		t.Errorf("the oldest item is still remembered: selected %d", got)
	}
	// the last ones are kept
	last := []*SuggestItem{scored("a", 10, false, 6), scored("item399", 10, false, 6)}
	if got := m.Select(MemoryRecentlyUsed, "go", "", false, last); got != 1 {
		t.Errorf("the last item used was forgotten: selected %d", got)
	}
}

func TestAfterSpace(t *testing.T) {
	tests := []struct {
		line string
		col  int
		want bool
	}{{"a b", 2, true}, {"a b", 3, false}, {"a b", 0, false}, {"a\tb", 2, true}, {"", 0, false}}
	for _, tt := range tests {
		if got := AfterSpace(tt.line, tt.col); got != tt.want {
			t.Errorf("AfterSpace(%q, %d) = %v", tt.line, tt.col, got)
		}
	}
}

var _ = buffer.Position{}
