// Package python is the Python language pack (SPEC §8.1, §10.15, §10.34): Python. The grammars and language configurations are in assets/,
// copied by scripts/update-assets.sh; the licenses are in third_party/ and NOTICE. Since step 24b.4 the pack also runs a language server:
// basedpyright by default, Pyright or python-lsp-server when python.languageServer says so, or none. The server is found, started and
// shown by extensions/internal/langserver (the path settings basedpyright.path, pyright.path and pylsp.path, PATH, the folder of the
// interpreter, the status item, the notification for a missing server).
//
// The pack also owns the choice of the Python interpreter (interpreter.go, interpreters.go): the discovery, "Python: Select Interpreter"
// (python.setInterpreter) with the status item that shows the interpreter's version and environment, the choice per folder kept in
// Emetos's state, and Interpreters.Current, which the debugger (M25) and the Testing provider (M26) of the pack read so that every
// feature runs the same Python.
//
// Since step 26.6 the pack is also the Testing provider of Python (testctl.go, testexec.go, testcover.go; SPEC §10.32): the controller
// "python" runs pytest or unittest through the helper scripts in helpers/, with coverage.py for coverage and debugpy for Debug.
package python

import (
	"context"
	"embed"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/langpack"
	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

//go:embed all:assets
var assets embed.FS

// New is the Factory of the pack.
func New() extension.Extension {
	p := &python{runner: runProcess, testDebounce: saveDebounce}
	p.base = langpack.New(langpack.Spec{
		ID: ID, Name: "Python", Description: "Python support: syntax, the language server (basedpyright, Pyright or python-lsp-server) and the interpreter.",
		Assets: assets, Server: p.serverConfig(),
	})
	return p
}

// python is the extension of one scope. Everything in it runs on the UI thread unless a comment says otherwise.
type python struct {
	base extension.Extension
	api  extension.API
	// ctx is the context of the activation; it is set before the launcher is, and only read afterwards.
	ctx context.Context
	ctl *langserver.Controller

	// interps is the interpreter of the scope; offer remembers that the folder's own environment was offered once.
	interps *Interpreters
	offer   offer
	// defaultPath is python.defaultInterpreterPath as the pack last saw it.
	defaultPath string
	// editorSnap is the editor in front for the goroutines of the providers of the debugger (debug.go).
	editorSnap atomic.Pointer[editorSnapshot]
	// tests is the controller of the tests in the Testing view (step 26.6); nil in a window that has none. runner runs the programs of the
	// provider (the tests replace it) and closers waits for the goroutines it started.
	tests   *testCtl
	runner  testRunner
	closers sync.WaitGroup
	// testKey is the settings that decide the tests, as the pack last saw them.
	testKey string
	// testDebounce is how long the tests wait for more saves before they are found again.
	testDebounce time.Duration
}

// Manifest implements extension.Extension: the manifest of the pack with the commands and the settings of the servers.
func (p *python) Manifest() extension.Manifest {
	m := p.base.Manifest()
	m.Contributes.Commands = append(m.Contributes.Commands, commandSpecs()...)
	m.Contributes.Settings = append(m.Contributes.Settings, settingSpecs()...)
	m.Contributes.Settings = append(m.Contributes.Settings, debugSettingSpecs()...)
	m.Contributes.Settings = append(m.Contributes.Settings, testSettingSpecs()...)
	m.Contributes.Debuggers = append(m.Contributes.Debuggers, debuggerSpecs()...)
	m.Contributes.Breakpoints = append(m.Contributes.Breakpoints, extension.BreakpointSpec{Language: "python"})
	m.Requirements = append(m.Requirements, requirements()...)
	return m
}

// Activate implements extension.Extension.
func (p *python) Activate(ctx context.Context, api extension.API) error {
	p.api, p.ctx = api, ctx
	folder := api.Scope().Folder
	p.interps = newInterpreters(ctx, api, func(s *langserver.Settings) langserver.Env { return langserver.DefaultEnv(folder, s) })
	p.interps.updated = p.updateStatus
	if err := p.base.Activate(ctx, api); err != nil {
		return err
	}
	p.ctl = langpack.Controller(p.base)
	for id, handler := range p.handlers() {
		if err := api.Commands().Handle(id, handler); err != nil {
			return err
		}
	}
	if _, err := api.Events().Subscribe(extension.TopicEditorActiveChanged, p.editorEvent); err != nil {
		return err
	}
	if _, err := api.Languages().RegisterCodeActionProvider(extension.DocumentSelector{Languages: []string{"python"}}, organizeImportsProvider{p}); err != nil {
		return err
	}
	p.registerDebug()
	if err := p.registerTests(); err != nil {
		return err
	}
	p.defaultPath, _ = extension.SettingValue[string](api.Settings(), DefaultInterpreterSetting)
	api.Settings().OnChange(p.settingsChanged)
	api.Settings().OnChange(p.testSettingsChanged)
	p.interps.Refresh()
	return nil
}

// settingsChanged looks for the interpreter again when python.defaultInterpreterPath changed: it is the only setting that decides it, and
// looking may run a program (--version), which a change of any other setting must not do.
func (p *python) settingsChanged() {
	now, _ := extension.SettingValue[string](p.api.Settings(), DefaultInterpreterSetting)
	if now == p.defaultPath {
		return
	}
	p.defaultPath = now
	p.interps.Refresh()
}

// Interpreters is the interpreter choice of the scope: Current is the one in use, Choose changes it, OnChange follows it. The debugger and
// the Testing provider of the pack read it. It is nil before Activate.
func (p *python) Interpreters() *Interpreters { return p.interps }

// editorEvent offers the folder's own environment the first time a Python file is in front.
func (p *python) editorEvent(ev extension.Event) {
	var ae extension.ActiveEditor
	if ev.Decode(&ae) != nil {
		return
	}
	p.editorSnap.Store(&editorSnapshot{uri: ae.URI, language: ae.LanguageID})
	if ae.LanguageID != "python" {
		return
	}
	p.interps.maybeOffer(&p.offer)
}

// updateStatus shows the interpreter in the status item of the server: its version and environment as the text, its path and the rule
// that chose it in the tooltip.
func (p *python) updateStatus() {
	if p.ctl == nil {
		return
	}
	i, ok := p.interps.Current()
	if !ok {
		p.ctl.SetLabel("Select Interpreter")
		p.ctl.SetDetail("No Python interpreter was found. Choose one with Select Interpreter... in the menu, or set python.defaultInterpreterPath.")
		return
	}
	p.ctl.SetLabel(i.Label())
	p.ctl.SetDetail("Interpreter: " + i.Path + " (" + p.interps.reason() + ")")
}
