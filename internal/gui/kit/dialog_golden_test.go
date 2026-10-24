package kit

import (
	"testing"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// dialogBackdrop is what a dialog covers in a golden: some text and a button.
func dialogBackdrop() widgets.Widget {
	return padded(themed(func(bc widgets.BuildContext) widgets.Widget {
		style := TextStyleOf(bc, 13, ThemeOf(bc).EditorForeground)
		return column(
			widgets.NewText("package main", widgets.TextOpts{Style: style}), vgap(6),
			widgets.NewText("func main() { println(\"hello\") }", widgets.TextOpts{Style: style}), vgap(14),
			NewButton("Below", ButtonOpts{OnPressed: func() {}}),
		)
	}))
}

func TestGoldenDialog(t *testing.T) {
	cases := []struct {
		name string
		spec DialogSpec
	}{
		{"info", DialogSpec{
			Type: DialogInfo, Message: "Emetos 0.1.0", Detail: "Commit: 1a2b3c4\nDate: 2026-10-24\nGo: go1.24\nOS: Linux x64",
			Buttons:       []string{"&&OK", "&&Copy"},
			CheckboxLabel: "Do not show this again", CheckboxChecked: true,
		}},
		{"error", DialogSpec{
			Type: DialogError, Message: "Unable to save 'main.go'", Detail: "The file is read-only. Try to overwrite it or save it with another name.",
			Buttons: []string{"&&Retry", "Save &&As..."}, CancelButton: foundation.Some(1),
		}},
		{"unsaved", saveSpec},
	}
	for _, th := range goldenThemes {
		for _, tc := range cases {
			t.Run(th.name+" "+tc.name, func(t *testing.T) {
				ctrl := NewDialogController()
				host := NewDialogHost(dialogBackdrop(), ctrl)
				ts := pumpThemed(t, th.colors(), ui.Size{Width: 560, Height: 300}, widgets.NewSizedBox(host, widgets.SizedBoxOpts{Width: foundation.Some(560.0), Height: foundation.Some(300.0)}))
				ctrl.Show(tc.spec)
				for range 3 {
					ts.Pump(1000000)
				}
				testutil.MatchesGolden(ts, "dialog_"+tc.name+"_"+th.name)
			})
		}
	}
}
