package javascript

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/emetos/extension"
)

// Node's TAP reporter (SPEC §10.32 "The providers", JavaScript and TypeScript; "As built (step 26.8)"), read line by line as the runner
// prints it: `# Subtest: name` lines open a level (4 spaces of indent for each), `ok N - name` and `not ok N - name` lines close a test
// or a suite, and a YAML block (`---` to `...`, 2 spaces further in) after a result has its duration, its type (`test` or `suite`) and, for
// a failure, the error, the two sides of an assertion and the stack. Any other line is output of the tests.

// tapResult is a test or suite that the TAP stream closed.
type tapResult struct {
	// Path is the titles from the outermost suite down to the test.
	Path []string
	OK   bool
	// Skip is a test that ran with a SKIP or TODO directive.
	Skip bool
	// Type is "test" or "suite"; Duration is in milliseconds, negative when not given.
	Type     string
	Duration float64
	// FailureType, Error, Code, Location (file:line:col of the test) and Stack are the YAML fields of a failure; Expected and Actual are the
	// two sides of an assertion and HasSides says that the block had both.
	FailureType, Error, Code, Location, Stack string
	Expected, Actual                          string
	HasSides                                  bool
}

// tapEvent is what a line of the stream gave: a result, or text for the output of the run.
type tapEvent struct {
	Result *tapResult
	Output string
	// IsOutput says that Output is meant (an empty line of output is an event too).
	IsOutput bool
}

// tapParser reads the stream. Its methods are not safe for concurrent use.
type tapParser struct {
	names   []string
	pending *tapPending
}

// tapPending is a result whose YAML block may still follow.
type tapPending struct {
	res    tapResult
	indent int
	inYAML bool
	// block is the key of the block scalar being read, and lines its lines.
	block string
	lines []string
}

var (
	tapResultRe = regexp.MustCompile(`^(\s*)(not ok|ok) (\d+)(?: - (.*))?$`)
	tapDirRe    = regexp.MustCompile(` # (SKIP|TODO)\b.*$`)
	tapPlanRe   = regexp.MustCompile(`^\s*\d+\.\.\d+\s*$`)
)

// tapIndent counts the leading spaces of a line.
func tapIndent(l string) int { return len(l) - len(strings.TrimLeft(l, " ")) }

// Feed reads a line and returns the events it completed (a result is complete when its YAML block ends).
func (p *tapParser) Feed(line string) []tapEvent {
	var evs []tapEvent
	if p.pending != nil {
		if p.pending.inYAML {
			if p.yaml(line) {
				evs = append(evs, p.finish())
			}
			return evs
		}
		if strings.TrimSpace(line) == "---" && tapIndent(line) == p.pending.indent+2 {
			p.pending.inYAML = true
			return nil
		}
		evs = append(evs, p.finish())
	}
	trimmed := strings.TrimSpace(line)
	switch {
	case strings.HasPrefix(trimmed, "TAP version") || tapPlanRe.MatchString(line):
		return evs
	case strings.HasPrefix(trimmed, "# Subtest: "):
		depth := tapIndent(line) / 4
		if depth > len(p.names) {
			depth = len(p.names)
		}
		p.names = append(p.names[:depth], strings.TrimPrefix(trimmed, "# Subtest: "))
		return evs
	}
	if m := tapResultRe.FindStringSubmatch(line); m != nil {
		depth := len(m[1]) / 4
		name := strings.ReplaceAll(m[4], `\#`, "#")
		res := tapResult{OK: m[2] == "ok", Duration: -1}
		if d := tapDirRe.FindStringSubmatchIndex(name); d != nil {
			res.Skip = true
			name = name[:d[0]]
		}
		if depth > len(p.names) {
			depth = len(p.names)
		}
		p.names = append(p.names[:depth], name)
		res.Path = append([]string(nil), p.names...)
		p.pending = &tapPending{res: res, indent: len(m[1])}
		return evs
	}
	if strings.HasPrefix(trimmed, "# ") || trimmed == "#" {
		return append(evs, tapEvent{Output: strings.TrimPrefix(strings.TrimPrefix(trimmed, "#"), " "), IsOutput: true})
	}
	return append(evs, tapEvent{Output: line, IsOutput: true})
}

// Flush completes the result that is waiting, at the end of the stream.
func (p *tapParser) Flush() []tapEvent {
	if p.pending == nil {
		return nil
	}
	return []tapEvent{p.finish()}
}

func (p *tapParser) finish() tapEvent {
	res := p.pending.res
	p.pending = nil
	return tapEvent{Result: &res}
}

// yaml reads a line of the block and reports whether the block ended with it.
func (p *tapParser) yaml(line string) bool {
	pd := p.pending
	keyIndent := pd.indent + 2
	trimmed := strings.TrimSpace(line)
	if pd.block != "" {
		if trimmed == "" || tapIndent(line) > keyIndent {
			if len(line) > keyIndent+2 {
				pd.lines = append(pd.lines, line[keyIndent+2:])
			} else {
				pd.lines = append(pd.lines, "")
			}
			return false
		}
		p.setField(pd.block, strings.TrimRight(strings.Join(pd.lines, "\n"), "\n"))
		pd.block, pd.lines = "", nil
	}
	if trimmed == "..." {
		return true
	}
	key, value, ok := strings.Cut(trimmed, ":")
	if !ok {
		return false
	}
	value = strings.TrimSpace(value)
	if value == "|-" || value == "|" || value == "|+" {
		pd.block = key
		return false
	}
	p.setField(key, yamlScalar(value))
	return false
}

// yamlScalar reads a single-line YAML value: a quoted string is unquoted.
func yamlScalar(v string) string {
	if len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'' {
		return strings.ReplaceAll(v[1:len(v)-1], "''", "'")
	}
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		if s, err := strconv.Unquote(v); err == nil {
			return s
		}
		return v[1 : len(v)-1]
	}
	return v
}

func (p *tapParser) setField(key, value string) {
	r := &p.pending.res
	switch key {
	case "duration_ms":
		if d, err := strconv.ParseFloat(value, 64); err == nil {
			r.Duration = d
		}
	case "type":
		r.Type = value
	case "failureType":
		r.FailureType = value
	case "error":
		r.Error = value
	case "code":
		r.Code = value
	case "location":
		r.Location = value
	case "stack":
		r.Stack = value
	case "expected":
		r.Expected = value
		r.HasSides = r.HasSides || r.Actual != ""
	case "actual":
		r.Actual = value
		r.HasSides = r.HasSides || r.Expected != ""
	}
}

// toResult turns a closed test into a result of the file; it reports false for a suite, whose state is its tests'.
func (t tapResult) toResult(file string) (testResult, bool) {
	if t.Type == "suite" {
		return testResult{}, false
	}
	r := testResult{File: file, Path: t.Path, Duration: extension.TestNoDuration}
	if t.Duration >= 0 {
		r.Duration = time.Duration(t.Duration * float64(time.Millisecond))
	}
	switch {
	case t.OK && t.Skip:
		r.State = stateSkipped
	case t.OK:
		r.State = statePassed
	case t.FailureType == "cancelledByParent":
		r.State = stateErrored
	default:
		r.State = stateFailed
	}
	if !t.OK {
		r.Failures = []failure{t.failure()}
	}
	return r, true
}

// failure is the failure a closed test that did not pass tells of.
func (t tapResult) failure() failure {
	text := strings.TrimSpace(t.Error)
	if text == "" {
		text = "The test failed."
	}
	f := failure{Text: text, Frames: parseFrames(strings.Split(t.Stack, "\n"))}
	if t.HasSides {
		f.Expected, f.Actual = t.Expected, t.Actual
	}
	if len(f.Frames) == 0 {
		if m := frameRe.FindStringSubmatch(t.Location); m != nil {
			line, _ := strconv.Atoi(m[3])
			col, _ := strconv.Atoi(m[4])
			f.Frames = []frame{{File: m[2], Line: line, Col: col}}
		}
	}
	return f
}
