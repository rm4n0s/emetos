// Package cpp is the C and C++ language pack (SPEC §8.1, §10.15, §10.34): C, C++ and CUDA C++. The grammars and language configurations
// are in assets/, copied by scripts/update-assets.sh; the licenses are in third_party/ and NOTICE. Since step 24b.3 the pack also runs
// clangd: the server is found, started and shown by extensions/internal/langserver (the path setting clangd.path, PATH, the status item,
// the notification for a missing server), clangd.arguments and clangd.fallbackFlags are sent to it, the status item says when the
// project has no compile_commands.json and offers a .clangd with a CompileFlags stub, clangd's own file status (the state of the file in
// front) is shown in the item, and the pack adds the commands Switch Between Source/Header (Alt+O), Show AST (a read-only tab), Toggle
// inlay hints, Open project configuration file and Restart language server with vscode-clangd's ids. Since step 25.9 the pack also owns the
// debuggers of native programs, gdb and lldb-dap, for C, C++ and Rust (extensions/internal/nativedebug; the Rust pack adds its cargo targets).
package cpp

import (
	"context"
	"embed"
	"sync"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/langpack"
	"github.com/rm4n0s/emetos/extensions/internal/langserver"
	"github.com/rm4n0s/emetos/extensions/internal/nativedebug"
)

// The ids of the extension.
const (
	// ID is the id of the extension, and ServerID the id of the language server clangd in its manifest.
	ID       = "emetos.cpp"
	ServerID = "clangd"
	// PathSetting holds the path of the clangd program (the name vscode-clangd gives it).
	PathSetting = "clangd.path"
	// ArgumentsSetting holds the arguments clangd is started with.
	ArgumentsSetting = "clangd.arguments"
	// FallbackFlagsSetting holds the flags clangd parses a file with when the project has no compilation database.
	FallbackFlagsSetting = "clangd.fallbackFlags"
	// TraceFileSetting names a file clangd writes a performance trace to (CLANGD_TRACE).
	TraceFileSetting = "clangd.trace"
	// TraceSetting traces the traffic with the server (the host's, as for the other packs).
	TraceSetting = "clangd.trace.server"
)

// The commands of the pack, with the ids of vscode-clangd.
const (
	// SwitchHeaderSourceCommand opens the header of a source file, or the source of a header.
	SwitchHeaderSourceCommand = "clangd.switchheadersource"
	// RestartCommand restarts clangd.
	RestartCommand = "clangd.restart"
	// ASTCommand shows the syntax tree of clangd for the selection, or the cursor, in a read-only tab.
	ASTCommand = "clangd.ast"
	// ToggleInlayHintsCommand turns the inlay hints of the editor on or off.
	ToggleInlayHintsCommand = "clangd.inlayHints.toggle"
	// ProjectConfigCommand opens the .clangd of the project, and offers to create it when there is none.
	ProjectConfigCommand = "clangd.projectConfig"
)

// ContentScheme is the scheme of the read-only documents the pack provides (the AST).
const ContentScheme = "clangd"

// InlayHintsSetting is the setting of the editor the command Toggle inlay hints flips.
const InlayHintsSetting = "editor.inlayHints.enabled"

//go:embed all:assets
var assets embed.FS

// cppLanguages are the languages clangd serves.
var cppLanguages = []string{"c", "cpp", "cuda-cpp"}

// New is the Factory of the pack.
func New() extension.Extension {
	c := &cpp{content: map[string]string{}, states: map[string]string{}, runner: runProcess, testDebounce: saveDebounce}
	c.base = langpack.New(langpack.Spec{
		ID: ID, Name: "C and C++", Description: "C, C++ and CUDA C++ support: syntax, clangd and its commands.",
		Assets: assets, Server: c.serverConfig(),
	})
	return c
}

// cpp is the extension of one scope. Everything in it runs on the UI thread unless a comment says otherwise.
type cpp struct {
	base extension.Extension
	api  extension.API
	ctx  context.Context
	ctl  *langserver.Controller
	// debugger is the debugger of native programs (gdb and lldb-dap).
	debugger *nativedebug.Debugger

	// reg is the provider of the read-only documents; nil when the scope cannot show them (the text opens in an untitled document).
	reg extension.ContentRegistration
	// mu guards content, which the provider reads off the UI thread: the text of each read-only document by its path.
	mu      sync.Mutex
	content map[string]string
	// states is the state clangd last told for each file (textDocument/clangd.fileStatus), by URI; running is whether the server runs.
	states  map[string]string
	running bool
	// work waits for the requests in flight, so that a test can see that none is left.
	work sync.WaitGroup

	// tests is the controller of the tests in the Testing view (step 26.9); nil in a window that has none. runner runs the programs of
	// the provider (the tests replace it) and testDebounce is how long the tests wait for more saves before they are found again.
	// lastBuild and lastDelimiter are the settings the tree was last made with.
	tests                    *testCtl
	runner                   testRunner
	testDebounce             time.Duration
	lastBuild, lastDelimiter string
}

// Manifest implements extension.Extension: the manifest of the pack with the commands, keybindings and settings of clangd.
func (c *cpp) Manifest() extension.Manifest {
	m := c.base.Manifest()
	m.Contributes.Commands = append(m.Contributes.Commands, commandSpecs()...)
	m.Contributes.Keybindings = append(m.Contributes.Keybindings, keybindings()...)
	m.Contributes.Settings = append(m.Contributes.Settings, settingSpecs()...)
	m.Contributes.Settings = append(m.Contributes.Settings, testSettingSpecs()...)
	m.Contributes.WritesSettings = append(m.Contributes.WritesSettings, InlayHintsSetting)
	m.Contributes.Debuggers = append(m.Contributes.Debuggers, nativedebug.Specs()...)
	m.Contributes.Settings = append(m.Contributes.Settings, nativedebug.SettingSpecs()...)
	for _, lang := range nativedebug.BreakpointLanguages {
		m.Contributes.Breakpoints = append(m.Contributes.Breakpoints, extension.BreakpointSpec{Language: lang})
	}
	return m
}

// Activate implements extension.Extension.
func (c *cpp) Activate(ctx context.Context, api extension.API) error {
	if err := c.base.Activate(ctx, api); err != nil {
		return err
	}
	c.api, c.ctx = api, ctx
	c.ctl = langpack.Controller(c.base)
	for id, handler := range c.handlers() {
		if err := api.Commands().Handle(id, handler); err != nil {
			return err
		}
	}
	if err := c.provideContent(); err != nil {
		return err
	}
	if _, err := api.LanguageServers().OnNotification(ServerID, fileStatusMethod, c.fileStatus); err != nil {
		return err
	}
	if _, err := api.LanguageServers().OnChange(ServerID, c.serverChanged); err != nil {
		return err
	}
	if _, err := api.Events().Subscribe(extension.TopicEditorActiveChanged, func(extension.Event) { c.refreshStatus() }); err != nil {
		return err
	}
	c.debugger = nativedebug.New(api)
	c.debugger.Register()
	if err := c.registerTests(); err != nil {
		return err
	}
	api.Settings().OnChange(c.refreshStatus)
	c.refreshStatus()
	return nil
}

// wait waits for the requests the pack sent to end (the tests use it).
func (c *cpp) wait() {
	if c.tests != nil {
		c.tests.stop()
	}
	c.work.Wait()
}
