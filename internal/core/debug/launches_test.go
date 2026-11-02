package debug_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/debug"
	"github.com/rm4n0s/errors"
)

func newLaunchService(t *testing.T, opts debug.Options) *debug.Service {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return debug.NewService(ctx, opts)
}

func writeLaunch(t *testing.T, folder, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(folder, ".emetos"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(debug.LaunchFilePath(folder), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLaunchNamesAndConfigurations(t *testing.T) {
	folder := t.TempDir()
	writeLaunch(t, folder, `{
  "configurations": [
    {"name": "B", "type": "go", "request": "launch", "presentation": {"group": "z", "order": 1}},
    {"name": "A", "type": "go", "request": "launch"},
    {"name": "Hidden", "type": "go", "request": "launch", "presentation": {"hidden": true}},
    {"name": "A", "type": "python", "request": "launch", "linux": {"program": "lin", "presentation": {"group": "a", "order": 5}}},
    {"name": "C", "type": "go", "request": "launch", "presentation": {"group": "a", "order": 1}},
    {"type": "go", "request": "launch"},
    {"name": 7},
  ],
  "compounds": [
    {"name": "All", "configurations": ["A", "B"], "presentation": {"group": "a", "order": 0}},
    {"name": "Empty", "configurations": []},
    {"name": "Hidden compound", "configurations": ["A"], "presentation": {"hidden": true}},
  ]
}`)
	svc := newLaunchService(t, debug.Options{})
	if err := svc.Launches().Reload(context.Background(), debug.LaunchSources{Folder: folder}); err != nil {
		t.Fatal(err)
	}
	l := svc.Launches().Launch(folder)
	if l == nil || l.Kind != debug.LaunchFolder || l.Name != filepath.Base(folder) || !l.Found {
		t.Fatalf("launch = %+v", l)
	}
	// Presentation: group "a" first (by order: All 0, C 1, the second A with the group of its linux block 5), then group "z",
	// then the ones with no group in file order; hidden ones and empty compounds are not shown.
	want := []string{"All", "C", "A (1)", "B", "A"}
	if got := l.ConfigurationNames(false); !reflect.DeepEqual(got, want) {
		t.Errorf("names = %v, want %v", got, want)
	}
	// The raw list: configurations with a string name, in file order, the second A renamed.
	wantRaw := []string{"B", "A", "Hidden", "A (1)", "C"}
	if got := l.ConfigurationNames(true); !reflect.DeepEqual(got, wantRaw) {
		t.Errorf("raw names = %v, want %v", got, wantRaw)
	}
	c, ok := l.Configuration("A (1)")
	if !ok || c.Type() != "python" || c.String("program") != "lin" {
		t.Errorf("A (1) = %v %v", c, ok)
	}
	if _, ok := l.Configuration("nope"); ok {
		t.Error("found a configuration that is not there")
	}
	if comp, ok := l.Compound("All"); !ok || len(comp.Configurations) != 2 {
		t.Errorf("compound = %+v %v", comp, ok)
	}
	if _, ok := l.Compound("nope"); ok {
		t.Error("found a compound that is not there")
	}
}

func TestLaunchesReloadAndSelection(t *testing.T) {
	folder := t.TempDir()
	writeLaunch(t, folder, `{"configurations": [{"name": "one", "type": "go", "request": "launch"}, {"name": "two", "type": "python", "request": "launch"}]}`)
	svc := newLaunchService(t, debug.Options{})
	var changes int
	svc.Subscribe(func(ev debug.Event) {
		if ev.Kind == debug.EventConfiguration {
			changes++
		}
	})
	ls := svc.Launches()
	src := debug.LaunchSources{
		Folder:           folder,
		WorkspaceSetting: map[string]any{"configurations": []any{map[string]any{"name": "w", "type": "go", "request": "launch"}}},
		UserSetting:      map[string]any{"configurations": []any{map[string]any{"name": "u", "type": "go", "request": "launch"}}},
	}
	if err := ls.Reload(context.Background(), src); err != nil {
		t.Fatal(err)
	}
	var kinds []debug.LaunchKind
	for _, l := range ls.Launches() {
		kinds = append(kinds, l.Kind)
	}
	if want := []debug.LaunchKind{debug.LaunchFolder, debug.LaunchWorkspace, debug.LaunchUser}; !reflect.DeepEqual(kinds, want) {
		t.Errorf("kinds = %v", kinds)
	}
	if got := ls.Selected(); got.Name != "one" || got.Type != "go" || got.Launch.Kind != debug.LaunchFolder {
		t.Errorf("first selection = %+v", got)
	}
	if !ls.Launch("user").Hidden() {
		t.Error("the launch of the user settings is not hidden")
	}
	if n := len(ls.All()); n != 4 {
		t.Errorf("All() has %d entries, want 4", n)
	}
	// Selecting another keeps it over a reload.
	ls.Select(ls.Launch(folder), "two", debug.SelectOptions{})
	if err := ls.Reload(context.Background(), src); err != nil {
		t.Fatal(err)
	}
	if got := ls.Selected(); got.Name != "two" || got.Type != "python" {
		t.Errorf("selection after reload = %+v", got)
	}
	// A name that is gone: the first one.
	writeLaunch(t, folder, `{"configurations": [{"name": "three", "type": "go", "request": "launch"}]}`)
	if err := ls.Reload(context.Background(), src); err != nil {
		t.Fatal(err)
	}
	if got := ls.Selected(); got.Name != "three" {
		t.Errorf("selection after the name went = %+v", got)
	}
	// No folder: only the user settings, which has the configuration.
	if err := ls.Reload(context.Background(), debug.LaunchSources{UserSetting: src.UserSetting}); err != nil {
		t.Fatal(err)
	}
	if got := ls.Selected(); got.Name != "u" || got.Launch.Kind != debug.LaunchUser {
		t.Errorf("user selection = %+v", got)
	}
	if changes == 0 {
		t.Error("no EventConfiguration")
	}
	// A launch file that cannot be read.
	other := t.TempDir()
	if err := os.MkdirAll(debug.LaunchFilePath(other), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = wantRoute(t, ls.Reload(context.Background(), debug.LaunchSources{Folder: other}), "ReadLaunchFile.LaunchFileReadFailed")
}

func TestLaunchesDynamicSelection(t *testing.T) {
	svc := newLaunchService(t, debug.Options{
		DynamicConfigurations: func(_ context.Context, folder, typ string) ([]debug.Config, error) {
			if typ != "node" {
				return nil, nil
			}
			return []debug.Config{{"name": "Node: index.js", "type": "node", "request": "launch"}, {"name": "Node: other", "type": "node", "request": "launch"}}, nil
		},
	})
	ls := svc.Launches()
	if err := ls.Reload(context.Background(), debug.LaunchSources{Folder: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if got := ls.Selected(); got.Name != "" {
		t.Errorf("selection with no configurations = %+v", got)
	}
	ls.Select(nil, "Node: other", debug.SelectOptions{Dynamic: &debug.DynamicSelection{Type: "node", Name: "Node: other"}})
	sel := ls.Selected()
	if !sel.Dynamic || sel.Name != "Node: other" || sel.Type != "node" {
		t.Fatalf("selection = %+v", sel)
	}
	cfg, err := ls.SelectedConfig(context.Background())
	if err != nil || cfg.Name() != "Node: other" {
		t.Errorf("SelectedConfig = %v, %v", cfg, err)
	}
	ls.Select(nil, "Node: index.js", debug.SelectOptions{Dynamic: &debug.DynamicSelection{Type: "node", Name: "Node: index.js"}})
	want := []debug.DynamicSelection{{Type: "node", Name: "Node: index.js"}, {Type: "node", Name: "Node: other"}}
	if got := ls.RecentDynamic(); !reflect.DeepEqual(got, want) {
		t.Errorf("recent = %v, want %v", got, want)
	}
	// Removing the selected one moves the selection on.
	ls.RemoveRecentDynamic("Node: index.js", "node")
	if got := ls.Selected(); got.Dynamic {
		t.Errorf("selection after removing = %+v", got)
	}
	if got := ls.RecentDynamic(); len(got) != 1 || got[0].Name != "Node: other" {
		t.Errorf("recent after removing = %v", got)
	}
}

func TestResolveByProviders(t *testing.T) {
	type call struct{ typ, name string }
	tests := []struct {
		name      string
		start     debug.Config
		typ       string
		fn        func(calls *[]call) func(ctx context.Context, folder, typ string, cfg debug.Config) (debug.Config, error)
		want      debug.Config
		wantCalls []call
		wantErr   string
	}{
		{
			name: "no provider",
			typ:  "go", start: debug.Config{"name": "n", "type": "go"},
			want: debug.Config{"name": "n", "type": "go"},
		},
		{
			name: "the type and the generic providers fill the configuration",
			typ:  "go", start: debug.Config{"name": "n", "type": "go"},
			fn: func(calls *[]call) func(context.Context, string, string, debug.Config) (debug.Config, error) {
				return func(_ context.Context, _ string, typ string, cfg debug.Config) (debug.Config, error) {
					*calls = append(*calls, call{typ, cfg.Name()})
					cfg = cfg.Clone()
					cfg["via-"+typ] = true
					return cfg, nil
				}
			},
			want:      debug.Config{"name": "n", "type": "go", "via-go": true, "via-*": true},
			wantCalls: []call{{"go", "n"}, {"*", "n"}},
		},
		{
			name: "a provider changes the type: the new type is asked once",
			typ:  "a", start: debug.Config{"name": "n", "type": "a"},
			fn: func(calls *[]call) func(context.Context, string, string, debug.Config) (debug.Config, error) {
				return func(_ context.Context, _ string, typ string, cfg debug.Config) (debug.Config, error) {
					*calls = append(*calls, call{typ, cfg.Name()})
					cfg = cfg.Clone()
					switch typ {
					case "a":
						cfg["type"] = "b"
					case "b":
						cfg["type"] = "a" // back to a type that was asked: stop
					}
					return cfg, nil
				}
			},
			want:      debug.Config{"name": "n", "type": "a"},
			wantCalls: []call{{"a", "n"}, {"*", "n"}, {"b", "n"}, {"*", "n"}},
		},
		{
			name: "a provider cancels",
			typ:  "go", start: debug.Config{"name": "n", "type": "go"},
			fn: func(calls *[]call) func(context.Context, string, string, debug.Config) (debug.Config, error) {
				return func(_ context.Context, _ string, typ string, cfg debug.Config) (debug.Config, error) {
					return nil, nil
				}
			},
			want: nil,
		},
		{
			name: "a provider fails",
			typ:  "go", start: debug.Config{"name": "n", "type": "go"},
			fn: func(calls *[]call) func(context.Context, string, string, debug.Config) (debug.Config, error) {
				return func(_ context.Context, _ string, typ string, cfg debug.Config) (debug.Config, error) {
					return nil, errors.New("ProviderFailed", "no")
				}
			},
			wantErr: "ProviderFailed",
		},
		{
			name: "the type comes from the argument when the configuration has none",
			typ:  "go", start: debug.Config{"name": "n"},
			fn: func(calls *[]call) func(context.Context, string, string, debug.Config) (debug.Config, error) {
				return func(_ context.Context, _ string, typ string, cfg debug.Config) (debug.Config, error) {
					*calls = append(*calls, call{typ, cfg.Name()})
					return cfg, nil
				}
			},
			want:      debug.Config{"name": "n"},
			wantCalls: []call{{"go", "n"}, {"*", "n"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls []call
			opts := debug.Options{}
			if tt.fn != nil {
				opts.ResolveConfiguration = tt.fn(&calls)
			}
			svc := newLaunchService(t, opts)
			got, err := svc.Launches().ResolveByProviders(context.Background(), "", tt.typ, tt.start)
			if tt.wantErr != "" {
				if appErr, ok := errors.FromError(err); !ok || appErr.Tag != tt.wantErr {
					t.Fatalf("err = %v, want tag %s", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
			if !reflect.DeepEqual(calls, tt.wantCalls) {
				t.Errorf("calls = %v, want %v", calls, tt.wantCalls)
			}
		})
	}
}
