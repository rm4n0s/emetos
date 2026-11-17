package app

import (
	"testing"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/keybinding"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// Ctrl+A in selectable text (the page of the Markdown preview, a widgets.SelectableRegion) is left to the text, as in a text
// field; the other keys of the editor and the keys outside such text are not (the user, 2026-11-17).
func TestSelectAllIsLeftToSelectableText(t *testing.T) {
	ts := testutil.New(t, ui.Size{Width: 300, Height: 200})
	inRegion := widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "in region"})
	outside := widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "outside"})
	t.Cleanup(func() { inRegion.Dispose(); outside.Dispose() })
	box := func(n *widgets.FocusNode) widgets.Widget {
		return widgets.NewFocus(widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(50.0), Height: foundation.Some(20.0)}),
			widgets.FocusOpts{FocusNode: n})
	}
	ts.PumpWidget(widgets.NewDirectionality(ui.TextDirectionLTR, widgets.NewColumn([]widgets.Widget{
		widgets.NewSelectableRegion(widgets.NewEmptyTextSelectionControls(), widgets.NewColumn([]widgets.Widget{
			widgets.NewText("selectable"), box(inRegion),
		})),
		box(outside),
	})))
	ctrlA := keybinding.Chord{Ctrl: true, Key: keybinding.KeyA}
	tests := []struct {
		name  string
		chord keybinding.Chord
		focus *widgets.FocusNode
		want  bool
	}{
		{"Ctrl+A in selectable text", ctrlA, inRegion, true},
		{"Ctrl+A outside it", ctrlA, outside, false},
		{"Ctrl+A with no focus", ctrlA, nil, false},
		{"Ctrl+Shift+A in selectable text", keybinding.Chord{Ctrl: true, Shift: true, Key: keybinding.KeyA}, inRegion, false},
		{"Ctrl+Alt+A in selectable text", keybinding.Chord{Ctrl: true, Alt: true, Key: keybinding.KeyA}, inRegion, false},
		{"Ctrl+Z in selectable text (the editor's undo)", keybinding.Chord{Ctrl: true, Key: keybinding.KeyZ}, inRegion, false},
		{"A in selectable text", keybinding.Chord{Key: keybinding.KeyA}, inRegion, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSelectAllChord(tt.chord) && selectableTextHasFocus(tt.focus); got != tt.want {
				t.Errorf("left to the text = %v, want %v", got, tt.want)
			}
		})
	}
}
