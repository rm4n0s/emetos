package testing

import (
	"github.com/rm4n0s/glutter/ui"

	coretesting "github.com/rm4n0s/emetos/internal/core/testing"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// StateIcon is the Codicon of a state (testing-passed-icon and the others); a test the controller is working on shows as running. The
// running icon does not turn (decision 1549).
//
// Ported from VS Code: src/vs/workbench/contrib/testing/browser/icons.ts (testingStatesToIcons)
func StateIcon(s coretesting.State) int {
	switch s {
	case coretesting.StateErrored:
		return fonts.CodiconIssues
	case coretesting.StateFailed:
		return fonts.CodiconError
	case coretesting.StatePassed:
		return fonts.CodiconPass
	case coretesting.StateQueued:
		return fonts.CodiconHistory
	case coretesting.StateRunning:
		return fonts.CodiconLoading
	case coretesting.StateSkipped:
		return fonts.CodiconDebugStepOver
	}
	return fonts.CodiconCircle
}

// StateColorID is the theme color of the icon of a state, and of a state of an outdated result.
//
// Ported from VS Code: src/vs/workbench/contrib/testing/browser/theme.ts (testStatesToIconColors, testStatesToRetiredIconColors)
func StateColorID(s coretesting.State, retired bool) string {
	id := ""
	switch s {
	case coretesting.StateErrored:
		id = "testing.iconErrored"
	case coretesting.StateFailed:
		id = "testing.iconFailed"
	case coretesting.StatePassed:
		id = "testing.iconPassed"
	case coretesting.StateQueued:
		id = "testing.iconQueued"
	case coretesting.StateSkipped:
		id = "testing.iconSkipped"
	case coretesting.StateUnset:
		id = "testing.iconUnset"
	default: // running keeps the foreground of the text
		return ""
	}
	if retired {
		return id + ".retired"
	}
	return id
}

// StateColor is the color of the icon of a state in the colors of a theme; the foreground for a state with none.
func StateColor(colors kit.Colors, s coretesting.State, retired bool) ui.Color {
	if id := StateColorID(s, retired); id != "" {
		if c, ok := colors.Named(id); ok && c.Alpha() != 0 {
			return c
		}
	}
	return colors.Foreground
}

// StateName is how a state is called in a hover ("Passed", "Not yet run").
func StateName(s coretesting.State) string {
	switch s {
	case coretesting.StateErrored:
		return "Errored"
	case coretesting.StateFailed:
		return "Failed"
	case coretesting.StatePassed:
		return "Passed"
	case coretesting.StateQueued:
		return "Queued"
	case coretesting.StateRunning:
		return "Running"
	case coretesting.StateSkipped:
		return "Skipped"
	}
	return "Not yet run"
}
