package editordecorationstest

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// OwnCommand is the id of the command that the extension of the harness has: the links of a hover that name it run it.
const OwnCommand = "test.run"

// Harness is what an implementation gives the suite.
type Harness struct {
	// API is the API of the extension under test, in an activation that is on. Its manifest declares OwnCommand, and the extension
	// handles it with Handle: Ran says what it was called with.
	API extension.API
	// Seen is what the editors that show the document would draw, sorted by line.
	Seen func(uri string) []extension.LineAnnotation
	// Follow is a click on the link dest of the hover of the annotation at the line of the document; it does nothing when there is no
	// such annotation.
	Follow func(uri string, line int, dest string)
	// Ran lists the arguments OwnCommand was run with, in order.
	Ran func() []any
	// Pump lets the work posted to the UI thread run.
	Pump func()
	// Deactivate ends the activation, as closing the window tab does.
	Deactivate func()
}

const doc = "file:///r/a.go"

// Run runs the suite: newHarness makes a fresh harness for each test.
func Run(t *testing.T, newHarness func(t *testing.T) *Harness) {
	t.Helper()
	tests := []struct {
		name string
		run  func(t *testing.T, h *Harness)
	}{
		{"the annotations of a key reach the editors, sorted by line", testSet},
		{"a key set again replaces its set, also in another document", testReplace},
		{"an empty list and Clear remove the set", testRemove},
		{"two keys draw together", testTwoKeys},
		{"an invalid set is refused", testInvalid},
		{"a long text and a long hover are cut", testCut},
		{"a link to a command of the extension runs it, with its arguments", testOwnLink},
		{"a link to another command, and every other link, is ignored", testForeignLink},
		{"the end of the activation takes the sets away and Set fails", testDeactivate},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { tc.run(t, newHarness(t)) })
	}
}

func lines(list []extension.LineAnnotation) []int {
	var out []int
	for _, a := range list {
		out = append(out, a.Line)
	}
	return out
}

func testSet(t *testing.T, h *Harness) {
	ed := h.API.EditorDecorations()
	if err := ed.Set("blame", doc, []extension.LineAnnotation{
		{Line: 7, Text: "later", Color: "c.two", Hover: "**hover**"}, {Line: 2, Text: "earlier", Color: "c.one"},
	}); err != nil {
		t.Fatal(err)
	}
	h.Pump()
	want := []extension.LineAnnotation{{Line: 2, Text: "earlier", Color: "c.one"}, {Line: 7, Text: "later", Color: "c.two", Hover: "**hover**"}}
	if got := h.Seen(doc); !reflect.DeepEqual(got, want) {
		t.Errorf("seen %+v, want %+v", got, want)
	}
	if got := h.Seen("file:///r/other.go"); len(got) != 0 {
		t.Errorf("another document shows %+v", got)
	}
}

func testReplace(t *testing.T, h *Harness) {
	ed := h.API.EditorDecorations()
	mustSet(t, ed, "blame", doc, []extension.LineAnnotation{{Line: 1, Text: "a"}})
	mustSet(t, ed, "blame", doc, []extension.LineAnnotation{{Line: 3, Text: "b"}})
	if got := lines(h.Seen(doc)); !reflect.DeepEqual(got, []int{3}) {
		t.Errorf("after a second Set: lines %v", got)
	}
	mustSet(t, ed, "blame", "file:///r/b.go", []extension.LineAnnotation{{Line: 4, Text: "c"}})
	if got := h.Seen(doc); len(got) != 0 {
		t.Errorf("the first document still shows %+v", got)
	}
	if got := lines(h.Seen("file:///r/b.go")); !reflect.DeepEqual(got, []int{4}) {
		t.Errorf("the second document: lines %v", got)
	}
}

func testRemove(t *testing.T, h *Harness) {
	ed := h.API.EditorDecorations()
	mustSet(t, ed, "a", doc, []extension.LineAnnotation{{Line: 1, Text: "a"}})
	mustSet(t, ed, "a", doc, nil)
	if got := h.Seen(doc); len(got) != 0 {
		t.Errorf("an empty list left %+v", got)
	}
	mustSet(t, ed, "a", doc, []extension.LineAnnotation{{Line: 1, Text: "a"}})
	ed.Clear("a")
	ed.Clear("never-set")
	if got := h.Seen(doc); len(got) != 0 {
		t.Errorf("Clear left %+v", got)
	}
}

func testTwoKeys(t *testing.T, h *Harness) {
	ed := h.API.EditorDecorations()
	mustSet(t, ed, "a", doc, []extension.LineAnnotation{{Line: 5, Text: "a"}})
	mustSet(t, ed, "b", doc, []extension.LineAnnotation{{Line: 1, Text: "b"}})
	if got := lines(h.Seen(doc)); !reflect.DeepEqual(got, []int{1, 5}) {
		t.Errorf("two keys: lines %v", got)
	}
	ed.Clear("a")
	if got := lines(h.Seen(doc)); !reflect.DeepEqual(got, []int{1}) {
		t.Errorf("after Clear(a): lines %v", got)
	}
}

func testInvalid(t *testing.T, h *Harness) {
	ed := h.API.EditorDecorations()
	many := make([]extension.LineAnnotation, extension.MaxLineAnnotations+1)
	tests := []struct {
		name     string
		key, uri string
		list     []extension.LineAnnotation
	}{
		{"no key", "", doc, nil},
		{"no document", "k", "", nil},
		{"a negative line", "k", doc, []extension.LineAnnotation{{Line: -1, Text: "x"}}},
		{"too many", "k", doc, many},
	}
	for _, tc := range tests {
		err := ed.Set(tc.key, tc.uri, tc.list)
		appErr, ok := errors.FromError(err)
		if !ok || appErr.Tag != "ExtensionEditorDecorationsInvalid" {
			t.Errorf("%s: error %v", tc.name, err)
		}
	}
	if got := h.Seen(doc); len(got) != 0 {
		t.Errorf("an invalid set drew %+v", got)
	}
}

func testCut(t *testing.T, h *Harness) {
	ed := h.API.EditorDecorations()
	long := strings.Repeat("é", extension.MaxLineAnnotationText+50)
	hover := strings.Repeat("é", extension.MaxLineAnnotationHover)
	mustSet(t, ed, "k", doc, []extension.LineAnnotation{{Line: 0, Text: long, Hover: hover}})
	got := h.Seen(doc)
	if len(got) != 1 {
		t.Fatalf("seen %+v", got)
	}
	if n := utf8.RuneCountInString(got[0].Text); n != extension.MaxLineAnnotationText {
		t.Errorf("the text has %d runes, want %d", n, extension.MaxLineAnnotationText)
	}
	if len(got[0].Hover) > extension.MaxLineAnnotationHover || !utf8.ValidString(got[0].Hover) {
		t.Errorf("the hover has %d bytes (valid %v)", len(got[0].Hover), utf8.ValidString(got[0].Hover))
	}
}

func testOwnLink(t *testing.T, h *Harness) {
	ed := h.API.EditorDecorations()
	mustSet(t, ed, "k", doc, []extension.LineAnnotation{{Line: 3, Text: "x", Hover: "[run](command:test.run)"}})
	tests := []struct {
		dest string
		want any
	}{
		{"command:" + OwnCommand, nil},
		{"command:" + OwnCommand + "?%5B%22one%22%5D", "one"},
		{"command:" + OwnCommand + "?%5B%7B%22id%22%3A%22abc%22%7D%5D", map[string]any{"id": "abc"}},
		{"command:" + OwnCommand + "?%5B%22a%22%2C2%5D", []any{"a", float64(2)}},
		{"command:" + OwnCommand + "?%22plain%22", "plain"},
	}
	for _, tc := range tests {
		before := len(h.Ran())
		h.Follow(doc, 3, tc.dest)
		h.Pump()
		ran := h.Ran()
		if len(ran) != before+1 || !reflect.DeepEqual(ran[len(ran)-1], tc.want) {
			t.Errorf("%s: ran %v, want one more call with %v", tc.dest, ran, tc.want)
		}
	}
}

func testForeignLink(t *testing.T, h *Harness) {
	ed := h.API.EditorDecorations()
	mustSet(t, ed, "k", doc, []extension.LineAnnotation{{Line: 3, Text: "x", Hover: "links"}})
	for _, dest := range []string{
		"command:other.extension.command", "command:workbench.action.openSettings?%5B%22git.blame%22%5D", "command:", "command:test.run?{not json",
		"https://example.com", "mailto:a@example.com",
	} {
		h.Follow(doc, 3, dest)
		h.Pump()
	}
	if ran := h.Ran(); len(ran) != 0 {
		t.Errorf("a link that is not to a command of the extension ran %v", ran)
	}
	h.Follow(doc, 99, "command:"+OwnCommand) // no annotation at the line: nothing to click
	h.Pump()
	if ran := h.Ran(); len(ran) != 0 {
		t.Errorf("a click where there is no annotation ran %v", ran)
	}
}

func testDeactivate(t *testing.T, h *Harness) {
	ed := h.API.EditorDecorations()
	mustSet(t, ed, "k", doc, []extension.LineAnnotation{{Line: 1, Text: "x"}})
	h.Deactivate()
	h.Pump()
	if got := h.Seen(doc); len(got) != 0 {
		t.Errorf("the end of the activation left %+v", got)
	}
	err := ed.Set("k", doc, []extension.LineAnnotation{{Line: 1, Text: "x"}})
	if appErr, ok := errors.FromError(err); !ok || appErr.Tag != "ExtensionDeactivated" {
		t.Errorf("Set after the end: %v", err)
	}
	ed.Clear("k") // does nothing, does not panic
}

func mustSet(t *testing.T, ed extension.EditorDecorations, key, uri string, list []extension.LineAnnotation) {
	t.Helper()
	if err := ed.Set(key, uri, list); err != nil {
		t.Fatal(err)
	}
}
