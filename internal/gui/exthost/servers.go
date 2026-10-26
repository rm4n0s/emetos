package exthost

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/glob"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/core/lsp"
	"github.com/rm4n0s/emetos/internal/core/notify"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/workspace"
)

// Language servers (SPEC §9.12 "Servers and their lifetime", "As built (step 12.3)"): the servers an extension declares in
// its manifest are run by the host, one per window tab and server, as part of the activation of the extension in that tab:
// the activation's end (the tab closes, the extension is turned off or fails) stops them.

// LanguageWorkspace is what a window offers to the language servers of its tab: its documents and where they come and go,
// the events of its file watcher, the text of the files that answers refer to, and the application of workspace edits.
// Documents and the document watchers belong to the UI thread; the watcher of files calls its function from the goroutine of
// the watcher.
type LanguageWorkspace interface {
	// Documents lists the documents that are open now.
	Documents() []*document.Document
	// WatchOpen calls fn with each document that is opened from now on, and WatchClose with each that is closed.
	WatchOpen(fn func(*document.Document)) (stop func())
	WatchClose(fn func(*document.Document)) (stop func())
	// WatchFiles calls fn with each batch of events of the file watcher, paths absolute.
	WatchFiles(fn func([]workspace.Event)) (stop func())
	// OpenText gives a snapshot of the document with the URI when it is open (UI thread). ReadText reads the file with the URI
	// from disk (any goroutine). Together they give the text that the positions of an answer refer to.
	OpenText(uri document.URI) (lsp.Lines, bool)
	ReadText(ctx context.Context, uri document.URI) (lsp.Lines, error)
	// Edits applies workspace edits to the documents and files of the window.
	Edits() langfeatures.EditHost
}

// ServerState is what a language server of an extension is in a window tab.
type ServerState int

// The states of a server.
const (
	// ServerIdle: declared, and waiting for the first document of its languages.
	ServerIdle ServerState = iota
	// ServerStarting: the program was started and the handshake is not done.
	ServerStarting
	// ServerRunning: initialized; its documents are open on it.
	ServerRunning
	// ServerFailed: it crashed too often (5 times in 3 minutes) and is not restarted until the user says so.
	ServerFailed
	// ServerMissing: the program was not found; one notification said so and nothing is restarted.
	ServerMissing
	// ServerStopped: the activation ended.
	ServerStopped
)

// String is the word for the state.
func (s ServerState) String() string {
	switch s {
	case ServerIdle:
		return "idle"
	case ServerStarting:
		return "starting"
	case ServerRunning:
		return "running"
	case ServerFailed:
		return "failed"
	case ServerMissing:
		return "missing"
	case ServerStopped:
		return "stopped"
	}
	return "unknown"
}

// ServerStatus describes a language server in a tab, for the tests and the pages that show it.
type ServerStatus struct {
	State ServerState
	// Starts counts the times the program was started, Crashes the crashes of the running server.
	Starts, Crashes int
	// PID is the process id of the running program, or 0.
	PID int
}

// Timings of the servers; HostOptions can change them (tests).
const (
	// DefaultServerGrace is how long a server has to end after shutdown and exit (SPEC §9.12: 3 s).
	DefaultServerGrace = 3 * time.Second
	// initializeTimeout is how long the handshake may take.
	initializeTimeout = 60 * time.Second
	// maxCrashes and crashWindow are the restart policy of VS Code's default error handler: the fifth crash within three
	// minutes stops the restarts.
	maxCrashes  = 5
	crashWindow = 3 * time.Minute
)

// serverCtl runs one language server of one extension in one scope. Its fields are used on the UI thread unless the
// comment says that mu guards them.
type serverCtl struct {
	s    *Scope
	inst *instance
	spec extension.LanguageServerSpec
	qid  string
	ws   LanguageWorkspace
	log  *slog.Logger

	channel  extension.OutputChannel
	status   extension.StatusHandle
	provider *langfeatures.ServerProvider

	// mu guards what the goroutines of the server read: the state, the client, the process, the generation and the
	// progress.
	mu       sync.Mutex
	state    ServerState
	client   *lsp.Client
	proc     *lsp.Process
	gen      int
	starts   int
	crashes  []time.Time
	progress []progressItem
	stopped  bool

	// traceLevel is <id>.trace.server as it was last read on the UI thread; the connection reads it from its goroutines.
	traceLevel atomic.Value

	wanted map[document.URI]*document.Document
	synced map[document.URI]*lsp.SyncedDocument
	// ctx ends with the controller (stop); diag is the state of the diagnostics (diagnostics.go) and pullSubs the
	// subscriptions to the changes of the documents that are pulled.
	ctx          context.Context
	cancel       context.CancelFunc
	diag         diagState
	pullSubs     map[*document.Document]func()
	lastConfig   string
	missingShown bool
	globs        sync.Map // pattern -> glob.Matcher, for the watched files
}

// progressItem is a work done progress that is running.
type progressItem struct {
	token, title, message string
	percent               int
}

// startServers makes the controllers of the servers the manifest of an activated extension declares. It runs on the UI
// thread right after Activate returned; the servers start when their first document opens (or at once with OnActivation).
func (s *Scope) startServers(inst *instance) {
	specs := inst.slot.info.Manifest.Contributes.LanguageServers
	if len(specs) == 0 || s.opts.LSP == nil {
		return
	}
	for _, spec := range specs {
		c := &serverCtl{
			s: s, inst: inst, spec: spec, qid: extension.QualifiedServerID(inst.id, spec), ws: s.opts.LSP,
			log:    inst.log.With("server", spec.ID),
			wanted: map[document.URI]*document.Document{}, synced: map[document.URI]*lsp.SyncedDocument{},
			diag: newDiagState(), pullSubs: map[*document.Document]func(){},
		}
		c.ctx, c.cancel = context.WithCancel(context.Background())
		inst.mu.Lock()
		inst.servers = append(inst.servers, c)
		inst.mu.Unlock()
		c.begin()
	}
}

// begin connects the controller to the tab: the Output channel and the status item, the providers of the registry, the
// documents, the settings and the files.
func (c *serverCtl) begin() {
	a := &api{inst: c.inst}
	if ch, err := a.Output().Channel(c.spec.Name); err != nil {
		c.log.Warn("the Output channel of a language server cannot be made", "err", err)
	} else {
		c.channel = ch
	}
	if h, err := a.StatusBar().Add(extension.StatusItem{
		ID: "server." + c.spec.ID, Name: c.spec.Name, Alignment: extension.StatusLeft, Priority: 0,
		OnClick: func() { c.showOutput(false) },
	}); err != nil {
		c.log.Warn("the status bar item of a language server cannot be made", "err", err)
	} else {
		c.status = h
		h.SetVisible(false)
	}
	c.provider = &langfeatures.ServerProvider{Name: c.qid, Client: c.currentServer, Texts: c.text}
	sel := langfeatures.Selector{Languages: c.spec.Languages, Folder: c.s.opts.Info.Folder}
	c.inst.track(c.provider.Register(c.s.features, c.inst.id, sel))
	c.inst.track(c.stop)

	c.inst.track(c.ws.WatchOpen(func(doc *document.Document) { c.s.post(func() { c.docOpened(doc) }) }))
	c.inst.track(c.ws.WatchClose(func(doc *document.Document) { c.s.post(func() { c.docClosed(doc) }) }))
	c.inst.track(c.ws.WatchFiles(c.filesChanged))
	if st := c.s.opts.Settings; st != nil {
		c.lastConfig = c.configJSON()
		c.readTraceLevel()
		c.inst.track(st.OnChange(func(e settings.ChangeEvent) {
			if !e.IsEmpty() {
				c.s.post(func() { c.settingsChanged(e) })
			}
		}))
	}
	for _, doc := range c.ws.Documents() {
		c.docOpened(doc)
	}
	if c.spec.OnActivation {
		c.start()
	}
}

// currentServer is the client now as a langfeatures.Server, or a nil interface.
func (c *serverCtl) currentServer() langfeatures.Server {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client == nil {
		return nil
	}
	return c.client
}

// text gives the text of a document for the conversion of positions: the snapshot of the open document, taken on the UI
// thread, else the file as it is on disk. It belongs to the goroutines of the requests.
func (c *serverCtl) text(ctx context.Context, uri document.URI) (lsp.Lines, error) {
	got := make(chan lsp.Lines, 1)
	c.s.post(func() {
		if l, ok := c.ws.OpenText(uri); ok {
			got <- l
			return
		}
		got <- nil
	})
	select {
	case l := <-got:
		if l != nil {
			return l, nil
		}
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return c.ws.ReadText(ctx, uri)
}

// Status is the state of the server.
func (c *serverCtl) Status() ServerStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := ServerStatus{State: c.state, Starts: c.starts, Crashes: len(c.crashes)}
	if c.proc != nil {
		st.PID = c.proc.PID()
	}
	return st
}

func (c *serverCtl) setState(st ServerState) {
	c.mu.Lock()
	c.state = st
	c.mu.Unlock()
}

// ---- documents ----

// serves tells whether the server is for the document: its language is one of the server's, and it is untitled or a file
// inside the folder of the tab.
func (c *serverCtl) serves(doc *document.Document) bool {
	if !slices.Contains(c.spec.Languages, doc.LanguageID()) {
		return false
	}
	u := doc.URI()
	switch u.Scheme {
	case document.SchemeUntitled:
		return true
	case document.SchemeFile:
		folder := c.s.opts.Info.Folder
		return folder != "" && within(folder, u.Path)
	}
	return false
}

// within tells whether path is the folder or below it.
func within(folder, path string) bool {
	rel, err := filepath.Rel(folder, path)
	return err == nil && filepath.IsLocal(rel)
}

// docOpened follows a document that opened: it is kept for the server and opened on it when it is up. The first document
// of its languages starts an idle server.
func (c *serverCtl) docOpened(doc *document.Document) {
	if c.isStopped() || !c.serves(doc) {
		return
	}
	if _, ok := c.wanted[doc.URI()]; ok {
		return
	}
	c.wanted[doc.URI()] = doc
	c.mu.Lock()
	st := c.state
	c.mu.Unlock()
	switch st {
	case ServerIdle:
		c.start()
	case ServerRunning:
		c.attach(doc)
	}
}

// docClosed follows a document that closed.
func (c *serverCtl) docClosed(doc *document.Document) {
	for uri, d := range c.wanted {
		if d == doc {
			delete(c.wanted, uri)
		}
	}
	for uri, sd := range c.synced {
		if sd.Document() == doc {
			sd.Close()
			delete(c.synced, uri)
		}
	}
	if unsub := c.pullSubs[doc]; unsub != nil {
		unsub()
		delete(c.pullSubs, doc)
	}
	c.forgetDocument(doc.URI())
}

// attach opens a document on the running server.
func (c *serverCtl) attach(doc *document.Document) {
	client := c.currentClient()
	if client == nil {
		return
	}
	for _, sd := range c.synced {
		if sd.Document() == doc {
			return
		}
	}
	sd, err := client.OpenDocument(doc)
	if err != nil {
		c.log.Warn("a document cannot be opened on the language server", "uri", doc.URI().String(), "err", err)
		return
	}
	c.synced[doc.URI()] = sd
	if _, ok := c.pullOptions(client); ok {
		c.pullSubs[doc] = doc.Subscribe(func(document.ChangeEvent) { c.schedulePull(doc) })
		c.pull(doc)
	}
	// a rename (Save As) changes the key
	doc.OnURIChange(func(oldURI, newURI document.URI) {
		c.s.post(func() {
			if sd, ok := c.synced[oldURI]; ok {
				delete(c.synced, oldURI)
				c.synced[newURI] = sd
			}
			if d, ok := c.wanted[oldURI]; ok {
				delete(c.wanted, oldURI)
				c.wanted[newURI] = d
			}
		})
	})
}

// closeSynced closes the documents on the server and stops following their changes (UI thread).
func (c *serverCtl) closeSynced() {
	for uri, sd := range c.synced {
		sd.Close()
		delete(c.synced, uri)
	}
	for doc, unsub := range c.pullSubs {
		unsub()
		delete(c.pullSubs, doc)
	}
}

func (c *serverCtl) currentClient() *lsp.Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.client
}

func (c *serverCtl) isStopped() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stopped
}

// ---- starting, crashing, restarting, stopping ----

// programPath is the program to run: the path the user's setting gives, else the manifest's.
func (c *serverCtl) programPath() string {
	if c.spec.PathSetting != "" && c.s.opts.Settings != nil {
		if _, ok := c.s.opts.Settings.Registry().Lookup(c.spec.PathSetting); !ok {
			return c.spec.Program
		}
		if p := strings.TrimSpace(settings.Get[string](c.s.opts.Settings, c.spec.PathSetting, "")); p != "" {
			return p
		}
	}
	return c.spec.Program
}

// start starts the program and does the handshake off the UI thread. A program that is not found is told to the user once
// and is not started again until the setting of its path changes or the user restarts the server.
func (c *serverCtl) start() {
	if c.isStopped() {
		return
	}
	program := c.programPath()
	path, err := lsp.FindProgram(program)
	if err != nil {
		c.setState(ServerMissing)
		c.hideStatus()
		c.logLine("Error", fmt.Sprintf("%s was not found: %s", program, describe(err)))
		if !c.missingShown {
			c.missingShown = true
			c.notify(notify.Error, fmt.Sprintf("%s was not found. Install it, or set the path in the settings.", c.spec.Program), c.openSettingsAction())
		}
		return
	}
	c.missingShown = false
	c.mu.Lock()
	c.gen++
	gen := c.gen
	c.state = ServerStarting
	c.starts++
	c.mu.Unlock()
	c.showStatus("starting")
	c.logLine("Info", "Starting "+c.spec.Name+": "+path+" "+strings.Join(c.spec.Args, " "))

	folder := c.s.opts.Info.Folder
	opts := c.clientOptions(gen)
	spec := lsp.ProcessSpec{Program: path, Args: c.spec.Args, Dir: folder, Env: c.spec.Env, Stderr: c.stderrLine}
	c.s.h.servers.Add(1)
	go func() {
		defer c.s.h.servers.Done()
		proc, client, err := c.launch(spec, opts)
		c.s.post(func() { c.started(gen, proc, client, err) })
	}()
}

// launch runs the program and the handshake. It owns the goroutine; on failure the program is stopped.
func (c *serverCtl) launch(spec lsp.ProcessSpec, opts lsp.ClientOptions) (*lsp.Process, *lsp.Client, error) {
	proc, err := lsp.StartProcess(context.Background(), spec)
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), initializeTimeout)
	defer cancel()
	client, err := lsp.NewClient(ctx, proc, opts)
	if err != nil {
		proc.Stop(0)
		return nil, nil, err
	}
	return proc, client, nil
}

// clientOptions are the options of the client of generation gen.
func (c *serverCtl) clientOptions(gen int) lsp.ClientOptions {
	var init json.RawMessage
	if c.spec.InitializationOptions != nil {
		if b, err := json.Marshal(c.spec.InitializationOptions); err == nil {
			init = b
		} else {
			c.log.Warn("the initialization options of a language server cannot be encoded", "err", err)
		}
	}
	folder := c.s.opts.Info.Folder
	return lsp.ClientOptions{
		Root: folder, InitializationOptions: init, Logger: c.log, Trace: c.trace,
		Handlers: lsp.Handlers{
			Configuration:      c.configuration,
			ApplyEdit:          c.applyEdit,
			ShowMessageRequest: c.showMessageRequest,
			ShowMessage:        c.showMessage,
			LogMessage:         c.logMessage,
			Progress:           c.onProgress,
			PublishDiagnostics: c.publishDiagnostics,
			Refresh:            c.refresh,
			RegistrationsChanged: func() {
				c.s.post(c.pullAll)
			},
		},
	}
}

// started is the end of a start, on the UI thread: a client of the current generation takes over; one that is too late
// (the server was stopped or restarted meanwhile) is shut down.
func (c *serverCtl) started(gen int, proc *lsp.Process, client *lsp.Client, err error) {
	c.mu.Lock()
	stale := c.stopped || gen != c.gen
	c.mu.Unlock()
	if stale {
		if client != nil {
			c.shutdown(client, proc)
		}
		return
	}
	if err != nil {
		c.logLine("Error", "The server did not start: "+describe(err))
		c.crashed(gen)
		return
	}
	c.mu.Lock()
	c.client, c.proc, c.state = client, proc, ServerRunning
	c.mu.Unlock()
	c.hideStatus()
	for _, doc := range c.wanted {
		c.attach(doc)
	}
	// not counted by the host: it lives as long as the server does
	go func() {
		<-client.Done()
		c.s.post(func() { c.exited(gen, client, proc) })
	}()
}

// exited is the end of the connection of a client: when the server did not ask for it, it crashed.
func (c *serverCtl) exited(gen int, client *lsp.Client, proc *lsp.Process) {
	c.mu.Lock()
	current := !c.stopped && gen == c.gen && c.client == client
	c.mu.Unlock()
	if !current {
		return
	}
	c.logLine("Error", fmt.Sprintf("The connection to %s ended: %v", c.spec.Name, client.Err()))
	// the program may be alive with a broken stream: stop what is left of it
	c.s.h.servers.Add(1)
	go func() {
		defer c.s.h.servers.Done()
		proc.Stop(0)
	}()
	c.closeSynced()
	c.mu.Lock()
	c.client, c.proc = nil, nil
	c.mu.Unlock()
	c.clearMarkers()
	c.crashed(gen)
}

// crashed applies the restart policy of VS Code's default error handler: a server that crashed is started again, unless it
// crashed 5 times within 3 minutes, which stops it and tells the user (Restart, Show Output).
func (c *serverCtl) crashed(gen int) {
	now := c.now()
	c.mu.Lock()
	if c.stopped || gen != c.gen {
		c.mu.Unlock()
		return
	}
	c.crashes = append(c.crashes, now)
	give := false
	if len(c.crashes) >= maxCrashes {
		if now.Sub(c.crashes[len(c.crashes)-maxCrashes]) <= crashWindow {
			give = true
		}
	}
	if len(c.crashes) > maxCrashes {
		c.crashes = c.crashes[len(c.crashes)-maxCrashes:]
	}
	if give {
		c.state = ServerFailed
	} else {
		c.state = ServerIdle
	}
	c.mu.Unlock()
	c.hideStatus()
	if !give {
		c.logLine("Info", "Restarting "+c.spec.Name)
		c.start()
		return
	}
	msg := fmt.Sprintf("The %s server crashed %d times in the last 3 minutes. The server will not be restarted.", c.spec.Name, maxCrashes)
	c.logLine("Error", msg)
	c.notify(notify.Error, msg,
		notify.Action{Label: "Restart", Run: c.restart},
		notify.Action{Label: "Show Output", Run: func() { c.showOutput(false) }})
}

// now is the clock of the scope.
func (c *serverCtl) now() time.Time {
	if c.s.opts.Now != nil {
		return c.s.opts.Now()
	}
	return time.Now()
}

// restart stops the running server (if any) and starts it again, forgetting the crashes. It runs on the UI thread.
func (c *serverCtl) restart() {
	if c.isStopped() {
		return
	}
	c.detach()
	c.mu.Lock()
	c.crashes = nil
	c.state = ServerIdle
	c.mu.Unlock()
	c.missingShown = false
	c.start()
}

// detach lets go of the running server: its documents are closed on it and it is shut down in the background, and the
// generation moves on so that a start in progress is thrown away when it ends.
func (c *serverCtl) detach() {
	c.closeSynced()
	c.mu.Lock()
	client, proc := c.client, c.proc
	c.client, c.proc = nil, nil
	c.gen++
	c.progress = nil
	c.mu.Unlock()
	c.clearMarkers()
	if client != nil {
		c.shutdown(client, proc)
	}
}

// stop ends the server with the activation: the shutdown is done in the background (the host waits for it when it quits).
func (c *serverCtl) stop() {
	c.mu.Lock()
	if c.stopped {
		c.mu.Unlock()
		return
	}
	c.stopped = true
	c.state = ServerStopped
	c.mu.Unlock()
	c.cancel()
	c.detach()
	if c.status != nil {
		c.status.Dispose()
	}
}

// shutdown does the shutdown handshake and stops the program, in the background, within the grace period.
func (c *serverCtl) shutdown(client *lsp.Client, proc *lsp.Process) {
	grace := c.s.h.grace
	c.s.h.servers.Add(1)
	go func() {
		defer c.s.h.servers.Done()
		ctx, cancel := context.WithTimeout(context.Background(), grace)
		defer cancel()
		if err := client.Shutdown(ctx); err != nil {
			c.log.Debug("the shutdown of a language server failed", "err", err)
		}
		if proc != nil {
			proc.Stop(grace)
		}
	}()
}

// ---- the user's side: notifications, Output, status ----

// logLine writes a line to the Output channel in the form of VS Code's language clients: [Level - time] text.
func (c *serverCtl) logLine(level, text string) {
	if c.channel == nil {
		return
	}
	c.channel.AppendLine(fmt.Sprintf("[%-5s - %s] %s", level, c.now().Format("15:04:05"), text))
}

// stderrLine writes a line the server wrote to its standard error.
func (c *serverCtl) stderrLine(line string) { c.logLine("Stderr", line) }

// showOutput shows the Output channel of the server.
func (c *serverCtl) showOutput(preserveFocus bool) {
	if c.channel != nil {
		c.channel.Show(preserveFocus)
	}
}

// notify shows a notification with the name of the extension as its source; the actions run guarded.
func (c *serverCtl) notify(sev notify.Severity, message string, actions ...notify.Action) {
	if c.isStopped() || c.s.opts.Notify == nil {
		return
	}
	list := make([]notify.Action, len(actions))
	for i, a := range actions {
		a := a
		list[i] = a
		if a.Run != nil {
			list[i].Run = func() { c.s.runNotificationAction(c.inst, a.Label, a.Run) }
		}
	}
	c.s.opts.Notify.Notify(notify.Notification{Severity: sev, Source: c.inst.name, Message: message, Actions: list})
}

// openSettingsAction is the button that opens the settings at the setting of the path of the program.
func (c *serverCtl) openSettingsAction() notify.Action {
	return notify.Action{Label: "Open Settings", Command: "workbench.action.openSettings", Args: c.spec.PathSetting}
}

// showStatus shows the status bar item with a text after the name of the server.
func (c *serverCtl) showStatus(text string) {
	if c.status == nil {
		return
	}
	label := c.spec.Name
	if text != "" {
		label += ": " + text
	}
	c.status.Update(extension.StatusItem{
		ID: "server." + c.spec.ID, Name: c.spec.Name, Text: "$(sync) " + label, Tooltip: label, Alignment: extension.StatusLeft,
		OnClick: func() { c.showOutput(false) },
	})
	c.status.SetVisible(true)
}

func (c *serverCtl) hideStatus() {
	if c.status != nil {
		c.status.SetVisible(false)
	}
}

// ---- handlers of the client (they run on the goroutines of the connection) ----

// logMessage writes window/logMessage to the Output channel.
func (c *serverCtl) logMessage(p lsp.LogMessageParams) {
	c.logLine(messageLevel(p.Type), p.Message)
}

// messageLevel is the word for a MessageType.
func messageLevel(t int) string {
	switch t {
	case lsp.MessageTypeError:
		return "Error"
	case lsp.MessageTypeWarning:
		return "Warn"
	case lsp.MessageTypeInfo:
		return "Info"
	}
	return "Log"
}

// showMessage shows window/showMessage as a notification (a log type goes to the Output channel only).
func (c *serverCtl) showMessage(p lsp.ShowMessageParams) {
	c.logLine(messageLevel(p.Type), p.Message)
	sev := notify.Info
	switch p.Type {
	case lsp.MessageTypeError:
		sev = notify.Error
	case lsp.MessageTypeWarning:
		sev = notify.Warning
	case lsp.MessageTypeLog:
		return
	}
	c.notify(sev, p.Message)
}

// showMessageRequest shows window/showMessageRequest as a notification with the actions as buttons and answers with the
// one the user pressed, or nil when the notification was dismissed or the request ended first.
func (c *serverCtl) showMessageRequest(ctx context.Context, p lsp.ShowMessageRequestParams) (*lsp.MessageActionItem, error) {
	c.logLine(messageLevel(p.Type), p.Message)
	if c.isStopped() || c.s.opts.Notify == nil {
		return nil, nil
	}
	choice := make(chan *lsp.MessageActionItem, 1)
	answer := func(item *lsp.MessageActionItem) {
		select {
		case choice <- item:
		default:
		}
	}
	sev := notify.Info
	switch p.Type {
	case lsp.MessageTypeError:
		sev = notify.Error
	case lsp.MessageTypeWarning:
		sev = notify.Warning
	}
	list := make([]notify.Action, len(p.Actions))
	for i, a := range p.Actions {
		item := a
		list[i] = notify.Action{Label: a.Title, Run: func() { c.s.runNotificationAction(c.inst, item.Title, func() { answer(&item) }) }}
	}
	var id atomic.Uint64
	stop := c.s.opts.Notify.OnChange(func(e notify.Event) {
		if want := id.Load(); e.Kind == notify.Dismissed && want != 0 && e.Item.ID == want {
			answer(nil)
		}
	})
	defer stop()
	nid := c.s.opts.Notify.Notify(notify.Notification{Severity: sev, Source: c.inst.name, Message: p.Message, Actions: list, Sticky: len(list) > 0})
	id.Store(nid)
	select {
	case item := <-choice:
		return item, nil
	case <-ctx.Done():
		c.s.opts.Notify.Dismiss(nid)
		return nil, nil
	}
}

// onProgress follows $/progress: a work done progress shows in the status bar item while it runs.
func (c *serverCtl) onProgress(p lsp.ProgressParams) {
	w, ok := p.WorkDone()
	if !ok {
		return
	}
	key := p.Token.Key()
	c.mu.Lock()
	idx := slices.IndexFunc(c.progress, func(x progressItem) bool { return x.token == key })
	switch w.Kind {
	case lsp.ProgressBegin:
		it := progressItem{token: key, title: w.Title, message: w.Message, percent: -1}
		if w.Percentage != nil {
			it.percent = *w.Percentage
		}
		if idx < 0 {
			c.progress = append(c.progress, it)
		} else {
			c.progress[idx] = it
		}
	case lsp.ProgressReport:
		if idx >= 0 {
			if w.Message != "" {
				c.progress[idx].message = w.Message
			}
			if w.Percentage != nil {
				c.progress[idx].percent = *w.Percentage
			}
		}
	case lsp.ProgressEnd:
		if idx >= 0 {
			c.progress = slices.Delete(c.progress, idx, idx+1)
		}
	}
	c.mu.Unlock()
	c.s.post(c.updateProgressStatus)
}

// updateProgressStatus shows the last running progress, or hides the item.
func (c *serverCtl) updateProgressStatus() {
	c.mu.Lock()
	stopped := c.stopped
	var cur *progressItem
	if n := len(c.progress); n > 0 {
		cur = &c.progress[n-1]
	}
	var text string
	if cur != nil {
		parts := []string{cur.title}
		if cur.message != "" {
			parts = append(parts, cur.message)
		}
		text = strings.Join(parts, " ")
		if cur.percent >= 0 {
			text += fmt.Sprintf(" %d%%", cur.percent)
		}
	}
	c.mu.Unlock()
	if stopped {
		return
	}
	if cur == nil {
		c.hideStatus()
		return
	}
	c.showStatus(text)
}

// applyEdit answers workspace/applyEdit: the edit is made on the UI thread.
func (c *serverCtl) applyEdit(ctx context.Context, p lsp.ApplyWorkspaceEditParams) (lsp.ApplyWorkspaceEditResult, error) {
	type outcome struct {
		res langfeatures.ApplyResult
		err error
	}
	done := make(chan outcome, 1)
	c.s.post(func() {
		we := langfeatures.FromLSPEdit(c.encoding(), p.Edit)
		we.Label = p.Label
		res, err := langfeatures.Apply(ctx, c.ws.Edits(), we)
		done <- outcome{res, err}
	})
	select {
	case o := <-done:
		if o.err != nil {
			c.logLine("Error", "The edit of the server was not applied: "+describe(o.err))
			r := lsp.ApplyWorkspaceEditResult{Applied: false, FailureReason: describe(o.err)}
			if o.res.FailedChange >= 0 {
				n := o.res.FailedChange
				r.FailedChange = &n
			}
			return r, nil
		}
		return lsp.ApplyWorkspaceEditResult{Applied: true}, nil
	case <-ctx.Done():
		return lsp.ApplyWorkspaceEditResult{Applied: false, FailureReason: "cancelled"}, nil
	}
}

// encoding is the position encoding of the running client; UTF-16 before there is one.
func (c *serverCtl) encoding() lsp.Encoding {
	if cl := c.currentClient(); cl != nil {
		return cl.Encoding()
	}
	return lsp.UTF16
}

// refresh answers the refresh requests of the server: the diagnostics of the open documents are pulled again.
func (c *serverCtl) refresh(_ context.Context, method string) error {
	if method == lsp.MethodDiagnosticRefresh {
		c.refreshDiagnostics()
	}
	return nil
}

// ---- configuration ----

// section reads a section of the settings of the tab as JSON; null when there is none.
func (c *serverCtl) section(key string) json.RawMessage {
	st := c.s.opts.Settings
	if st == nil || key == "" {
		return json.RawMessage("null")
	}
	v, ok := st.Section(key)
	if !ok {
		return json.RawMessage("null")
	}
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("null")
	}
	return b
}

// configJSON is the section of the server.
func (c *serverCtl) configJSON() string { return string(c.section(c.spec.ConfigurationSection)) }

// configuration answers workspace/configuration (see LanguageServerSpec.ConfigurationSection).
func (c *serverCtl) configuration(_ context.Context, p lsp.ConfigurationParams) ([]json.RawMessage, error) {
	out := make([]json.RawMessage, len(p.Items))
	id, base := c.spec.ID, c.spec.ConfigurationSection
	for i, item := range p.Items {
		sec := item.Section
		switch {
		case sec == "" || sec == id:
			out[i] = c.section(base)
		case base != "" && strings.HasPrefix(sec, id+"."):
			out[i] = c.section(base + sec[len(id):])
		default:
			out[i] = c.section(sec)
		}
	}
	return out, nil
}

// settingsChanged follows a change of the settings: the path of the program restarts a server that was not found, and a
// change of the section is sent to the running server.
func (c *serverCtl) settingsChanged(e settings.ChangeEvent) {
	if c.isStopped() {
		return
	}
	if e.Affects(extension.TraceSetting(c.inst.id, c.spec), "") {
		c.readTraceLevel()
	}
	c.mu.Lock()
	st := c.state
	c.mu.Unlock()
	if c.spec.PathSetting != "" && e.Affects(c.spec.PathSetting, "") && st == ServerMissing {
		c.start()
		return
	}
	now := c.configJSON()
	if now == c.lastConfig {
		return
	}
	c.lastConfig = now
	if client := c.currentClient(); client != nil {
		if err := client.Notify(lsp.MethodDidChangeConfiguration, lsp.DidChangeConfigurationParams{Settings: json.RawMessage(now)}); err != nil {
			c.log.Debug("the configuration was not sent", "err", err)
		}
	}
}

// ---- trace ----

// readTraceLevel reads <id>.trace.server (off, messages or verbose) into traceLevel. It runs on the UI thread, while the
// setting is registered.
func (c *serverCtl) readTraceLevel() {
	st := c.s.opts.Settings
	if st == nil {
		return
	}
	c.traceLevel.Store(settings.Get[string](st, extension.TraceSetting(c.inst.id, c.spec), ""))
}

// trace writes a message of the connection to the Output channel as the setting says, in the form of VS Code's language
// clients. It runs on the goroutines of the connection and does not block.
func (c *serverCtl) trace(m lsp.TraceMessage) {
	level, _ := c.traceLevel.Load().(string)
	if level != "messages" && level != "verbose" {
		return
	}
	name := m.Method
	if m.ID != "" {
		name += " - (" + m.ID + ")"
	}
	var head string
	switch {
	case m.Direction == lsp.ToServer && m.Kind == lsp.KindRequest:
		head = fmt.Sprintf("Sending request '%s'.", name)
	case m.Direction == lsp.ToServer && m.Kind == lsp.KindNotification:
		head = fmt.Sprintf("Sending notification '%s'.", name)
	case m.Direction == lsp.ToServer:
		head = fmt.Sprintf("Sending response '%s'.", name)
	case m.Kind == lsp.KindRequest:
		head = fmt.Sprintf("Received request '%s'.", name)
	case m.Kind == lsp.KindNotification:
		head = fmt.Sprintf("Received notification '%s'.", name)
	default:
		head = fmt.Sprintf("Received response '%s' in %dms.", name, m.Elapsed.Milliseconds())
	}
	c.logLine("Trace", head)
	if level == "verbose" && c.channel != nil {
		c.channel.AppendLine(string(m.Body))
	}
}

// ---- watched files ----

// filesChanged sends the events of the file watcher that the server watches (workspace/didChangeWatchedFiles). It runs on
// the goroutine of the watcher.
func (c *serverCtl) filesChanged(events []workspace.Event) {
	client := c.currentClient()
	folder := c.s.opts.Info.Folder
	if client == nil || folder == "" {
		return
	}
	patterns := c.watchPatterns(client)
	if len(patterns) == 0 {
		return
	}
	var changes []lsp.FileEvent
	add := func(path string, typ int) {
		if !within(folder, path) {
			return
		}
		for _, p := range patterns {
			if p.kind&(1<<(typ-1)) == 0 {
				continue
			}
			base := p.base
			if base == "" {
				base = folder
			}
			rel, err := filepath.Rel(base, path)
			if err != nil || !filepath.IsLocal(rel) {
				continue
			}
			if c.matcher(p.pattern).Match(filepath.ToSlash(rel)) {
				changes = append(changes, lsp.FileEvent{URI: lsp.DocumentURI(document.FileURI(path)), Type: typ})
				return
			}
		}
	}
	for _, e := range events {
		switch e.Kind {
		case workspace.EventCreated:
			add(e.Path, lsp.FileCreated)
		case workspace.EventChanged:
			add(e.Path, lsp.FileChanged)
		case workspace.EventDeleted:
			add(e.Path, lsp.FileDeleted)
		case workspace.EventRenamed:
			add(e.OldPath, lsp.FileDeleted)
			add(e.Path, lsp.FileCreated)
		}
	}
	if len(changes) == 0 {
		return
	}
	if err := client.Notify(lsp.MethodDidChangeWatchedFiles, lsp.DidChangeWatchedFilesParams{Changes: changes}); err != nil {
		c.log.Debug("the file events were not sent", "err", err)
	}
}

// watchPattern is a glob the server watches: relative to base (the folder when empty), for the kinds in the mask.
type watchPattern struct {
	pattern, base string
	kind          int
}

// watchPatterns are the globs of the manifest and those the server registered.
func (c *serverCtl) watchPatterns(client *lsp.Client) []watchPattern {
	var out []watchPattern
	for _, g := range c.spec.WatchedFiles {
		out = append(out, watchPattern{pattern: g, kind: 7})
	}
	for _, reg := range client.Registered(lsp.MethodDidChangeWatchedFiles) {
		var opts lsp.DidChangeWatchedFilesRegistrationOptions
		if json.Unmarshal(reg.RegisterOptions, &opts) != nil {
			continue
		}
		for _, w := range opts.Watchers {
			kind := 7
			if w.Kind != nil {
				kind = *w.Kind
			}
			var plain string
			if json.Unmarshal(w.GlobPattern, &plain) == nil {
				out = append(out, watchPattern{pattern: plain, kind: kind})
				continue
			}
			var rel struct {
				BaseURI json.RawMessage `json:"baseUri"`
				Pattern string          `json:"pattern"`
			}
			if json.Unmarshal(w.GlobPattern, &rel) != nil || rel.Pattern == "" {
				continue
			}
			out = append(out, watchPattern{pattern: rel.Pattern, kind: kind, base: baseURIPath(rel.BaseURI)})
		}
	}
	return out
}

// baseURIPath is the folder a relative pattern is for: its baseUri is a URI or a workspace folder; "" when it is neither or is
// not a file.
func baseURIPath(raw json.RawMessage) string {
	var uri string
	if json.Unmarshal(raw, &uri) != nil {
		var folder struct {
			URI string `json:"uri"`
		}
		if json.Unmarshal(raw, &folder) != nil {
			return ""
		}
		uri = folder.URI
	}
	u, err := lsp.ParseDocumentURI(uri)
	if err != nil || u.Scheme != document.SchemeFile {
		return ""
	}
	return u.Path
}

// matcher compiles a glob once.
func (c *serverCtl) matcher(pattern string) *glob.Matcher {
	if m, ok := c.globs.Load(pattern); ok {
		if matcher, ok := m.(*glob.Matcher); ok {
			return matcher
		}
	}
	m := glob.Parse(pattern, glob.Options{})
	c.globs.Store(pattern, m)
	return m
}

// ---- the scope's view of the servers ----

// ServerStatus describes the server of an extension in the scope; false when the extension is not active here or has no
// such server.
func (s *Scope) ServerStatus(extensionID, serverID string) (ServerStatus, bool) {
	s.mu.Lock()
	var inst *instance
	for _, sl := range s.slots {
		if sl.info.Manifest.ID == extensionID {
			inst = sl.inst
		}
	}
	s.mu.Unlock()
	if inst == nil {
		return ServerStatus{}, false
	}
	inst.mu.Lock()
	servers := slices.Clone(inst.servers)
	inst.mu.Unlock()
	for _, c := range servers {
		if c.spec.ID == serverID {
			return c.Status(), true
		}
	}
	return ServerStatus{}, false
}

// Features is the registry of the language feature providers of the scope: the language servers of its extensions and the
// providers they wrote in Go are in it (SPEC §9.12).
func (s *Scope) Features() *langfeatures.Registry { return s.features }

// restartServers restarts the servers of an extension in the scope (the command <id>.restartServer); an idle server is
// started.
func (s *Scope) restartServers(sl *slot) {
	if s.State(sl.info.Manifest.ID) == StateInactive {
		s.activate(sl)
	}
	s.mu.Lock()
	inst := sl.inst
	s.mu.Unlock()
	if inst == nil {
		return
	}
	inst.mu.Lock()
	servers := slices.Clone(inst.servers)
	inst.mu.Unlock()
	for _, c := range servers {
		c.restart()
	}
}

// installServers adds what the servers of a manifest need before the extension is activated: the settings of the trace and
// of the path of the program, and the command that restarts the servers. It returns the functions that take them away.
func (s *Scope) installServers(sl *slot) []func() {
	m := sl.info.Manifest
	specs := m.Contributes.LanguageServers
	if len(specs) == 0 {
		return nil
	}
	var removers []func()
	for _, spec := range specs {
		key := extension.TraceSetting(m.ID, spec)
		err := s.h.holdSetting(m.ID, settings.Setting{
			Key: key, Types: []settings.Type{settings.String}, Default: "off", Enum: []any{"off", "messages", "verbose"},
			Description: "Traces the communication between Emetos and the " + spec.Name + " language server in its Output channel.",
			Scope:       settings.ScopeWindow, Category: m.Name,
		})
		if err != nil {
			s.log.Warn("the trace setting of a language server is left out", "extension", m.ID, "key", key, "err", err)
		} else {
			removers = append(removers, func() { s.h.releaseSetting(key) })
		}
		if spec.PathSetting == "" {
			continue
		}
		pathKey := spec.PathSetting
		err = s.h.holdSetting(m.ID, settings.Setting{
			Key: pathKey, Types: []settings.Type{settings.String}, Default: "",
			Description: "The path of the " + spec.Program + " program, when it is not in PATH.",
			Scope:       settings.ScopeWindow, Category: m.Name,
		})
		if err != nil {
			s.log.Warn("the path setting of a language server is left out", "extension", m.ID, "key", pathKey, "err", err)
		} else {
			removers = append(removers, func() { s.h.releaseSetting(pathKey) })
		}
	}
	if s.opts.Commands != nil {
		id := extension.RestartServerCommand(m.ID)
		err := s.opts.Commands.Register(command.Meta{ID: id, Title: "Restart Language Server", Category: m.Name})
		if err == nil {
			var remove func()
			remove, err = s.opts.Commands.SetHandler(id, func(command.Context, any) error {
				s.restartServers(sl)
				return nil
			})
			if err == nil {
				removers = append(removers, func() { remove(); s.opts.Commands.Unregister(id) })
			} else {
				s.opts.Commands.Unregister(id)
			}
		}
		if err != nil {
			s.log.Warn("the restart command of a language server is left out", "extension", m.ID, "err", err)
		}
	}
	return removers
}
