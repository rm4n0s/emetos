package exthost

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/document"
)

// The core request topics, the privileges and the confinement (step 16.1, SPEC §10.19 "As built (step 16.1)").

func TestPrivilegesAreRefusedToEveryExtensionButRelay(t *testing.T) {
	priv := []string{extension.PrivilegeEventsPublishAny}
	mk := func(id string, privileges []string) extension.Factory {
		m := manifest(id)
		m.Privileges = privileges
		f, _ := factory(m, nil)
		return f
	}
	r := newRig(t,
		mk(extension.RelayID, append(priv, extension.PrivilegeCommandsExecute)),
		mk("ext.sneaky", priv),
		mk("emetos.relay2", []string{extension.PrivilegeCommandsExecute}),
		mk("ext.typo", []string{"files.deleteAll"}),
		mk("ext.fine", nil),
	)
	var ids []string
	for _, m := range r.host.Manifests() {
		ids = append(ids, m.ID)
	}
	if !slices.Equal(ids, []string{extension.RelayID, "ext.fine"}) {
		t.Errorf("the extensions kept are %v", ids)
	}
	problems := r.host.Problems()
	if len(problems) != 3 {
		t.Fatalf("problems: %v", problems)
	}
	wantRoute(t, problems[0], "validateManifest.ExtensionPrivilegeRefused")
	wantRoute(t, problems[1], "validateManifest.ExtensionPrivilegeRefused")
	wantRoute(t, problems[2], "checkPrivileges.ExtensionPrivilegeUnknown")
	if appErr, _ := errors.FromError(problems[0]); appErr.Metadata["id"] != "ext.sneaky" {
		t.Errorf("the problem is about %v", appErr.Metadata["id"])
	}
	// the extension that was refused is not installed: no scope has it, and it is on no row of the page
	tb := r.newTab("one")
	tb.scope.Start()
	for _, row := range r.host.Rows(nil) {
		if row.Manifest.ID == "ext.sneaky" || row.Manifest.ID == "emetos.relay2" {
			t.Errorf("%s is listed", row.Manifest.ID)
		}
	}
	if _, _, found := tb.scope.status("ext.sneaky"); found {
		t.Error("the refused extension has a slot in the scope")
	}
}

func TestTheTerminalMayDeclareOnlyCommandsExecute(t *testing.T) {
	mk := func(id string, privileges []string) extension.Factory {
		m := manifest(id)
		m.Privileges = privileges
		f, _ := factory(m, nil)
		return f
	}
	r := newRig(t,
		mk(extension.TerminalID, []string{extension.PrivilegeCommandsExecute}),
		mk("emetos.terminal2", []string{extension.PrivilegeCommandsExecute}),
	)
	var ids []string
	for _, m := range r.host.Manifests() {
		ids = append(ids, m.ID)
	}
	if !slices.Equal(ids, []string{extension.TerminalID}) {
		t.Errorf("the extensions kept are %v", ids)
	}
	if problems := r.host.Problems(); len(problems) != 1 {
		t.Errorf("problems: %v", problems)
	} else {
		wantRoute(t, problems[0], "validateManifest.ExtensionPrivilegeRefused")
	}
	// events.publishAny is not the terminal's: the extension is left out as a whole
	r = newRig(t, mk(extension.TerminalID, []string{extension.PrivilegeCommandsExecute, extension.PrivilegeEventsPublishAny}))
	if len(r.host.Manifests()) != 0 || len(r.host.Problems()) != 1 {
		t.Errorf("manifests %v, problems %v", r.host.Manifests(), r.host.Problems())
	}
}

func TestOnlyAPrivilegedExtensionPublishesAnyTopic(t *testing.T) {
	c := newCoreHarness(t, nil)
	heard := c.subscribe(plain, "**")
	for _, tt := range []struct {
		from, topic string
		route       string
	}{
		{privileged, "ext.plain.thing", ""},
		{privileged, "emetos.document.changed", ""},
		{privileged, "anything.at.all", ""},
		{plain, "ext.plain.thing", ""},
		{plain, "emetos.relay.x", "eventsAPI.Publish.EventTopicNotOwned"},
		{plain, "anything.at.all", "eventsAPI.Publish.EventTopicNotOwned"},
		{plain, "emetos.document.changed", "eventsAPI.Publish.EventTopicNotOwned"},
		{privileged, "bad..topic", "CheckTopic.EventTopicInvalid"},
	} {
		err := c.events(tt.from).Publish(tt.topic, map[string]int{"n": 1})
		if tt.route == "" {
			if err != nil {
				t.Errorf("%s publishes %s: %v", tt.from, tt.topic, err)
			}
			continue
		}
		wantRoute(t, err, tt.route)
	}
	c.tb.pump()
	got := map[string]string{}
	for _, ev := range heard.all() {
		got[ev.Topic] = ev.Publisher
	}
	if got["emetos.document.changed"] != privileged || got["anything.at.all"] != privileged || got["ext.plain.thing"] == "" {
		t.Errorf("heard %v: the publisher of a privileged event is the extension, never emetos", got)
	}
}

func TestCommandsExecuteNeedsThePrivilegeAndRunsInTheScope(t *testing.T) {
	c := newCoreHarness(t, nil)
	c.register("test.hello", func(_ command.Context, args any) error {
		c.ran = append(c.ran, "test.hello")
		c.runArgs = append(c.runArgs, args)
		return nil
	})
	_, err := c.ask(plain, extension.TopicCommandsExecute, extension.CommandsExecuteRequest{ID: "test.hello"})
	wantRoute(t, err, "Scope.coreCommandsExecute.ExtensionPrivilegeMissing")
	if len(c.ran) != 0 {
		t.Fatal("an extension without the privilege ran a command")
	}
	args := json.RawMessage(`{"n":1,"list":["a"]}`)
	ev, err := c.ask(privileged, extension.TopicCommandsExecute, extension.CommandsExecuteRequest{ID: "test.hello", Args: args})
	if err != nil {
		t.Fatal(err)
	}
	reply, err := extension.EventValue[extension.CommandsExecuteReply](ev)
	if err != nil || string(reply.Result) != "null" || ev.Publisher != extension.CorePublisher || ev.Topic != extension.TopicCommandsExecute {
		t.Errorf("reply %+v (%v), event %+v", reply, err, ev)
	}
	want := map[string]any{"n": float64(1), "list": []any{"a"}}
	if len(c.ran) != 1 || !jsonEqual(c.runArgs[0], want) {
		t.Errorf("ran %v with %v", c.ran, c.runArgs)
	}
	// a command without arguments gets nil
	if _, err := c.ask(privileged, extension.TopicCommandsExecute, extension.CommandsExecuteRequest{ID: "test.hello", Args: json.RawMessage("null")}); err != nil || c.runArgs[1] != nil {
		t.Errorf("args %v, %v", c.runArgs, err)
	}
	// an extension's own command runs in the scope too
	if err := c.api[plain].Commands().Register(extension.CommandSpec{ID: "ext.plain.go", Title: "Go"}, func(context.Context, any) error {
		c.counters["ext.plain.go"]++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ask(privileged, extension.TopicCommandsExecute, extension.CommandsExecuteRequest{ID: "ext.plain.go"}); err != nil || c.counters["ext.plain.go"] != 1 {
		t.Errorf("the command of another extension: %v, ran %d", err, c.counters["ext.plain.go"])
	}
}

func jsonEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

// Every class of refused commands is refused to the privileged extension, and the command is not run.
func TestCommandsExecuteRefusesEveryClass(t *testing.T) {
	c := newCoreHarness(t, nil)
	examples := map[string][]string{
		extension.RefusedWindowTab: {"emetos.workspaceTab.close", "emetos.workspaceTab.openFolder", "emetos.workspaceTab.next", "emetos.windowTab.open"},
		extension.RefusedQuit:      {"workbench.action.quit", "workbench.action.closeWindow"},
		extension.RefusedWindow:    {"workbench.action.newWindow", "workbench.action.switchWindow"},
		extension.RefusedFolder:    {"workbench.action.files.openFolder", "workbench.action.closeFolder", "workbench.action.openRecent", "openRecentFolder", "openRecentFile"},
		extension.RefusedProfile:   {"workbench.profiles.actions.switchProfile", "workbench.profiles.actions.createProfile"},
		extension.RefusedSettings:  {"workbench.action.openSettingsJson", "workbench.action.toggleAutoSave", "workbench.action.selectTheme", "emetos.extensions.manageEventSharing"},
		extension.RefusedRemote:    {"emetos.relay.listen", "emetos.remotework.manage"},
		extension.RefusedTerminal:  {"workbench.action.terminal.new", "workbench.action.terminal.sendSequence", "workbench.action.createTerminalEditor"},
		extension.RefusedOutside:   {"emetos.databases.newConnection", "emetos.databases.connect", "emetos.sql.newQuery"},
	}
	if len(examples) != len(command.RefusedClasses()) {
		t.Fatalf("%d classes have an example, the policy has %d", len(examples), len(command.RefusedClasses()))
	}
	for class, ids := range examples {
		for _, id := range ids {
			t.Run(id, func(t *testing.T) {
				c.register(id, func(command.Context, any) error { c.counters[id]++; return nil })
				_, err := c.ask(privileged, extension.TopicCommandsExecute, extension.CommandsExecuteRequest{ID: id})
				wantRoute(t, err, "Scope.coreCommandsExecute.RelayCommandRefused")
				appErr, _ := errors.FromError(err)
				if appErr.Metadata["class"] != class || appErr.Metadata["command"] != id {
					t.Errorf("metadata %v, want class %s", appErr.Metadata, class)
				}
				if c.counters[id] != 0 {
					t.Error("the refused command ran")
				}
			})
		}
	}
	// the settings of the workspace are allowed
	c.register("workbench.action.openWorkspaceSettingsFile", func(command.Context, any) error { c.counters["ws"]++; return nil })
	if _, err := c.ask(privileged, extension.TopicCommandsExecute, extension.CommandsExecuteRequest{ID: "workbench.action.openWorkspaceSettingsFile"}); err != nil || c.counters["ws"] != 1 {
		t.Errorf("workspace settings: %v, ran %d", err, c.counters["ws"])
	}
}

func TestCommandsExecuteErrors(t *testing.T) {
	c := newCoreHarness(t, nil)
	c.register("test.foreign", func(command.Context, any) error { return os.ErrPermission })
	c.register("test.ours", func(command.Context, any) error { return errors.New("TestOwnFailure", "ours") })
	c.register("test.panics", func(command.Context, any) error { panic("boom") })
	if err := c.tb.commands.Register(command.Meta{ID: "test.gated", Title: "Gated", Precondition: mustParse(t, "someKeyThatIsNotSet")}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.tb.commands.SetHandler("test.gated", func(command.Context, any) error { return nil }); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		payload any
		route   string
	}{
		{"no such command", extension.CommandsExecuteRequest{ID: "no.such.command"}, "Scope.coreCommandsExecute.CoreCommandNotFound"},
		{"no handler", extension.CommandsExecuteRequest{ID: "workbench.action.files.save"}, "Scope.coreCommandsExecute.CoreCommandDisabled"},
		{"a precondition that does not hold", extension.CommandsExecuteRequest{ID: "test.gated"}, "Scope.coreCommandsExecute.CoreCommandDisabled"},
		{"a payload that is not a request", json.RawMessage(`"x"`), "decodeCore->decodeJSON.CorePayloadInvalid"},
		{"an error of another package", extension.CommandsExecuteRequest{ID: "test.foreign"}, "Scope.coreCommandsExecute.CoreCommandFailed"},
		{"our own error is returned as it is", extension.CommandsExecuteRequest{ID: "test.ours"}, "test_error"},
		{"a command that panics", extension.CommandsExecuteRequest{ID: "test.panics"}, "startCore.func3.CoreHandlerPanicked"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := c.ask(privileged, extension.TopicCommandsExecute, tt.payload)
			if tt.route == "test_error" {
				if appErr, ok := errors.FromError(err); !ok || appErr.Tag != "TestOwnFailure" {
					t.Errorf("error %v", err)
				}
				return
			}
			wantRoute(t, err, tt.route)
		})
	}
	// a panic in a command of the core is not a failure of the extension that asked
	if st := c.tb.scope.State(privileged); st != StateActive {
		t.Errorf("the requester is %v", st)
	}
	// a scope with no commands
	n := newCoreHarness(t, func(r *rig) { r.noCommands["one"] = true })
	_, err := n.ask(privileged, extension.TopicCommandsExecute, extension.CommandsExecuteRequest{ID: "x.y"})
	wantRoute(t, err, "Scope.coreCommandsExecute.CoreCommandsUnavailable")
	_, err = n.ask(plain, extension.TopicCommandsList, nil)
	wantRoute(t, err, "Scope.coreCommandsList.CoreCommandsUnavailable")
}

func mustParse(t *testing.T, when string) contextkey.Expr {
	t.Helper()
	e, err := contextkey.Parse(when)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestCommandsListMarksTheRefusedOnes(t *testing.T) {
	c := newCoreHarness(t, nil)
	c.register("test.hello", func(command.Context, any) error { return nil })
	for _, from := range []string{privileged, plain} { // no privilege is needed to list
		ev, err := c.ask(from, extension.TopicCommandsList, nil)
		if err != nil {
			t.Fatal(err)
		}
		list, err := extension.EventValue[extension.CommandsList](ev)
		if err != nil {
			t.Fatal(err)
		}
		byID := map[string]extension.CommandInfo{}
		for _, info := range list.Commands {
			byID[info.ID] = info
		}
		if !slices.IsSortedFunc(list.Commands, func(a, b extension.CommandInfo) int { return strings.Compare(a.ID, b.ID) }) {
			t.Error("the list is not ordered by id")
		}
		for id, class := range map[string]string{
			"workbench.action.quit": extension.RefusedQuit, "emetos.workspaceTab.close": extension.RefusedWindowTab,
			"workbench.profiles.actions.switchProfile": extension.RefusedProfile, "workbench.action.openSettingsJson": extension.RefusedSettings,
		} {
			info := byID[id]
			if !info.Refused || info.RefusedClass != class || info.Reason == "" {
				t.Errorf("%s: %+v", id, info)
			}
		}
		if h := byID["test.hello"]; h.Refused || !h.Enabled || h.Title != "test.hello" || h.RefusedClass != "" {
			t.Errorf("test.hello: %+v", h)
		}
		if s := byID["workbench.action.files.save"]; s.Refused || s.Enabled {
			t.Errorf("a command with no handler is not enabled: %+v", s)
		}
	}
}

// The command of a client runs in the tab the extension instance belongs to, never in another.
func TestACommandRunInOneTabDoesNotReachAnother(t *testing.T) {
	apis := map[string]extension.API{}
	m := manifest(privileged)
	m.Privileges = []string{extension.PrivilegeCommandsExecute}
	f, _ := factory(m, func(f *fake) {
		f.activate = func(_ context.Context, api extension.API) error { apis[api.Scope().Name] = api; return nil }
	})
	r := newRig(t, f)
	a, b := r.newTab("a"), r.newTab("b")
	a.scope.Start()
	b.scope.Start()
	runs := map[string]int{}
	for name, tb := range map[string]*tab{"a": a, "b": b} {
		if err := tb.commands.Register(command.Meta{ID: "test.where", Title: "Where"}); err != nil {
			t.Fatal(err)
		}
		if _, err := tb.commands.SetHandler("test.where", func(command.Context, any) error { runs[name]++; return nil }); err != nil {
			t.Fatal(err)
		}
	}
	// only tab b has this one
	if err := b.commands.Register(command.Meta{ID: "test.onlyB", Title: "B"}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.commands.SetHandler("test.onlyB", func(command.Context, any) error { runs["onlyB"]++; return nil }); err != nil {
		t.Fatal(err)
	}
	ask := func(name string, id string) error {
		o := newOutcome()
		if err := apis[name].Events().Request(context.Background(), extension.TopicCommandsExecute, extension.CommandsExecuteRequest{ID: id}, o.fn); err != nil {
			t.Fatal(err)
		}
		_, _, err := o.get()
		return err
	}
	if err := ask("a", "test.where"); err != nil {
		t.Fatal(err)
	}
	if runs["a"] != 1 || runs["b"] != 0 {
		t.Errorf("a command asked in tab a ran %v", runs)
	}
	if err := ask("b", "test.where"); err != nil || runs["b"] != 1 || runs["a"] != 1 {
		t.Errorf("a command asked in tab b ran %v (%v)", runs, err)
	}
	wantRoute(t, ask("a", "test.onlyB"), "Scope.coreCommandsExecute.CoreCommandNotFound")
	if runs["onlyB"] != 0 {
		t.Error("a command of tab b ran from tab a")
	}
}

func TestFilesTopicsAreConfinedToTheFolder(t *testing.T) {
	c := newCoreHarness(t, nil)
	ask := func(topic string, payload any) (extension.Event, error) { return c.ask(plain, topic, payload) } // no privilege is needed
	t.Run("list", func(t *testing.T) {
		ev, err := ask(extension.TopicFilesList, extension.FilesListRequest{Depth: 2, Limit: 4})
		if err != nil {
			t.Fatal(err)
		}
		reply, _ := extension.EventValue[extension.FilesListReply](ev)
		var got []string
		for _, e := range reply.Entries {
			got = append(got, e.Path)
		}
		if !slices.Equal(got, []string{"a.txt", "bin", "fifo", "in"}) || !reply.More {
			t.Errorf("entries %v more=%v", got, reply.More)
		}
		ev, err = ask(extension.TopicFilesList, extension.FilesListRequest{Path: "src"})
		reply, _ = extension.EventValue[extension.FilesListReply](ev)
		if err != nil || len(reply.Entries) != 1 || reply.Entries[0].Path != "src/m.go" || reply.More {
			t.Errorf("src: %+v %v", reply, err)
		}
		ev, err = ask(extension.TopicFilesList, extension.FilesListRequest{Offset: 100})
		if err != nil || !strings.Contains(string(ev.Data), `"entries":[]`) {
			t.Errorf("past the end: %s %v", ev.Data, err)
		}
	})
	t.Run("stat and read", func(t *testing.T) {
		ev, err := ask(extension.TopicFilesStat, extension.FilesPathRequest{Path: "a.txt"})
		entry, _ := extension.EventValue[extension.FileEntry](ev)
		if err != nil || entry.Kind != extension.EntryFile || entry.Size != 12 {
			t.Errorf("stat %+v %v", entry, err)
		}
		ev, err = ask(extension.TopicFilesRead, extension.FilesPathRequest{Path: "src/m.go"})
		read, _ := extension.EventValue[extension.FilesReadReply](ev)
		if err != nil || read.Text != "package main\n" || read.Path != "src/m.go" {
			t.Errorf("read %+v %v", read, err)
		}
		ev, err = ask(extension.TopicFilesRead, extension.FilesPathRequest{Path: "in"})
		read, _ = extension.EventValue[extension.FilesReadReply](ev)
		if err != nil || read.Text != "hello\nworld\n" {
			t.Errorf("read through a link inside %+v %v", read, err)
		}
	})
	tests := []struct {
		name, topic, path, route string
	}{
		{"list ..", extension.TopicFilesList, "..", "List->Clean.ConfinePathInvalid"},
		{"stat ..", extension.TopicFilesStat, "../outside/secret", "Stat->Clean.ConfinePathInvalid"},
		{"read ..", extension.TopicFilesRead, "src/../../outside/secret", "Read->Clean.ConfinePathInvalid"},
		{"read an absolute path", extension.TopicFilesRead, "/etc/passwd", "Read->Clean.ConfinePathInvalid"},
		{"read the absolute path of the secret", extension.TopicFilesRead, "/tmp/secret", "Read->Clean.ConfinePathInvalid"},
		{"read a link out", extension.TopicFilesRead, "out-abs", "Read->statIn.ConfineOutside"},
		{"read through a folder link out", extension.TopicFilesRead, "out-dir/secret", "Read->statIn.ConfineOutside"},
		{"stat a link out", extension.TopicFilesStat, "out-dir", "Stat->statIn.ConfineOutside"},
		{"list a link out", extension.TopicFilesList, "out-dir", "List->statIn.ConfineOutside"},
		{"read a loop", extension.TopicFilesRead, "loop1", "Read->statIn.ConfineSymlinkLoop"},
		{"read a fifo", extension.TopicFilesRead, "fifo", "Read.ConfineSpecialFile"},
		{"read a folder", extension.TopicFilesRead, "src", "Read.ConfineIsDirectory"},
		{"read binary", extension.TopicFilesRead, "bin", "Read->readChecked.ConfineNotText"},
		{"read what is not there", extension.TopicFilesRead, "nope", "Read->statIn.ConfineNotFound"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var payload any = extension.FilesPathRequest{Path: tt.path}
			if tt.topic == extension.TopicFilesList {
				payload = extension.FilesListRequest{Path: tt.path}
			}
			_, err := ask(tt.topic, payload)
			wantRoute(t, err, tt.route)
		})
	}
	t.Run("a payload that is not a request", func(t *testing.T) {
		for _, topic := range []string{extension.TopicFilesList, extension.TopicFilesStat, extension.TopicFilesRead} {
			_, err := ask(topic, json.RawMessage(`[1]`))
			wantRoute(t, err, "decodeCore->decodeJSON.CorePayloadInvalid")
		}
	})
	t.Run("a scope with no folder", func(t *testing.T) {
		n := newCoreHarness(t, func(r *rig) { r.folders["one"] = "" })
		_, err := n.ask(plain, extension.TopicFilesRead, extension.FilesPathRequest{Path: "a.txt"})
		wantRoute(t, err, "Read->openRoot.ConfineNoFolder")
	})
	t.Run("a request that waits for a slot while it is cancelled", func(t *testing.T) {
		s := c.tb.scope
		for range FileOpSlots {
			s.fileSlots <- struct{}{}
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		done := make(chan error, 1)
		s.fileOp(&coreCall{ctx: ctx, reply: func(_ any, err error) { done <- err }}, func(context.Context, string) (any, error) { return nil, nil })
		wantRoute(t, <-done, "Scope.fileOp.func1.CoreFileWaitCanceled")
		for range FileOpSlots {
			<-s.fileSlots
		}
	})
}

// The window tabs of extensions have no workspace to answer for.
func TestCoreTopicsAreNotAnsweredInAWindowTabOrByAnExtension(t *testing.T) {
	c := newCoreHarness(t, nil)
	// an extension cannot take a core topic: the handler is refused
	wantRoute(t, c.events(plain).Handle(extension.TopicFilesRead, func(context.Context, extension.Event) (any, error) { return nil, nil }), "eventsAPI.HandleAsync.EventTopicNotOwned")
	wantRoute(t, c.events(privileged).Handle(extension.TopicCommandsExecute, func(context.Context, extension.Event) (any, error) { return nil, nil }), "eventsAPI.HandleAsync.EventTopicNotOwned")
	// the window tab of an extension has no workspace to answer for: its requests find no handler
	c.tb.scope.opts.Info.Kind = extension.ScopeWindowTab
	_, err := c.ask(privileged, extension.TopicFilesList, extension.FilesListRequest{})
	wantRoute(t, err, "bus.start.EventNoHandler")
	c.tb.scope.opts.Info.Kind = extension.ScopeWorkspace
	// the list of the topics is the list of the handlers
	for _, topic := range extension.CoreRequestTopics() {
		if coreHandlerFor(topic) == nil {
			t.Errorf("%s has no handler", topic)
		}
	}
	if coreHandlerFor("emetos.files.changed") != nil || coreHandlerFor("emetos.nothing") != nil {
		t.Error("a topic that is not a request has a handler")
	}
}

func (c *coreHarness) fileURI(rel string) string {
	return document.FileURI(filepath.Join(c.dir, filepath.FromSlash(rel))).String()
}

func TestDocumentTopics(t *testing.T) {
	c := newCoreHarness(t, nil)
	docs := c.tb.coreDocs
	a := docs.add(document.FileURI(filepath.Join(c.dir, "a.txt")), "hello\nwörld\n")
	untitled := docs.add(document.UntitledURI(1), "scratch")
	outside := docs.add(document.FileURI(filepath.Join(c.outside, "other.go")), "package other\n")
	viaLink := docs.add(document.FileURI(filepath.Join(c.dir, "out-dir", "other.go")), "package other\n")
	virtual := docs.add(document.VirtualURI("settings.json"), "{}")
	ask := func(topic string, payload any) (extension.Event, error) { return c.ask(plain, topic, payload) }
	aURI := c.fileURI("a.txt")
	edit := func(uri string, version int, edits ...extension.TextChange) (extension.DocumentEditReply, error) {
		ev, err := ask(extension.TopicDocumentEdit, extension.DocumentEditRequest{URI: uri, Version: version, Edits: edits})
		if err != nil {
			return extension.DocumentEditReply{}, err
		}
		return extension.EventValue[extension.DocumentEditReply](ev)
	}
	rng := func(l1, c1, l2, c2 int) extension.Range {
		return extension.Range{Start: extension.Position{Line: l1, Col: c1}, End: extension.Position{Line: l2, Col: c2}}
	}

	t.Run("open", func(t *testing.T) {
		ev, err := ask(extension.TopicDocumentOpen, extension.DocumentOpenRequest{Path: "src/m.go", Line: 0, Col: 3})
		info, _ := extension.EventValue[extension.DocumentInfo](ev)
		want := filepath.Join(c.dir, "src", "m.go")
		if err != nil || info.URI != "file://"+want || info.Version != 1 || info.LanguageID == "" {
			t.Fatalf("open: %+v %v", info, err)
		}
		if len(docs.opened) != 1 || docs.opened[0] != want || docs.revealAt[0] != (extension.Position{Line: 0, Col: 3}) {
			t.Errorf("the window opened %v at %v", docs.opened, docs.revealAt)
		}
		docs.openErr = errors.New("TestOpenRefused", "no")
		_, err = ask(extension.TopicDocumentOpen, extension.DocumentOpenRequest{Path: "a.txt"})
		wantRoute(t, err, "TestOpenRefused")
		docs.openErr = nil
	})
	t.Run("open is confined", func(t *testing.T) {
		for path, route := range map[string]string{
			"../outside/other.go": "CheckFile->Clean.ConfinePathInvalid", "/etc/passwd": "CheckFile->Clean.ConfinePathInvalid",
			"out-abs": "CheckFile->statIn.ConfineOutside", "out-dir/other.go": "CheckFile->statIn.ConfineOutside",
			"loop1": "CheckFile->statIn.ConfineSymlinkLoop", "fifo": "CheckFile.ConfineSpecialFile", "src": "CheckFile.ConfineIsDirectory",
			"nope.go": "CheckFile->statIn.ConfineNotFound",
		} {
			_, err := ask(extension.TopicDocumentOpen, extension.DocumentOpenRequest{Path: path})
			wantRoute(t, err, route)
		}
		if len(docs.opened) != 2 { // the two of the test before
			t.Errorf("the window opened %v", docs.opened)
		}
	})
	t.Run("read", func(t *testing.T) {
		ev, err := ask(extension.TopicDocumentRead, extension.DocumentURIRequest{URI: aURI})
		got, _ := extension.EventValue[extension.DocumentContent](ev)
		if err != nil || got.Text != "hello\nwörld\n" || got.Version != a.Version() || got.Dirty || got.URI != aURI {
			t.Errorf("read %+v %v", got, err)
		}
		ev, err = ask(extension.TopicDocumentRead, extension.DocumentURIRequest{URI: "untitled:1"})
		got, _ = extension.EventValue[extension.DocumentContent](ev)
		if err != nil || got.Text != "scratch" {
			t.Errorf("untitled %+v %v", got, err)
		}
		_, err = ask(extension.TopicDocumentRead, extension.DocumentURIRequest{URI: c.fileURI("closed.txt")})
		wantRoute(t, err, "Scope.coreDocumentRead.CoreDocumentNotOpen")
		_, err = ask(extension.TopicDocumentRead, extension.DocumentURIRequest{URI: "nonsense"})
		wantRoute(t, err, "ParseURI.URIInvalid")
		_, err = ask(extension.TopicDocumentRead, json.RawMessage(`1`))
		wantRoute(t, err, "decodeCore->decodeJSON.CorePayloadInvalid")
		big := docs.add(document.FileURI(filepath.Join(c.dir, "big.txt")), strings.Repeat("x", extension.MaxDocumentReadSize+1))
		_, err = ask(extension.TopicDocumentRead, extension.DocumentURIRequest{URI: c.fileURI("big.txt")})
		wantRoute(t, err, "Scope.coreDocumentRead.CoreDocumentTooLarge")
		docs.mu.Lock()
		delete(docs.docs, big.URI())
		docs.mu.Unlock()
	})
	t.Run("edit is one undo step", func(t *testing.T) {
		v := a.Version()
		reply, err := edit(aURI, v,
			extension.TextChange{Range: rng(0, 0, 0, 5), Text: "HELLO"},
			extension.TextChange{Range: rng(1, 0, 1, 0), Text: ">> "})
		if err != nil || !reply.Changed || reply.Version != v+1 || reply.URI != aURI {
			t.Fatalf("edit %+v %v", reply, err)
		}
		if a.Text() != "HELLO\n>> wörld\n" {
			t.Errorf("text %q", a.Text())
		}
		if docs.began != docs.end || docs.began == 0 {
			t.Errorf("the window was told %d/%d", docs.began, docs.end)
		}
		if _, err := a.Undo(); err != nil || a.Text() != "hello\nwörld\n" {
			t.Errorf("one undo should bring the text back: %q %v", a.Text(), err)
		}
	})
	t.Run("a stale version is refused", func(t *testing.T) {
		v := a.Version()
		_, err := edit(aURI, v-1, extension.TextChange{Range: rng(0, 0, 0, 1), Text: "X"})
		wantRoute(t, err, "ApplyTextEdits.RelayStaleVersion")
		appErr, _ := errors.FromError(err)
		if appErr.Metadata["current"] != v || appErr.Metadata["version"] != v-1 {
			t.Errorf("metadata %v", appErr.Metadata)
		}
		_, err = edit(aURI, v+5, extension.TextChange{Range: rng(0, 0, 0, 1), Text: "X"})
		wantRoute(t, err, "ApplyTextEdits.RelayStaleVersion")
		if a.Text() != "hello\nwörld\n" || a.Version() != v {
			t.Error("a refused edit changed the document")
		}
	})
	t.Run("a changed text makes the version stale", func(t *testing.T) {
		v := a.Version()
		if _, err := edit(aURI, v, extension.TextChange{Range: rng(0, 0, 0, 1), Text: "J"}); err != nil {
			t.Fatal(err)
		}
		_, err := edit(aURI, v, extension.TextChange{Range: rng(0, 0, 0, 1), Text: "K"})
		wantRoute(t, err, "ApplyTextEdits.RelayStaleVersion")
		if _, err := a.Undo(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("an edit that is not valid changes nothing", func(t *testing.T) {
		v := a.Version()
		for name, edits := range map[string][]extension.TextChange{
			"past the last line": {{Range: rng(9, 0, 9, 0), Text: "x"}},
			"inside a character (the byte after the first of o with a diaeresis)": {{Range: rng(1, 2, 1, 2), Text: "x"}},
			"past the line":         {{Range: rng(0, 99, 0, 99), Text: "x"}},
			"a negative column":     {{Range: rng(0, -1, 0, 0), Text: "x"}},
			"ends before it starts": {{Range: rng(1, 3, 0, 1), Text: "x"}},
			"overlapping":           {{Range: rng(0, 0, 0, 4), Text: "x"}, {Range: rng(0, 2, 0, 5), Text: "y"}},
		} {
			_, err := edit(aURI, v, edits...)
			wantRoute(t, err, "ApplyTextEdits.CoreEditInvalid")
			if a.Version() != v {
				t.Errorf("%s changed the document", name)
			}
		}
		// touching edits are not overlapping
		if _, err := edit(aURI, v, extension.TextChange{Range: rng(0, 0, 0, 2), Text: "x"}, extension.TextChange{Range: rng(0, 2, 0, 3), Text: "y"}); err != nil {
			t.Errorf("edits that touch: %v", err)
		}
		if _, err := a.Undo(); err != nil {
			t.Fatal(err)
		}
		// no edit at all changes nothing
		reply, err := edit(aURI, a.Version())
		if err != nil || reply.Changed {
			t.Errorf("no edits: %+v %v", reply, err)
		}
	})
	t.Run("the limits", func(t *testing.T) {
		v := a.Version()
		many := make([]extension.TextChange, extension.MaxDocumentEdits+1)
		_, err := edit(aURI, v, many...)
		wantRoute(t, err, "ApplyTextEdits.CoreEditTooLarge")
		_, err = edit(aURI, v, extension.TextChange{Range: rng(0, 0, 0, 0), Text: strings.Repeat("x", extension.MaxDocumentEditBytes+1)})
		wantRoute(t, err, "ApplyTextEdits.CoreEditTooLarge")
	})
	t.Run("edit is for the documents of the workspace", func(t *testing.T) {
		one := extension.TextChange{Range: rng(0, 0, 0, 0), Text: "x"}
		_, err := edit("file://"+filepath.Join(c.outside, "other.go"), outside.Version(), one)
		wantRoute(t, err, "Contains.ConfineOutside")
		_, err = edit(c.fileURI("out-dir/other.go"), viaLink.Version(), one)
		wantRoute(t, err, "Contains->statIn.ConfineOutside")
		_, err = edit(c.fileURI("closed.txt"), 1, one)
		wantRoute(t, err, "CoreDocumentNotOpen")
		_, err = edit("emetos-virtual:settings.json", virtual.Version(), one)
		wantRoute(t, err, "Scope.coreDocument.CoreDocumentSchemeUnsupported")
		_, err = edit("nonsense", 1, one)
		wantRoute(t, err, "ParseURI.URIInvalid")
		if outside.Text() != "package other\n" || viaLink.Text() != "package other\n" {
			t.Error("a document outside the folder was edited")
		}
		if reply, err := edit("untitled:1", untitled.Version(), one); err != nil || !reply.Changed || untitled.Text() != "xscratch" {
			t.Errorf("untitled: %+v %v %q", reply, err, untitled.Text())
		}
		ro := docs.add(document.FileURI(filepath.Join(c.dir, "ro.txt")), "ro")
		ro.SetReadOnly(true)
		_, err = edit(c.fileURI("ro.txt"), ro.Version(), one)
		wantRoute(t, err, "ApplyTextEdits.CoreDocumentReadOnly")
	})
	t.Run("save", func(t *testing.T) {
		ev, err := ask(extension.TopicDocumentSave, extension.DocumentURIRequest{URI: aURI})
		saved, _ := extension.EventValue[extension.DocumentSaved](ev)
		if err != nil || saved.URI != aURI || saved.Version != a.Version() || len(docs.saved) != 1 {
			t.Errorf("save %+v %v %v", saved, err, docs.saved)
		}
		docs.saveErr = errors.New("TestSaveRefused", "disk full")
		_, err = ask(extension.TopicDocumentSave, extension.DocumentURIRequest{URI: aURI})
		wantRoute(t, err, "TestSaveRefused")
		docs.saveErr = nil
		_, err = ask(extension.TopicDocumentSave, extension.DocumentURIRequest{URI: "untitled:1"})
		wantRoute(t, err, "Scope.coreDocument.CoreDocumentSchemeUnsupported")
		_, err = ask(extension.TopicDocumentSave, extension.DocumentURIRequest{URI: "file://" + filepath.Join(c.outside, "other.go")})
		wantRoute(t, err, "Contains.ConfineOutside")
		if len(docs.saved) != 2 {
			t.Errorf("saved %v", docs.saved)
		}
	})
	t.Run("a save that never answers times out", func(t *testing.T) {
		docs.saveHangs = true
		o := newOutcome()
		if err := c.events(plain).Request(context.Background(), extension.TopicDocumentSave, extension.DocumentURIRequest{URI: aURI}, o.fn); err != nil {
			t.Fatal(err)
		}
		c.tb.pump()
		if n, _, _ := o.get(); n != 0 {
			t.Fatal("answered early")
		}
		c.tb.clock.advance(DefaultRequestTimeout)
		c.tb.pump()
		_, _, err := o.get()
		wantRoute(t, err, "EventTimeout")
		docs.saveHangs = false
	})
	t.Run("close", func(t *testing.T) {
		dirty := docs.add(document.FileURI(filepath.Join(c.dir, "d.txt")), "d")
		if _, err := dirty.Edit([]document.Edit{{Text: "x"}}, document.EditOptions{}); err != nil {
			t.Fatal(err)
		}
		_, err := ask(extension.TopicDocumentClose, extension.DocumentCloseRequest{URI: c.fileURI("d.txt")})
		wantRoute(t, err, "Scope.coreDocumentClose.func1.CoreDocumentDirty")
		if _, ok := docs.Document(dirty.URI()); !ok {
			t.Fatal("a dirty document was closed without discard")
		}
		ev, err := ask(extension.TopicDocumentClose, extension.DocumentCloseRequest{URI: c.fileURI("d.txt"), Discard: true})
		closed, _ := extension.EventValue[extension.DocumentClosed](ev)
		if err != nil || closed.URI != c.fileURI("d.txt") {
			t.Errorf("close %+v %v", closed, err)
		}
		if _, ok := docs.Document(dirty.URI()); ok {
			t.Error("the document is still open")
		}
		if _, err := ask(extension.TopicDocumentClose, extension.DocumentCloseRequest{URI: "untitled:1", Discard: true}); err != nil {
			t.Errorf("an untitled document: %v", err)
		}
		_, err = ask(extension.TopicDocumentClose, extension.DocumentCloseRequest{URI: "file://" + filepath.Join(c.outside, "other.go")})
		wantRoute(t, err, "Contains.ConfineOutside")
	})
	t.Run("reveal", func(t *testing.T) {
		ev, err := ask(extension.TopicEditorReveal, extension.EditorRevealRequest{URI: aURI, Line: 1, Col: 2})
		got, _ := extension.EventValue[extension.EditorRevealReply](ev)
		if err != nil || got.URI != aURI || len(docs.revealed) != 1 || docs.revealAt[len(docs.revealAt)-1] != (extension.Position{Line: 1, Col: 2}) {
			t.Errorf("reveal %+v %v %v", got, err, docs.revealed)
		}
		_, err = ask(extension.TopicEditorReveal, extension.EditorRevealRequest{URI: "file://" + filepath.Join(c.outside, "other.go")})
		wantRoute(t, err, "Contains.ConfineOutside")
		_, err = ask(extension.TopicEditorReveal, extension.EditorRevealRequest{URI: c.fileURI("closed.txt")})
		wantRoute(t, err, "CoreDocumentNotOpen")
	})
	t.Run("a scope that cannot edit documents", func(t *testing.T) {
		n := newCoreHarness(t, func(r *rig) { r.noCoreDocs["one"] = true })
		for topic, payload := range map[string]any{
			extension.TopicDocumentOpen: extension.DocumentOpenRequest{Path: "a.txt"}, extension.TopicDocumentRead: extension.DocumentURIRequest{URI: "untitled:1"},
			extension.TopicDocumentEdit: extension.DocumentEditRequest{URI: "untitled:1"}, extension.TopicDocumentSave: extension.DocumentURIRequest{URI: "untitled:1"},
			extension.TopicDocumentClose: extension.DocumentCloseRequest{URI: "untitled:1"}, extension.TopicEditorReveal: extension.EditorRevealRequest{URI: "untitled:1"},
		} {
			_, err := n.ask(plain, topic, payload)
			wantRoute(t, err, "CoreDocumentsUnavailable")
		}
	})
	t.Run("a scope with no folder", func(t *testing.T) {
		n := newCoreHarness(t, func(r *rig) { r.folders["one"] = "" })
		n.tb.coreDocs.add(document.FileURI("/somewhere/a.txt"), "x")
		_, err := n.ask(plain, extension.TopicDocumentEdit, extension.DocumentEditRequest{URI: "file:///somewhere/a.txt", Version: 1})
		wantRoute(t, err, "Contains.ConfineNoFolder")
	})
}
