package exthost

import (
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/javascript"
	"github.com/rm4n0s/emetos/internal/core/dap/daptest"
	"github.com/rm4n0s/emetos/internal/core/debug"
)

// The JavaScript debugger (step 25.10, SPEC §10.31 "The debuggers", JavaScript and TypeScript): the children of a session reach the server
// program of their parent (DebugAdapter.ServesChildren), and the attributes js-debug's package.json says are read by the checks and
// the completion of launch.json.

// childScripts are the scripts of the fake server: the parent's session, in which the adapter asks for a child with startDebugging, and
// the session of the child, which another connection to the same server runs.
func childScripts() (parent, child daptest.Script) {
	parent = sessionScript(
		daptest.Request("child", "startDebugging", map[string]any{"request": "attach", "configuration": map[string]any{"name": "child", "__pendingTargetId": "t1"}}),
		daptest.Await("child"),
	)
	child = daptest.Script{
		daptest.Reply("initialize", map[string]any{}),
		daptest.Expect("attach", map[string]any{"type": "fake", "request": "attach", "__pendingTargetId": "t1"}),
		daptest.Event("initialized", nil),
		daptest.ReplyArgs("setExceptionBreakpoints", map[string]any{"filters": []string{}}, map[string]any{}),
		daptest.Respond("attach", nil),
		daptest.Reply("threads", map[string]any{"threads": []map[string]any{{"id": 1, "name": "child"}}}),
		daptest.Reply("disconnect", nil),
		daptest.WaitClose(),
	}
	return parent, child
}

func TestTheChildOfASessionIsServedByTheServerOfItsParent(t *testing.T) {
	tests := []struct {
		name           string
		servesChildren bool
		// wantFactory is how many times the factory is asked for an adapter
		wantFactory int
	}{
		{"a server that serves its children", true, 1},
		{"a server that does not: the factory is asked again, and its adapter is a program that has no child script", false, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newDebugRig(t, dbgOpts{})
			parent, child := childScripts()
			parentFile, childFile := d.scriptFile(parent), d.scriptFile(child)
			d.override = func(s extension.DebugSession) (*extension.DebugAdapter, error) {
				args := []string{"-listen", "tcp", "-announce", "-script", parentFile}
				if s.Parent() != nil {
					// a child that gets its own adapter: a program that serves it the child's script
					args = []string{"-listen", "tcp", "-announce", "-script", childFile}
				} else if tt.servesChildren {
					args = append(args, "-child-script", childFile)
				}
				return &extension.DebugAdapter{Kind: extension.DebugAdapterExecutableServer, Command: daptest.Program(d.t), Args: args, ServesChildren: tt.servesChildren}, nil
			}
			s := d.start(fakeConfig("parent", "exec"))
			d.idle(s)
			waitDebug(t, "the child session", func() bool { return len(d.starter.Debug().Sessions()) == 2 })
			var kid extension.DebugSession
			for _, c := range d.starter.Debug().Sessions() {
				if c.Name() == "child" {
					kid = c
				}
			}
			if kid == nil || kid.Parent() == nil || kid.Parent().ID() != s.ID() {
				t.Fatalf("the child session = %v, want one whose parent is %s", kid, s.ID())
			}
			d.idle(kid)
			d.mu.Lock()
			calls := d.factoryCalls
			d.mu.Unlock()
			if calls != tt.wantFactory {
				t.Errorf("the factory was asked %d times, want %d", calls, tt.wantFactory)
			}
			cs := d.coreSession(kid)
			waitDebug(t, "the child's threads, which its server gave", func() bool { return len(cs.Threads()) > 0 })
			d.stop()
			// the address of the server goes with the session
			d.debug.mu.Lock()
			left := len(d.debug.servers)
			d.debug.mu.Unlock()
			if left != 0 {
				t.Errorf("%d server addresses are kept after the sessions ended", left)
			}
		})
	}
}

// jsSchemas are the declarations of the debug types of the JavaScript extension as the host turns them into the schemas of launch.json.
func jsSchemas(t *testing.T) []debug.DebuggerSchema {
	t.Helper()
	m := javascript.New().Manifest()
	var out []debug.DebuggerSchema
	for _, d := range m.Contributes.Debuggers {
		out = append(out, schemaOf(d, nil))
	}
	if len(out) != 2 {
		t.Fatalf("%d debuggers", len(out))
	}
	return out
}

func TestTheAttributesOfTheNodeDebuggerCheckALaunchFile(t *testing.T) {
	tests := []struct {
		name string
		text string
		// problem is a part of the message of the only problem; empty when there is none
		problem string
	}{
		{"a program", `{"configurations": [{"name": "P", "type": "node", "request": "launch", "program": "${workspaceFolder}/app.js", "skipFiles": ["<node_internals>/**"]}]}`, ""},
		{"the old name", `{"configurations": [{"name": "P", "type": "pwa-node", "request": "launch", "program": "${file}"}]}`, ""},
		{"an npm script", `{"configurations": [{"name": "N", "type": "node", "request": "launch", "runtimeExecutable": "npm", "runtimeArgs": ["run", "debug"], "console": "integratedTerminal"}]}`, ""},
		{"a TypeScript program", `{"configurations": [{"name": "T", "type": "node", "request": "launch", "program": "${workspaceFolder}/src/app.ts", "outFiles": ["${workspaceFolder}/out/**/*.js"], "sourceMaps": true}]}`, ""},
		{"an attach by port", `{"configurations": [{"name": "A", "type": "node", "request": "attach", "port": 9229, "restart": true}]}`, ""},
		{"an attach to a process", `{"configurations": [{"name": "A", "type": "node", "request": "attach", "processId": "${command:PickProcess}"}]}`, ""},
		{"a remote attach", `{"configurations": [{"name": "R", "type": "node", "request": "attach", "address": "10.0.0.2", "port": 9229, "localRoot": "${workspaceFolder}", "remoteRoot": "/app"}]}`, ""},
		{"a console that is none", `{"configurations": [{"name": "P", "type": "node", "request": "launch", "program": "x.js", "console": "bogus"}]}`, "console"},
		{"args that are a number", `{"configurations": [{"name": "P", "type": "node", "request": "launch", "program": "x.js", "args": 7}]}`, "args"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			problems := debug.CheckAttributes(tt.text, jsSchemas(t))
			switch {
			case tt.problem == "" && len(problems) != 0:
				t.Errorf("problems = %+v, want none", problems)
			case tt.problem != "" && (len(problems) != 1 || !strings.Contains(problems[0].Message+problems[0].Path, tt.problem)):
				t.Errorf("problems = %+v, want one about %s", problems, tt.problem)
			}
		})
	}
}

func TestTheSnippetsOfTheNodeDebuggerAreOfferedInALaunchFile(t *testing.T) {
	text := "{\"configurations\": [  ]}"
	items := debug.CompleteLaunch(text, strings.Index(text, "  ]")+1, jsSchemas(t))
	labels := map[string]bool{}
	for _, it := range items {
		if it.Kind == debug.CompleteSnippet {
			labels[it.Label] = true
		}
	}
	for _, want := range []string{"Node.js: Launch Program", "Node.js: Attach", "Node.js: Attach to Process", "Node.js: Launch via npm", "Node.js: Mocha Tests"} {
		if !labels[want] {
			t.Errorf("snippet %q is not offered: %v", want, labels)
		}
	}
}
