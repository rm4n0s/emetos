// Package rust is the Rust language pack (SPEC §8.1, §10.15, §10.34): Rust. The grammars and language configurations are in assets/,
// copied by scripts/update-assets.sh; the licenses are in third_party/ and NOTICE. Since step 24b.2 the pack also runs rust-analyzer:
// the server is found, started and shown by extensions/internal/langserver (the path setting rust-analyzer.server.path, PATH,
// ~/.cargo/bin, the status item, the notification for a missing server), the settings of rust-analyzer's VS Code extension that change
// the server are registered (ra-settings.json, written by scripts/update-rust-analyzer-settings.sh) and sent to it, and the pack adds
// the commands Expand macro recursively at caret and View Syntax Tree (the result in a read-only tab), Locate parent module, Open
// Cargo.toml, Join lines, Find matching brace, Reload workspace and Restart server, the Run lenses (which run through the Tasks
// extension) and rust-analyzer's own status (experimental/serverStatus) in the status item.
package rust

import (
	"context"
	"embed"
	"sync"
	"sync/atomic"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/langpack"
	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

// The ids of the extension.
const (
	// ID is the id of the extension, and ServerID the id of the language server rust-analyzer in its manifest.
	ID       = "emetos.rust"
	ServerID = "rust-analyzer"
	// PathSetting holds the path of the rust-analyzer program (the name VS Code's extension gives it).
	PathSetting = "rust-analyzer.server.path"
	// TraceSetting traces the traffic with the server (the name VS Code's extension gives it).
	TraceSetting = "rust-analyzer.trace.server"
	// SettingPrefix starts the key of every setting of rust-analyzer.
	SettingPrefix = "rust-analyzer."
)

// The commands of the pack, with the ids of rust-analyzer's VS Code extension.
const (
	// ExpandMacroCommand shows the expansion of the macro at the cursor in a read-only tab.
	ExpandMacroCommand = "rust-analyzer.expandMacro"
	// ViewSyntaxTreeCommand shows the syntax tree of the file (or the selection) in a read-only tab.
	ViewSyntaxTreeCommand = "rust-analyzer.viewSyntaxTree"
	// ParentModuleCommand goes to the module that declares the one of the file.
	ParentModuleCommand = "rust-analyzer.parentModule"
	// OpenCargoTomlCommand opens the Cargo.toml of the package of the file.
	OpenCargoTomlCommand = "rust-analyzer.openCargoToml"
	// JoinLinesCommand joins the lines of the selections the way rust-analyzer does (it removes the braces and semicolons a join leaves).
	JoinLinesCommand = "rust-analyzer.joinLines"
	// MatchingBraceCommand moves the cursor to the bracket that matches the one at it.
	MatchingBraceCommand = "rust-analyzer.matchingBrace"
	// ReloadWorkspaceCommand makes rust-analyzer read the Cargo workspace again.
	ReloadWorkspaceCommand = "rust-analyzer.reloadWorkspace"
	// RestartServerCommand restarts rust-analyzer.
	RestartServerCommand = "rust-analyzer.restartServer"
)

// ContentScheme is the scheme of the read-only documents the pack provides (the expansion of a macro, the syntax tree).
const ContentScheme = "rust-analyzer"

//go:embed all:assets
var assets embed.FS

// rustLanguages are the languages rust-analyzer serves.
var rustLanguages = []string{"rust"}

// New is the Factory of the pack.
func New() extension.Extension {
	r := &rust{content: map[string]string{}}
	r.base = langpack.New(langpack.Spec{
		ID: ID, Name: "Rust", Description: "Rust language support: syntax, rust-analyzer and its commands.",
		Assets: assets, Server: r.serverConfig(),
	})
	return r
}

// rust is the extension of one scope. Everything in it runs on the UI thread unless a comment says otherwise.
type rust struct {
	base extension.Extension
	api  extension.API
	ctx  context.Context
	ctl  *langserver.Controller

	// reg is the provider of the read-only documents; nil when the scope cannot show them (the text opens in an untitled document).
	reg extension.ContentRegistration
	// mu guards content, which the provider reads off the UI thread: the text of each read-only document by its path.
	mu      sync.Mutex
	content map[string]string
	// wasRunning and quiescent follow the server, to ask for the lenses again when they can have changed (status.go).
	wasRunning, quiescent bool
	// lensesOn is rust-analyzer.lens.enable and lens.run.enable: the provider of the lenses reads it off the UI thread.
	lensesOn atomic.Bool
	// work waits for the requests in flight, so that a test can see that none is left.
	work sync.WaitGroup
}

// Manifest implements extension.Extension: the manifest of the pack with the commands, keybindings and settings of rust-analyzer.
func (r *rust) Manifest() extension.Manifest {
	m := r.base.Manifest()
	m.Contributes.Commands = append(m.Contributes.Commands, commandSpecs()...)
	m.Contributes.Keybindings = append(m.Contributes.Keybindings, keybindings()...)
	m.Contributes.Settings = append(m.Contributes.Settings, settingSpecs()...)
	return m
}

// Activate implements extension.Extension.
func (r *rust) Activate(ctx context.Context, api extension.API) error {
	if err := r.base.Activate(ctx, api); err != nil {
		return err
	}
	r.api, r.ctx = api, ctx
	r.ctl = langpack.Controller(r.base)
	for id, handler := range r.handlers() {
		if err := api.Commands().Handle(id, handler); err != nil {
			return err
		}
	}
	if err := r.provideContent(); err != nil {
		return err
	}
	if _, err := api.LanguageServers().OnNotification(ServerID, statusMethod, r.serverStatus); err != nil {
		return err
	}
	r.readLensSettings()
	if _, err := api.Languages().RegisterCodeLensProvider(extension.DocumentSelector{Languages: rustLanguages}, lenses{r}); err != nil {
		return err
	}
	if _, err := api.LanguageServers().OnChange(ServerID, r.serverChanged); err != nil {
		return err
	}
	api.Settings().OnChange(r.lensSettingsChanged)
	return nil
}

// wait waits for the requests the pack sent to end (the tests use it).
func (r *rust) wait() { r.work.Wait() }
