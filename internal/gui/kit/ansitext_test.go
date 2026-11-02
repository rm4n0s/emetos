package kit

import (
	"testing"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

var servicesControl = services.LogicalKeyboardKeyControl

var richText = gluttertest.FindOpts{FindRichText: true}

func TestAnsiTextShowsThePlainText(t *testing.T) {
	tests := []struct {
		name, text string
		opts       AnsiTextOpts
		want       string
	}{
		{"colors are not text", "\x1b[31mred\x1b[0m and plain", AnsiTextOpts{}, "red and plain"},
		{"a line feed at the end is trimmed on request", "line\n", AnsiTextOpts{TrimEndNewline: true}, "line"},
		{"and kept otherwise", "line\n", AnsiTextOpts{}, "line\n"},
		{"links keep their text", "see /a/b.go:3 now", AnsiTextOpts{Links: &AnsiLinks{Resolve: func(LinkPart) bool { return true }}}, "see /a/b.go:3 now"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := pumpThemed(t, DarkModern(), ui.Size{Width: 300, Height: 100}, NewAnsiText(tt.text, tt.opts))
			if !ts.Exists(gluttertest.ByText(tt.want, richText)) {
				t.Errorf("no text %q", tt.want)
			}
		})
	}
}

func TestAnsiTextLinksOpenWithControl(t *testing.T) {
	var opened []LinkPart
	links := &AnsiLinks{
		Resolve: func(p LinkPart) bool { return p.Kind == LinkWeb },
		Open:    func(p LinkPart) { opened = append(opened, p) },
	}
	ts := pumpThemed(t, DarkModern(), ui.Size{Width: 400, Height: 100}, NewAnsiText("go to https://example.com/x now", AnsiTextOpts{Links: links}))
	at := ts.TopLeft(gluttertest.ByType[*AnsiText]())
	click := ui.Offset{Dx: at.Dx + 60, Dy: at.Dy + 6}
	ts.TapAt(click)
	if len(opened) != 0 {
		t.Fatalf("a click without Control opened %v", opened)
	}
	ts.SendKeyDownEvent(servicesControl)
	ts.TapAt(click)
	ts.SendKeyUpEvent(servicesControl)
	if len(opened) != 1 || opened[0].Value != "https://example.com/x" {
		t.Errorf("opened %v", opened)
	}
}

func TestGoldenAnsiText(t *testing.T) {
	text := "\x1b[1mbold\x1b[0m \x1b[3mitalic\x1b[0m \x1b[4munderline\x1b[0m \x1b[9mstrike\x1b[0m \x1b[2mdim\x1b[0m\n" +
		"\x1b[30mblack \x1b[31mred \x1b[32mgreen \x1b[33myellow \x1b[34mblue \x1b[35mmagenta \x1b[36mcyan \x1b[37mwhite\x1b[0m\n" +
		"\x1b[90mgray \x1b[91mbrred \x1b[92mbrgreen \x1b[93mbryellow \x1b[94mbrblue \x1b[95mbrmagenta \x1b[96mbrcyan \x1b[97mbrwhite\x1b[0m\n" +
		"\x1b[38;5;208m256 orange\x1b[0m \x1b[38;2;120;200;80mtrue color\x1b[0m \x1b[41;97m white on red \x1b[0m \x1b[7mreversed\x1b[0m\n" +
		"at /work/src/main.go:12:3 and https://example.com"
	for _, th := range []struct {
		name   string
		colors func() Colors
	}{{"dark", DarkModern}, {"light", LightModern}} {
		t.Run(th.name, func(t *testing.T) {
			colors := th.colors()
			links := &AnsiLinks{Resolve: func(LinkPart) bool { return true }}
			ts := pumpThemed(t, colors, ui.Size{Width: 560, Height: 130}, padded(NewAnsiText(text, AnsiTextOpts{Links: links, Background: foundation.Some(colors.EditorBackground)})))
			testutil.MatchesGolden(ts, "ansitext_"+th.name)
		})
	}
}
