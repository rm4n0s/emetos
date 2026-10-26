// Package agents is the Agents extension (SPEC §10.16, step 13.2): it runs a coding agent that the user has installed and
// logged into, in editor tabs. The first adapter is the user's own claude program (package claude, which speaks its
// stream-json protocol); this package is the UI and the state around it: the "Agents" view of the side bar (the sessions of
// the workspace), the session tab (the conversation, the input, Stop and the pickers), the editor title action that opens a
// session, "Add Selection to Agent", and the panel view that runs the program's own login in a pseudo-terminal.
//
// The extension never touches a token or a file of the program's configuration, and never runs a program of its own. Every
// program it starts (a session, the check of the login, the login itself) runs in a process group of its own that is ended
// when the tab, the view or the scope ends.
package agents

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/agents/claude"
)

// The ids of the extension.
const (
	// ID is the id of the extension; the settings are under it (agents.claude.path).
	ID = "agents"
	// ContainerID and ViewID are the container and the view of the side bar, LoginViewID the panel view that runs the login
	// and KindID the kind of the session tab.
	ContainerID = "agents.container"
	ViewID      = "agents.sessions"
	LoginViewID = "agents.login"
	KindID      = "agents.session"

	// NewSessionCommand opens a session tab and starts the program; AddSelectionCommand puts a reference to the selection of
	// the active editor in the input of a session; LoginCommand runs the login of the program in the panel.
	NewSessionCommand   = "agents.newSession"
	AddSelectionCommand = "agents.addSelection"
	LoginCommand        = "agents.login"
	// OpenSessionCommand, RenameSessionCommand and DeleteSessionCommand take {"session": id}; Rename also takes {"name": text}
	// (without it the row of the list becomes an input box).
	OpenSessionCommand   = "agents.openSession"
	RenameSessionCommand = "agents.renameSession"
	DeleteSessionCommand = "agents.deleteSession"
	// ReplayCommand is the dev command of the proofs: it plays a recorded stream into a session tab, with no program.
	ReplayCommand = "dev.agents.replay"

	// PathSetting is the program to run and AttachSetting says whether the path of the active file goes with each message.
	PathSetting   = "agents.claude.path"
	AttachSetting = "agents.attachActiveFile"
)

// agentIcon is the generic agent icon of the activity bar, the tabs and the title action.
const agentIcon = "hubot"

// maxSnapshots is how many sessions of the index keep a copy of their conversation, the most recent ones.
const maxSnapshots = 20

// New is the Factory of the extension.
func New() extension.Extension { return &agents{now: time.Now, random: rand.Reader} }

// Manifest implements extension.Extension.
func (*agents) Manifest() extension.Manifest {
	return extension.Manifest{
		ID: ID, Name: "Agents", Version: "0.1.0",
		Description: "Talks to a coding agent in editor tabs. The Claude CLI adapter runs Claude Code, the program you have installed and logged into.",
		// The extension follows the editor (the file and the selection "Add Selection to Agent" refers to) from the start, so it is
		// activated in every workspace that has a file; a workspace with no folder has no session to talk to.
		ActivationEvents: []string{extension.OnFile("**")},
		Contributes: extension.Contributes{
			Commands: []extension.CommandSpec{
				{ID: NewSessionCommand, Title: "New Session (Claude CLI)", Category: "Agents", Description: "Opens a session tab and starts the agent in the folder of the workspace."},
				{ID: AddSelectionCommand, Title: "Add Selection to Agent", Category: "Agents", Description: "Puts a reference to the selection of the active editor in the input of the session."},
				{ID: LoginCommand, Title: "Log In (Claude CLI)", Category: "Agents", Description: "Runs the login of the claude program in the panel."},
				{ID: OpenSessionCommand}, {ID: RenameSessionCommand}, {ID: DeleteSessionCommand},
			},
			Keybindings: []extension.Keybinding{{Command: AddSelectionCommand, Key: "alt+k", When: "editorTextFocus"}},
			Menus: []extension.MenuItem{
				{Menu: extension.MenuEditorContext, Command: AddSelectionCommand, Group: "9_agents", Order: 1, When: "editorTextFocus"},
			},
			ViewContainers: []extension.ViewContainerSpec{{ID: ContainerID, Title: "Agents", Icon: agentIcon, Order: 65}},
			Views:          []extension.ViewSpec{{ID: ViewID, Name: "Sessions", Container: ContainerID}},
			ViewActions:    []extension.ViewActionSpec{{View: ViewID, Command: NewSessionCommand, Icon: "add", Tooltip: "New Session", Order: 1}},
			PanelViews:     []extension.PanelViewSpec{{ID: LoginViewID, Title: "Agents: Log in", Icon: "key", Order: 40}},
			EditorKinds:    []extension.EditorKindSpec{{ID: KindID, Name: "Session", Icon: agentIcon}},
			EditorActions:  []extension.EditorActionSpec{{Command: NewSessionCommand, Icon: agentIcon, Tooltip: "New Session (runs Claude Code)", Order: 20}},
		},
		Events: extension.EventDecl{Subscribes: []string{extension.TopicEditorActiveChanged, extension.TopicEditorSelectionChanged}},
	}
}

// sessionEntry is a session of the index: what the side bar lists and what a closed session comes back with.
type sessionEntry struct {
	// ID is the session ID of the program (a UUID that Emetos made).
	ID string `json:"id"`
	// Name is the name the user gave; Title is the first message of the session.
	Name  string `json:"name,omitempty"`
	Title string `json:"title,omitempty"`
	// Created and Updated are Unix times in milliseconds.
	Created int64 `json:"created"`
	Updated int64 `json:"updated"`
	// Model and Mode are the model and the permission mode the session ended with.
	Model string `json:"model,omitempty"`
	Mode  string `json:"mode,omitempty"`
	// Items is a copy of the end of the conversation, kept for the most recent sessions, so that a session comes back with
	// its messages after its tab was closed (the transcript files of the program are not read: their format is its own).
	Items []*item `json:"items,omitempty"`
}

// label is what the list and the tab call the session.
func (e *sessionEntry) label() string {
	switch {
	case strings.TrimSpace(e.Name) != "":
		return strings.TrimSpace(e.Name)
	case strings.TrimSpace(e.Title) != "":
		return strings.TrimSpace(e.Title)
	}
	return untitled
}

// untitled is the name of a session that has no message yet.
const untitled = "New Session"

// listState is the state of the view: the index of the sessions of the workspace.
type listState struct {
	Sessions []*sessionEntry `json:"sessions"`
}

// agents is the extension of one scope. Everything in it runs on the UI thread unless a comment says otherwise.
type agents struct {
	api extension.API
	ctx context.Context
	now func() time.Time
	// random is the source of the session IDs.
	random io.Reader

	index    []*sessionEntry
	tabs     []*sessionTab
	active   *sessionTab
	handle   extension.ViewHandle
	model    *ui.ListViewModel
	renaming string
	renameIn *ui.InputBoxController

	login       *loginRun
	loginHandle extension.ViewHandle
	// loginState is what the last check of `claude auth status` said: loginUnknown, loginIn or loginOut. It is read from the
	// goroutines that start programs.
	loginState atomic.Int32

	editor lastEditor
	// out is the Output channel "Agents" and status the item of the status bar (status.go).
	out    extension.OutputChannel
	status extension.StatusHandle
	// statusKey is what the status item shows now ("" when it is hidden).
	statusKey string
	// closers waits for the programs that were told to end, so that a test can see that none is left.
	closers sync.WaitGroup
}

// The values of agents.loginState.
const (
	loginUnknown int32 = iota
	loginIn
	loginOut
)

// Activate implements extension.Extension. The state of the view (the index) comes back when the view is provided, before
// the tabs of the last session are made.
func (a *agents) Activate(ctx context.Context, api extension.API) error {
	a.api, a.ctx = api, ctx
	a.model = ui.NewListViewModel(ui.ListViewModelOpts{RowHeight: foundation.Some(24.0)})
	if err := a.registerSettings(); err != nil {
		return err
	}
	var err error
	if a.out, err = api.Output().Channel(OutputChannel); err != nil {
		return err
	}
	if a.status, err = api.StatusBar().Add(extension.StatusItem{ID: StatusID, Name: "Agents", Alignment: extension.StatusLeft, Priority: 40}); err != nil {
		return err
	}
	a.status.SetVisible(false)
	a.handle, err = api.Views().Provide(ViewID, extension.ViewContent{Build: a.build, SaveState: a.saveState, RestoreState: a.restoreState})
	if err != nil {
		return err
	}
	if err := a.provideLogin(); err != nil {
		return err
	}
	for id, h := range map[string]extension.CommandHandler{
		NewSessionCommand: a.newSession, AddSelectionCommand: a.addSelection, LoginCommand: a.loginCommand,
		OpenSessionCommand: a.openSessionCommand, RenameSessionCommand: a.renameCommand, DeleteSessionCommand: a.deleteCommand,
	} {
		if err := api.Commands().Handle(id, h); err != nil {
			return err
		}
	}
	for _, topic := range []string{extension.TopicEditorActiveChanged, extension.TopicEditorSelectionChanged} {
		if _, err := api.Events().Subscribe(topic, a.editorEvent); err != nil {
			return err
		}
	}
	if err := a.registerDev(); err != nil {
		return err
	}
	return api.Editors().Provide(KindID, extension.EditorKind{Create: a.createTab})
}

// registerSettings registers the settings of the extension.
func (a *agents) registerSettings() error {
	if err := a.api.Settings().Register(extension.SettingSpec{
		Key: PathSetting, Type: "string", Category: "Agents",
		Description: "The claude program that the Claude CLI adapter runs. Empty runs the one named `claude` in PATH. A relative path is relative to the folder of the workspace.",
	}); err != nil {
		return err
	}
	return a.api.Settings().Register(extension.SettingSpec{
		Key: AttachSetting, Type: "boolean", Default: true, Category: "Agents",
		Description: "Add the path of the file open in the active editor to each message sent to the agent. Files that the read deny rules of the project (or `.env` and similar files) exclude are never added.",
	})
}

// adapter makes the adapter for the workspace, from the setting. A relative path is relative to the folder.
func (a *agents) adapter() *claude.Adapter {
	path, _ := extension.SettingValue[string](a.api.Settings(), PathSetting)
	path = strings.TrimSpace(path)
	folder := a.api.Scope().Folder
	if path != "" && strings.Contains(path, "/") && !filepath.IsAbs(path) && folder != "" {
		path = filepath.Join(folder, path)
	}
	return &claude.Adapter{Path: path, Dir: folder, Logger: a.logger()}
}

// attachActiveFile reads the setting agents.attachActiveFile.
func (a *agents) attachActiveFile() bool {
	v, err := extension.SettingValue[bool](a.api.Settings(), AttachSetting)
	return err != nil || v
}

// newUUID makes a random (version 4) UUID, the ID of a new session.
func newUUID(random io.Reader) (string, error) {
	var b [16]byte
	if _, err := io.ReadFull(random, b[:]); err != nil {
		return "", errors.NewErr("SessionIDFailed", err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:], nil
}

// ---- the index ----

// entry is the session of the index with the ID, or nil.
func (a *agents) entry(id string) *sessionEntry {
	for _, e := range a.index {
		if e.ID == id {
			return e
		}
	}
	return nil
}

// saveState implements the state of the view: the index, with a copy of the conversation for the most recent sessions.
func (a *agents) saveState() (any, error) {
	byAge := slices.Clone(a.index)
	slices.SortStableFunc(byAge, func(x, y *sessionEntry) int { return int(y.Updated - x.Updated) })
	for i, e := range byAge {
		if i >= maxSnapshots {
			e.Items = nil
		}
	}
	return listState{Sessions: a.index}, nil
}

// restoreState gives the view the index of the last session. The sessions that tabs made already stay.
func (a *agents) restoreState(raw json.RawMessage) error {
	var st listState
	if err := json.Unmarshal(raw, &st); err != nil {
		return errors.NewErr("AgentsStateInvalid", err)
	}
	for _, e := range st.Sessions {
		if e != nil && e.ID != "" && a.entry(e.ID) == nil {
			a.index = append(a.index, e)
		}
	}
	a.sync()
	return nil
}

// touch records a session in the index (making it on its first message) and returns it.
func (a *agents) touch(t *sessionTab) *sessionEntry {
	e := a.entry(t.id)
	if e == nil {
		e = &sessionEntry{ID: t.id, Created: a.now().UnixMilli()}
		a.index = append(a.index, e)
	}
	e.Updated = a.now().UnixMilli()
	e.Model, e.Mode = t.model, string(t.mode)
	return e
}

// remember keeps the end of the conversation of the session in the index, for a tab that comes back after it was closed.
func (a *agents) remember(t *sessionTab) {
	if e := a.entry(t.id); e != nil {
		e.Items = t.tr.saved(maxSnapshotBytes)
	}
}

// remove deletes a session from the index and closes its tabs.
func (a *agents) remove(id string) {
	i := slices.IndexFunc(a.index, func(e *sessionEntry) bool { return e.ID == id })
	if i < 0 {
		return
	}
	a.index = slices.Delete(a.index, i, i+1)
	for _, t := range slices.Clone(a.tabs) {
		if t.id == id {
			t.forget = true
			t.tab.Close()
		}
	}
	a.sync()
}

// newest returns the sessions of the index, the most recently used first.
func (a *agents) newest() []*sessionEntry {
	out := slices.Clone(a.index)
	slices.SortStableFunc(out, func(x, y *sessionEntry) int { return int(y.Updated - x.Updated) })
	return out
}

// wait returns when the programs that tabs and the login told to end are gone; it is for tests.
func (a *agents) wait() { a.closers.Wait() }
