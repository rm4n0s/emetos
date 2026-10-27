package exthost

import (
	"context"
	"io"
	"io/fs"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/logs"
	"github.com/rm4n0s/emetos/internal/gui/views/problems"
)

// The contributions to the built-in parts (SPEC §10.15 "As built (step 11.5)"): views, Output channels, Problems markers,
// the folder of the workspace and context keys. Each is tracked by the instance, so the end of the activation takes it
// away.

// Views implements extension.API.
func (a *api) Views() extension.Views { return viewsAPI{a.inst} }

// Output implements extension.API.
func (a *api) Output() extension.Output { return outputAPI{a.inst} }

// Problems implements extension.API.
func (a *api) Problems() extension.Problems { return problemsAPI{a.inst} }

// Workspace implements extension.API.
func (a *api) Workspace() extension.Workspace { return workspaceAPI{a.inst} }

// Context implements extension.API.
func (a *api) Context() extension.ContextKeys { return contextAPI{a.inst} }

// ---- views ----

type viewsAPI struct{ inst *instance }

// Provide implements extension.Views.
func (v viewsAPI) Provide(viewID string, content extension.ViewContent) (extension.ViewHandle, error) {
	in := v.inst
	if in.isDead() {
		return nil, errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	s := in.scope
	d := s.decl(viewID)
	if d == nil || d.slot != in.slot {
		return nil, errors.New("ExtensionViewNotDeclared", "the manifest of the extension does not declare this view", "extension", in.id, "view", viewID)
	}
	if content.Build == nil {
		return nil, errors.New("ExtensionViewContentInvalid", "the content of a view needs a Build function", "extension", in.id, "view", viewID)
	}
	s.mu.Lock()
	if d.content != nil && d.inst == in {
		s.mu.Unlock()
		return nil, errors.New("ExtensionViewProvided", "the activation provided this view already", "extension", in.id, "view", viewID)
	}
	d.content, d.inst = &content, in
	s.mu.Unlock()
	// the content has its saved state at once, so that what it saves with the window is never less than what it had
	s.restoreView(d)
	s.mu.Lock()
	d.provided = true
	s.mu.Unlock()
	h := &viewHandle{inst: in, decl: d}
	in.track(h.release)
	s.post(h.refresh)
	return h, nil
}

// viewHandle is the extension.ViewHandle of one view.
type viewHandle struct {
	inst *instance
	decl *viewDecl
}

// refresh builds the boxes of the view again; it runs on the UI thread.
func (h *viewHandle) refresh() {
	s := h.inst.scope
	s.mu.Lock()
	boxes := append([]*viewBoxState(nil), h.decl.boxes...)
	s.mu.Unlock()
	for _, b := range boxes {
		b.refresh()
	}
}

// Refresh implements extension.ViewHandle.
func (h *viewHandle) Refresh() {
	if h.inst.isDead() {
		return
	}
	h.inst.scope.post(h.refresh)
}

// SetBadge implements extension.ViewHandle.
func (h *viewHandle) SetBadge(count int, tooltip string) {
	in := h.inst
	if in.isDead() || !h.decl.panel {
		return
	}
	s := in.scope
	s.post(func() {
		s.mu.Lock()
		changed := h.decl.badge != count || h.decl.badgeTip != tooltip
		h.decl.badge, h.decl.badgeTip = max(count, 0), tooltip
		s.mu.Unlock()
		if changed && s.opts.Views != nil {
			s.opts.Views.Changed()
		}
	})
}

// Reveal implements extension.ViewHandle.
func (h *viewHandle) Reveal(focus bool) {
	in := h.inst
	if in.isDead() {
		return
	}
	s := in.scope
	d := h.decl
	s.post(func() {
		views := s.opts.Views
		if views == nil {
			return
		}
		if d.panel {
			views.OpenPanel(d.container)
		} else {
			views.Open(d.container)
			views.SetHidden(d.id, false)
			views.SetCollapsed(d.id, false)
		}
		if focus && s.opts.FocusView != nil {
			s.opts.FocusView(d.id)
		}
	})
}

// Dispose implements extension.ViewHandle.
func (h *viewHandle) Dispose() {
	h.release()
	h.inst.scope.post(h.refresh)
}

// release saves the state of the view and takes its content away; the view stays declared. The end of the activation calls
// it.
func (h *viewHandle) release() {
	s := h.inst.scope
	s.saveViewWhile(h.decl, true)
	s.mu.Lock()
	if h.decl.inst == h.inst {
		h.decl.content, h.decl.inst, h.decl.provided = nil, nil, false
		h.decl.badge, h.decl.badgeTip = 0, ""
	}
	s.mu.Unlock()
	if s.opts.Views != nil {
		s.post(s.opts.Views.Changed)
	}
}

// ---- Output channels ----

type outputAPI struct{ inst *instance }

// Channel implements extension.Output. The channel is made in the store of the Output panel by the first holder of its name.
func (o outputAPI) Channel(name string) (extension.OutputChannel, error) {
	in := o.inst
	if in.isDead() {
		return nil, errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	store := in.scope.opts.Channels
	if store == nil {
		return nil, errors.New("ExtensionOutputUnavailable", "the scope has no Output panel", "extension", in.id)
	}
	name = strings.TrimSpace(name)
	if name == "" || name == logs.MainChannel {
		return nil, errors.New("ExtensionOutputChannelInvalid", "an Output channel needs a name that is not the one of the log of Emetos", "extension", in.id, "channel", name)
	}
	in.mu.Lock()
	taken := in.channels[name]
	if !taken {
		in.channels[name] = true
	}
	in.mu.Unlock()
	if taken {
		return nil, errors.New("ExtensionOutputChannelDuplicate", "the extension has this Output channel open", "extension", in.id, "channel", name)
	}
	in.scope.h.holdChannel(store, name)
	c := &outputChannel{inst: in, store: store, name: name}
	in.track(c.Dispose)
	return c, nil
}

// outputChannel is the extension.OutputChannel of one channel.
type outputChannel struct {
	inst  *instance
	store *logs.Store
	name  string

	disposed bool
}

// Name implements extension.OutputChannel.
func (c *outputChannel) Name() string { return c.name }

// live tells whether the channel can be written: it was not disposed and the activation goes on.
func (c *outputChannel) live() bool {
	c.inst.mu.Lock()
	defer c.inst.mu.Unlock()
	return !c.disposed && !c.inst.dead
}

// AppendLine implements extension.OutputChannel.
func (c *outputChannel) AppendLine(text string) {
	if c.live() {
		c.store.Append(c.name, text)
	}
}

// Clear implements extension.OutputChannel.
func (c *outputChannel) Clear() {
	if c.live() {
		c.store.Clear(c.name)
	}
}

// Show implements extension.OutputChannel.
func (c *outputChannel) Show(preserveFocus bool) {
	if !c.live() {
		return
	}
	s := c.inst.scope
	if show := s.opts.ShowChannel; show != nil {
		s.post(func() { show(c.name, preserveFocus) })
	}
}

// Dispose implements extension.OutputChannel.
func (c *outputChannel) Dispose() {
	c.inst.mu.Lock()
	if c.disposed {
		c.inst.mu.Unlock()
		return
	}
	c.disposed = true
	delete(c.inst.channels, c.name)
	c.inst.mu.Unlock()
	c.inst.scope.h.releaseChannel(c.store, c.name)
}

// holdChannel counts a hold on an Output channel and makes the channel; releaseChannel takes the channel away with the last
// hold. Two window tabs that open the same name share it.
func (h *Host) holdChannel(store *logs.Store, name string) {
	h.mu.Lock()
	if h.channelHolds == nil {
		h.channelHolds = map[string]int{}
	}
	h.channelHolds[name]++
	h.mu.Unlock()
	store.Add(name)
}

func (h *Host) releaseChannel(store *logs.Store, name string) {
	h.mu.Lock()
	h.channelHolds[name]--
	last := h.channelHolds[name] <= 0
	if last {
		delete(h.channelHolds, name)
	}
	h.mu.Unlock()
	if last {
		store.Remove(name)
	}
}

// ---- Problems ----

type problemsAPI struct{ inst *instance }

// ownerOf makes the owner of the markers an extension sets: they are the extension's, so another cannot replace them.
func ownerOf(extensionID, owner string) string { return extensionID + ":" + owner }

// Set implements extension.Problems.
func (p problemsAPI) Set(owner, uri string, markers []extension.Marker) error {
	in := p.inst
	if in.isDead() {
		return errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	service := in.scope.opts.Markers
	if service == nil {
		return errors.New("ExtensionProblemsUnavailable", "the scope has no Problems panel", "extension", in.id)
	}
	if strings.TrimSpace(owner) == "" {
		return errors.New("ExtensionProblemsOwnerInvalid", "the markers of an extension need an owner", "extension", in.id)
	}
	u, err := document.ParseURI(uri)
	if err != nil {
		return err
	}
	if u.Scheme != document.SchemeFile {
		return errors.New("ExtensionProblemsURIInvalid", "markers belong to files: the URI is file:///path", "extension", in.id, "uri", uri)
	}
	full := ownerOf(in.id, owner)
	list := make([]problems.Marker, 0, len(markers))
	for _, m := range markers {
		source := m.Source
		if source == "" {
			source = in.name
		}
		list = append(list, problems.Marker{
			Severity: markerSeverity(m.Severity), Code: m.Code, Source: source, Message: m.Message,
			StartLine: m.StartLine, StartColumn: m.StartColumn, EndLine: m.EndLine, EndColumn: m.EndColumn,
		})
	}
	in.mu.Lock()
	first := !in.markerOwners[full]
	in.markerOwners[full] = true
	in.mu.Unlock()
	if first {
		in.track(func() { service.Remove(full) })
	}
	service.Set(full, u.Path, list)
	return nil
}

// Clear implements extension.Problems.
func (p problemsAPI) Clear(owner string) {
	in := p.inst
	service := in.scope.opts.Markers
	if in.isDead() || service == nil {
		return
	}
	service.Remove(ownerOf(in.id, owner))
}

// markerSeverity maps the severity of the contract to the panel's.
func markerSeverity(s extension.Severity) problems.Severity {
	switch s {
	case extension.SeverityWarning:
		return problems.SeverityWarning
	case extension.SeverityInfo:
		return problems.SeverityInfo
	case extension.SeverityHint:
		return problems.SeverityHint
	}
	return problems.SeverityError
}

// ---- the folder of the workspace ----

type workspaceAPI struct{ inst *instance }

// OpenURI implements extension.Workspace.
func (w workspaceAPI) OpenURI(ctx context.Context, uri string) error {
	open := w.inst.scope.opts.OpenURI
	if open == nil {
		return errors.New("ExtensionOpenURIUnavailable", "the scope cannot open a URI", "extension", w.inst.id, "uri", uri)
	}
	return open(ctx, uri)
}

// NewHeadlessView implements extension.Workspace. The view is closed when the activation ends.
func (w workspaceAPI) NewHeadlessView(size extension.Size) (extension.HeadlessView, error) {
	in := w.inst
	if in.isDead() {
		return nil, errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	if size.Width <= 0 || size.Height <= 0 || size.Width > extension.MaxHeadlessSide || size.Height > extension.MaxHeadlessSide ||
		size.Width*size.Height > extension.MaxHeadlessPixels {
		return nil, errors.New("ExtensionHeadlessSize", "a headless view is at most extension.MaxHeadlessSide a side and extension.MaxHeadlessPixels in all",
			"extension", in.id, "width", size.Width, "height", size.Height)
	}
	mk := in.scope.opts.NewHeadlessView
	if mk == nil {
		return nil, errors.New("ExtensionHeadlessUnavailable", "the scope has no workspace to show in a second view", "extension", in.id)
	}
	view, err := mk(size)
	if err != nil {
		return nil, err
	}
	if !in.track(view.Close) {
		return nil, errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	return view, nil
}

// Files implements extension.Workspace.
func (w workspaceAPI) Files() (extension.FileList, error) {
	in := w.inst
	if in.scope.opts.Info.Folder == "" || in.scope.opts.Files == nil {
		return extension.FileList{}, errors.New("ExtensionNoFolder", "the scope has no folder", "extension", in.id)
	}
	files := append([]string(nil), in.scope.opts.Files()...)
	complete := true
	if f := in.scope.opts.FilesComplete; f != nil {
		complete = f()
	}
	return extension.FileList{Files: files, Complete: complete}, nil
}

// ReadFile implements extension.Workspace. The folder is opened as a root, so neither a path with .. nor a link can reach
// a file outside it.
func (w workspaceAPI) ReadFile(ctx context.Context, path string) (string, error) {
	in := w.inst
	folder := in.scope.opts.Info.Folder
	if folder == "" {
		return "", errors.New("ExtensionNoFolder", "the scope has no folder", "extension", in.id)
	}
	if !fs.ValidPath(path) || path == "." {
		return "", errors.New("ExtensionFilePathInvalid", "a path is relative to the folder, with forward slashes and no ..", "extension", in.id, "path", path)
	}
	if err := ctx.Err(); err != nil {
		return "", errors.NewErr("ExtensionFileReadFailed", err).SetMetadata("extension", in.id, "path", path)
	}
	root, err := os.OpenRoot(folder)
	if err != nil {
		return "", errors.NewErr("ExtensionFileReadFailed", err).SetMetadata("extension", in.id, "path", path)
	}
	defer func() { _ = root.Close() }() // a read only file: closing cannot lose anything
	f, err := root.Open(path)
	if err != nil {
		return "", errors.NewErr("ExtensionFileReadFailed", err).SetMetadata("extension", in.id, "path", path)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, extension.MaxReadFileSize+1))
	if err != nil {
		return "", errors.NewErr("ExtensionFileReadFailed", err).SetMetadata("extension", in.id, "path", path)
	}
	if len(data) > extension.MaxReadFileSize {
		return "", errors.New("ExtensionFileTooLarge", "the file is larger than extension.MaxReadFileSize", "extension", in.id, "path", path)
	}
	if !utf8.Valid(data) {
		return "", errors.New("ExtensionFileNotText", "the file is not UTF-8 text", "extension", in.id, "path", path)
	}
	return string(data), nil
}

// ---- context keys ----

type contextAPI struct{ inst *instance }

// Set implements extension.ContextKeys.
func (c contextAPI) Set(key string, value any) error {
	in := c.inst
	if in.isDead() {
		return errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	if !strings.HasPrefix(key, in.id+".") || len(key) == len(in.id)+1 {
		return errors.New("ExtensionContextKeyForeign", "the key of a context key starts with the id of the extension and a dot", "extension", in.id, "key", key)
	}
	s := in.scope
	if s.opts.Keys == nil {
		return errors.New("ExtensionContextUnavailable", "the scope has no context keys", "extension", in.id)
	}
	in.mu.Lock()
	first := !in.contextKeys[key]
	in.contextKeys[key] = true
	in.mu.Unlock()
	if first {
		in.track(func() { s.opts.Keys.Reset(key) })
	}
	s.post(func() {
		s.opts.Keys.Set(key, value)
		if s.opts.Views != nil {
			s.opts.Views.Changed()
		}
	})
	return nil
}

// Reset implements extension.ContextKeys.
func (c contextAPI) Reset(key string) {
	in := c.inst
	s := in.scope
	if in.isDead() || s.opts.Keys == nil || !strings.HasPrefix(key, in.id+".") {
		return
	}
	s.post(func() {
		s.opts.Keys.Reset(key)
		if s.opts.Views != nil {
			s.opts.Views.Changed()
		}
	})
}
