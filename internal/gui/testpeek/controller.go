package testpeek

import (
	"strings"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/gui/editor"
)

// StackFrame is a line of the stack trace of a failure.
type StackFrame struct {
	// Label is what the frame is (a function); Path is the file, empty for a frame with no file, and Line and Col its place, counted from 1
	// (0 for none). Display is how the file is written in the peek (the path in the folder of the workspace); Path when empty.
	Label, Path, Display string
	Line, Col            int
}

// Subject is a message the peek can show: a failure of a test, or a logged message.
type Subject struct {
	// Key identifies the message among those of the file, and stays the same while the result lives.
	Key string
	// TestID is the test the message belongs to, and Title its label.
	TestID, Title string
	// Place is where the message is, "calculator_test.go:13", for the heading.
	Place string
	// Line is the line of the editor the zone comes after, counted from 0.
	Line int
	// Text is the message; Markdown says that it is Markdown (shown as it is written).
	Text     string
	Markdown bool
	// Expected and Actual are shown as a diff when HasDiff is true.
	Expected, Actual string
	HasDiff          bool
	// Stack are the frames of the failure, the innermost first.
	Stack []StackFrame
	// Output is the output of the test (or of its run) with its ANSI sequences; empty for none.
	Output string
	// Failure is true for the message of a failure and false for a logged message, which has the colors of an info.
	Failure bool
	// Language is the language id of the file, for the colors of the diff.
	Language string
}

// BodySpec is what the window makes the small diff editor from.
type BodySpec struct {
	Expected, Actual, Language string
	// Peeker is the controller; Escape in the body closes the peek.
	Peeker *Controller
}

// Body is the small diff editor of the peek: the expected text and the actual, in one column.
type Body struct {
	// Widget is the diff editor, as wide and as tall as the pane leaves it.
	Widget widgets.Widget
	// Focus gives the keyboard to the editor and HasFocus tells whether it has it.
	Focus    func()
	HasFocus func() bool
	// Ready tells whether the diff has been computed (nil for always); tests wait for it.
	Ready func() bool
	// Dispose lets go of the editors and the documents of the body.
	Dispose func()
}

// Action is a button of the title bar.
type Action struct {
	// Icon is the Codicon (a fonts.Codicon... constant) and Tooltip the hover.
	Icon    int
	Tooltip string
	// Run is called when the button is pressed.
	Run func()
}

// Config configures a Controller.
type Config struct {
	// Editor is the editor of the file the peek is shown in.
	Editor *editor.ViewModel
	// List gives the messages of the file now, in the order Step goes through them.
	List func() []Subject
	// NewBody makes the small diff editor. It is called when a message with a diff is shown.
	NewBody func(BodySpec) (Body, error)
	// OpenFrame follows a frame of the stack trace.
	OpenFrame func(StackFrame)
	// Actions are the buttons of the title bar for a message, before the arrows and the close button.
	Actions func(Subject) []Action
	// FocusEditor gives the keyboard to the editor.
	FocusEditor func()
	// OnChange is told when the peek opens, moves or closes.
	OnChange func()
	// OnError is told about a body that could not be made.
	OnError func(error)
}

// Controller is the peek of the test messages of one editor. It is used on the UI thread only.
type Controller struct {
	cfg Config

	open bool
	cur  Subject
	zone editor.ZoneID
	body *Body
	// bodyKey is what the body was made for: the expected and the actual text.
	bodyExpected, bodyActual string
	// showOutput says that the right pane shows the output instead of the diff.
	showOutput bool
	changed    *foundation.ChangeNotifier
}

// New makes the peek of an editor.
func New(cfg Config) *Controller {
	return &Controller{cfg: cfg, changed: foundation.NewChangeNotifier()}
}

// Dispose closes the peek.
func (c *Controller) Dispose() {
	c.cfg.FocusEditor = nil // the editor is going too: the keyboard is not for it
	c.Close()
}

// Visible tells whether the peek is open (the context key testing.isPeekVisible); a nil controller (an editor that never made one) is closed.
func (c *Controller) Visible() bool { return c != nil && c.open }

// Current is the message the peek shows; false while it is closed (or for a nil controller).
func (c *Controller) Current() (Subject, bool) {
	if c == nil {
		return Subject{}, false
	}
	return c.cur, c.open
}

// Body is the small diff editor of the peek; nil while it is closed or when the message has no diff.
func (c *Controller) Body() *Body {
	if c == nil || !c.open {
		return nil
	}
	return c.body
}

// AddListener calls fn when the peek opens, moves, changes or closes.
func (c *Controller) AddListener(fn foundation.VoidCallback) foundation.ListenerHandle {
	return c.changed.AddListener(fn)
}

// RemoveListener removes a listener of AddListener.
func (c *Controller) RemoveListener(h foundation.ListenerHandle) { c.changed.RemoveListener(h) }

// Position is the place of the message among those of the file, counted from 1, and their number.
func (c *Controller) Position() (index, total int) {
	list := c.cfg.List()
	for i, s := range list {
		if s.Key == c.cur.Key {
			return i + 1, len(list)
		}
	}
	return 0, len(list)
}

// Title is the heading of the title bar.
func (c *Controller) Title() string { return c.cur.Title }

// Actions are the buttons of the title bar for the message shown.
func (c *Controller) Actions() []Action {
	if !c.open || c.cfg.Actions == nil {
		return nil
	}
	return c.cfg.Actions(c.cur)
}

// ShowOutput tells whether the right pane shows the output.
func (c *Controller) ShowOutput() bool { return c.showOutput }

// ToggleOutput switches the right pane between the diff and the output.
func (c *Controller) ToggleOutput() {
	if !c.open {
		return
	}
	c.showOutput = !c.showOutput
	c.notify()
}

// OpenFrame follows a frame of the stack trace.
func (c *Controller) OpenFrame(f StackFrame) {
	if c.cfg.OpenFrame != nil {
		c.cfg.OpenFrame(f)
	}
}

// Show opens the peek on the message with the key, or moves it there; false when the file has no such message.
func (c *Controller) Show(key string) bool {
	for _, s := range c.cfg.List() {
		if s.Key == key {
			c.ShowSubject(s)
			return true
		}
	}
	return false
}

// Step moves the peek to the next (or previous) message of the file, going round; while it is closed it opens on the first one after the
// cursor's line (before it, going back).
//
// Ported from VS Code: src/vs/workbench/contrib/testing/browser/testingOutputPeek.ts (TestingOutputPeekController.next and previous)
func (c *Controller) Step(next bool) {
	list := c.cfg.List()
	if len(list) == 0 {
		c.Close()
		return
	}
	at := 0
	if c.open {
		for i, s := range list {
			if s.Key == c.cur.Key {
				at = i
			}
		}
		if next {
			at = (at + 1) % len(list)
		} else {
			at = (at - 1 + len(list)) % len(list)
		}
	} else {
		line := c.cfg.Editor.Cursors().Primary().Position().Line
		at = -1
		if next {
			for i, s := range list {
				if s.Line >= line {
					at = i
					break
				}
			}
			if at < 0 {
				at = 0
			}
		} else {
			for i := len(list) - 1; i >= 0; i-- {
				if list[i].Line <= line {
					at = i
					break
				}
			}
			if at < 0 {
				at = len(list) - 1
			}
		}
	}
	c.ShowSubject(list[at])
}

// lines counts the lines of a text, none for an empty one.
func lines(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(strings.TrimRight(s, "\n"), "\n") + 1
}

// rows is the height of the zone in rows of the editor: the title bar and the lines of the longer pane (the message and its stack, or the
// two texts of the diff), but not more than twelve or half of the editor, and at least six, so that the title bar and a few lines show in a
// small editor.
func (c *Controller) rows(s Subject) int {
	left := lines(s.Text)
	if len(s.Stack) > 0 {
		left += 2 + len(s.Stack)
	}
	right := 0
	switch {
	case s.HasDiff:
		right = lines(s.Expected) + lines(s.Actual)
	case s.Output != "" && left == 0:
		right = lines(s.Output)
	}
	rows := min(max(left, right)+3, 12)
	vm := c.cfg.Editor
	if lh := vm.Options().LineHeight; lh > 0 {
		if _, h := vm.ViewportSize(); h > 0 {
			rows = min(rows, int(h/lh)/2)
		}
	}
	return max(rows, 6)
}

// ShowSubject opens the peek on a message, or moves it there. The cursor goes to the line of the message and the zone comes into view.
func (c *Controller) ShowSubject(s Subject) {
	vm := c.cfg.Editor
	prev := c.cur
	c.cur = s
	if !c.open || prev.Key != s.Key {
		c.showOutput = false
	}
	if err := c.ensureBody(s); err != nil {
		if c.cfg.OnError != nil {
			c.cfg.OnError(err)
		}
		c.disposeBody()
	}
	spec := editor.ZoneSpec{AfterLine: s.Line + 1, Rows: c.rows(s), Widget: newFrame(c), ShowInHiddenAreas: true}
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
	pos := buffer.Position{Line: s.Line}
	vm.PlaceCursor(pos)
	vm.RevealZone(c.zone, pos)
	c.notify()
}

// Refresh follows a change of the messages of the file: the peek goes when its message does, and otherwise shows the message again (its text
// or its output may have changed).
func (c *Controller) Refresh() {
	if !c.open {
		return
	}
	for _, s := range c.cfg.List() {
		if s.Key != c.cur.Key {
			continue
		}
		moved := s.Line != c.cur.Line
		c.cur = s
		if err := c.ensureBody(s); err != nil && c.cfg.OnError != nil {
			c.cfg.OnError(err)
		}
		if moved {
			if err := c.cfg.Editor.ChangeZone(c.zone, func(z *editor.ZoneSpec) { z.AfterLine = s.Line + 1 }); err != nil && c.cfg.OnError != nil {
				c.cfg.OnError(err)
			}
		}
		c.notify()
		return
	}
	c.Close()
}

// ensureBody makes the small diff editor for the message when it has a diff and the body is not that of its text.
func (c *Controller) ensureBody(s Subject) error {
	if !s.HasDiff {
		c.disposeBody()
		return nil
	}
	if c.body != nil && c.bodyExpected == s.Expected && c.bodyActual == s.Actual {
		return nil
	}
	c.disposeBody()
	if c.cfg.NewBody == nil {
		return errors.New("TestPeekNoBody", "the peek has no function that makes the small diff editor")
	}
	body, err := c.cfg.NewBody(BodySpec{Expected: s.Expected, Actual: s.Actual, Language: s.Language, Peeker: c})
	if err != nil {
		return err
	}
	c.body, c.bodyExpected, c.bodyActual = &body, s.Expected, s.Actual
	return nil
}

func (c *Controller) disposeBody() {
	if c.body != nil && c.body.Dispose != nil {
		c.body.Dispose()
	}
	c.body, c.bodyExpected, c.bodyActual = nil, "", ""
}

// Close removes the peek, and the keyboard is the editor's.
func (c *Controller) Close() {
	if !c.open {
		return
	}
	c.open, c.showOutput = false, false
	if err := c.cfg.Editor.RemoveZone(c.zone); err != nil && c.cfg.OnError != nil {
		c.cfg.OnError(err)
	}
	c.disposeBody()
	c.cur = Subject{}
	c.notify()
	if c.cfg.FocusEditor != nil {
		c.cfg.FocusEditor()
	}
}

func (c *Controller) notify() {
	c.changed.NotifyListeners()
	if c.cfg.OnChange != nil {
		c.cfg.OnChange()
	}
}
