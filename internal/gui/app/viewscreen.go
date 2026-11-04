package app

import (
	"strings"
	"sync/atomic"

	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	scmcore "github.com/rm4n0s/emetos/internal/core/scm"
	"github.com/rm4n0s/emetos/internal/gui/exthost"
	"github.com/rm4n0s/emetos/internal/gui/workbench"
)

// The screens of a hosted workspace (SPEC §10.18 "As built (step 24.25)", D50, D51). The scope of the extensions belongs to the workspace of
// the tab; the window of the host and every headless view of a client are screens of it. A screen has an origin (extension.Origin): the
// host's window is 0 and the view of a client has the number its parent gave it. The commands, menu items, views, status items and context
// keys that the extensions put in the parent's registries are seen by a view through the registries' Inherit; a command run in a view is
// forwarded to the parent's handler with the view's origin in its context, and the questions the handler asks are shown on that view.

// screenOf is the workspace that shows the screen with the origin: the workspace itself for the host's window, the workspace of one of its
// views for a client; nil when the view is gone. UI thread only.
func (w *Workspace) screenOf(o extension.Origin) *Workspace {
	if o == extension.HostOrigin {
		return w
	}
	for _, v := range w.views {
		if v.ws != nil && v.ws.origin == o && !v.ended {
			return v.ws
		}
	}
	return nil
}

// onUIThread tells whether the caller is on the UI thread of the shell that shows the workspace; true where nothing shows it.
func (w *Workspace) onUIThread() bool {
	if w.isView() {
		return w.viewOf.onUIThread()
	}
	if f := w.uiCheck.Load(); f != nil {
		return (*f)()
	}
	return true
}

// scopeScreenOptions adds to the options of the scope what it needs to serve the screens of the workspace: the thread check, the keys of a
// screen, and the origin of a widget.
func (w *Workspace) scopeScreenOptions(o *exthost.ScopeOptions) {
	o.OnUI = w.onUIThread
	o.OriginKeys = func(origin extension.Origin) *contextkey.Scope {
		if screen := w.screenOf(origin); screen != nil {
			return screen.Keys
		}
		return nil
	}
	o.OriginViews = func(origin extension.Origin) *workbench.Views {
		if screen := w.screenOf(origin); screen != nil {
			return screen.Views
		}
		return nil
	}
	o.OriginFocusView = func(origin extension.Origin, viewID string) {
		if screen := w.screenOf(origin); screen != nil {
			screen.focusExtensionView(viewID)
		}
	}
	o.OriginCommands = func(origin extension.Origin) *command.Registry {
		if screen := w.screenOf(origin); screen != nil {
			return screen.Commands
		}
		return nil
	}
	o.KeysOf = func(bc widgets.BuildContext) *contextkey.Scope { return PeekWorkspace(bc).Keys }
}

// followExtensionKeys gives the view the context keys the extensions set for every screen, now and as they change.
func (w *Workspace) followExtensionKeys() {
	p := w.viewOf
	scope := p.Extensions()
	if scope == nil {
		return
	}
	for k, v := range scope.SharedKeys() {
		w.Keys.Set(k, v)
	}
	w.stops = append(w.stops, scope.OnSharedKey(func(key string, value any, reset bool) {
		if reset {
			w.Keys.Reset(key)
		} else {
			w.Keys.Set(key, value)
		}
	}))
}

// startViewSCM follows the Source Control model in the view: each change of the model sets the context keys again, once for a burst of
// them. The settings of the model are the parent's to follow.
func (w *Workspace) startViewSCM() {
	m := w.SCM
	if m == nil {
		return
	}
	var pending atomic.Bool
	w.stops = append(w.stops, m.Subscribe(func(scmcore.Event) {
		if pending.Swap(true) {
			return
		}
		w.onUI(func() {
			pending.Store(false)
			w.syncSCMKeys()
		})
	}))
	w.syncSCMKeys()
}

// extensionStatusID tells whether the id of a status item is one an extension added (exthost's statusIDPrefix) and a client's view shows.
func extensionStatusID(id string) bool {
	// the HTTP client's environment item is the host's own: its click is refused to a client (step 27.9, D46), so the view does not show it
	return strings.HasPrefix(id, "extension.") && !strings.HasPrefix(id, "extension.emetos.httpclient.")
}
