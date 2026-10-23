package editor

import (
	"testing"

	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/fileio"
)

// TestLargeFileModeSwitchesTheFeaturesOff is the table of SPEC §6.4: what the editor does with a
// document in large file mode and with one that is not.
func TestLargeFileModeSwitchesTheFeaturesOff(t *testing.T) {
	cases := []struct {
		name  string
		large bool
		// want is what each feature reports.
		wrapping, sticky, highlights bool
	}{
		{"normal file", false, true, true, true},
		{"large file", true, false, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := document.FromRead(document.FileURI("/f.txt"),
				fileio.ReadResult{Text: "line\n" + minimapSample(40), LineEnding: fileio.LF, LargeFile: tc.large}, document.NewOptions())
			opts := goOptions()
			opts.WordWrap = "on"
			opts.StickyScroll = StickyScrollOptions{Enabled: true, MaxLineCount: 5, ScrollWithEditor: true}
			vm := NewViewModel(doc, opts, nil)
			vm.SetViewportHeight(300)
			vm.SetTextMetrics(400, 300, 1000, 100, 8)
			if got := vm.Wrapping(); got != tc.wrapping {
				t.Errorf("wrapping %v, want %v", got, tc.wrapping)
			}
			if got := vm.maxStickyLines() > 0; got != tc.sticky {
				t.Errorf("sticky scroll %v, want %v", got, tc.sticky)
			}
			if got := vm.highlightsEnabled(); got != tc.highlights {
				t.Errorf("highlights %v, want %v", got, tc.highlights)
			}
		})
	}
}
