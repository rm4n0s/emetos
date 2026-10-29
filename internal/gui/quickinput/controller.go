package quickinput

import (
	"context"
	"slices"
	"strings"

	"github.com/rm4n0s/emetos/internal/core/contextkey"
)

// Ported from VS Code: src/vs/platform/quickinput/browser/quickAccess.ts (QuickAccessController)
// and src/vs/platform/quickinput/browser/quickInputController.ts (show, hide)

// ShowOptions are the options of Controller.Show.
type ShowOptions struct {
	// PreserveValue takes the value as it is, instead of selecting what follows the prefix and
	// restoring the last input of a provider.
	PreserveValue bool
	// QuickNavigate opens the picker the way Ctrl+Tab does: it is a switcher that the keyboard drives while
	// the keys that opened it are held. The input is hidden, the second item is active (the first, when
	// Activation says so), and the active item is chosen when the held modifier key is released.
	QuickNavigate *QuickNavigate
	// Activation says which item is active when the items are set. In the quick navigate mode it is the
	// second item unless it says otherwise.
	Activation ItemActivation
	// OnFile, when it is set, is given the file that is chosen in the file mode of this session, in the place of
	// opening it: "Compare Active File With..." asks for a file this way. It is called on the UI thread, once the
	// picker is closed, as FilesOptions.Open is.
	OnFile func(FileOpen)
}

// Modifiers are the modifier keys of a chord.
type Modifiers struct {
	Ctrl, Shift, Alt, Meta bool
}

// Modifier names a modifier key.
type Modifier int

// The modifier keys.
const (
	// ModCtrl is the Ctrl key.
	ModCtrl Modifier = iota + 1
	// ModShift is the Shift key.
	ModShift
	// ModAlt is the Alt key.
	ModAlt
	// ModMeta is the Meta key (Super).
	ModMeta
)

// QuickNavigate is the state of a picker that is driven by keys that are held (VS Code's
// IQuickNavigateConfiguration): the chords of the keys that opened it, and the keys that move in it. Releasing
// the modifier of one of the chords chooses the active item.
type QuickNavigate struct {
	// Chords are the modifiers of the keybindings of the command that opened the picker; a chord of more than
	// one key press is not in the list.
	Chords []Modifiers
}

// triggered tells whether releasing a modifier key, with the modifiers still held, is the signal to choose
// the active item (VS Code's registerQuickNavigation): the release of Shift counts only when no other
// modifier is held, since Ctrl+Shift+Tab lets Shift go first to go back.
func (q *QuickNavigate) triggered(released Modifier, held Modifiers) bool {
	for _, c := range q.Chords {
		switch {
		case c.Shift && released == ModShift:
			if held.Ctrl || held.Alt || held.Meta {
				continue
			}
			return true
		case c.Alt && released == ModAlt, c.Ctrl && released == ModCtrl, c.Meta && released == ModMeta:
			return true
		}
	}
	return false
}

// heldBy tells whether the modifiers held now are those of one of the chords.
func (q *QuickNavigate) heldBy(held Modifiers) bool {
	for _, c := range q.Chords {
		if (!c.Ctrl || held.Ctrl) && (!c.Shift || held.Shift) && (!c.Alt || held.Alt) && (!c.Meta || held.Meta) {
			return true
		}
	}
	return false
}

// PickOptions describe a pick from a list of items that the caller owns: the Insert Snippet
// picker. The items filter by their label, and by their description and detail when asked.
type PickOptions struct {
	// Placeholder is the hint of the input.
	Placeholder string
	// Items are the items; each item's Accept runs when it is chosen.
	Items []Item
	// MatchOnDescription and MatchOnDetail let the filter look at those texts too.
	MatchOnDescription, MatchOnDetail bool
	// ActiveID, when it is the ID of one of the items, makes that item the active one when the picker opens
	// (VS Code's activeItem: the current end of line sequence, the current tab size).
	ActiveID string
	// OnCancel is called when the picker closes without a choice.
	OnCancel func()
	// OnActive is called when the active item changes while the picker is open, with the item (the arrow keys
	// and typing a filter move it; the pointer does not, as in VS Code's onDidChangeActive); ok is false when
	// no item is active. It is not called for the item that ActiveID made active: the caller knows it. The theme
	// pickers preview with it.
	OnActive func(item Item, ok bool)
	// OnHide is called once when the picker ends, whatever the way: chosen (accepted is true, before the item's
	// Accept runs), closed without a choice, or replaced by another quick input. It is called before OnCancel.
	OnHide func(accepted bool)
}

// PromptOptions describe an input box: a line of text that the user accepts with Enter or cancels with Escape (VS
// Code's showInputBox). It is how the Save As dialog of the editor asks for a path when the desktop has no file
// chooser.
type PromptOptions struct {
	// Placeholder is the hint of the input while it is empty.
	Placeholder string
	// Value is the text the input starts with; the caret is at its end.
	Value string
	// Password shows a dot for each character typed, as a password box does (VS Code's password option).
	Password bool
	// Validate, when set, checks the text on every change and when it is accepted: a message that is not empty is
	// shown under the input (as an error) and the text is not accepted (VS Code's validateInput).
	Validate func(value string) string
	// OnAccept is called with the text, after the input hides and the focus is back, when the user accepted a
	// text that is valid.
	OnAccept func(value string)
	// OnCancel is called when the input closes without an accepted text.
	OnCancel func()
}

// Controller shows the quick input: one picker at a time, with the provider that the value selects
// (SPEC §10.6). The widget (Host) follows it with OnChange. It is used on the UI thread only.
type Controller struct {
	registry *Registry
	picker   *Picker
	visible  bool
	session  *Session
	provider Provider
	handler  Handler
	// fixed is set while a pick of the caller is shown: the value does not change the provider.
	fixed    bool
	onCancel func()
	// onFile is ShowOptions.OnFile of the last Show.
	onFile func(FileOpen)
	// onHide is PickOptions.OnHide, and stopActive stops following the active item for PickOptions.OnActive.
	onHide     func(accepted bool)
	stopActive func()
	accepted   bool
	prompt     *prompt
	listeners  []*func()
	last       map[string]string

	post    func(func())
	after   func(func())
	invoker func() contextkey.Context
	// fallback is the hint of the input while no provider takes the value.
	fallback string
	// context is the context keys of the widget that had the focus when the quick input opened.
	context contextkey.Context
	// quickNav is set while the picker waits for the release of the keys that opened it; quickMode says
	// that it was opened as a switcher (and has no input), whether the release was seen or not.
	quickNav  *QuickNavigate
	quickMode bool
}

// ControllerOptions are the options of NewController.
type ControllerOptions struct {
	// Registry holds the providers.
	Registry *Registry
	// Post runs a function on the UI thread; a provider that works in the background hands its
	// results over with it. Without it the function runs at once.
	Post func(func())
	// After runs a function once the picker is hidden and the focus is back where it was: an accepted
	// item runs with it. Without it the function runs at once.
	After func(func())
	// FallbackPlaceholder is the hint of the input while no provider takes the value (the file mode
	// is the default provider of VS Code and does not exist yet).
	FallbackPlaceholder string
	// Invoker gives the context keys of the widget that has the focus; the controller asks for them
	// when the quick input opens, before anything takes the focus, and keeps them while it is shown
	// (the keys the command palette looks at). Without it the keys are empty.
	Invoker func() contextkey.Context
}

// NewController makes a controller over a registry.
func NewController(opts ControllerOptions) *Controller {
	c := &Controller{registry: opts.Registry, picker: NewPicker(), last: map[string]string{}, post: opts.Post, after: opts.After, invoker: opts.Invoker, fallback: opts.FallbackPlaceholder}
	if c.registry == nil {
		c.registry = NewRegistry()
	}
	if c.post == nil {
		c.post = func(f func()) { f() }
	}
	if c.after == nil {
		c.after = func(f func()) { f() }
	}
	return c
}

// Configure sets the hooks of the options that are not nil: the window makes its controller before
// its widgets exist and gives it the hooks that need them when the shell starts.
func (c *Controller) Configure(opts ControllerOptions) {
	if opts.Post != nil {
		c.post = opts.Post
	}
	if opts.After != nil {
		c.after = opts.After
	}
	if opts.Invoker != nil {
		c.invoker = opts.Invoker
	}
	if opts.FallbackPlaceholder != "" {
		c.fallback = opts.FallbackPlaceholder
	}
}

// Registry is the registry of the providers.
func (c *Controller) Registry() *Registry { return c.registry }

// InvokerContext is the context keys of the widget that had the focus when the quick input opened;
// it is never nil.
func (c *Controller) InvokerContext() contextkey.Context {
	if c.context == nil {
		return contextkey.MapContext{}
	}
	return c.context
}

// Visible tells whether the quick input is shown.
func (c *Controller) Visible() bool { return c.visible }

// Picker is the picker that is shown (or was shown last).
func (c *Controller) Picker() *Picker { return c.picker }

// Provider is the provider of the shown picker, or nil for a pick of the caller or no provider.
func (c *Controller) Provider() Provider { return c.provider }

// OnChange calls f when the quick input is shown or hidden and when its picker is replaced; the
// picker tells of its own changes.
func (c *Controller) OnChange(f func()) (remove func()) { return addListener(&c.listeners, &f) }

func (c *Controller) fire() {
	for _, f := range slices.Clone(c.listeners) {
		(*f)()
	}
}

// Show opens the quick input with a value (VS Code's quickAccess.show). When the provider of the
// value is the one that is on already, the value replaces the input if it says more than the
// prefix; the part after the prefix is selected, so that typing replaces it. A value for another
// provider starts that provider and keeps the text typed after the old prefix.
func (c *Controller) Show(value string, opts ShowOptions) {
	if !c.visible && c.invoker != nil {
		c.context = c.invoker()
	}
	provider := c.registry.Resolve(value)
	if c.visible && !c.fixed && provider != nil && provider == c.provider {
		if opts.QuickNavigate != nil {
			c.quickNav = opts.QuickNavigate
		}
		c.onFile = opts.OnFile
		if c.session != nil {
			c.session.onFile = opts.OnFile
		}
		prefix := provider.Info().Prefix
		if value != prefix && !opts.PreserveValue {
			c.picker.SetValue(value)
		}
		c.selectValue(prefix, opts)
		return
	}
	if provider != nil && !opts.PreserveValue {
		prefix := provider.Info().Prefix
		newValue := ""
		if c.visible && c.provider != nil && c.provider != provider && !c.fixed {
			if rest := strings.TrimPrefix(c.picker.Value(), c.provider.Info().Prefix); rest != "" {
				newValue = prefix + rest
			}
		}
		if newValue == "" && value == prefix {
			if f, ok := provider.(DefaultFilterer); ok && f.DefaultFilterValue() == DefaultFilterLast {
				newValue = c.last[prefix]
			}
		}
		if newValue != "" {
			value = newValue
		}
	}
	wasVisible := c.visible
	c.releasePick(false)
	c.endSession(false)
	c.endPrompt()
	c.fixed, c.onCancel, c.accepted = false, nil, false
	c.visible = true
	c.picker = NewPicker()
	c.picker.SetValue(value)
	activation := opts.Activation
	c.quickNav, c.quickMode = opts.QuickNavigate, opts.QuickNavigate != nil
	if c.quickMode {
		// the input is hidden unless a picker was open already
		c.picker.HideInput = !wasVisible
		if activation == ItemFirst {
			activation = ItemSecond
		}
	}
	c.picker.SetItemActivation(activation)
	c.onFile = opts.OnFile
	c.start(provider)
	prefix := ""
	if provider != nil {
		prefix = provider.Info().Prefix
	}
	c.selectValue(prefix, opts)
	c.fire()
}

// selectValue selects the part of the value after the prefix (or puts the caret at the end with
// PreserveValue), as VS Code's adjustValueSelection does.
func (c *Controller) selectValue(prefix string, opts ShowOptions) {
	n := len(c.picker.Value())
	if opts.PreserveValue {
		c.picker.SetValueSelection(n, n)
		return
	}
	c.picker.SetValueSelection(min(len(prefix), n), n)
}

// start makes the session of a provider (nil for a value no provider takes) on the current picker.
func (c *Controller) start(p Provider) {
	c.provider = p
	var info ProviderInfo
	if p != nil {
		info = p.Info()
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.session = &Session{c: c, picker: c.picker, ctx: ctx, cancel: cancel, prefix: info.Prefix, onFile: c.onFile}
	c.picker.SetPrefixLen(len(info.Prefix))
	c.picker.SetPlaceholder(info.Placeholder)
	if p == nil {
		c.picker.SetPlaceholder(c.fallback)
	}
	if p != nil {
		c.handler = p.Provide(c.session)
	}
	c.watchValue()
	if c.handler != nil {
		c.handler.ValueChanged(c.picker.Filter())
	}
}

// watchValue follows the value of the picker for the life of the session.
func (c *Controller) watchValue() {
	s := c.session
	if s == nil {
		return
	}
	remove := c.picker.OnDidChangeValue(func(v string) { c.valueChanged(s, v) })
	s.OnEnd(remove)
}

// valueChanged is called when the user (or Show) changed the value.
func (c *Controller) valueChanged(s *Session, v string) {
	if s != c.session || !c.visible {
		return
	}
	if !c.fixed {
		if next := c.registry.Resolve(v); next != c.provider {
			// another provider: start again with the same text
			c.endSession(false)
			c.picker = NewPicker()
			c.picker.SetValue(v)
			c.picker.SetValueSelection(len(v), len(v))
			c.start(next)
			c.fire()
			return
		}
	}
	if c.handler != nil {
		c.handler.ValueChanged(c.picker.Filter())
	}
}

// SetValue is the user typing: it changes the value of the input.
func (c *Controller) SetValue(v string) {
	if c.visible {
		c.picker.SetValue(v)
	}
}

// Pick shows a pick from a list of items that the caller owns. The item the user chooses has its
// Accept run after the picker hides.
func (c *Controller) Pick(opts PickOptions) {
	if !c.visible && c.invoker != nil {
		c.context = c.invoker()
	}
	c.releasePick(false)
	c.endSession(false)
	c.endPrompt()
	c.quickNav, c.quickMode = nil, false
	c.visible, c.fixed, c.accepted = true, true, false
	c.onCancel = opts.OnCancel
	c.provider, c.handler, c.session = nil, nil, nil
	c.picker = NewPicker()
	c.picker.MatchOnDescription, c.picker.MatchOnDetail = opts.MatchOnDescription, opts.MatchOnDetail
	c.picker.SetPlaceholder(opts.Placeholder)
	c.picker.SetItems(opts.Items)
	if opts.ActiveID != "" {
		for i, r := range c.picker.Rows() {
			if r.Item.ID == opts.ActiveID && !r.Item.Separator {
				c.picker.SetActive(i)
				break
			}
		}
	}
	c.onHide = opts.OnHide
	if opts.OnActive != nil {
		c.stopActive = c.picker.OnDidChangeActive(func(r Row, ok bool) { opts.OnActive(r.Item, ok) })
	}
	c.fire()
}

// releasePick ends what the pick that is shown asked for: it stops following the active item and calls its
// OnHide. It is for the end of the pick, by hiding or by another quick input taking its place.
func (c *Controller) releasePick(accepted bool) {
	if c.stopActive != nil {
		c.stopActive()
		c.stopActive = nil
	}
	hide := c.onHide
	c.onHide = nil
	if hide != nil && c.visible {
		hide(accepted)
	}
}

// prompt is the state of an input box that is shown.
type prompt struct {
	opts   PromptOptions
	remove func()
}

// Prompt shows an input box (see PromptOptions) in the place of the picker.
func (c *Controller) Prompt(opts PromptOptions) {
	if !c.visible && c.invoker != nil {
		c.context = c.invoker()
	}
	c.releasePick(false)
	c.endSession(false)
	c.endPrompt()
	c.quickNav, c.quickMode = nil, false
	c.visible, c.fixed, c.accepted = true, true, false
	c.onCancel = opts.OnCancel
	c.provider, c.handler, c.session = nil, nil, nil
	c.picker = NewPicker()
	c.picker.SetPlaceholder(opts.Placeholder)
	c.picker.Obscure = opts.Password
	c.picker.SetValue(opts.Value)
	c.picker.SetValueSelection(len(opts.Value), len(opts.Value))
	p := &prompt{opts: opts}
	if opts.Validate != nil {
		p.remove = c.picker.OnDidChangeValue(func(v string) { c.picker.SetMessage(opts.Validate(v)) })
	}
	c.prompt = p
	c.fire()
}

// Prompting tells whether the quick input shows an input box.
func (c *Controller) Prompting() bool { return c.visible && c.prompt != nil }

// endPrompt forgets the input box.
func (c *Controller) endPrompt() {
	if c.prompt != nil && c.prompt.remove != nil {
		c.prompt.remove()
	}
	c.prompt = nil
}

// acceptPrompt accepts the text of the input box when it is valid; the message of the validation shows otherwise.
func (c *Controller) acceptPrompt() {
	p := c.prompt
	value := c.picker.Value()
	if p.opts.Validate != nil {
		if msg := p.opts.Validate(value); msg != "" {
			c.picker.SetMessage(msg)
			return
		}
	}
	c.hide(true)
	if accept := p.opts.OnAccept; accept != nil {
		c.after(func() { accept(value) })
	}
}

// Accept accepts the active row of the picker: the provider decides what happens (a list provider
// hides the picker and runs the item). It does nothing when no row is active.
func (c *Controller) Accept(mods KeyMods) {
	if c.Prompting() {
		c.acceptPrompt()
		return
	}
	row, ok := c.picker.Active()
	if !c.visible || !ok {
		return
	}
	if c.fixed || c.handler == nil {
		c.AcceptRow(row, mods)
		return
	}
	c.handler.Accept(row, mods)
}

// AcceptRow hides the picker and runs the Accept of a row's item once the focus is back. It is
// what a list provider does when the user accepts; the item without an Accept keeps the picker
// open.
func (c *Controller) AcceptRow(row Row, mods KeyMods) {
	if row.Item.Accept == nil {
		return
	}
	c.hide(true)
	accept := row.Item.Accept
	c.after(func() { accept(mods) })
}

// Trigger runs the Trigger of a button of a row's item and does what it says.
func (c *Controller) Trigger(row Row, button int, mods KeyMods) {
	if row.Item.Trigger == nil || button < 0 || button >= len(row.Item.Buttons) {
		return
	}
	switch row.Item.Trigger(button, mods) {
	case TriggerClose:
		c.hide(false)
	case TriggerRefresh:
		if c.handler != nil {
			c.handler.ValueChanged(c.picker.Filter())
		}
	case TriggerRemove:
		items := slices.Clone(c.picker.Items())
		if row.Index >= 0 && row.Index < len(items) {
			items = slices.Delete(items, row.Index, row.Index+1)
			keep := c.picker.KeepScrollPosition
			c.picker.KeepScrollPosition = true
			c.picker.SetItems(items)
			c.picker.KeepScrollPosition = keep
		}
	}
}

// QuickNavigating tells whether the picker waits for the release of the keys that opened it.
func (c *Controller) QuickNavigating() bool { return c.visible && c.quickNav != nil }

// QuickMode tells whether the picker was opened as a switcher driven by held keys. It stays true when the
// release was never seen and the picker works as any other.
func (c *Controller) QuickMode() bool { return c.visible && c.quickMode }

// QuickHeld tells whether the modifiers held now are those the quick navigate picker waits for the release of;
// false when it does not wait.
func (c *Controller) QuickHeld(held Modifiers) bool {
	return c.QuickNavigating() && c.quickNav.heldBy(held)
}

// Navigate moves the active row to the next or the previous one, going round at the ends, and waits again for
// the release of the keys (VS Code's navigate; the commands quickOpenNavigateNextInEditorPicker and
// quickOpenNavigatePreviousInEditorPicker run it). With qn nil the picker keeps waiting or not as it was.
func (c *Controller) Navigate(next bool, qn *QuickNavigate) {
	if !c.visible || len(c.picker.Rows()) == 0 {
		return
	}
	if next {
		c.picker.Move(1)
	} else {
		c.picker.Move(-1)
	}
	if qn != nil {
		c.quickNav = qn
	}
}

// QuickNavigateKeyUp tells the controller that a modifier key was released, with the modifiers held after it.
// When the picker waits for that key it chooses the active row and stops waiting (it waits for the release
// only once); it tells whether it did.
func (c *Controller) QuickNavigateKeyUp(released Modifier, held Modifiers) bool {
	if !c.QuickNavigating() || !c.quickNav.triggered(released, held) {
		return false
	}
	c.quickNav = nil
	c.Accept(KeyMods{})
	return true
}

// DisarmQuickNavigate stops waiting for the release of the keys: the release was missed, or the window lost
// the keyboard. The picker goes on as any other: Enter or a click chooses, Escape closes.
func (c *Controller) DisarmQuickNavigate() { c.quickNav = nil }

// RememberValue stores the value as the last input of the provider, for DefaultFilterLast.
func (c *Controller) RememberValue(prefix, value string) { c.last[prefix] = value }

// Hide closes the quick input without a choice.
func (c *Controller) Hide() { c.hide(false) }

func (c *Controller) hide(accepted bool) {
	if !c.visible {
		return
	}
	if accepted && c.provider != nil {
		c.last[c.provider.Info().Prefix] = c.picker.Value()
	}
	c.accepted = accepted
	cancel := c.onCancel
	c.onCancel = nil
	// the pick ends first, with the quick input still shown, so that its hooks see the picker they asked for
	c.releasePick(accepted)
	c.visible = false
	c.quickNav, c.quickMode = nil, false
	c.endPrompt()
	c.endSession(accepted)
	if cancel != nil && !accepted {
		cancel()
	}
	c.fire()
}

func (c *Controller) endSession(accepted bool) {
	s, h := c.session, c.handler
	c.session, c.handler = nil, nil
	if s == nil {
		return
	}
	s.end()
	if h != nil {
		h.Done(accepted)
	}
}
