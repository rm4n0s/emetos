package keybinding

// registerTestingKeys registers the keys of Testing (Linux): the Ctrl+; chords that run, debug and cover tests, rerun the last run or its
// failures, show the output, cancel a run and refresh the tests, and Alt+Enter on a test of the Test Explorer. The keys of the peek (step
// 26.3) are registered by that step.
//
// Ported from VS Code: src/vs/workbench/contrib/testing/browser/testExplorerActions.ts (the keybinding of each action)
func registerTestingKeys(r *Registry) {
	const explorer = "focusedView == 'workbench.view.testing'"
	r.MustRegister(
		Rule{Command: "testing.runAll", Weight: WeightWorkbenchContrib, Primary: "ctrl+; a"},
		Rule{Command: "testing.debugAll", Weight: WeightWorkbenchContrib, Primary: "ctrl+; ctrl+a"},
		Rule{Command: "testing.coverageAll", Weight: WeightWorkbenchContrib, Primary: "ctrl+; ctrl+shift+a"},
		Rule{Command: "testing.runAtCursor", Weight: WeightWorkbenchContrib, When: mustWhen(whenEditorText), Primary: "ctrl+; c"},
		Rule{Command: "testing.debugAtCursor", Weight: WeightWorkbenchContrib, When: mustWhen(whenEditorText), Primary: "ctrl+; ctrl+c"},
		Rule{Command: "testing.coverageAtCursor", Weight: WeightWorkbenchContrib, When: mustWhen(whenEditorText), Primary: "ctrl+; ctrl+shift+c"},
		Rule{Command: "testing.runCurrentFile", Weight: WeightWorkbenchContrib, When: mustWhen(whenEditorText), Primary: "ctrl+; f"},
		Rule{Command: "testing.debugCurrentFile", Weight: WeightWorkbenchContrib, When: mustWhen(whenEditorText), Primary: "ctrl+; ctrl+f"},
		Rule{Command: "testing.coverageCurrentFile", Weight: WeightWorkbenchContrib, When: mustWhen(whenEditorText), Primary: "ctrl+; ctrl+shift+f"},
		Rule{Command: "testing.reRunFailTests", Weight: WeightWorkbenchContrib, Primary: "ctrl+; e"},
		Rule{Command: "testing.debugFailTests", Weight: WeightWorkbenchContrib, Primary: "ctrl+; ctrl+e"},
		Rule{Command: "testing.reRunLastRun", Weight: WeightWorkbenchContrib, Primary: "ctrl+; l"},
		Rule{Command: "testing.debugLastRun", Weight: WeightWorkbenchContrib, Primary: "ctrl+; ctrl+l"},
		Rule{Command: "testing.coverageLastRun", Weight: WeightWorkbenchContrib, Primary: "ctrl+; ctrl+shift+l"},
		Rule{Command: "testing.showMostRecentOutput", Weight: WeightWorkbenchContrib, Primary: "ctrl+; ctrl+o"},
		Rule{Command: "testing.cancelRun", Weight: WeightWorkbenchContrib, Primary: "ctrl+; ctrl+x"},
		Rule{Command: "testing.refreshTests", Weight: WeightWorkbenchContrib, When: mustWhen("testing.canRefresh"), Primary: "ctrl+; ctrl+r"},
		Rule{Command: "testing.editFocusedTest", Weight: WeightEditorContrib - 10, When: mustWhen(explorer), Primary: "alt+enter"},
	)
}
