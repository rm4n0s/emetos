package quickdiffpeek

import (
	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/scm/quickdiff"
	"github.com/rm4n0s/emetos/internal/gui/editor"
)

// Model is what the peek reads of the changes of the document of its editor: the result of the model of the quick diff, with the text of
// the originals.
type Model interface {
	// Result is the providers and the changes, sorted by their place in the text.
	Result() quickdiff.Result
	// OnChange calls fn when the result changes and returns the function that stops it.
	OnChange(fn func()) (stop func())
	// OriginalText is the text the provider with the id was compared with; false when the model does not have it.
	OriginalText(providerID string) (string, bool)
}

// Body is the small diff editor of the peek: the original of a provider on one side and the document of the editor on the other, in one
// column (VS Code's EmbeddedDiffEditorWidget with renderSideBySide off).
type Body struct {
	// Widget is the diff editor, as wide and as tall as the zone leaves it.
	Widget widgets.Widget
	// Reveal scrolls the diff editor so that the change is in the middle of it.
	Reveal func(c quickdiff.Change)
	// Focus gives the keyboard to the editor of the modified text, and HasFocus tells whether it has it.
	Focus    func()
	HasFocus func() bool
	// Dispose lets go of the editors and the documents of the body.
	Dispose func()
}

// BodySpec is what the window makes a Body from.
type BodySpec struct {
	// ProviderID and Label are the provider's; Original is the URI of its original document and Text its text.
	ProviderID, Label, Original, Text string
	// Peeker is the controller; the editors of the body give it to their view models, so that Escape in the peek closes it.
	Peeker editor.QuickDiffPeeker
}

// Shown is the change the peek shows, with the changes of its provider.
type Shown struct {
	// ProviderID, Label and Original are those of the provider.
	ProviderID, Label, Original string
	// Change is the change, Index its place among Changes, the changes of the provider in the order of the text.
	Change  quickdiff.Change
	Index   int
	Changes []quickdiff.Change
	// Total is the number of changes of every provider (the title says "change" for one).
	Total int
}

// Action is a button of the title bar: an icon with a tooltip that runs a command of the menu of the peek.
type Action struct {
	// Icon is the Codicon (a fonts.Codicon... constant) and Tooltip the hover.
	Icon    int
	Tooltip string
	// Run is called when the button is pressed; the peek closes after it, as VS Code's does after an action that succeeded.
	Run func()
}

// Config configures a Controller.
type Config struct {
	// Editor is the editor of the file the peek is shown in.
	Editor *editor.ViewModel
	// Model is the model of the changes of the document of the editor.
	Model Model
	// Name is the title of the peek, the name of the file.
	Name func() string
	// NewBody makes the small diff editor of a provider's original. It is called when the peek opens and again when the original changes.
	NewBody func(BodySpec) (Body, error)
	// Actions are the buttons the menu of the peek puts in the title bar for the change, in the order they are shown.
	Actions func(Shown) []Action
	// FocusEditor gives the keyboard to the editor.
	FocusEditor func()
	// OnError is told about a body that could not be made.
	OnError func(error)
}

// Controller is the peek of the changes of one editor. It is used on the UI thread only.
type Controller struct {
	cfg  Config
	stop func()

	open bool
	// index is the change shown, among Result().Changes, and providerID its provider.
	index      int
	providerID string
	zone       editor.ZoneID
	body       *Body
	// bodyKey is what the body was made for: the provider and the text of its original.
	bodyProvider, bodyText string
	changed                *foundation.ChangeNotifier
}

var _ editor.QuickDiffPeeker = (*Controller)(nil)

// New makes the peek of an editor. It follows the model for as long as it lives; Dispose stops it.
func New(cfg Config) *Controller {
	c := &Controller{cfg: cfg, changed: foundation.NewChangeNotifier(), index: -1}
	c.stop = cfg.Model.OnChange(c.modelChanged)
	return c
}

// Dispose closes the peek and stops following the model.
func (c *Controller) Dispose() {
	c.cfg.FocusEditor = nil // the editor is going too: the keyboard is not for it
	c.Close()
	if c.stop != nil {
		c.stop()
		c.stop = nil
	}
}

// Visible implements editor.QuickDiffPeeker.
func (c *Controller) Visible() bool { return c.open }

// Index is the change shown among the changes of the model, -1 while the peek is closed.
func (c *Controller) Index() int {
	if !c.open {
		return -1
	}
	return c.index
}

// ProviderID is the provider of the change shown, empty while the peek is closed.
func (c *Controller) ProviderID() string {
	if !c.open {
		return ""
	}
	return c.providerID
}

// AddListener calls fn when the peek opens, moves, changes or closes.
func (c *Controller) AddListener(fn foundation.VoidCallback) foundation.ListenerHandle {
	return c.changed.AddListener(fn)
}

// RemoveListener removes a listener of AddListener.
func (c *Controller) RemoveListener(h foundation.ListenerHandle) { c.changed.RemoveListener(h) }

// Shown describes the change the peek shows; false while it is closed.
func (c *Controller) Shown() (Shown, bool) {
	if !c.open {
		return Shown{}, false
	}
	res := c.cfg.Model.Result()
	if c.index < 0 || c.index >= len(res.Changes) {
		return Shown{}, false
	}
	ch := res.Changes[c.index]
	s := Shown{ProviderID: ch.ProviderID, Change: ch.Change, Total: len(res.Changes)}
	for _, p := range res.Providers {
		if p.ID == ch.ProviderID {
			s.Label, s.Original = p.Label, p.Original
		}
	}
	for i, o := range res.Changes {
		if o.ProviderID != ch.ProviderID {
			continue
		}
		if i == c.index {
			s.Index = len(s.Changes)
		}
		s.Changes = append(s.Changes, o.Change)
	}
	return s, true
}

// Body is the small diff editor of the peek; nil while it is closed.
func (c *Controller) Body() *Body {
	if !c.open {
		return nil
	}
	return c.body
}

// Title is the name of the file the title bar shows.
func (c *Controller) Title() string {
	if c.cfg.Name == nil {
		return ""
	}
	return c.cfg.Name()
}

// Actions are the buttons of the title bar for the change shown.
func (c *Controller) Actions() []Action {
	s, ok := c.Shown()
	if !ok || c.cfg.Actions == nil {
		return nil
	}
	return c.cfg.Actions(s)
}

// providerIndexes are the indexes among the changes of those of the provider; all of them for an empty id.
func providerIndexes(changes []quickdiff.ProviderChange, providerID string) []int {
	var out []int
	for i, ch := range changes {
		if providerID == "" || ch.ProviderID == providerID {
			out = append(out, i)
		}
	}
	return out
}

// CanNavigate says that Step can move: the peek is closed (it opens on a change), or there is more than one change.
func (c *Controller) CanNavigate() bool {
	return !c.open || len(c.cfg.Model.Result().Changes) > 1
}

// Step implements editor.QuickDiffPeeker: the first step opens the peek at the first change that ends at or after the line of the cursor
// (before it, going back), the next ones go round the changes of the provider of the change shown.
//
// Ported from VS Code: quickDiffWidget.ts (QuickDiffEditorController.next, previous and assertWidget)
func (c *Controller) Step(next bool) {
	res := c.cfg.Model.Result()
	if len(res.Changes) == 0 {
		c.Close()
		return
	}
	if !c.CanNavigate() {
		return
	}
	if !c.open {
		c.at(res.Changes, c.cursorLine(), next, "")
		return
	}
	mine := providerIndexes(res.Changes, c.providerID)
	at := 0
	for i, idx := range mine {
		if idx == c.index {
			at = i
		}
	}
	if next {
		at = (at + 1) % len(mine)
	} else {
		at = (at - 1 + len(mine)) % len(mine)
	}
	c.show(mine[at], true)
}

// at shows the change nearest to a line among those of a provider (all with an empty id), going forward or back.
func (c *Controller) at(changes []quickdiff.ProviderChange, line int, next bool, providerID string) {
	mine := providerIndexes(changes, providerID)
	if len(mine) == 0 {
		return
	}
	list := make([]quickdiff.Change, len(mine))
	for i, idx := range mine {
		list[i] = changes[idx].Change
	}
	var at int
	if next {
		at = quickdiff.FindNext(list, line, true)
	} else {
		at = quickdiff.FindPrevious(list, line, true)
	}
	c.show(mine[at], true)
}

// Click implements editor.QuickDiffPeeker: the peek of the change at the line opens, or closes when it shows that change.
//
// Ported from VS Code: quickDiffWidget.ts (QuickDiffEditorController.onEditorMouseUp)
func (c *Controller) Click(line int) {
	res := c.cfg.Model.Result()
	index := -1
	for i, ch := range res.Changes {
		if ch.Intersects(line) {
			index = i
			break
		}
	}
	if index < 0 {
		return
	}
	if c.open && index == c.index {
		c.Close()
		return
	}
	provider := ""
	if c.open {
		provider = c.providerID
	}
	c.at(res.Changes, line, true, provider)
}

func (c *Controller) cursorLine() int { return c.cfg.Editor.Cursors().Primary().Position().Line + 1 }

// rows is the height of the zone in rows of the editor: the lines of the change, a row for the title bar and the frame, and three lines
// above and below, but not more than a third of the editor (VS Code's formula; at least five rows, so that the title bar and a few lines show
// in a small editor).
func (c *Controller) rows(ch quickdiff.Change) int {
	rows := ch.Height() + 2 + 6
	vm := c.cfg.Editor
	if lh := vm.Options().LineHeight; lh > 0 {
		if _, h := vm.ViewportSize(); h > 0 {
			rows = min(rows, int(h/lh)/3)
		}
	}
	return max(rows, 5)
}

// show opens the peek on the change at the index, or moves it there; usePosition moves the cursor to the change and brings the zone into
// view.
func (c *Controller) show(index int, usePosition bool) {
	res := c.cfg.Model.Result()
	if index < 0 || index >= len(res.Changes) {
		return
	}
	ch := res.Changes[index]
	c.index, c.providerID = index, ch.ProviderID
	if err := c.ensureBody(res, ch); err != nil {
		if c.cfg.OnError != nil {
			c.cfg.OnError(err)
		}
		c.Close()
		return
	}
	vm := c.cfg.Editor
	line := ch.ModifiedEndLine()
	spec := editor.ZoneSpec{AfterLine: line, Rows: c.rows(ch.Change), Widget: newFrame(c)}
	if !c.open {
		id, err := vm.AddZone(spec)
		if err != nil {
			if c.cfg.OnError != nil {
				c.cfg.OnError(err)
			}
			c.disposeBody()
			return
		}
		c.zone, c.open = id, true
	} else if err := vm.ChangeZone(c.zone, func(z *editor.ZoneSpec) { z.AfterLine, z.Rows = spec.AfterLine, spec.Rows }); err != nil {
		if c.cfg.OnError != nil {
			c.cfg.OnError(err)
		}
	}
	if c.body != nil && c.body.Reveal != nil {
		c.body.Reveal(ch.Change)
	}
	if usePosition {
		pos := buffer.Position{Line: line - 1}
		vm.PlaceCursor(pos)
		vm.RevealZone(c.zone, pos)
		if c.cfg.FocusEditor != nil {
			c.cfg.FocusEditor()
		}
	}
	vm.QuickDiffPeekChanged()
	c.changed.NotifyListeners()
}

// ensureBody makes the small diff editor for the provider of a change when the peek has none for it, or its original changed.
func (c *Controller) ensureBody(res quickdiff.Result, ch quickdiff.ProviderChange) error {
	text, _ := c.cfg.Model.OriginalText(ch.ProviderID)
	if c.body != nil && c.bodyProvider == ch.ProviderID && c.bodyText == text {
		return nil
	}
	c.disposeBody()
	spec := BodySpec{ProviderID: ch.ProviderID, Text: text, Peeker: c}
	for _, p := range res.Providers {
		if p.ID == ch.ProviderID {
			spec.Label, spec.Original = p.Label, p.Original
		}
	}
	if c.cfg.NewBody == nil {
		return errors.New("QuickDiffPeekNoBody", "the peek has no function that makes the small diff editor")
	}
	body, err := c.cfg.NewBody(spec)
	if err != nil {
		return err
	}
	c.body, c.bodyProvider, c.bodyText = &body, ch.ProviderID, text
	return nil
}

func (c *Controller) disposeBody() {
	if c.body != nil && c.body.Dispose != nil {
		c.body.Dispose()
	}
	c.body, c.bodyProvider, c.bodyText = nil, "", ""
}

// Close implements editor.QuickDiffPeeker: the peek goes, and the keyboard is the editor's.
func (c *Controller) Close() {
	if !c.open {
		return
	}
	c.open = false
	if err := c.cfg.Editor.RemoveZone(c.zone); err != nil && c.cfg.OnError != nil {
		c.cfg.OnError(err)
	}
	c.disposeBody()
	c.index, c.providerID = -1, ""
	c.cfg.Editor.QuickDiffPeekChanged()
	c.changed.NotifyListeners()
	if c.cfg.FocusEditor != nil {
		c.cfg.FocusEditor()
	}
}

// ToggleFocus implements editor.QuickDiffPeeker: the keyboard goes into the small diff editor, or back to the editor.
func (c *Controller) ToggleFocus() {
	if !c.open || c.body == nil {
		return
	}
	if c.body.HasFocus != nil && c.body.HasFocus() {
		if c.cfg.FocusEditor != nil {
			c.cfg.FocusEditor()
		}
		return
	}
	if c.body.Focus != nil {
		c.body.Focus()
	}
}

// modelChanged follows the changes of the model while the peek is open: it goes when there are none, and otherwise shows the change at the
// place of the one it showed, in the same provider (VS Code moves on to the next one when a change before the shown one went), unless the
// keyboard is in the small editor, which is left as the user is working in it.
//
// Ported from VS Code: quickDiffWidget.ts (QuickDiffEditorController.onDidModelChange)
func (c *Controller) modelChanged() {
	if !c.open {
		return
	}
	res := c.cfg.Model.Result()
	if len(res.Changes) == 0 {
		c.Close()
		return
	}
	mine := providerIndexes(res.Changes, c.providerID)
	if len(mine) == 0 {
		mine = providerIndexes(res.Changes, "")
	}
	focused := c.body != nil && c.body.HasFocus != nil && c.body.HasFocus()
	index := min(c.index, len(res.Changes)-1)
	if !focused {
		// the change nearest to the line the peek is at
		line := 1
		if z := c.zoneLine(); z > 0 {
			line = z
		}
		list := make([]quickdiff.Change, len(mine))
		for i, idx := range mine {
			list[i] = res.Changes[idx].Change
		}
		index = mine[quickdiff.FindNext(list, line, true)]
	}
	c.show(index, false)
}

// zoneLine is the line the zone of the peek comes after now, counted from 1; 0 when it is not there.
func (c *Controller) zoneLine() int {
	for _, z := range c.cfg.Editor.Zones() {
		if z.ID == c.zone {
			return z.AfterLine
		}
	}
	return 0
}
