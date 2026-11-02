package exthost

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/dap/daptest"
	"github.com/rm4n0s/emetos/internal/core/debug"
)

// The rig of the debug host's tests (step 25.3): a tab with two extensions. "starter" is active from the start and is where the tests
// call the Debug API; "dbg" contributes the debug type "fake" and is activated by it. The adapters are daptest's fake adapter, as a
// program (executable and executable-server) and in process (server and pipe).

const debugWait = 10 * time.Second

// waitDebug polls a condition for a while.
func waitDebug(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(debugWait)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// sessionScript is the fake adapter's side of a session of the type fake: the start, whatever the test adds while it runs, and the end.
func sessionScript(extra ...daptest.Step) daptest.Script {
	s := daptest.Script{
		daptest.Reply("initialize", map[string]any{}),
		daptest.Expect("launch", map[string]any{"type": "fake", "request": "launch"}),
		daptest.Event("initialized", nil),
		daptest.ReplyArgs("setExceptionBreakpoints", map[string]any{"filters": []string{}}, map[string]any{}),
		daptest.Respond("launch", nil),
		daptest.Reply("threads", map[string]any{"threads": []map[string]any{{"id": 1, "name": "main"}}}),
	}
	s = append(s, extra...)
	return append(s, daptest.Reply("disconnect", nil), daptest.WaitClose())
}

// dbgOpts tune a debugRig.
type dbgOpts struct {
	// manifest changes the manifest of the extension "dbg".
	manifest func(*extension.Manifest)
	// activate is called by Activate of "dbg" after it registered its adapter factory.
	activate func(d *debugRig, api extension.API) error
	// noFactory leaves the adapter factory out.
	noFactory bool
	// tune changes the options of the debug service.
	tune func(*debug.Options)
	// noHost gives the tab no debug host.
	noHost bool
}

// debugRig is the tab of the debug tests.
type debugRig struct {
	*tab
	r       *rig
	t       *testing.T
	starter extension.API
	dbgAPI  extension.API

	mu       sync.Mutex
	scripts  map[string]daptest.Script
	servers  map[string]*daptest.Server
	adapters []extension.DebugAdapter
	// executable is what the factory got as the manifest's default adapter.
	executable *extension.DebugAdapter
	// override replaces the adapter the factory makes, when it is set.
	override     func(s extension.DebugSession) (*extension.DebugAdapter, error)
	factoryCalls int
}

// debugSchema is the attribute schema of the type fake.
const debugSchema = `{"required": ["program"], "properties": {"program": {"type": "string"}, "transport": {"type": "string"}, "mode": {"type": "string", "enum": ["a", "b"]}}}`

// debugManifest is the manifest of "dbg".
func debugManifest() extension.Manifest {
	m := manifest("dbg", extension.OnCommand("dbg.never"))
	m.Contributes.Debuggers = []extension.DebuggerSpec{{
		Type: "fake", Label: "Fake", Languages: []string{"fakelang"},
		ConfigurationAttributes: map[string]json.RawMessage{"launch": json.RawMessage(debugSchema), "attach": json.RawMessage(`{"properties": {"port": {"type": "integer"}}}`)},
		InitialConfigurations:   []extension.DebugConfiguration{{"name": "Fake: Launch", "request": "launch", "program": "${file}"}},
		ConfigurationSnippets:   []extension.DebugSnippet{{Label: "Fake: Launch", Body: extension.DebugConfiguration{"name": "Fake", "type": "fake", "request": "launch", "program": "${1:p}"}}},
		Variables:               map[string]string{"pickProcess": "dbg.pickProcess"},
	}}
	m.Contributes.Breakpoints = []extension.BreakpointSpec{{Language: "fakelang"}}
	return m
}

func newDebugRig(t *testing.T, o dbgOpts) *debugRig {
	t.Helper()
	d := &debugRig{t: t, scripts: map[string]daptest.Script{}, servers: map[string]*daptest.Server{}}
	starterFactory, _ := factory(manifest("starter"), func(f *fake) {
		f.activate = func(_ context.Context, api extension.API) error { d.starter = api; return nil }
	})
	m := debugManifest()
	if o.manifest != nil {
		o.manifest(&m)
	}
	dbgFactory, _ := factory(m, func(f *fake) {
		f.activate = func(_ context.Context, api extension.API) error {
			d.dbgAPI = api
			if !o.noFactory {
				if _, err := api.Debug().RegisterAdapterDescriptorFactory("fake", d.descriptor); err != nil {
					return err
				}
			}
			if o.activate != nil {
				return o.activate(d, api)
			}
			return nil
		}
	})
	d.r = newRig(t, starterFactory, dbgFactory)
	d.r.noDebug["w"] = o.noHost
	d.r.debugTune = o.tune
	d.tab = d.r.newTab("w")
	d.scope.Start()
	return d
}

// script sets the script of the adapter that the session named name gets.
func (d *debugRig) script(name string, s daptest.Script) {
	d.mu.Lock()
	d.scripts[name] = s
	d.mu.Unlock()
}

// server is the in-process server of a session of the transports server and pipe.
func (d *debugRig) server(name string) *daptest.Server {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.servers[name]
}

// descriptor is the adapter factory of "dbg": the configuration's transport picks how the fake adapter is reached.
func (d *debugRig) descriptor(_ context.Context, s extension.DebugSession, def *extension.DebugAdapter) (*extension.DebugAdapter, error) {
	d.mu.Lock()
	d.factoryCalls++
	d.executable = def
	script := d.scripts[s.Name()]
	override := d.override
	d.mu.Unlock()
	if override != nil {
		return override(s)
	}
	transport, _ := s.Configuration()["transport"].(string)
	var a *extension.DebugAdapter
	switch transport {
	case "server", "pipe":
		a = d.listen(s.Name(), transport, script)
	case "execserver":
		a = &extension.DebugAdapter{Kind: extension.DebugAdapterExecutableServer, Command: daptest.Program(d.t), Args: []string{"-listen", "tcp", "-announce", "-script", d.scriptFile(script)}}
	case "default":
		return nil, nil
	default:
		a = &extension.DebugAdapter{Kind: extension.DebugAdapterExecutable, Command: daptest.Program(d.t), Args: []string{"-script", d.scriptFile(script)}}
	}
	d.mu.Lock()
	d.adapters = append(d.adapters, *a)
	d.mu.Unlock()
	return a, nil
}

// scriptFile writes a script for the fake adapter program.
func (d *debugRig) scriptFile(s daptest.Script) string { return scriptFileFor(d.t, s) }

// scriptFileFor writes a script for the fake adapter program.
func scriptFileFor(t *testing.T, s daptest.Script) string {
	t.Helper()
	lines, err := s.Lines()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "script.jsonl")
	if err := os.WriteFile(path, lines, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// listen starts the fake adapter in process on a TCP port or a Unix socket and returns the descriptor of it.
func (d *debugRig) listen(name, transport string, script daptest.Script) *extension.DebugAdapter {
	d.t.Helper()
	var l net.Listener
	var err error
	var a *extension.DebugAdapter
	if transport == "pipe" {
		dir, derr := os.MkdirTemp("", "dbg")
		if derr != nil {
			d.t.Fatal(derr)
		}
		d.t.Cleanup(func() {
			if rerr := os.RemoveAll(dir); rerr != nil {
				d.t.Error(rerr)
			}
		})
		path := filepath.Join(dir, "s")
		l, err = net.Listen("unix", path)
		a = &extension.DebugAdapter{Kind: extension.DebugAdapterPipeServer, Path: path}
	} else {
		l, err = net.Listen("tcp", "127.0.0.1:0")
		if err == nil {
			a = &extension.DebugAdapter{Kind: extension.DebugAdapterServer, Port: l.Addr().(*net.TCPAddr).Port}
		}
	}
	if err != nil {
		d.t.Fatal(err)
	}
	srv := daptest.NewServer(script, daptest.ServerOptions{})
	d.mu.Lock()
	d.servers[name] = srv
	d.mu.Unlock()
	go func() {
		conn, aerr := l.Accept()
		if cerr := l.Close(); cerr != nil {
			d.t.Log("closing the listener:", cerr)
		}
		if aerr != nil {
			return
		}
		if serr := srv.Serve(conn); serr != nil {
			d.t.Log("the fake adapter stopped:", serr)
		}
	}()
	d.t.Cleanup(func() {
		if cerr := l.Close(); cerr != nil {
			return
		}
	})
	return a
}

// fakeConfig is a configuration of the type fake.
func fakeConfig(name, transport string) extension.DebugConfiguration {
	return extension.DebugConfiguration{"name": name, "type": "fake", "request": "launch", "program": "p", "transport": transport}
}

// start starts a configuration given whole, from the extension "starter", and returns the session.
func (d *debugRig) start(cfg extension.DebugConfiguration) extension.DebugSession {
	d.t.Helper()
	ok, err := d.startErr(cfg)
	if err != nil || !ok {
		d.t.Fatalf("StartDebugging = %v, %v", ok, err)
	}
	return d.sessionNamed(cfg["name"].(string))
}

func (d *debugRig) startErr(cfg extension.DebugConfiguration) (bool, error) {
	d.t.Helper()
	if d.starter == nil {
		d.t.Fatal("the starter is not active")
	}
	ctx, cancel := context.WithTimeout(context.Background(), debugWait*2)
	defer cancel()
	return d.starter.Debug().StartDebugging(ctx, "", extension.DebugTarget{Config: cfg}, extension.DebugStartOptions{})
}

func (d *debugRig) sessionNamed(name string) extension.DebugSession {
	d.t.Helper()
	for _, s := range d.starter.Debug().Sessions() {
		if s.Name() == name {
			return s
		}
	}
	d.t.Fatalf("no session named %s among %d", name, len(d.starter.Debug().Sessions()))
	return nil
}

// coreSession is the session of the service behind an extension's session.
func (d *debugRig) coreSession(s extension.DebugSession) *debug.Session {
	d.t.Helper()
	cs, err := d.debug.coreSession(s)
	if err != nil {
		d.t.Fatal(err)
	}
	return cs
}

// idle waits for the work a session queued.
func (d *debugRig) idle(s extension.DebugSession) {
	d.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), debugWait)
	defer cancel()
	if err := d.coreSession(s).Idle(ctx); err != nil {
		d.t.Fatal(err)
	}
}

// stop stops all the sessions.
func (d *debugRig) stop() {
	d.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), debugWait)
	defer cancel()
	if err := d.starter.Debug().StopDebugging(ctx, nil); err != nil {
		d.t.Fatal(err)
	}
	waitDebug(d.t, "the sessions to end", func() bool { return len(d.starter.Debug().Sessions()) == 0 })
}

// strRecorder keeps strings that several goroutines add.
type strRecorder struct {
	mu   sync.Mutex
	list []string
}

func (r *strRecorder) add(s string) {
	r.mu.Lock()
	r.list = append(r.list, s)
	r.mu.Unlock()
}

func (r *strRecorder) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.list...)
}
