package workbench

import "testing"

func TestEffectiveMenuVisibility(t *testing.T) {
	cases := []struct {
		name            string
		setting         string
		zen, fullScreen bool
		want            string
	}{
		{"compact is the default", "", false, false, "compact"},
		{"a value that is unknown is compact", "sideways", false, false, "compact"},
		{"classic", "classic", false, false, "classic"},
		{"visible", "visible", false, false, "visible"},
		{"toggle", "toggle", false, false, "toggle"},
		{"hidden", "hidden", false, false, "hidden"},
		{"compact", "compact", false, false, "compact"},
		{"classic in full screen waits for Alt", "classic", false, true, "toggle"},
		{"compact in full screen waits for Alt", "compact", false, true, "toggle"},
		{"visible stays in full screen", "visible", false, true, "visible"},
		{"toggle in full screen", "toggle", false, true, "toggle"},
		{"hidden in full screen", "hidden", false, true, "hidden"},
		{"zen hides every one of them", "visible", true, false, "hidden"},
		{"zen in full screen", "classic", true, true, "hidden"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := effectiveMenuVisibility(tc.setting, tc.zen, tc.fullScreen); got != tc.want {
				t.Errorf("effectiveMenuVisibility(%q, zen=%v, fullScreen=%v) = %q, want %q", tc.setting, tc.zen, tc.fullScreen, got, tc.want)
			}
		})
	}
}

func TestMenuPlace(t *testing.T) {
	for _, tc := range []struct {
		appBar, fullScreen bool
		want               bool
	}{
		{true, false, true}, {true, true, false}, {false, false, false}, {false, true, false},
	} {
		if got := menuInTitleBar(tc.appBar, tc.fullScreen); got != tc.want {
			t.Errorf("menuInTitleBar(appBar=%v, fullScreen=%v) = %v, want %v", tc.appBar, tc.fullScreen, got, tc.want)
		}
	}
	rows := []struct {
		visibility           string
		inTitleBar, shown, w bool
	}{
		{"classic", false, false, true}, {"visible", false, false, true}, {"compact", false, false, true},
		{"toggle", false, false, false}, {"toggle", false, true, true}, {"hidden", false, true, false},
		{"classic", true, false, false}, {"toggle", true, true, false},
	}
	for _, tc := range rows {
		if got := menuRowShown(tc.visibility, tc.inTitleBar, tc.shown); got != tc.w {
			t.Errorf("menuRowShown(%q, inTitleBar=%v, shown=%v) = %v, want %v", tc.visibility, tc.inTitleBar, tc.shown, got, tc.w)
		}
	}
	if !titleBarRowShown(false) || titleBarRowShown(true) {
		t.Error("the title bar row hides in zen mode only")
	}
}
