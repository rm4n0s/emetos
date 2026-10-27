package pageui_test

import (
	"testing"

	"github.com/rm4n0s/glutter/gluttertest"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/internal/pageui"
)

func TestNotes(t *testing.T) {
	for _, tc := range []struct {
		name string
		note pageui.Note
		text string
		bad  bool
	}{
		{"fine", pageui.Fine("Saved."), "Saved.", false},
		{"bad", pageui.Bad("Cannot save."), "Cannot save.", true},
		{"empty", pageui.Fine(""), "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.note.Text != tc.text || tc.note.Bad != tc.bad {
				t.Errorf("note = %+v, want text %q bad %v", tc.note, tc.text, tc.bad)
			}
		})
	}
}

func TestTheWidgetsShowTheirText(t *testing.T) {
	th := uitest.Themes()[0]
	for _, tc := range []struct {
		name  string
		build func(bc widgets.BuildContext) widgets.Widget
		texts []string
		none  []string
	}{
		{"a section", func(bc widgets.BuildContext) widgets.Widget {
			return pageui.Section(bc, th.Colors, "Be a Host", pageui.Paragraph("a row", ui.TextStyle(bc, 13, th.Colors.Foreground)))
		}, []string{"Be a Host", "a row"}, nil},
		{"a fine note", func(bc widgets.BuildContext) widgets.Widget {
			return pageui.NoteLine(bc, th.Colors, pageui.Fine("Added."))
		}, []string{"Added."}, nil},
		{"a bad note", func(bc widgets.BuildContext) widgets.Widget {
			return pageui.NoteLine(bc, th.Colors, pageui.Bad("Refused."))
		}, []string{"Refused."}, nil},
		{"an empty note shows nothing", func(bc widgets.BuildContext) widgets.Widget {
			return pageui.NoteLine(bc, th.Colors, pageui.Note{})
		}, nil, []string{"Added."}},
		{"a problem", func(bc widgets.BuildContext) widgets.Widget {
			return pageui.Problem(bc, th.Colors, "It broke.")
		}, []string{"It broke."}, nil},
		{"the mono family", func(bc widgets.BuildContext) widgets.Widget {
			return pageui.Paragraph(pageui.MonoFamily(bc), nil)
		}, []string{uitest.MonoFamily}, nil},
		{"gaps and nothing", func(bc widgets.BuildContext) widgets.Widget {
			return widgets.NewRow([]widgets.Widget{pageui.Gap(4), pageui.HGap(4), pageui.Nothing(), pageui.Paragraph("x", pageui.Bold(ui.TextStyle(bc, 13, th.Colors.Foreground)))})
		}, []string{"x"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts := uitest.Pump(t, th, gui.Size{Width: 400, Height: 200}, widgets.NewBuilder(tc.build))
			for _, s := range tc.texts {
				if !ts.Exists(gluttertest.ByText(s)) {
					t.Errorf("%q is not shown", s)
				}
			}
			for _, s := range tc.none {
				if ts.Exists(gluttertest.ByText(s)) {
					t.Errorf("%q is shown", s)
				}
			}
		})
	}
}

func TestFieldShowsItsLabelAndTakesText(t *testing.T) {
	th := uitest.Themes()[0]
	c := ui.NewInputBoxController("")
	ts := uitest.Pump(t, th, gui.Size{Width: 400, Height: 200}, widgets.NewBuilder(func(bc widgets.BuildContext) widgets.Widget {
		return pageui.Field(bc, th.Colors, "Port", c, 120, false, func() {})
	}))
	if !ts.Exists(gluttertest.ByText("Port")) {
		t.Fatal("the label is not shown")
	}
	ts.EnterText(gluttertest.ByType[*ui.InputBox](), "1234")
	if got := c.Value(); got != "1234" {
		t.Errorf("text = %q", got)
	}
}
