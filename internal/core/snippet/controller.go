package snippet

import (
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
)

// Ported from VS Code: src/vs/editor/contrib/snippet/browser/snippetController2.ts

// State is what the editor's context keys say about the snippet mode (SPEC §9.9): inSnippetMode,
// hasNextTabstop, hasPrevTabstop and inSnippetChoice.
type State struct {
	// InSnippet is true while a snippet is being filled in.
	InSnippet bool
	// HasNext and HasPrev tell whether there is a tab stop after and before the active one.
	HasNext, HasPrev bool
	// InChoice tells that the active tab stop is a choice.
	InChoice bool
}

// Controller runs the snippet session of an editor: it starts it when a snippet is inserted,
// nests a snippet that is inserted in it, follows the cursors and the document, and ends the
// session when the cursors leave the tab stops, the final tab stop is reached or the document is
// replaced.
type Controller struct {
	ed  Editor
	env func() Env
	// onState is told when the state changes, to set the context keys.
	onState func(State)

	session *Session
	// altVersion is the alternative version id of the document before the snippet was inserted: an
	// undo that comes back to it ends the session.
	altVersion int
	state      State
	choice     *Choice
	// listening is false while a snippet is inserted: the cursors it moves are not a reason to cancel.
	listening   bool
	unsubscribe func()
	// onChoice is told when the active tab stop becomes a choice.
	onChoice func(*Choice, buffer.Range)
}

// NewController makes the controller of an editor. env gives the values of the variables when a
// snippet is inserted; onState is called with the state after each change (it may be nil).
func NewController(ed Editor, env func() Env, onState func(State)) *Controller {
	return &Controller{ed: ed, env: env, onState: onState, altVersion: -1}
}

// OnChoice sets the function called when the active tab stop is a choice that was not shown yet,
// with the choice and the range of the text it replaces; the editor shows the options.
func (c *Controller) OnChoice(fn func(choice *Choice, replace buffer.Range)) { c.onChoice = fn }

// State is the snippet mode now.
func (c *Controller) State() State { return c.state }

// IsInSnippet tells whether a snippet is being filled in.
func (c *Controller) IsInSnippet() bool { return c.state.InSnippet }

func (c *Controller) setState(s State) {
	if s == c.state {
		return
	}
	c.state = s
	if c.onState != nil {
		c.onState(s)
	}
}

// Insert inserts a snippet at the cursors; inside a session it becomes a nested snippet. On an
// error the session ends, and the error is returned.
func (c *Controller) Insert(template string, opts InsertOptions) error {
	return c.insert(opts, func(s *Session) error { return s.Insert() }, func() *Session {
		return NewSession(c.ed, c.env(), template, opts)
	}, func(s *Session) error { return s.Merge(template, opts) })
}

// Apply inserts templates in ranges as one snippet. A session that is running ends first.
func (c *Controller) Apply(edits []Edit, opts InsertOptions) error {
	if c.session != nil {
		c.Cancel(false)
	}
	return c.insert(opts, func(s *Session) error { return s.Insert() }, func() *Session {
		return NewSessionFromEdits(c.ed, c.env(), edits, opts)
	}, nil)
}

func (c *Controller) insert(opts InsertOptions, start func(*Session) error, make func() *Session, merge func(*Session) error) error {
	// do not listen while inserting: the changes it makes would end the session
	c.stopListening()
	if opts.UndoStopBefore {
		c.ed.Doc.PushStackElement()
	}
	var err error
	if c.session == nil {
		c.altVersion = c.ed.Doc.AlternativeVersion()
		c.session = make()
		err = start(c.session)
	} else if merge != nil {
		err = merge(c.session)
	}
	if err != nil {
		c.Cancel(false)
		return err
	}
	if opts.UndoStopAfter {
		c.ed.Doc.PushStackElement()
	}
	c.updateState()
	if c.session != nil {
		c.listen()
	}
	return nil
}

func (c *Controller) listen() {
	c.listening = true
	c.unsubscribe = c.ed.Doc.Subscribe(func(ev document.ChangeEvent) {
		if ev.Cause == document.CauseSetText {
			c.Cancel(false)
		}
	})
}

func (c *Controller) stopListening() {
	c.listening = false
	if c.unsubscribe != nil {
		c.unsubscribe()
		c.unsubscribe = nil
	}
}

// SelectionsChanged tells the controller that the cursors moved: the session ends when they are
// out of the tab stops.
func (c *Controller) SelectionsChanged() {
	if c.listening {
		c.updateState()
	}
}

func (c *Controller) updateState() {
	if c.session == nil {
		return
	}
	switch {
	case c.altVersion == c.ed.Doc.AlternativeVersion():
		// undone to the state before the snippet
		c.Cancel(false)
		return
	case !c.session.HasPlaceholder():
		// plain text: do not listen and set no context keys
		c.Cancel(false)
		return
	case c.session.IsAtLastPlaceholder() || !c.session.IsSelectionWithinPlaceholders():
		c.ed.Doc.PushStackElement()
		c.Cancel(false)
		return
	}
	c.setState(State{InSnippet: true, HasNext: !c.session.IsAtLastPlaceholder(), HasPrev: !c.session.IsAtFirstPlaceholder(), InChoice: c.state.InChoice})
	c.handleChoice()
}

func (c *Controller) handleChoice() {
	choice, r, ok := c.session.ActiveChoice()
	if !ok {
		c.choice = nil
		st := c.state
		st.InChoice = false
		c.setState(st)
		return
	}
	st := c.state
	st.InChoice = true
	c.setState(st)
	if c.choice != choice {
		c.choice = choice
		if c.onChoice != nil {
			c.onChoice(choice, r)
		}
	}
}

// ActiveChoice is the choice of the active tab stop and the range of its text.
func (c *Controller) ActiveChoice() (*Choice, buffer.Range, bool) {
	if c.session == nil {
		return nil, buffer.Range{}, false
	}
	return c.session.ActiveChoice()
}

// Next goes to the next tab stop.
func (c *Controller) Next() error {
	if c.session == nil {
		return nil
	}
	err := c.session.Next()
	c.updateState()
	return err
}

// Prev goes to the previous tab stop.
func (c *Controller) Prev() error {
	if c.session == nil {
		return nil
	}
	err := c.session.Prev()
	c.updateState()
	return err
}

// Finish goes through the tab stops up to the final one.
func (c *Controller) Finish() error {
	for c.state.InSnippet {
		if err := c.Next(); err != nil {
			return err
		}
	}
	return nil
}

// Cancel ends the session. With resetSelection (Escape) the cursors collapse to the primary one.
func (c *Controller) Cancel(resetSelection bool) {
	c.stopListening()
	c.choice = nil
	c.setState(State{})
	if c.session != nil {
		c.session.Dispose()
		c.session = nil
	}
	c.altVersion = -1
	if resetSelection && c.ed.Cursors.Len() > 1 {
		c.ed.Cursors.SetSelections([]buffer.Selection{c.ed.Cursors.Primary().Selection})
	}
}

// EnclosingRange is the range that covers the tab stops of the session.
func (c *Controller) EnclosingRange() (buffer.Range, bool) {
	if c.session == nil {
		return buffer.Range{}, false
	}
	return c.session.EnclosingRange()
}
