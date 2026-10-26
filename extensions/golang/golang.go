// Package golang is the Go extension (SPEC §8.1, §10.15, §10.17): the Go language pack (the grammars and language configuration
// are in assets/, copied by scripts/update-assets.sh; the licenses are in third_party/ and NOTICE) and, since step 14.1, Go's
// language features through the language server gopls: its settings (go.gopls), the command "Go: Restart Language Server", the
// status bar item with the Go version and the state of the server, formatting and organize imports on save (the defaults of the
// [go] language block), "Go: Install/Update Tools" with its panel view and the notification for a missing tool, and "Go: Show
// Go Environment". Step 14.2 adds the commands that gopls and the tools give: Add Tags To Struct Fields and Remove Tags From Struct
// Fields (gopls.modify_tags, else a Go port of gomodifytags), Fill Struct (the code action of gopls), Generate Unit Tests For
// Function (a table-driven skeleton in the style of gotests), Toggle Test File and Run (go run . in the panel view "Go Run"). The
// server itself is declared in the manifest and run by the host (SPEC §9.12). Step 14.3 adds the tests (code lenses from the
// extension's own provider, the commands Test Function At Cursor, Test File, Test Package and Test Previous, `go test -json` read into
// the panel view "Go Tests", the failures in Problems) and go vet and the lint tool on save into Problems.
//
// Every program the extension starts (go env, go install, go run, go test, go vet, the lint tool) runs in a process group of its own,
// under a timeout, a Cancel or a Stop, and ends with the activation.
package golang

import (
	"context"
	"embed"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/langpack"
)

// The ids of the extension.
const (
	// ID is the id of the extension.
	ID = "emetos.golang"
	// ServerID is the id of the language server gopls in the manifest.
	ServerID = "gopls"
	// InstallToolsCommand runs "go install" for the tools in the panel; ShowEnvironmentCommand writes `go env` to the Output
	// channel. RestartCommand is the command of the host that restarts gopls.
	InstallToolsCommand    = "emetos.golang.installTools"
	ShowEnvironmentCommand = "emetos.golang.showEnvironment"
	RestartCommand         = ID + ".restartServer"
	// ToolsViewID is the panel view of the tools.
	ToolsViewID = "emetos.golang.tools"
	// StatusID is the status bar item, and EnvironmentChannel the Output channel of Show Go Environment.
	StatusID           = "status"
	EnvironmentChannel = "Go"
)

//go:embed all:assets
var assets embed.FS

// goLanguages are the languages gopls serves.
var goLanguages = []string{"go", "go.mod", "go.work"}

// New is the Factory of the extension.
func New() extension.Extension {
	return &golang{
		base: langpack.New(langpack.Spec{
			ID: ID, Name: "Go", Description: "Go language support: syntax, gopls, the Go tools and the Go version.",
			Assets: assets,
			LanguageServers: []extension.LanguageServerSpec{{
				ID: ServerID, Name: "gopls", Languages: goLanguages,
				Program: "gopls", PathSetting: GoplsPathSetting, ConfigurationSection: GoplsSetting,
				OwnUI: true, Category: "Go",
			}},
		}),
	}
}

// Manifest implements extension.Extension: the manifest of the pack with the contributions of step 14.1.
func (g *golang) Manifest() extension.Manifest {
	m := g.base.Manifest()
	m.Contributes.Commands = append(m.Contributes.Commands,
		extension.CommandSpec{ID: InstallToolsCommand, Title: "Install/Update Tools", Category: "Go", Description: "Installs or updates gopls, dlv and staticcheck with go install, in the panel."},
		extension.CommandSpec{ID: ShowEnvironmentCommand, Title: "Show Go Environment", Category: "Go", Description: "Writes the output of go env to the Output panel."},
	)
	m.Contributes.Commands = append(m.Contributes.Commands, commandSpecs()...)
	m.Contributes.Menus = append(m.Contributes.Menus, menuItems()...)
	m.Contributes.PanelViews = append(m.Contributes.PanelViews,
		extension.PanelViewSpec{ID: ToolsViewID, Title: "Go Tools", Icon: "tools", Order: 50},
		extension.PanelViewSpec{ID: RunViewID, Title: "Go Run", Icon: "play", Order: 51},
		extension.PanelViewSpec{ID: TestsViewID, Title: "Go Tests", Icon: "beaker", Order: 52})
	m.Contributes.Settings = append(m.Contributes.Settings, settingSpecs()...)
	m.Events.Subscribes = append(m.Events.Subscribes, extension.TopicEditorActiveChanged, extension.TopicEditorSelectionChanged, extension.TopicDocumentSaved)
	return m
}

// handlers are the commands of the extension and what runs them.
func (g *golang) handlers() map[string]extension.CommandHandler {
	return map[string]extension.CommandHandler{
		InstallToolsCommand:    g.installToolsCommand,
		ShowEnvironmentCommand: g.showEnvironmentCommand,
		AddTagsCommand:         g.addTagsCommand,
		RemoveTagsCommand:      g.removeTagsCommand,
		FillStructCommand:      g.fillStructCommand,
		GenerateTestsCommand:   g.generateTestsCommand,
		ToggleTestFileCommand:  g.toggleTestFileCommand,
		RunCommand:             g.runCommand,
		TestCursorCommand:      g.testCursorCommand,
		BenchmarkCursorCommand: g.benchmarkCursorCommand,
		TestFileCommand:        g.testFileCommand,
		TestPackageCommand:     g.testPackageCommand,
		TestPreviousCommand:    g.testPreviousCommand,
		TestCancelCommand:      g.testCancelCommand,
		TestFunctionCommand:    g.testFunctionCommand,
		VetPackageCommand:      g.diagCommandHandler(false, scopePackage),
		VetWorkspaceCommand:    g.diagCommandHandler(false, scopeWorkspace),
		LintPackageCommand:     g.diagCommandHandler(true, scopePackage),
		LintWorkspaceCommand:   g.diagCommandHandler(true, scopeWorkspace),
	}
}

// commandSpecs are the commands the manifest declares besides those of the tools: the commands of VS Code's Go extension that
// gopls and the tools give (step 14.2), with its titles.
func commandSpecs() []extension.CommandSpec {
	return []extension.CommandSpec{
		{ID: AddTagsCommand, Title: "Add Tags To Struct Fields", Category: "Go", Description: "Adds tags to the fields of the struct at the cursor or in the selection, as set in go.addTags."},
		{ID: RemoveTagsCommand, Title: "Remove Tags From Struct Fields", Category: "Go", Description: "Removes tags from the fields of the struct at the cursor or in the selection, as set in go.removeTags."},
		{ID: FillStructCommand, Title: "Fill Struct", Category: "Go", Description: "Fills the struct literal at the cursor with the fields it lacks, with the code action of gopls."},
		{ID: GenerateTestsCommand, Title: "Generate Unit Tests For Function", Category: "Go", Description: "Writes a table-driven test for the function at the cursor into the test file of the file."},
		{ID: ToggleTestFileCommand, Title: "Toggle Test File", Category: "Go", Description: "Shows the test file of the file in front, or the file of the test file."},
		{ID: RunCommand, Title: "Run", Category: "Go", Description: "Runs go run . in the folder of the Go file in front and shows the output in the Go Run panel."},
		// step 14.3; the titles are those of VS Code's Go extension ("Go: Test Function At Cursor", "Go: Test File", ...)
		{ID: TestCursorCommand, Title: "Test Function At Cursor", Category: "Go", Description: "Runs the test, example or fuzz function at the cursor with go test and shows the result in the Go Tests panel."},
		{ID: BenchmarkCursorCommand, Title: "Benchmark Function At Cursor", Category: "Go", Description: "Runs the benchmark function at the cursor with go test -bench."},
		{ID: TestFileCommand, Title: "Test File", Category: "Go", Description: "Runs all the tests of the test file in front."},
		{ID: TestPackageCommand, Title: "Test Package", Category: "Go", Description: "Runs all the tests of the package of the Go file in front."},
		{ID: TestPreviousCommand, Title: "Test Previous", Category: "Go", Description: "Runs the last test run again."},
		{ID: TestCancelCommand, Title: "Cancel Running Tests", Category: "Go", Description: "Stops the test run that goes on."},
		// the lenses run this one; it has no title, so it is not in the palette
		{ID: TestFunctionCommand, Category: "Go", Description: "Runs one test or benchmark by its name: {\"uri\": ..., \"name\": ...}."},
		{ID: VetPackageCommand, Title: "Vet Current Package", Category: "Go", Description: "Runs go vet in the package of the Go file in front and puts the findings into the Problems panel."},
		{ID: VetWorkspaceCommand, Title: "Vet Workspace", Category: "Go", Description: "Runs go vet ./... in the folder of the workspace."},
		{ID: LintPackageCommand, Title: "Lint Current Package", Category: "Go", Description: "Runs the lint tool of go.lintTool in the package of the Go file in front."},
		{ID: LintWorkspaceCommand, Title: "Lint Workspace", Category: "Go", Description: "Runs the lint tool of go.lintTool in the folder of the workspace."},
	}
}

// menuItems are the items of the context menu of the editor, for Go files, where VS Code's Go extension puts them.
func menuItems() []extension.MenuItem {
	const when = "editorTextFocus && editorLangId == go"
	var items []extension.MenuItem
	for i, id := range []string{ToggleTestFileCommand, AddTagsCommand, RemoveTagsCommand, FillStructCommand, GenerateTestsCommand} {
		items = append(items, extension.MenuItem{Menu: extension.MenuEditorContext, Command: id, Group: "Go group 1", Order: float64(i + 1), When: when})
	}
	return items
}

// golang is the extension of one scope. Everything in it runs on the UI thread unless a comment says otherwise.
type golang struct {
	base extension.Extension
	api  extension.API
	ctx  context.Context

	// version is the answer of `go env GOVERSION` ("go1.26.1"), and goError says why there is none.
	version, goError string
	// server is the last state of gopls the host told, activeGo says that the editor in front is a Go file.
	server   extension.ServerStatus
	activeGo bool
	// editor is the text editor the user looked at last (editor.go).
	editor lastEditor

	status    extension.StatusHandle
	statusKey string
	visible   bool
	// env is the Output channel of Show Go Environment, made when it is first needed.
	env extension.OutputChannel
	// settingsKey is what the settings that decide the programs were when the version was last asked for.
	settingsKey string
	// versionRun counts the questions for the version: an answer of an older one is dropped.
	versionRun int

	tools *toolsPanel
	// runs is the panel view of Go: Run.
	runs *runPanel
	// tests is the panel view "Go Tests" and lastTest the request of the last run, which Test Previous and Run Again run again.
	tests    *testsPanel
	lastTest *testRequest
	// lensesOn is go.enableCodeLens.runtest: the provider of the lenses reads it off the UI thread.
	lensesOn atomic.Bool
	// debounce is the wait after a save; timers are the runs that wait, and diagRuns the runs that go on, by their key; vetSet and
	// lintSet remember the files the markers of go vet and of the lint tool are in.
	debounce time.Duration
	timers   map[string]*time.Timer
	diagRuns map[string]*diagRun
	vetSet   *markerSet
	lintSet  *markerSet
	// missing are the tools the user was told are missing, once each.
	missing map[string]bool
	// closers waits for the programs that were started, so that a test can see that none is left.
	closers sync.WaitGroup
}

// Activate implements extension.Extension.
func (g *golang) Activate(ctx context.Context, api extension.API) error {
	if err := g.base.Activate(ctx, api); err != nil {
		return err
	}
	g.api, g.ctx = api, ctx
	g.missing = map[string]bool{}
	g.debounce, g.timers, g.diagRuns = saveDebounce, map[string]*time.Timer{}, map[string]*diagRun{}
	g.vetSet, g.lintSet = newMarkerSet(VetMarkerOwner), newMarkerSet(LintMarkerOwner)
	var err error
	if g.status, err = api.StatusBar().Add(extension.StatusItem{ID: StatusID, Name: "Go", Alignment: extension.StatusRight, Priority: statusPriority}); err != nil {
		return err
	}
	g.status.SetVisible(false)
	g.tools = newToolsPanel(g)
	if g.tools.handle, err = api.Views().Provide(ToolsViewID, extension.ViewContent{Build: g.tools.build}); err != nil {
		return err
	}
	g.runs = newRunPanel(g)
	if g.runs.handle, err = api.Views().Provide(RunViewID, extension.ViewContent{Build: g.runs.build}); err != nil {
		return err
	}
	g.tests = newTestsPanel(g)
	if g.tests.handle, err = api.Views().Provide(TestsViewID, extension.ViewContent{Build: g.tests.build}); err != nil {
		return err
	}
	g.readLensSetting()
	if _, err = api.Languages().RegisterCodeLensProvider(extension.DocumentSelector{Languages: []string{"go"}}, goLenses{g}); err != nil {
		return err
	}
	for id, handler := range g.handlers() {
		if err := api.Commands().Handle(id, handler); err != nil {
			return err
		}
	}
	if g.server, err = api.LanguageServers().Status(ServerID); err != nil {
		return err
	}
	if _, err = api.LanguageServers().OnChange(ServerID, g.serverChanged); err != nil {
		return err
	}
	for _, topic := range []string{extension.TopicEditorActiveChanged, extension.TopicEditorSelectionChanged} {
		if _, err = api.Events().Subscribe(topic, g.editorEvent); err != nil {
			return err
		}
	}
	if _, err = api.Events().Subscribe(extension.TopicDocumentSaved, g.documentSaved); err != nil {
		return err
	}
	g.settingsKey = g.programSettings()
	api.Settings().OnChange(g.settingsChanged)
	api.Settings().OnChange(g.lensSettingChanged)
	for _, uri := range api.Documents().Open() {
		g.activeGo = g.activeGo || goURI(uri)
	}
	g.askVersion()
	g.updateStatus()
	return nil
}

// readLensSetting reads go.enableCodeLens.runtest.
func (g *golang) readLensSetting() {
	m, err := extension.SettingValue[map[string]any](g.api.Settings(), EnableCodeLensSetting)
	on := defaultCodeLensTest
	if err == nil {
		if v, ok := m["runtest"].(bool); ok {
			on = v
		}
	}
	g.lensesOn.Store(on)
}

// lensSettingChanged asks the editors for the lenses again when go.enableCodeLens.runtest changed.
func (g *golang) lensSettingChanged() {
	before := g.lensesOn.Load()
	g.readLensSetting()
	if g.lensesOn.Load() != before {
		g.api.Languages().RefreshCodeLenses()
	}
}

// goURI tells whether the URI is of a Go file, by its name: the editor tells the language of the editor in front only when it
// changes.
func goURI(uri string) bool {
	return strings.HasSuffix(uri, ".go") || strings.HasSuffix(uri, "/go.mod") || strings.HasSuffix(uri, "/go.work")
}

// goLanguage tells whether the language is one gopls serves.
func goLanguage(id string) bool {
	for _, l := range goLanguages {
		if l == id {
			return true
		}
	}
	return false
}

// wait waits for the programs the extension started to end (the tests use it).
func (g *golang) wait() { g.closers.Wait() }
