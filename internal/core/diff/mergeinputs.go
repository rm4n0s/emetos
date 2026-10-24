package diff

import (
	"strings"

	"github.com/rm4n0s/errors"
)

// MergeInputs are the three texts of a three-way merge and what goes with
// them. Input1 is the incoming side (the left pane of the merge editor;
// "theirs"; the first path of --merge) and Input2 is the current side (the
// right pane; "ours", HEAD; the second path of --merge), as in VS Code's merge
// editor. Texts are lines without their line breaks; a text that ends with a
// line break has an empty last line, and a text with no line at all counts as
// one empty line.
type MergeInputs struct {
	// Base is the common ancestor.
	Base []string
	// Input1 is the incoming text.
	Input1 []string
	// Input2 is the current text.
	Input2 []string
	// Input1Label, Input2Label and BaseLabel describe the texts in the panes
	// (the labels of the markers, a branch name or a path); they may be empty.
	Input1Label, Input2Label, BaseLabel string
	// EOL is the line break the result is written with: "\n" or "\r\n"; empty
	// means "\n".
	EOL string
	// MarkedConflicts are the lines of Base where the conflicts of a file with
	// conflict markers were (empty for a conflict without a base section, so
	// at a position between two lines). A change that touches one is a
	// conflict that has to be resolved even if only one side changed the base
	// there (the side with no lines against a base with none), which the
	// diffs alone could not tell. Empty for the inputs of --merge.
	MarkedConflicts []LineRange
}

// MergeInputsFromFiles makes the inputs of a merge of three texts (the --merge
// flow): base, input 1 (incoming) and input 2 (current). The texts are not
// copied and are not changed by the merge model.
func MergeInputsFromFiles(base, input1, input2 []string) MergeInputs {
	return MergeInputs{Base: base, Input1: input1, Input2: input2}
}

// MergeInputsFromMarkers makes the inputs of the merge of a file with
// conflict markers (SPEC section 9.11 (a)): lines is the file and conflicts
// what ParseConflictMarkers found in it. Input 2 (current) is the file with
// every conflict taken from its current side, input 1 (incoming) the file with
// every conflict taken from its incoming side, and the base the file with
// every conflict taken from its first "|||||||" section, or with the conflict
// left out when it has none (an empty conflict region). The labels are those
// of the first conflict that has a label for the side. EOL is left empty.
func MergeInputsFromMarkers(lines []string, conflicts []MarkerConflict) MergeInputs {
	var in MergeInputs
	next := 1 // the first line of lines that is not copied yet
	for _, c := range conflicts {
		outside := lines[next-1 : c.Range.StartLine-1]
		in.Base = append(in.Base, outside...)
		in.Input1 = append(in.Input1, outside...)
		in.Input2 = append(in.Input2, outside...)

		marked := LineRange{StartLine: len(in.Base) + 1, EndLine: len(in.Base) + 1}
		if len(c.CommonAncestors) > 0 {
			ancestor := lines[c.CommonAncestors[0].Content.StartLine-1 : c.CommonAncestors[0].Content.EndLine-1]
			in.Base = append(in.Base, ancestor...)
			marked.EndLine += len(ancestor)
		}
		in.Input1 = append(in.Input1, sectionLines(lines, c.Incoming)...)
		in.Input2 = append(in.Input2, sectionLines(lines, c.Current)...)
		in.MarkedConflicts = append(in.MarkedConflicts, marked)

		if in.Input1Label == "" {
			in.Input1Label = c.Incoming.Label
		}
		if in.Input2Label == "" {
			in.Input2Label = c.Current.Label
		}
		if in.BaseLabel == "" && len(c.CommonAncestors) > 0 {
			in.BaseLabel = c.CommonAncestors[0].Label
		}
		next = c.Range.EndLine
	}
	rest := lines[next-1:]
	in.Base = append(in.Base, rest...)
	in.Input1 = append(in.Input1, rest...)
	in.Input2 = append(in.Input2, rest...)
	return in
}

// sectionLines returns the content lines of a section.
func sectionLines(lines []string, s MarkerSection) []string {
	return lines[s.Content.StartLine-1 : s.Content.EndLine-1]
}

// MergeInputsFromMarkerText makes the inputs of the merge of a file with
// conflict markers from its text (see MergeInputsFromMarkers), with the line
// break of the file in EOL. The error MergeMarkersNotFound reports a text
// without a complete conflict.
func MergeInputsFromMarkerText(text string) (MergeInputs, error) {
	lines, eol := SplitLines(text)
	conflicts := ParseConflictMarkers(lines)
	if len(conflicts) == 0 {
		return MergeInputs{}, errors.New("MergeMarkersNotFound", "the text has no complete conflict markers")
	}
	in := MergeInputsFromMarkers(lines, conflicts)
	in.EOL = eol
	return in, nil
}

// SplitLines splits a text into lines without their line breaks ("\n" or
// "\r\n"; a text that ends with a line break has an empty last line) and
// returns the line break the text uses: the one that ends more lines, the
// first one on a tie, and "\n" for a text with no line break. Lines of a text
// with mixed line breaks lose theirs, so joining them with the one returned
// (JoinLines) makes all alike.
func SplitLines(text string) (lines []string, eol string) {
	var lf, crlf int
	first := ""
	for {
		i := strings.IndexByte(text, '\n')
		if i < 0 {
			return append(lines, text), pickEOL(lf, crlf, first)
		}
		line := text[:i]
		if strings.HasSuffix(line, "\r") {
			line = line[:len(line)-1]
			crlf++
			if first == "" {
				first = "\r\n"
			}
		} else {
			lf++
			if first == "" {
				first = "\n"
			}
		}
		lines = append(lines, line)
		text = text[i+1:]
	}
}

// pickEOL chooses the line break of a text from the number of lines ended by
// each kind.
func pickEOL(lf, crlf int, first string) string {
	switch {
	case crlf > lf:
		return "\r\n"
	case lf > crlf:
		return "\n"
	case first != "":
		return first
	}
	return "\n"
}

// JoinLines joins lines with eol, the inverse of SplitLines.
func JoinLines(lines []string, eol string) string {
	return strings.Join(lines, eol)
}
