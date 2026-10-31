package gitscm

import (
	"hash/fnv"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/rm4n0s/emetos/extensions/gitscm/git"
)

// The data of the blame text (SPEC §10.30 "Blame", step 24.22): which commit made each line of the version in HEAD, how the lines of the
// document the user is editing map to the lines of that version, and the text a template makes of a commit. The pure parts of blame.go.
//
// Ported from VS Code: extensions/git/src/blame.ts (formatBlameInformationMessage, lineRangesContainLine, mapModifiedLineNumberToOriginalLineNumber)
// and util.ts (fromNow, truncate, getCommitShortHash). VS Code maps the lines of the editor to those of HEAD with the diff information of the
// editor (the changes of the quick diff); Emetos maps them with a line diff of the document against the version in HEAD, which needs no
// history of the edits and is right when the window starts with a document that has unsaved changes already.

// blameIndex is a blamed version of a file: for each of its lines the commit that made it, and a hash of the line to find it again in the
// document.
type blameIndex struct {
	// commits are the commits that blame found, and owner the index in commits of the commit of each line (-1 for a line that
	// blame did not name, such as the empty text after the last line break).
	commits []git.BlameCommit
	owner   []int32
	// hashes are the hashes of the lines, as lineHashes makes them (with the same whitespace rule as the blame).
	hashes []uint64
}

// newBlameIndex makes the index of a version from the output of blame and the text of the version. ignoreWhitespace is the -w of the blame:
// the hashes of the lines leave the white space out then, so that a line that differs in white space is the same line.
func newBlameIndex(commits []git.BlameCommit, text string, ignoreWhitespace bool) *blameIndex {
	hashes := lineHashes(text, ignoreWhitespace)
	b := &blameIndex{commits: commits, hashes: hashes, owner: make([]int32, len(hashes))}
	for i := range b.owner {
		b.owner[i] = -1
	}
	for ci, c := range commits {
		for _, r := range c.Ranges {
			for l := r.Start; l <= r.End; l++ {
				if l >= 1 && l <= len(b.owner) {
					b.owner[l-1] = int32(ci)
				}
			}
		}
	}
	return b
}

// commitOf is the commit that made a line (counted from 0) of the blamed version, or false.
func (b *blameIndex) commitOf(line int) (git.BlameCommit, bool) {
	if line < 0 || line >= len(b.owner) || b.owner[line] < 0 {
		return git.BlameCommit{}, false
	}
	return b.commits[b.owner[line]], true
}

// splitLines cuts a text at its line breaks (\n; a \r before it is not part of the line), as the editor counts lines: a text that ends with a
// line break has an empty last line.
func splitLines(text string) []string {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	return lines
}

// lineHashes are the hashes of the lines of a text; with ignoreWhitespace the white space of a line is left out of its hash.
func lineHashes(text string, ignoreWhitespace bool) []uint64 {
	lines := splitLines(text)
	out := make([]uint64, len(lines))
	for i, l := range lines {
		if ignoreWhitespace {
			l = strings.Map(func(r rune) rune {
				if unicode.IsSpace(r) {
					return -1
				}
				return r
			}, l)
		}
		h := fnv.New64a()
		_, _ = h.Write([]byte(l))
		out[i] = h.Sum64()
	}
	return out
}

// maxBlameDiff is the most lines of difference between the document and the version in HEAD that the mapping looks for; a document that
// differs more has no blame text (it is not the file the commits are about any more).
const maxBlameDiff = 500

// mapLines maps the lines of a document to the lines of a version: the result has the index in head of the line that doc[i] is, or -1 for a
// line that is not in the version (added or changed). The lines that match are those of a longest common subsequence of the two, found by
// Myers's O(ND) algorithm after the common start and end are taken off. It reports false when the two differ by more than maxBlameDiff
// lines.
func mapLines(head, doc []uint64) (mapping []int, ok bool) {
	mapping = make([]int, len(doc))
	for i := range mapping {
		mapping[i] = -1
	}
	// the common start and end
	start := 0
	for start < len(head) && start < len(doc) && head[start] == doc[start] {
		mapping[start] = start
		start++
	}
	endHead, endDoc := len(head), len(doc)
	for endHead > start && endDoc > start && head[endHead-1] == doc[endDoc-1] {
		endHead--
		endDoc--
		mapping[endDoc] = endHead
	}
	a, b := head[start:endHead], doc[start:endDoc]
	if len(a) == 0 || len(b) == 0 {
		return mapping, true
	}
	pairs, found := myers(a, b, min(maxBlameDiff, len(a)+len(b)))
	if !found {
		return nil, false
	}
	for _, p := range pairs {
		mapping[start+p[1]] = start + p[0]
	}
	return mapping, true
}

// myers finds the pairs (index in a, index in b) of a longest common subsequence of a and b, when the shortest edit script between them
// has at most maxD insertions and deletions.
func myers(a, b []uint64, maxD int) (pairs [][2]int, ok bool) {
	n, m := len(a), len(b)
	off := maxD + 1
	v := make([]int, 2*maxD+3)
	var trace [][]int
	finalD := -1
search:
	for d := 0; d <= maxD; d++ {
		trace = append(trace, append([]int(nil), v...))
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[off+k-1] < v[off+k+1]) {
				x = v[off+k+1]
			} else {
				x = v[off+k-1] + 1
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}
			v[off+k] = x
			if x >= n && y >= m {
				finalD = d
				break search
			}
		}
	}
	if finalD < 0 {
		return nil, false
	}
	x, y := n, m
	for d := finalD; d > 0; d-- {
		pv := trace[d]
		k := x - y
		var prevK int
		if k == -d || (k != d && pv[off+k-1] < pv[off+k+1]) {
			prevK = k + 1
		} else {
			prevK = k - 1
		}
		prevX := pv[off+prevK]
		prevY := prevX - prevK
		for x > prevX && y > prevY {
			x--
			y--
			pairs = append(pairs, [2]int{x, y})
		}
		x, y = prevX, prevY
	}
	for x > 0 && y > 0 {
		x--
		y--
		pairs = append(pairs, [2]int{x, y})
	}
	return pairs, true
}

// blameValues are the variables of a blame template (VS Code's BlameInformationTemplateTokens).
type blameValues struct {
	hash, hashShort, subject, authorName, authorEmail, authorDate, authorDateAgo string
}

// blameSubjectMax is how many characters of the subject a template shows (VS Code's _subjectMaxLength).
const blameSubjectMax = 50

var templateVariable = regexp.MustCompile(`\$\{(.+?)\}`)

// formatBlame fills a template (git.blame.editorDecoration.template, git.blame.statusBarItem.template): each ${name} is the value of the
// variable, and one that is not a variable stays as written.
//
// Ported from VS Code: extensions/git/src/blame.ts (formatBlameInformationMessage)
func formatBlame(template string, v blameValues) string {
	return templateVariable.ReplaceAllStringFunc(template, func(m string) string {
		switch m[2 : len(m)-1] {
		case "hash":
			return v.hash
		case "hashShort":
			return v.hashShort
		case "subject":
			return v.subject
		case "authorName":
			return v.authorName
		case "authorEmail":
			return v.authorEmail
		case "authorDate":
			return v.authorDate
		case "authorDateAgo":
			return v.authorDateAgo
		}
		return m
	})
}

// truncate cuts a text to n characters and ends it with an ellipsis when it was longer (VS Code's truncate, which counts UTF-16 units;
// Emetos counts characters, so that a cut never falls inside one).
func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

// shortHash is the first n characters of a hash.
func shortHash(hash string, n int) string {
	if n <= 0 || n > len(hash) {
		return hash
	}
	return hash[:n]
}

// dateString is the date as a user reads it in a list: "1/2/2006, 3:04:05 PM", as the en-US toLocaleString of VS Code gives it.
func dateString(t time.Time, loc *time.Location) string {
	return t.In(loc).Format("1/2/2006, 3:04:05 PM")
}

// longDateString is the date of the hover: "January 2, 2006 at 3:04 PM".
func longDateString(t time.Time, loc *time.Location) string {
	return t.In(loc).Format("January 2, 2006 at 3:04 PM")
}

const (
	minute = 60
	hour   = 60 * minute
	day    = 24 * hour
	week   = 7 * day
	month  = 30 * day
	year   = 365 * day
)

// fromNow is how long ago a date was, in words: "now" for less than 30 seconds, then seconds, minutes, hours, days, weeks, months (30
// days) and years, with " ago" appended; a date in the future is "in ..." (VS Code's fromNow with appendAgoLabel and useFullTimeWords).
//
// Ported from VS Code: extensions/git/src/util.ts (fromNow)
func fromNow(date, now time.Time, ago bool) string {
	seconds := int(roundHalfUp(now.Sub(date).Seconds()))
	if seconds < -30 {
		return "in " + fromNow(now.Add(time.Duration(seconds)*time.Second), now, false)
	}
	if seconds < 30 {
		return "now"
	}
	units := []struct {
		limit int
		size  int
		name  string
	}{{minute, 1, "second"}, {hour, minute, "minute"}, {day, hour, "hour"}, {week, day, "day"}, {month, week, "week"}, {year, month, "month"}}
	for _, u := range units {
		if seconds < u.limit {
			return plural(seconds/u.size, u.name, ago)
		}
	}
	return plural(seconds/year, "year", ago)
}

func plural(n int, unit string, ago bool) string {
	s := strconv.Itoa(n) + " " + unit
	if n != 1 {
		s += "s"
	}
	if ago {
		s += " ago"
	}
	return s
}

// roundHalfUp rounds as JavaScript's Math.round does: halves go toward positive infinity.
func roundHalfUp(f float64) float64 { return math.Floor(f + 0.5) }
