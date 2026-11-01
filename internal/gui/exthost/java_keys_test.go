package exthost

import (
	"testing"

	javapack "github.com/rm4n0s/emetos/extensions/java"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
)

// The keys of the Java pack as the window's keymap resolves them (SPEC §10.34 "Java"): Shift+Alt+O runs java.action.organizeImports in a
// Java file with the focus in the editor (not in a read-only class file) and the editor's own command in any other file; Shift+Alt+U runs
// java.projectConfiguration.update in a Java file or a build file; and the user's keybindings.json still wins.
func TestTheKeysOfTheJavaPackReplaceTheEditorsOnlyInJavaFiles(t *testing.T) {
	r := newRig(t, javapack.New)
	tb := r.newTab("one")
	tb.scope.Start()
	var items []keybinding.Item
	tb.keys.mu.Lock()
	for _, it := range tb.keys.items {
		items = append(items, it)
	}
	tb.keys.mu.Unlock()
	if len(items) != 2 {
		t.Fatalf("the pack added %d keybindings: %+v", len(items), items)
	}
	resolver := keybinding.NewResolver(keybinding.DefaultRegistry().Defaults(), items, nil)
	tests := []struct {
		name string
		key  string
		ctx  contextkey.MapContext
		want string
	}{
		{"organize imports in a Java file", "shift+alt+O", contextkey.MapContext{"editorTextFocus": true, "editorLangId": "java"}, javapack.OrganizeImportsCommand},
		{"organize imports in a class file", "shift+alt+O", contextkey.MapContext{"editorTextFocus": true, "textInputFocus": true, "editorReadonly": true, "editorLangId": "java",
			"supportedCodeAction": "source.organizeImports"}, ""},
		{"organize imports in another language", "shift+alt+O", contextkey.MapContext{"editorTextFocus": true, "textInputFocus": true, "editorLangId": "go",
			"editorHasCodeActionsProvider": true, "supportedCodeAction": "source.organizeImports"}, "editor.action.organizeImports"},
		{"update in a Java file", "shift+alt+U", contextkey.MapContext{"editorTextFocus": true, "editorLangId": "java"}, javapack.UpdateProjectConfigurationCommand},
		{"update in a pom.xml", "shift+alt+U", contextkey.MapContext{"editorTextFocus": true, "editorLangId": "xml", "resourceFilename": "pom.xml"}, javapack.UpdateProjectConfigurationCommand},
		{"update in a build.gradle", "shift+alt+U", contextkey.MapContext{"editorTextFocus": true, "editorLangId": "groovy", "resourceFilename": "build.gradle"}, javapack.UpdateProjectConfigurationCommand},
		{"update in another file", "shift+alt+U", contextkey.MapContext{"editorTextFocus": true, "editorLangId": "go"}, ""},
		{"update without the focus in the editor", "shift+alt+U", contextkey.MapContext{"editorLangId": "java"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolver.Resolve(tt.ctx, nil, tt.key)
			if tt.want == "" {
				if got.Kind == keybinding.KbFound {
					t.Errorf("%s runs %s", tt.key, got.Command)
				}
				return
			}
			if got.Kind != keybinding.KbFound || got.Command != tt.want {
				t.Errorf("%s = %+v, want %s", tt.key, got, tt.want)
			}
		})
	}
	user := keybinding.NewResolver(keybinding.DefaultRegistry().Defaults(), append(items, userItem(t, "shift+alt+o", "workbench.action.quickOpen")), nil)
	if got := user.Resolve(contextkey.MapContext{"editorTextFocus": true, "editorLangId": "java"}, nil, "shift+alt+O"); got.Command != "workbench.action.quickOpen" {
		t.Errorf("shift+alt+o with the user's binding = %+v", got)
	}
}
