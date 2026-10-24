package diff

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
)

// describeMarkers writes conflicts as
// "[1,6) cur[2,3)=HEAD anc[3,4)=base spl=4 inc[5,5)=topic" (the whole range,
// then each section's content range and label, then the splitter line).
func describeMarkers(cs []MarkerConflict) string {
	var parts []string
	for _, c := range cs {
		var b strings.Builder
		fmt.Fprintf(&b, "[%d,%d) cur[%d,%d)=%s", c.Range.StartLine, c.Range.EndLine,
			c.Current.Content.StartLine, c.Current.Content.EndLine, c.Current.Label)
		for _, a := range c.CommonAncestors {
			fmt.Fprintf(&b, " anc[%d,%d)=%s", a.Content.StartLine, a.Content.EndLine, a.Label)
		}
		fmt.Fprintf(&b, " spl=%d inc[%d,%d)=%s", c.Splitter.StartLine, c.Incoming.Content.StartLine,
			c.Incoming.Content.EndLine, c.Incoming.Label)
		parts = append(parts, b.String())
	}
	return strings.Join(parts, "; ")
}

func TestParseConflictMarkers(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{"simple", "a\n<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> topic\nz", "[2,7) cur[3,4)=HEAD spl=4 inc[5,6)=topic"},
		{"diff3", "<<<<<<< HEAD\nours\n||||||| base\norig\n=======\ntheirs\n>>>>>>> topic\n",
			"[1,8) cur[2,3)=HEAD anc[4,5)=base spl=5 inc[6,7)=topic"},
		{"two ancestors", "<<<<<<< a\nx\n||||||| b1\ny\n||||||| b2\nz\n=======\nw\n>>>>>>> c",
			"[1,10) cur[2,3)=a anc[4,5)=b1 anc[6,7)=b2 spl=7 inc[8,9)=c"},
		{"multiple", "<<<<<<< A\n1\n=======\n2\n>>>>>>> B\nmid\n<<<<<<< C\n3\n=======\n4\n>>>>>>> D",
			"[1,6) cur[2,3)=A spl=3 inc[4,5)=B; [7,12) cur[8,9)=C spl=9 inc[10,11)=D"},
		{"labels with spaces", "<<<<<<< my branch (1)\na\n=======\nb\n>>>>>>> origin/feature x", "[1,6) cur[2,3)=my branch (1) spl=3 inc[4,5)=origin/feature x"},
		{"no space after the marker loses a character", "<<<<<<<HEAD\na\n=======\nb\n>>>>>>>topic", "[1,6) cur[2,3)=EAD spl=3 inc[4,5)=opic"},
		{"bare markers have no label", "<<<<<<<\na\n=======\nb\n>>>>>>>", "[1,6) cur[2,3)= spl=3 inc[4,5)="},
		{"multibyte character after the marker", "<<<<<<<é label\na\n=======\nb\n>>>>>>> x", "[1,6) cur[2,3)= label spl=3 inc[4,5)=x"},
		{"empty current", "<<<<<<< HEAD\n=======\nb\n>>>>>>> t", "[1,5) cur[2,2)=HEAD spl=2 inc[3,4)=t"},
		{"empty incoming", "<<<<<<< HEAD\na\n=======\n>>>>>>> t", "[1,5) cur[2,3)=HEAD spl=3 inc[4,4)=t"},
		{"both empty", "<<<<<<< HEAD\n=======\n>>>>>>> t", "[1,4) cur[2,2)=HEAD spl=2 inc[3,3)=t"},
		{"empty ancestor", "<<<<<<< HEAD\na\n||||||| b\n=======\nc\n>>>>>>> t", "[1,7) cur[2,3)=HEAD anc[4,4)=b spl=4 inc[5,6)=t"},
		{"whole file", "<<<<<<< HEAD\na\n=======\nb\n>>>>>>> t\n", "[1,6) cur[2,3)=HEAD spl=3 inc[4,5)=t"},
		{"no trailing newline", "x\n<<<<<<< HEAD\na\n=======\nb\n>>>>>>> t", "[2,7) cur[3,4)=HEAD spl=4 inc[5,6)=t"},
		{"crlf", "x\r\n<<<<<<< HEAD\r\na\r\n=======\r\nb\r\n>>>>>>> t\r\ny\r\n", "[2,7) cur[3,4)=HEAD spl=4 inc[5,6)=t"},
		{"no markers", "a\nb\n", ""},
		{"empty text", "", ""},
		{"splitter outside a conflict is ignored", "a\n=======\nb\n<<<<<<< H\nc\n=======\nd\n>>>>>>> t", "[4,9) cur[5,6)=H spl=6 inc[7,8)=t"},
		{"footer outside a conflict is ignored", ">>>>>>> t\nx\n<<<<<<< H\nc\n=======\nd\n>>>>>>> t", "[3,8) cur[4,5)=H spl=5 inc[6,7)=t"},
		{"ancestor outside a conflict is ignored", "||||||| b\n<<<<<<< H\nc\n=======\nd\n>>>>>>> t", "[2,7) cur[3,4)=H spl=4 inc[5,6)=t"},
		{"indented markers are not markers", " <<<<<<< H\nc\n =======\nd\n >>>>>>> t", ""},
		{"indented footer is text of the conflict", "<<<<<<< H\nc\n=======\nd\n >>>>>>> t\n>>>>>>> u", "[1,7) cur[2,3)=H spl=3 inc[4,6)=u"},
		{"longer runs start and end a conflict", "<<<<<<<< H\nc\n=======\nd\n>>>>>>>> t", "[1,6) cur[2,3)= H spl=3 inc[4,5)= t"},
		{"longer splitter is not a splitter", "<<<<<<< H\nc\n========\nd\n>>>>>>> t", ""},
		{"splitter with trailing space is not a splitter", "<<<<<<< H\nc\n======= \nd\n>>>>>>> t", ""},
		{"second splitter is text", "<<<<<<< H\nc\n=======\nd\n=======\ne\n>>>>>>> t", "[1,8) cur[2,3)=H spl=3 inc[4,7)=t"},
		{"ancestor after the splitter is text", "<<<<<<< H\nc\n=======\n||||||| x\n>>>>>>> t", "[1,6) cur[2,3)=H spl=3 inc[4,5)=t"},
		{"footer without splitter drops the conflict", "<<<<<<< H\nc\n>>>>>>> t\n<<<<<<< H\nc\n=======\nd\n>>>>>>> t", "[4,9) cur[5,6)=H spl=6 inc[7,8)=t"},
		{"unclosed conflict at the end is dropped", "<<<<<<< H\nc\n=======\nd\n", ""},
		{"a second start gives up", "<<<<<<< A\n1\n=======\n2\n>>>>>>> B\n<<<<<<< C\n3\n<<<<<<< D\n4\n=======\n5\n>>>>>>> E\n<<<<<<< F\n6\n=======\n7\n>>>>>>> G",
			"[1,6) cur[2,3)=A spl=3 inc[4,5)=B"},
		{"a second start inside the first drops both", "<<<<<<< A\n1\n<<<<<<< B\n2\n=======\n3\n>>>>>>> C", ""},
		{"white space lines are skipped", "<<<<<<< H\n  \nc\n\t\n=======\n\nd\n>>>>>>> t", "[1,9) cur[2,5)=H spl=5 inc[6,8)=t"},
		{"conflict of two markers adjacent", "<<<<<<< a\n=======\n>>>>>>> b\n<<<<<<< c\n=======\n>>>>>>> d", "[1,4) cur[2,2)=a spl=2 inc[3,3)=b; [4,7) cur[5,5)=c spl=5 inc[6,6)=d"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines, _ := SplitLines(tt.text)
			if got := describeMarkers(ParseConflictMarkers(lines)); got != tt.want {
				t.Errorf("got  %s\nwant %s", got, tt.want)
			}
		})
	}
}

func TestParseConflictMarkersSections(t *testing.T) {
	lines, _ := SplitLines("a\n<<<<<<< HEAD\nours\n||||||| base\norig\n=======\ntheirs\n>>>>>>> topic\nz")
	cs := ParseConflictMarkers(lines)
	if len(cs) != 1 {
		t.Fatalf("got %d conflicts, want 1", len(cs))
	}
	c := cs[0]
	one := func(n int) LineRange { return LineRange{StartLine: n, EndLine: n + 1} }
	if c.Current.Header != one(2) || c.CommonAncestors[0].Header != one(4) || c.Splitter != one(6) || c.Incoming.Header != one(8) {
		t.Errorf("headers: %+v", c)
	}
	if c.Range != (LineRange{StartLine: 2, EndLine: 9}) {
		t.Errorf("range %+v", c.Range)
	}
}

func TestContainsConflictMarkers(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		{"<<<<<<< a\n=======\n>>>>>>> b", true},
		{"<<<<<<< a", false},
		{">>>>>>> a", false},
		{"", false},
		{"x <<<<<<< y >>>>>>> z", true},
	}
	for _, tt := range tests {
		if got := ContainsConflictMarkers(tt.text); got != tt.want {
			t.Errorf("ContainsConflictMarkers(%q) = %v, want %v", tt.text, got, tt.want)
		}
	}
}

func TestSplitLines(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		want    []string
		wantEOL string
	}{
		{"empty", "", []string{""}, "\n"},
		{"no line break", "a", []string{"a"}, "\n"},
		{"lf", "a\nb\n", []string{"a", "b", ""}, "\n"},
		{"crlf", "a\r\nb\r\n", []string{"a", "b", ""}, "\r\n"},
		{"mixed majority crlf", "a\r\nb\r\nc\n", []string{"a", "b", "c", ""}, "\r\n"},
		{"mixed majority lf", "a\nb\nc\r\nd", []string{"a", "b", "c", "d"}, "\n"},
		{"tie takes the first", "a\r\nb\n", []string{"a", "b", ""}, "\r\n"},
		{"tie takes the first lf", "a\nb\r\n", []string{"a", "b", ""}, "\n"},
		{"lone cr stays", "a\rb\n", []string{"a\rb", ""}, "\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, eol := SplitLines(tt.text)
			if !slices.Equal(got, tt.want) || eol != tt.wantEOL {
				t.Errorf("SplitLines(%q) = %q, %q; want %q, %q", tt.text, got, eol, tt.want, tt.wantEOL)
			}
			if tt.name == "crlf" || tt.name == "lf" {
				if back := JoinLines(got, eol); back != tt.text {
					t.Errorf("JoinLines gives %q, want %q", back, tt.text)
				}
			}
		})
	}
}

func TestMergeInputsFromMarkerTextNoConflict(t *testing.T) {
	_, err := MergeInputsFromMarkerText("a\nb\n")
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T", err)
	}
	if !appErr.HasRoute("MergeInputsFromMarkerText.MergeMarkersNotFound") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}
