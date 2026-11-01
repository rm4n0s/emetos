package langserver

import (
	"context"
	"strings"
	"sync"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// Controller is the part of a pack that follows its language server in one tab: it gives the host the launcher, shows the status
// item and its menu, tells the user when the server is missing, and restarts the server when a setting its start read changes.
// Everything but the launcher runs on the UI thread.
type Controller struct {
	api       extension.API
	cfg       Config
	osRelease func() string

	// mu guards last, which the launcher writes from a goroutine of the host.
	mu   sync.Mutex
	last launchResult

	server   extension.ServerStatus
	label    string
	detail   string
	health   Health
	activeIn bool
	status   extension.StatusHandle
	key      string
	visible  bool
	// told are the problems the user was told about in this tab, by message.
	told map[string]bool
}

// launchResult is what the last start learned.
type launchResult struct {
	// have says that a launch ran at all.
	have    bool
	err     error
	version string
	found   Found
	program Program
	root    string
	// read are the settings the launch read, with their Fingerprint then.
	read map[string]string
}

// Option changes how Attach makes a Controller; the tests use them.
type Option func(*Controller)

// WithOSRelease makes the controller read the family of the system from the text instead of from /etc/os-release.
func WithOSRelease(text string) Option {
	return func(c *Controller) { c.osRelease = func() string { return text } }
}

// StatusID is the id of the status bar item of a server: "status." and the id of the server.
func StatusID(serverID string) string { return "status." + serverID }

// Attach connects the server the pack declared (Config.Spec) to the tab: the launcher, the status item, the notification for a
// missing server, and the restart on a change of the settings. The manifest of the pack must declare the topics of
// Config.Subscribes and the server and settings of Config.Spec and Config.SettingSpecs; langpack does it for a Spec with a Server. It
// fails with ServerConfigInvalid for a Config without an id, a name, languages or a program.
func Attach(ctx context.Context, api extension.API, cfg Config, opts ...Option) (*Controller, error) {
	if !cfg.valid() {
		return nil, errors.New("ServerConfigInvalid", "a language server needs an id, a name, languages and a program", "server", cfg.ID)
	}
	c := &Controller{api: api, cfg: cfg, osRelease: ReadOSRelease, told: map[string]bool{}, label: cfg.Name}
	for _, opt := range opts {
		opt(c)
	}
	var err error
	if c.status, err = api.StatusBar().Add(extension.StatusItem{
		ID: StatusID(cfg.ID), Name: cfg.Name, Alignment: extension.StatusRight, Priority: statusPriority, OnClick: c.showMenu,
	}); err != nil {
		return nil, err
	}
	c.status.SetVisible(false)
	servers := api.LanguageServers()
	if err = servers.SetLauncher(cfg.ID, c.launch); err != nil {
		return nil, err
	}
	if c.server, err = servers.Status(cfg.ID); err != nil {
		return nil, err
	}
	if _, err = servers.OnChange(cfg.ID, c.serverChanged); err != nil {
		return nil, err
	}
	if _, err = api.Events().Subscribe(extension.TopicEditorActiveChanged, c.editorEvent); err != nil {
		return nil, err
	}
	api.Settings().OnChange(c.settingsChanged)
	c.updateStatus()
	return c, nil
}

// statusPriority puts the item on the right side of the bar, just after the language mode, where the Go item is.
const statusPriority = 100.05

// launch is the ServerLauncher: it runs on a goroutine of the host.
func (c *Controller) launch(ctx context.Context, req extension.LaunchRequest) (extension.ServerLaunch, error) {
	s := NewSettings(req.Setting)
	l, err := Resolve(ctx, c.cfg, DefaultEnv(req.Folder, s))
	res := launchResult{have: true, err: err, read: s.Read()}
	var out extension.ServerLaunch
	if err == nil {
		res.version, res.found, res.program, res.root = l.Version, l.Found, l.Program, l.Root
		out = l.serverLaunch()
		if l.VersionErr != nil {
			c.api.Log().Debug("the version of a language server could not be read", "server", c.cfg.ID, "err", l.VersionErr)
		}
	} else {
		res.program, _ = c.cfg.program(s)
	}
	c.mu.Lock()
	c.last = res
	c.mu.Unlock()
	c.api.Post(c.updateStatus)
	return out, err
}

// lastResult is what the last start learned.
func (c *Controller) lastResult() launchResult {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.last
}

// Version is the version of the server that runs or started last, "" when it is not known.
func (c *Controller) Version() string { return c.lastResult().version }

// Found is where the server was found at the last start; the zero value before the first start or when it was not found.
func (c *Controller) Found() Found { return c.lastResult().found }

// State is the last state of the server the host told.
func (c *Controller) State() extension.ServerStatus { return c.server }

// SetLabel sets the text of the status item (the Python pack shows the interpreter); the name of the server when empty.
func (c *Controller) SetLabel(text string) {
	if text == "" {
		text = c.cfg.Name
	}
	c.label = text
	c.updateStatus()
}

// The levels of a Health, as rust-analyzer's experimental/serverStatus names them.
const (
	HealthOK      = "ok"
	HealthWarning = "warning"
	HealthError   = "error"
)

// Health is how a running server says it is doing, beyond its process being up (rust-analyzer's experimental/serverStatus): whether it
// is busy, and whether something is wrong with what it was asked to load.
type Health struct {
	// Level is HealthOK (or empty), HealthWarning or HealthError; the item shows the icon of the level while the server runs and has no
	// progress to show.
	Level string
	// Busy says that the server is working (it is not quiescent); the item spins.
	Busy bool
	// Message is a line of the tooltip ("Failed to load workspace").
	Message string
}

// SetHealth sets how the server is doing; a restart of the server forgets it.
func (c *Controller) SetHealth(h Health) {
	c.health = h
	c.updateStatus()
}

// SetDetail sets a line the tooltip of the status item shows after the state of the server ("Interpreter: /usr/bin/python3").
func (c *Controller) SetDetail(text string) {
	c.detail = text
	c.updateStatus()
}

// Refresh draws the status item again; a pack calls it when what its label shows changed.
func (c *Controller) Refresh() { c.updateStatus() }

// program is the program the last start chose, the first one before any start.
func (c *Controller) program() Program {
	if res := c.lastResult(); res.have && res.program.Name != "" {
		return res.program
	}
	return c.cfg.Programs[0]
}

// serverChanged follows the state the host tells.
func (c *Controller) serverChanged(st extension.ServerStatus) {
	if st.State != extension.ServerRunning {
		c.health = Health{}
	}
	c.server = st
	if st.State == extension.ServerMissing {
		c.tellProblem()
	}
	c.updateStatus()
}

// editorEvent follows the editor in front.
func (c *Controller) editorEvent(ev extension.Event) {
	var ae extension.ActiveEditor
	if ev.Decode(&ae) != nil || ae.URI == "" {
		return // no text editor in front: the last one stays the one to refer to
	}
	c.activeIn = false
	for _, l := range c.cfg.Languages {
		if l == ae.LanguageID {
			c.activeIn = true
		}
	}
	c.updateStatus()
}

// settingsChanged restarts the server when a setting its last start read has another value now.
func (c *Controller) settingsChanged() {
	c.mu.Lock()
	read := c.last.read
	c.mu.Unlock()
	for key, was := range read {
		v, err := c.api.Settings().Get(key)
		if Fingerprint(v, err == nil) == was {
			continue
		}
		c.mu.Lock()
		c.last.read = nil // the start that follows reads them again; more changes meanwhile do not restart it again
		c.mu.Unlock()
		if err := c.api.LanguageServers().Restart(c.cfg.ID); err != nil && !benign(err) {
			c.api.Log().Warn("the language server cannot be restarted after a change of the settings", "server", c.cfg.ID, "err", err)
		}
		return
	}
}

// benign tells whether an error only says that the host has no such UI or the extension ended; nothing is logged for it.
func benign(err error) bool {
	appErr, ok := errors.FromError(err)
	if !ok {
		return false
	}
	return strings.HasSuffix(appErr.Tag, "Unavailable") || appErr.Tag == "ExtensionDeactivated"
}

// problemKind says what kind of start failure it was.
type problemKind int

const (
	problemNone problemKind = iota
	problemNotFound
	problemPathInvalid
	problemTooOld
	problemOther
)

// problem classifies the failure of the last start.
func (c *Controller) problem() (problemKind, string) {
	res := c.lastResult()
	if res.err == nil {
		return problemNone, ""
	}
	appErr, ok := errors.FromError(res.err)
	if !ok {
		return problemOther, c.cfg.Name + " cannot be started: " + res.err.Error()
	}
	switch appErr.Tag {
	case "ServerNotFound":
		return problemNotFound, appErr.Message
	case "ServerPathInvalid":
		return problemPathInvalid, appErr.Message
	case "ServerTooOld":
		return problemTooOld, appErr.Message
	}
	return problemOther, c.cfg.Name + " cannot be started: " + appErr.Message
}

// tellProblem tells the user once, in this tab, why the server is not running.
func (c *Controller) tellProblem() {
	kind, msg := c.problem()
	if kind == problemNone || c.told[msg] {
		return
	}
	c.told[msg] = true
	var actions []extension.NotificationAction
	howTo := extension.NotificationAction{Label: "How to Install", Run: c.howToInstall}
	setPath := extension.NotificationAction{Label: "Set Path...", Run: c.setPath}
	copyCmd := extension.NotificationAction{Label: "Copy Install Command", Run: c.copyInstallCommand}
	_, haveCommand := c.program().Install.CommandFor(Family(c.osRelease()))
	switch kind {
	case problemNotFound, problemTooOld:
		actions = append(actions, howTo)
		if haveCommand {
			actions = append(actions, copyCmd)
		}
		actions = append(actions, setPath)
	case problemPathInvalid:
		actions = append(actions, setPath, howTo)
	default:
		actions = append(actions, extension.NotificationAction{Label: "Show Output", Run: c.showOutput})
	}
	c.api.Notifications().Warn(msg+".", actions...)
}

// showOutput shows the Output channel of the server.
func (c *Controller) showOutput() {
	if err := c.api.LanguageServers().ShowOutput(c.cfg.ID, false); err != nil && !benign(err) {
		c.api.Log().Warn("the output of a language server cannot be shown", "server", c.cfg.ID, "err", err)
	}
}

// restart restarts the server.
func (c *Controller) restart() {
	if err := c.api.LanguageServers().Restart(c.cfg.ID); err != nil && !benign(err) {
		c.api.Log().Warn("a language server cannot be restarted", "server", c.cfg.ID, "err", err)
	}
}

// folder is the folder of the tab.
func (c *Controller) folder() string { return c.api.Scope().Folder }

// env is the Env of this machine for the tab, for the checks the UI makes.
func (c *Controller) env() Env { return DefaultEnv(c.folder(), nil) }
