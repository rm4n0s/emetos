package keybinding

import (
	"testing"
)

// The keys of Testing (step 26.2): VS Code's chords with Ctrl+; as their first key.
func TestTestingKeysAreVSCodesChords(t *testing.T) {
	want := map[string]string{
		"testing.runAll": "ctrl+; a", "testing.debugAll": "ctrl+; ctrl+a", "testing.coverageAll": "ctrl+; ctrl+shift+a",
		"testing.runAtCursor": "ctrl+; c", "testing.debugAtCursor": "ctrl+; ctrl+c", "testing.coverageAtCursor": "ctrl+; ctrl+shift+c",
		"testing.runCurrentFile": "ctrl+; f", "testing.debugCurrentFile": "ctrl+; ctrl+f", "testing.coverageCurrentFile": "ctrl+; ctrl+shift+f",
		"testing.reRunFailTests": "ctrl+; e", "testing.debugFailTests": "ctrl+; ctrl+e",
		"testing.reRunLastRun": "ctrl+; l", "testing.debugLastRun": "ctrl+; ctrl+l", "testing.coverageLastRun": "ctrl+; ctrl+shift+l",
		"testing.showMostRecentOutput": "ctrl+; ctrl+o", "testing.cancelRun": "ctrl+; ctrl+x", "testing.refreshTests": "ctrl+; ctrl+r",
		"testing.editFocusedTest": "alt+enter", "testing.openOutputPeek": "ctrl+; ctrl+m",
		"testing.goToNextMessage": "alt+f8", "testing.goToPreviousMessage": "shift+alt+f8",
	}
	got := map[string]string{}
	for _, it := range DefaultRegistry().Defaults() {
		if len(it.Command) > 8 && it.Command[:8] == "testing." {
			got[it.Command] = it.Keybinding.UserSettingsLabel()
		}
	}
	for cmd, keys := range want {
		if got[cmd] != keys {
			t.Errorf("%s: keys %q, want %q", cmd, got[cmd], keys)
		}
	}
	if len(got) != len(want) {
		t.Errorf("%d testing keys, want %d: %v", len(got), len(want), got)
	}
}

// The chords resolve through the first key and then the second, in a context; the keys that have a condition resolve only when it holds:
// the editor's commands need the text focus, Refresh Tests a refreshable controller and Go to Test the Test Explorer.
func TestTestingChordsResolveWhereTheirConditionHolds(t *testing.T) {
	resolver := NewResolver(DefaultRegistry().Defaults(), nil, nil)
	editor := map[string]any{"editorTextFocus": true}
	tests := []struct {
		name  string
		extra map[string]any
		keys  string
		want  string
	}{
		{"run all anywhere", nil, "ctrl+; a", "testing.runAll"},
		{"debug all anywhere", nil, "ctrl+; ctrl+a", "testing.debugAll"},
		{"the cursor in the editor", editor, "ctrl+; c", "testing.runAtCursor"},
		{"the cursor outside the editor", map[string]any{"editorTextFocus": false}, "ctrl+; c", ""},
		{"the file in the editor", editor, "ctrl+; ctrl+shift+f", "testing.coverageCurrentFile"},
		{"the file outside the editor", map[string]any{"editorTextFocus": false}, "ctrl+; f", ""},
		{"refresh with a refreshable controller", map[string]any{"testing.canRefresh": true}, "ctrl+; ctrl+r", "testing.refreshTests"},
		{"refresh without", nil, "ctrl+; ctrl+r", ""},
		{"alt+enter in the explorer", map[string]any{"focusedView": "workbench.view.testing"}, "alt+enter", "testing.editFocusedTest"},
		{"alt+enter elsewhere", nil, "alt+enter", ""},
		{"alt+f8 in the peek", map[string]any{"editorFocus": true, "testing.isPeekVisible": true}, "alt+f8", "testing.goToNextMessage"},
		{"shift+alt+f8 in the peek", map[string]any{"editorFocus": true, "testing.isPeekVisible": true}, "shift+alt+f8", "testing.goToPreviousMessage"},
		{"alt+f8 with no peek is the problems'", map[string]any{"editorFocus": true}, "alt+f8", "editor.action.marker.next"},
		{"escape in the peek", map[string]any{"testing.isPeekVisible": true}, "escape", "editor.closeTestPeek"},
		{"escape with no peek", nil, "escape", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			kb := mustKB(t, tc.keys)
			var chords []string
			var res Resolution
			for _, c := range kb {
				res = resolver.Resolve(defaultContext(tc.extra), chords, c.Dispatch())
				chords = append(chords, c.Dispatch())
			}
			got := ""
			if res.Kind == KbFound {
				got = res.Command
			}
			if got != tc.want {
				t.Errorf("%s resolves to %q, want %q", tc.keys, got, tc.want)
			}
		})
	}
}
