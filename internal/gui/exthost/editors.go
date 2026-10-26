package exthost

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"path"
	"slices"
	"strings"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/glob"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// The contributions to the editor area (SPEC §10.15 "As built (step 11.6)"): editor tab kinds, editor title actions and file
// views. The manifest declares them; the scope installs the declarations when the extension is enabled in it, so the
// icons and the micro-tabs are there before the extension runs. A tab is an editorgroup.ExtensionTabInput that the window
// shows; its widget is built by a box (box.go) like a view's, and the box's declaration (viewDecl) keeps the content, the
// activation and the saved state of the tab.

// EditorHost is what the window offers the extensions to show tabs and file views.
type EditorHost interface {
	// OpenTab shows the input in the group with the id (the active group when 0 or gone), making it the active tab
	// unless inactive.
	OpenTab(in *editorgroup.ExtensionTabInput, group int, inactive bool) error
	// CloseTab closes the tab without asking.
	CloseTab(in *editorgroup.ExtensionTabInput)
	// RevealTab makes the tab the active tab of its group, and the group the active group; with focus the keyboard focus
	// goes into it.
	RevealTab(in *editorgroup.ExtensionTabInput, focus bool)
	// GroupOf is the id of the group that shows the tab, 0 for none.
	GroupOf(in *editorgroup.ExtensionTabInput) int
	// OpenFileView shows the document with the URI in the file view, in the active group or the group on its side.
	OpenFileView(uri, viewID string, side bool, group int) error
	// OpenProposed shows the diff editor of a proposed change (SPEC §10.16 "As built (step 13.3)") in the group beside the
	// active one:
	// the original text on the left, read-only, and the proposed text on the right, which can be edited, with Accept and
	// Reject in the title. answer is called once, on the UI thread, when the user accepts (with the text of the right
	// side), rejects, or closes the tab (a rejection). The function it returns closes the tab and the answer is never
	// called after that; it does nothing once the user answered.
	OpenProposed(change extension.ProposedChange, answer func(extension.ProposedResult)) (closeTab func(), err error)
}

// LanguageSource is what the window offers of its languages: the grammar of a language, for highlighting.
type LanguageSource interface {
	// Resolve gives the id of the language called name.
	Resolve(name string) (string, bool)
	// Tokenize tokenizes text with the language's grammar.
	Tokenize(languageID, text string) ([][]extension.Token, error)
}

// kindDecl is an editor kind the manifest of an extension declares.
type kindDecl struct {
	spec     extension.EditorKindSpec
	slot     *slot
	provided *extension.EditorKind
	inst     *instance
}

// actionDecl is an editor title action.
type actionDecl struct {
	spec extension.EditorActionSpec
	code int
	when contextkey.Expr
}

// fileViewDecl is a file view the manifest declares.
type fileViewDecl struct {
	spec     extension.FileViewSpec
	slot     *slot
	provider *extension.FileViewProvider
	inst     *instance
	globs    []interface{ Match(string) bool }
}

// editorDecls are the editor contributions of one extension in one scope; the slot owns them.
type editorDecls struct {
	kinds     []*kindDecl
	actions   []actionDecl
	fileViews []*fileViewDecl
}

// editorTab is a tab of an extension in a scope; it is the extension.EditorTab of the extension that provides its kind.
type editorTab struct {
	scope *Scope
	input *editorgroup.ExtensionTabInput
	decl  *viewDecl
	// inst is the activation that made the content, nil while the tab waits for its extension.
	inst    *instance
	onClose func()
}

var _ extension.EditorTab = (*editorTab)(nil)

// tabKey is the key of the state of the tab in the state of the views.
func tabKey(id string) string { return "editor:" + id }

// newTabID makes the id of a tab: random, so that the ids of two sessions never meet.
func newTabID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		// the system has no randomness: the order of the tabs of the scope is unique enough
		return "t" + strings.Repeat("0", 11)
	}
	return hex.EncodeToString(b[:])
}

// installEditors registers the editor contributions that the manifest of an extension declares and returns the function
// that takes them away.
func (s *Scope) installEditors(sl *slot) (remove func()) {
	c := sl.info.Manifest.Contributes
	m := sl.info.Manifest
	if len(c.EditorKinds) == 0 && len(c.EditorActions) == 0 && len(c.FileViews) == 0 {
		return func() {}
	}
	log := s.log.With("extension", m.ID)
	d := &editorDecls{}
	for _, spec := range c.EditorKinds {
		d.kinds = append(d.kinds, &kindDecl{spec: spec, slot: sl})
	}
	for _, spec := range c.EditorActions {
		when, err := parseWhen(spec.When)
		if err != nil {
			log.Warn("an editor title action of an extension is left out", "command", spec.Command, "err", err)
			continue
		}
		code, _ := iconCodeOf(spec.Icon)
		d.actions = append(d.actions, actionDecl{spec: spec, code: code, when: when})
	}
	for _, spec := range c.FileViews {
		fv := &fileViewDecl{spec: spec, slot: sl}
		for _, g := range spec.Globs {
			fv.globs = append(fv.globs, glob.Parse(g, glob.Options{}))
		}
		d.fileViews = append(d.fileViews, fv)
	}
	s.mu.Lock()
	sl.editors = d
	if s.kindDecls == nil {
		s.kindDecls = map[string]*kindDecl{}
	}
	for _, k := range d.kinds {
		s.kindDecls[k.spec.ID] = k
	}
	s.mu.Unlock()
	s.editorsChanged()
	return func() {
		s.mu.Lock()
		for _, k := range d.kinds {
			if s.kindDecls[k.spec.ID] == k {
				delete(s.kindDecls, k.spec.ID)
			}
		}
		sl.editors = nil
		s.mu.Unlock()
		s.editorsChanged()
	}
}

// iconCodeOf is the code point of a Codicon by name.
func iconCodeOf(name string) (int, bool) {
	code := iconCode(name)
	return code, code != 0
}

// editorsChanged tells the window that the actions, file views or tabs changed.
func (s *Scope) editorsChanged() {
	if f := s.opts.EditorsChanged; f != nil {
		f()
	}
}

// liveSlot tells whether the extension of a slot contributes to the editor area: it is on, and has not failed.
func liveSlot(sl *slot) bool { return sl.state == StateInactive || sl.state == StateActive }

// ---- editor title actions ----

// EditorAction is an editor title action as the window shows it.
type EditorAction struct {
	// Command is the command the icon runs, Icon the code point of its Codicon and Tooltip its hover.
	Command string
	Icon    int
	Tooltip string
}

// EditorActions are the editor title actions whose `when` holds in keys, ordered by extension (the order of activation)
// and then by Order. The extensions that are off or failed contribute none.
func (s *Scope) EditorActions(keys contextkey.Context) []EditorAction {
	s.mu.Lock()
	type pending struct {
		a    actionDecl
		slot int
	}
	var all []pending
	for i, sl := range s.slots {
		if !liveSlot(sl) || sl.editors == nil {
			continue
		}
		for _, a := range sl.editors.actions {
			all = append(all, pending{a: a, slot: i})
		}
	}
	s.mu.Unlock()
	slices.SortStableFunc(all, func(x, y pending) int {
		switch {
		case x.slot != y.slot:
			return x.slot - y.slot
		case x.a.spec.Order < y.a.spec.Order:
			return -1
		case x.a.spec.Order > y.a.spec.Order:
			return 1
		}
		return 0
	})
	var out []EditorAction
	for _, p := range all {
		if p.a.when != nil && keys != nil && !p.a.when.Evaluate(keys) {
			continue
		}
		tooltip := p.a.spec.Tooltip
		if tooltip == "" {
			if meta, ok := s.opts.Commands.Lookup(p.a.spec.Command); ok {
				tooltip = meta.Title
			}
		}
		out = append(out, EditorAction{Command: p.a.spec.Command, Icon: p.a.code, Tooltip: tooltip})
	}
	return out
}

// ---- editor tabs ----

// TabBody is the widget of an editor tab of an extension: a box that builds the content the extension made for the tab,
// or a notice when the extension is off, failed or not part of this Emetos. The first call for a tab registers it; the
// extension is activated when its box is on screen.
func (s *Scope) TabBody(in *editorgroup.ExtensionTabInput) widgets.Widget {
	t := s.registerTab(in)
	s.mu.Lock()
	sl := t.decl.slot
	state := StateInactive
	if sl != nil {
		state = sl.state
	}
	name := in.Extension()
	if sl != nil {
		name = sl.info.Manifest.Name
	}
	s.mu.Unlock()
	switch {
	case sl == nil:
		return notice("This tab needs the extension " + in.Extension() + ", which this Emetos does not have.")
	case state == StateDisabled:
		return notice(name + " is turned off. Turn it on in the Extensions page to use this tab.")
	}
	return newViewBox(s, t.decl)
}

// notice is a centered line of dimmed text.
func notice(text string) widgets.Widget {
	return widgets.NewBuilder(func(bc widgets.BuildContext) widgets.Widget {
		colors := kit.ThemeOf(bc)
		return widgets.NewColoredBox(colors.EditorBackground, widgets.NewCenter(widgets.NewPadding(painting.NewEdgeInsetsAll(16),
			widgets.NewText(text, widgets.TextOpts{Style: kit.TextStyleOf(bc, 13, colors.DescriptionForeground)}))))
	})
}

// RegisterTab makes the scope know the tab of an input (the window calls it when it makes the editor of a restored or a
// copied tab, before the tab is on screen), so that the extension can be told of it when it is activated.
func (s *Scope) RegisterTab(in *editorgroup.ExtensionTabInput) { s.registerTab(in) }

// registerTab makes the tab of an input known to the scope, once; the content comes when the extension provides the kind.
func (s *Scope) registerTab(in *editorgroup.ExtensionTabInput) *editorTab {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t := s.tabs[in.ID()]; t != nil {
		return t
	}
	var sl *slot
	for _, x := range s.slots {
		if x.info.Manifest.ID == in.Extension() {
			sl = x
		}
	}
	t := &editorTab{scope: s, input: in}
	t.decl = &viewDecl{id: tabKey(in.ID()), slot: sl, tab: t, event: extension.OnEditor(in.Kind())}
	if s.tabs == nil {
		s.tabs = map[string]*editorTab{}
	}
	if s.viewDecls == nil {
		s.viewDecls = map[string]*viewDecl{}
	}
	s.tabs[in.ID()] = t
	s.viewDecls[t.decl.id] = t.decl
	in.SetCopier(t.copy)
	return t
}

// TabClosed is called when the tab of an input is closed for good: the extension is told, and the tab and its state are
// forgotten.
func (s *Scope) TabClosed(in *editorgroup.ExtensionTabInput) {
	s.mu.Lock()
	t := s.tabs[in.ID()]
	if t == nil {
		s.mu.Unlock()
		return
	}
	delete(s.tabs, in.ID())
	delete(s.viewDecls, t.decl.id)
	delete(s.viewStates, t.decl.id)
	onClose, inst := t.onClose, t.inst
	t.onClose = nil
	t.decl.content, t.decl.inst = nil, nil
	s.mu.Unlock()
	if onClose != nil && inst != nil && !inst.isDead() {
		s.runTabCallback(inst, t, "OnClose", onClose)
	}
}

// runTabCallback calls a function an extension gave for a tab; a panic fails the extension with
// ExtensionEditorCallbackPanicked.
func (s *Scope) runTabCallback(inst *instance, t *editorTab, what string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			failure := errors.NewErrf("ExtensionEditorCallbackPanicked", "it panicked in %s of an editor tab: %v", what, r).SetMetadata("extension", inst.id, "tab", t.input.ID(), "scope", s.opts.Info.Name)
			s.post(func() { s.fail(inst.slot, failure, "editor tab "+what) })
		}
	}()
	f()
}

// copy makes the input of a copy of the tab (a split): another instance whose content is made with the state this one
// saves now.
func (t *editorTab) copy() *editorgroup.ExtensionTabInput {
	s := t.scope
	s.saveView(t.decl)
	in := editorgroup.NewExtensionTabInput(t.input.Extension(), t.input.Kind(), newTabID(), t.input.Name(), t.input.Icon())
	s.mu.Lock()
	raw := s.viewStates[t.decl.id]
	if s.viewStates == nil {
		s.viewStates = map[string]json.RawMessage{}
	}
	if len(raw) > 0 {
		s.viewStates[tabKey(in.ID())] = slices.Clone(raw)
	}
	s.mu.Unlock()
	nt := s.registerTab(in)
	s.mu.Lock()
	kd := s.kindDecls[in.Kind()]
	s.mu.Unlock()
	if kd != nil && kd.provided != nil && kd.inst != nil && !kd.inst.isDead() {
		_ = s.makeContent(nt, kd, slices.Clone(raw))
	}
	return in
}

// makeContent asks the kind for the content of a tab with the state; a panic fails the extension (the error is nil, the
// extension has been told), an error is returned and the tab shows it.
func (s *Scope) makeContent(t *editorTab, kd *kindDecl, raw json.RawMessage) (err error) {
	inst, create := kd.inst, kd.provided.Create
	// the tab answers the extension while Create runs, so that it can set the title
	s.mu.Lock()
	t.inst = inst
	s.mu.Unlock()
	var content extension.EditorTabContent
	func() {
		defer func() {
			if r := recover(); r != nil {
				failure := errors.NewErrf("ExtensionEditorCreatePanicked", "it panicked in Create of the editor kind %s: %v", kd.spec.ID, r).SetMetadata("extension", inst.id, "kind", kd.spec.ID, "scope", s.opts.Info.Name)
				s.post(func() { s.fail(kd.slot, failure, "editor kind "+kd.spec.ID) })
				err = failure
			}
		}()
		content, err = create(t, slices.Clone(raw))
	}()
	if err != nil {
		s.mu.Lock()
		t.inst = nil
		s.mu.Unlock()
		return err
	}
	if content.Build == nil {
		s.mu.Lock()
		t.inst = nil
		s.mu.Unlock()
		return errors.New("ExtensionEditorContentInvalid", "the content of an editor tab needs a Build function", "extension", inst.id, "kind", kd.spec.ID)
	}
	s.mu.Lock()
	t.inst = inst
	t.onClose = content.OnClose
	t.decl.content = &extension.ViewContent{Build: content.Build, SaveState: content.SaveState}
	t.decl.inst = inst
	t.decl.provided = true
	s.mu.Unlock()
	s.post(func() { s.refreshDecl(t.decl) })
	return nil
}

// refreshDecl builds the boxes of a declaration again.
func (s *Scope) refreshDecl(d *viewDecl) {
	s.mu.Lock()
	boxes := append([]*viewBoxState(nil), d.boxes...)
	s.mu.Unlock()
	for _, b := range boxes {
		b.refresh()
	}
}

// ID implements extension.EditorTab.
func (t *editorTab) ID() string { return t.input.ID() }

// Kind implements extension.EditorTab.
func (t *editorTab) Kind() string { return t.input.Kind() }

func (t *editorTab) live() bool { return t.inst != nil && !t.inst.isDead() }

// SetTitle implements extension.EditorTab.
func (t *editorTab) SetTitle(title string) {
	if t.live() {
		t.input.SetTitle(title)
	}
}

// SetHint implements extension.EditorTab.
func (t *editorTab) SetHint(hint string) {
	if t.live() {
		t.input.SetHint(hint)
	}
}

// SetDirty implements extension.EditorTab.
func (t *editorTab) SetDirty(dirty bool) {
	if t.live() {
		t.input.SetDirty(dirty)
	}
}

// SetBusy implements extension.EditorTab.
func (t *editorTab) SetBusy(busy bool) {
	if t.live() {
		t.input.SetBusy(busy)
	}
}

// Refresh implements extension.EditorTab.
func (t *editorTab) Refresh() {
	if t.live() {
		t.scope.post(func() { t.scope.refreshDecl(t.decl) })
	}
}

// Reveal implements extension.EditorTab.
func (t *editorTab) Reveal(focus bool) {
	if h := t.scope.opts.Editors; h != nil && t.live() {
		t.scope.post(func() { h.RevealTab(t.input, focus) })
	}
}

// Group implements extension.EditorTab.
func (t *editorTab) Group() int {
	if h := t.scope.opts.Editors; h != nil {
		return h.GroupOf(t.input)
	}
	return 0
}

// Close implements extension.EditorTab.
func (t *editorTab) Close() {
	if h := t.scope.opts.Editors; h != nil && t.live() {
		t.scope.post(func() { h.CloseTab(t.input) })
	}
}

// editorsAPI is the extension.Editors of an activation.
type editorsAPI struct{ inst *instance }

// Editors implements extension.API.
func (a *api) Editors() extension.Editors { return editorsAPI{a.inst} }

// Provide implements extension.Editors.
func (e editorsAPI) Provide(kindID string, kind extension.EditorKind) error {
	in := e.inst
	if in.isDead() {
		return errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	s := in.scope
	s.mu.Lock()
	kd := s.kindDecls[kindID]
	switch {
	case kd == nil || kd.slot != in.slot:
		s.mu.Unlock()
		return errors.New("ExtensionEditorKindNotDeclared", "the manifest of the extension does not declare this editor kind", "extension", in.id, "kind", kindID)
	case kind.Create == nil:
		s.mu.Unlock()
		return errors.New("ExtensionEditorKindInvalid", "an editor kind needs a Create function", "extension", in.id, "kind", kindID)
	case kd.provided != nil && kd.inst == in:
		s.mu.Unlock()
		return errors.New("ExtensionEditorKindProvided", "the activation provided this editor kind already", "extension", in.id, "kind", kindID)
	}
	kd.provided, kd.inst = &kind, in
	var waiting []*editorTab
	for _, t := range s.tabs {
		if t.input.Kind() == kindID && t.decl.content == nil {
			waiting = append(waiting, t)
		}
	}
	s.mu.Unlock()
	in.track(func() { s.releaseKind(kd, in) })
	for _, t := range waiting {
		s.mu.Lock()
		raw := slices.Clone(s.viewStates[t.decl.id])
		s.mu.Unlock()
		if err := s.makeContent(t, kd, raw); err != nil {
			in.log.Warn("an editor tab could not be restored", "kind", kindID, "tab", t.input.ID(), "err", err)
			s.showTabError(t, err)
		}
	}
	return nil
}

// showTabError puts the text of an error in the place of the content of a tab.
func (s *Scope) showTabError(t *editorTab, err error) {
	msg := describe(err)
	s.mu.Lock()
	t.decl.content = &extension.ViewContent{Build: func(bc widgets.BuildContext) widgets.Widget {
		return notice("This tab could not be made: " + msg)
	}}
	t.decl.inst = t.scope.instOf(t.decl)
	s.mu.Unlock()
	s.post(func() { s.refreshDecl(t.decl) })
}

// instOf is the instance of the extension of a declaration, for notices that need one.
func (s *Scope) instOf(d *viewDecl) *instance {
	if d.slot == nil {
		return nil
	}
	return d.slot.inst
}

// releaseKind takes the content of the tabs of a kind away, saving their state first, when the activation that provided it
// ends. The tabs stay, waiting for the extension.
func (s *Scope) releaseKind(kd *kindDecl, in *instance) {
	s.mu.Lock()
	var tabs []*editorTab
	for _, t := range s.tabs {
		if t.inst == in {
			tabs = append(tabs, t)
		}
	}
	s.mu.Unlock()
	for _, t := range tabs {
		s.saveViewWhile(t.decl, true)
		s.mu.Lock()
		t.decl.content, t.decl.inst, t.decl.provided, t.inst, t.onClose = nil, nil, false, nil, nil
		s.mu.Unlock()
		s.post(func() { s.refreshDecl(t.decl) })
	}
	s.mu.Lock()
	if kd.inst == in {
		kd.provided, kd.inst = nil, nil
	}
	s.mu.Unlock()
}

// Open implements extension.Editors.
func (e editorsAPI) Open(kindID string, state any, opts extension.EditorOpenOptions) (extension.EditorTab, error) {
	in := e.inst
	if in.isDead() {
		return nil, errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	s := in.scope
	host := s.opts.Editors
	if host == nil {
		return nil, errors.New("ExtensionEditorsUnavailable", "the scope has no editor area", "extension", in.id)
	}
	s.mu.Lock()
	kd := s.kindDecls[kindID]
	ok := kd != nil && kd.slot == in.slot && kd.provided != nil && kd.inst == in
	s.mu.Unlock()
	if !ok {
		return nil, errors.New("ExtensionEditorKindNotProvided", "the activation did not provide this editor kind", "extension", in.id, "kind", kindID)
	}
	var raw json.RawMessage
	if state != nil {
		data, err := json.Marshal(state)
		if err != nil {
			return nil, errors.NewErr("ExtensionEditorStateInvalid", err).SetMetadata("extension", in.id, "kind", kindID)
		}
		raw = data
	}
	input := editorgroup.NewExtensionTabInput(in.id, kindID, newTabID(), kd.spec.Name, kd.spec.Icon)
	t := s.registerTab(input)
	if len(raw) > 0 {
		s.mu.Lock()
		if s.viewStates == nil {
			s.viewStates = map[string]json.RawMessage{}
		}
		s.viewStates[t.decl.id] = slices.Clone(raw)
		s.mu.Unlock()
	}
	if err := s.makeContent(t, kd, raw); err != nil {
		s.dropTab(t)
		return nil, err
	}
	if err := host.OpenTab(input, opts.Group, opts.Inactive); err != nil {
		s.dropTab(t)
		return nil, err
	}
	return t, nil
}

// dropTab forgets a tab that was never shown.
func (s *Scope) dropTab(t *editorTab) {
	s.mu.Lock()
	delete(s.tabs, t.input.ID())
	delete(s.viewDecls, t.decl.id)
	delete(s.viewStates, t.decl.id)
	s.mu.Unlock()
}

// Tabs implements extension.Editors.
func (e editorsAPI) Tabs(kindID string) []extension.EditorTab {
	s := e.inst.scope
	s.mu.Lock()
	defer s.mu.Unlock()
	var tabs []*editorTab
	for _, t := range s.tabs {
		if t.input.Kind() == kindID && t.decl.content != nil && t.inst == e.inst {
			tabs = append(tabs, t)
		}
	}
	slices.SortFunc(tabs, func(a, b *editorTab) int { return strings.Compare(a.input.ID(), b.input.ID()) })
	out := make([]extension.EditorTab, len(tabs))
	for i, t := range tabs {
		out[i] = t
	}
	return out
}

// OpenProposedChange implements extension.Editors.
func (e editorsAPI) OpenProposedChange(ctx context.Context, change extension.ProposedChange) (extension.ProposedResult, error) {
	in := e.inst
	if in.isDead() {
		return extension.ProposedResult{}, errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	s := in.scope
	host := s.opts.Editors
	if host == nil {
		return extension.ProposedResult{}, errors.New("ExtensionEditorsUnavailable", "the scope has no editor area", "extension", in.id)
	}
	if change.URI == "" {
		return extension.ProposedResult{}, errors.New("ExtensionProposedChangeInvalid", "a proposed change needs the URI of its file", "extension", in.id)
	}
	type opened struct {
		closeTab func()
		err      error
	}
	answered := make(chan extension.ProposedResult, 1)
	openedCh := make(chan opened, 1)
	s.post(func() {
		if in.isDead() {
			openedCh <- opened{err: errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)}
			return
		}
		closeTab, err := host.OpenProposed(change, func(r extension.ProposedResult) { answered <- r })
		openedCh <- opened{closeTab: closeTab, err: err}
	})
	var closeTab func()
	select {
	case o := <-openedCh:
		if o.err != nil {
			return extension.ProposedResult{}, o.err
		}
		closeTab = o.closeTab
	case <-ctx.Done():
	case <-in.ctx.Done():
	}
	if closeTab == nil {
		// cancelled while the tab was being opened: it is closed as soon as it is there
		go func() {
			if o := <-openedCh; o.closeTab != nil {
				s.post(o.closeTab)
			}
		}()
	} else {
		select {
		case r := <-answered:
			return r, nil
		case <-ctx.Done():
		case <-in.ctx.Done():
		}
		s.post(closeTab)
	}
	cause := ctx.Err()
	if cause == nil {
		cause = in.ctx.Err()
	}
	return extension.ProposedResult{}, errors.NewErr("ExtensionProposedChangeCancelled", cause).SetMetadata("extension", in.id, "uri", change.URI)
}

// ---- file views ----

// FileViewInfo is a file view that is selected for a file, as the window shows it in the micro-tabs.
type FileViewInfo struct {
	// ID is the id of the view, Name its label and Icon the code point of its Codicon (0 for none).
	ID   string
	Name string
	Icon int
}

// FileViewsFor are the file views of the extensions that are on that select a file, in the order of the micro-tabs: by
// Order (views without one last) and then by extension. folder is the folder of the workspace, to match globs against the
// path relative to it. Text is not among them.
func (s *Scope) FileViewsFor(file, languageID, folder string) []FileViewInfo {
	base := path.Base(strings.ReplaceAll(file, "\\", "/"))
	rel := ""
	if folder != "" {
		if r, ok := strings.CutPrefix(file, strings.TrimSuffix(folder, "/")+"/"); ok {
			rel = r
		}
	}
	type pending struct {
		info  FileViewInfo
		order int
		slot  int
	}
	var all []pending
	s.mu.Lock()
	for i, sl := range s.slots {
		if !liveSlot(sl) || sl.editors == nil {
			continue
		}
		for _, fv := range sl.editors.fileViews {
			if !fv.selects(base, rel, languageID) {
				continue
			}
			all = append(all, pending{info: FileViewInfo{ID: fv.spec.ID, Name: fv.spec.Name, Icon: iconCode(fv.spec.Icon)}, order: orderOf(fv.spec.Order), slot: i})
		}
	}
	s.mu.Unlock()
	slices.SortStableFunc(all, func(a, b pending) int {
		if a.order != b.order {
			return a.order - b.order
		}
		return a.slot - b.slot
	})
	out := make([]FileViewInfo, len(all))
	for i, p := range all {
		out[i] = p.info
	}
	return out
}

// selects tells whether the view is for a file with the base name, the path relative to the folder and the language.
func (fv *fileViewDecl) selects(base, rel, languageID string) bool {
	if languageID != "" && slices.Contains(fv.spec.Languages, languageID) {
		return true
	}
	for _, g := range fv.globs {
		if g.Match(base) || (rel != "" && g.Match(rel)) {
			return true
		}
	}
	return false
}

// fileViewDecl finds the declaration of a file view among the extensions that are on.
func (s *Scope) fileViewDecl(viewID string) *fileViewDecl {
	for _, sl := range s.slots {
		if !liveSlot(sl) || sl.editors == nil {
			continue
		}
		for _, fv := range sl.editors.fileViews {
			if fv.spec.ID == viewID {
				return fv
			}
		}
	}
	return nil
}

// fileViewInstance is a file view in one editor tab.
type fileViewInstance struct {
	key    any
	viewID string
	decl   *viewDecl
	fv     *fileViewDecl
	doc    extension.DocumentHandle
	// failed says that Open failed or panicked: the tab shows Text.
	failed bool
	close  func()
	// opened says that the provider's Open was called.
	opened bool
}

// fileViewTab is the extension.FileViewTab of an instance.
type fileViewTab struct {
	s *Scope
	d *viewDecl
}

// Refresh implements extension.FileViewTab.
func (t fileViewTab) Refresh() { t.s.post(func() { t.s.refreshDecl(t.d) }) }

// FileViewBody is the widget of a file view in the editor tab identified by key (any comparable value; the window uses the
// tab's input), showing the document. The extension is activated when the widget is on screen. It reports false when the
// view is not available: its extension is off or failed, it does not select the file, or the view failed to open; the window
// shows Text then.
func (s *Scope) FileViewBody(key any, viewID string, doc extension.DocumentHandle) (widgets.Widget, bool) {
	s.mu.Lock()
	fv := s.fileViewDecl(viewID)
	if fv == nil {
		s.mu.Unlock()
		return nil, false
	}
	var inst *fileViewInstance
	for _, x := range s.fileViewInstances {
		if x.key == key && x.viewID == viewID {
			inst = x
		}
	}
	if inst == nil {
		d := &viewDecl{id: "fileview:" + viewID + "@" + newTabID(), slot: fv.slot, event: extension.OnView(viewID)}
		inst = &fileViewInstance{key: key, viewID: viewID, decl: d, fv: fv, doc: guardedDoc{DocumentHandle: doc, s: s, fv: fv}}
		s.fileViewInstances = append(s.fileViewInstances, inst)
		if s.viewDecls == nil {
			s.viewDecls = map[string]*viewDecl{}
		}
		s.viewDecls[d.id] = d
	}
	failed := inst.failed
	s.mu.Unlock()
	if failed {
		return nil, false
	}
	_ = s.openFileView(inst)
	s.mu.Lock()
	failed = inst.failed
	s.mu.Unlock()
	if failed {
		return nil, false
	}
	return newViewBox(s, inst.decl), true
}

// openFileView calls the provider's Open for an instance, once, when the extension has provided the view. A failure marks the
// instance failed (the tab shows Text) and is returned.
func (s *Scope) openFileView(inst *fileViewInstance) error {
	s.mu.Lock()
	fv := inst.fv
	if inst.opened || fv.provider == nil || fv.inst == nil || fv.inst.isDead() {
		s.mu.Unlock()
		return nil
	}
	inst.opened = true
	provider, ext := fv.provider, fv.inst
	s.mu.Unlock()
	var content extension.FileViewContent
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				failure := errors.NewErrf("ExtensionFileViewPanicked", "it panicked in Open of the file view %s: %v", inst.viewID, r).SetMetadata("extension", ext.id, "view", inst.viewID, "scope", s.opts.Info.Name)
				s.post(func() { s.fail(fv.slot, failure, "file view "+inst.viewID) })
				err = failure
			}
		}()
		content, err = provider.Open(inst.doc, fileViewTab{s: s, d: inst.decl})
	}()
	if err == nil && content.Build == nil {
		err = errors.New("ExtensionFileViewContentInvalid", "the content of a file view needs a Build function", "extension", ext.id, "view", inst.viewID)
	}
	s.mu.Lock()
	if err != nil {
		inst.failed = true
		s.mu.Unlock()
		ext.log.Warn("a file view could not be opened; the tab shows the text", "view", inst.viewID, "err", describe(err))
		s.post(s.editorsChanged)
		return err
	}
	inst.close = content.Close
	inst.decl.content = &extension.ViewContent{Build: content.Build}
	inst.decl.inst = ext
	s.mu.Unlock()
	return nil
}

// ReleaseFileViews ends the file views of an editor tab, when it closes, and the view when only viewID is given and the
// tab goes back to Text. An empty viewID releases all of the tab's views.
func (s *Scope) ReleaseFileViews(key any, viewID string) {
	s.mu.Lock()
	var gone []*fileViewInstance
	s.fileViewInstances = slices.DeleteFunc(s.fileViewInstances, func(x *fileViewInstance) bool {
		if x.key == key && (viewID == "" || x.viewID == viewID) {
			gone = append(gone, x)
			delete(s.viewDecls, x.decl.id)
			return true
		}
		return false
	})
	s.mu.Unlock()
	for _, x := range gone {
		s.closeFileView(x)
	}
}

// closeFileView calls Close of the content of a file view instance.
func (s *Scope) closeFileView(x *fileViewInstance) {
	s.mu.Lock()
	closeFn, ext := x.close, x.decl.inst
	x.close = nil
	s.mu.Unlock()
	if closeFn == nil || ext == nil || ext.isDead() {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			failure := errors.NewErrf("ExtensionFileViewPanicked", "it panicked in Close of the file view %s: %v", x.viewID, r).SetMetadata("extension", ext.id, "view", x.viewID, "scope", s.opts.Info.Name)
			s.post(func() { s.fail(x.fv.slot, failure, "file view "+x.viewID) })
		}
	}()
	closeFn()
}

// fileViewsAPI is the extension.FileViews of an activation.
type fileViewsAPI struct{ inst *instance }

// FileViews implements extension.API.
func (a *api) FileViews() extension.FileViews { return fileViewsAPI{a.inst} }

// Provide implements extension.FileViews.
func (f fileViewsAPI) Provide(viewID string, p extension.FileViewProvider) error {
	in := f.inst
	if in.isDead() {
		return errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	s := in.scope
	s.mu.Lock()
	var fv *fileViewDecl
	if in.slot.editors != nil {
		for _, x := range in.slot.editors.fileViews {
			if x.spec.ID == viewID {
				fv = x
			}
		}
	}
	switch {
	case fv == nil:
		s.mu.Unlock()
		return errors.New("ExtensionFileViewNotDeclared", "the manifest of the extension does not declare this file view", "extension", in.id, "view", viewID)
	case p.Open == nil:
		s.mu.Unlock()
		return errors.New("ExtensionFileViewInvalid", "a file view needs an Open function", "extension", in.id, "view", viewID)
	case fv.provider != nil && fv.inst == in:
		s.mu.Unlock()
		return errors.New("ExtensionFileViewProvided", "the activation provided this file view already", "extension", in.id, "view", viewID)
	}
	fv.provider, fv.inst = &p, in
	var waiting []*fileViewInstance
	for _, x := range s.fileViewInstances {
		if x.fv == fv {
			waiting = append(waiting, x)
		}
	}
	s.mu.Unlock()
	in.track(func() { s.releaseFileView(fv, in) })
	for _, x := range waiting {
		_ = s.openFileView(x)
		s.post(func() { s.refreshDecl(x.decl) })
	}
	return nil
}

// releaseFileView takes the content of a file view away when the activation that provided it ends; the tabs fall back to
// Text, which the window learns from editorsChanged.
func (s *Scope) releaseFileView(fv *fileViewDecl, in *instance) {
	s.mu.Lock()
	var mine []*fileViewInstance
	for _, x := range s.fileViewInstances {
		if x.fv == fv {
			mine = append(mine, x)
		}
	}
	s.mu.Unlock()
	for _, x := range mine {
		s.closeFileView(x)
		s.mu.Lock()
		x.decl.content, x.decl.inst, x.opened = nil, nil, false
		s.mu.Unlock()
	}
	s.mu.Lock()
	if fv.inst == in {
		fv.provider, fv.inst = nil, nil
	}
	s.mu.Unlock()
	s.post(s.editorsChanged)
}

// Open implements extension.FileViews.
func (f fileViewsAPI) Open(uri, viewID string, opts extension.FileViewOpenOptions) error {
	in := f.inst
	if in.isDead() {
		return errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	host := in.scope.opts.Editors
	if host == nil {
		return errors.New("ExtensionEditorsUnavailable", "the scope has no editor area", "extension", in.id)
	}
	return host.OpenFileView(uri, viewID, opts.Side, opts.Group)
}

// ---- languages ----

type languagesAPI struct{ inst *instance }

// Languages implements extension.API.
func (a *api) Languages() extension.Languages { return languagesAPI{a.inst} }

// Resolve implements extension.Languages.
func (l languagesAPI) Resolve(name string) (string, bool) {
	src := l.inst.scope.opts.Languages
	if src == nil {
		return "", false
	}
	return src.Resolve(name)
}

// Tokenize implements extension.Languages.
func (l languagesAPI) Tokenize(languageID, text string) ([][]extension.Token, error) {
	src := l.inst.scope.opts.Languages
	if src == nil {
		return nil, errors.New("ExtensionLanguageUnknown", "the scope has no languages", "extension", l.inst.id, "language", languageID)
	}
	return src.Tokenize(languageID, text)
}

// guardedDoc is the document handle a file view gets: the callbacks the view registers are called under a guard, so that a
// panic in one fails the extension (ExtensionFileViewPanicked) instead of reaching the editor that changed the document.
type guardedDoc struct {
	extension.DocumentHandle
	s  *Scope
	fv *fileViewDecl
}

// fail fails the extension of the view with the value a callback panicked with.
func (g guardedDoc) fail(r any) {
	id := g.fv.spec.ID
	failure := errors.NewErrf("ExtensionFileViewPanicked", "it panicked in a callback of the file view %s: %v", id, r).SetMetadata("extension", g.fv.slot.info.Manifest.ID, "view", id, "scope", g.s.opts.Info.Name)
	g.s.post(func() { g.s.fail(g.fv.slot, failure, "file view "+id) })
}

// OnChange implements extension.DocumentHandle.
func (g guardedDoc) OnChange(fn func(extension.ViewDocumentChange)) (cancel func()) {
	return g.DocumentHandle.OnChange(func(c extension.ViewDocumentChange) {
		defer func() {
			if r := recover(); r != nil {
				g.fail(r)
			}
		}()
		fn(c)
	})
}

// OnScroll implements extension.DocumentHandle.
func (g guardedDoc) OnScroll(fn func(topLine int)) (cancel func()) {
	return g.DocumentHandle.OnScroll(func(line int) {
		defer func() {
			if r := recover(); r != nil {
				g.fail(r)
			}
		}()
		fn(line)
	})
}
