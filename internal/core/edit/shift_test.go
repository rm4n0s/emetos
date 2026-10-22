package edit

import (
	"strings"
	"testing"
)

// Ported from VS Code: reference/vscode/src/vs/editor/test/browser/commands/shiftCommand.test.ts
// (21 of its 25 calls; the 4 that need the on-enter rules of a language are
// skipped). Tab size 4, tabs for indentation, both with and without tab stops.
func TestVSCodeShiftCommand(t *testing.T) {
	for _, tt := range shiftCases {
		t.Run(tt.name, func(t *testing.T) {
			opts := tabsOptions()
			opts.UseTabStops = tt.useTabStops
			e := newEd(t, opts, tt.lines...).at(tt.sel)
			if tt.unshift {
				e.outdent()
			} else {
				e.indent()
			}
			e.assertText(strings.Join(tt.want, "\n"))
			e.assertSels(tt.wantSel)
		})
	}
}
