package app

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/dap"
	"github.com/rm4n0s/emetos/internal/core/debug"
	"github.com/rm4n0s/emetos/internal/gui/exthost"
)

// The terminal of a runInTerminal request and debug.terminal.clearBeforeReusing (step 25.11), against a fake Terminal extension that
// answers the requests of the real one (list, write, run) and keeps what it was sent.

// fakeTerminals is the state of the fake Terminal extension.
type fakeTerminals struct {
	mu     sync.Mutex
	nextID int
	items  []*fakeTerminalItem
	// log is what the terminals were sent, in order: "new <name>", "write <id> <data>", "run <id> <text>".
	log []string
}

type fakeTerminalItem struct {
	id     int
	name   string
	exited bool
}

func (f *fakeTerminals) snapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.log)
}

func (f *fakeTerminals) add(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	f.items = append(f.items, &fakeTerminalItem{id: f.nextID, name: name})
	f.log = append(f.log, "new "+name)
}

func (f *fakeTerminals) remove(id int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.items = slices.DeleteFunc(f.items, func(t *fakeTerminalItem) bool { return t.id == id })
}

func (f *fakeTerminals) exit(id int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, t := range f.items {
		if t.id == id {
			t.exited = true
		}
	}
}

// fakeTerminalExtension is the extension: it handles the three requests the window makes.
type fakeTerminalExtension struct{ f *fakeTerminals }

func (e fakeTerminalExtension) Manifest() extension.Manifest {
	return extension.Manifest{
		ID: extension.TerminalID, Name: "Fake terminal", Version: "0.0.1",
		ActivationEvents: []string{extension.OnStartup},
	}
}

func (e fakeTerminalExtension) Activate(_ context.Context, api extension.API) error {
	f := e.f
	if err := api.Events().Handle(terminalListTopic, func(context.Context, extension.Event) (any, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		out := []map[string]any{}
		for _, t := range f.items {
			out = append(out, map[string]any{"id": t.id, "name": t.name, "exited": t.exited})
		}
		return map[string]any{"terminals": out}, nil
	}); err != nil {
		return err
	}
	record := func(kind string) extension.EventHandler {
		return func(_ context.Context, ev extension.Event) (any, error) {
			var req struct {
				ID   int    `json:"id"`
				Text string `json:"text"`
				Data string `json:"data"`
			}
			if err := json.Unmarshal(ev.Data, &req); err != nil {
				return nil, err
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			f.log = append(f.log, kind+" "+strconv.Itoa(req.ID)+" "+req.Text+req.Data)
			return struct{}{}, nil
		}
	}
	if err := api.Events().Handle(terminalRunTopic, record("run")); err != nil {
		return err
	}
	return api.Events().Handle(terminalWriteTopic, record("write"))
}

// terminalRig is a debug rig with the fake Terminal extension and the commands the window runs to open a terminal.
func terminalRig(t *testing.T, settings map[string]any) (*debugRig, *fakeTerminals) {
	t.Helper()
	f := &fakeTerminals{}
	r := newDebugRigWith(t, debugFolder(t, scriptOf(stopSteps("breakpoint", "1")...)), rigOpts{
		extensions: []extension.Factory{func() extension.Extension { return fakeTerminalExtension{f} }},
		settings:   settings,
	})
	// the real one starts with the window
	r.waitFor("the terminal extension", func() bool {
		r.win.Extensions().Activate(extension.TerminalID)
		return r.win.Extensions().State(extension.TerminalID) == exthost.StateActive
	})
	r.pump()
	register := func(id string, run func(args map[string]any)) {
		if err := r.win.Commands.Register(command.Meta{ID: id, Title: id}); err != nil {
			t.Fatal(err)
		}
		if _, err := r.win.Commands.SetHandler(id, func(_ command.Context, args any) error {
			m, _ := args.(map[string]any)
			run(m)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	register("workbench.action.terminal.new", func(args map[string]any) {
		name, _ := args["name"].(string)
		f.add(name)
	})
	register("workbench.action.terminal.sendSequence", func(args map[string]any) {
		text, _ := args["text"].(string)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.log = append(f.log, "type "+strconv.Itoa(f.items[len(f.items)-1].id)+" "+strings.TrimSuffix(text, "\n"))
	})
	return r, f
}

// runInTerminal makes the request of a session and waits for it.
func (r *debugRig) runInTerminal(session string, args dap.RunInTerminalRequestArguments) error {
	r.t.Helper()
	done := make(chan error, 1)
	go func() {
		_, err := r.win.debugRunInTerminal(context.Background(), args, session)
		done <- err
	}()
	var err error
	r.waitFor("the terminal", func() bool {
		select {
		case err = <-done:
			return true
		default:
			return false
		}
	})
	return err
}

func TestARunInTerminalRequestReusesTheTerminalOfASessionThatEnded(t *testing.T) {
	args := dap.RunInTerminalRequestArguments{Title: "Debug", Cwd: "/work", Args: []string{"go", "run", "."}}
	tests := []struct {
		name  string
		clear bool
		want  []string // what the terminal got for the third request, after the two that made terminals
	}{
		{"Ctrl+C, then the command", false, []string{"write 1 \x03", "run 1 cd /work && go run ."}},
		{"clearBeforeReusing clears first", true, []string{"write 1 \x03", "run 1 clear", "run 1 cd /work && go run ."}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, f := terminalRig(t, map[string]any{"debug.terminal.clearBeforeReusing": tc.clear})
			// a session that runs has its terminal; a second one gets a terminal of its own
			if err := r.runInTerminal("s1", args); err != nil {
				t.Fatal(err)
			}
			if err := r.runInTerminal("s2", args); err != nil {
				t.Fatal(err)
			}
			want := []string{"new Debug", "type 1 go run .", "new Debug", "type 2 go run ."}
			if got := f.snapshot(); !slices.Equal(got, want) {
				t.Fatalf("the terminals got %q, want %q", got, want)
			}
			// the first session ends: its terminal waits for the next one
			r.win.freeDebugTerminals("s1")
			if err := r.runInTerminal("s3", args); err != nil {
				t.Fatal(err)
			}
			if got := f.snapshot()[len(want):]; !slices.Equal(got, tc.want) {
				t.Errorf("the reused terminal got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestATerminalThatIsGoneOrHasEndedIsNotReused(t *testing.T) {
	args := dap.RunInTerminalRequestArguments{Title: "Debug", Args: []string{"./app"}}
	tests := []struct {
		name  string
		close func(f *fakeTerminals)
	}{
		{"the user closed it", func(f *fakeTerminals) { f.remove(1) }},
		{"its shell ended", func(f *fakeTerminals) { f.exit(1) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, f := terminalRig(t, nil)
			if err := r.runInTerminal("s1", args); err != nil {
				t.Fatal(err)
			}
			r.win.freeDebugTerminals("s1")
			tc.close(f)
			if err := r.runInTerminal("s2", args); err != nil {
				t.Fatal(err)
			}
			log := f.snapshot()
			if want := []string{"new Debug", "type 1 ./app", "new Debug", "type 2 ./app"}; !slices.Equal(log, want) {
				t.Errorf("the terminals got %q, want %q", log, want)
			}
		})
	}
}

func TestTheTerminalOfASessionIsFreedWhenTheSessionEnds(t *testing.T) {
	r, f := terminalRig(t, map[string]any{"debug.terminal.clearBeforeReusing": true})
	r.startOne("Debug the order", debug.SessionOptions{})
	session := r.svc().Model().Sessions(false)[0].ID()
	args := dap.RunInTerminalRequestArguments{Args: []string{"./app"}}
	if err := r.runInTerminal(session, args); err != nil {
		t.Fatal(err)
	}
	r.stopAll()
	if err := r.runInTerminal("next", args); err != nil {
		t.Fatal(err)
	}
	want := []string{"new Debug", "type 1 ./app", "write 1 \x03", "run 1 clear", "run 1 ./app"}
	if got := f.snapshot(); !slices.Equal(got, want) {
		t.Errorf("the terminal got %q, want %q", got, want)
	}
}

func TestARunInTerminalRequestWithoutTheTerminalExtensionStillOpensATerminal(t *testing.T) {
	// the rig has no terminal extension: the requests fail and the window falls back on a new terminal by its commands
	r := newDebugRig(t, debugFolder(t, nil))
	var created []string
	if err := r.win.Commands.Register(command.Meta{ID: "workbench.action.terminal.new", Title: "new"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.win.Commands.SetHandler("workbench.action.terminal.new", func(_ command.Context, args any) error {
		m, _ := args.(map[string]any)
		name, _ := m["name"].(string)
		created = append(created, name)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.win.Commands.Register(command.Meta{ID: "workbench.action.terminal.sendSequence", Title: "send"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.win.Commands.SetHandler("workbench.action.terminal.sendSequence", func(command.Context, any) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := r.runInTerminal("s1", dap.RunInTerminalRequestArguments{Title: "Mine", Args: []string{"x"}}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(created, []string{"Mine"}) {
		t.Errorf("terminals made: %v", created)
	}
}
