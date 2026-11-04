package debug

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/debug"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// The Debug Console (SPEC §10.31 "The Debug Console", step 25.6): the panel view that shows what the sessions printed and evaluates
// what the user types. Console is its model: the session whose console is shown, the rows (the elements of the console flattened as the
// groups and objects that are open say), the filter, the input with its history and completions, and the position of the scroll. It is
// used on the UI thread only; the events of the service come on any goroutine and are handed over with Opts.Post.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/repl.ts (Repl), replViewer.ts and replFilter.ts

// ConsoleConfig is what the console reads of the debug.console.* settings.
type ConsoleConfig struct {
	// FontSize is debug.console.fontSize (14), FontFamily debug.console.fontFamily ("default": the monospace font of the desktop) and
	// LineHeight debug.console.lineHeight (0: 1.4 times the font size).
	FontSize   float64
	FontFamily string
	LineHeight float64
	// WordWrap is debug.console.wordWrap.
	WordWrap bool
	// HistorySuggestions is debug.console.historySuggestions and AcceptSuggestionOnEnter debug.console.acceptSuggestionOnEnter == "on".
	HistorySuggestions      bool
	AcceptSuggestionOnEnter bool
}

// DefaultConsoleConfig is VS Code's defaults of the settings.
var DefaultConsoleConfig = ConsoleConfig{FontSize: 14, FontFamily: "default", WordWrap: true, HistorySuggestions: true}

// ConsoleLineHeight is the height of a line in pixels: the setting, or 1.4 times the font size.
func (c ConsoleConfig) ConsoleLineHeight() float64 {
	if c.LineHeight > 0 {
		return c.LineHeight
	}
	return 1.4 * c.FontSize
}

// ConsoleOpts are what the console needs of its window. Everything is called on the UI thread, except Post, which any goroutine may
// call.
type ConsoleOpts struct {
	// Ctx ends the requests the console makes when it is done.
	Ctx context.Context
	// Service is the debug service of the workspace; the view of a client of a hosted workspace gets the host's.
	Service *debug.Service
	// Post runs a function on the UI thread, later.
	Post func(func())
	// Logger logs a request that failed; slog's default when nil.
	Logger *slog.Logger
	// Config gives the settings; DefaultConsoleConfig when nil.
	Config func() ConsoleConfig
	// Folder is the folder the relative paths of the output are of.
	Folder string
	// OpenFile shows a file at a line and a column (counted from 1; 0 for none), OpenURL an address.
	OpenFile func(path string, line, column int)
	OpenURL  func(url string)
	// OpenSource shows the source an output came from.
	OpenSource func(src debug.ReplSource)
	// Stat tells whether a path is a file (not a folder); os.Stat by default. It runs on a goroutine of its own.
	Stat func(path string) (isFile bool)
	// Home is the home folder `~` stands for in a path; empty for none.
	Home string
	// AfterFrame runs a function after the next frame.
	AfterFrame func(func())
	// CompletionDelay is how long the input waits after a key before it asks the adapter for completions (150 ms by default; tests
	// use less).
	CompletionDelay time.Duration
	// FilterDelay is how long the filter waits after a key before it matches the lines of a large console (300 ms, as VS Code's filter
	// boxes wait; tests use less). A console of up to FilterAsyncLines elements is filtered at once.
	FilterDelay time.Duration
}

// ConsoleRowKind says what a row of the console shows.
type ConsoleRowKind int

// The kinds of rows.
const (
	// ConsoleInput is a line the user typed, ConsoleOutput a line of output (with the object behind it, if any), ConsoleResult the result
	// of an evaluation, ConsoleValue an object or a value logged with no text, ConsoleGroup the title of a group.
	ConsoleInput ConsoleRowKind = iota + 1
	ConsoleOutput
	ConsoleResult
	ConsoleValue
	ConsoleGroup
	// ConsoleChild is a variable under an object, ConsoleLoading the "Loading..." that stands in place of its children while they are
	// read.
	ConsoleChild
	ConsoleLoading
)

// ConsoleRow is a row of the console.
type ConsoleRow struct {
	// Key identifies the row in the tree: the id of the element, and below an object the ids of the variables.
	Key   string
	Kind  ConsoleRowKind
	Depth int
	// Element is the element of the console the row is, or the one the object below the row belongs to.
	Element debug.ReplElement
	// Node is the object the row shows or opens: the expression of an output, a value or a result, or a variable of a child row.
	Node debug.Node
	// Expandable and Expanded say whether the row has a twistie and whether it is open.
	Expandable, Expanded bool
}

// kids are the children of an object, as far as they are read.
type kids struct {
	vars    []*debug.Variable
	loading bool
}

// Console is the model of the Debug Console view.
type Console struct {
	opts ConsoleOpts
	ctx  context.Context
	stop context.CancelFunc
	off  func()

	shown   *debug.Session
	ignored map[*debug.Session]bool

	// filter is the filter whose answers are in vis: the one the rows show. filterText is the text typed, which the rows catch up
	// with after the debounce and the match on a goroutine (filterGen counts the changes of the text).
	filter      *debug.ReplFilter
	vis         map[debug.ReplElement]bool
	filterGen   atomic.Uint64
	filterTimer *time.Timer
	filterText  string
	total       int
	filtered    int

	rows     []ConsoleRow
	version  uint64
	expanded map[string]bool
	autoDone map[string]bool
	kids     map[string]*kids
	dirty    atomic.Bool
	wantEnd  bool
	// atEnd tells whether the view is scrolled to the end of the rows; the body of the view sets it while it is on screen.
	atEnd     func() bool
	listeners []*func()

	input *kit.InputBoxController
	// history is the input's history; the model keeps the same values for the state of the window.
	history *kit.History
	filterC *kit.InputBoxController
	sugg    suggestions

	evalQ    chan func()
	evalOnce sync.Once

	pathMu sync.Mutex
	paths  map[string]pathState
}

type pathState int

const (
	pathUnknown pathState = iota
	pathPending
	pathFile
	pathNotFile
)

// NewConsole makes the console and follows the service.
func NewConsole(opts ConsoleOpts) *Console {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Ctx == nil {
		opts.Ctx = context.Background()
	}
	if opts.Config == nil {
		opts.Config = func() ConsoleConfig { return DefaultConsoleConfig }
	}
	if opts.Stat == nil {
		opts.Stat = func(path string) bool {
			st, err := os.Stat(path)
			return err == nil && !st.IsDir()
		}
	}
	if opts.FilterDelay == 0 {
		opts.FilterDelay = 300 * time.Millisecond
	}
	if opts.CompletionDelay == 0 {
		opts.CompletionDelay = 150 * time.Millisecond
	}
	c := &Console{
		opts: opts, ignored: map[*debug.Session]bool{}, filter: debug.NewReplFilter(""), vis: map[debug.ReplElement]bool{},
		expanded: map[string]bool{}, autoDone: map[string]bool{}, kids: map[string]*kids{}, paths: map[string]pathState{},
		history: kit.NewHistory(opts.Service.Model().ReplHistory()),
		input:   kit.NewInputBoxController(""), filterC: kit.NewInputBoxController(""),
		evalQ: make(chan func(), 64),
	}
	c.ctx, c.stop = context.WithCancel(opts.Ctx)
	c.sugg.c = c
	c.off = opts.Service.Subscribe(c.onEvent)
	c.selectSession()
	c.rebuild(true)
	return c
}

// Dispose stops following the service and cancels what the console asked for.
func (c *Console) Dispose() {
	c.off()
	c.stop()
	c.sugg.close()
	if c.filterTimer != nil {
		c.filterTimer.Stop()
	}
	c.input.Dispose()
	c.filterC.Dispose()
}

// OnChange calls fn after the rows, the filter, the session or the state of the input changed, and returns the function that stops it.
func (c *Console) OnChange(fn func()) func() {
	p := &fn
	c.listeners = append(c.listeners, p)
	return func() {
		if i := slices.Index(c.listeners, p); i >= 0 {
			c.listeners = slices.Delete(c.listeners, i, i+1)
		}
	}
}

func (c *Console) notify() {
	for _, f := range slices.Clone(c.listeners) {
		(*f)()
	}
}

func (c *Console) config() ConsoleConfig { return c.opts.Config() }

// onEvent hands the events of the service to the UI thread, one burst at a time. The events of the other views are not the console's.
func (c *Console) onEvent(ev debug.Event) {
	switch ev.Kind {
	case debug.EventRepl, debug.EventFocus, debug.EventSessions, debug.EventSessionEnded, debug.EventSessionStarted:
	default:
		return
	}
	if c.dirty.Swap(true) {
		return
	}
	c.opts.Post(func() {
		c.dirty.Store(false)
		c.Refresh()
	})
}

// Refresh reads the model of the service again, now: the session that is shown, and the rows.
func (c *Console) Refresh() {
	c.selectSession()
	c.rebuild(false)
	c.notify()
}

// ---- the session

// Session is the session whose console is shown, or nil.
func (c *Console) Session() *debug.Session { return c.shown }

// Sessions are the sessions that have a console of their own and are not hidden by Clear, in the order of the model.
func (c *Console) Sessions() []*debug.Session {
	var out []*debug.Session
	for _, s := range c.opts.Service.Model().Sessions(true) {
		if s.HasSeparateRepl() && !c.ignored[s] {
			out = append(out, s)
		}
	}
	return out
}

// Multiple tells whether there is more than one console to pick from (the context key multiSessionRepl).
func (c *Console) Multiple() bool { return len(c.Sessions()) > 1 }

// Select shows the console of a session.
func (c *Console) Select(s *debug.Session) {
	if s == nil {
		return
	}
	c.show(s.ConsoleRoot())
	c.rebuild(true)
	c.notify()
}

func (c *Console) show(s *debug.Session) {
	if s == c.shown {
		return
	}
	c.shown = s
	c.sugg.close()
}

// selectSession picks the console to show as VS Code's selectSession does: the focused session's, else the one shown while it
// lives, else the newest session that was not cleared away.
func (c *Console) selectSession() {
	m := c.opts.Service.Model()
	if f := m.Focus().Session; f != nil {
		delete(c.ignored, f.ConsoleRoot())
		c.show(f.ConsoleRoot())
		return
	}
	if c.shown != nil && !c.ignored[c.shown] && (c.shown.State() != debug.Inactive || len(m.Sessions(false)) == 0) {
		return
	}
	all := m.Sessions(true)
	// a session that lives is better than one that is over
	for _, active := range []bool{true, false} {
		for i := len(all) - 1; i >= 0; i-- {
			s := all[i]
			if s.HasSeparateRepl() && !c.ignored[s] && (!active || s.State() != debug.Inactive) {
				c.show(s)
				return
			}
		}
	}
}

// ReadOnly tells whether the input cannot be used: there is no session that lives to evaluate in.
func (c *Console) ReadOnly() bool { return c.shown == nil || c.shown.State() == debug.Inactive }

// ---- the rows

// Rows are the rows of the console.
func (c *Console) Rows() []ConsoleRow { return c.rows }

// Len is the number of rows.
func (c *Console) Len() int { return len(c.rows) }

// Counts are the number of top-level elements and how many of them the filter lets through.
func (c *Console) Counts() (total, shown int) { return c.total, c.filtered }

// Elements is the number of elements of the console that is shown, with no filter.
func (c *Console) elements() []debug.ReplElement {
	if c.shown == nil {
		return nil
	}
	return c.shown.Repl().Elements()
}

// rebuild reads the elements again and makes the rows. It keeps the end of the console in view when it was in view.
func (c *Console) rebuild(forceEnd bool) {
	els := c.elements()
	if forceEnd || c.atEnd == nil || c.atEnd() {
		c.wantEnd = true
	}
	c.pruneAnswers(els)
	c.rows = c.rows[:0]
	filtered := 0
	for _, e := range els {
		if c.walk(e, 0) {
			filtered++
		}
	}
	c.total, c.filtered = len(els), filtered
	if c.shown != nil {
		c.version = c.shown.Repl().Version()
	}
}

// walk adds the row of an element and the rows below it that are open; it reports whether the element shows.
func (c *Console) walk(e debug.ReplElement, depth int) bool {
	if !c.visible(e) {
		return false
	}
	row := ConsoleRow{Key: e.ID(), Depth: depth, Element: e}
	switch x := e.(type) {
	case *debug.ReplInput:
		row.Kind = ConsoleInput
	case *debug.ReplOutput:
		row.Kind, row.Node, row.Expandable = ConsoleOutput, x.Expression, x.HasChildren()
	case *debug.ReplVariable:
		row.Kind, row.Node, row.Expandable = ConsoleValue, x.Expression, x.HasChildren()
	case *debug.ReplResult:
		row.Kind, row.Node, row.Expandable = ConsoleResult, x.Expression, x.HasChildren()
	case *debug.ReplGroup:
		row.Kind, row.Expandable = ConsoleGroup, true
		if x.AutoExpand && !c.autoDone[row.Key] {
			c.autoDone[row.Key] = true
			c.expanded[row.Key] = true
		}
	}
	row.Expanded = row.Expandable && c.expanded[row.Key]
	c.rows = append(c.rows, row)
	if !row.Expanded {
		return true
	}
	if g, ok := e.(*debug.ReplGroup); ok {
		for _, child := range g.Children() {
			c.walk(child, depth+1)
		}
		return true
	}
	c.walkKids(row.Key, row.Node, depth+1, e)
	return true
}

// walkKids adds the rows of the children of an object, reading them first when that is not done.
func (c *Console) walkKids(key string, node debug.Node, depth int, owner debug.ReplElement) {
	k := c.kids[key]
	if k == nil {
		k = &kids{loading: true}
		c.kids[key] = k
		c.load(key, node)
	}
	if k.loading {
		c.rows = append(c.rows, ConsoleRow{Key: key + "#loading", Kind: ConsoleLoading, Depth: depth, Element: owner})
		return
	}
	seen := map[string]int{}
	for _, v := range k.vars {
		id := v.ID()
		n := seen[id]
		seen[id]++
		ck := key + "/" + id
		if n > 0 {
			ck += "#" + strconv.Itoa(n)
		}
		row := ConsoleRow{Key: ck, Kind: ConsoleChild, Depth: depth, Element: owner, Node: v, Expandable: v.HasChildren()}
		row.Expanded = row.Expandable && c.expanded[ck]
		c.rows = append(c.rows, row)
		if row.Expanded {
			c.walkKids(ck, v, depth+1, owner)
		}
	}
}

// load reads the children of an object on a goroutine of its own and hands them over.
func (c *Console) load(key string, node debug.Node) {
	ctx := c.ctx
	go func() {
		vars, err := node.Children(ctx)
		if err != nil {
			return // the console is gone
		}
		c.opts.Post(func() {
			if k := c.kids[key]; k != nil {
				k.vars, k.loading = vars, false
				c.rebuild(false)
				c.notify()
			}
		})
	}()
}

// Toggle opens or closes the row with the key; it reports whether the row can be toggled.
func (c *Console) Toggle(key string) bool {
	for _, r := range c.rows {
		if r.Key == key {
			if !r.Expandable {
				return false
			}
			c.expanded[key] = !r.Expanded
			c.rebuild(false)
			c.notify()
			return true
		}
	}
	return false
}

// CollapseAll closes every group and object.
func (c *Console) CollapseAll() {
	clear(c.expanded)
	c.rebuild(false)
	c.notify()
}

// Clear removes the elements of the console that is shown. A console of a session that is over is hidden by it, and the console of
// another session is shown (VS Code's clearRepl).
func (c *Console) Clear() {
	s := c.shown
	if s == nil {
		return
	}
	s.Repl().Clear()
	clear(c.kids)
	if s.State() == debug.Inactive {
		c.ignored[s] = true
		c.shown = nil
		c.selectSession()
	}
	c.rebuild(true)
	c.notify()
}

// ---- the filter

// FilterText is the text of the filter box.
func (c *Console) FilterText() string { return c.filterText }

// FilterController is the controller of the filter box, which the window keeps for as long as the console lives.
func (c *Console) FilterController() *kit.InputBoxController { return c.filterC }

// SetFilter changes the filter and shows the end of the console, as VS Code does.
//
// A small console is filtered at once. A large one (FilterAsyncLines elements or more) waits FilterDelay for the next key, then
// matches its lines on a goroutine and swaps the result in on the UI thread: the match of 100,000 lines takes half a second, which
// the UI thread must not spend. Every change of the text counts a generation, and a result of an older one is dropped, so a fast
// sequence of edits shows the last one only. Lines that arrive meanwhile are matched when the result is swapped in, and until then
// by the filter the rows show.
func (c *Console) SetFilter(text string) {
	if text == c.filterText {
		return
	}
	c.filterText = text
	gen := c.filterGen.Add(1)
	if c.filterTimer != nil {
		c.filterTimer.Stop()
		c.filterTimer = nil
	}
	f := debug.NewReplFilter(text)
	if f.Empty() || len(c.elements()) < FilterAsyncLines {
		c.applyFilter(f, nil)
		return
	}
	c.filterTimer = time.AfterFunc(c.opts.FilterDelay, func() {
		c.opts.Post(func() { c.startMatch(gen, f) })
	})
}

// FilterAsyncLines is the number of elements from which the filter matches on a goroutine, after a delay.
const FilterAsyncLines = 1000

// applyFilter makes the rows show a filter. answers are its answers for the elements (nil: they are asked for when the rows need them).
func (c *Console) applyFilter(f *debug.ReplFilter, answers map[debug.ReplElement]bool) {
	c.filter = f
	if answers == nil {
		answers = map[debug.ReplElement]bool{}
	}
	c.vis = answers
	c.rebuild(true)
	c.notify()
}

// startMatch matches the elements of the console against a filter on a goroutine (UI thread).
func (c *Console) startMatch(gen uint64, f *debug.ReplFilter) {
	if c.filterGen.Load() != gen || c.ctx.Err() != nil {
		return
	}
	els := c.elements()
	go func() {
		answers := make(map[debug.ReplElement]bool, len(els))
		var match func(e debug.ReplElement) bool
		match = func(e debug.ReplElement) bool {
			if len(answers)&1023 == 0 && (c.filterGen.Load() != gen || c.ctx.Err() != nil) {
				return false
			}
			answers[e] = f.Visible(e, true)
			if g, ok := e.(*debug.ReplGroup); ok {
				for _, child := range g.Children() {
					if !match(child) {
						return false
					}
				}
			}
			return true
		}
		for _, e := range els {
			if !match(e) {
				return // a newer text came
			}
		}
		c.opts.Post(func() {
			if c.filterGen.Load() == gen && c.ctx.Err() == nil {
				c.applyFilter(f, answers)
			}
		})
	}()
}

// visible tells whether an element shows. The answers of the filter are kept, so that the rows are made again without matching
// every line again; an element that has no answer yet (it arrived after the match) is matched now.
func (c *Console) visible(e debug.ReplElement) bool {
	if c.filter.Empty() {
		return true
	}
	if v, ok := c.vis[e]; ok {
		return v
	}
	v := c.filter.Visible(e, true)
	c.vis[e] = v
	return v
}

// pruneAnswers drops the answers of elements that are gone (cleared, or pushed out by debug.console.maximumLines).
func (c *Console) pruneAnswers(els []debug.ReplElement) {
	if len(c.vis) <= 2*len(els)+1024 {
		return
	}
	keep := make(map[debug.ReplElement]bool, len(els))
	var add func(e debug.ReplElement)
	add = func(e debug.ReplElement) {
		if v, ok := c.vis[e]; ok {
			keep[e] = v
		}
		if g, ok := e.(*debug.ReplGroup); ok {
			for _, child := range g.Children() {
				add(child)
			}
		}
	}
	for _, e := range els {
		add(e)
	}
	c.vis = keep
}

// ---- the end

// WantsEnd tells whether the view should scroll to the end of the console now (and forgets that it was asked).
func (c *Console) WantsEnd() bool {
	w := c.wantEnd
	c.wantEnd = false
	return w
}

// KeepEnd says that the view was at the end of the console before the rows changed, so that the end stays in view.
func (c *Console) KeepEnd(atEnd bool) {
	if atEnd {
		c.wantEnd = true
	}
}

// ---- text

// RowText is the text of a row, without colors: what Copy puts on the clipboard.
func RowText(r ConsoleRow) string {
	var text string
	switch {
	case r.Kind == ConsoleChild:
		text = nodeLine(r.Node)
	case r.Kind == ConsoleLoading:
		text = "Loading..."
	case r.Element != nil:
		text = r.Element.Text(false)
	}
	return kit.RemoveANSI(strings.TrimRight(text, " \t\r\n"))
}

// nodeLine is "name: value" of a variable, or the value alone of the rest.
func nodeLine(n debug.Node) string {
	if v, ok := n.(*debug.Variable); ok && v.Name != "" {
		return v.Name + ": " + v.Value()
	}
	return n.Value()
}

// VisibleText is the text of the rows that show, a line for each: Copy All (VS Code's getVisibleContent).
func (c *Console) VisibleText() string {
	var b strings.Builder
	for _, r := range c.rows {
		if r.Kind == ConsoleLoading {
			continue
		}
		b.WriteString(RowText(r))
		b.WriteByte('\n')
	}
	return b.String()
}

// ---- paths

// ResolveLink says whether a part of the output is a link that can be followed: an address, a relative path when there is a folder, a
// path of a file (found out once, on a goroutine of its own, while the text shows plain). The view rebuilds when it is known.
func (c *Console) ResolveLink(p kit.LinkPart) bool {
	switch p.Kind {
	case kit.LinkWeb:
		return true
	case kit.LinkPath:
		path := p.Path
		if strings.HasPrefix(path, "//") {
			return false // most likely the rest of an address
		}
		if strings.HasPrefix(path, ".") {
			return c.opts.Folder != ""
		}
		if strings.HasPrefix(path, "~") {
			if c.opts.Home == "" {
				return false
			}
			path = filepath.Join(c.opts.Home, path[1:])
		}
		return c.isFile(filepath.Clean(path))
	}
	return false
}

func (c *Console) isFile(path string) bool {
	c.pathMu.Lock()
	st := c.paths[path]
	if st == pathUnknown {
		c.paths[path] = pathPending
	}
	c.pathMu.Unlock()
	switch st {
	case pathFile:
		return true
	case pathUnknown:
		go func() {
			ok := c.opts.Stat(path)
			c.pathMu.Lock()
			c.paths[path] = pathNotFile
			if ok {
				c.paths[path] = pathFile
			}
			c.pathMu.Unlock()
			if ok {
				c.opts.Post(c.notify)
			}
		}()
	}
	return false
}

// OpenLink follows a link of the output.
func (c *Console) OpenLink(p kit.LinkPart) {
	switch p.Kind {
	case kit.LinkWeb:
		address, line, col := kit.WebLinkPosition(p.Value)
		if path, ok := strings.CutPrefix(address, "file://"); ok && c.opts.OpenFile != nil {
			c.opts.OpenFile(filepath.Clean(path), line, col)
			return
		}
		if c.opts.OpenURL != nil {
			c.opts.OpenURL(p.Value)
		}
	case kit.LinkPath:
		if c.opts.OpenFile == nil {
			return
		}
		path := p.Path
		switch {
		case strings.HasPrefix(path, "."):
			path = filepath.Join(c.opts.Folder, path)
		case strings.HasPrefix(path, "~"):
			path = filepath.Join(c.opts.Home, path[1:])
		}
		c.opts.OpenFile(filepath.Clean(path), p.Line, p.Column)
	}
}

// ---- evaluating

// Accept evaluates the text of the input in the focused frame, and clears the input. It does nothing when the console is read-only.
func (c *Console) Accept() {
	text := c.input.Value()
	if c.ReadOnly() || strings.TrimSpace(text) == "" {
		return
	}
	c.history.Add(text)
	c.Evaluate(text)
	c.sugg.close()
	c.input.SetValue("")
}

// Evaluate evaluates a text in the console that is shown, as if it was typed (Evaluate in Debug Console). The input is left alone.
func (c *Console) Evaluate(text string) {
	s := c.shown
	if s == nil || s.State() == debug.Inactive {
		return
	}
	c.opts.Service.Model().AddReplHistory(text)
	var frame *debug.StackFrame
	if f := c.opts.Service.Model().Focus(); f.Frame != nil && f.Frame.Thread.Session().ConsoleRoot() == s {
		frame = f.Frame
	}
	c.wantEnd = true
	c.evalOnce.Do(func() {
		go func() {
			for {
				select {
				case f := <-c.evalQ:
					f()
				case <-c.ctx.Done():
					return
				}
			}
		}()
	})
	// the evaluations run one after the other, so that their lines keep the order they were typed in
	select {
	case c.evalQ <- func() {
		s.AddReplExpression(c.ctx, frame, text)
		c.opts.Post(func() {
			c.rebuild(true)
			c.notify()
		})
	}:
	default:
		c.opts.Logger.Warn("the Debug Console has too many evaluations waiting", "expression", text)
	}
}

// Input is the controller of the input box.
func (c *Console) Input() *kit.InputBoxController { return c.input }

// History is the history of the input.
func (c *Console) History() *kit.History { return c.history }

// Paste appends a text to the input and moves the cursor to the end.
func (c *Console) Paste(text string) {
	if text == "" {
		return
	}
	c.input.SetValue(c.input.Value() + text)
	c.input.Select(len(c.input.Value()), len(c.input.Value()))
	c.input.Focus()
}

var _ widgets.BuildContext
