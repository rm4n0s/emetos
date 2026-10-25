package fonts

import (
	"bytes"
	"testing"

	"github.com/go-text/typesetting/font"
	ot "github.com/go-text/typesetting/font/opentype"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"
)

func TestControlsFontHasAGlyphForTheTabOnly(t *testing.T) {
	loaders, err := ot.NewLoaders(bytes.NewReader(ControlsFont()))
	if err != nil || len(loaders) != 1 {
		t.Fatalf("the font is not read: %v, %d faces", err, len(loaders))
	}
	ft, err := font.NewFont(loaders[0])
	if err != nil {
		t.Fatal(err)
	}
	face := font.NewFace(ft)
	tests := []struct {
		name string
		r    rune
		want bool
	}{
		{"the tab", '\t', true},
		{"a letter", 'a', false},
		{"the space", ' ', false},
		{"the line feed", '\n', false},
		{"the character before the tab", '\b', false},
		{"the one after", '\v', false},
		{"a character past the end of the segments", 0x1F600, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, ok := face.NominalGlyph(tt.r)
			if ok != tt.want {
				t.Errorf("NominalGlyph(%q) = %d, %v; want a glyph: %v", tt.r, g, ok, tt.want)
			}
		})
	}
	// the glyphs have an advance and no outline: the tab takes no room of its own where the paragraph
	// has no tab stops, and draws nothing
	g, _ := face.NominalGlyph('\t')
	if adv := face.HorizontalAdvance(g); adv <= 0 {
		t.Errorf("the advance of the tab glyph is %v", adv)
	}
}

func TestControlsFontChecksums(t *testing.T) {
	data := ControlsFont()
	if len(data)%4 != 0 {
		t.Errorf("the font is %d bytes: the tables are not padded to 4", len(data))
	}
	n := int(data[4])<<8 | int(data[5])
	for i := range n {
		rec := data[12+16*i:]
		off := int(rec[8])<<24 | int(rec[9])<<16 | int(rec[10])<<8 | int(rec[11])
		length := int(rec[12])<<24 | int(rec[13])<<16 | int(rec[14])<<8 | int(rec[15])
		if off+length > len(data) {
			t.Fatalf("table %q is outside the font", rec[:4])
		}
		got := uint32(rec[4])<<24 | uint32(rec[5])<<16 | uint32(rec[6])<<8 | uint32(rec[7])
		if want := checksum(data[off : off+length]); got != want {
			t.Errorf("table %q: checksum %#x, want %#x", rec[:4], got, want)
		}
	}
}

func TestControlsFontLoadsInAnEngine(t *testing.T) {
	ts := gluttertest.New(t, gluttertest.Opts{Fonts: []gluttertest.Font{{Family: Controls, Data: ControlsFont()}}})
	got, ok := ts.Binding.PlatformDispatcher().ResolveFont(Controls, ui.FontWeightW400, ui.FontStyleNormal)
	if !ok || got.Origin != ui.FontOriginLoaded {
		t.Errorf("the family %q resolves to %+v, %v", Controls, got, ok)
	}
}
