package exthost

import (
	"testing"

	"github.com/rm4n0s/emetos/extensions/cpp"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
)

// The key of the C and C++ pack as the window's keymap resolves it (SPEC §10.34 "C and C++"): Alt+O runs clangd.switchheadersource in
// a C, C++ or CUDA file with the focus in the editor, over the default keymap of Emetos, and nowhere else.
func TestAltOSwitchesBetweenSourceAndHeaderInTheFilesOfClangd(t *testing.T) {
	r := newRig(t, cpp.New)
	tb := r.newTab("one")
	tb.scope.Start()
	var items []keybinding.Item
	tb.keys.mu.Lock()
	for _, it := range tb.keys.items {
		items = append(items, it)
	}
	tb.keys.mu.Unlock()
	if len(items) != 1 || items[0].Command != cpp.SwitchHeaderSourceCommand {
		t.Fatalf("the pack added %d keybindings: %+v", len(items), items)
	}
	resolver := keybinding.NewResolver(keybinding.DefaultRegistry().Defaults(), items, nil)
	tests := []struct {
		name string
		ctx  contextkey.MapContext
		want string
	}{
		{"a C++ file", contextkey.MapContext{"editorTextFocus": true, "editorLangId": "cpp"}, cpp.SwitchHeaderSourceCommand},
		{"a C file", contextkey.MapContext{"editorTextFocus": true, "editorLangId": "c"}, cpp.SwitchHeaderSourceCommand},
		{"a CUDA file", contextkey.MapContext{"editorTextFocus": true, "editorLangId": "cuda-cpp"}, cpp.SwitchHeaderSourceCommand},
		{"a Rust file", contextkey.MapContext{"editorTextFocus": true, "editorLangId": "rust"}, ""},
		{"a C++ file without the focus in the editor", contextkey.MapContext{"editorLangId": "cpp"}, ""},
		{"nothing open", contextkey.MapContext{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolver.Resolve(tt.ctx, nil, "alt+O")
			if tt.want == "" {
				if got.Kind == keybinding.KbFound {
					t.Errorf("alt+o runs %s", got.Command)
				}
				return
			}
			if got.Kind != keybinding.KbFound || got.Command != tt.want {
				t.Errorf("alt+o = %+v, want %s", got, tt.want)
			}
		})
	}
	// the user's keybindings.json still wins over the pack's default
	user := keybinding.NewResolver(keybinding.DefaultRegistry().Defaults(), append(items, userItem(t, "alt+o", "workbench.action.quickOpen")), nil)
	if got := user.Resolve(contextkey.MapContext{"editorTextFocus": true, "editorLangId": "cpp"}, nil, "alt+O"); got.Command != "workbench.action.quickOpen" {
		t.Errorf("alt+o with the user's binding = %+v", got)
	}
}

// userItem is a keybinding of the user's keybindings.json.
func userItem(t *testing.T, key, command string) keybinding.Item {
	t.Helper()
	kb, err := keybinding.ParseKeybinding(key)
	if err != nil {
		t.Fatal(err)
	}
	return keybinding.NewItem(kb, command, nil, nil, false)
}
