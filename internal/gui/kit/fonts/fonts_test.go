package fonts

import (
	"testing"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
)

func TestSources(t *testing.T) {
	sources := Sources()
	want := []string{Codicon, Seti}
	if len(sources) != len(want) {
		t.Fatalf("Sources has %d fonts, want %d", len(sources), len(want))
	}
	for i, src := range sources {
		if src.Family != want[i] {
			t.Errorf("Sources[%d].Family = %q, want %q", i, src.Family, want[i])
		}
		if len(src.Data) < 10_000 {
			t.Errorf("%s: %d bytes of font data, want a real font", src.Family, len(src.Data))
		}
	}
}

// TestEveryFontLoads loads each bundled font into a test engine. A font that
// Glutter can't read (the WOFF of Seti, for one) fails the test, and then the
// family must resolve to the loaded font.
func TestEveryFontLoads(t *testing.T) {
	for _, src := range Sources() {
		t.Run(src.Family, func(t *testing.T) {
			ts := gluttertest.New(t, gluttertest.Opts{
				Fonts: []gluttertest.Font{{Family: src.Family, Data: src.Data}},
			})
			got, ok := ts.Binding.PlatformDispatcher().ResolveFont(src.Family, ui.FontWeightW400, ui.FontStyleNormal)
			if !ok {
				t.Fatalf("the family %q does not resolve to a font", src.Family)
			}
			if got.Origin != ui.FontOriginLoaded {
				t.Errorf("the family %q resolves to a font with origin %v, want the loaded one", src.Family, got.Origin)
			}
		})
	}
}

func TestCodiconIcon(t *testing.T) {
	icon := CodiconIcon(CodiconCode)
	if icon.CodePoint != 0xEAC4 || icon.FontFamily != Codicon {
		t.Errorf("CodiconIcon(CodiconCode) = %+v, want code point 0xEAC4 in the family %q", icon, Codicon)
	}
}

// TestIconsDraw puts a codicon in a widget tree: the font is loaded, the icon builds
// and lays out without the framework reporting an error.
func TestIconsDraw(t *testing.T) {
	ts := gluttertest.New(t, gluttertest.Opts{
		Fonts: []gluttertest.Font{{Family: Codicon, Data: codiconData}},
	})
	ts.PumpWidget(widgets.NewDirectionality(ui.TextDirectionLTR,
		widgets.NewIcon(CodiconIcon(CodiconCode), widgets.IconOpts{Size: foundation.Some(32.0)})))
	if errs := ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("the framework reported errors: %v", errs)
	}
	if err := gluttertest.FindsOneWidget(ts, gluttertest.ByType[*widgets.Icon]()); err != nil {
		t.Error(err)
	}
}

func TestCodiconByName(t *testing.T) {
	tests := []struct {
		name string
		want int
		ok   bool
	}{
		{"bell", CodiconBell, true},
		{"bell-dot", CodiconBellDot, true},
		{"bell-slash-dot", CodiconBellSlashDot, true},
		{"error", CodiconError, true},
		{"not-an-icon", 0, false},
		{"", 0, false},
		{"Bell", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := CodiconByName(tt.name)
			if got != tt.want || ok != tt.ok {
				t.Errorf("CodiconByName(%q) = %#x, %v; want %#x, %v", tt.name, got, ok, tt.want, tt.ok)
			}
		})
	}
}
