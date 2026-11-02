//go:build emetos_jsdebug

package exthost

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/javascript"
	"github.com/rm4n0s/emetos/internal/core/dap"
	"github.com/rm4n0s/emetos/internal/core/debug"
)

// The host with the real JavaScript debugger (tag emetos_jsdebug, D43; step 25.10): the JavaScript extension, the debug host and js-debug
// from /var/tmp/emetos-tools/js-debug (EMETOS_TOOLS names another folder), run by the system's node. A program is started, and the
// session of the launch and the session of the program, which js-debug asks for with startDebugging, are two sessions of the one server.

func TestTheHostRunsAProgramThroughJsDebugWithItsChildSession(t *testing.T) {
	tools := os.Getenv("EMETOS_TOOLS")
	if tools == "" {
		tools = "/var/tmp/emetos-tools"
	}
	adapter := filepath.Join(tools, "js-debug")
	if _, err := os.Stat(filepath.Join(adapter, "js-debug", "src", "dapDebugServer.js")); err != nil {
		t.Skip("js-debug is not in " + adapter + ": scripts/fetch-tools.sh js-debug")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed")
	}
	// the program runs in a copy, with a home folder and temporary folder of the test
	work := t.TempDir()
	copyFolder(t, filepath.Join("..", "..", "..", "extensions", "javascript", "testdata", "jsdebug"), work)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("TMPDIR", home)

	d := newJsRig(t, work, adapter)
	file := filepath.Join(work, "orders.ts")
	if err := d.starter.Debug().AddBreakpoints(context.Background(), []extension.DebugBreakpoint{{URI: "file://" + file, Position: extension.Position{Line: 9}}}); err != nil {
		t.Fatal(err)
	}
	cfg := extension.DebugConfiguration{"name": "Launch Orders", "type": "node", "request": "launch", "program": filepath.Join(work, "orders.js"),
		"outFiles": []any{work + "/**/*.js"}}
	ok, err := d.starter.Debug().StartDebugging(context.Background(), work, extension.DebugTarget{Config: cfg}, extension.DebugStartOptions{})
	if err != nil || !ok {
		t.Fatalf("StartDebugging = %v, %v", ok, err)
	}
	// two sessions: the launch, and the program under it, which stops at the breakpoint of the TypeScript file
	var child *debug.Session
	var top *debug.StackFrame
	waitDebug(t, "the program's session to stop at the breakpoint, with its call stack", func() bool {
		for _, s := range d.starter.Debug().Sessions() {
			if s.Parent() == nil {
				continue
			}
			cs := d.coreSession(s)
			for _, th := range cs.Threads() {
				if th.Stopped() && th.TopStackFrame() != nil {
					child, top = cs, th.TopStackFrame()
					return true
				}
			}
		}
		return false
	})
	parent := child.Parent()
	if parent == nil || parent.Name() != "Launch Orders" || parent.Type() != "pwa-node" || child.Type() != "pwa-node" {
		t.Errorf("sessions: child %s %s of %v", child.Name(), child.Type(), parent)
	}
	if top == nil || !strings.HasSuffix(top.Name, "discount") || filepath.Base(top.Source.Path) != "orders.ts" || top.Range.Line != 10 {
		t.Fatalf("top frame = %+v", top)
	}
	d.stop()
}

// newJsRig is the debug rig with the real JavaScript extension: a starter, the pack, a folder and the setting that says where js-debug is.
func newJsRig(t *testing.T, folder, adapter string) *debugRig {
	t.Helper()
	d := &debugRig{t: t, scripts: nil, servers: nil}
	starter, _ := factory(manifest("starter"), func(f *fake) {
		f.activate = func(_ context.Context, api extension.API) error { d.starter = api; return nil }
	})
	d.r = newRig(t, starter, javascript.New)
	d.r.folders["w"] = folder
	d.r.debugTune = func(o *debug.Options) {
		if os.Getenv("JSDEBUG_TRACE") == "" {
			return
		}
		o.Trace = func(s *debug.Session, m dap.TraceMessage) {
			body := string(m.Body)
			if len(body) > 300 {
				body = body[:300]
			}
			t.Logf("%s %v %s", s.Name(), m.Direction, body)
		}
	}
	d.tab = d.r.newTab("w")
	d.changeSetting("debug.javascript.adapterPath", adapter)
	d.scope.Start()
	return d
}

// copyFolder copies the files of a folder (no sub-folders).
func copyFolder(t *testing.T, from, to string) {
	t.Helper()
	list, err := os.ReadDir(from)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range list {
		if e.IsDir() {
			continue
		}
		in, err := os.Open(filepath.Join(from, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		out, err := os.Create(filepath.Join(to, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		_, cerr := io.Copy(out, in)
		if cerr == nil {
			cerr = out.Close()
		} else {
			_ = out.Close()
		}
		if err := in.Close(); err != nil || cerr != nil {
			t.Fatal(err, cerr)
		}
	}
}
