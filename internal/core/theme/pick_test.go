package theme

import (
	"reflect"
	"testing"
	"testing/fstest"

	"github.com/rm4n0s/errors"
)

// labels lists the labels of the entries of a group.
func labels(g PickerGroup) []string {
	var out []string
	for _, e := range g.Entries {
		out = append(out, e.Info.Label)
	}
	return out
}

func TestPickerGroups(t *testing.T) {
	user, _, err := LoadUserThemes(fstest.MapFS{
		"a-light.json": file(`{"name": "Aardvark", "type": "light", "colors": {}}`),
		"z-dark.json":  file(`{"name": "Zebra", "type": "dark", "colors": {}}`),
		"hc.json":      file(`{"name": "Contrast", "type": "hc-light", "colors": {}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		users    []UserTheme
		settings Settings
		titles   []string
		groups   map[string][]string
		current  string
	}{
		{
			name: "bundled themes in VS Code's groups and order", settings: Settings{},
			titles: []string{PickLightThemes, PickDarkThemes, PickHighContrastThemes},
			groups: map[string][]string{
				PickLightThemes:        {"Light Modern", "Light+"},
				PickDarkThemes:         {"Dark Modern", "Dark+"},
				PickHighContrastThemes: {"Dark High Contrast", "Light High Contrast"},
			},
			current: "Dark Modern",
		},
		{
			name: "the user's themes join their group, sorted by label", users: user, settings: Settings{ColorTheme: "Aardvark"},
			titles: []string{PickLightThemes, PickDarkThemes, PickHighContrastThemes},
			groups: map[string][]string{
				PickLightThemes:        {"Light Modern", "Aardvark", "Light+"},
				PickDarkThemes:         {"Dark Modern", "Dark+", "Zebra"},
				PickHighContrastThemes: {"Contrast", "Dark High Contrast", "Light High Contrast"},
			},
			current: "Aardvark",
		},
		{
			name: "the current theme is marked in its group", settings: Settings{ColorTheme: "Default High Contrast"},
			titles: []string{PickLightThemes, PickDarkThemes, PickHighContrastThemes},
			groups: map[string][]string{
				PickLightThemes:        {"Light Modern", "Light+"},
				PickDarkThemes:         {"Dark Modern", "Dark+"},
				PickHighContrastThemes: {"Dark High Contrast", "Light High Contrast"},
			},
			current: "Dark High Contrast",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestService(t)
			if err := s.SetUserThemes(tc.users); err != nil {
				t.Fatal(err)
			}
			if err := s.Apply(tc.settings); err != nil {
				t.Fatal(err)
			}
			groups, err := s.PickerGroups()
			if err != nil {
				t.Fatal(err)
			}
			var titles []string
			var current []string
			for _, g := range groups {
				titles = append(titles, g.Title)
				if want := tc.groups[g.Title]; !reflect.DeepEqual(labels(g), want) {
					t.Errorf("%s: %v, want %v", g.Title, labels(g), want)
				}
				for _, e := range g.Entries {
					if e.Value != e.Info.Label {
						t.Errorf("%s: the setting value is %q, want the label", e.Info.Label, e.Value)
					}
					if e.Current {
						current = append(current, e.Info.Label)
					}
				}
			}
			if !reflect.DeepEqual(titles, tc.titles) {
				t.Errorf("groups %v, want %v", titles, tc.titles)
			}
			if !reflect.DeepEqual(current, []string{tc.current}) {
				t.Errorf("current %v, want only %q", current, tc.current)
			}
		})
	}
}

func TestPickerGroupsLeavesOutEmptyGroups(t *testing.T) {
	s := newTestService(t)
	groups, err := s.PickerGroups()
	if err != nil || len(groups) != 3 {
		t.Fatalf("%v %v", groups, err)
	}
	for _, g := range groups {
		if len(g.Entries) == 0 {
			t.Errorf("group %q is empty", g.Title)
		}
	}
}

func TestPickDescription(t *testing.T) {
	tests := []struct {
		name string
		info Info
		want string
	}{
		{"Dark Modern", Info{ID: "Dark Modern", Label: "Dark Modern", Bundled: true}, "Default Dark"},
		{"Light Modern", Info{ID: "Light Modern", Label: "Light Modern", Bundled: true}, "Default Light"},
		{"Dark+", Info{ID: "Dark+", Label: "Dark+", Bundled: true}, "Default Dark+"},
		{"an id that is not the label", Info{ID: "Default High Contrast", Label: "Dark High Contrast", Bundled: true}, "Default High Contrast"},
		{"a user theme named by its file", Info{ID: "mine", Label: "mine"}, ""},
		{"a user theme with a name", Info{ID: "mine", Label: "Mine"}, "mine"},
		{"a user theme that shadows a bundled id", Info{ID: "Dark Modern", Label: "Dark Modern"}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := PickDescription(tc.info); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFileIconChoices(t *testing.T) {
	choices := FileIconChoices()
	if len(choices) != 2 {
		t.Fatalf("%+v", choices)
	}
	tests := []struct {
		index int
		id    string
		label string
		value any
	}{
		{0, "", "None", nil},
		{1, DefaultFileIconTheme, "Seti (Visual Studio Code)", DefaultFileIconTheme},
	}
	for _, tc := range tests {
		c := choices[tc.index]
		if c.ID != tc.id || c.Label != tc.label || c.SettingValue() != tc.value {
			t.Errorf("choice %d: %+v value %v", tc.index, c, c.SettingValue())
		}
	}
	// every choice that has an id loads
	for _, c := range choices {
		if c.ID == "" {
			continue
		}
		if _, err := BundledFileIconTheme(c.ID); err != nil {
			t.Errorf("%s: %v", c.ID, err)
		}
	}
}

func TestServicePreview(t *testing.T) {
	s := newTestService(t)
	var seen []string
	s.OnChange(func(ct *ColorTheme) { seen = append(seen, s.CurrentInfo().Label) })
	steps := []struct {
		name  string
		do    func() error
		want  string
		on    bool
		calls int
	}{
		{"preview a theme", func() error { return s.Preview("Light Modern") }, "Light Modern", true, 1},
		{"preview another", func() error { return s.Preview("Dark+") }, "Dark+", true, 2},
		{"the same again is no change", func() error { return s.Preview("Dark+") }, "Dark+", true, 2},
		{"ending goes back to the settings", s.EndPreview, "Dark Modern", false, 3},
		{"ending twice is nothing", s.EndPreview, "Dark Modern", false, 3},
	}
	for _, st := range steps {
		if err := st.do(); err != nil {
			t.Fatalf("%s: %v", st.name, err)
		}
		if got := s.CurrentInfo().Label; got != st.want || s.Previewing() != st.on || len(seen) != st.calls {
			t.Errorf("%s: theme %q previewing %v after %d changes, want %q %v %d", st.name, got, s.Previewing(), len(seen), st.want, st.on, st.calls)
		}
	}
}

func TestServicePreviewGivesWayToTheSettings(t *testing.T) {
	tests := []struct {
		name     string
		settings Settings
		want     string
		on       bool
	}{
		{"the same settings again keep the preview", Settings{}, "Light Modern", true},
		{"other settings end it and win", Settings{ColorTheme: "Dark+"}, "Dark+", false},
		{"other settings that name the previewed theme", Settings{ColorTheme: "Light Modern"}, "Light Modern", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestService(t)
			if err := s.Apply(Settings{}); err != nil {
				t.Fatal(err)
			}
			if err := s.Preview("Light Modern"); err != nil {
				t.Fatal(err)
			}
			if err := s.Apply(tc.settings); err != nil {
				t.Fatal(err)
			}
			if got := s.CurrentInfo().Label; got != tc.want || s.Previewing() != tc.on {
				t.Errorf("theme %q previewing %v, want %q %v", got, s.Previewing(), tc.want, tc.on)
			}
		})
	}
}

func TestServicePreviewOfAMissingTheme(t *testing.T) {
	s := newTestService(t)
	err := s.Preview("Nope")
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("Service.find.ThemeNotFound") {
		t.Fatalf("err = %v", err)
	}
	if s.Previewing() || s.CurrentInfo().Label != "Dark Modern" {
		t.Error("a failed preview changes nothing")
	}
}

func TestServicePreviewOfAUserTheme(t *testing.T) {
	user, _, err := LoadUserThemes(fstest.MapFS{"mine.json": file(`{"name": "Mine", "type": "light", "colors": {"editor.background": "#fafafa"}}`)})
	if err != nil {
		t.Fatal(err)
	}
	s := newTestService(t)
	if err := s.SetUserThemes(user); err != nil {
		t.Fatal(err)
	}
	if err := s.Preview("Mine"); err != nil {
		t.Fatal(err)
	}
	if c, _ := s.Current().Color("editor.background"); c.Hex() != "#fafafa" {
		t.Errorf("editor.background %s", c)
	}
	if err := s.EndPreview(); err != nil {
		t.Fatal(err)
	}
	if got := s.CurrentInfo().Label; got != "Dark Modern" {
		t.Errorf("after the preview %q", got)
	}
}
