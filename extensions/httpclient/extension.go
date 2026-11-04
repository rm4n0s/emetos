package httpclient

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/httpclient/engine"
	"github.com/rm4n0s/emetos/extensions/httpclient/history"
	"github.com/rm4n0s/emetos/extensions/httpclient/oauth"
	"github.com/rm4n0s/emetos/extensions/httpclient/store"
	"github.com/rm4n0s/emetos/extensions/httpclient/variables"
	"github.com/rm4n0s/emetos/extensions/internal/dbconn"
)

// The ids of the extension: the container of the activity bar and its three views, the editor kind of the environment editor, and the
// item of the status bar.
const (
	// ID is the id of the extension.
	ID = "emetos.httpclient"
	// ContainerID is the view container of the activity bar, "HTTP Client".
	ContainerID = ID + ".container"
	// CollectionsViewID, EnvironmentsViewID and HistoryViewID are the three views of the container.
	CollectionsViewID  = ID + ".collections"
	EnvironmentsViewID = ID + ".environments"
	HistoryViewID      = ID + ".history"
	// EnvironmentKindID is the kind of editor tab of the environment editor.
	EnvironmentKindID = ID + ".environment"
	// StatusEnvironmentID is the item of the status bar that names the active environment.
	StatusEnvironmentID = "environment"
)

// The keys of the state of the workspace (extension.State) the extension keeps; the history and the current values have theirs in
// package history and package variables.
const (
	// StateActiveEnvironment holds the key (the file name without its extension) of the active environment as a JSON string.
	StateActiveEnvironment = "httpclient.activeEnvironment"
)

// The settings of the extension (SPEC §10.33 "Commands"). Their keys are the SPEC's, not under the id of the extension.
const (
	SettingTimeoutMs         = "httpClient.timeoutMs"
	SettingVerifyTLS         = "httpClient.verifyTls"
	SettingProxy             = "httpClient.proxy"
	SettingClientCerts       = "httpClient.clientCertificates"
	SettingHistoryLimit      = "httpClient.historyLimit"
	SettingResponseLayout    = "httpClient.responseLayout"
	SettingAllowedProcessEnv = "httpClient.allowedProcessEnv"
	SettingMaxResponseMiB    = "httpClient.maxResponseMiB"
)

// Options are the parts of the extension a test or the host replaces.
type Options struct {
	// Keyring keeps the values of secret variables; the desktop's Secret Service when nil.
	Keyring variables.Secrets
	// Now is the clock; time.Now when nil.
	Now func() time.Time
	// Browser shows the authorization page of OAuth 2.0 to the user; the desktop's browser (Workspace().OpenURI) when nil. The tests give
	// a fake that follows the redirect.
	Browser oauth.Opener
	// OAuthTimeout is how long "Get New Access Token" waits for the user; oauth.DefaultTimeout when 0.
	OAuthTimeout time.Duration
}

// New is the Factory of the extension with the desktop keyring.
func New() extension.Extension { return &Extension{} }

// FactoryOf makes the Factory of an extension with options.
func FactoryOf(opts Options) extension.Factory {
	return func() extension.Extension { return &Extension{opts: opts} }
}

// Extension is the HTTP Client in one workspace.
type Extension struct {
	opts Options
	api  extension.API
	ctx  context.Context

	folder string
	// store is nil until the first scan of the files ends (Activate reads them off the UI thread); loadErr says why it could not be made.
	store   *store.Store
	loadErr error
	tree    *store.Tree
	values  *variables.Values
	hist    *history.Log

	collections  *collectionsView
	environments *environmentsView
	historyView  *historyView
	status       extension.StatusHandle

	// pending counts the jobs of async that have not handed their result to the UI thread yet; the tests wait for it to reach 0.
	pending atomic.Int32

	// envTabs are the environment editor tabs, which the tests look at.
	envTabs []*envTab

	// runTabs are the runner tabs.
	runTabs []*runTab

	// reqTabs are the request tabs, lastReq the one that was shown or had the focus last (the commands without an argument act on it).
	reqTabs []*reqTab
	lastReq *reqTab
	// jars are the cookies of the engine, one jar for each environment, kept in the state.
	jars *engine.Jars
	// preview reads the current values for the colors of the URL box; it has no keyring, so a secret is never read for a form.
	preview *variables.Values
	// extractMu keeps two sends from writing the values they extracted at the same time (the globals are a read-modify-write of a file).
	extractMu sync.Mutex
	// browser replaces the browser of OAuth 2.0 while the dev fake authorization server runs (debug builds only); nil: Options.Browser or the desktop's.
	browser oauth.Opener
	// oauthKeyring keeps the tokens in memory instead of the keyring while the dev fake authorization server runs; nil: the keyring.
	oauthKeyring variables.Secrets
}

// Manifest implements extension.Extension.
func (*Extension) Manifest() extension.Manifest {
	return extension.Manifest{
		ID: ID, Name: "HTTP Client", Version: "0.1.0",
		Description: "Collections of HTTP requests in the files of the project, environments, variables and a history of what was sent.",
		// onStartup: the status bar item names the active environment from the start. Activating reads nothing on the UI thread; the
		// files of .emetos/http are read in the background.
		ActivationEvents: []string{extension.OnStartup},
		Contributes: extension.Contributes{
			Commands:       commandSpecs(),
			ViewContainers: []extension.ViewContainerSpec{{ID: ContainerID, Title: "HTTP Client", Icon: "globe", Order: 45}},
			Views: []extension.ViewSpec{
				{ID: CollectionsViewID, Name: "Collections", Container: ContainerID, Order: 1},
				{ID: EnvironmentsViewID, Name: "Environments", Container: ContainerID, Order: 2},
				{ID: HistoryViewID, Name: "History", Container: ContainerID, Order: 3},
			},
			ViewActions: viewActions(),
			EditorKinds: []extension.EditorKindSpec{
				{ID: EnvironmentKindID, Name: "Environment", Icon: "symbol-variable"},
				{ID: RequestKindID, Name: "Request", Icon: "globe"},
				{ID: RunnerKindID, Name: "Runner", Icon: "run-all"},
			},
			EditorActions: []extension.EditorActionSpec{{Command: CmdOpenAsText, Icon: "go-to-file", Tooltip: "Open as Text", When: "activeEditorKind == '" + RequestKindID + "'", Order: 1}},
			Keybindings:   keybindingSpecs(),
			Settings:      settingSpecs(),
		},
	}
}

// Activate implements extension.Extension.
func (e *Extension) Activate(ctx context.Context, api extension.API) error {
	e.api, e.ctx = api, ctx
	e.folder = api.Scope().Folder
	if e.opts.Now == nil {
		e.opts.Now = time.Now
	}
	if e.opts.Keyring == nil {
		e.opts.Keyring = dbconn.NewSecretService()
	}
	e.values = variables.NewValues(e.folder, api.State(), e.opts.Keyring)
	e.preview = variables.NewValues(e.folder, api.State(), nil)
	e.jars = engine.NewJars(api.State())
	e.hist = history.Open(api.State(), history.Options{Limit: e.historyLimit, Now: e.opts.Now})
	e.collections, e.environments, e.historyView = newCollectionsView(e), newEnvironmentsView(e), newHistoryView(e)
	for _, v := range []interface{ provide() error }{e.collections, e.environments, e.historyView} {
		if err := v.provide(); err != nil {
			return err
		}
	}
	if err := api.Editors().Provide(RequestKindID, extension.EditorKind{Create: e.createRequestTab}); err != nil {
		return err
	}
	if err := api.Editors().Provide(RunnerKindID, extension.EditorKind{Create: e.createRunnerTab}); err != nil {
		return err
	}
	if err := api.Editors().Provide(EnvironmentKindID, extension.EditorKind{Create: e.createEnvTab}); err != nil {
		return err
	}
	if err := e.handleCommands(); err != nil {
		return err
	}
	e.registerDev()
	if err := e.watchRequestFiles(); err != nil {
		return err
	}
	item := e.statusItem()
	h, err := api.StatusBar().Add(item)
	if err != nil {
		return err
	}
	e.status = h
	h.SetVisible(false)
	stop := api.Settings().OnChange(func() { e.historyView.rebuild() })
	go func() {
		<-ctx.Done()
		stop()
	}()
	if e.folder != "" {
		go e.openStore()
	}
	return nil
}

// openStore reads the files off the UI thread and hands the store to it.
func (e *Extension) openStore() {
	s, err := store.Open(e.ctx, e.folder, e.api.Workspace(), store.Options{Logger: e.api.Log()})
	e.api.Post(func() {
		if err != nil {
			e.loadErr = err
			e.api.Log().Warn("the HTTP client cannot read its files", "err", err)
			e.refreshAll()
			return
		}
		e.store, e.tree = s, s.Snapshot()
		stop := s.OnChange(func() { e.api.Post(e.storeChanged) })
		go func() {
			<-e.ctx.Done()
			stop()
			s.Close()
		}()
		e.refreshAll()
	})
}

// storeChanged takes the new tree of the store in and builds the views again.
func (e *Extension) storeChanged() {
	if e.store == nil {
		return
	}
	e.tree = e.store.Snapshot()
	e.refreshAll()
}

// refreshAll builds the views, the status item and the open environment editors again.
func (e *Extension) refreshAll() {
	e.collections.rebuild()
	e.environments.rebuild()
	e.historyView.rebuild()
	e.updateStatus()
	for _, t := range e.envTabs {
		t.storeChanged()
	}
	for _, t := range append([]*runTab(nil), e.runTabs...) {
		t.storeChanged()
	}
	for _, t := range append([]*reqTab(nil), e.reqTabs...) {
		t.storeChanged()
	}
}

// historyLimit is the setting httpClient.historyLimit.
func (e *Extension) historyLimit() int {
	n, err := extension.SettingValue[int](e.api.Settings(), SettingHistoryLimit)
	if err != nil {
		return history.DefaultLimit
	}
	return n
}

// ---- helpers shared by the views ----

// fileURI is the URI of a file of the workspace.
func fileURI(path string) string { return (&url.URL{Scheme: "file", Path: path}).String() }

// ready says whether the files were read; when not it tells the user why and returns false.
func (e *Extension) ready() bool {
	switch {
	case e.folder == "":
		e.api.Notifications().Info("Open a folder to use the HTTP client: its collections are files of the folder.")
	case e.loadErr != nil:
		e.api.Notifications().Error("The HTTP client cannot read its files: " + e.loadErr.Error())
	case e.store == nil:
		e.api.Notifications().Info("The HTTP client is still reading its files.")
	default:
		return true
	}
	return false
}

// fail logs an error and tells the user which action failed.
func (e *Extension) fail(what string, err error) {
	e.api.Log().Warn("the HTTP client could not "+what, "err", err)
	e.api.Notifications().Error("Cannot " + what + ": " + err.Error())
}

// async runs work off the UI thread, with a context that ends with the extension, and hands its error to done on the UI thread.
func (e *Extension) async(work func(ctx context.Context) error, done func(err error)) {
	e.pending.Add(1)
	go func() {
		err := work(e.ctx)
		e.api.Post(func() {
			defer e.pending.Add(-1)
			done(err)
		})
	}()
}

// activeKey is the key of the active environment, or "".
func (e *Extension) activeKey() string {
	key, _ := extension.StateValue[string](e.api.State(), StateActiveEnvironment)
	return key
}

// activeEnvironment is the node of the active environment, or nil when none is chosen or the file is gone.
func (e *Extension) activeEnvironment() *store.Node {
	key := e.activeKey()
	if key == "" || e.tree == nil {
		return nil
	}
	for _, n := range e.tree.Environments {
		if n.Key == key {
			return n
		}
	}
	return nil
}

// setActive makes the environment with the key the active one ("" for none).
func (e *Extension) setActive(key string) {
	var v any
	if key != "" {
		v = key
	}
	if err := e.api.State().Set(StateActiveEnvironment, v); err != nil {
		e.fail("keep the active environment", err)
		return
	}
	e.environments.rebuild()
	e.updateStatus()
}

// copyText puts text on the clipboard.
func (e *Extension) copyText(text string) {
	if err := e.api.Clipboard().WriteText(text); err != nil {
		e.api.Log().Warn("the clipboard did not take the text", "err", err)
		e.api.Notifications().Warn("Cannot copy to the clipboard: " + err.Error())
	}
}

// argString reads a text a command was given: a string, or {"name": "..."}.
func argString(args any, key string) string {
	switch a := args.(type) {
	case string:
		return a
	case map[string]any:
		s, _ := a[key].(string)
		return s
	case json.RawMessage:
		var m map[string]any
		if json.Unmarshal(a, &m) == nil {
			s, _ := m[key].(string)
			return s
		}
	}
	return ""
}

// plural is "1 variable", "2 variables".
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}
