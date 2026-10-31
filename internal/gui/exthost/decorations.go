package exthost

import (
	"sync"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/decorations"
)

// The binding of extension.Decorations to the file decorations service of a scope (SPEC §10.30 "File decorations and the Explorer",
// step 24.6; after extHostDecorations.ts and mainThreadDecorations.ts). A provider of an extension is a provider of the service;
// the service calls it on the UI thread, caught at the boundary like every call into an extension (SPEC §10.15 "Containment").

// extensionWeight is the weight of the decorations of an extension (VS Code's mainThreadDecorations gives every extension 10); the
// Priority of a FileDecoration is added to it.
const extensionWeight = 10

// decorationsAPI is the extension.Decorations of one activation.
type decorationsAPI struct{ inst *instance }

var _ extension.Decorations = decorationsAPI{}

// Decorations implements extension.API.
func (a *api) Decorations() extension.Decorations { return decorationsAPI{a.inst} }

// Register implements extension.Decorations. It fails with ExtensionDeactivated after the activation ended,
// ExtensionDecorationsInvalid for a nil provider and ExtensionDecorationsUnavailable in a scope with no decorations service.
func (d decorationsAPI) Register(provider extension.FileDecorationProvider) (extension.FileDecorations, error) {
	in := d.inst
	if in.isDead() {
		return nil, errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	if provider == nil {
		return nil, errors.New("ExtensionDecorationsInvalid", "a decorations provider is needed", "extension", in.id)
	}
	svc := in.scope.opts.Decorations
	if svc == nil {
		return nil, errors.New("ExtensionDecorationsUnavailable", "the scope has no file decorations service", "extension", in.id)
	}
	reg, err := svc.Register(in.id, &decorationProvider{inst: in, svc: svc, p: provider})
	if err != nil {
		return nil, err
	}
	h := &decorationsHandle{inst: in, reg: reg}
	if !in.track(h.Dispose) {
		return nil, errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	return h, nil
}

// decorationProvider is the decorations.Provider of a provider of an extension.
type decorationProvider struct {
	inst *instance
	svc  *decorations.Service
	p    extension.FileDecorationProvider
}

// Decoration implements decorations.Provider: it asks the extension. A decoration that VS Code's FileDecoration.validate refuses (a
// badge of more than two characters, nothing in it) is logged and dropped; a panic fails the extension once the service has released its
// lock (the end of the activation takes the provider out of it).
func (d *decorationProvider) Decoration(uri string) (data decorations.Data, ok bool) {
	in := d.inst
	if in.isDead() {
		return decorations.Data{}, false
	}
	defer func() {
		if r := recover(); r != nil {
			data, ok = decorations.Data{}, false
			failure := errors.NewErrf("ExtensionDecorationsPanicked", "it panicked in the file decorations provider: %v", r).SetMetadata("extension", in.id, "scope", in.scope.opts.Info.Name)
			d.svc.Defer(func() { in.scope.post(func() { in.scope.fail(in.slot, failure, "file decorations provider") }) })
		}
	}()
	fd, found := d.p.Decoration(uri)
	if !found {
		return decorations.Data{}, false
	}
	switch {
	case !extension.ValidBadge(fd.Badge):
		in.log.Warn("invalid file decoration: the badge must be one or two characters", "uri", uri, "badge", fd.Badge)
		return decorations.Data{}, false
	case fd.IsEmpty():
		in.log.Warn("invalid file decoration: it is empty", "uri", uri)
		return decorations.Data{}, false
	}
	return decorations.Data{
		Weight: extensionWeight + fd.Priority, Color: fd.Color, Letter: fd.Badge, Icon: fd.Icon, Tooltip: fd.Tooltip, Bubble: fd.Propagate,
	}, true
}

// fireChunk is the number of files a turn of the UI thread asks the provider about: a repository with 100,000 changes is asked in 50 turns
// of a few milliseconds each, with the frames of the window between them, and not in one that freezes it (SPEC §14).
const fireChunk = 2000

// decorationsHandle is the extension.FileDecorations of a registered provider.
type decorationsHandle struct {
	inst *instance
	reg  *decorations.Registration

	mu sync.Mutex
	// pending are the calls of Fire that wait for the UI thread, in order; a call with no URI forgets everything the provider said.
	pending []pendingFire
	// draining says that a turn of the UI thread is posted to take the first of them.
	draining bool
}

// pendingFire is a call of Fire.
type pendingFire struct {
	uris []string
	all  bool
}

var _ extension.FileDecorations = (*decorationsHandle)(nil)

// Fire implements extension.FileDecorations: the questions are asked on the UI thread, in the order the calls came, fireChunk files in each
// turn.
func (h *decorationsHandle) Fire(uris ...string) {
	in := h.inst
	if in.isDead() {
		return
	}
	call := pendingFire{uris: append([]string(nil), uris...), all: len(uris) == 0}
	h.mu.Lock()
	h.pending = append(h.pending, call)
	post := !h.draining
	h.draining = true
	h.mu.Unlock()
	if post {
		in.scope.post(h.drain)
	}
}

// drain runs on the UI thread: it asks the provider about the first fireChunk files that wait, and posts itself again when more wait.
func (h *decorationsHandle) drain() {
	in := h.inst
	h.mu.Lock()
	if in.isDead() || len(h.pending) == 0 {
		h.pending, h.draining = nil, false
		h.mu.Unlock()
		return
	}
	call := &h.pending[0]
	var chunk []string
	all := call.all
	if all {
		h.pending = h.pending[1:]
	} else if len(call.uris) <= fireChunk {
		chunk = call.uris
		h.pending = h.pending[1:]
	} else {
		chunk, call.uris = call.uris[:fireChunk], call.uris[fireChunk:]
	}
	more := len(h.pending) > 0
	if !more {
		h.draining = false
	}
	h.mu.Unlock()
	if all {
		h.reg.Fire()
	} else {
		h.reg.Fire(chunk...)
	}
	if more {
		in.scope.post(h.drain)
	}
}

// Dispose implements extension.FileDecorations.
func (h *decorationsHandle) Dispose() { h.reg.Dispose() }
