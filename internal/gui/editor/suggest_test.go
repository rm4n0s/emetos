package editor

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
)

// fakeCompletions is a completion provider the tests drive: the items it answers, the requests it got, and a gate that holds
// the answer until the test opens it.
type fakeCompletions struct {
	mu          sync.Mutex
	items       []langfeatures.CompletionItem
	incomplete  bool
	triggers    []string
	calls       []langfeatures.CompletionTrigger
	positions   []langfeatures.Position
	gate        chan struct{}
	resolvable  bool
	resolved    []string
	resolveFn   func(item langfeatures.CompletionItem) langfeatures.CompletionItem
	resolveGate chan struct{}
}

func (f *fakeCompletions) ProvideCompletion(ctx context.Context, _ langfeatures.Document, pos langfeatures.Position, trig langfeatures.CompletionTrigger) (langfeatures.CompletionList, error) {
	f.mu.Lock()
	f.calls = append(f.calls, trig)
	f.positions = append(f.positions, pos)
	gate, items, incomplete := f.gate, slices.Clone(f.items), f.incomplete
	f.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return langfeatures.CompletionList{}, ctx.Err()
		}
	}
	return langfeatures.CompletionList{Items: items, Incomplete: incomplete}, nil
}

func (f *fakeCompletions) CompletionTriggers() []string { return f.triggers }

func (f *fakeCompletions) CanResolve(langfeatures.CompletionItem) bool { return f.resolvable }

func (f *fakeCompletions) ResolveCompletion(ctx context.Context, _ langfeatures.Document, item langfeatures.CompletionItem) (langfeatures.CompletionItem, error) {
	f.mu.Lock()
	f.resolved = append(f.resolved, item.Label)
	fn, gate := f.resolveFn, f.resolveGate
	f.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return item, ctx.Err()
		}
	}
	if fn != nil {
		return fn(item), nil
	}
	return item, nil
}

func (f *fakeCompletions) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeCompletions) lastCall() langfeatures.CompletionTrigger {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[len(f.calls)-1]
}

func (f *fakeCompletions) resolvedLabels() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.resolved)
}

// suggestRig is a view model with a registry, a clock and a poster the test drives.
type suggestRig struct {
	t        *testing.T
	vm       *ViewModel
	clock    *fakeTimers
	fake     *fakeCompletions
	reg      *langfeatures.Registry
	posts    chan func()
	executed []langfeatures.Command
	reported []error
}

func newSuggestRig(t *testing.T, text string, tweak func(o *SuggestOptions)) *suggestRig {
	t.Helper()
	opts := goOptions()
	opts.CursorBlinking = "solid"
	opts.Suggest = SuggestOptions{
		QuickSuggestions: QuickSuggestions{Other: "on", Comments: "off", Strings: "off"}, QuickSuggestionsDelay: 10,
		SuggestOnTriggerCharacters: true, AcceptSuggestionOnEnter: "on", AcceptSuggestionOnCommitCharacter: true, SuggestSelection: "first",
		SnippetSuggestions: "inline", WordBasedSuggestions: "matchingDocuments", InsertMode: "insert", FilterGraceful: true,
		ShowIcons: true, ShowInlineDetails: true, ShowDeprecated: true, MatchOnWordStartOnly: true, SelectionMode: "always",
	}
	if tweak != nil {
		tweak(&opts.Suggest)
	}
	doc := newTestDoc(text)
	doc.SetLanguageID("go")
	vm := NewViewModel(doc, opts, nil)
	r := &suggestRig{t: t, vm: vm, clock: &fakeTimers{}, fake: &fakeCompletions{}, reg: langfeatures.NewRegistry(), posts: make(chan func(), 256)}
	vm.SetTimer(r.clock.after)
	vm.SetPoster(func(fn func()) { r.posts <- fn })
	r.reg.RegisterCompletion("fake", langfeatures.Selector{Languages: []string{"go"}}, r.fake)
	vm.SetSuggest(&SuggestHost{
		Ctx: context.Background(), Features: r.reg,
		Execute: func(_ context.Context, cmd langfeatures.Command) error {
			r.executed = append(r.executed, cmd)
			return nil
		},
		Report: func(err error) { r.reported = append(r.reported, err) },
	})
	t.Cleanup(vm.suggestCancel)
	return r
}

// settle runs the next function posted to the UI thread (the answer of a request), waiting for it.
func (r *suggestRig) settle() {
	r.t.Helper()
	select {
	case fn := <-r.posts:
		fn()
	case <-time.After(3 * time.Second):
		r.t.Fatal("nothing was posted to the UI thread")
	}
}

// waitFor runs what is posted to the UI thread until the condition holds.
func (r *suggestRig) waitFor(what string, cond func() bool) {
	r.t.Helper()
	deadline := time.After(3 * time.Second)
	for !cond() {
		select {
		case fn := <-r.posts:
			fn()
		case <-deadline:
			r.t.Fatalf("timed out waiting for %s", what)
		}
	}
}

// quiet runs what is posted until nothing more comes for a short while.
func (r *suggestRig) quiet() {
	for {
		select {
		case fn := <-r.posts:
			fn()
		case <-time.After(150 * time.Millisecond):
			return
		}
	}
}

// openExplicit types the text with the quick suggestions out of the way and asks for the widget as the user does.
func (r *suggestRig) openExplicit(s string) {
	r.t.Helper()
	r.typeText(s)
	r.run("editor.action.triggerSuggest")
	r.waitFor("the widget", r.vm.SuggestVisible)
}

// drain runs what is posted now, without waiting.
func (r *suggestRig) drain() {
	for {
		select {
		case fn := <-r.posts:
			fn()
		default:
			return
		}
	}
}

// typeText types the characters one by one, as the keyboard does.
func (r *suggestRig) typeText(s string) {
	r.t.Helper()
	for _, c := range s {
		if err := r.vm.RunCommand("type", string(c)); err != nil {
			r.t.Fatal(err)
		}
	}
}

// open types text and waits for the widget: the quick suggestions fire after their delay and the answer is awaited.
func (r *suggestRig) open(s string) {
	r.t.Helper()
	r.typeText(s)
	r.clock.advance(10 * time.Millisecond)
	r.settle()
	if !r.vm.SuggestVisible() {
		r.t.Fatalf("the widget does not show after typing %q", s)
	}
}

func (r *suggestRig) labels() []string {
	var out []string
	if v := r.vm.SuggestView(); v != nil {
		for _, it := range v.Items {
			out = append(out, it.Label)
		}
	}
	return out
}

func (r *suggestRig) focused() string {
	v := r.vm.SuggestView()
	if v == nil || v.Focus < 0 || v.Focus >= len(v.Items) {
		return ""
	}
	return v.Items[v.Focus].Label
}

func (r *suggestRig) run(id string) {
	r.t.Helper()
	if err := r.vm.RunCommand(id, nil); err != nil {
		r.t.Fatal(err)
	}
}

func item(label string) langfeatures.CompletionItem {
	return langfeatures.CompletionItem{Label: label, Kind: 3}
}

func printItems() []langfeatures.CompletionItem {
	return []langfeatures.CompletionItem{item("Println"), item("Printf"), item("Print"), item("Sprint")}
}

// ---- triggers ----

func TestQuickSuggestionsOpenTheWidgetAfterTheDelay(t *testing.T) {
	r := newSuggestRig(t, "fmt.", nil)
	r.fake.items = printItems()
	cursorAt(r.vm, 0, 4)
	r.typeText("P")
	r.clock.advance(9 * time.Millisecond)
	if r.fake.callCount() != 0 || r.vm.SuggestVisible() {
		t.Fatal("the request was made before the delay")
	}
	r.clock.advance(time.Millisecond)
	r.settle()
	if !r.vm.SuggestVisible() {
		t.Fatal("the widget does not show")
	}
	if got, want := r.labels(), []string{"Print", "Printf", "Println"}; !slices.Equal(got, want) {
		t.Errorf("items %v, want %v: the ones that match P, by the order of the request", got, want)
	}
	if call := r.fake.lastCall(); call.Kind != langfeatures.TriggerInvoked {
		t.Errorf("trigger %+v, want invoked", call)
	}
	if r.focused() != "Print" {
		t.Errorf("focused %q, want the first", r.focused())
	}
}

func TestQuickSuggestionsSettings(t *testing.T) {
	tests := []struct {
		name  string
		tweak func(o *SuggestOptions)
		text  string
		want  bool
	}{
		{"on", nil, "Pr", true},
		{"all off", func(o *SuggestOptions) {
			o.QuickSuggestions = QuickSuggestions{Other: "off", Comments: "off", Strings: "off"}
		}, "Pr", false},
		{"inline is not the widget", func(o *SuggestOptions) { o.QuickSuggestions.Other = "inline" }, "Pr", false},
		{"offWhenInlineCompletions shows without inline suggestions", func(o *SuggestOptions) { o.QuickSuggestions.Other = "offWhenInlineCompletions" }, "Pr", true},
		{"a number is not a word to complete", nil, "42", false},
		{"no word", nil, " ", false},
		{"the delay is a setting", func(o *SuggestOptions) { o.QuickSuggestionsDelay = 0 }, "Pr", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newSuggestRig(t, "", tt.tweak)
			r.fake.items = printItems()
			r.typeText(tt.text)
			r.clock.advance(time.Second)
			if tt.want {
				r.settle()
			}
			if got := r.vm.SuggestVisible(); got != tt.want {
				t.Errorf("visible = %v, want %v (requests %d)", got, tt.want, r.fake.callCount())
			}
		})
	}
}

func TestSnippetsPreventQuickSuggestions(t *testing.T) {
	r := newSuggestRig(t, "", func(o *SuggestOptions) { o.SnippetsPreventQuickSuggestions = true })
	r.fake.items = printItems()
	if err := r.vm.RunCommand("editor.action.insertSnippet", map[string]any{"snippet": "${1:a} ${2:b}"}); err != nil {
		t.Fatal(err)
	}
	r.typeText("P")
	r.clock.advance(time.Second)
	if r.fake.callCount() != 0 {
		t.Error("quick suggestions were asked for in a snippet")
	}
}

func TestTriggerCharacterOpensTheWidget(t *testing.T) {
	r := newSuggestRig(t, "fmt", func(o *SuggestOptions) {
		o.QuickSuggestions = QuickSuggestions{Other: "off", Comments: "off", Strings: "off"}
	})
	r.fake.items = printItems()
	r.fake.triggers = []string{"."}
	cursorAt(r.vm, 0, 3)
	r.typeText("a") // not a trigger character
	r.clock.advance(time.Second)
	if r.fake.callCount() != 0 {
		t.Fatal("a character that is no trigger asked for suggestions")
	}
	r.typeText(".")
	r.settle()
	if call := r.fake.lastCall(); call.Kind != langfeatures.TriggerCharacter || call.Character != "." {
		t.Errorf("trigger %+v, want the character .", call)
	}
	if !r.vm.SuggestVisible() || len(r.labels()) != 4 {
		t.Errorf("widget %v items %v", r.vm.SuggestVisible(), r.labels())
	}
	if pos := r.fake.positions[len(r.fake.positions)-1]; pos.Col != 5 {
		t.Errorf("asked at %+v, want after the dot", pos)
	}
}

func TestTriggerCharactersAreASetting(t *testing.T) {
	r := newSuggestRig(t, "fmt", func(o *SuggestOptions) {
		o.QuickSuggestions = QuickSuggestions{Other: "off", Comments: "off", Strings: "off"}
		o.SuggestOnTriggerCharacters = false
	})
	r.fake.triggers = []string{"."}
	cursorAt(r.vm, 0, 3)
	r.typeText(".")
	r.clock.advance(time.Second)
	if r.fake.callCount() != 0 {
		t.Error("a trigger character asked although the setting is off")
	}
}

func TestTriggerSuggestCommand(t *testing.T) {
	r := newSuggestRig(t, "fmt.P", func(o *SuggestOptions) {
		o.QuickSuggestions = QuickSuggestions{Other: "off", Comments: "off", Strings: "off"}
	})
	r.fake.items = printItems()
	cursorAt(r.vm, 0, 5)
	r.run("editor.action.triggerSuggest")
	r.settle()
	if !r.vm.SuggestVisible() || r.focused() == "" {
		t.Fatalf("the widget does not show with a focused row: %v", r.labels())
	}
	if call := r.fake.lastCall(); call.Kind != langfeatures.TriggerInvoked {
		t.Errorf("trigger %+v", call)
	}
	k := r.vm.SuggestKeys()
	if !k.Visible || !k.HasFocused || !k.Multiple {
		t.Errorf("keys %+v", k)
	}
}

func TestSlowRequestShowsLoadingAfterAShortWhile(t *testing.T) {
	r := newSuggestRig(t, "fmt.", nil)
	r.fake.items = printItems()
	r.fake.gate = make(chan struct{})
	cursorAt(r.vm, 0, 4)
	r.run("editor.action.triggerSuggest")
	r.clock.advance(49 * time.Millisecond)
	if r.vm.SuggestVisible() {
		t.Fatal("Loading... shows before its delay")
	}
	r.clock.advance(time.Millisecond)
	if v := r.vm.SuggestView(); v == nil || v.State != SuggestLoading {
		t.Fatalf("view %+v, want Loading...", v)
	}
	// typing never waits for the server
	r.typeText("P")
	close(r.fake.gate)
	r.settle()
	if v := r.vm.SuggestView(); v == nil || v.State != SuggestOpen || len(v.Items) != 3 {
		t.Errorf("view %+v after the answer: the items that match P", v)
	}
}

func TestAutoTriggeredRequestShowsNothingWhileItWaits(t *testing.T) {
	r := newSuggestRig(t, "", nil)
	r.fake.gate = make(chan struct{})
	r.typeText("P")
	r.clock.advance(time.Second)
	if r.vm.SuggestView() != nil {
		t.Error("the widget shows Loading... for a request nobody asked for")
	}
	close(r.fake.gate)
}

func TestNoSuggestionsMessage(t *testing.T) {
	r := newSuggestRig(t, "", func(o *SuggestOptions) { o.WordBasedSuggestions = "off" })
	r.run("editor.action.triggerSuggest")
	r.settle()
	if v := r.vm.SuggestView(); v == nil || v.State != SuggestEmpty {
		t.Fatalf("view %+v, want No suggestions.", v)
	}
	// an empty answer to a request that was made by typing shows nothing
	r.vm.suggestCancel()
	r.typeText("x")
	r.clock.advance(time.Second)
	r.settle()
	if r.vm.SuggestView() != nil {
		t.Error("an automatic request with no items shows a message")
	}
}

func TestStaleAnswersAreDropped(t *testing.T) {
	t.Run("the cursor moved to another line", func(t *testing.T) {
		r := newSuggestRig(t, "fmt.\nnext", nil)
		r.fake.items = printItems()
		r.fake.gate = make(chan struct{})
		cursorAt(r.vm, 0, 4)
		r.run("editor.action.triggerSuggest")
		cursorAt(r.vm, 1, 2)
		r.run("cursorLeft")
		close(r.fake.gate)
		r.settle()
		if r.vm.SuggestVisible() {
			t.Error("an answer for another line shows")
		}
	})
	t.Run("the text before the request changed", func(t *testing.T) {
		r := newSuggestRig(t, "fmt.Pr", nil)
		r.fake.items = printItems()
		r.fake.gate = make(chan struct{})
		cursorAt(r.vm, 0, 6)
		r.run("editor.action.triggerSuggest")
		// the text before the request is replaced, as by a paste or an undo, and the cursor stays at its end
		if _, err := r.vm.Document().Edit([]document.Edit{{Range: buffer.Range{Start: positionOf(0, 0), End: positionOf(0, 3)}, Text: "xyz"}}, document.EditOptions{}); err != nil {
			t.Fatal(err)
		}
		close(r.fake.gate)
		r.settle()
		if r.vm.SuggestVisible() {
			t.Error("an answer about text that changed shows")
		}
	})
	t.Run("a newer request replaces the older", func(t *testing.T) {
		r := newSuggestRig(t, "fmt.", nil)
		r.fake.items = printItems()
		r.fake.gate = make(chan struct{})
		cursorAt(r.vm, 0, 4)
		r.run("editor.action.triggerSuggest")
		r.run("editor.action.triggerSuggest")
		close(r.fake.gate)
		// the first request was cancelled: its answer is an error or is dropped by its generation
		r.waitFor("the newer answer", r.vm.SuggestVisible)
	})
}

func TestIncompleteListsAreAskedAgain(t *testing.T) {
	r := newSuggestRig(t, "", nil)
	other := &fakeCompletions{items: []langfeatures.CompletionItem{item("Pxx")}}
	r.reg.RegisterCompletion("other", langfeatures.Selector{Languages: []string{"go"}}, other)
	r.fake.items = []langfeatures.CompletionItem{item("Pa"), item("Pb")}
	r.fake.incomplete = true
	r.open("P")
	if r.fake.callCount() != 1 || other.callCount() != 1 {
		t.Fatalf("requests %d and %d, want 1 each", r.fake.callCount(), other.callCount())
	}
	r.fake.mu.Lock()
	r.fake.items = []langfeatures.CompletionItem{item("Pac"), item("Pad")}
	r.fake.mu.Unlock()
	r.typeText("a")
	r.settle()
	if call := r.fake.lastCall(); call.Kind != langfeatures.TriggerIncomplete {
		t.Errorf("trigger %+v, want the repetition of an incomplete list", call)
	}
	if other.callCount() != 1 {
		t.Errorf("the provider whose list was complete was asked again (%d requests)", other.callCount())
	}
	// the items of the complete provider are kept: Pxx does not match Pa, the new ones do
	if got := r.labels(); !slices.Equal(got, []string{"Pac", "Pad"}) {
		t.Errorf("items %v", got)
	}
}

func TestRefilterWhileTypingAndCursorMovesEndTheRun(t *testing.T) {
	r := newSuggestRig(t, "", nil)
	r.fake.items = printItems()
	r.open("P")
	if got := r.labels(); len(got) != 3 {
		t.Fatalf("items %v", got)
	}
	r.typeText("rin")
	if got := r.labels(); !slices.Equal(got, []string{"Print", "Printf", "Println"}) {
		t.Errorf("items %v after Prin", got)
	}
	r.typeText("l")
	if got := r.labels(); !slices.Equal(got, []string{"Println"}) {
		t.Errorf("items %v after Print l", got)
	}
	if r.fake.callCount() != 1 {
		t.Errorf("%d requests: typing filters, it does not ask again", r.fake.callCount())
	}
	// backspace widens the filter again
	r.run("deleteLeft")
	if got := r.labels(); !slices.Equal(got, []string{"Print", "Printf", "Println"}) {
		t.Errorf("items %v after backspace", got)
	}
	// a character that matches nothing ends the run
	r.typeText("z")
	if r.vm.SuggestVisible() {
		t.Error("the widget stays with nothing that matches")
	}
}

func TestMovingTheCursorWithTheKeysFiltersAgainAndOtherCommandsEndTheRun(t *testing.T) {
	tests := []struct {
		name    string
		command string
		visible bool
		items   []string
	}{
		{"left inside the word", "cursorLeft", true, []string{"Print", "Printf", "Println"}},
		{"home leaves the word", "cursorHome", false, nil},
		{"select all", "editor.action.selectAll", false, nil},
		{"undo", "undo", false, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newSuggestRig(t, "", nil)
			r.fake.items = printItems()
			r.open("Prin")
			if tt.command == "cursorLeft" {
				// the cursor moves left: the widget asks again and keeps the word
				r.run(tt.command)
				r.settle()
			} else {
				r.run(tt.command)
				r.drain()
			}
			if got := r.vm.SuggestVisible(); got != tt.visible {
				t.Errorf("visible = %v, want %v", got, tt.visible)
			}
			if tt.visible {
				if got := r.labels(); !slices.Equal(got, tt.items) {
					t.Errorf("items %v, want %v", got, tt.items)
				}
			}
		})
	}
}

func TestEscapeAndLosingTheFocusHideTheWidget(t *testing.T) {
	r := newSuggestRig(t, "", nil)
	r.fake.items = printItems()
	r.open("P")
	r.run("hideSuggestWidget")
	if r.vm.SuggestVisible() {
		t.Error("Escape does not hide the widget")
	}
	r.open("r")
	r.vm.SetFocused(false)
	if r.vm.SuggestVisible() {
		t.Error("the widget stays when the editor loses the focus")
	}
}

func TestNavigationInTheList(t *testing.T) {
	r := newSuggestRig(t, "", nil)
	var items []langfeatures.CompletionItem
	for _, l := range []string{"Pa", "Pb", "Pc", "Pd", "Pe", "Pf", "Pg", "Ph", "Pi", "Pj", "Pk", "Pl", "Pm", "Pn"} {
		items = append(items, item(l))
	}
	r.fake.items = items
	r.open("P")
	if r.focused() != "Pa" {
		t.Fatalf("focused %q", r.focused())
	}
	steps := []struct {
		command string
		want    string
		top     int
	}{
		{"selectNextSuggestion", "Pb", 0},
		{"selectPrevSuggestion", "Pa", 0},
		{"selectPrevSuggestion", "Pn", 2}, // wraps around
		{"selectNextSuggestion", "Pa", 0},
		{"selectLastSuggestion", "Pn", 2},
		{"selectFirstSuggestion", "Pa", 0},
		{"selectNextPageSuggestion", "Pl", 0},
		{"selectNextPageSuggestion", "Pn", 2},
		{"selectPrevPageSuggestion", "Pc", 2},
	}
	for _, st := range steps {
		r.run(st.command)
		if r.focused() != st.want || r.vm.SuggestView().Top != st.top {
			t.Errorf("%s: focused %q top %d, want %q %d", st.command, r.focused(), r.vm.SuggestView().Top, st.want, st.top)
		}
	}
	r.vm.SuggestScroll(1)
	if r.vm.SuggestView().Top != 2 {
		t.Errorf("top %d after scrolling past the end", r.vm.SuggestView().Top)
	}
	r.vm.SuggestScroll(-5)
	if r.vm.SuggestView().Top != 0 {
		t.Errorf("top %d after scrolling to the start", r.vm.SuggestView().Top)
	}
}

// ---- selection ----

func TestSelectionOfTheFocusedRow(t *testing.T) {
	pre := item("Printf")
	pre.Preselect = true
	tests := []struct {
		name  string
		tweak func(o *SuggestOptions)
		prior func(r *suggestRig)
		want  string
	}{
		{"first", nil, nil, "Print"},
		{"recently used", func(o *SuggestOptions) { o.SuggestSelection = "recentlyUsed" }, func(r *suggestRig) {
			r.open("Prin")
			r.run("selectNextSuggestion") // Printf
			r.run("selectNextSuggestion") // Println
			r.run("acceptSelectedSuggestion")
		}, "Println"},
		{"recently used by prefix", func(o *SuggestOptions) { o.SuggestSelection = "recentlyUsedByPrefix" }, func(r *suggestRig) {
			r.open("Prin")
			r.run("selectLastSuggestion")
			r.run("acceptSelectedSuggestion")
		}, "Println"},
		{"never selects when typing", func(o *SuggestOptions) { o.SelectionMode = "never" }, nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newSuggestRig(t, "", tt.tweak)
			r.fake.items = printItems()
			if tt.prior != nil {
				tt.prior(r)
				r.vm.suggestCancel()
				r.vm.Document().PushStackElement()
				if _, err := r.vm.Document().Edit([]document.Edit{{Range: buffer.Range{Start: positionOf(0, 0), End: positionOf(0, 100)}}}, document.EditOptions{}); err != nil {
					t.Fatal(err)
				}
				cursorAt(r.vm, 0, 0)
			}
			r.open("Prin")
			if r.focused() != tt.want {
				t.Errorf("focused %q, want %q", r.focused(), tt.want)
			}
		})
	}
	t.Run("a preselected item among the best", func(t *testing.T) {
		r := newSuggestRig(t, "", nil)
		r.fake.items = []langfeatures.CompletionItem{item("Print"), pre, item("Println")}
		r.open("Pri")
		if r.focused() != "Printf" {
			t.Errorf("focused %q, want the preselected one", r.focused())
		}
	})
}

func TestSelectionModeByHowTheWidgetWasTriggered(t *testing.T) {
	tests := []struct {
		mode     string
		viaChar  bool
		explicit bool
		focused  bool
	}{
		{"always", false, false, true},
		{"never", false, false, false},
		{"never", false, true, true}, // asked for by the user: always selected
		{"whenTriggerCharacter", false, false, false},
		{"whenTriggerCharacter", true, false, true},
		{"whenQuickSuggestion", false, false, true},
		{"whenQuickSuggestion", true, false, false},
	}
	for _, tt := range tests {
		name := tt.mode
		if tt.viaChar {
			name += " by character"
		}
		if tt.explicit {
			name += " by the key"
		}
		t.Run(name, func(t *testing.T) {
			r := newSuggestRig(t, "fmt ", func(o *SuggestOptions) { o.SelectionMode = tt.mode })
			r.fake.items = printItems()
			r.fake.triggers = []string{"."}
			cursorAt(r.vm, 0, 4)
			switch {
			case tt.explicit:
				r.run("editor.action.triggerSuggest")
			case tt.viaChar:
				r.typeText(".")
			default:
				r.typeText("P")
				r.clock.advance(10 * time.Millisecond)
			}
			r.waitFor("the widget", r.vm.SuggestVisible)
			if got := r.focused() != ""; got != tt.focused {
				t.Errorf("a row is focused = %v, want %v", got, tt.focused)
			}
		})
	}
}

// ---- accepting ----

func TestAcceptReplacesTheWord(t *testing.T) {
	r := newSuggestRig(t, "fmt.", nil)
	r.fake.items = printItems()
	cursorAt(r.vm, 0, 4)
	r.open("Pri")
	r.run("selectNextSuggestion") // Printf
	r.run("acceptSelectedSuggestion")
	if got := text(r.vm); got != "fmt.Printf" {
		t.Errorf("text %q", got)
	}
	if r.vm.SuggestVisible() {
		t.Error("the widget stays after the acceptance")
	}
	if c := r.vm.Cursors().Primary().Position(); c.Col != 10 {
		t.Errorf("cursor %+v", c)
	}
}

func TestAcceptEditKinds(t *testing.T) {
	rg := func(l1, c1, l2, c2 int) *langfeatures.Range {
		return &langfeatures.Range{Start: positionOf(l1, c1), End: positionOf(l2, c2)}
	}
	// the text is "x.Pri|nt(y)" with the cursor after Pri
	tests := []struct {
		name  string
		mode  string
		item  langfeatures.CompletionItem
		alt   bool
		want  string
		wantC int
	}{
		{"the word before the cursor, insert mode: the rest of the word stays", "insert", langfeatures.CompletionItem{Label: "Println"}, false, "x.Printlnnt(y)", 9},
		{"replace mode takes the word", "replace", langfeatures.CompletionItem{Label: "Println"}, false, "x.Println(y)", 9},
		{"the other way with Shift+Enter", "insert", langfeatures.CompletionItem{Label: "Println"}, true, "x.Println(y)", 9},
		{"insert and replace ranges, insert", "insert", langfeatures.CompletionItem{Label: "Println", Insert: rg(0, 2, 0, 5), Replace: rg(0, 2, 0, 7)}, false, "x.Printlnnt(y)", 9},
		{"insert and replace ranges, replace", "replace", langfeatures.CompletionItem{Label: "Println", Insert: rg(0, 2, 0, 5), Replace: rg(0, 2, 0, 7)}, false, "x.Println(y)", 9},
		{"a plain edit range", "insert", langfeatures.CompletionItem{Label: "Println", Insert: rg(0, 2, 0, 5)}, false, "x.Printlnnt(y)", 9},
		{"a plain edit range past the cursor", "insert", langfeatures.CompletionItem{Label: "Println", Insert: rg(0, 2, 0, 7)}, false, "x.Println(y)", 9},
		{"insert text instead of the label", "insert", langfeatures.CompletionItem{Label: "Println(…)", InsertText: "Println"}, false, "x.Printlnnt(y)", 9},
		{"text with $ is plain text when it is not a snippet", "insert", langfeatures.CompletionItem{Label: "$x", InsertText: "${1:a}$0 and $"}, false, "x.${1:a}$0 and $nt(y)", 16},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newSuggestRig(t, "x.nt(y)", func(o *SuggestOptions) { o.InsertMode = tt.mode })
			it := tt.item
			it.FilterText = "Pri"
			r.fake.items = []langfeatures.CompletionItem{it}
			cursorAt(r.vm, 0, 2)
			r.openExplicit("Pri")
			if err := r.vm.RunCommand(map[bool]string{false: "acceptSelectedSuggestion", true: "acceptAlternativeSelectedSuggestion"}[tt.alt], nil); err != nil {
				t.Fatal(err)
			}
			if got := text(r.vm); got != tt.want {
				t.Errorf("text %q, want %q", got, tt.want)
			}
			if c := r.vm.Cursors().Primary().Position(); c.Col != tt.wantC {
				t.Errorf("cursor at %d, want %d", c.Col, tt.wantC)
			}
		})
	}
}

func TestAcceptSnippetItemStartsASession(t *testing.T) {
	r := newSuggestRig(t, "", nil)
	r.fake.items = []langfeatures.CompletionItem{{Label: "Printf", Kind: 3, InsertText: "Printf(${1:format}, ${2:args})$0", Snippet: true}}
	r.open("Pr")
	r.run("acceptSelectedSuggestion")
	if got := text(r.vm); got != "Printf(format, args)" {
		t.Fatalf("text %q", got)
	}
	if !r.vm.SnippetState().InSnippet || !r.vm.SnippetState().HasNext {
		t.Errorf("state %+v: the tab stops of the snippet are not active", r.vm.SnippetState())
	}
	if sels := r.vm.Cursors().Selections(); len(sels) != 1 || sels[0].Range().Start.Col != 7 || sels[0].Range().End.Col != 13 {
		t.Errorf("selection %+v, want the first placeholder", sels)
	}
}

func TestAcceptKeepsWhitespaceAsItIsWhenAsked(t *testing.T) {
	for _, keep := range []bool{false, true} {
		r := newSuggestRig(t, "\tx := \n", nil)
		it := langfeatures.CompletionItem{Label: "multi", Kind: 3, InsertText: "a\n    b", Snippet: true, KeepWhitespace: keep}
		r.fake.items = []langfeatures.CompletionItem{it}
		cursorAt(r.vm, 0, 6)
		r.open("m")
		r.run("acceptSelectedSuggestion")
		want := "\tx := a\n        b\n" // the indentation of the line (a tab, four spaces here) is added
		if keep {
			want = "\tx := a\n    b\n"
		}
		if got := text(r.vm); got != want {
			t.Errorf("keep=%v: text %q, want %q", keep, got, want)
		}
	}
}

func TestAcceptWithAdditionalEditsIsOneUndoStep(t *testing.T) {
	r := newSuggestRig(t, "package main\n\nfunc main() {\n\t\n}\n", nil)
	it := item("Println")
	it.AdditionalEdits = []langfeatures.TextEdit{{Range: langfeatures.Range{Start: positionOf(1, 0), End: positionOf(1, 0)}, Text: "import \"fmt\"\n\n"}}
	r.fake.items = []langfeatures.CompletionItem{it}
	cursorAt(r.vm, 3, 1)
	r.open("Pr")
	before := text(r.vm)
	r.run("acceptSelectedSuggestion")
	want := "package main\nimport \"fmt\"\n\n\nfunc main() {\n\tPrintln\n}\n"
	if got := text(r.vm); got != want {
		t.Fatalf("text %q, want %q", got, want)
	}
	if c := r.vm.Cursors().Primary().Position(); c.Line != 5 || c.Col != 8 {
		t.Errorf("cursor %+v: it moved with the text, to the end of the item", c)
	}
	doc := r.vm.Document()
	if _, err := doc.Undo(); err != nil {
		t.Fatal(err)
	}
	if got := text(r.vm); got != before {
		t.Errorf("after one undo %q, want the text before the acceptance %q", got, before)
	}
}

func TestAdditionalEditsAfterTheCursorShiftWithWhatWasTyped(t *testing.T) {
	r := newSuggestRig(t, "x.\n", nil)
	it := item("Println")
	// asked after the dot; the edit is on the same line after it, and two characters are typed before the answer is used
	it.AdditionalEdits = []langfeatures.TextEdit{{Range: langfeatures.Range{Start: positionOf(0, 2), End: positionOf(0, 2)}, Text: "!"}}
	r.fake.items = []langfeatures.CompletionItem{it}
	cursorAt(r.vm, 0, 2)
	r.run("editor.action.triggerSuggest")
	r.settle()
	r.typeText("Pr") // the widget filters; the cursor is two columns after the place of the request
	r.run("acceptSelectedSuggestion")
	if got := text(r.vm); got != "x.Println!\n" && got != "x.!Println\n" {
		t.Errorf("text %q", got)
	}
}

func TestAcceptResolvesAndMakesTheEditsOfAnItemNotResolvedYet(t *testing.T) {
	setup := func(text string, edit langfeatures.TextEdit) (*suggestRig, chan struct{}) {
		r := newSuggestRig(t, text, nil)
		gate := make(chan struct{})
		r.fake.resolvable = true
		r.fake.resolveGate = gate
		r.fake.resolveFn = func(it langfeatures.CompletionItem) langfeatures.CompletionItem {
			it.AdditionalEdits = []langfeatures.TextEdit{edit}
			return it
		}
		r.fake.items = []langfeatures.CompletionItem{item("Println")}
		cursorAt(r.vm, 0, 0)
		r.open("Pr")
		return r, gate
	}
	t.Run("the edits come in the undo step of the item", func(t *testing.T) {
		r, gate := setup("\n", langfeatures.TextEdit{Range: langfeatures.Range{Start: positionOf(0, 0), End: positionOf(0, 0)}, Text: "import \"fmt\"\n"})
		r.run("acceptSelectedSuggestion") // the item is not resolved yet: the acceptance asks for the edits
		if got := text(r.vm); got != "Println\n" {
			t.Fatalf("text %q: the item is inserted at once", got)
		}
		close(gate)
		r.waitFor("the edits", func() bool { return strings.HasPrefix(text(r.vm), "import") })
		if got := text(r.vm); got != "import \"fmt\"\nPrintln\n" {
			t.Fatalf("text %q", got)
		}
		if _, err := r.vm.Document().Undo(); err != nil {
			t.Fatal(err)
		}
		if got := text(r.vm); got != "Pr\n" {
			t.Errorf("one undo left %q, want the text before the acceptance", got)
		}
	})
	t.Run("they are not made when the text changed before them", func(t *testing.T) {
		r, gate := setup("\nabc", langfeatures.TextEdit{Range: langfeatures.Range{Start: positionOf(1, 3), End: positionOf(1, 3)}, Text: "!"})
		r.run("acceptSelectedSuggestion")
		// the user edits the start of the text before the answer comes
		if _, err := r.vm.Document().Edit([]document.Edit{{Range: buffer.Range{Start: positionOf(0, 0), End: positionOf(0, 0)}, Text: "ZZ"}}, document.EditOptions{}); err != nil {
			t.Fatal(err)
		}
		close(gate)
		r.quiet()
		if got := text(r.vm); strings.Contains(got, "!") {
			t.Errorf("an edit was made at a place the text moved from: %q", got)
		}
	})
}

func TestResolveOfTheFocusedRowOnly(t *testing.T) {
	r := newSuggestRig(t, "", nil)
	r.fake.resolvable = true
	r.fake.resolveFn = func(it langfeatures.CompletionItem) langfeatures.CompletionItem {
		it.Documentation = &langfeatures.Markup{Value: "docs of " + it.Label}
		return it
	}
	r.fake.items = []langfeatures.CompletionItem{item("Pa"), item("Pb"), item("Pc")}
	r.open("P")
	r.settle() // the answer of the resolve of the first row
	if got := r.fake.resolvedLabels(); !slices.Equal(got, []string{"Pa"}) {
		t.Fatalf("resolved %v, want only the focused row", got)
	}
	if v := r.vm.SuggestView(); v.Items[0].Documentation == nil || v.Items[0].Documentation.Value != "docs of Pa" || !v.Items[0].Resolved {
		t.Errorf("the item was not updated: %+v", v.Items[0].CompletionItem)
	}
	r.run("selectNextSuggestion")
	r.settle()
	r.run("selectPrevSuggestion") // already resolved: no second request
	if got := r.fake.resolvedLabels(); !slices.Equal(got, []string{"Pa", "Pb"}) {
		t.Errorf("resolved %v, want Pa and Pb once each", got)
	}
}

func TestMovingOnCancelsTheResolveOfTheRowLeft(t *testing.T) {
	r := newSuggestRig(t, "", nil)
	r.fake.resolvable = true
	r.fake.resolveGate = make(chan struct{})
	r.fake.resolveFn = func(it langfeatures.CompletionItem) langfeatures.CompletionItem {
		it.Detail = "late"
		return it
	}
	r.fake.items = []langfeatures.CompletionItem{item("Pa"), item("Pb")}
	r.open("P")
	r.run("selectNextSuggestion")
	close(r.fake.resolveGate)
	r.waitFor("the focused row to be resolved", func() bool { return r.vm.SuggestView().Items[1].Resolved })
	r.quiet() // the answer of the row that was left comes too, and is dropped
	v := r.vm.SuggestView()
	if v.Items[0].Resolved || v.Items[0].Detail == "late" {
		t.Error("the answer for the row that was left was used")
	}
	if !v.Items[1].Resolved || v.Items[1].Detail != "late" {
		t.Errorf("the focused row was not resolved: %+v", v.Items[1].CompletionItem)
	}
}

func TestCommitCharactersAcceptTheFocusedItem(t *testing.T) {
	tests := []struct {
		name  string
		typed string
		tweak func(o *SuggestOptions)
		want  string
	}{
		{"a commit character accepts and is typed", "(", nil, "Println("},
		{"another character does not", "x", nil, "Prinx"},
		{"the setting off", "(", func(o *SuggestOptions) { o.AcceptSuggestionOnCommitCharacter = false }, "Prin("},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newSuggestRig(t, "", tt.tweak)
			it := item("Println")
			it.CommitCharacters = []string{"(", "."}
			r.fake.items = []langfeatures.CompletionItem{it}
			r.open("Prin")
			r.typeText(tt.typed)
			if got := text(r.vm); got != tt.want {
				t.Errorf("text %q, want %q", got, tt.want)
			}
		})
	}
}

func TestItemCommandRunsAfterTheAcceptance(t *testing.T) {
	r := newSuggestRig(t, "", nil)
	it := item("Println")
	arg := json.RawMessage(`"x"`)
	it.Command = &langfeatures.Command{ID: "gopls.after", Owner: "srv", Arguments: []json.RawMessage{arg}}
	r.fake.items = []langfeatures.CompletionItem{it}
	r.open("Pr")
	r.run("acceptSelectedSuggestion")
	if len(r.executed) != 1 || r.executed[0].ID != "gopls.after" || r.executed[0].Owner != "srv" || string(r.executed[0].Arguments[0]) != `"x"` {
		t.Errorf("executed %+v", r.executed)
	}
}

func TestItemCommandThatTriggersSuggestOpensTheWidgetAgain(t *testing.T) {
	r := newSuggestRig(t, "", nil)
	it := item("xyz")
	it.InsertText = "xyz."
	it.Command = &langfeatures.Command{ID: "editor.action.triggerSuggest"}
	r.fake.items = []langfeatures.CompletionItem{it}
	r.open("x")
	calls := r.fake.callCount()
	r.run("acceptSelectedSuggestion")
	if len(r.executed) != 0 {
		t.Errorf("the command of the widget was run as a command: %+v", r.executed)
	}
	r.waitFor("the second request", func() bool { return r.fake.callCount() == calls+1 && r.vm.SuggestVisible() })
}

func TestAcceptInAReadOnlyEditorReportsAndChangesNothing(t *testing.T) {
	r := newSuggestRig(t, "", nil)
	r.fake.items = printItems()
	r.open("Pr")
	o := r.vm.Options()
	o.ReadOnly = true
	r.vm.SetOptions(o)
	r.vm.SetTimer(r.clock.after)
	before := text(r.vm)
	r.run("acceptSelectedSuggestion")
	if text(r.vm) != before || len(r.reported) != 1 {
		t.Errorf("text %q, reported %v", text(r.vm), r.reported)
	}
}

func TestAcceptSuggestionOnEnterSmart(t *testing.T) {
	tests := []struct {
		name       string
		setting    string
		text       string
		wantKey    bool
		wantAccept bool
	}{
		{"on", "on", "Print", true, true},
		{"off", "off", "Print", false, false},
		{"smart with a change", "smart", "Prin", true, true},
		{"smart without a change", "smart", "Print", true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newSuggestRig(t, "", func(o *SuggestOptions) { o.AcceptSuggestionOnEnter = tt.setting })
			r.fake.items = []langfeatures.CompletionItem{item("Print")}
			r.open(tt.text)
			k := r.vm.SuggestKeys()
			if k.AcceptOnEnter != tt.wantKey {
				t.Errorf("acceptSuggestionOnEnter = %v, want %v", k.AcceptOnEnter, tt.wantKey)
			}
			if k.MakesTextEdit != tt.wantAccept && tt.setting == "smart" {
				t.Errorf("suggestionMakesTextEdit = %v, want %v", k.MakesTextEdit, tt.wantAccept)
			}
		})
	}
}

func TestClickingARowAcceptsIt(t *testing.T) {
	r := newSuggestRig(t, "", nil)
	r.fake.items = printItems()
	r.open("Pri")
	r.vm.SuggestAccept(2)
	if got := text(r.vm); got != "Println" {
		t.Errorf("text %q", got)
	}
}

func TestDetailsPane(t *testing.T) {
	r := newSuggestRig(t, "", nil)
	docs := item("Println")
	docs.Detail = "func(a ...any)"
	docs.Documentation = &langfeatures.Markup{Value: "Println formats.", Markdown: true}
	r.fake.items = []langfeatures.CompletionItem{item("Printf"), docs}
	r.open("Pri")
	if r.focused() != "Printf" {
		t.Fatalf("focused %q", r.focused())
	}
	r.run("toggleSuggestionDetails") // nothing to show for this row: the pane stays closed
	if r.vm.SuggestView().Details != nil || r.vm.SuggestKeys().DetailsVisible {
		t.Error("the pane opened for an item with nothing to show")
	}
	r.run("selectNextSuggestion")
	if r.vm.SuggestView().Details != nil {
		t.Error("the details show before they were asked for")
	}
	r.run("toggleSuggestionDetails")
	v := r.vm.SuggestView()
	if v.Details == nil || v.Details.Detail != "func(a ...any)" || v.Details.Documentation.Value != "Println formats." {
		t.Fatalf("details %+v", v.Details)
	}
	if !r.vm.SuggestKeys().DetailsVisible {
		t.Error("suggestWidgetDetailsVisible is not set")
	}
	// a row with nothing to show has no pane, and the pane comes back with a row that has
	r.run("selectPrevSuggestion")
	if r.vm.SuggestView().Details != nil {
		t.Error("a pane for an item with nothing to show")
	}
	r.run("selectNextSuggestion")
	if r.vm.SuggestView().Details == nil {
		t.Error("the pane did not come back")
	}
	r.run("toggleSuggestionDetails")
	if r.vm.SuggestView().Details != nil {
		t.Error("the pane stays")
	}
}

// ---- words and snippets ----

func TestWordBasedSuggestionsWhenNoProviderAnswers(t *testing.T) {
	r := newSuggestRig(t, "alpha beta\n", nil)
	wp := &langfeatures.WordProvider{Mode: func(langfeatures.Document) string { return "currentDocument" }, WordAt: r.vm.wordFunc()}
	r.reg.RegisterCompletion(langfeatures.WordOwner, langfeatures.Selector{}, wp)
	cursorAt(r.vm, 1, 0)
	r.open("a")
	if got := r.labels(); !slices.Equal(got, []string{"alpha"}) {
		t.Errorf("words %v: the words that start with a, not the one being typed", got)
	}
	// with items from the provider of the language the words are not asked for
	r.vm.suggestCancel()
	r.fake.items = printItems()
	r.typeText("l")
	r.clock.advance(10 * time.Millisecond)
	r.settle()
	if got := r.labels(); slices.Contains(got, "alpha") {
		t.Errorf("words %v mixed with the items of the language", got)
	}
}

func TestSnippetsAreRows(t *testing.T) {
	svc := snippetService(t, map[string]string{
		"go.json": `{"for": {"prefix": "forr", "description": "loop", "body": ["for ${1:i} := range ${2:n} {", "\t$0", "}"]}}`,
	})
	tests := []struct {
		name  string
		order string
		want  []string
	}{
		{"inline", "inline", []string{"forr", "fork"}},
		{"top", "top", []string{"forr", "fork"}},
		{"bottom", "bottom", []string{"fork", "forr"}},
		{"none", "none", []string{"fork"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newSuggestRig(t, "", func(o *SuggestOptions) { o.SnippetSuggestions = tt.order })
			r.vm.SetSnippets(svc)
			r.fake.items = []langfeatures.CompletionItem{item("fork")}
			r.open("for")
			if got := r.labels(); !slices.Equal(got, tt.want) {
				t.Errorf("rows %v, want %v", got, tt.want)
			}
			if tt.order == "none" {
				return
			}
			for _, it := range r.vm.SuggestView().Items {
				if it.Label == "forr" && (it.Kind != langfeatures.SnippetKind || !it.Snippet) {
					t.Errorf("the snippet row is %+v", it.CompletionItem)
				}
			}
		})
	}
	t.Run("accepting a snippet row inserts the snippet", func(t *testing.T) {
		r := newSuggestRig(t, "", nil)
		r.vm.SetSnippets(svc)
		r.open("forr")
		r.run("acceptSelectedSuggestion")
		if got := text(r.vm); !strings.HasPrefix(got, "for i := range n {") {
			t.Errorf("text %q", got)
		}
		if !r.vm.SnippetState().InSnippet {
			t.Error("the snippet is not active")
		}
	})
}

func TestKindFilterAndDeprecated(t *testing.T) {
	r := newSuggestRig(t, "", func(o *SuggestOptions) {
		o.HiddenKinds = map[int]bool{2: true}
		o.ShowDeprecated = false
	})
	old := item("Old")
	old.Deprecated = true
	method := item("Method")
	method.Kind = 2
	r.fake.items = []langfeatures.CompletionItem{old, method, item("Keep")}
	r.run("editor.action.triggerSuggest")
	r.settle()
	if got := r.labels(); !slices.Equal(got, []string{"Keep"}) {
		t.Errorf("rows %v", got)
	}
}

func TestQuickSuggestionsParse(t *testing.T) {
	tests := []struct {
		in   any
		want QuickSuggestions
	}{
		{true, QuickSuggestions{"on", "on", "on"}},
		{false, QuickSuggestions{"off", "off", "off"}},
		{"inline", QuickSuggestions{"inline", "inline", "inline"}},
		{map[string]any{"other": "on", "comments": true}, QuickSuggestions{"on", "on", "off"}},
		{map[string]any{"strings": "bogus"}, QuickSuggestions{"offWhenInlineCompletions", "off", "off"}},
		{nil, QuickSuggestions{"offWhenInlineCompletions", "off", "off"}},
		{"bogus", QuickSuggestions{"offWhenInlineCompletions", "off", "off"}},
	}
	for _, tt := range tests {
		if got := ParseQuickSuggestions(tt.in); got != tt.want {
			t.Errorf("ParseQuickSuggestions(%v) = %+v, want %+v", tt.in, got, tt.want)
		}
	}
}

func TestSuggestNeedsAHost(t *testing.T) {
	vm := NewViewModel(newTestDoc(""), goOptions(), nil)
	if err := vm.RunCommand("type", "a"); err != nil {
		t.Fatal(err)
	}
	if err := vm.RunCommand("editor.action.triggerSuggest", nil); err != nil {
		t.Fatal(err)
	}
	if vm.SuggestVisible() || vm.SuggestHasProvider() {
		t.Error("an editor without a host completes")
	}
}
