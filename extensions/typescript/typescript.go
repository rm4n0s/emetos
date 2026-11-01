// Package typescript is the TypeScript language pack (SPEC §8.1, §10.15, §10.34): TypeScript and TypeScript React. The grammars and
// language configurations are in assets/, copied by scripts/update-assets.sh; the licenses are in third_party/ and NOTICE. Since step 24b.5
// the pack also runs typescript-language-server for TypeScript and for the JavaScript pack's languages (JavaScript and JavaScript React):
// the server is found, started and shown by extensions/internal/langserver (the path setting typescript.languageServer.path, PATH, the
// status item, the notification for a missing server), node runs it (typescript.nodePath), the preferences of VS Code's typescript.* and
// javascript.* settings are sent to it, and the pack owns the choice of the TypeScript it uses: the one beside the server or the folder's
// own, offered once and never taken (version.go), with "TypeScript: Select TypeScript Version..." and the version in the status item. The
// commands are VS Code's: Restart TS Server, Go to Project Configuration, Go to Source Definition, Find All File References, Organize
// Imports, Sort Imports and Remove Unused Imports.
package typescript

import (
	"context"
	"embed"
	"encoding/json"
	"sync"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/langpack"
	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

//go:embed all:assets
var assets embed.FS

// New is the Factory of the pack.
func New() extension.Extension {
	t := &typescript{}
	t.base = langpack.New(langpack.Spec{
		ID: ID, Name: "TypeScript", Description: "TypeScript and JavaScript support: syntax, the language server (typescript-language-server) and the TypeScript version.",
		Assets: assets, Server: t.serverConfig(),
	})
	return t
}

// typescript is the extension of one scope. Everything in it runs on the UI thread unless a comment says otherwise.
type typescript struct {
	base extension.Extension
	api  extension.API
	ctx  context.Context
	ctl  *langserver.Controller

	// mu guards the plan, which the launcher writes off the UI thread, and reported.
	mu sync.Mutex
	// plan is the TypeScript decision of the last start of the server; have says there was one.
	plan Plan
	have bool
	// reported is the version the server told ($/typescriptVersion) after its last start, "" before.
	reported string
	// offered says that the folder's own TypeScript was offered in this activation.
	offered bool
	// work waits for the requests in flight, so that a test can see that none is left.
	work sync.WaitGroup
}

// Manifest implements extension.Extension: the manifest of the pack with the commands, keybindings and settings of the server.
func (t *typescript) Manifest() extension.Manifest {
	m := t.base.Manifest()
	m.Contributes.Commands = append(m.Contributes.Commands, commandSpecs()...)
	m.Contributes.Settings = append(m.Contributes.Settings, settingSpecs()...)
	return m
}

// Activate implements extension.Extension.
func (t *typescript) Activate(ctx context.Context, api extension.API) error {
	t.api, t.ctx = api, ctx
	if err := t.base.Activate(ctx, api); err != nil {
		return err
	}
	t.ctl = langpack.Controller(t.base)
	for id, handler := range t.handlers() {
		if err := api.Commands().Handle(id, handler); err != nil {
			return err
		}
	}
	if _, err := api.LanguageServers().OnNotification(ServerID, versionMethod, t.versionReported); err != nil {
		return err
	}
	if _, err := api.Events().Subscribe(extension.TopicEditorActiveChanged, t.editorEvent); err != nil {
		return err
	}
	return nil
}

// wait waits for the requests the pack sent to end (the tests use it).
func (t *typescript) wait() { t.work.Wait() }

// Version is the TypeScript the server was last started with, false before its first start or when it could not be started.
func (t *typescript) Version() (Version, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.plan.Use, t.have
}

// forgetPlan forgets the decision of the last start, which a start that fails before it is made must not leave on the item (launcher's goroutine).
func (t *typescript) forgetPlan() {
	t.mu.Lock()
	t.plan, t.have, t.reported = Plan{}, false, ""
	t.mu.Unlock()
	t.api.Post(t.updateStatus)
}

// setPlan keeps the decision of a start (the launcher's goroutine) and shows it. A new start forgets the version the last one reported.
func (t *typescript) setPlan(p Plan) {
	t.mu.Lock()
	t.plan, t.have, t.reported = p, true, ""
	t.mu.Unlock()
	t.api.Post(t.updateStatus)
}

// versionMethod is the notification the server sends after it initialized, with the version of TypeScript it uses.
const versionMethod = "$/typescriptVersion"

// versionReported follows $/typescriptVersion: the server says which TypeScript runs, whatever the pack asked for.
func (t *typescript) versionReported(params json.RawMessage) {
	var v struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(params, &v) != nil {
		return
	}
	t.mu.Lock()
	t.reported = v.Version
	t.mu.Unlock()
	t.updateStatus()
}

// updateStatus shows the TypeScript in the status item of the server: its version as the text, its path and where it is from in the tooltip.
func (t *typescript) updateStatus() {
	if t.ctl == nil {
		return
	}
	t.mu.Lock()
	use, have, reported := t.plan.Use, t.have, t.reported
	t.mu.Unlock()
	if !have {
		t.ctl.SetLabel("")
		t.ctl.SetDetail("")
		return
	}
	if reported != "" {
		use.Number = reported
	}
	t.ctl.SetLabel(use.Label())
	t.ctl.SetDetail("TypeScript: " + use.Lib + " (" + use.Source + ")")
}

// editorEvent offers the folder's own TypeScript the first time a TypeScript or JavaScript file is in front.
func (t *typescript) editorEvent(ev extension.Event) {
	var ae extension.ActiveEditor
	if ev.Decode(&ae) != nil || !isServed(ae.LanguageID) {
		return
	}
	t.maybeOffer()
}
