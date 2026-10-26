package editor

import (
	"context"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/edit"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/core/snippet"
	"github.com/rm4n0s/emetos/internal/core/textmate"
)

// The suggest widget and its triggers (SPEC §9.12 "Completion", "As built (step 12.5)"): when the widget is asked for, what it
// is asked, how the answer is filtered while the user types, and which row it focuses. Accepting an item is in
// suggest_accept.go, the keyboard in suggest_commands.go and the drawing in suggest_widget.go.
//
// Ported from VS Code: src/vs/editor/contrib/suggest/browser/suggestModel.ts and suggestController.ts

// QuickSuggestions are the values of editor.quickSuggestions for the code outside strings and comments, the comments and the
// strings: "on", "inline", "off" or "offWhenInlineCompletions".
type QuickSuggestions struct{ Other, Comments, Strings string }

// ParseQuickSuggestions reads the value of editor.quickSuggestions: a boolean, a string for all three, or an object. A value
// that is not understood is VS Code's default.
//
// Ported from VS Code: src/vs/editor/common/config/editorOptions.ts (EditorQuickSuggestions.validate)
func ParseQuickSuggestions(v any) QuickSuggestions {
	def := QuickSuggestions{Other: "offWhenInlineCompletions", Comments: "off", Strings: "off"}
	one := func(v any, fallback string) string {
		switch x := v.(type) {
		case bool:
			if x {
				return "on"
			}
			return "off"
		case string:
			if slices.Contains([]string{"on", "inline", "off", "offWhenInlineCompletions"}, x) {
				return x
			}
		}
		return fallback
	}
	switch x := v.(type) {
	case bool, string:
		all := one(x, "")
		if all == "" {
			return def
		}
		return QuickSuggestions{all, all, all}
	case map[string]any:
		return QuickSuggestions{one(x["other"], def.Other), one(x["comments"], def.Comments), one(x["strings"], def.Strings)}
	}
	return def
}

// allOff tells whether no kind of text gets quick suggestions in the widget.
func (q QuickSuggestions) allOff() bool {
	off := func(v string) bool { return v == "off" || v == "inline" }
	return off(q.Other) && off(q.Comments) && off(q.Strings)
}

// SuggestOptions are the settings of the suggest widget.
type SuggestOptions struct {
	// QuickSuggestions is editor.quickSuggestions and QuickSuggestionsDelay editor.quickSuggestionsDelay (milliseconds).
	QuickSuggestions      QuickSuggestions
	QuickSuggestionsDelay int
	// SuggestOnTriggerCharacters is editor.suggestOnTriggerCharacters.
	SuggestOnTriggerCharacters bool
	// AcceptSuggestionOnEnter is editor.acceptSuggestionOnEnter ("on", "smart", "off") and AcceptSuggestionOnCommitCharacter
	// editor.acceptSuggestionOnCommitCharacter.
	AcceptSuggestionOnEnter           string
	AcceptSuggestionOnCommitCharacter bool
	// SuggestSelection is editor.suggestSelection: "first", "recentlyUsed" or "recentlyUsedByPrefix".
	SuggestSelection string
	// SnippetSuggestions is editor.snippetSuggestions: "top", "bottom", "inline" or "none".
	SnippetSuggestions string
	// WordBasedSuggestions is editor.wordBasedSuggestions; the window gives it to the provider of words.
	WordBasedSuggestions string
	// FontSize and LineHeight are editor.suggestFontSize and editor.suggestLineHeight; 0 is the editor's.
	FontSize, LineHeight int
	// InsertMode is editor.suggest.insertMode: "insert" or "replace".
	InsertMode string
	// FilterGraceful, LocalityBonus, SnippetsPreventQuickSuggestions, ShowIcons, ShowStatusBar, Preview, ShowInlineDetails,
	// ShowDeprecated and MatchOnWordStartOnly are the editor.suggest.* settings of those names; SelectionMode is
	// editor.suggest.selectionMode.
	FilterGraceful, LocalityBonus, SnippetsPreventQuickSuggestions bool
	ShowIcons, ShowStatusBar, Preview, ShowInlineDetails           bool
	ShowDeprecated, MatchOnWordStartOnly                           bool
	SelectionMode                                                  string
	// HiddenKinds are the kinds (the LSP numbers) that editor.suggest.show<Kind> turned off.
	HiddenKinds map[int]bool
}

// SuggestShared is what the editors of a window tab share about the suggest widget: the memory of the accepted suggestions and
// whether the details pane is open (VS Code keeps the second in the storage of the workspace, as "expandSuggestionDocs").
type SuggestShared struct {
	Memory         *langfeatures.SuggestMemory
	DetailsVisible bool
}

// NewSuggestShared makes the shared state of a window tab.
func NewSuggestShared() *SuggestShared {
	return &SuggestShared{Memory: langfeatures.NewSuggestMemory()}
}

// SuggestHost is what a window gives the editor to complete with.
type SuggestHost struct {
	// Ctx ends every request of the editor with the window tab.
	Ctx context.Context
	// Features is the registry of the completion providers (and the resolvers) of the tab.
	Features *langfeatures.Registry
	// Shared is the state the editors of the tab share; the editor makes one when nil.
	Shared *SuggestShared
	// Execute runs the command of an accepted item, on the UI thread: the command of a language server goes to it, the others
	// to the commands of the window. Without it a command is ignored.
	Execute func(ctx context.Context, cmd langfeatures.Command) error
	// Report shows a failure to the user (an item that cannot be inserted, a command that failed); without it failures are not
	// shown. Log writes the failure of a provider to the log: a provider that fails on a keystroke is not shown on every one.
	Report func(err error)
	Log    func(message string, err error)
	// Tokenize gives the tokens of a code block of a language, named as a fence names it ("go"); false when the language has no
	// grammar. The hover and the parameter hints use it (and the registry above) to color the code of their Markdown.
	Tokenize func(language, text string) ([][]extension.Token, bool)

	// The means of navigation (step 12.7).
	//
	// Symbols is the cache of the document symbols that the editors of the window share; an editor shows no symbols without it.
	Symbols *langfeatures.SymbolCache
	// Open shows a location in an editor of the window, as a jump of the navigation history (OpenLocation says how).
	Open func(loc langfeatures.Location, o OpenLocation)
	// Embedded makes the editor that a peek view shows a file in, without a tab: done is called on the UI thread with its view
	// model and the function that lets go of it (or the error). The view model has no navigation host of its own.
	Embedded func(ctx context.Context, uri document.URI, done func(vm *ViewModel, release func(), err error))
	// Texts reads the text of a document that is not this editor's, for the rows of the peek view.
	Texts langfeatures.TextSource
	// DirLabel is how the peek view writes the folder of a file: relative to the folder of the window, with the home folder as
	// "~"; without it the absolute folder.
	DirLabel func(dir string) string
	// JumpTo moves the cursor of an editor to a position as a navigation of the history (choosing a symbol of the outline or of
	// a picker); without it the cursor just moves.
	JumpTo func(vm *ViewModel, p buffer.Position)

	// The means of the changes the language features make (step 12.8).
	//
	// ApplyEdit makes a workspace edit (a code action's, a rename's) on the UI thread, with the cursors of the editors following
	// the text, and fails with the reason; the window applies it with langfeatures.Apply.
	ApplyEdit func(ctx context.Context, we *langfeatures.WorkspaceEdit) error
	// KeyLabel gives the label of the key of a command ("Ctrl+."), or empty when it has none: the tooltip of the light bulb and
	// the keys in the list of actions show them.
	KeyLabel func(command string) string
}

// SetSuggest gives the editor the means to complete (SPEC §9.12): without a host the editor has no suggest widget.
func (vm *ViewModel) SetSuggest(h *SuggestHost) {
	vm.suggestCancel()
	if h != nil && h.Shared == nil {
		h.Shared = NewSuggestShared()
	}
	vm.sg.host = h
	vm.symbolsHostChanged()
	vm.sel.NotifyListeners()
}

// SuggestState is what the suggest widget is showing.
type SuggestState int

// The states of the suggest widget.
const (
	// SuggestHidden: nothing shows.
	SuggestHidden SuggestState = iota
	// SuggestLoading: the request is slow and the widget says "Loading...".
	SuggestLoading
	// SuggestEmpty: a request that was asked for by the user has no items: "No suggestions."
	SuggestEmpty
	// SuggestOpen: the list shows.
	SuggestOpen
	// SuggestFrozen: the list shows and no longer follows what is typed (the user asked for it and what they typed since
	// matches nothing).
	SuggestFrozen
)

// triggerOptions are how a request was made.
type triggerOptions struct {
	auto       bool
	shy        bool
	retrigger  bool
	kind       langfeatures.CompletionTriggerKind
	character  string
	refilter   bool
	reuse      map[string][]*langfeatures.SuggestItem
	onlyOwners map[string]bool
}

// lineCtx is the text before the cursor of the line the cursor is on.
type lineCtx struct {
	line, col int
	leading   string
	// word is the word before the cursor (up to it) and wordStart its first column.
	word      string
	wordStart int
}

// suggestRequest is what a request was asked at.
type suggestRequest struct {
	lineCtx
	doc langfeatures.Document
}

// suggestSession is a run of the suggest widget, from the trigger to the acceptance or the cancel.
type suggestSession struct {
	trigger triggerOptions
	req     suggestRequest
	gen     int
	cancel  context.CancelFunc
	model   *langfeatures.SuggestModel
	// state, items (the rows), focus (-1 for none) and top (the first row in view).
	state SuggestState
	items []*langfeatures.SuggestItem
	focus int
	top   int
	// loading cancels the wait that shows "Loading...".
	loading func()
	// suffix follows the text right of the cursor (see lineSuffix).
	suffix lineSuffix
	// resolving is the item whose details are being asked for and cancelResolve ends the request.
	resolving     *langfeatures.SuggestItem
	cancelResolve context.CancelFunc
	noFocus       bool
	// commit is the item whose commit characters accept it when typed.
	commit *langfeatures.SuggestItem
}

// suggestState is the suggest widget of a view model.
type suggestState struct {
	host *SuggestHost
	// session is the run in progress, or nil.
	session *suggestSession
	gen     int
	// quick cancels the wait of the quick suggestions.
	quick func()
	// changed tells the widget that something it shows changed.
	changed *foundation.ChangeNotifier
	// rows is the number of rows the widget shows at once, as it last measured (0 before the first layout).
	rows int
	// pending is set while an accepted item waits for the details that bring its additional edits.
	pending *pendingEdits
	// runKind tells what the command that is about to run is, and typed the text it typed.
	runKind runKind
	typed   string
	// trace of the last accepted item, for the tests.
	lastAccepted *langfeatures.SuggestItem
}

// runKind classifies a command for the suggest widget: a command that edits the text as the keyboard does, one that moves the
// cursor, a command of the widget, or another.
type runKind int

const (
	runOther runKind = iota
	runEdit
	runCursor
	runSuggest
)

// runKindOf classifies a command by its id.
func runKindOf(id string) runKind {
	switch {
	case id == "":
		return runOther
	case id == "type" || id == "tab" || id == "outdent" || id == "lineBreakInsert" || strings.HasPrefix(id, "delete"):
		return runEdit
	case strings.HasPrefix(id, "cursor") && !strings.Contains(id, "Undo") && !strings.Contains(id, "Redo"):
		return runCursor
	case isSuggestCommand(id):
		return runSuggest
	}
	return runOther
}

func newSuggestState() suggestState { return suggestState{changed: foundation.NewChangeNotifier()} }

// AddSuggestListener calls fn when what the suggest widget shows changes.
func (vm *ViewModel) AddSuggestListener(fn foundation.VoidCallback) foundation.ListenerHandle {
	return vm.sg.changed.AddListener(fn)
}

// RemoveSuggestListener removes a listener of AddSuggestListener.
func (vm *ViewModel) RemoveSuggestListener(h foundation.ListenerHandle) {
	vm.sg.changed.RemoveListener(h)
}

// suggestChanged tells the widget and the context keys that the state of the widget changed.
func (vm *ViewModel) suggestChanged() {
	vm.sg.changed.NotifyListeners()
	vm.sel.NotifyListeners()
}

// SuggestHasProvider tells whether a completion provider matches the document (the key editorHasCompletionItemProvider).
func (vm *ViewModel) SuggestHasProvider() bool {
	h := vm.sg.host
	return h != nil && h.Features != nil && h.Features.HasCompletion(vm.langDocument())
}

// langDocument is the document as the providers are asked about it: a snapshot of its text at its version.
func (vm *ViewModel) langDocument() langfeatures.Document {
	return langfeatures.Document{URI: vm.doc.URI(), LanguageID: vm.doc.LanguageID(), Version: vm.doc.Version(), Text: vm.doc.Snapshot()}
}

// wordFunc finds words as editor.wordSeparators says.
func (vm *ViewModel) wordFunc() langfeatures.WordFunc {
	sep := vm.opts.WordSeparators
	if sep == "" {
		sep = edit.DefaultWordSeparators
	}
	wc := edit.NewWordClassifier(sep)
	return func(line string, col int) (int, int, bool) {
		w, ok := wc.WordAt(line, col)
		return w.Start, w.End, ok
	}
}

// currentLineCtx is the line context at the cursor.
func (vm *ViewModel) currentLineCtx() lineCtx {
	pos := vm.cursors.Primary().Position()
	line, _ := vm.doc.Snapshot().LineContent(pos.Line)
	col := min(pos.Col, len(line))
	c := lineCtx{line: pos.Line, col: col, leading: line[:col], wordStart: col}
	if s, e, ok := vm.wordFunc()(line, col); ok && s <= col && col <= e {
		c.wordStart, c.word = s, line[s:col]
	}
	return c
}

// shouldAutoTrigger tells whether the cursor is where typing asks for suggestions: at the end of a word that is not a number,
// or just after the first character of one.
//
// Ported from VS Code: src/vs/editor/contrib/suggest/browser/suggestModel.ts (LineContext.shouldAutoTrigger)
func (vm *ViewModel) shouldAutoTrigger() bool {
	pos := vm.cursors.Primary().Position()
	line, _ := vm.doc.Snapshot().LineContent(pos.Line)
	s, e, ok := vm.wordFunc()(line, min(pos.Col, len(line)))
	if !ok {
		return false
	}
	_, first := utf8.DecodeRuneInString(line[s:e])
	if e != pos.Col && s+first != pos.Col {
		return false // not at the end of the word, nor after its first character
	}
	return !isNumeric(line[s:e])
}

func isNumeric(w string) bool {
	if w == "" {
		return false
	}
	dot := false
	for i, r := range w {
		switch {
		case unicode.IsDigit(r):
		case r == '.' && !dot && i > 0:
			dot = true
		default:
			return false
		}
	}
	return true
}

// ---- running commands ----

// suggestPre is what is noted before a command runs.
type suggestPre struct {
	version int
	sels    []buffer.Selection
}

func (vm *ViewModel) suggestBefore() suggestPre {
	return suggestPre{version: vm.doc.Version(), sels: vm.cursors.Selections()}
}

// suggestAfter lets the widget see what a command did: the typing and the deleting that go on with a run of the widget (the
// text is filtered again) or start one (quick suggestions, trigger characters), the movement of the cursor with the keys that
// filters again, and everything else that ends the run.
//
// Ported from VS Code: src/vs/editor/contrib/suggest/browser/suggestModel.ts (_onCursorChange)
func (vm *ViewModel) suggestAfter(pre suggestPre, kind runKind, typed string) {
	sg := &vm.sg
	if sg.host == nil || kind == runSuggest {
		return
	}
	changed := vm.doc.Version() != pre.version
	now := vm.cursors.Selections()
	moved := !buffer.SelectionsEqual(now, pre.sels)
	if !changed && !moved {
		return
	}
	if len(now) == 0 || !now[0].IsEmpty() {
		vm.suggestCancel()
		return
	}
	switch {
	case kind == runEdit && changed:
		if sg.session == nil {
			if len(pre.sels) > 0 && (pre.sels[0].Range().ContainsRange(now[0].Range()) || pre.sels[0].End().IsBeforeOrEqual(now[0].Active)) {
				vm.suggestQuick() // the cursor moved to the right by typing
			}
		} else {
			vm.suggestRefilter()
		}
		if typed != "" && vm.sg.host != nil {
			vm.suggestTriggerCharacter()
		}
	case kind == runCursor && !changed:
		if sg.session != nil {
			vm.suggestRefilter()
		}
	case sg.session != nil:
		vm.suggestCancel()
	}
}

// suggestWillType lets the commit characters of the focused item accept it before a text is typed.
//
// Ported from VS Code: src/vs/editor/contrib/suggest/browser/suggestCommitCharacters.ts
func (vm *ViewModel) suggestWillType(text string) {
	s := vm.sg.session
	if s == nil || s.commit == nil || s.state != SuggestOpen || !vm.opts.Suggest.AcceptSuggestionOnCommitCharacter || text == "" {
		return
	}
	last, _ := utf8.DecodeLastRuneInString(text)
	for _, c := range s.commit.CommitCharacters {
		if c != "" {
			if first, _ := utf8.DecodeRuneInString(c); first == last {
				vm.suggestInsert(s.commit, insertOptions{noAfterUndoStop: true})
				return
			}
		}
	}
}

// suggestCancel ends the run of the widget and hides it.
func (vm *ViewModel) suggestCancel() {
	vm.cancelSuggest(false)
}

// suggestShutdown ends the run of an editor that goes away. The decoration that follows the text right of the cursor is removed
// later, by post: removing it changes the document, which tells the other editors of it, and the tree is not stable while an
// editor is disposed.
func (vm *ViewModel) suggestShutdown(post func(func())) {
	sg := &vm.sg
	if sg.quick != nil {
		sg.quick()
		sg.quick = nil
	}
	s := sg.session
	if s == nil {
		return
	}
	sg.session = nil
	for _, stop := range []func(){s.cancel, s.cancelResolve, s.loading} {
		if stop != nil {
			stop()
		}
	}
	post(func() { s.suffix.dispose(vm) })
}

// cancelSuggest ends the run; retrigger leaves the widget on screen, because a new request follows at once.
func (vm *ViewModel) cancelSuggest(retrigger bool) {
	sg := &vm.sg
	if sg.quick != nil {
		sg.quick()
		sg.quick = nil
	}
	s := sg.session
	if s == nil {
		return
	}
	sg.session = nil
	if s.cancel != nil {
		s.cancel()
	}
	if s.cancelResolve != nil {
		s.cancelResolve()
	}
	if s.loading != nil {
		s.loading()
	}
	s.suffix.dispose(vm)
	if !retrigger {
		vm.suggestChanged()
	}
}

// ---- triggers ----

// suggestQuick is the start of quick suggestions: after a delay, if the cursor is still at a word and the kind of text under
// it asks for suggestions, a request is made.
//
// Ported from VS Code: src/vs/editor/contrib/suggest/browser/suggestModel.ts (_doTriggerQuickSuggest)
func (vm *ViewModel) suggestQuick() {
	sg := &vm.sg
	o := vm.opts.Suggest
	if sg.host == nil || vm.opts.ReadOnly || o.QuickSuggestions.allOff() {
		return
	}
	if o.SnippetsPreventQuickSuggestions && vm.SnippetState().InSnippet {
		return
	}
	vm.suggestCancel()
	run := func() {
		sg.quick = nil
		if sg.session != nil || !vm.shouldAutoTrigger() || !vm.focused || vm.sg.host == nil {
			return
		}
		cfg := vm.opts.Suggest.QuickSuggestions
		if cfg.allOff() {
			return
		}
		switch vm.tokenTypeBeforeCursor() {
		case textmate.TokenComment:
			if cfg.Comments == "off" || cfg.Comments == "inline" {
				return
			}
		case textmate.TokenString:
			if cfg.Strings == "off" || cfg.Strings == "inline" {
				return
			}
		default:
			if cfg.Other == "off" || cfg.Other == "inline" {
				return
			}
		}
		if !vm.SuggestHasProvider() {
			return
		}
		vm.suggestTrigger(triggerOptions{auto: true})
	}
	if delay := o.QuickSuggestionsDelay; delay > 0 && vm.afterTimer != nil {
		sg.quick = vm.afterTimer(timeMillis(delay), run)
		return
	}
	run()
}

// tokenTypeBeforeCursor is the type of the token of the character before the cursor.
func (vm *ViewModel) tokenTypeBeforeCursor() textmate.TokenType {
	pos := vm.cursors.Primary().Position()
	toks := indentTokens{vm}.LineTokens(pos.Line)
	at := max(pos.Col-1, 0)
	for _, t := range toks {
		if int(t.End) > at {
			return t.Type
		}
	}
	return textmate.TokenOther
}

// suggestTriggerCharacter starts a request when the character just typed is a trigger character of a provider, unless the
// cursor is where quick suggestions take it.
func (vm *ViewModel) suggestTriggerCharacter() {
	sg := &vm.sg
	if !vm.opts.Suggest.SuggestOnTriggerCharacters || vm.opts.ReadOnly || vm.shouldAutoTrigger() {
		return
	}
	c := vm.currentLineCtx()
	if c.leading == "" {
		return
	}
	r, _ := utf8.DecodeLastRuneInString(c.leading)
	last := string(r)
	if sg.host.Features == nil || !slices.Contains(sg.host.Features.CompletionTriggers(vm.langDocument()), last) {
		return
	}
	vm.suggestTrigger(triggerOptions{auto: true, kind: langfeatures.TriggerCharacter, character: last, retrigger: sg.session != nil && sg.session.model != nil})
}

// suggestTrigger makes a request: the earlier one is cancelled, the providers are asked off the UI thread, and the answer is
// shown when it comes if the text it was asked for is still what is typed.
//
// Ported from VS Code: src/vs/editor/contrib/suggest/browser/suggestModel.ts (trigger)
func (vm *ViewModel) suggestTrigger(t triggerOptions) {
	sg := &vm.sg
	h := sg.host
	if h == nil || h.Features == nil {
		return
	}
	vm.cancelSuggest(t.retrigger)
	sg.gen++
	c := vm.currentLineCtx()
	doc := vm.langDocument()
	ctx := h.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)
	s := &suggestSession{trigger: t, req: suggestRequest{lineCtx: c, doc: doc}, gen: sg.gen, cancel: cancel, focus: -1}
	if t.retrigger && sg.session == nil {
		s.state = SuggestHidden
	}
	sg.session = s
	s.suffix = vm.newLineSuffix()
	vm.suggestShowTriggered(s)

	pos := vm.cursors.Primary().Position()
	trigger := langfeatures.CompletionTrigger{Kind: t.kind, Character: t.character}
	if t.kind == langfeatures.TriggerIncomplete {
		trigger.Kind = langfeatures.TriggerIncomplete
	}
	filter := vm.itemFilter()
	order := vm.snippetOrder()
	var snippets []langfeatures.CompletionItem
	if !t.hasReuse(langfeatures.SnippetOwner) && vm.opts.Suggest.SnippetSuggestions != "none" && vm.snip.service != nil && t.onlyOwners == nil {
		list := vm.snip.service.Snippets(snippet.Query{LanguageID: doc.LanguageID, Path: doc.URI.Path})
		line, _ := doc.Text.LineContent(pos.Line)
		snippets = langfeatures.SnippetItems(list, line, pos, vm.wordFunc(), trigger)
	}
	req := langfeatures.SuggestRequest{Doc: doc, Pos: pos, WordAt: vm.wordFunc()}
	gen := s.gen
	go func() {
		list, errs := h.Features.CompletionFor(ctx, doc, pos, trigger, langfeatures.CompletionQuery{Owners: t.onlyOwners})
		items := langfeatures.NewSuggestItems(req, append(list.Items, snippets...), filter)
		for _, reused := range t.reuse {
			items = append(items, reused...)
		}
		langfeatures.SortSuggestItems(items, order)
		vm.post(func() { vm.suggestResult(gen, items, errs) })
	}()
}

// hasReuse tells whether the items of the owner are kept from the earlier request.
func (t triggerOptions) hasReuse(owner string) bool { _, ok := t.reuse[owner]; return ok }

// itemFilter is the kind filter of the options.
func (vm *ViewModel) itemFilter() langfeatures.ItemFilter {
	o := vm.opts.Suggest
	f := langfeatures.ItemFilter{HideDeprecated: !o.ShowDeprecated}
	if len(o.HiddenKinds) > 0 || o.SnippetSuggestions == "none" {
		f.HiddenKinds = map[langfeatures.CompletionKind]bool{}
		for k := range o.HiddenKinds {
			f.HiddenKinds[langfeatures.CompletionKind(k)] = true
		}
		if o.SnippetSuggestions == "none" {
			f.HiddenKinds[langfeatures.SnippetKind] = true
		}
	}
	return f
}

func (vm *ViewModel) snippetOrder() langfeatures.SnippetOrder {
	switch vm.opts.Suggest.SnippetSuggestions {
	case "top":
		return langfeatures.SnippetsTop
	case "bottom":
		return langfeatures.SnippetsBottom
	}
	return langfeatures.SnippetsInline
}

func (vm *ViewModel) modelOptions() langfeatures.SuggestModelOptions {
	o := vm.opts.Suggest
	return langfeatures.SuggestModelOptions{FilterGraceful: o.FilterGraceful, MatchOnWordStartOnly: o.MatchOnWordStartOnly, Snippets: vm.snippetOrder()}
}

// loadingDelay is how long a request may take before the widget says "Loading...": 50 ms, 250 for a request made on the quiet.
const (
	loadingDelay    = 50
	loadingDelayShy = 250
)

// suggestShowTriggered makes the widget say "Loading..." when a request that the user asked for takes long.
func (vm *ViewModel) suggestShowTriggered(s *suggestSession) {
	if s.trigger.auto || vm.afterTimer == nil {
		return
	}
	delay := loadingDelay
	if s.trigger.shy {
		delay = loadingDelayShy
	}
	s.loading = vm.afterTimer(timeMillis(delay), func() {
		s.loading = nil
		if vm.sg.session == s && s.state == SuggestHidden {
			s.state = SuggestLoading
			vm.suggestChanged()
		}
	})
}

// suggestResult takes the answer of a request. An answer of a request that was cancelled or replaced is dropped.
func (vm *ViewModel) suggestResult(gen int, items []*langfeatures.SuggestItem, errs []langfeatures.ProviderError) {
	s := vm.sg.session
	if s == nil || s.gen != gen {
		return
	}
	for _, e := range errs {
		if vm.sg.host.Log != nil && !errors.Is(e.Err, context.Canceled) {
			vm.sg.host.Log("completion provider "+e.Owner+" failed", e.Err)
		}
	}
	cur := vm.currentLineCtx()
	// the text before the place of the request must still be what it was: an answer about text that changed means nothing
	if cur.line != s.req.line || (cur.col >= s.req.col && !strings.HasPrefix(cur.leading, s.req.leading)) {
		vm.suggestCancel()
		return
	}
	s.model = langfeatures.NewSuggestModel(items, s.req.col, langfeatures.LineContext{LeadingLine: cur.leading, Delta: cur.col - s.req.col}, vm.modelOptions())
	vm.suggestNewContext(cur)
}

// suggestRefilter filters the items again for the text now before the cursor.
func (vm *ViewModel) suggestRefilter() {
	if vm.sg.session == nil {
		return
	}
	vm.suggestNewContext(vm.currentLineCtx())
}

// suggestNewContext reacts to a change of the text before the cursor while a run is on.
//
// Ported from VS Code: src/vs/editor/contrib/suggest/browser/suggestModel.ts (_onNewContext)
func (vm *ViewModel) suggestNewContext(cur lineCtx) {
	s := vm.sg.session
	if s == nil {
		return
	}
	req := s.req
	switch {
	case cur.line != req.line:
		vm.suggestCancel() // e.g. Enter was pressed while the request was made
		return
	case langfeatures.LeadingWhitespace(cur.leading) != langfeatures.LeadingWhitespace(req.leading):
		vm.suggestCancel() // the word was outdented
		return
	case cur.col < req.col:
		// typed, then moved left: ask again if still on a word
		if cur.word != "" {
			vm.suggestTrigger(triggerOptions{auto: s.trigger.auto, retrigger: true})
		} else {
			vm.suggestCancel()
		}
		return
	case s.model == nil:
		return // not answered yet
	}
	model := s.model
	if cur.word != "" && cur.wordStart > req.wordStart {
		// started a new word while the widget shows: ask again, and keep the items we have
		if vm.shouldAutoTrigger() {
			vm.suggestTrigger(triggerOptions{auto: s.trigger.auto, retrigger: true, reuse: model.ItemsByOwner()})
		}
		return
	}
	if incomplete := model.IncompleteOwners(); cur.col > req.col && len(incomplete) > 0 && cur.word != "" {
		// typed more and a list is incomplete: ask the providers of it again, keep the others' items
		reuse := map[string][]*langfeatures.SuggestItem{}
		for owner, items := range model.ItemsByOwner() {
			if !incomplete[owner] {
				reuse[owner] = items
			}
		}
		vm.suggestTrigger(triggerOptions{auto: s.trigger.auto, retrigger: true, kind: langfeatures.TriggerIncomplete, reuse: reuse, onlyOwners: incomplete})
		return
	}
	old := model.LineContext()
	frozen := false
	model.SetLineContext(langfeatures.LineContext{LeadingLine: cur.leading, Delta: cur.col - req.col})
	if len(model.Items()) == 0 {
		if vm.shouldAutoTrigger() && req.col < cur.wordStart {
			vm.suggestTrigger(triggerOptions{auto: s.trigger.auto, retrigger: true}) // heading into a new word
			return
		}
		if s.trigger.auto {
			vm.suggestCancel() // nothing left
			return
		}
		// asked for by the user: keep what was shown, frozen, until they leave the word
		model.SetLineContext(old)
		frozen = len(model.Items()) > 0
		if frozen && cur.word == "" {
			vm.suggestCancel()
			return
		}
	}
	vm.suggestShow(model, frozen)
}

// suggestShow puts the items of the model in the widget and focuses a row.
func (vm *ViewModel) suggestShow(model *langfeatures.SuggestModel, frozen bool) {
	s := vm.sg.session
	if s.loading != nil {
		s.loading()
		s.loading = nil
	}
	s.items = model.Items()
	switch {
	case frozen:
		s.state = SuggestFrozen
	case len(s.items) == 0:
		s.state = SuggestEmpty
	default:
		s.state = SuggestOpen
	}
	idx := 0
	if len(s.items) > 0 {
		idx = vm.selectItem(s.items)
	}
	s.noFocus = false
	if s.trigger.auto {
		switch vm.opts.Suggest.SelectionMode {
		case "never":
			s.noFocus = true
		case "whenTriggerCharacter":
			s.noFocus = s.trigger.kind != langfeatures.TriggerCharacter
		case "whenQuickSuggestion":
			s.noFocus = s.trigger.kind == langfeatures.TriggerCharacter && !s.trigger.refilter
		}
	}
	s.focus, s.top = -1, 0
	if len(s.items) > 0 && !s.noFocus {
		vm.suggestFocus(idx)
	} else {
		s.commit = nil
		vm.cancelResolveOf(s)
	}
	vm.suggestChanged()
}

// selectItem is the row the widget focuses at first: the memory of the accepted suggestions says.
func (vm *ViewModel) selectItem(items []*langfeatures.SuggestItem) int {
	h := vm.sg.host
	mode := langfeatures.MemoryMode(vm.opts.Suggest.SuggestSelection)
	if mode == "" {
		mode = langfeatures.MemoryFirst
	}
	c := vm.currentLineCtx()
	return h.Shared.Memory.Select(mode, vm.doc.LanguageID(), c.word, langfeatures.AfterSpace(c.leading, c.col), items)
}

// timeMillis is d milliseconds.
func timeMillis(d int) time.Duration { return time.Duration(d) * time.Millisecond }

// hasFeature tells whether a provider offers a feature for the document (the keys editorHasDocumentSymbolProvider and its kin).
func (vm *ViewModel) hasFeature(f langfeatures.Feature) bool {
	reg := vm.hoverFeatures()
	return reg != nil && reg.Has(vm.langDocument(), f)
}
