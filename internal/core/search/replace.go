package search

import (
	"context"
	"slices"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/fileio"
	"github.com/rm4n0s/emetos/internal/core/find"
	"github.com/rm4n0s/emetos/internal/core/regex"
)

// Replacer makes the replacement of a match (VS Code's ReplacePattern of the search: the replace string of a regular
// expression has $1, $&, \n, \t and the case operations \u \U \l \L; the replace string of a plain text is used as it is; and
// preserve case makes the replacement follow the case of the text it replaces).
type Replacer struct {
	pattern      find.ReplacePattern
	re           *regex.Regexp
	preserveCase bool
	scratch      scratch
}

// NewReplacer makes the Replacer of a search. It fails with the errors of Query.Validate.
func NewReplacer(q Query, replacement string, preserveCase bool) (*Replacer, error) {
	if q.Pattern == "" {
		return nil, errors.New("SearchPatternEmpty", "there is nothing to search for")
	}
	r := &Replacer{pattern: find.StaticReplacePattern(replacement), preserveCase: preserveCase}
	if !q.IsRegExp {
		return r, nil
	}
	re, err := regex.Compile(regexSource(q), regex.Options{
		Syntax: regex.ECMAScript, IgnoreCase: !q.CaseSensitive(), Multiline: true, Unicode: true, Timeout: regexTimeout,
	})
	if err != nil {
		return nil, err // an error of internal/core/regex, with its own tag
	}
	r.re = re
	r.pattern = find.ParseReplaceString(replacement)
	return r, nil
}

// Replace is the replacement for the text of a match. For a regular expression the expression is run on the text of the
// match to find its groups, as VS Code does (a pattern that needs the text around the match, such as a lookbehind, then finds
// no groups, and the replacement refers to the whole match only). A Replacer is not safe for concurrent use.
func (r *Replacer) Replace(matchText string) string {
	groups := []string{matchText}
	if r.re != nil {
		groups = make([]string, r.re.NumGroups()+1)
		groups[0] = matchText
	}
	if r.re != nil && r.pattern.HasReplacementPatterns() {
		runes, _ := r.scratch.fill([]byte(matchText))
		if m, ok, err := r.re.Find(runes, 0); err == nil && ok {
			for i, g := range m.Groups {
				if g.Start >= 0 {
					groups[i] = string(runes[g.Start:g.End])
				}
			}
		}
	}
	return r.pattern.Build(groups, r.preserveCase)
}

// Edit replaces a range of a file's text with Text. Lines and columns count from 0, a column counts bytes of the UTF-8 line
// (the positions of a Match).
type Edit struct {
	Line, Col, EndLine, EndCol int
	// Expected is the text the range has now (Match.Text); an edit whose range has other text is stale, and skipped.
	Expected string
	// Text is the replacement.
	Text string
}

// ApplyEdits applies the edits to a text with "\n" line ends. The edits must not overlap (one that overlaps an earlier one is
// skipped). It returns the new text and the indexes of the edits that were skipped because the range is outside the text or
// has other text than Edit.Expected.
func ApplyEdits(text string, edits []Edit) (string, []int) {
	order := make([]int, len(edits))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int {
		x, y := edits[a], edits[b]
		if x.Line != y.Line {
			return x.Line - y.Line
		}
		return x.Col - y.Col
	})
	starts := lineStarts(text)
	offset := func(line, col int) (int, bool) {
		if line < 0 || line >= len(starts) || col < 0 {
			return 0, false
		}
		off := starts[line] + col
		end := len(text)
		if line+1 < len(starts) {
			end = starts[line+1] - 1
		}
		return off, off <= end
	}
	var out strings.Builder
	out.Grow(len(text))
	var skipped []int
	last := 0
	for _, i := range order {
		e := edits[i]
		s, ok1 := offset(e.Line, e.Col)
		t, ok2 := offset(e.EndLine, e.EndCol)
		if !ok1 || !ok2 || s < last || t < s || text[s:t] != strings.ReplaceAll(e.Expected, "\r\n", "\n") {
			skipped = append(skipped, i)
			continue
		}
		out.WriteString(text[last:s])
		out.WriteString(e.Text)
		last = t
	}
	out.WriteString(text[last:])
	slices.Sort(skipped)
	return out.String(), skipped
}

// lineStarts are the offsets where the lines of a text begin.
func lineStarts(text string) []int {
	starts := []int{0}
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

// ReplaceResult says what ReplaceInFile did.
type ReplaceResult struct {
	// Applied is the number of edits made and Skipped the number that were stale or invalid. The file is written only when
	// Applied is not 0.
	Applied, Skipped int
}

// ReplaceInFile makes the edits in a file that no editor has open: it reads the file as the editor does (the encoding, a byte
// order mark and the line ending are kept), applies the edits that are not stale, and writes the file again, atomically. It
// does not write a file that changed on disk since it was read. The errors are those of fileio.Read and fileio.Write
// (FileBinary, FileTooLarge, FileModifiedSince, FileReadOnly, ...); the encoding is files.encoding.
func ReplaceInFile(ctx context.Context, path, encoding string, edits []Edit) (ReplaceResult, error) {
	res, err := fileio.Read(ctx, path, fileio.ReadOptions{Encoding: encoding})
	if err != nil {
		return ReplaceResult{}, err
	}
	text, skipped := ApplyEdits(res.Text, edits)
	out := ReplaceResult{Applied: len(edits) - len(skipped), Skipped: len(skipped)}
	if out.Applied == 0 {
		return out, nil
	}
	stat := res.Stat
	if _, err := fileio.Write(ctx, path, text, fileio.WriteOptions{
		Encoding: res.Encoding, LineEnding: res.LineEnding, BOM: res.HasBOM, IfUnchanged: &stat,
	}); err != nil {
		return ReplaceResult{}, err
	}
	return out, nil
}
