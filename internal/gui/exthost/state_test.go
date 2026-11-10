package exthost

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// State().Get and Set (step 24b.4, extension/state.go): a value an extension keeps for the folder of its scope, by the id of the extension.

func TestTheStateOfAnExtensionIsKeptByExtensionAndFolder(t *testing.T) {
	var api extension.API
	f, _ := factory(manifest("emetos.keeper"), func(f *fake) {
		f.activate = func(_ context.Context, a extension.API) error { api = a; return nil }
	})
	r := newRig(t, f)
	tb := r.newTab("one")
	tb.scope.Start()
	st := api.State()
	if _, ok := st.Get("k"); ok {
		t.Fatal("a value before any was kept")
	}
	if err := st.Set("k", map[string]any{"path": "/opt/py"}); err != nil {
		t.Fatal(err)
	}
	raw, ok := st.Get("k")
	var got struct{ Path string }
	if !ok || json.Unmarshal(raw, &got) != nil || got.Path != "/opt/py" {
		t.Errorf("Get = %s, %v", raw, ok)
	}
	if v, ok := extension.StateValue[map[string]string](st, "k"); !ok || v["path"] != "/opt/py" {
		t.Errorf("StateValue = %v, %v", v, ok)
	}
	if _, ok := extension.StateValue[int](st, "k"); ok {
		t.Error("an object decoded as an int")
	}
	// the host keeps it under the extension's id and the folder of the tab
	var keys []string
	for k := range tb.kept {
		keys = append(keys, strings.ReplaceAll(k, "\x00", "|"))
	}
	if len(keys) != 1 || keys[0] != "emetos.keeper|"+r.folderOf("one")+"|k" {
		t.Errorf("kept under %q", keys)
	}
	// nil takes it away
	if err := st.Set("k", nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := st.Get("k"); ok {
		t.Error("the value is still there after Set(nil)")
	}
}

// GlobalState(): the values of the extension for every folder, kept under the folder "*" (what an extension told the user once).
func TestTheGlobalStateOfAnExtensionIsNotTheStateOfTheFolder(t *testing.T) {
	var api extension.API
	f, _ := factory(manifest("emetos.keeper"), func(f *fake) {
		f.activate = func(_ context.Context, a extension.API) error { api = a; return nil }
	})
	r := newRig(t, f)
	tb := r.newTab("one")
	tb.scope.Start()
	if err := api.State().Set("k", "of the folder"); err != nil {
		t.Fatal(err)
	}
	if _, ok := api.GlobalState().Get("k"); ok {
		t.Fatal("the global state has the value of the folder")
	}
	if !extension.FirstTime(api.GlobalState(), "missing:tool") || extension.FirstTime(api.GlobalState(), "missing:tool") {
		t.Error("FirstTime did not say true once and false after")
	}
	if err := api.GlobalState().Set("k", "of every folder"); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		state extension.State
		key   string
		want  string
		found bool
	}{
		{"the folder keeps its value", api.State(), "k", "of the folder", true},
		{"the global state has its own", api.GlobalState(), "k", "of every folder", true},
		{"what was told is global", api.GlobalState(), "told:missing:tool", "", true},
		{"and not of the folder", api.State(), "told:missing:tool", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, ok := extension.StateValue[string](tt.state, tt.key); ok != tt.found || got != tt.want {
				t.Errorf("value = %q, %v; want %q, %v", got, ok, tt.want, tt.found)
			}
		})
	}
	var keys []string
	for k := range tb.kept {
		keys = append(keys, strings.ReplaceAll(k, "\x00", "|"))
	}
	slices.Sort(keys)
	want := []string{"emetos.keeper|*|k", "emetos.keeper|*|told:missing:tool", "emetos.keeper|" + r.folderOf("one") + "|k"}
	if !slices.Equal(keys, want) {
		t.Errorf("kept under %q, want %q", keys, want)
	}
}

func TestStateFailuresHaveTheirOwnTags(t *testing.T) {
	var api extension.API
	f, _ := factory(manifest("emetos.keeper"), func(f *fake) {
		f.activate = func(_ context.Context, a extension.API) error { api = a; return nil }
	})
	r := newRig(t, f)
	tb := r.newTab("one")
	tb.scope.Start()
	st := api.State()
	route(t, st.Set("", 1), "stateAPI.Set.ExtensionStateInvalid")
	route(t, st.Set("k", make(chan int)), "stateAPI.Set.ExtensionStateInvalid")
	route(t, st.Set("k", strings.Repeat("x", maxStateValue)), "stateAPI.Set.ExtensionStateInvalid")
	tb.keptErr = errors.New("StateBroken", "the disk is full")
	route(t, st.Set("k", 1), "StateBroken")
	tb.keptErr = nil

	r2 := newRig(t, f)
	r2.noState = map[string]bool{"two": true}
	tb2 := r2.newTab("two")
	tb2.scope.Start()
	st2 := api.State() // the activation in the second tab
	route(t, st2.Set("k", 1), "stateAPI.Set.ExtensionStateUnavailable")
	if _, ok := st2.Get("k"); ok {
		t.Error("a scope that keeps no state gave a value")
	}

	tb.scope.Close()
	tb2.scope.Close()
	route(t, st.Set("k", 1), "stateAPI.Set.ExtensionDeactivated")
	route(t, st2.Set("k", 1), "stateAPI.Set.ExtensionDeactivated")
	if _, ok := st.Get("k"); ok {
		t.Error("a deactivated extension read a value")
	}
}
