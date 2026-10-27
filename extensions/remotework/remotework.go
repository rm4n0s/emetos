package remotework

import (
	"context"
	"encoding/json"
	"path/filepath"

	"github.com/rm4n0s/emetos/extension"
)

// The ids of the extension.
const (
	// ID is the id of the extension (SPEC §10.18: "emetos.remotework" on the Extensions page).
	ID = "emetos.remotework"
	// KindID is the kind of the editor tab that is the page, and ManageCommand the command that opens it.
	KindID        = "emetos.remotework.page"
	ManageCommand = "emetos.remotework.manage"
)

// NewFactory makes the Factory of the extension: the instances it makes, one for each workspace, share one Hub made here,
// which holds the listener, the sessions and remote-work.json. extensions.All calls it once; a test passes the options of its
// own (a loopback listener, a clock).
func NewFactory(opts HubOptions) extension.Factory {
	hub := NewHub(opts)
	return func() extension.Extension { return &remoteWork{hub: hub} }
}

// remoteWork is the extension in one workspace.
type remoteWork struct {
	hub *Hub
	api extension.API
}

// Manifest implements extension.Extension.
func (*remoteWork) Manifest() extension.Manifest {
	return extension.Manifest{
		ID: ID, Name: "Remote Work", Version: "0.1.0",
		Description: "Lets another computer use a workspace of this Emetos, after it logs in over SSH with a key you listed.",
		// The listener must be open as soon as a workspace that has Remote Work on is open, so the extension is active from the start.
		ActivationEvents: []string{extension.OnStartup},
		Contributes: extension.Contributes{
			Commands: []extension.CommandSpec{{
				ID: ManageCommand, Title: "Manage", Category: "Remote Work",
				Description: "Opens the Remote Work page: turn it on for this workspace, set the port and the keys that may log in.",
			}},
			EditorKinds: []extension.EditorKindSpec{{ID: KindID, Name: "Remote Work", Icon: "remote"}},
		},
	}
}

// Activate implements extension.Extension. In a workspace it tells the hub the workspace is there, and provides the page.
func (r *remoteWork) Activate(ctx context.Context, api extension.API) error {
	r.api = api
	if err := api.Editors().Provide(KindID, extension.EditorKind{Create: r.createPage}); err != nil {
		return err
	}
	if err := api.Commands().Handle(ManageCommand, r.manage); err != nil {
		return err
	}
	if api.Scope().Kind != extension.ScopeWorkspace {
		return nil
	}
	unregister := r.hub.Register(api)
	context.AfterFunc(ctx, unregister)
	return nil
}

// manage opens the page, or shows the one that is open.
func (r *remoteWork) manage(context.Context, any) error {
	if tabs := r.api.Editors().Tabs(KindID); len(tabs) > 0 {
		tabs[0].Reveal(true)
		return nil
	}
	_, err := r.api.Editors().Open(KindID, nil, extension.EditorOpenOptions{})
	return err
}

// createPage makes the content of the tab of the page; the page keeps no state of its own.
func (r *remoteWork) createPage(tab extension.EditorTab, _ json.RawMessage) (extension.EditorTabContent, error) {
	tab.SetTitle("Remote Work")
	p := newPage(r.hub, r.api, tab, filepath.Clean(r.api.Scope().Folder))
	return extension.EditorTabContent{Build: p.build, OnClose: p.close}, nil
}
