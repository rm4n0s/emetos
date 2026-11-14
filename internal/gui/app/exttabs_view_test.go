package app

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	extui "github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
)

// The editor tabs of a kind that is shared between the screens of a hosted workspace (SPEC §10.18 "Tabs of extensions in the view";
// the terminals of the editor area are such a kind): the window of the host and the view of each client show the tab, a tab opened
// or closed on one screen is opened or closed on all, and the content is built for each screen.

// shState is what the test extension "sh" keeps.
type shState struct {
	mu sync.Mutex
	// tabs are the tabs made, closes the tabs the extension was told are closed, and built the screens Build ran for.
	tabs   []extension.EditorTab
	closes []string
	built  map[extension.Origin]int
	api    extension.API
}

type shExt struct{ st *shState }

func (e *shExt) Manifest() extension.Manifest {
	m := extension.Manifest{ID: "sh", Name: "Sh", Version: "1.0.0"}
	m.Contributes.Commands = []extension.CommandSpec{
		{ID: "sh.new", Title: "New Pane"}, {ID: "sh.newSolo", Title: "New Solo"}, {ID: "sh.reveal", Title: "Reveal the First Pane"},
	}
	m.Contributes.EditorKinds = []extension.EditorKindSpec{
		{ID: "sh.pane", Name: "Pane", Icon: "terminal", Shared: true},
		{ID: "sh.solo", Name: "Solo", Icon: "note"},
	}
	return m
}

func (e *shExt) Activate(_ context.Context, api extension.API) error {
	st := e.st
	st.mu.Lock()
	st.api, st.built = api, map[extension.Origin]int{}
	st.mu.Unlock()
	kind := func(prefix string) extension.EditorKind {
		return extension.EditorKind{Create: func(tab extension.EditorTab, raw json.RawMessage) (extension.EditorTabContent, error) {
			var name string
			if err := json.Unmarshal(raw, &name); err != nil {
				return extension.EditorTabContent{}, err
			}
			st.mu.Lock()
			st.tabs = append(st.tabs, tab)
			st.mu.Unlock()
			tab.SetTitle(name)
			return extension.EditorTabContent{
				Build: func(bc widgets.BuildContext) widgets.Widget {
					st.mu.Lock()
					st.built[extui.OriginOf(bc)]++
					st.mu.Unlock()
					return widgets.NewText(prefix+":"+name, widgets.TextOpts{Style: extui.TextStyle(bc, 13, extui.Theme(bc).Foreground)})
				},
				SaveState: func() (any, error) { return name, nil },
				OnClose: func() {
					st.mu.Lock()
					st.closes = append(st.closes, name)
					st.mu.Unlock()
				},
			}, nil
		}}
	}
	for id, prefix := range map[string]string{"sh.pane": "pane", "sh.solo": "solo"} {
		if err := api.Editors().Provide(id, kind(prefix)); err != nil {
			return err
		}
	}
	open := func(kindID, prefix string) extension.CommandHandler {
		return func(_ context.Context, args any) error {
			st.mu.Lock()
			name := fmt.Sprintf("%s %d", prefix, len(st.tabs)+1)
			st.mu.Unlock()
			_, err := api.Editors().Open(kindID, name, extension.EditorOpenOptions{Group: extension.GroupOfArgs(args)})
			return err
		}
	}
	for id, h := range map[string]extension.CommandHandler{
		"sh.new":     open("sh.pane", "Pane"),
		"sh.newSolo": open("sh.solo", "Solo"),
		"sh.reveal": func(context.Context, any) error {
			st.mu.Lock()
			tab := st.tabs[0]
			st.mu.Unlock()
			tab.Reveal(true)
			return nil
		},
	} {
		if err := api.Commands().Handle(id, h); err != nil {
			return err
		}
	}
	return nil
}

// shRig is a window with the extension sh, a text file open, and views of clients.
type shRig struct {
	t   *testing.T
	run *stateRun
	st  *shState
}

func newShRig(t *testing.T) *shRig {
	t.Helper()
	env := newStateEnv(t, nil)
	st := &shState{}
	run := env.startWith(env.root, ProcessOptions{Extensions: []extension.Factory{func() extension.Extension { return &shExt{st: st} }}},
		filepath.Join(env.root, filepath.FromSlash(stateFiles[0])))
	run.pump()
	return &shRig{t: t, run: run, st: st}
}

// view makes the view of a client.
func (r *shRig) view() *headlessView {
	r.t.Helper()
	v, err := r.run.win.makeHeadlessView(extension.Size{Width: 500, Height: 400}, rigFrameRate)
	if err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(v.Close)
	r.run.pump()
	return v
}

// inView runs a command in the view of a client, as a key or a menu of the client does.
func (r *shRig) inView(v *headlessView, id string, args any) {
	r.t.Helper()
	if err := v.ws.Commands.Run(command.Context{Ctx: context.Background(), Keys: v.ws.Keys}, id, args); err != nil {
		r.t.Fatalf("%s in the view: %v", id, err)
	}
	r.run.pump()
}

// tabsOf lists the tabs of the groups of a shell, the active tab of each group starred.
func tabsOf(s *shellState) string {
	var groups []string
	for _, g := range s.group.grid.Groups(editorgroup.GridAppearance) {
		var tabs []string
		for _, in := range g.Model.Editors(editorgroup.Sequential, false) {
			name := in.Name()
			if in == g.Model.ActiveEditor() {
				name += "*"
			}
			tabs = append(tabs, name)
		}
		groups = append(groups, strings.Join(tabs, ", "))
	}
	return strings.Join(groups, " | ")
}

func (r *shRig) closed() []string {
	r.st.mu.Lock()
	defer r.st.mu.Unlock()
	return slices.Clone(r.st.closes)
}

func (r *shRig) builtOn(o extension.Origin) int {
	r.st.mu.Lock()
	defer r.st.mu.Unlock()
	return r.st.built[o]
}

func TestATabOfASharedKindIsOnEveryScreenOfTheWorkspace(t *testing.T) {
	r := newShRig(t)
	file := filepath.Base(stateFiles[0])
	v := r.view()
	host, client := r.run.shell(), v.ws.shell
	if client == nil {
		t.Fatal("the view has no shell")
	}
	check := func(what, wantHost, wantClient string) {
		t.Helper()
		if got := tabsOf(host); got != wantHost {
			t.Errorf("%s: the window of the host has %q, want %q", what, got, wantHost)
		}
		if got := tabsOf(client); got != wantClient {
			t.Errorf("%s: the view of the client has %q, want %q", what, got, wantClient)
		}
	}
	check("at the start", file+"*", file+"*")

	// the host opens a tab: it is in front in its window, and behind the file in the view of the client
	r.run.command("sh.new", nil)
	check("the host opened a tab", file+", Pane 1*", file+"*, Pane 1")
	if r.builtOn(extension.HostOrigin) == 0 || r.builtOn(v.ws.origin) != 0 {
		t.Errorf("built on the host %d times and on the client %d: the client's tab is not in front", r.builtOn(extension.HostOrigin), r.builtOn(v.ws.origin))
	}

	// the client reveals it: on its own screen, where the content is built for it
	r.inView(v, "sh.reveal", nil)
	check("the client revealed the tab", file+", Pane 1*", file+", Pane 1*")
	if r.builtOn(v.ws.origin) == 0 {
		t.Error("the content was not built for the screen of the client")
	}

	// the client opens a tab: in front in its view, behind what the host shows
	r.inView(v, "sh.new", nil)
	check("the client opened a tab", file+", Pane 1*, Pane 2", file+", Pane 1, Pane 2*")

	// what the extension sets on the tab shows on both
	r.st.mu.Lock()
	second := r.st.tabs[1]
	r.st.mu.Unlock()
	second.SetTitle("Renamed")
	r.run.pump()
	check("the tab was renamed", file+", Pane 1*, Renamed", file+", Pane 1, Renamed*")

	// a tab of a kind that is not shared opens in the window of the host only, also for the client (beside the tab in front there)
	r.inView(v, "sh.newSolo", nil)
	check("the client opened a tab of another kind", file+", Pane 1, Solo 3*, Renamed", file+", Pane 1, Renamed*")

	// the client closes its tab: it closes in the window of the host, and the extension is told once
	r.inView(v, "workbench.action.closeActiveEditor", nil)
	check("the client closed a tab", file+", Pane 1, Solo 3*", file+", Pane 1*")
	if got := r.closed(); !slices.Equal(got, []string{"Pane 2"}) {
		t.Errorf("the extension was told of %v", got)
	}

	// the extension closes the other: gone from both
	r.st.mu.Lock()
	first := r.st.tabs[0]
	r.st.mu.Unlock()
	first.Close()
	r.run.pump()
	check("the extension closed a tab", file+", Solo 3*", file+"*")
	if got := r.closed(); !slices.Equal(got, []string{"Pane 2", "Pane 1"}) {
		t.Errorf("the extension was told of %v", got)
	}
}

func TestAViewStartsWithTheSharedTabsOfTheHostAndLeavesThemWhenItEnds(t *testing.T) {
	r := newShRig(t)
	file := filepath.Base(stateFiles[0])
	r.run.command("sh.new", nil)
	r.run.command("sh.newSolo", nil)
	r.run.command("sh.new", nil)
	host := r.run.shell()
	if got, want := tabsOf(host), file+", Pane 1, Solo 2, Pane 3*"; got != want {
		t.Fatalf("the window of the host has %q, want %q", got, want)
	}
	// a client arrives: its view has the file and the shared tabs, the one the host has in front in front
	v := r.view()
	if got, want := tabsOf(v.ws.shell), file+", Pane 1, Pane 3*"; got != want {
		t.Errorf("the view of the client starts with %q, want %q", got, want)
	}
	// a second client gets them too, and a tab opened by the first is in the view of the second
	v2 := r.view()
	r.inView(v, "sh.new", nil)
	if got, want := tabsOf(v2.ws.shell), file+", Pane 1, Pane 3*, Pane 4"; got != want {
		t.Errorf("the view of the second client has %q, want %q", got, want)
	}
	// the views end (the clients left): nothing closes, and the tabs of the host go on following the extension
	v.Close()
	v2.Close()
	r.run.pump()
	if got := r.closed(); len(got) != 0 {
		t.Errorf("the end of a view closed %v", got)
	}
	r.st.mu.Lock()
	first := r.st.tabs[0]
	r.st.mu.Unlock()
	first.SetTitle("Still Here")
	r.run.pump()
	if got, want := tabsOf(host), file+", Still Here, Solo 2, Pane 3*, Pane 4"; got != want {
		t.Errorf("after the views ended the window of the host has %q, want %q", got, want)
	}
}
