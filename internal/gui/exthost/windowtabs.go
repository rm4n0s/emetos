package exthost

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
)

// The window tabs of the extensions (SPEC §10.15 "As built (step 11.7)"). The manifest declares the kinds; the window lists
// them in the "+" menu, makes the scope of a tab (Info.Kind ScopeWindowTab, Options.Only the owner of the kind) when one
// is opened or comes back, and shows Scope.WindowTabBody as the whole area below the title bar. The content of the tab is
// the viewDecl of the scope (views.go) with the box, the saved state and the failure of a view; this file adds the kind,
// the API of the extension and the calls the window makes.

// windowTabViewID is the id of the view declaration that stands for the one tab of a window tab scope; it is the value of
// the context key focusedView while the content of the tab has the keyboard focus (WindowTabViewID).
const windowTabViewID = "windowTab"

// WindowTabViewID is the value of the context key focusedView while the content of a window tab has the keyboard focus.
const WindowTabViewID = windowTabViewID

// WindowTabHost is what the window offers the extension of a window tab: the strip's view of the tab.
type WindowTabHost interface {
	// SetTitle and SetHint change the name on the tab and the text of its tooltip.
	SetTitle(title string)
	SetHint(hint string)
	// StateChanged asks the window to write the state of the tab soon.
	StateChanged()
	// CaptureKeys sets whether the shortcuts of the tab leave the keys to its content (extension.WindowTab.CaptureKeys).
	CaptureKeys(capture bool)
	// Close closes the tab without asking.
	Close()
}

// WindowTabKindInfo is a kind of window tab as the "+" menu lists it.
type WindowTabKindInfo struct {
	// Extension and ExtensionName are the id and the name of the extension that owns the kind.
	Extension, ExtensionName string
	// Spec is the declaration of the kind.
	Spec extension.WindowTabKindSpec
}

// Label is the text of the entry of the "+" menu.
func (k WindowTabKindInfo) Label() string {
	if k.Spec.PlusMenuLabel != "" {
		return k.Spec.PlusMenuLabel
	}
	return k.Spec.Title
}

// WindowTabKinds lists the kinds of window tab of the extensions that are enabled (not in disabled), in activation order.
func (h *Host) WindowTabKinds(disabled []string) []WindowTabKindInfo {
	var out []WindowTabKindInfo
	for _, in := range h.infos {
		if slices.Contains(disabled, in.Manifest.ID) {
			continue
		}
		for _, k := range in.Manifest.Contributes.WindowTabKinds {
			out = append(out, WindowTabKindInfo{Extension: in.Manifest.ID, ExtensionName: in.Manifest.Name, Spec: k})
		}
	}
	return out
}

// WindowTabKind finds a kind of window tab by its id, whether or not its extension is enabled.
func (h *Host) WindowTabKind(id string) (WindowTabKindInfo, bool) {
	for _, in := range h.infos {
		for _, k := range in.Manifest.Contributes.WindowTabKinds {
			if k.ID == id {
				return WindowTabKindInfo{Extension: in.Manifest.ID, ExtensionName: in.Manifest.Name, Spec: k}, true
			}
		}
	}
	return WindowTabKindInfo{}, false
}

// windowTab is the one window tab of a window tab scope; it is the extension.WindowTab of the extension that owns it.
type windowTab struct {
	scope *Scope
	spec  extension.WindowTabKindSpec
	id    string
	decl  *viewDecl
	// inst is the activation that made the content, nil while the tab waits for its extension.
	inst    *instance
	onClose func()
}

var _ extension.WindowTab = (*windowTab)(nil)

// initWindowTab makes the tab of a window tab scope, from the kind its Info names; it does nothing when the owner does not
// declare the kind (the scope then has no content and the window shows a notice).
func (s *Scope) initWindowTab() {
	info := s.opts.Info
	for _, sl := range s.slots {
		for _, k := range sl.info.Manifest.Contributes.WindowTabKinds {
			if k.ID != info.WindowTab {
				continue
			}
			_, id, _ := strings.Cut(info.ID, ":")
			wt := &windowTab{scope: s, spec: k, id: id}
			wt.decl = &viewDecl{id: windowTabViewID, slot: sl, event: extension.OnWindowTab(k.ID)}
			s.wtab = wt
			s.viewDecls = map[string]*viewDecl{windowTabViewID: wt.decl}
			return
		}
	}
}

// WindowTabBody is the widget of the window tab of the scope: the box that builds the content its extension made, or a
// notice when the extension is turned off or not part of this Emetos. Showing the box activates the extension. It is nil
// for a scope that is not a window tab.
func (s *Scope) WindowTabBody() widgets.Widget {
	s.mu.Lock()
	wt := s.wtab
	s.mu.Unlock()
	if wt == nil {
		return notice("This tab needs the extension that made it, which this Emetos does not have.")
	}
	s.mu.Lock()
	state, name := wt.decl.slot.state, wt.decl.slot.info.Manifest.Name
	s.mu.Unlock()
	if state == StateDisabled {
		return notice(name + " is turned off. Turn it on in the Extensions page to use this tab.")
	}
	return newViewBox(s, wt.decl)
}

// windowTabChanged builds the box of the window tab again and tells the window to build its body again: the state of the
// extension changed (it failed, was turned off or on).
func (s *Scope) windowTabChanged() {
	s.mu.Lock()
	wt := s.wtab
	s.mu.Unlock()
	if wt == nil {
		return
	}
	s.post(func() {
		s.refreshDecl(wt.decl)
		if s.opts.EditorsChanged != nil {
			s.opts.EditorsChanged()
		}
	})
}

// WindowTabState is the state the content of the window tab saved, as JSON, or nil for none: the content is asked for it
// now when it is there, and what it saved when it went off screen or its extension ended is returned otherwise. Call it
// on the UI thread.
func (s *Scope) WindowTabState() json.RawMessage {
	s.mu.Lock()
	wt := s.wtab
	s.mu.Unlock()
	if wt == nil {
		return nil
	}
	return s.ViewStates()[wt.decl.id]
}

// SetWindowTabState gives the tab the state it saved in an earlier session; the content gets it when it is made. Call it
// before Start.
func (s *Scope) SetWindowTabState(raw json.RawMessage) {
	s.mu.Lock()
	wt := s.wtab
	s.mu.Unlock()
	if wt == nil || len(raw) == 0 {
		return
	}
	s.SetViewStates(map[string]json.RawMessage{wt.decl.id: raw})
}

// WindowTabClosed is what the window calls when the tab closes for good (not when Emetos quits): the content is told
// (OnClose) and its state is dropped. A panic in OnClose fails the extension with ExtensionWindowTabCallbackPanicked.
func (s *Scope) WindowTabClosed() {
	s.mu.Lock()
	wt := s.wtab
	if wt == nil {
		s.mu.Unlock()
		return
	}
	onClose, inst := wt.onClose, wt.inst
	wt.onClose = nil
	// the content is gone, so that asking for the state of the scope does not save it again
	wt.decl.content, wt.decl.provided = nil, false
	delete(s.viewStates, wt.decl.id)
	s.mu.Unlock()
	if onClose != nil && inst != nil && !inst.isDead() {
		s.runWindowTabCallback(inst, wt, "OnClose", onClose)
	}
}

// runWindowTabCallback calls a function an extension gave for its window tab; a panic fails the extension with
// ExtensionWindowTabCallbackPanicked.
func (s *Scope) runWindowTabCallback(inst *instance, wt *windowTab, what string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			failure := errors.NewErrf("ExtensionWindowTabCallbackPanicked", "it panicked in %s of a window tab: %v", what, r).SetMetadata("extension", inst.id, "tab", wt.id, "scope", s.opts.Info.Name)
			s.post(func() { s.fail(inst.slot, failure, "window tab "+what) })
		}
	}()
	f()
}

// live tells whether the extension that made the content is running.
func (t *windowTab) live() bool { return t.inst != nil && !t.inst.isDead() }

// ID implements extension.WindowTab.
func (t *windowTab) ID() string { return t.id }

// Kind implements extension.WindowTab.
func (t *windowTab) Kind() string { return t.spec.ID }

// SetTitle implements extension.WindowTab.
func (t *windowTab) SetTitle(title string) {
	if h := t.scope.opts.WindowTab; h != nil && t.live() {
		h.SetTitle(title)
	}
}

// SetHint implements extension.WindowTab.
func (t *windowTab) SetHint(hint string) {
	if h := t.scope.opts.WindowTab; h != nil && t.live() {
		h.SetHint(hint)
	}
}

// StateChanged implements extension.WindowTab.
func (t *windowTab) StateChanged() {
	if h := t.scope.opts.WindowTab; h != nil && t.live() {
		h.StateChanged()
	}
}

// CaptureKeys implements extension.WindowTab.
func (t *windowTab) CaptureKeys(capture bool) {
	if h := t.scope.opts.WindowTab; h != nil && t.live() {
		h.CaptureKeys(capture)
	}
}

// Refresh implements extension.WindowTab.
func (t *windowTab) Refresh() {
	if t.live() {
		t.scope.post(func() { t.scope.refreshDecl(t.decl) })
	}
}

// Close implements extension.WindowTab.
func (t *windowTab) Close() {
	if h := t.scope.opts.WindowTab; h != nil && t.live() {
		h.Close()
	}
}

// ---- the API of the extension ----

type windowTabsAPI struct{ inst *instance }

// WindowTabs implements extension.API.
func (a *api) WindowTabs() extension.WindowTabs { return windowTabsAPI{a.inst} }

// Provide implements extension.WindowTabs.
func (w windowTabsAPI) Provide(kindID string, kind extension.WindowTabKind) error {
	in := w.inst
	if in.isDead() {
		return errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	declared := false
	for _, k := range in.slot.info.Manifest.Contributes.WindowTabKinds {
		declared = declared || k.ID == kindID
	}
	s := in.scope
	s.mu.Lock()
	wt := s.wtab
	switch {
	case !declared:
		s.mu.Unlock()
		return errors.New("ExtensionWindowTabKindNotDeclared", "the manifest of the extension does not declare this window tab kind", "extension", in.id, "kind", kindID)
	case wt == nil || wt.spec.ID != kindID || wt.decl.slot != in.slot:
		s.mu.Unlock()
		return errors.New("ExtensionWindowTabUnavailable", "the scope is not the window tab of this kind", "extension", in.id, "kind", kindID, "scope", s.opts.Info.Name)
	case kind.Create == nil:
		s.mu.Unlock()
		return errors.New("ExtensionWindowTabKindInvalid", "a window tab kind needs a Create function", "extension", in.id, "kind", kindID)
	case wt.inst == in:
		s.mu.Unlock()
		return errors.New("ExtensionWindowTabKindProvided", "the activation provided this window tab kind already", "extension", in.id, "kind", kindID)
	}
	raw := slices.Clone(s.viewStates[wt.decl.id])
	wt.inst = in // the tab answers the extension while Create runs, so that it can set the title
	s.mu.Unlock()
	in.track(func() { s.releaseWindowTab(wt, in) })
	if err := s.makeWindowTabContent(wt, in, kind, raw); err != nil {
		in.log.Warn("the window tab could not be made", "kind", kindID, "err", err)
		s.showWindowTabError(wt, in, err)
	}
	return nil
}

// makeWindowTabContent asks the kind for the content of the tab with the saved state. A panic fails the extension (the
// error is returned too, the tab then shows the failure), an error is returned and the tab shows it.
func (s *Scope) makeWindowTabContent(wt *windowTab, in *instance, kind extension.WindowTabKind, raw json.RawMessage) (err error) {
	var content extension.WindowTabContent
	func() {
		defer func() {
			if r := recover(); r != nil {
				failure := errors.NewErrf("ExtensionWindowTabCreatePanicked", "it panicked in Create of the window tab kind %s: %v", wt.spec.ID, r).SetMetadata("extension", in.id, "kind", wt.spec.ID, "scope", s.opts.Info.Name)
				s.post(func() { s.fail(wt.decl.slot, failure, "window tab kind "+wt.spec.ID) })
				err = failure
			}
		}()
		content, err = kind.Create(wt, slices.Clone(raw))
	}()
	if err != nil {
		return err
	}
	if content.Build == nil {
		return errors.New("ExtensionWindowTabContentInvalid", "the content of a window tab needs a Build function", "extension", in.id, "kind", wt.spec.ID)
	}
	s.mu.Lock()
	wt.onClose = content.OnClose
	wt.decl.content = &extension.ViewContent{Build: content.Build, SaveState: content.SaveState}
	wt.decl.inst = in
	wt.decl.provided = true
	s.mu.Unlock()
	s.post(func() { s.refreshDecl(wt.decl) })
	return nil
}

// showWindowTabError puts the text of an error in the place of the content of the tab.
func (s *Scope) showWindowTabError(wt *windowTab, in *instance, err error) {
	msg := describe(err)
	s.mu.Lock()
	wt.decl.content = &extension.ViewContent{Build: func(bc widgets.BuildContext) widgets.Widget {
		return notice("This tab could not be made: " + msg)
	}}
	wt.decl.inst = in
	s.mu.Unlock()
	s.post(func() { s.refreshDecl(wt.decl) })
}

// releaseWindowTab takes the content of the tab away, saving its state first, when the activation that made it ends. The
// tab stays, waiting for its extension.
func (s *Scope) releaseWindowTab(wt *windowTab, in *instance) {
	s.saveViewWhile(wt.decl, true)
	s.mu.Lock()
	if wt.inst == in {
		wt.decl.content, wt.decl.inst, wt.decl.provided, wt.inst, wt.onClose = nil, nil, false, nil, nil
	}
	s.mu.Unlock()
	s.windowTabChanged()
}
