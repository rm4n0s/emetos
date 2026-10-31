package fakeapi

import (
	"slices"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The Source Control API of the fake (SPEC §10.30, step 24.2): the source controls, groups and resources an extension made, in
// memory, with the rules of the host (internal/gui/exthost/scm.go): the same errors, the same disposal with the activation, the
// same containment of a panic in a callback. extension/scmtest runs one suite on this and on the host.

// scmState is the Source Control state of the fake; the API's mutex guards it.
type scmState struct {
	sources []*FakeSource
	handles int
	// dead is set when the activation ended (Deactivate) or the extension failed; failed only for the latter.
	dead, failed bool
}

// FakeSource is a source control the extension created: a test reads what the view would show and does what the user does.
type FakeSource struct {
	a        *API
	handle   string
	key      string
	id       string
	label    string
	root     string
	ctxValue string

	count      int
	status     []extension.StatusCommand
	busy       bool
	activeName string
	accept     extension.Command
	button     *extension.ActionButton
	input      *FakeInput
	groups     []*FakeGroup
	disposed   bool
}

// FakeInput is the commit message box of a FakeSource.
type FakeInput struct {
	src         *FakeSource
	value       string
	placeholder string
	visible     bool
	enabled     bool
	focusAsks   int
	validate    func(string) *extension.Validation
	listeners   []*func(string)
}

// FakeGroup is a group of resources of a FakeSource.
type FakeGroup struct {
	src          *FakeSource
	id           string
	label        string
	hide         bool
	contextValue string
	resources    []extension.ResourceState
	disposed     bool
}

// SourceControl implements extension.API.
func (a *API) SourceControl() extension.SourceControls { return scmAPI{a} }

type scmAPI struct{ a *API }

// Create implements extension.SourceControls with the errors of the host: ExtensionDeactivated, ExtensionSCMInvalid and
// ExtensionSCMDuplicate.
func (c scmAPI) Create(spec extension.SourceControlSpec) (extension.SourceControl, error) {
	a := c.a
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.scm.dead {
		return nil, errors.New("ExtensionDeactivated", "the extension was deactivated")
	}
	if strings.TrimSpace(spec.ID) == "" || strings.TrimSpace(spec.Label) == "" {
		return nil, errors.New("ExtensionSCMInvalid", "a source control needs an id and a label", "id", spec.ID)
	}
	key := spec.ID + "\x00" + spec.RootURI
	for _, s := range a.scm.sources {
		if s.key == key {
			return nil, errors.New("ExtensionSCMDuplicate", "the extension has a source control with this id and root already", "id", spec.ID, "root", spec.RootURI)
		}
	}
	a.scm.handles++
	s := &FakeSource{a: a, handle: "scm" + strconv.Itoa(a.scm.handles), key: key, id: spec.ID, label: spec.Label, root: spec.RootURI, ctxValue: spec.ContextValue, count: -1}
	s.input = &FakeInput{src: s, visible: true, enabled: true}
	a.scm.sources = append(a.scm.sources, s)
	return s, nil
}

// SourceControls are the source controls that are shown (not disposed, and not ended with the activation), in the order they
// were created.
func (a *API) SourceControls() []*FakeSource {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.scm.sources)
}

// SourceControlByHandle returns the source control with the handle, or nil.
func (a *API) SourceControlByHandle(handle string) *FakeSource {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, s := range a.scm.sources {
		if s.handle == handle {
			return s
		}
	}
	return nil
}

// Deactivate ends the activation as closing the window tab does: the source controls go, and the calls of the API that change
// something fail with ExtensionDeactivated or do nothing.
func (a *API) Deactivate() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.scm.dead = true
	for _, s := range a.scm.sources {
		s.disposed = true
	}
	a.scm.sources = nil
	for _, f := range a.decorations.providers {
		f.disposed = true
	}
	a.decorations.providers = nil
}

// Failed tells whether the fake took the extension off because of a panic in one of its callbacks, as the host does
// (SPEC §10.15 "Containment").
func (a *API) Failed() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.scm.failed
}

// scmFail takes the extension off: what Deactivate does, and Failed says so.
func (a *API) scmFail(what string, panicked any) {
	a.t.Helper()
	a.logger.Error("a callback of the extension panicked and the extension is off", "where", what, "panic", panicked)
	a.mu.Lock()
	a.scm.failed = true
	a.mu.Unlock()
	a.Deactivate()
}

// live reports whether a change of the source control has an effect: the activation is on and the source control is not
// disposed. The caller holds the lock.
func (s *FakeSource) live() bool { return !s.a.scm.dead && !s.disposed }

// Handle implements extension.SourceControl.
func (s *FakeSource) Handle() string { return s.handle }

// SetCount implements extension.SourceControl.
func (s *FakeSource) SetCount(count int) {
	s.a.mu.Lock()
	defer s.a.mu.Unlock()
	if s.live() {
		s.count = max(count, -1)
	}
}

// SetStatusBarCommands implements extension.SourceControl.
func (s *FakeSource) SetStatusBarCommands(commands []extension.StatusCommand) {
	s.a.mu.Lock()
	defer s.a.mu.Unlock()
	if s.live() {
		s.status = slices.Clone(commands)
	}
}

// SetActiveRepositoryName implements extension.SourceControl.
func (s *FakeSource) SetActiveRepositoryName(name string) {
	s.a.mu.Lock()
	defer s.a.mu.Unlock()
	if s.live() {
		s.activeName = name
	}
}

// SetBusy implements extension.SourceControl.
func (s *FakeSource) SetBusy(busy bool) {
	s.a.mu.Lock()
	defer s.a.mu.Unlock()
	if s.live() {
		s.busy = busy
	}
}

// SetAcceptInputCommand implements extension.SourceControl.
func (s *FakeSource) SetAcceptInputCommand(c extension.Command) {
	s.a.mu.Lock()
	defer s.a.mu.Unlock()
	if s.live() {
		s.accept = c
	}
}

// SetActionButton implements extension.SourceControl: the fake keeps its own copy, as the host does.
func (s *FakeSource) SetActionButton(b *extension.ActionButton) {
	s.a.mu.Lock()
	defer s.a.mu.Unlock()
	if !s.live() {
		return
	}
	if b == nil {
		s.button = nil
		return
	}
	c := *b
	c.Secondary = make([][]extension.Command, len(b.Secondary))
	for i, g := range b.Secondary {
		c.Secondary[i] = slices.Clone(g)
	}
	s.button = &c
}

// Input implements extension.SourceControl.
func (s *FakeSource) Input() extension.SCMInput { return s.input }

// Dispose implements extension.SourceControl.
func (s *FakeSource) Dispose() {
	s.a.mu.Lock()
	defer s.a.mu.Unlock()
	s.disposed = true
	s.a.scm.sources = slices.DeleteFunc(s.a.scm.sources, func(o *FakeSource) bool { return o == s })
}

// CreateGroup implements extension.SourceControl with the errors of the host: ExtensionDeactivated, ExtensionSCMGroupInvalid,
// ExtensionSCMDisposed and ExtensionSCMGroupDuplicate.
func (s *FakeSource) CreateGroup(spec extension.ResourceGroupSpec) (extension.ResourceGroup, error) {
	s.a.mu.Lock()
	defer s.a.mu.Unlock()
	if s.a.scm.dead {
		return nil, errors.New("ExtensionDeactivated", "the extension was deactivated")
	}
	if strings.TrimSpace(spec.ID) == "" {
		return nil, errors.New("ExtensionSCMGroupInvalid", "a group of resources needs an id", "source", s.id)
	}
	if s.disposed {
		return nil, errors.New("ExtensionSCMDisposed", "the source control was disposed", "source", s.id)
	}
	for _, g := range s.groups {
		if g.id == spec.ID {
			return nil, errors.New("ExtensionSCMGroupDuplicate", "the source control has a group with this id already", "source", s.id, "group", spec.ID)
		}
	}
	g := &FakeGroup{src: s, id: spec.ID, label: spec.Label, hide: spec.HideWhenEmpty, contextValue: spec.ContextValue}
	s.groups = append(s.groups, g)
	return g, nil
}

// ID, Label, RootURI, ContextValue, Count, StatusBarCommands, Busy, ActiveRepositoryName, AcceptInputCommand, ActionButton and
// Groups say what the view would show.

// ID is the id of the provider.
func (s *FakeSource) ID() string { return s.id }

// Label is the label of the source control.
func (s *FakeSource) Label() string { return s.label }

// RootURI is the root of the repository.
func (s *FakeSource) RootURI() string { return s.root }

// ContextValue is the value of scmProviderContext.
func (s *FakeSource) ContextValue() string { return s.ctxValue }

// Count is the count the extension set, -1 for none.
func (s *FakeSource) Count() int {
	s.a.mu.Lock()
	defer s.a.mu.Unlock()
	return s.count
}

// BadgeCount is the count of the badge of the repository: the count, else the number of resources of the groups.
func (s *FakeSource) BadgeCount() int {
	s.a.mu.Lock()
	defer s.a.mu.Unlock()
	if s.count >= 0 {
		return s.count
	}
	n := 0
	for _, g := range s.groups {
		n += len(g.resources)
	}
	return n
}

// StatusBarCommands are the items of the header.
func (s *FakeSource) StatusBarCommands() []extension.StatusCommand {
	s.a.mu.Lock()
	defer s.a.mu.Unlock()
	return slices.Clone(s.status)
}

// Busy tells whether the progress bar is shown.
func (s *FakeSource) Busy() bool {
	s.a.mu.Lock()
	defer s.a.mu.Unlock()
	return s.busy
}

// ActiveRepositoryName is the name set for the active repository, empty for none.
func (s *FakeSource) ActiveRepositoryName() string {
	s.a.mu.Lock()
	defer s.a.mu.Unlock()
	return s.activeName
}

// AcceptInputCommand is the command of Ctrl+Enter, with an empty ID for none.
func (s *FakeSource) AcceptInputCommand() extension.Command {
	s.a.mu.Lock()
	defer s.a.mu.Unlock()
	return s.accept
}

// ActionButton is the button under the input, nil for none.
func (s *FakeSource) ActionButton() *extension.ActionButton {
	s.a.mu.Lock()
	defer s.a.mu.Unlock()
	if s.button == nil {
		return nil
	}
	c := *s.button
	c.Secondary = make([][]extension.Command, len(s.button.Secondary))
	for i, g := range s.button.Secondary {
		c.Secondary[i] = slices.Clone(g)
	}
	return &c
}

// Groups are the groups that are shown, in the order they were created.
func (s *FakeSource) Groups() []*FakeGroup {
	s.a.mu.Lock()
	defer s.a.mu.Unlock()
	return slices.Clone(s.groups)
}

// Group returns the group with the id, or nil.
func (s *FakeSource) Group(id string) *FakeGroup {
	for _, g := range s.Groups() {
		if g.id == id {
			return g
		}
	}
	return nil
}

// ---- the input ----

// Value implements extension.SCMInput.
func (in *FakeInput) Value() string {
	in.src.a.mu.Lock()
	defer in.src.a.mu.Unlock()
	return in.value
}

// SetValue implements extension.SCMInput: the listeners are called later, when the test pumps, as the host calls them on the
// UI thread.
func (in *FakeInput) SetValue(value string) {
	a := in.src.a
	a.mu.Lock()
	if !in.src.live() || in.value == value {
		a.mu.Unlock()
		return
	}
	in.value = value
	listeners := slices.Clone(in.listeners)
	a.mu.Unlock()
	in.fire(listeners, value)
}

// Type is the user typing in the box: the text becomes the text of the box and the listeners are called when the test pumps.
func (in *FakeInput) Type(text string) { in.SetValue(text) }

func (in *FakeInput) fire(listeners []*func(string), value string) {
	a := in.src.a
	for _, f := range listeners {
		f := f
		a.Post(func() { a.runSCMChange(*f, value) })
	}
}

// runSCMChange calls an OnChange function; a panic takes the extension off.
func (a *API) runSCMChange(fn func(string), value string) {
	a.mu.Lock()
	dead := a.scm.dead
	a.mu.Unlock()
	if dead {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			a.scmFail("OnChange of a source control input", r)
		}
	}()
	fn(value)
}

// OnChange implements extension.SCMInput.
func (in *FakeInput) OnChange(fn func(value string)) (cancel func()) {
	a := in.src.a
	a.mu.Lock()
	defer a.mu.Unlock()
	if fn == nil || a.scm.dead {
		return func() {}
	}
	f := &fn
	in.listeners = append(in.listeners, f)
	return func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		in.listeners = slices.DeleteFunc(in.listeners, func(o *func(string)) bool { return o == f })
	}
}

// SetPlaceholder implements extension.SCMInput.
func (in *FakeInput) SetPlaceholder(placeholder string) {
	in.src.a.mu.Lock()
	defer in.src.a.mu.Unlock()
	if in.src.live() {
		in.placeholder = placeholder
	}
}

// SetValidation implements extension.SCMInput.
func (in *FakeInput) SetValidation(fn func(value string) *extension.Validation) {
	in.src.a.mu.Lock()
	defer in.src.a.mu.Unlock()
	if in.src.live() {
		in.validate = fn
	}
}

// Validate is the view asking for the message under the box: what the validation function says for the text, nil when there is
// none. A panic in the function takes the extension off and gives no message.
func (in *FakeInput) Validate(text string) (v *extension.Validation) {
	a := in.src.a
	a.mu.Lock()
	fn, dead := in.validate, a.scm.dead
	a.mu.Unlock()
	if fn == nil || dead {
		return nil
	}
	defer func() {
		if r := recover(); r != nil {
			v = nil
			a.scmFail("validation of a source control input", r)
		}
	}()
	return fn(text)
}

// SetVisible implements extension.SCMInput.
func (in *FakeInput) SetVisible(visible bool) {
	in.src.a.mu.Lock()
	defer in.src.a.mu.Unlock()
	if in.src.live() {
		in.visible = visible
	}
}

// SetEnabled implements extension.SCMInput.
func (in *FakeInput) SetEnabled(enabled bool) {
	in.src.a.mu.Lock()
	defer in.src.a.mu.Unlock()
	if in.src.live() {
		in.enabled = enabled
	}
}

// Focus implements extension.SCMInput.
func (in *FakeInput) Focus() {
	in.src.a.mu.Lock()
	defer in.src.a.mu.Unlock()
	if in.src.live() {
		in.focusAsks++
	}
}

// Placeholder is the text shown while the box is empty.
func (in *FakeInput) Placeholder() string {
	in.src.a.mu.Lock()
	defer in.src.a.mu.Unlock()
	return in.placeholder
}

// Visible tells whether the box is shown.
func (in *FakeInput) Visible() bool {
	in.src.a.mu.Lock()
	defer in.src.a.mu.Unlock()
	return in.visible
}

// Enabled tells whether the box accepts text.
func (in *FakeInput) Enabled() bool {
	in.src.a.mu.Lock()
	defer in.src.a.mu.Unlock()
	return in.enabled
}

// FocusRequests counts the calls of Focus.
func (in *FakeInput) FocusRequests() int {
	in.src.a.mu.Lock()
	defer in.src.a.mu.Unlock()
	return in.focusAsks
}

// ---- the groups ----

// ID implements extension.ResourceGroup.
func (g *FakeGroup) ID() string { return g.id }

func (g *FakeGroup) live() bool { return g.src.live() && !g.disposed }

// SetResources implements extension.ResourceGroup: the fake keeps its own copy of the list.
func (g *FakeGroup) SetResources(resources []extension.ResourceState) {
	g.src.a.mu.Lock()
	defer g.src.a.mu.Unlock()
	if g.live() {
		g.resources = slices.Clone(resources)
	}
}

// SetLabel implements extension.ResourceGroup.
func (g *FakeGroup) SetLabel(label string) {
	g.src.a.mu.Lock()
	defer g.src.a.mu.Unlock()
	if g.live() {
		g.label = label
	}
}

// SetHideWhenEmpty implements extension.ResourceGroup.
func (g *FakeGroup) SetHideWhenEmpty(hide bool) {
	g.src.a.mu.Lock()
	defer g.src.a.mu.Unlock()
	if g.live() {
		g.hide = hide
	}
}

// Dispose implements extension.ResourceGroup.
func (g *FakeGroup) Dispose() {
	g.src.a.mu.Lock()
	defer g.src.a.mu.Unlock()
	g.disposed = true
	g.src.groups = slices.DeleteFunc(g.src.groups, func(o *FakeGroup) bool { return o == g })
}

// Label is the title of the group.
func (g *FakeGroup) Label() string {
	g.src.a.mu.Lock()
	defer g.src.a.mu.Unlock()
	return g.label
}

// HideWhenEmpty tells whether the group is hidden while it has no resources.
func (g *FakeGroup) HideWhenEmpty() bool {
	g.src.a.mu.Lock()
	defer g.src.a.mu.Unlock()
	return g.hide
}

// ContextValue is the value of scmResourceGroupState.
func (g *FakeGroup) ContextValue() string {
	g.src.a.mu.Lock()
	defer g.src.a.mu.Unlock()
	return g.contextValue
}

// Resources are the resources of the group.
func (g *FakeGroup) Resources() []extension.ResourceState {
	g.src.a.mu.Lock()
	defer g.src.a.mu.Unlock()
	return slices.Clone(g.resources)
}
