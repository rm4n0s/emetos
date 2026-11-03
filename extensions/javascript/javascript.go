// Package javascript is the JavaScript language pack (SPEC §8.1, §10.15): JavaScript and JavaScript React. The grammars and language configurations
// are in assets/, copied by scripts/update-assets.sh; the licenses are in third_party/ and NOTICE. The language server of these languages is
// typescript-language-server, which the TypeScript pack (extensions/typescript) runs for both packs, with the javascript.* settings and the
// commands of JavaScript (SPEC §10.34 "As built (step 24b.5)").
//
// Since step 25.10 the pack also owns the debugger of JavaScript and TypeScript (SPEC §10.31 "The debuggers"): the debug types node and
// pwa-node, served by js-debug, VS Code's own debugger, run as a server program by the user's node (debug.go); its configuration
// providers (debugconfig.go); the setting debug.javascript.adapterPath; and "JavaScript Debugger: Download" (jsdebug.go), which fetches
// the release pinned in this package, on the user's click only.
package javascript

import (
	"context"
	"embed"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/langpack"
	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

// ID is the id of the extension.
const ID = "emetos.javascript"

//go:embed all:assets
var assets embed.FS

// New is the Factory of the pack.
func New() extension.Extension {
	return &javascript{
		base: langpack.New(langpack.Spec{
			ID: ID, Name: "JavaScript", Description: "Syntax highlighting and language configuration for JavaScript and JavaScript React; the TypeScript pack serves them with typescript-language-server.",
			Assets: assets,
		}),
		release:      pinned(),
		client:       http.DefaultClient,
		dataDir:      dataDirFromEnv,
		osRelease:    langserver.ReadOSRelease,
		procRoot:     "/proc",
		runner:       runProcess,
		testDebounce: saveDebounce,
	}
}

// javascript is the extension of one scope. Everything in it runs on the UI thread unless a comment says otherwise. The members after
// the activation context are what the tests replace.
type javascript struct {
	base extension.Extension
	api  extension.API
	ctx  context.Context

	// editorSnap is the editor in front for the goroutines of the providers of the debugger (debugconfig.go).
	editorSnap atomic.Pointer[editorSnapshot]
	// downloading is set while "JavaScript Debugger: Download" runs, so that a second click does not start a second download.
	downloading atomic.Bool

	// release is what the download fetches, client how, and dataDir where it goes (it returns the folder js-debug is unpacked below).
	release release
	client  *http.Client
	dataDir func() (string, error)
	// osRelease reads the text that names the system, for the install command of node.
	osRelease func() string
	// procRoot is the folder that lists the processes (/proc), for the process picker.
	procRoot string

	// tests are the controllers of the tests in the Testing view (step 26.8), one for each runner a project of the folder uses; testingOn is
	// false in a window that has no Testing view. runner runs the programs of the provider (the tests replace it), testDebounce is how long
	// the tests wait for more saves before they are found again and work waits for the goroutines of the provider.
	tests        map[string]*testCtl
	testingOn    bool
	runner       testRunner
	testDebounce time.Duration
	work         sync.WaitGroup
}

// Manifest implements extension.Extension: the manifest of the pack with the debugger, its setting and its command.
func (j *javascript) Manifest() extension.Manifest {
	m := j.base.Manifest()
	m.Contributes.Commands = append(m.Contributes.Commands, commandSpecs()...)
	m.Contributes.Settings = append(m.Contributes.Settings, debugSettingSpecs()...)
	m.Events.Subscribes = append(m.Events.Subscribes, extension.TopicEditorActiveChanged, extension.TopicDocumentSaved, extension.TopicFilesChanged)
	m.Contributes.Debuggers = append(m.Contributes.Debuggers, debuggerSpecs()...)
	for _, lang := range breakpointLanguages {
		m.Contributes.Breakpoints = append(m.Contributes.Breakpoints, extension.BreakpointSpec{Language: lang})
	}
	return m
}

// Activate implements extension.Extension.
func (j *javascript) Activate(ctx context.Context, api extension.API) error {
	j.api, j.ctx = api, ctx
	if err := j.base.Activate(ctx, api); err != nil {
		return err
	}
	if err := api.Commands().Handle(DownloadCommand, j.downloadCommand); err != nil {
		return err
	}
	if _, err := api.Events().Subscribe(extension.TopicEditorActiveChanged, j.editorEvent); err != nil {
		return err
	}
	j.registerDebug()
	if err := j.registerTests(); err != nil {
		return err
	}
	return nil
}

// editorEvent keeps the editor in front for the goroutines of the providers.
func (j *javascript) editorEvent(ev extension.Event) {
	var ae extension.ActiveEditor
	if ev.Decode(&ae) != nil {
		return
	}
	j.editorSnap.Store(&editorSnapshot{uri: ae.URI, language: ae.LanguageID})
}

// wait waits for the requests the pack sent to end (the tests use it).
func (j *javascript) wait() {
	for _, tc := range j.tests {
		tc.stop()
	}
	j.work.Wait()
}
