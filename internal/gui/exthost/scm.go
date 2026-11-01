package exthost

import (
	"encoding/json"
	"strings"
	"sync"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/scm"
)

// The binding of extension.SourceControls to the Source Control model of a scope (SPEC §10.30 "The SCM API", step 24.2; after
// extHostSCM.ts). A source control belongs to the extension that made it: only that extension changes it (the owner is the id of
// the extension in the model's Source), it goes with the activation, and the callbacks the extension gave (Validate, OnChange)
// run on the UI thread, caught at the boundary like every call into an extension (SPEC §10.15 "Containment").

// scmAPI is the extension.SourceControls of one activation.
type scmAPI struct{ inst *instance }

var _ extension.SourceControls = scmAPI{}

// SourceControl implements extension.API.
func (a *api) SourceControl() extension.SourceControls { return scmAPI{a.inst} }

// Create implements extension.SourceControls. It fails with ExtensionDeactivated after the activation ended,
// ExtensionSCMInvalid for an empty id or label, ExtensionSCMUnavailable in a scope with no Source Control model and
// ExtensionSCMDuplicate for a source control of the extension that has the id and the root already. The source control is
// removed when the activation ends.
func (c scmAPI) Create(spec extension.SourceControlSpec) (extension.SourceControl, error) {
	in := c.inst
	if in.isDead() {
		return nil, errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	if strings.TrimSpace(spec.ID) == "" || strings.TrimSpace(spec.Label) == "" {
		return nil, errors.New("ExtensionSCMInvalid", "a source control needs an id and a label", "extension", in.id, "id", spec.ID)
	}
	model := in.scope.opts.SCM
	if model == nil {
		return nil, errors.New("ExtensionSCMUnavailable", "the scope has no Source Control model", "extension", in.id)
	}
	key := spec.ID + "\x00" + spec.RootURI
	in.mu.Lock()
	if in.scmKeys == nil {
		in.scmKeys = map[string]bool{}
	}
	if in.scmKeys[key] {
		in.mu.Unlock()
		return nil, errors.New("ExtensionSCMDuplicate", "the extension has a source control with this id and root already", "extension", in.id, "id", spec.ID, "root", spec.RootURI)
	}
	in.scmKeys[key] = true
	in.mu.Unlock()
	src := model.Add(scm.SourceSpec{ProviderID: spec.ID, Label: spec.Label, RootURI: spec.RootURI, ContextValue: spec.ContextValue, Owner: in.id})
	h := &scmSource{inst: in, src: src, key: key, model: model}
	if !in.track(h.Dispose) {
		return nil, errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	return h, nil
}

// scmSource is the extension.SourceControl of a source control of the model.
type scmSource struct {
	inst  *instance
	src   *scm.Source
	key   string
	model *scm.Model

	mu       sync.Mutex // makes the check of a group's id and its creation one step
	disposed bool
	// quickDiffKinds are the kinds of quick diff provider the source control has, and quickDiffRegs the providers (quickdiff.go).
	quickDiffKinds map[extension.QuickDiffKind]bool
	quickDiffRegs  []*quickDiffHandle
	// history is the registration of the history provider (history.go).
	history *historyHandle
}

var _ extension.SourceControl = (*scmSource)(nil)

// scmOwned checks that the source control was made by the extension: it fails with ExtensionSCMForeign for another's. Every
// call that changes a source control goes through it.
func (s *Scope) scmOwned(in *instance, src *scm.Source) error {
	if src.Owner() != in.id {
		return errors.New("ExtensionSCMForeign", "a source control belongs to the extension that made it", "extension", in.id, "owner", src.Owner(), "id", src.ProviderID())
	}
	return nil
}

// mutate runs a change of the source control: nothing happens after the activation ended, and a source control of another
// extension is not changed (it is logged).
func (h *scmSource) mutate(fn func(src *scm.Source)) {
	in := h.inst
	if in.isDead() {
		return
	}
	if err := in.scope.scmOwned(in, h.src); err != nil {
		in.log.Warn("a call on a source control of another extension was refused", "err", err)
		return
	}
	fn(h.src)
}

// Handle implements extension.SourceControl.
func (h *scmSource) Handle() string { return h.src.Key() }

// SetCount implements extension.SourceControl.
func (h *scmSource) SetCount(count int) { h.mutate(func(s *scm.Source) { s.SetCount(count) }) }

// SetStatusBarCommands implements extension.SourceControl.
func (h *scmSource) SetStatusBarCommands(commands []extension.StatusCommand) {
	list := make([]scm.StatusCommand, len(commands))
	for i, c := range commands {
		list[i] = scm.StatusCommand{Text: c.Text, Tooltip: c.Tooltip, Command: c.Command, Args: c.Args}
	}
	h.mutate(func(s *scm.Source) { s.SetStatusBarCommands(list) })
}

// SetActiveRepositoryName implements extension.SourceControl.
func (h *scmSource) SetActiveRepositoryName(name string) {
	h.mutate(func(s *scm.Source) { s.SetActiveRepositoryName(name) })
}

// SetBusy implements extension.SourceControl.
func (h *scmSource) SetBusy(busy bool) { h.mutate(func(s *scm.Source) { s.SetBusy(busy) }) }

// SetAcceptInputCommand implements extension.SourceControl.
func (h *scmSource) SetAcceptInputCommand(c extension.Command) {
	cmd := scmCommand(c)
	h.mutate(func(s *scm.Source) { s.SetAcceptInputCommand(cmd) })
}

// SetActionButton implements extension.SourceControl.
func (h *scmSource) SetActionButton(b *extension.ActionButton) {
	var button *scm.ActionButton
	if b != nil {
		button = &scm.ActionButton{Command: scmCommand(b.Command), Enabled: b.Enabled, Description: b.Description}
		for _, group := range b.Secondary {
			list := make([]scm.Command, len(group))
			for i, c := range group {
				list[i] = scmCommand(c)
			}
			button.Secondary = append(button.Secondary, list)
		}
	}
	h.mutate(func(s *scm.Source) { s.SetActionButton(button) })
}

// Input implements extension.SourceControl.
func (h *scmSource) Input() extension.SCMInput { return scmInput{h} }

// Dispose implements extension.SourceControl. It does nothing the second time.
func (h *scmSource) Dispose() {
	h.mu.Lock()
	if h.disposed {
		h.mu.Unlock()
		return
	}
	h.disposed = true
	regs := h.quickDiffRegs
	h.quickDiffRegs = nil
	history := h.history
	h.mu.Unlock()
	if history != nil {
		history.Dispose()
	}
	for _, r := range regs {
		r.Dispose()
	}
	h.model.Remove(h.src)
	in := h.inst
	in.mu.Lock()
	delete(in.scmKeys, h.key)
	in.mu.Unlock()
}

// CreateGroup implements extension.SourceControl. It fails with ExtensionDeactivated after the activation ended,
// ExtensionSCMGroupInvalid for an empty id, ExtensionSCMGroupDuplicate for an id the source control has, and
// ExtensionSCMDisposed for a source control that was disposed.
func (h *scmSource) CreateGroup(spec extension.ResourceGroupSpec) (extension.ResourceGroup, error) {
	in := h.inst
	if in.isDead() {
		return nil, errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	if err := in.scope.scmOwned(in, h.src); err != nil {
		return nil, err
	}
	if strings.TrimSpace(spec.ID) == "" {
		return nil, errors.New("ExtensionSCMGroupInvalid", "a group of resources needs an id", "extension", in.id, "source", h.src.ProviderID())
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.disposed || h.src.Removed() {
		return nil, errors.New("ExtensionSCMDisposed", "the source control was disposed", "extension", in.id, "source", h.src.ProviderID())
	}
	if h.src.Group(spec.ID) != nil {
		return nil, errors.New("ExtensionSCMGroupDuplicate", "the source control has a group with this id already", "extension", in.id, "source", h.src.ProviderID(), "group", spec.ID)
	}
	g := h.src.AddGroup(spec.ID, spec.Label, spec.HideWhenEmpty, spec.ContextValue)
	return &scmGroup{h: h, g: g}, nil
}

// scmInput is the extension.SCMInput of a source control.
type scmInput struct{ h *scmSource }

var _ extension.SCMInput = scmInput{}

// Value implements extension.SCMInput.
func (i scmInput) Value() string { return i.h.src.Input().Value() }

// SetValue implements extension.SCMInput.
func (i scmInput) SetValue(value string) {
	i.h.mutate(func(s *scm.Source) { s.Input().SetValue(value) })
}

// OnChange implements extension.SCMInput: fn is called on the UI thread, later, with the text of the change; the listener goes
// with the activation.
func (i scmInput) OnChange(fn func(value string)) (cancel func()) {
	in := i.h.inst
	if fn == nil || in.isDead() {
		return func() {}
	}
	stop := i.h.src.Input().OnChange(func(value string) {
		in.scope.post(func() { in.scope.runSCMChange(in, fn, value) })
	})
	in.track(stop)
	return stop
}

// runSCMChange calls the OnChange function of an input. A panic fails the extension with ExtensionSCMChangePanicked.
func (s *Scope) runSCMChange(inst *instance, fn func(string), value string) {
	if inst.isDead() {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			failure := errors.NewErrf("ExtensionSCMChangePanicked", "it panicked in the OnChange function of a source control input: %v", r).SetMetadata("extension", inst.id, "scope", s.opts.Info.Name)
			s.fail(inst.slot, failure, "source control input change")
		}
	}()
	fn(value)
}

// SetPlaceholder implements extension.SCMInput.
func (i scmInput) SetPlaceholder(placeholder string) {
	i.h.mutate(func(s *scm.Source) { s.Input().SetPlaceholder(placeholder) })
}

// SetValidation implements extension.SCMInput: the function is called by the view on the UI thread.
func (i scmInput) SetValidation(fn func(value string) *extension.Validation) {
	in := i.h.inst
	if fn == nil {
		i.h.mutate(func(s *scm.Source) { s.Input().SetValidation(nil) })
		return
	}
	i.h.mutate(func(s *scm.Source) {
		s.Input().SetValidation(func(value string) *scm.Validation {
			v := in.scope.runSCMValidate(in, fn, value)
			if v == nil {
				return nil
			}
			return &scm.Validation{Message: v.Message, Level: scm.ValidationLevel(v.Level)}
		})
	})
}

// runSCMValidate calls the validation function of an input and returns what it says. A panic fails the extension with
// ExtensionSCMValidatePanicked and shows no message.
func (s *Scope) runSCMValidate(inst *instance, fn func(string) *extension.Validation, value string) (v *extension.Validation) {
	if inst.isDead() {
		return nil
	}
	defer func() {
		if r := recover(); r != nil {
			v = nil
			failure := errors.NewErrf("ExtensionSCMValidatePanicked", "it panicked in the validation function of a source control input: %v", r).SetMetadata("extension", inst.id, "scope", s.opts.Info.Name)
			s.fail(inst.slot, failure, "source control input validation")
		}
	}()
	return fn(value)
}

// SetVisible implements extension.SCMInput.
func (i scmInput) SetVisible(visible bool) {
	i.h.mutate(func(s *scm.Source) { s.Input().SetVisible(visible) })
}

// SetEnabled implements extension.SCMInput.
func (i scmInput) SetEnabled(enabled bool) {
	i.h.mutate(func(s *scm.Source) { s.Input().SetEnabled(enabled) })
}

// Focus implements extension.SCMInput.
func (i scmInput) Focus() { i.h.mutate(func(s *scm.Source) { s.Input().Focus() }) }

// scmGroup is the extension.ResourceGroup of a group of the model.
type scmGroup struct {
	h *scmSource
	g *scm.Group
}

var _ extension.ResourceGroup = (*scmGroup)(nil)

// ID implements extension.ResourceGroup.
func (g *scmGroup) ID() string { return g.g.ID() }

// SetResources implements extension.ResourceGroup. The list is converted at once and replaces the group's in one step.
func (g *scmGroup) SetResources(resources []extension.ResourceState) {
	list := make([]scm.Resource, len(resources))
	for i := range resources {
		r := &resources[i]
		list[i] = scm.Resource{
			URI: r.URI, ContextValue: r.ContextValue,
			Decorations:          scm.Decorations{Icon: r.Decorations.Icon, Tooltip: r.Decorations.Tooltip, StrikeThrough: r.Decorations.StrikeThrough, Faded: r.Decorations.Faded, Color: r.Decorations.Color},
			MultiDiffOriginalURI: r.MultiFileDiffEditorOriginalURI, MultiDiffModifiedURI: r.MultiFileDiffEditorModifiedURI,
		}
		if r.Command != nil {
			c := scmCommand(*r.Command)
			list[i].Command = &c
		}
	}
	g.h.mutate(func(*scm.Source) { g.g.SetResources(list) })
}

// SetLabel implements extension.ResourceGroup.
func (g *scmGroup) SetLabel(label string) { g.h.mutate(func(*scm.Source) { g.g.SetLabel(label) }) }

// SetHideWhenEmpty implements extension.ResourceGroup.
func (g *scmGroup) SetHideWhenEmpty(hide bool) {
	g.h.mutate(func(*scm.Source) { g.g.SetHideWhenEmpty(hide) })
}

// Dispose implements extension.ResourceGroup.
func (g *scmGroup) Dispose() { g.h.mutate(func(*scm.Source) { g.g.Remove() }) }

// scmCommand converts the command of an extension: the arguments of a command of the language providers (JSON values) become
// one argument, the value of Args when it is set.
func scmCommand(c extension.Command) scm.Command {
	return scm.Command{ID: c.ID, Title: c.Title, Tooltip: c.Tooltip, Args: scmCommandArgs(c)}
}

// scmCommandArgs is the argument a command is run with: Args, else the one JSON argument decoded, else the list of them; nil for
// none. An argument that is not valid JSON is left out.
func scmCommandArgs(c extension.Command) any {
	if c.Args != nil {
		return c.Args
	}
	var list []any
	for _, raw := range c.Arguments {
		var v any
		if err := json.Unmarshal(raw, &v); err == nil {
			list = append(list, v)
		}
	}
	switch len(list) {
	case 0:
		return nil
	case 1:
		return list[0]
	}
	return list
}
