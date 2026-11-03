package rust

import (
	"regexp"
	"strconv"
	"strings"
)

// The output of libtest, Rust's test harness, in its human format (`cargo test` with no --format), which every Rust toolchain prints (the
// JSON format needs a nightly compiler). The parser is fed the lines of one test binary or of rustdoc's run of the documentation tests:
//
//	running 3 tests
//	test tests::adds ... ok
//	test tests::later ... ignored, slow
//	test tests::divides ... FAILED
//	test tests::zero_panics - should panic ... ok
//
//	failures:
//
//	---- tests::divides stdout ----
//	thread 'tests::divides' (123) panicked at src/lib.rs:43:9:
//	assertion `left == right` failed
//	  left: 5
//	 right: 4
//
//	failures:
//	    tests::divides
//
//	test result: FAILED. 1 passed; 1 failed; 1 ignored; 0 measured; 0 filtered out; finished in 0.00s
//
// With --show-output the output of the passing tests follows the results in a section "successes:" shaped like the failures one.

// outcome is how libtest says a test ended.
type outcome int

// The outcomes of a test line.
const (
	outcomeOK outcome = iota
	outcomeFailed
	outcomeIgnored
)

// eventKind says what a libtestEvent is.
type eventKind int

// The kinds of event.
const (
	// evLine is a line of the output that is not a result or a block of a test: cargo's own lines, "running 3 tests", the summary.
	evLine eventKind = iota
	// evRunning is "running N tests".
	evRunning
	// evResult is a line "test NAME ... ok".
	evResult
	// evBlock is the output of one test from the "failures:" or "successes:" section.
	evBlock
	// evSummary is the "test result:" line that ends a binary.
	evSummary
)

// libtestEvent is something the parser found in the output.
type libtestEvent struct {
	Kind eventKind
	// Text is the line (evLine) or the lines of the block (evBlock), with no trailing blank lines.
	Text string
	// Name is the name of the test (evResult, evBlock): "tests::adds", or "src/lib.rs - add (line 5)" for a documentation test.
	Name string
	// Outcome and Reason (the text after "ignored, ") are those of an evResult; ShouldPanic says the test is marked #[should_panic].
	Outcome     outcome
	Reason      string
	ShouldPanic bool
	// Success is true for a block from the section "successes:".
	Success bool
	// Count is the number of tests of "running N tests".
	Count int
	// Passed, Failed, Ignored, Filtered and Seconds are those of the summary; Ok is true when it says "ok".
	Passed, Failed, Ignored, Filtered int
	Seconds                           float64
	Ok                                bool
}

// section is the part of the output the parser is in.
type section int

const (
	sectionNone section = iota
	// sectionHeader follows "failures:" or "successes:" and is waiting for the first block or the list of names.
	sectionHeader
	sectionBlocks
	sectionList
)

// libtestParser reads the output of libtest line by line. It is not safe for concurrent use.
type libtestParser struct {
	section section
	success bool
	// block is the test whose output is being read; none when its name is empty and it has no lines.
	name  string
	lines []string
	open  bool
}

var (
	runningRe = regexp.MustCompile(`^running (\d+) tests?$`)
	resultRe  = regexp.MustCompile(`^test result: (ok|FAILED)\. (\d+) passed; (\d+) failed; (\d+) ignored; \d+ measured; (\d+) filtered out(?:; finished in ([0-9.]+)s)?`)
)

// flush ends the block being read.
func (p *libtestParser) flush() []libtestEvent {
	if !p.open {
		return nil
	}
	p.open = false
	for len(p.lines) > 0 && strings.TrimSpace(p.lines[len(p.lines)-1]) == "" {
		p.lines = p.lines[:len(p.lines)-1]
	}
	ev := libtestEvent{Kind: evBlock, Name: p.name, Text: strings.Join(p.lines, "\n"), Success: p.success}
	p.name, p.lines = "", nil
	return []libtestEvent{ev}
}

// Flush ends the output: a block still being read is complete.
func (p *libtestParser) Flush() []libtestEvent {
	p.section = sectionNone
	return p.flush()
}

// Feed reads a line and returns what it finished.
func (p *libtestParser) Feed(line string) []libtestEvent {
	switch {
	case line == "failures:" || line == "successes:":
		evs := p.flush()
		p.section, p.success = sectionHeader, line == "successes:"
		return evs
	case p.section != sectionNone && strings.HasPrefix(line, "---- ") && strings.HasSuffix(line, " ----"):
		if p.section == sectionList {
			break
		}
		evs := p.flush()
		name := strings.TrimSuffix(strings.TrimPrefix(line, "---- "), " ----")
		name = strings.TrimSuffix(name, " stdout")
		p.section, p.open, p.name = sectionBlocks, true, name
		return evs
	case strings.HasPrefix(line, "test result: "):
		evs := p.Flush()
		evs = append(evs, libtestEvent{Kind: evLine, Text: line})
		if m := resultRe.FindStringSubmatch(line); m != nil {
			ev := libtestEvent{Kind: evSummary, Ok: m[1] == "ok"}
			ev.Passed, _ = strconv.Atoi(m[2])
			ev.Failed, _ = strconv.Atoi(m[3])
			ev.Ignored, _ = strconv.Atoi(m[4])
			ev.Filtered, _ = strconv.Atoi(m[5])
			ev.Seconds, _ = strconv.ParseFloat(m[6], 64)
			evs = append(evs, ev)
		}
		return evs
	}
	switch p.section {
	case sectionBlocks:
		p.lines = append(p.lines, line)
		return nil
	case sectionHeader:
		switch {
		case strings.TrimSpace(line) == "":
			return nil
		case strings.HasPrefix(line, "    "):
			p.section = sectionList
			return nil
		}
		p.section = sectionNone
	case sectionList:
		if strings.HasPrefix(line, "    ") {
			return nil
		}
		p.section = sectionNone
		if strings.TrimSpace(line) == "" {
			return nil
		}
	}
	if m := runningRe.FindStringSubmatch(line); m != nil {
		n, _ := strconv.Atoi(m[1])
		return []libtestEvent{{Kind: evLine, Text: line}, {Kind: evRunning, Count: n}}
	}
	if ev, ok := parseTestLine(line); ok {
		return []libtestEvent{ev}
	}
	return []libtestEvent{{Kind: evLine, Text: line}}
}

// parseTestLine reads a line "test NAME ... OUTCOME".
func parseTestLine(line string) (libtestEvent, bool) {
	rest, ok := strings.CutPrefix(line, "test ")
	if !ok {
		return libtestEvent{}, false
	}
	i := strings.LastIndex(rest, " ... ")
	if i < 0 {
		return libtestEvent{}, false
	}
	name, res := rest[:i], rest[i+len(" ... "):]
	ev := libtestEvent{Kind: evResult, Name: name}
	if n, found := strings.CutSuffix(name, " - should panic"); found {
		ev.Name, ev.ShouldPanic = n, true
	}
	switch {
	case res == "ok":
		ev.Outcome = outcomeOK
	case res == "FAILED":
		ev.Outcome = outcomeFailed
	case res == "ignored":
		ev.Outcome = outcomeIgnored
	case strings.HasPrefix(res, "ignored, "):
		ev.Outcome, ev.Reason = outcomeIgnored, strings.TrimPrefix(res, "ignored, ")
	case strings.HasPrefix(res, "bench: "):
		ev.Outcome = outcomeOK
	default:
		return libtestEvent{}, false
	}
	return ev, true
}

// ---- the failures

// frame is a line of a backtrace that has a place.
type frame struct {
	Name, File string
	Line, Col  int
}

// panicInfo is a panic found in the output of a test.
type panicInfo struct {
	// Thread is the name of the thread that panicked; File, Line and Col (from 1) the place; Text the message.
	Thread, File string
	Line, Col    int
	Text         string
	// Cmp says that the message is a failed assert_eq!, with the two sides in Left and Right as the macro printed them (Debug).
	Cmp         bool
	Left, Right string
	// Frames are the frames of the backtrace that came after the panic, the innermost first.
	Frames []frame
}

var (
	panicNewRe = regexp.MustCompile(`^thread '(.*)' (?:\(\d+\) )?panicked at (.+?):(\d+):(\d+):$`)
	panicOldRe = regexp.MustCompile(`(?s)thread '([^']*)' panicked at '(.*?)', ([^\s:]+):(\d+):(\d+)`)
	frameRe    = regexp.MustCompile(`^\s*(\d+): (.+)$`)
	frameAtRe  = regexp.MustCompile(`^\s+at (.+?):(\d+)(?::(\d+))?$`)
	arrowRe    = regexp.MustCompile(`^\s*--> (.+?):(\d+):(\d+)$`)
)

// parsePanics finds the panics in the output of a test. A message that is a failed assert_eq! gets its two sides.
func parsePanics(text string) []panicInfo {
	lines := strings.Split(text, "\n")
	var out []panicInfo
	for i := 0; i < len(lines); i++ {
		m := panicNewRe.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		p := panicInfo{Thread: m[1], File: m[2]}
		p.Line, _ = strconv.Atoi(m[3])
		p.Col, _ = strconv.Atoi(m[4])
		var msg []string
		j := i + 1
		for ; j < len(lines); j++ {
			l := lines[j]
			if strings.HasPrefix(l, "note: run with `RUST_BACKTRACE") || l == "stack backtrace:" || panicNewRe.MatchString(l) {
				break
			}
			if strings.TrimSpace(l) == "" && (len(msg) == 0 || !isCmpContinuation(lines, j)) {
				break
			}
			msg = append(msg, l)
		}
		p.Text = strings.TrimRight(strings.Join(msg, "\n"), "\n ")
		p.setSides()
		if j < len(lines) && lines[j] != "" {
			k := j
			if strings.HasPrefix(lines[k], "note: run with") {
				k++
			}
			p.Frames, k = parseFrames(lines, k)
			j = k - 1
		}
		out = append(out, p)
		i = j - 1
		if i < 0 {
			i = 0
		}
	}
	if len(out) > 0 {
		return out
	}
	for _, m := range panicOldRe.FindAllStringSubmatch(text, -1) {
		p := panicInfo{Thread: m[1], Text: m[2], File: m[3]}
		p.Line, _ = strconv.Atoi(m[4])
		p.Col, _ = strconv.Atoi(m[5])
		p.setOldSides()
		out = append(out, p)
	}
	return out
}

// isCmpContinuation tells whether the blank line at i is inside the value of an assert_eq! (a side that has blank lines): the macro's
// lines "  left:" and " right:" are not far.
func isCmpContinuation(lines []string, i int) bool {
	sawLeft := false
	for _, l := range lines[max(0, i-200):i] {
		if strings.HasPrefix(l, "  left: ") {
			sawLeft = true
		}
		if strings.HasPrefix(l, " right: ") {
			return false
		}
	}
	return sawLeft
}

// parseFrames reads the frames of "stack backtrace:" that have a file, from line i; it returns them and the line it stopped at.
func parseFrames(lines []string, i int) ([]frame, int) {
	if i >= len(lines) || lines[i] != "stack backtrace:" {
		return nil, i
	}
	var out []frame
	var cur *frame
	k := i + 1
	for ; k < len(lines); k++ {
		l := lines[k]
		if m := frameRe.FindStringSubmatch(l); m != nil {
			out = append(out, frame{Name: strings.TrimSpace(m[2])})
			cur = &out[len(out)-1]
			continue
		}
		if m := frameAtRe.FindStringSubmatch(l); m != nil && cur != nil {
			cur.File = m[1]
			cur.Line, _ = strconv.Atoi(m[2])
			cur.Col, _ = strconv.Atoi(m[3])
			continue
		}
		if strings.HasPrefix(l, "note: Some details are omitted") || strings.TrimSpace(l) == "" {
			break
		}
	}
	return out, k
}

// setSides reads the two sides of a failed assert_eq!:
//
//	assertion `left == right` failed: the optional message
//	  left: 5
//	 right: 4
func (p *panicInfo) setSides() {
	lines := strings.Split(p.Text, "\n")
	if !strings.HasPrefix(lines[0], "assertion `left == right` failed") {
		return
	}
	left, right := -1, -1
	for i, l := range lines {
		switch {
		case left < 0 && strings.HasPrefix(l, "  left: "):
			left = i
		case left >= 0 && right < 0 && strings.HasPrefix(l, " right: "):
			right = i
		}
	}
	if left < 0 || right < 0 {
		return
	}
	p.Cmp = true
	p.Left = strings.Join(append([]string{strings.TrimPrefix(lines[left], "  left: ")}, lines[left+1:right]...), "\n")
	p.Right = strings.Join(append([]string{strings.TrimPrefix(lines[right], " right: ")}, lines[right+1:]...), "\n")
}

// setOldSides reads the sides of the message of a compiler before Rust 1.73:
//
//	assertion failed: `(left == right)`
//	  left: `5`,
//	 right: `4`
func (p *panicInfo) setOldSides() {
	lines := strings.Split(p.Text, "\n")
	if !strings.HasPrefix(lines[0], "assertion failed: `(left == right)`") || len(lines) < 3 {
		return
	}
	side := func(l, prefix string) (string, bool) {
		v, ok := strings.CutPrefix(l, prefix)
		if !ok {
			return "", false
		}
		v = strings.TrimSuffix(v, ",")
		if len(v) >= 2 && v[0] == '`' && v[len(v)-1] == '`' {
			v = v[1 : len(v)-1]
		}
		return v, true
	}
	left, ok1 := side(lines[1], "  left: ")
	right, ok2 := side(lines[2], " right: ")
	if ok1 && ok2 {
		p.Cmp, p.Left, p.Right = true, left, right
	}
}

// compilerPlace is the first place a compiler error points at (`--> src/lib.rs:6:5`), for a test that did not compile.
func compilerPlace(text string) (file string, line, col int, ok bool) {
	for _, l := range strings.Split(text, "\n") {
		if m := arrowRe.FindStringSubmatch(l); m != nil {
			line, _ = strconv.Atoi(m[2])
			col, _ = strconv.Atoi(m[3])
			return m[1], line, col, true
		}
	}
	return "", 0, 0, false
}
