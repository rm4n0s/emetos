package edit

import (
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
)

// Ported from VS Code: reference/vscode/src/vs/editor/contrib/linesOperations/test/browser/
// copyLinesCommand.test.ts (8 of 10: the other 2 are Duplicate Selection),
// moveLinesCommand.test.ts (11 calls of the first suite; the others need
// indentation rules) and sortLinesCommand.test.ts (7).
func TestVSCodeLineCommands(t *testing.T) {
	for _, tt := range lineCases {
		t.Run(tt.file+"/"+tt.name, func(t *testing.T) {
			e := newEd(t, DefaultOptions(), tt.lines...).at(tt.sel)
			e.run(func(c Commands, s []buffer.Selection) Result {
				switch tt.op {
				case "copyDown":
					return c.CopyLines(s, true)
				case "copyUp":
					return c.CopyLines(s, false)
				case "moveDown":
					return c.MoveLines(s, true)
				case "moveUp":
					return c.MoveLines(s, false)
				case "sortAscending":
					return c.sortLines(s, false, false)
				case "sortDescending":
					return c.sortLines(s, true, false)
				}
				t.Fatalf("unknown operation %q", tt.op)
				return Result{}
			})
			e.assertText(strings.Join(tt.want, "\n"))
			e.assertSels(tt.wantSel)
		})
	}
}
