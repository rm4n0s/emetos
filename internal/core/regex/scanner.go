package regex

import (
	"sort"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/rm4n0s/errors"
)

// ScanTimeout is the longest a call of Scanner.Find may take (SPEC §8.2).
const ScanTimeout = 100 * time.Millisecond

// Line is a line of text prepared for searching: it is converted to runes once, whatever the
// number of searches, and keeps the byte offset of every rune. A byte that is not valid UTF-8 is
// the rune U+FFFD, one for each such byte. Offsets in the API of Scanner are bytes of the text.
type Line struct {
	text    string
	runes   []rune
	offsets []int // the byte offset of each rune, and len(text) at the end; nil for ASCII
}

// NewLine prepares text for searching.
func NewLine(text string) *Line {
	l := &Line{text: text}
	ascii := true
	for i := 0; i < len(text); i++ {
		if text[i] >= utf8.RuneSelf {
			ascii = false
			break
		}
	}
	if ascii {
		l.runes = make([]rune, len(text))
		for i := 0; i < len(text); i++ {
			l.runes[i] = rune(text[i])
		}
		return l
	}
	l.runes = make([]rune, 0, len(text))
	l.offsets = make([]int, 0, len(text)+1)
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		l.runes = append(l.runes, r)
		l.offsets = append(l.offsets, i)
		i += size
	}
	l.offsets = append(l.offsets, len(text))
	return l
}

// Text is the text of the line.
func (l *Line) Text() string { return l.text }

// byteOffset is the byte offset of the rune index i.
func (l *Line) byteOffset(i int) int {
	if l.offsets == nil {
		return i
	}
	return l.offsets[i]
}

// runeIndex is the index of the rune that starts at, or next after, the byte offset b.
func (l *Line) runeIndex(b int) int {
	if l.offsets == nil {
		return min(b, len(l.runes))
	}
	return sort.SearchInts(l.offsets, b)
}

// Capture is a range of a line in bytes. A group that did not match has the capture {-1, -1}.
type Capture struct {
	Start, End int
}

// ScanMatch is the result of Scanner.Find.
type ScanMatch struct {
	// Pattern is the index of the pattern that matched.
	Pattern int
	// Captures holds the whole match first and then each capturing group.
	Captures []Capture
}

// Scanner searches a line for the earliest match of several patterns, as vscode-textmate's
// OnigScanner does. It is safe for concurrent use.
type Scanner struct {
	patterns []string
	cache    *Cache
	timeout  time.Duration
	// base are the patterns compiled with \A and \G allowed; errs are the failures.
	base []*Regexp
	errs []error
	// anchored are the variants for the lines where \A, \G or both are not allowed, made when
	// they are first needed.
	mu       sync.Mutex
	anchored [3][]*Regexp
}

// ScannerOptions are the options of NewScanner.
type ScannerOptions struct {
	// Cache compiles the patterns; nil compiles without remembering.
	Cache *Cache
	// Timeout is the longest a call of Find may take; the default is ScanTimeout.
	Timeout time.Duration
}

// NewScanner compiles patterns, in the Oniguruma syntax. A pattern that cannot be compiled never
// matches; Err tells why.
func NewScanner(patterns []string, opts ScannerOptions) *Scanner {
	s := &Scanner{patterns: patterns, cache: opts.Cache, timeout: opts.Timeout}
	if s.timeout <= 0 {
		s.timeout = ScanTimeout
	}
	s.base = make([]*Regexp, len(patterns))
	s.errs = make([]error, len(patterns))
	for i, p := range patterns {
		s.base[i], s.errs[i] = s.compile(p, Options{})
	}
	return s
}

// compile compiles a pattern with the options and the timeout of the scanner.
func (s *Scanner) compile(pattern string, opts Options) (*Regexp, error) {
	opts.Timeout = s.timeout
	if s.cache != nil {
		return s.cache.Compile(pattern, opts)
	}
	return Compile(pattern, opts)
}

// Len is the number of patterns.
func (s *Scanner) Len() int { return len(s.patterns) }

// Err is the error of the pattern i that could not be compiled, or nil.
func (s *Scanner) Err(i int) error { return s.errs[i] }

// variant gives the compiled patterns for a line where \A is allowed when allowA and \G when
// allowG. A pattern that uses neither is the same in every variant.
func (s *Scanner) variant(allowA, allowG bool) []*Regexp {
	if allowA && allowG {
		return s.base
	}
	slot := 0
	switch {
	case allowA:
		slot = 1
	case allowG:
		slot = 2
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.anchored[slot] != nil {
		return s.anchored[slot]
	}
	out := make([]*Regexp, len(s.patterns))
	for i, base := range s.base {
		if base == nil {
			continue
		}
		disableA := !allowA && base.UsesStartAnchor()
		disableG := !allowG && base.UsesAnchorPosition()
		if !disableA && !disableG {
			out[i] = base
			continue
		}
		// It compiled once, so it compiles again; if it does not, the pattern never matches.
		if re, err := s.compile(s.patterns[i], Options{DisableA: disableA, DisableG: disableG}); err == nil {
			out[i] = re
		}
	}
	s.anchored[slot] = out
	return out
}

// Find searches line from the byte offset pos for the match that starts first; when patterns
// match at the same place, the one that comes first wins. \G matches only when pos is
// anchorPos (the end of the last begin match; -1 for none), and \A only when isFirstLine. It
// reports false when nothing matches. A search that takes longer than the timeout has an error
// with the tag RegexTimeout.
func (s *Scanner) Find(line *Line, pos, anchorPos int, isFirstLine bool) (ScanMatch, bool, error) {
	if pos < 0 || pos > len(line.text) {
		return ScanMatch{}, false, nil
	}
	regs := s.variant(isFirstLine, pos == anchorPos)
	from := line.runeIndex(pos)
	started := time.Now()
	var best Match
	bestPattern, bestStart := -1, 0
	for i, re := range regs {
		if re == nil {
			continue
		}
		m, ok, err := re.Find(line.runes, from)
		if err != nil {
			return ScanMatch{}, false, err
		}
		if !ok {
			continue
		}
		if start := m.Groups[0].Start; bestPattern < 0 || start < bestStart {
			best, bestPattern, bestStart = m, i, start
			if start == from {
				// Nothing can start before this, and the patterns after it lose a tie.
				break
			}
		}
		if time.Since(started) > s.timeout {
			return ScanMatch{}, false, errors.New("RegexTimeout", "the search ran past its time limit", "patterns", len(s.patterns), "timeout", s.timeout.String())
		}
	}
	if bestPattern < 0 {
		return ScanMatch{}, false, nil
	}
	caps := make([]Capture, len(best.Groups))
	for i, g := range best.Groups {
		if g.Start < 0 {
			caps[i] = Capture{-1, -1}
			continue
		}
		caps[i] = Capture{line.byteOffset(g.Start), line.byteOffset(g.End)}
	}
	return ScanMatch{Pattern: bestPattern, Captures: caps}, true, nil
}
