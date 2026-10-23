package editor

// Ported from VS Code: src/vs/workbench/contrib/codeEditor/browser/toggleWordWrap.ts

// wrapActions adds the command that toggles word wrap to the table of the editor's commands.
func wrapActions(a map[string]action) {
	// Toggle Word Wrap turns wrapping on for this editor when it is off and off when it is on,
	// whatever the setting says; the setting is not changed.
	a["editor.action.toggleWordWrap"] = func(vm *ViewModel, _ any) error {
		vm.skipReveal = true
		if vm.Wrapping() {
			vm.SetWordWrapOverride("off")
		} else {
			vm.SetWordWrapOverride("on")
		}
		return nil
	}
}
