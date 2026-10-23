package theme

import (
	"testing"
	"testing/fstest"

	"github.com/rm4n0s/errors"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	return NewService(DefaultColorRegistry())
}

func TestServiceStartsWithDarkModern(t *testing.T) {
	s := newTestService(t)
	if info := s.CurrentInfo(); info.Label != "Dark Modern" || !info.Bundled {
		t.Errorf("info %+v", info)
	}
	if c, _ := s.Current().Color("editor.background"); c.Hex() != "#1f1f1f" {
		t.Errorf("editor.background %s", c)
	}
	if s.Registry() == nil {
		t.Error("registry")
	}
}

func TestServiceChoosesTheTheme(t *testing.T) {
	cases := []struct {
		name     string
		settings Settings
		dark     bool
		want     string
	}{
		{"the theme setting", Settings{ColorTheme: "Light+"}, true, "Light+"},
		{"by id", Settings{ColorTheme: "Default High Contrast"}, true, "Dark High Contrast"},
		{"empty names use the defaults", Settings{}, true, "Dark Modern"},
		{"auto detect, dark system", Settings{ColorTheme: "Light+", AutoDetectColorScheme: true, PreferredDarkColorTheme: "Dark+", PreferredLightColorTheme: "Light Modern"}, true, "Dark+"},
		{"auto detect, light system", Settings{ColorTheme: "Dark+", AutoDetectColorScheme: true, PreferredDarkColorTheme: "Dark+", PreferredLightColorTheme: "Light Modern"}, false, "Light Modern"},
		{"auto detect with no preferred themes, light system", Settings{AutoDetectColorScheme: true}, false, "Light Modern"},
		{"the system is ignored without auto detect", Settings{ColorTheme: "Dark+"}, false, "Dark+"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestService(t)
			if err := s.SetPlatformBrightness(tc.dark); err != nil {
				t.Fatal(err)
			}
			if err := s.Apply(tc.settings); err != nil {
				t.Fatal(err)
			}
			if got := s.CurrentInfo().Label; got != tc.want {
				t.Errorf("theme %q, want %q", got, tc.want)
			}
		})
	}
}

func TestServiceFollowsTheBrightness(t *testing.T) {
	s := newTestService(t)
	var seen []string
	unsubscribe := s.OnChange(func(ct *ColorTheme) { seen = append(seen, string(ct.Type())) })
	if err := s.Apply(Settings{AutoDetectColorScheme: true, PreferredDarkColorTheme: "Dark+", PreferredLightColorTheme: "Light+"}); err != nil {
		t.Fatal(err)
	}
	steps := []struct {
		dark bool
		want string
	}{{false, "Light+"}, {true, "Dark+"}, {true, "Dark+"}, {false, "Light+"}}
	for _, step := range steps {
		if err := s.SetPlatformBrightness(step.dark); err != nil {
			t.Fatal(err)
		}
		if got := s.CurrentInfo().Label; got != step.want {
			t.Errorf("dark=%v: %q, want %q", step.dark, got, step.want)
		}
	}
	// Dark Modern to Dark+ (settings), to Light+, to Dark+, (no change), to Light+.
	if want := []string{"dark", "light", "dark", "light"}; len(seen) != len(want) {
		t.Fatalf("changes %v, want %v", seen, want)
	} else {
		for i := range want {
			if seen[i] != want[i] {
				t.Errorf("changes %v, want %v", seen, want)
			}
		}
	}
	unsubscribe()
	if err := s.SetPlatformBrightness(true); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 4 {
		t.Errorf("a listener that unsubscribed was called: %v", seen)
	}
}

func TestServiceReportsThemesItCannotFind(t *testing.T) {
	s := newTestService(t)
	err := s.Apply(Settings{ColorTheme: "Nope"})
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("Service.find.ThemeNotFound") {
		t.Fatalf("err = %v, route %s", err, appErr.Route())
	}
	if got := s.CurrentInfo().Label; got != "Dark Modern" {
		t.Errorf("the default dark theme is used: %q", got)
	}
	// With the system light and auto detect on, the fallback is the light default.
	if err := s.Apply(Settings{}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPlatformBrightness(false); err != nil {
		t.Fatal(err)
	}
	err = s.Apply(Settings{AutoDetectColorScheme: true, PreferredLightColorTheme: "Nope"})
	if err == nil {
		t.Fatal("no error for a missing theme")
	}
	if got := s.CurrentInfo().Label; got != "Light Modern" {
		t.Errorf("the default light theme is used: %q", got)
	}
}

func TestServiceUserThemes(t *testing.T) {
	s := newTestService(t)
	fsys := fstest.MapFS{
		"mine.json":   file(`{"name": "Mine", "type": "light", "colors": {"editor.background": "#fafafa"}}`),
		"broken.json": file(`{`),
	}
	user, problems, err := LoadUserThemes(fsys)
	if err != nil || len(problems) != 1 {
		t.Fatalf("%v %v", err, problems)
	}
	// The setting names the theme before the theme is known: the fallback is used, and the theme
	// is taken when the user's themes arrive.
	if err := s.Apply(Settings{ColorTheme: "Mine"}); err == nil {
		t.Fatal("the theme is not there yet")
	}
	var changes int
	s.OnChange(func(*ColorTheme) { changes++ })
	if err := s.SetUserThemes(user); err != nil {
		t.Fatal(err)
	}
	info := s.CurrentInfo()
	if info.Label != "Mine" || info.Bundled || info.Type != TypeLight || changes != 1 {
		t.Errorf("info %+v after %d changes", info, changes)
	}
	if c, _ := s.Current().Color("editor.background"); c.Hex() != "#fafafa" {
		t.Errorf("editor.background %s", c)
	}
	list, err := s.Themes()
	if err != nil || len(list) != 7 || list[6].ID != "mine" {
		t.Errorf("themes %+v %v", list, err)
	}
	if err := s.SetUserThemes(user); err != nil {
		t.Fatal(err)
	}
	if changes != 1 {
		t.Errorf("the same themes again are no change: %d", changes)
	}
}
