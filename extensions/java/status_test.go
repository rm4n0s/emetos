package java

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// The notifications of jdtls (language/status, language/progressReport, language/actionableNotification, language/eventNotification)
// over the fake API.

// javaFile puts a Java file in front, so that the status item is shown.
func (r *rig) javaFile() { r.editor("A.java", "java", "class A {}\n") }

func TestTheStatusItemFollowsLanguageStatusAndProgress(t *testing.T) {
	r := newRig(t, rigOptions{javaVersion: "21"})
	r.javaFile()
	if _, err := r.run(); err != nil {
		t.Fatal(err)
	}
	if got := r.item().Text; got != "$(check) Java" {
		t.Errorf("item after the start = %q", got)
	}
	steps := []struct {
		name   string
		method string
		params string
		text   string
		tip    string
	}{
		{"starting", statusMethod, `{"type":"Starting","message":"Init..."}`, "$(sync~spin) Java", "jdtls is working\nInit..."},
		{"a message", statusMethod, `{"type":"Message","message":"Importing projects"}`, "$(sync~spin) Java", "Importing projects"},
		{"a task", progressMethod, `{"id":"1","task":"Building","subTask":"app","status":"","workDone":1,"totalWork":10,"complete":false}`, "$(sync~spin) Java", "Building: app"},
		{"a second task", progressMethod, `{"id":"2","task":"Indexing","subTask":"","complete":false}`, "$(sync~spin) Java", "Indexing"},
		{"the second ends", progressMethod, `{"id":"2","task":"Indexing","complete":true}`, "$(sync~spin) Java", "Building: app"},
		{"started", statusMethod, `{"type":"Started","message":"Ready"}`, "$(sync~spin) Java", "Building: app"},
		{"the first ends", progressMethod, `{"id":"1","task":"Building","complete":true}`, "$(sync~spin) Java", "Ready"},
		{"ready", statusMethod, `{"type":"ServiceReady","message":"ServiceReady"}`, "$(check) Java", "jdtls is running"},
		{"a project with problems", statusMethod, `{"type":"ProjectStatus","message":"WARNING"}`, "$(warning) Java", "A project has problems with its configuration."},
		{"the problems are gone", statusMethod, `{"type":"ProjectStatus","message":"OK"}`, "$(check) Java", "jdtls is running"},
		{"an error", statusMethod, `{"type":"Error","message":"Out of memory"}`, "$(error) Java", "Out of memory"},
	}
	for _, s := range steps {
		var params any
		if err := json.Unmarshal([]byte(s.params), &params); err != nil {
			t.Fatal(err)
		}
		r.api.ServerNotify(ServerID, s.method, params)
		r.settle()
		if got := r.item().Text; got != s.text {
			t.Errorf("%s: item = %q, want %q", s.name, got, s.text)
		}
		if got := r.item().Tooltip; !strings.Contains(got, s.tip) {
			t.Errorf("%s: tooltip = %q, want it to say %q", s.name, got, s.tip)
		}
	}
}

func TestARestartForgetsWhatTheLastStartTold(t *testing.T) {
	r := newRig(t, rigOptions{javaVersion: "21"})
	r.javaFile()
	if _, err := r.run(); err != nil {
		t.Fatal(err)
	}
	r.api.ServerNotify(ServerID, statusMethod, map[string]any{"type": "ServiceReady"})
	r.api.ServerNotify(ServerID, progressMethod, map[string]any{"id": "7", "task": "Building"})
	r.api.SetServerStatus(ServerID, extension.ServerStatus{State: extension.ServerStopped})
	r.settle()
	if r.ext.status.ready || len(r.ext.status.tasks) != 0 || len(r.ext.status.order) != 0 {
		t.Errorf("state = %+v", r.ext.status)
	}
	if _, err := r.run(); err != nil {
		t.Fatal(err)
	}
	if got := r.item().Text; got != "$(check) Java" {
		t.Errorf("item after the second start = %q", got)
	}
}

func TestNotificationsThatCannotBeReadAreLoggedNotShown(t *testing.T) {
	var buf logged
	r := newRigWith(t, rigOptions{javaVersion: "21"}, func(a *fakeapi.API) { a.SetLogger(newLogger(&buf)) })
	if _, err := r.run(); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{statusMethod, progressMethod, actionableMethod} {
		r.api.ServerNotify(ServerID, method, "not an object")
	}
	r.api.ServerNotify(ServerID, eventNotificationMethod, "not an object")
	r.api.ServerNotify(ServerID, actionableMethod, map[string]any{"severity": 3})
	r.settle()
	if len(r.notes()) != 0 {
		t.Errorf("notes = %v", r.notes())
	}
	for _, want := range []string{"the status of jdtls cannot be read", "the progress of jdtls cannot be read", "a question of jdtls cannot be read"} {
		if !strings.Contains(buf.text(), want) {
			t.Errorf("the log lacks %q:\n%s", want, buf.text())
		}
	}
}

func TestEventsOfTheProjectsAreLoggedAtDebugLevel(t *testing.T) {
	var buf logged
	r := newRigWith(t, rigOptions{javaVersion: "21"}, func(a *fakeapi.API) { a.SetLogger(newLogger(&buf)) })
	r.api.ServerNotify(ServerID, eventNotificationMethod, map[string]any{"eventType": 200, "data": []string{"file:///a"}})
	r.settle()
	if !strings.Contains(buf.text(), "eventType=200") || strings.Contains(buf.text(), "level=WARN") || len(r.notes()) != 0 {
		t.Errorf("log %q, notes %v", buf.text(), r.notes())
	}
}

// buildChanged is the question jdtls asks when a build file changed.
func buildChanged(uri string) map[string]any {
	id := map[string]any{"uri": uri}
	return map[string]any{
		"severity": 3, "message": "A build file was modified. Do you want to synchronize the Java classpath/configuration?",
		"commands": []any{
			map[string]any{"title": "Yes", "command": "java.projectConfiguration.update", "arguments": []any{id}},
			map[string]any{"title": "Always", "command": "java.projectConfiguration.status", "arguments": []any{id, "automatic"}},
			map[string]any{"title": "Never", "command": "java.projectConfiguration.status", "arguments": []any{id, "disabled"}},
		},
	}
}

func TestTheQuestionAboutAChangedBuildFileHasThreeAnswers(t *testing.T) {
	pom := "file:///work/pom.xml"
	tests := []struct {
		button      string
		wantRequest bool
		wantSetting string
	}{
		{"Yes", true, ""},
		{"Always", true, "automatic"},
		{"Never", false, "disabled"},
	}
	for _, tt := range tests {
		t.Run(tt.button, func(t *testing.T) {
			r := newRig(t, rigOptions{javaVersion: "21"})
			if _, err := r.run(); err != nil {
				t.Fatal(err)
			}
			r.api.ServerNotify(ServerID, actionableMethod, buildChanged(pom))
			r.settle()
			notes := r.api.Notes()
			if len(notes) != 1 || notes[0].Severity != "info" || !strings.HasPrefix(notes[0].Message, "A build file was modified.") ||
				strings.Join(notes[0].Actions, ",") != "Yes,Always,Never" {
				t.Fatalf("notes = %+v", notes)
			}
			r.api.RunNoteAction(0, tt.button)
			r.settle()
			got := r.requests(projectConfigUpdateMethod)
			if tt.wantRequest != (len(got) == 1) || (len(got) == 1 && got[0] != `{"uri":"`+pom+`"}`) {
				t.Errorf("update requests = %v", got)
			}
			updates := r.api.SettingUpdates()
			switch {
			case tt.wantSetting == "" && len(updates) != 0:
				t.Errorf("updates = %+v", updates)
			case tt.wantSetting != "" && (len(updates) != 1 || updates[0].Key != UpdateBuildConfigurationSetting || updates[0].Value != tt.wantSetting):
				t.Errorf("updates = %+v, want %s = %s", updates, UpdateBuildConfigurationSetting, tt.wantSetting)
			}
		})
	}
	t.Run("a client of a hosted workspace changes nothing", func(t *testing.T) {
		r := newRig(t, rigOptions{javaVersion: "21"})
		if _, err := r.run(); err != nil {
			t.Fatal(err)
		}
		r.api.ServerNotify(ServerID, actionableMethod, buildChanged(pom))
		r.settle()
		r.api.RunAs(extension.Origin(3), func() { r.api.RunNoteAction(0, "Never") })
		r.settle()
		if len(r.api.SettingUpdates()) != 0 || !strings.Contains(r.note(), "setting of the host") {
			t.Errorf("updates %+v, note %q", r.api.SettingUpdates(), r.note())
		}
	})
}

func TestTheStatusOfTheBuildConfigurationIsReadAsAnAnswerOrANumber(t *testing.T) {
	tests := []struct {
		args string
		want string
	}{
		{`[{"uri":"file:///a"},"automatic"]`, "automatic"},
		{`[{"uri":"file:///a"},"DISABLED"]`, "disabled"},
		{`["file:///a",2]`, "automatic"},
		{`["file:///a",0]`, "disabled"},
		{`["file:///a",1]`, "interactive"},
		{`["file:///a"]`, ""},
		{`[]`, ""},
		{`["file:///a",7]`, "7"},
	}
	for _, tt := range tests {
		var raw []json.RawMessage
		if err := json.Unmarshal([]byte(tt.args), &raw); err != nil {
			t.Fatal(err)
		}
		if got := featureStatus(raw); got != tt.want {
			t.Errorf("featureStatus(%s) = %q, want %q", tt.args, got, tt.want)
		}
	}
	for _, tt := range []struct{ args, want string }{{`[{"uri":"file:///a"}]`, "file:///a"}, {`["file:///b"]`, "file:///b"}, {`[1]`, ""}, {`[]`, ""}} {
		var raw []json.RawMessage
		if err := json.Unmarshal([]byte(tt.args), &raw); err != nil {
			t.Fatal(err)
		}
		if got := documentURI(raw); got != tt.want {
			t.Errorf("documentURI(%s) = %q, want %q", tt.args, got, tt.want)
		}
	}
}

func TestTheSeveritiesOfTheQuestionsAreTheKindsOfNotification(t *testing.T) {
	tests := []struct {
		severity int
		want     string
	}{{1, "error"}, {2, "warn"}, {3, "info"}}
	for _, tt := range tests {
		r := newRig(t, rigOptions{javaVersion: "21"})
		r.api.ServerNotify(ServerID, actionableMethod, map[string]any{"severity": tt.severity, "message": "Something happened"})
		r.settle()
		notes := r.api.Notes()
		if len(notes) != 1 || notes[0].Severity != tt.want || notes[0].Message != "Something happened" || len(notes[0].Actions) != 0 {
			t.Errorf("severity %d: notes = %+v", tt.severity, notes)
		}
	}
	// a log message (4) is not shown
	r := newRig(t, rigOptions{javaVersion: "21"})
	r.api.ServerNotify(ServerID, actionableMethod, map[string]any{"severity": 4, "message": "details"})
	r.settle()
	if len(r.notes()) != 0 {
		t.Errorf("notes = %v", r.notes())
	}
}

func TestACommandTheServerOffersGoesBackToTheServerOrIsSaidToBeUnknown(t *testing.T) {
	offer := func(command string, args ...any) map[string]any {
		return map[string]any{"severity": 2, "message": "Do it?", "commands": []any{map[string]any{"title": "Do it", "command": command, "arguments": args}}}
	}
	t.Run("a command the server executes", func(t *testing.T) {
		r := newRig(t, rigOptions{javaVersion: "21"})
		if _, err := r.run(); err != nil {
			t.Fatal(err)
		}
		r.api.SetServerCommands(ServerID, []string{"java.project.upgradeGradle"})
		r.api.ServerNotify(ServerID, actionableMethod, offer("java.project.upgradeGradle", "file:///work", "8.5"))
		r.settle()
		r.api.RunNoteAction(0, "Do it")
		r.settle()
		calls := r.api.ServerCalls()
		if len(calls) != 1 || calls[0].Command != "java.project.upgradeGradle" || len(calls[0].Args) != 2 {
			t.Errorf("calls = %+v", calls)
		}
	})
	t.Run("the server's command fails", func(t *testing.T) {
		r := newRig(t, rigOptions{javaVersion: "21"})
		if _, err := r.run(); err != nil {
			t.Fatal(err)
		}
		r.api.SetServerCommands(ServerID, []string{"java.project.upgradeGradle"})
		r.api.OnServerCommand(func(fakeapi.ServerCall) error { return errors.New("TestBoom", "boom") })
		r.api.ServerNotify(ServerID, actionableMethod, offer("java.project.upgradeGradle"))
		r.settle()
		r.api.RunNoteAction(0, "Do it")
		r.settle()
		if !strings.Contains(r.note(), "The language server failed: boom") {
			t.Errorf("note = %q", r.note())
		}
	})
	t.Run("a command of the client that Emetos has not", func(t *testing.T) {
		r := newRig(t, rigOptions{javaVersion: "21"})
		if _, err := r.run(); err != nil {
			t.Fatal(err)
		}
		r.api.SetServerCommands(ServerID, []string{"java.project.upgradeGradle"})
		r.api.ServerNotify(ServerID, actionableMethod, offer("java.action.overrideMethodsPrompt"))
		r.settle()
		r.api.RunNoteAction(0, "Do it")
		r.settle()
		if !strings.Contains(r.note(), "Emetos cannot run java.action.overrideMethodsPrompt") || len(r.api.ServerCalls()) != 0 {
			t.Errorf("note = %q, calls %+v", r.note(), r.api.ServerCalls())
		}
	})
	t.Run("a button with no title or command is left out", func(t *testing.T) {
		r := newRig(t, rigOptions{javaVersion: "21"})
		r.api.ServerNotify(ServerID, actionableMethod, map[string]any{"severity": 2, "message": "m", "commands": []any{
			map[string]any{"title": "", "command": "x"}, map[string]any{"title": "Y", "command": ""}, map[string]any{"title": "Z", "command": "java.projectConfiguration.update"}}})
		r.settle()
		if n := r.api.Notes(); len(n) != 1 || strings.Join(n[0].Actions, ",") != "Z" {
			t.Errorf("notes = %+v", n)
		}
	})
	t.Run("an unknown status", func(t *testing.T) {
		r := newRig(t, rigOptions{javaVersion: "21"})
		r.api.ServerNotify(ServerID, actionableMethod, map[string]any{"severity": 3, "message": "m", "commands": []any{
			map[string]any{"title": "S", "command": "java.projectConfiguration.status", "arguments": []any{map[string]any{"uri": "file:///a"}, "sometimes"}}}})
		r.settle()
		r.api.RunNoteAction(0, "S")
		r.settle()
		if len(r.api.SettingUpdates()) != 0 {
			t.Errorf("updates = %+v", r.api.SettingUpdates())
		}
	})
}

func TestNoWarningIsLoggedInNormalUse(t *testing.T) {
	var buf logged
	r := newRigWith(t, rigOptions{javaVersion: "25.0.4.1"}, func(a *fakeapi.API) { a.SetLogger(newLogger(&buf)) })
	r.javaFile()
	if _, err := r.run(); err != nil {
		t.Fatal(err)
	}
	r.answer(map[string]string{buildWorkspaceMethod: "1", classFileContentsMethod: `"class X {}"`})
	r.api.ServerNotify(ServerID, statusMethod, map[string]any{"type": "ServiceReady"})
	r.api.ServerNotify(ServerID, progressMethod, map[string]any{"id": "1", "task": "Building"})
	r.api.ServerNotify(ServerID, progressMethod, map[string]any{"id": "1", "task": "Building", "complete": true})
	r.api.ServerNotify(ServerID, actionableMethod, buildChanged("file:///work/pom.xml"))
	r.settle()
	for _, id := range []string{RestartCommand, UpdateProjectConfigurationCommand} {
		if err := r.api.Run(id, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.api.Run(CompileWorkspaceCommand, true); err != nil {
		t.Fatal(err)
	}
	r.settle()
	if _, err := r.api.ContentText(t.Context(), extension.ContentURI(ContentScheme, classPath)); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.text(), "level=WARN") || strings.Contains(buf.text(), "level=ERROR") {
		t.Errorf("the log has a warning or an error:\n%s", buf.text())
	}
}
