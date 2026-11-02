package debug_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/dap/daptest"
	"github.com/rm4n0s/emetos/internal/core/debug"
	"github.com/rm4n0s/errors"
)

// launchFolder writes a launch.json and makes the launches of the service read it.
func launchFolder(t *testing.T, e *env, text string) (string, *debug.Launch) {
	t.Helper()
	folder := t.TempDir()
	writeLaunch(t, folder, text)
	if err := e.svc.Launches().Reload(context.Background(), debug.LaunchSources{Folder: folder}); err != nil {
		t.Fatal(err)
	}
	return folder, e.svc.Launches().Launch(folder)
}

func plain(name string) daptest.Script {
	caps := map[string]any{}
	return join(handshake(caps, noBreakpoints(caps)), ending())
}

func TestStartDebuggingFromTheSelectedConfigurationSubstitutesVariables(t *testing.T) {
	var mu sync.Mutex
	var commands []string
	e := newEnv(t, func(o *debug.Options) {
		o.Variables = func() *debug.VariableContext {
			return &debug.VariableContext{
				ActiveFile: "/w/app/main.go", Line: 7,
				Command: func(_ context.Context, id string, _ any) (any, error) {
					mu.Lock()
					commands = append(commands, id)
					mu.Unlock()
					return "4711", nil
				},
				PromptString: func(_ context.Context, r debug.PromptRequest) (string, bool, error) {
					return "answer-" + r.ID, true, nil
				},
			}
		}
	})
	folder, _ := launchFolder(t, e, `{
  "configurations": [{
    "name": "app", "type": "fake", "request": "launch",
    "program": "${workspaceFolder}/${fileBasename}:${lineNumber}",
    "args": ["${input:who}", "${command:pickProcess}"],
    "linux": {"console": "terminal"},
    "windows": {"console": "none"},
  }],
  "inputs": [{"id": "who", "type": "promptString", "description": "Who"}]
}`)
	e.script("app", plain("app"))
	ok, err := e.svc.StartDebugging(ctxT(t), nil, debug.Target{}, debug.SessionOptions{})
	if err != nil || !ok {
		t.Fatalf("start: %v %v", ok, err)
	}
	sess := e.svc.Model().Sessions(false)[0]
	idle(t, sess)
	cfg := sess.Configuration()
	if cfg.String("program") != folder+"/main.go:7" || cfg.String("console") != "terminal" {
		t.Errorf("resolved configuration = %v", cfg)
	}
	if args, _ := cfg["args"].([]any); len(args) != 2 || args[0] != "answer-who" || args[1] != "4711" {
		t.Errorf("args = %v", cfg["args"])
	}
	if _, has := cfg["linux"]; has {
		t.Errorf("platform attributes are left: %v", cfg)
	}
	// The unresolved configuration is kept, for a restart.
	if un := sess.UnresolvedConfiguration(); un.String("program") != "${workspaceFolder}/${fileBasename}:${lineNumber}" {
		t.Errorf("unresolved = %v", un)
	}
	// The command went through the debugger's mapping.
	mu.Lock()
	got := append([]string(nil), commands...)
	mu.Unlock()
	if len(got) != 1 || got[0] != "ext.pickProcess" {
		t.Errorf("commands = %v", got)
	}
	// What the adapter got.
	if v := e.requests(0, "launch"); len(v) != 1 || !containsJSON(v[0], `"program":"`+folder+`/main.go:7"`) {
		t.Errorf("launch = %s", v)
	}
	if err := e.svc.Stop(ctxT(t), sess, true, false); err != nil {
		t.Fatal(err)
	}
	e.wait(debug.EventSessionEnded, nil)
}

func TestStartDebuggingErrorsAndCancellations(t *testing.T) {
	var opened []string
	prompt := func(ok bool) func(*debug.Options) {
		return func(o *debug.Options) {
			o.Variables = func() *debug.VariableContext {
				return &debug.VariableContext{PromptString: func(context.Context, debug.PromptRequest) (string, bool, error) { return "", ok, nil }}
			}
		}
	}
	tests := []struct {
		name    string
		file    string
		tune    func(*debug.Options)
		target  debug.Target
		started bool
		tag     string
		route   string
		check   func(t *testing.T, e *env)
	}{
		{name: "a configuration that is not in the file", file: `{"configurations": []}`, target: debug.Target{Name: "nope"}, tag: "DebugConfigurationMissing", route: "startDebugging.DebugConfigurationMissing"},
		{name: "no type", file: `{"configurations": [{"name": "a", "request": "launch"}]}`, target: debug.Target{Name: "a"}, tag: "DebugTypeMissing", route: "createSession.DebugTypeMissing"},
		{name: "no request", file: `{"configurations": [{"name": "a", "type": "fake"}]}`, target: debug.Target{Name: "a"}, tag: "DebugRequestMissing", route: "createSession.DebugRequestMissing"},
		{name: "a request that is none", file: `{"configurations": [{"name": "a", "type": "fake", "request": "run"}]}`, target: debug.Target{Name: "a"}, tag: "DebugRequestUnsupported", route: "createSession.DebugRequestUnsupported"},
		{name: "a type nobody debugs", file: `{"configurations": [{"name": "a", "type": "cobol", "request": "launch"}]}`, target: debug.Target{Name: "a"}, tag: "DebugTypeUnsupported", route: "createSession.DebugTypeUnsupported"},
		{name: "a variable that cannot be resolved", file: `{"configurations": [{"name": "a", "type": "fake", "request": "launch", "program": "${file}"}]}`, target: debug.Target{Name: "a"}, tag: "VariableNoEditor", route: "ResolveConfig"},
		{name: "an input the user cancels", file: `{"configurations": [{"name": "a", "type": "fake", "request": "launch", "program": "${input:x}"}], "inputs": [{"id": "x", "type": "promptString", "description": "d"}]}`, tune: prompt(false), target: debug.Target{Name: "a"}},
		{name: "an input that is not defined", file: `{"configurations": [{"name": "a", "type": "fake", "request": "launch", "program": "${input:y}"}], "inputs": []}`, tune: prompt(true), target: debug.Target{Name: "a"}, tag: "InputSectionMissing", route: "input.InputSectionMissing"},
		{name: "a compound with nothing in it", file: `{"compounds": [{"name": "c", "configurations": []}]}`, target: debug.Target{Name: "c"}, tag: "DebugCompoundEmpty"},
		{name: "a provider cancels", file: `{"configurations": [{"name": "a", "type": "fake", "request": "launch"}]}`, target: debug.Target{Name: "a"},
			tune: func(o *debug.Options) {
				o.ResolveConfiguration = func(context.Context, string, string, debug.Config) (debug.Config, error) { return nil, nil }
			}},
		{name: "a provider asks for launch.json", file: `{"configurations": [{"name": "a", "type": "fake", "request": "launch"}]}`, target: debug.Target{Name: "a"},
			tune: func(o *debug.Options) {
				o.ResolveConfiguration = func(context.Context, string, string, debug.Config) (debug.Config, error) {
					return nil, debug.ErrOpenLaunchFile
				}
				o.OpenLaunchFile = func(_ context.Context, l *debug.Launch, typ string) { opened = append(opened, typ+"@"+l.Name) }
			},
			check: func(t *testing.T, e *env) {
				if len(opened) != 1 || !strings.HasPrefix(opened[0], "fake@") {
					t.Errorf("opened = %v", opened)
				}
			}},
		{name: "a provider fails", file: `{"configurations": [{"name": "a", "type": "fake", "request": "launch"}]}`, target: debug.Target{Name: "a"},
			tune: func(o *debug.Options) {
				o.ResolveConfiguration = func(context.Context, string, string, debug.Config) (debug.Config, error) {
					return nil, errors.New("ProviderBroke", "the provider broke")
				}
			}, tag: "ProviderBroke"},
		{name: "the task before fails", file: `{"configurations": [{"name": "a", "type": "fake", "request": "launch", "preLaunchTask": "build"}]}`, target: debug.Target{Name: "a"},
			tune: func(o *debug.Options) {
				o.RunTask = func(context.Context, string, debug.TaskRef) (debug.TaskResult, error) { return debug.TaskFailure, nil }
			}},
		{name: "the task before cannot run", file: `{"configurations": [{"name": "a", "type": "fake", "request": "launch", "preLaunchTask": "build"}]}`, target: debug.Target{Name: "a"},
			tune: func(o *debug.Options) {
				o.RunTask = func(context.Context, string, debug.TaskRef) (debug.TaskResult, error) {
					return debug.TaskSuccess, errors.New("TaskGone", "no such task")
				}
			}, tag: "TaskGone"},
		{name: "the second provider call cancels", file: `{"configurations": [{"name": "a", "type": "fake", "request": "launch"}]}`, target: debug.Target{Name: "a"},
			tune: func(o *debug.Options) {
				o.ResolveSubstituted = func(context.Context, string, string, debug.Config) (debug.Config, error) { return nil, nil }
			}},
		{name: "the user does not want a second instance", file: `{"configurations": [{"name": "a", "type": "fake", "request": "launch"}]}`, target: debug.Target{Name: "a"},
			tune: func(o *debug.Options) {
				o.ConfirmConcurrent = func(context.Context, string) bool { return false }
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opened = nil
			e := newEnv(t, tt.tune)
			_, launch := launchFolder(t, e, tt.file)
			e.script("a", plain("a"))
			opts := debug.SessionOptions{}
			if tt.name == "the user does not want a second instance" {
				// A session of the same configuration already runs.
				ok, err := e.svc.StartDebugging(ctxT(t), launch, debug.Target{Name: "a"}, debug.SessionOptions{})
				if err != nil || !ok {
					t.Fatalf("first: %v %v", ok, err)
				}
				first := e.svc.Model().Sessions(false)[0]
				idle(t, first)
				opts.StartedByUser = true
				defer func() {
					if err := e.svc.Stop(ctxT(t), first, true, false); err != nil {
						t.Error(err)
					}
				}()
			}
			ok, err := e.svc.StartDebugging(ctxT(t), launch, tt.target, opts)
			if tt.tag == "" {
				if err != nil || ok {
					t.Fatalf("StartDebugging = %v, %v; want a silent no", ok, err)
				}
				if tt.name != "the user does not want a second instance" && len(e.svc.Model().Sessions(true)) != 0 {
					t.Errorf("a session was made")
				}
				if tt.check != nil {
					tt.check(t, e)
				}
				return
			}
			if ok {
				t.Fatal("started")
			}
			appErr, isApp := errors.FromError(err)
			if !isApp {
				t.Fatalf("err = %v, want *errors.Error", err)
			}
			if appErr.Tag != tt.tag && !appErr.HasRoute(tt.tag) {
				t.Errorf("tag = %s, route = %s; want %s", appErr.Tag, appErr.Route(), tt.tag)
			}
			if tt.route != "" && !appErr.HasRoute(tt.route) {
				t.Errorf("route = %s, want it to contain %s", appErr.Route(), tt.route)
			}
			if e.svc.State() != debug.Inactive {
				t.Errorf("state = %v", e.svc.State())
			}
		})
	}
}

func TestStartDebuggingWithoutALaunchFile(t *testing.T) {
	e := newEnv(t, nil)
	// No launch at all: a configuration that does not exist.
	_, err := e.svc.StartDebugging(ctxT(t), nil, debug.Target{Name: "x"}, debug.SessionOptions{})
	_ = wantRoute(t, err, "startDebugging.DebugLaunchMissing")
}

func TestStartDebuggingAsksTheDebuggerOfTheEditor(t *testing.T) {
	var asked int
	e := newEnv(t, func(o *debug.Options) {
		o.Guess = func(context.Context) (debug.GuessResult, bool) {
			asked++
			return debug.GuessResult{Type: "fake", Config: debug.Config{"name": "Current File", "request": "launch", "program": "/w/x"}}, true
		}
	})
	e.script("Current File", plain("x"))
	// F5 with no launch.json: the configuration is empty and the type comes from the debugger of the active file.
	ok, err := e.svc.StartDebugging(ctxT(t), nil, debug.Target{Config: debug.Config{}}, debug.SessionOptions{})
	if err != nil || !ok || asked != 1 {
		t.Fatalf("start: %v %v asked %d", ok, err, asked)
	}
	sess := e.svc.Model().Sessions(false)[0]
	idle(t, sess)
	if c := sess.Configuration(); c.Type() != "" && c.String("program") != "/w/x" {
		t.Errorf("configuration = %v", c)
	}
	if err := e.svc.Stop(ctxT(t), sess, true, false); err != nil {
		t.Fatal(err)
	}
	e.wait(debug.EventSessionEnded, nil)
}

func TestCompounds(t *testing.T) {
	const file = `{
  "configurations": [
    {"name": "A", "type": "fake", "request": "launch"},
    {"name": "B", "type": "fake", "request": "launch"}
  ],
  "compounds": [
    {"name": "Both", "stopAll": true, "preLaunchTask": "prep", "configurations": ["A", "B"]},
    {"name": "Loose", "configurations": ["A", "B", "Loose"]},
    {"name": "Missing", "configurations": ["A", "Z"]},
    {"name": "Folder", "configurations": [{"name": "A", "folder": "nowhere"}]}
  ]
}`
	t.Run("stopAll ends the other sessions", func(t *testing.T) {
		var mu sync.Mutex
		var tasks []string
		e := newEnv(t, func(o *debug.Options) {
			o.RunTask = func(_ context.Context, _ string, task debug.TaskRef) (debug.TaskResult, error) {
				mu.Lock()
				tasks = append(tasks, task.String())
				mu.Unlock()
				return debug.TaskSuccess, nil
			}
		})
		_, launch := launchFolder(t, e, file)
		e.script("A", plain("A"))
		e.script("B", plain("B"))
		ok, err := e.svc.StartDebugging(ctxT(t), launch, debug.Target{Name: "Both"}, debug.SessionOptions{})
		if err != nil || !ok {
			t.Fatalf("start: %v %v", ok, err)
		}
		sessions := e.svc.Model().Sessions(false)
		if len(sessions) != 2 {
			t.Fatalf("sessions = %d", len(sessions))
		}
		for _, s := range sessions {
			idle(t, s)
		}
		mu.Lock()
		if len(tasks) != 1 || tasks[0] != "prep" {
			t.Errorf("tasks = %v", tasks)
		}
		mu.Unlock()
		// Stopping one stops both.
		if err := e.svc.Stop(ctxT(t), sessions[0], true, false); err != nil {
			t.Fatal(err)
		}
		e.wait(debug.EventSessionEnded, nil)
		e.wait(debug.EventSessionEnded, nil)
		waitUntil(t, "both sessions to end", func() bool { return len(e.svc.Model().Sessions(false)) == 0 })
	})
	t.Run("without stopAll the others go on", func(t *testing.T) {
		e := newEnv(t, nil)
		_, launch := launchFolder(t, e, file)
		e.script("A", plain("A"))
		e.script("B", plain("B"))
		ok, err := e.svc.StartDebugging(ctxT(t), launch, debug.Target{Name: "Loose"}, debug.SessionOptions{})
		if err != nil || !ok {
			t.Fatalf("start: %v %v", ok, err)
		}
		sessions := e.svc.Model().Sessions(false)
		if len(sessions) != 2 {
			t.Fatalf("sessions = %d (a compound does not start itself)", len(sessions))
		}
		for _, s := range sessions {
			idle(t, s)
		}
		if err := e.svc.Stop(ctxT(t), sessions[0], true, false); err != nil {
			t.Fatal(err)
		}
		e.wait(debug.EventSessionEnded, nil)
		if sessions[1].State() == debug.Inactive {
			t.Error("the other session ended")
		}
		if err := e.svc.Stop(ctxT(t), sessions[1], true, false); err != nil {
			t.Fatal(err)
		}
		e.wait(debug.EventSessionEnded, nil)
	})
	t.Run("one that fails makes the compound fail", func(t *testing.T) {
		e := newEnv(t, nil)
		_, launch := launchFolder(t, e, file)
		e.script("A", plain("A"))
		ok, err := e.svc.StartDebugging(ctxT(t), launch, debug.Target{Name: "Missing"}, debug.SessionOptions{})
		if ok {
			t.Fatal("started")
		}
		_ = wantRoute(t, err, "startCompound.DebugCompoundConfigurationMissing")
	})
	t.Run("a folder that is not there", func(t *testing.T) {
		e := newEnv(t, nil)
		_, launch := launchFolder(t, e, file)
		_, err := e.svc.StartDebugging(ctxT(t), launch, debug.Target{Name: "Folder"}, debug.SessionOptions{})
		_ = wantRoute(t, err, "startCompound.DebugCompoundFolderMissing")
	})
	t.Run("a name in two other launches", func(t *testing.T) {
		e := newEnv(t, nil)
		folder := t.TempDir()
		writeLaunch(t, folder, `{"configurations": [{"name": "A", "type": "fake", "request": "launch"}]}`)
		src := debug.LaunchSources{
			Folder:           folder,
			UserSetting:      map[string]any{"configurations": []any{map[string]any{"name": "A", "type": "fake", "request": "launch"}}},
			WorkspaceSetting: map[string]any{"compounds": []any{map[string]any{"name": "C", "configurations": []any{"A"}}}},
		}
		if err := e.svc.Launches().Reload(context.Background(), src); err != nil {
			t.Fatal(err)
		}
		var ws *debug.Launch
		for _, l := range e.svc.Launches().Launches() {
			if l.Kind == debug.LaunchWorkspace {
				ws = l
			}
		}
		if ws == nil {
			t.Fatal("no workspace launch")
		}
		_, err := e.svc.StartDebugging(ctxT(t), ws, debug.Target{Name: "C"}, debug.SessionOptions{})
		_ = wantRoute(t, err, "startCompound.DebugCompoundConfigurationAmbiguous")
	})
	t.Run("the launch of the compound wins when it has the name too", func(t *testing.T) {
		e := newEnv(t, nil)
		folder := t.TempDir()
		writeLaunch(t, folder, `{"configurations": [{"name": "A", "type": "fake", "request": "launch"}], "compounds": [{"name": "C", "configurations": ["A"]}]}`)
		src := debug.LaunchSources{Folder: folder, UserSetting: map[string]any{"configurations": []any{map[string]any{"name": "A", "type": "fake", "request": "launch"}}}}
		if err := e.svc.Launches().Reload(context.Background(), src); err != nil {
			t.Fatal(err)
		}
		e.script("A", plain("A"))
		ok, err := e.svc.StartDebugging(ctxT(t), e.svc.Launches().Launch(folder), debug.Target{Name: "C"}, debug.SessionOptions{})
		if err != nil || !ok {
			t.Fatalf("start: %v %v", ok, err)
		}
		sess := e.svc.Model().Sessions(false)[0]
		idle(t, sess)
		if err := e.svc.Stop(ctxT(t), sess, true, false); err != nil {
			t.Fatal(err)
		}
		e.wait(debug.EventSessionEnded, nil)
	})
}
