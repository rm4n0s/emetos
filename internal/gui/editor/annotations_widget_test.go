package editor

import (
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// blameHover is the markdown the Git extension gives the hover of a blame text: the author, the message and the links to its commands.
const blameHover = "**Ada Example**, 3 days ago\n\nAdd the loader, with a long enough message to show how the text wraps in the box of a hover\n\n---\n\n" +
	"[Open Commit](command:git.viewCommit?%5B%7B%22id%22%3A%22abc123%22%7D%5D \"Open Commit\") | [Copy Hash](command:git.copyContentToClipboard?%5B%22abc123%22%5D \"Copy Commit Hash\") | [Mail](mailto:ada@example.com)"

// annotationRig is an editor on screen whose lines 0, 4, 5 and 6 have annotations, the one on line 5 with a hover.
func annotationRig(t *testing.T, themeName, hover string, links *[]string, opened *[]string) (*ViewModel, *gluttertest.Tester) {
	t.Helper()
	vm, ts := hoverWidgetRig(t, ui.Size{Width: 640, Height: 300}, themeName, nil)
	vm.SetOpeners(func(u string) { *opened = append(*opened, u) }, nil)
	vm.SetLineAnnotations([]LineAnnotation{
		{Line: 0, Text: "Initial commit, Ada Example (3 days ago)"},
		{Line: 4, Text: "Add main, Grace Hopper (now)", Color: "editorWarning.foreground"},
		{Line: 5, Text: "Print a greeting, Ada Example (3 days ago)", Hover: hover, Link: func(dest string) { *links = append(*links, dest) }},
		{Line: 6, Text: "A subject that goes on far past the right edge of the editor, so that the text is clipped, " + strings.Repeat("and on ", 20)},
	})
	settleWidgets(ts)
	return vm, ts
}

// annotationOffset is the point of the screen on the annotation of a line, a few characters into its text.
func annotationOffset(vm *ViewModel, ts *gluttertest.Tester, line int, chars float64) ui.Offset {
	g := newGeometry(vm, ts.Dispatcher().TextEngine())
	origin := ts.TopLeft(gluttertest.ByType[*textLayer]())
	return ui.Offset{Dx: origin.Dx + g.rowEnd(line) + annotationMargin + chars*g.charWidth, Dy: origin.Dy + float64(line)*19 + 9}
}

func TestGoldenLineAnnotations(t *testing.T) {
	for _, tc := range []struct{ golden, theme string }{
		{"line_annotations_dark", "Dark Modern"},
		{"line_annotations_light", "Light Modern"},
	} {
		t.Run(tc.theme, func(t *testing.T) {
			var links, opened []string
			_, ts := annotationRig(t, tc.theme, blameHover, &links, &opened)
			noErrors(t, ts)
			testutil.MatchesGolden(ts, tc.golden)
		})
	}
}

func TestGoldenLineAnnotationHover(t *testing.T) {
	for _, tc := range []struct{ golden, theme string }{
		{"line_annotation_hover_dark", "Dark Modern"},
		{"line_annotation_hover_light", "Light Modern"},
	} {
		t.Run(tc.theme, func(t *testing.T) {
			var links, opened []string
			vm, ts := annotationRig(t, tc.theme, blameHover, &links, &opened)
			ts.Hover(annotationOffset(vm, ts, 5, 4))
			for range 6 {
				ts.Pump(100 * time.Millisecond)
			}
			settleWidgets(ts)
			noErrors(t, ts)
			if h := vm.Hover(); h == nil || len(h.Parts) != 1 {
				t.Fatalf("hover = %+v", h)
			}
			testutil.MatchesGolden(ts, tc.golden)
		})
	}
}

func TestTheHoverOfAnAnnotationRunsItsCommandLinks(t *testing.T) {
	var links, opened []string
	// one link to a paragraph, so that each is a text of its own to tap
	vm, ts := annotationRig(t, "Dark Modern", "Add the loader\n\n[Open Commit](command:git.viewCommit?%5B%7B%22id%22%3A%22abc123%22%7D%5D)\n\n[Copy Hash](command:git.copyContentToClipboard?%5B%22abc123%22%5D)\n\n[Mail](mailto:ada@example.com)", &links, &opened)
	show := func() {
		ts.Hover(annotationOffset(vm, ts, 5, 4))
		for range 6 {
			ts.Pump(100 * time.Millisecond)
		}
		settleWidgets(ts)
		if vm.Hover() == nil {
			t.Fatal("the hover does not show")
		}
	}
	tap := func(label string) {
		at := ts.TopLeft(richContaining(label))
		ts.TapAt(ui.Offset{Dx: at.Dx + 6, Dy: at.Dy + 8})
		settleWidgets(ts)
	}
	show()
	tap("Open Commit")
	if len(links) != 1 || !strings.HasPrefix(links[0], "command:git.viewCommit?") || vm.Hover() != nil {
		t.Errorf("after Open Commit: links %v, hover %v", links, vm.Hover())
	}
	show()
	tap("Copy Hash")
	if len(links) != 2 || !strings.HasPrefix(links[1], "command:git.copyContentToClipboard?") {
		t.Errorf("after Copy Hash: links %v", links)
	}
	show()
	tap("Mail")
	if len(opened) != 1 || opened[0] != "mailto:ada@example.com" || len(links) != 2 {
		t.Errorf("a link that is not a command opens as every link does: opened %v, commands %v", opened, links)
	}
}

func TestTheHoverOfAnAnnotationHidesWithThePointerAndKeepsForTheSameAnnotation(t *testing.T) {
	var links, opened []string
	vm, ts := annotationRig(t, "Dark Modern", blameHover, &links, &opened)
	at := annotationOffset(vm, ts, 5, 4)
	ts.Hover(at)
	for range 6 {
		ts.Pump(100 * time.Millisecond)
	}
	if vm.Hover() == nil {
		t.Fatal("no hover")
	}
	first := vm.Hover()
	ts.Hover(ui.Offset{Dx: at.Dx + 8, Dy: at.Dy}) // still on the annotation
	ts.Pump(50 * time.Millisecond)
	if vm.Hover() != first {
		t.Error("the hover was made again for the same annotation")
	}
	// an annotation with no hover shows nothing, and the pointer on it takes the old hover away
	ts.Hover(annotationOffset(vm, ts, 4, 4))
	for range 8 {
		ts.Pump(100 * time.Millisecond)
	}
	if vm.Hover() != nil {
		t.Errorf("the annotation with no hover shows %+v", vm.Hover())
	}
	// new annotations take the hover away
	ts.Hover(at)
	for range 6 {
		ts.Pump(100 * time.Millisecond)
	}
	if vm.Hover() == nil {
		t.Fatal("no hover the second time")
	}
	vm.SetLineAnnotations(nil)
	if vm.Hover() != nil {
		t.Error("the hover stays after the annotation is gone")
	}
}

func TestAnnotationsWithoutHoverOptionsOrWithTheHoverOff(t *testing.T) {
	var links, opened []string
	vm, ts := annotationRig(t, "Dark Modern", blameHover, &links, &opened)
	opts := vm.Options()
	opts.Hover.Enabled = "off"
	vm.SetOptions(opts)
	ts.Hover(annotationOffset(vm, ts, 5, 4))
	for range 6 {
		ts.Pump(100 * time.Millisecond)
	}
	if vm.Hover() != nil {
		t.Errorf("editor.hover.enabled off still shows %+v", vm.Hover())
	}
}
