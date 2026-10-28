package references

import (
	"slices"
	"testing"

	"github.com/rm4n0s/emetos/extension"
)

func TestTheHistoryKeepsTheNewestSearchOfAWordFirst(t *testing.T) {
	// Ported from VS Code: tree.ts (TreeInputHistory.add: a search with the same key goes to the end of the map, which is the
	// front of the list the view shows)
	lines := []string{"func Greet() {}", "Greet()", "other()"}
	var h history
	refs := &input{title: titleReferences, kind: kindReferences, uri: "file:///a.go", pos: extension.Position{Line: 0, Col: 6}}
	impls := &input{title: titleImplementations, kind: kindImplementations, uri: "file:///a.go", pos: extension.Position{Line: 0, Col: 6}}
	elsewhere := &input{title: titleReferences, kind: kindReferences, uri: "file:///a.go", pos: extension.Position{Line: 2, Col: 1}}
	h.add(refs, lines, "a.go")
	h.add(impls, lines, "a.go")
	h.add(elsewhere, lines, "a.go")
	// the same word at another column of the same kind is the same search
	again := h.add(refs.with(extension.Position{Line: 0, Col: 8}), lines, "a.go")
	var words, descs []string
	for _, it := range h.newestFirst() {
		words, descs = append(words, it.word), append(descs, it.description)
	}
	if want := []string{"Greet", "other", "Greet"}; !slices.Equal(words, want) {
		t.Errorf("words = %v, want %v", words, want)
	}
	if want := []string{"a.go • references", "a.go • references", "a.go • implementations"}; !slices.Equal(descs, want) {
		t.Errorf("descriptions = %v, want %v", descs, want)
	}
	if h.size() != 3 || h.last() != again {
		t.Errorf("size = %d, last = %v", h.size(), h.last())
	}
	h.clear()
	if h.size() != 0 || h.last() != nil || len(h.newestFirst()) != 0 {
		t.Error("clear left searches")
	}
}

func TestTheHistoryNamesAPositionWithNoWord(t *testing.T) {
	var h history
	it := h.add(&input{title: titleReferences, uri: "file:///a", pos: extension.Position{Line: 0, Col: 2}}, []string{"a  b"}, "a")
	if it.word != "???" {
		t.Errorf("the word of white space = %q", it.word)
	}
}

func TestTheAnchorFollowsAWordThatMoved(t *testing.T) {
	// Ported from VS Code: utils.ts (WordAnchor.guessedTrackedPosition)
	before := []string{"package a", "", "func Greet() {}", "", "func other() {}"}
	a := newAnchor("file:///a.go", before, extension.Position{Line: 2, Col: 6})
	tests := []struct {
		name  string
		lines []string
		want  extension.Position
	}{
		{"nothing changed", before, extension.Position{Line: 2, Col: 6}},
		{"the text above changed but the word is still there", []string{"package a", "", "func Greet() {}"}, extension.Position{Line: 2, Col: 6}},
		{"lines were added above", append([]string{"// one", "// two"}, before...), extension.Position{Line: 4, Col: 5}},
		{"lines were removed above", []string{"func Greet() {}", "", "func other() {}"}, extension.Position{Line: 0, Col: 5}},
		{"the word is gone", []string{"nothing here"}, extension.Position{Line: 2, Col: 6}},
		{"the word moved along its line", []string{"package a", "", "func (t T) Greet() {}"}, extension.Position{Line: 2, Col: 11}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := a.guessedPosition(tc.lines); got != tc.want {
				t.Errorf("position = %+v, want %+v", got, tc.want)
			}
		})
	}
	if got := (anchor{pos: extension.Position{Line: 1, Col: 1}}).guessedPosition([]string{"x"}); got != (extension.Position{Line: 1, Col: 1}) {
		t.Errorf("an anchor with no word moved to %+v", got)
	}
}

func TestWordOfLine(t *testing.T) {
	lines := []string{"func Greet() {}", "  ;; "}
	if got := wordOfLine(lines, extension.Position{Line: 0, Col: 7}); got != "Greet" {
		t.Errorf("word = %q", got)
	}
	if got := wordOfLine(lines, extension.Position{Line: 1, Col: 3}); got != ";;" {
		t.Errorf("token = %q", got)
	}
	if got := wordOfLine(lines, extension.Position{Line: 5}); got != "" {
		t.Errorf("a line that is not there has the word %q", got)
	}
}
