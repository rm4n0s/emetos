package app

import (
	"slices"
	"testing"

	"github.com/rm4n0s/emetos/extensions/containers"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
)

// The Docker extension in a workspace (step 18.3, SPEC §10.20): its entries of the context menus of the Explorer and of the editor
// appear for a Dockerfile and for a Compose file, by the language id of the row and of the editor.

// menuCommands lists the commands of a menu in a context.
func menuCommands(w *Workspace, menu string, keys map[string]any) []string {
	ctx := contextkey.NewRoot()
	for k, v := range keys {
		ctx.Set(k, v)
	}
	var out []string
	for _, e := range w.Menus.Items(menu, ctx) {
		if e.Command != "" {
			out = append(out, e.Command)
		}
	}
	return out
}

func TestTheDockerMenusOfTheExplorerAndTheEditorFollowTheLanguage(t *testing.T) {
	p := extensionProcess(t, containers.New)
	w := newExtensionWorkspace(t, p, "", "go.mod")
	compose := []string{"containers.compose.up", "containers.compose.up.subset", "containers.compose.down", "containers.compose.down.subset", "containers.compose.restart", "containers.compose.logs"}
	tests := []struct {
		name string
		menu string
		keys map[string]any
		want []string
	}{
		{"a Dockerfile in the Explorer", command.ExplorerContext, map[string]any{"resourceLangId": "dockerfile"}, []string{"containers.images.build"}},
		{"a Compose file in the Explorer", command.ExplorerContext, map[string]any{"resourceLangId": "dockercompose"}, compose},
		{"another file in the Explorer", command.ExplorerContext, map[string]any{"resourceLangId": "go"}, nil},
		{"a folder in the Explorer", command.ExplorerContext, map[string]any{"resourceLangId": ""}, nil},
		{"a Dockerfile in the editor", command.EditorContext, map[string]any{"editorTextFocus": true, "editorLangId": "dockerfile"}, []string{"containers.images.build"}},
		{"a Compose file in the editor", command.EditorContext, map[string]any{"editorTextFocus": true, "editorLangId": "dockercompose"}, compose},
		{"a Go file in the editor", command.EditorContext, map[string]any{"editorTextFocus": true, "editorLangId": "go"}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, c := range menuCommands(w, tc.menu, tc.keys) {
				if len(c) > len("containers.") && c[:len("containers.")] == "containers." {
					got = append(got, c)
				}
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("%v, want %v", got, tc.want)
			}
		})
	}
}

func TestTheCommandsOfTheDockerWorkflowsAreExtensionCommands(t *testing.T) {
	p := extensionProcess(t, containers.New)
	w := newExtensionWorkspace(t, p, "", "go.mod")
	scope := w.Extensions()
	for _, id := range []string{"containers.images.build", "containers.compose.up", "containers.configure", "containers.containers.attachShell"} {
		if !scope.IsExtensionCommand(id) {
			t.Errorf("%s is not a command of an extension", id)
		}
	}
	if scope.IsExtensionCommand("explorer.newFile") {
		t.Error("the editor's command counts as an extension's")
	}
}
