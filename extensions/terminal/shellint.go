package terminal

import (
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/rm4n0s/emetos/extensions/terminal/emulator"
)

// Shell integration (SPEC §10.25 "Shell integration"): the shell tells the terminal where each prompt, command and output
// starts and ends with OSC 633 (VS Code's scripts), OSC 133 (FinalTerm, used by many shells and prompts) and OSC 1337 (iTerm2:
// CurrentDir and SetMark); this file turns those strings into commands. The positions are markers of the emulator, which move
// with their lines through the scrollback and reflow, so a command's place is still right after a resize.
//
// The sequences, as VS Code's documentation lists them (https://code.visualstudio.com/docs/terminal/shell-integration) and
// as its scripts print them:
//
//	OSC 633 ; A       the prompt starts
//	OSC 633 ; B       the prompt ends and the command line starts
//	OSC 633 ; C       the command was run and its output starts
//	OSC 633 ; D [; n] the command ended, with the exit status n when the shell knows it
//	OSC 633 ; E ; line [; nonce]   the command line, escaped (\\ and \xAB), with the secret of the terminal
//	OSC 633 ; P ; Name=Value       a property: Cwd, HasRichCommandDetection, IsWindows, PromptType ...
//	OSC 133 ; A, B, C and D [; n]  the same four, from the FinalTerm protocol
//	OSC 1337 ; CurrentDir=path     the working folder; OSC 1337 ; SetMark  a mark the user can jump to

// The OSC codes that carry the shell integration; the emulator makes a marker for each of their strings.
var shellOSCCodes = []int{133, 633, 1337}

// maxCommands is the most finished commands a terminal keeps; the oldest are forgotten first. The ones whose lines left the
// scrollback are forgotten earlier.
const maxCommands = 4000

// Command line confidence, as in VS Code: high is the line the shell reported with the secret of the terminal, medium a line
// read from the screen that starts after the prompt, low any other line read from the screen.
const (
	confLow    = "low"
	confMedium = "medium"
	confHigh   = "high"
)

// command is one command of the shell: its markers, columns, line, working folder and exit status.
type command struct {
	// text is the command line; confidence says how far it can be trusted; trusted that the shell reported it with the nonce.
	text       string
	confidence string
	trusted    bool
	// cwd is the working folder when the command started.
	cwd string
	// exit is the exit status, hasExit says the shell reported one (a command that was interrupted has none).
	exit    int
	hasExit bool
	// the markers: the prompt starts, the command line starts (the line the decoration sits on), the command was run (the output
	// starts), and the command ended. 0 is none.
	promptStart, start, executed, end emulator.MarkerID
	// startX and executedX are the columns where the command line and the output start.
	startX, executedX, endX int
	// started is when the command was run, duration how long it took.
	started  time.Time
	duration time.Duration
}

// cmdView is a command with the lines its markers are on now (absolute lines; -1 for none or for one that left the buffer).
type cmdView struct {
	command
	promptLine, startLine, execLine, endLine int64
}

// hasOutput tells whether the command wrote lines between its run and its end.
func (v *cmdView) hasOutput() bool {
	return v.execLine >= 0 && v.endLine >= 0 && v.execLine < v.endLine
}

// top is the first line of the command: its prompt, else the line of the command line.
func (v *cmdView) top() int64 {
	if v.promptLine >= 0 {
		return v.promptLine
	}
	return v.startLine
}

// shellState follows the shell integration of one terminal. The parser goroutine feeds it (handle); the user interface and the
// requests read it. Its mutex is never held while the emulator's is wanted by the parser, so the two cannot lock each other.
type shellState struct {
	em    *emulator.Emulator
	nonce string
	now   func() time.Time

	mu sync.Mutex
	// active says that the shell sent a sequence of the integration; rich that it said it detects commands richly.
	active, rich bool
	// cmds are the finished commands in order, cur the one being typed or run.
	cmds []*command
	cur  *command
	// marks are the marks the user can jump to (OSC 1337 SetMark).
	marks []emulator.MarkerID
	// cwd is the working folder the shell reported; cwds the folders it visited, in order, without repeats of the last.
	cwd  string
	cwds []string
	// gen counts the changes, for whoever caches what it drew.
	gen uint64
	// onFinish is called, without the lock, with every command that ended; onChange when the state changed.
	onFinish func(v cmdView)
	onChange func()
	// onCwd is called with a folder the shell reported, and also onCwdMore (set by SetHooks).
	onCwd, onCwdMore func(dir string)
}

func newShellState(em *emulator.Emulator, nonce string) *shellState {
	return &shellState{em: em, nonce: nonce, now: time.Now}
}

// Active tells whether the shell sent any sequence of the integration.
func (s *shellState) Active() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active
}

// Gen is a number that changes with every change of the commands.
func (s *shellState) Gen() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gen
}

// handle takes an OSC string the emulator passed on. It runs on the goroutine that feeds the emulator.
func (s *shellState) handle(ev emulator.OSCEvent) {
	switch ev.Code {
	case 133, 633:
		s.handleMark(ev)
	case 1337:
		s.handle1337(ev)
	default:
		if ev.Marker != 0 {
			s.em.RemoveMarker(ev.Marker)
		}
	}
}

// handle1337 reads CurrentDir=path and SetMark of iTerm2's protocol (and ignores the rest).
func (s *shellState) handle1337(ev emulator.OSCEvent) {
	switch {
	case ev.Data == "SetMark" && ev.Marker != 0:
		s.mu.Lock()
		s.marks = append(s.marks, ev.Marker)
		s.active = true
		s.gen++
		s.mu.Unlock()
		s.changed()
		return
	case strings.HasPrefix(ev.Data, "CurrentDir="):
		s.setCwd(strings.TrimPrefix(ev.Data, "CurrentDir="))
	}
	if ev.Marker != 0 {
		s.em.RemoveMarker(ev.Marker)
	}
}

// setCwd records the working folder the shell reported.
func (s *shellState) setCwd(dir string) {
	if dir == "" {
		return
	}
	s.mu.Lock()
	s.cwd = dir
	s.active = true
	if len(s.cwds) == 0 || s.cwds[len(s.cwds)-1] != dir {
		s.cwds = append(s.cwds, dir)
		if len(s.cwds) > 1000 {
			s.cwds = slices.Delete(s.cwds, 0, len(s.cwds)-1000)
		}
	}
	f, more := s.onCwd, s.onCwdMore
	s.mu.Unlock()
	if f != nil {
		f(dir)
	}
	if more != nil {
		more(dir)
	}
}

// SetHooks sets the functions that are called, from the goroutine that feeds the emulator, with every command that ended and
// with every folder the shell reports.
func (s *shellState) SetHooks(onFinish func(v cmdView), onCwd func(dir string)) {
	s.mu.Lock()
	s.onFinish, s.onCwdMore = onFinish, onCwd
	s.mu.Unlock()
}

// handleMark takes a string of OSC 133 or 633.
func (s *shellState) handleMark(ev emulator.OSCEvent) {
	if ev.Alt {
		return
	}
	kind, rest, _ := strings.Cut(ev.Data, ";")
	if kind == "" {
		return
	}
	switch kind {
	case "A", "B", "C", "D":
		s.sequence(kind, rest, ev)
	case "E":
		if ev.Code == 633 {
			s.commandLine(rest)
		}
		s.drop(ev)
	case "P":
		if ev.Code == 633 {
			s.property(rest)
		}
		s.drop(ev)
	default: // F, G, H, I (continuation and right prompts) and the properties of the environment
		s.drop(ev)
	}
}

// drop forgets the marker of a string that needs none.
func (s *shellState) drop(ev emulator.OSCEvent) {
	if ev.Marker != 0 {
		s.em.RemoveMarker(ev.Marker)
	}
}

// property reads P ; Name=Value.
func (s *shellState) property(rest string) {
	name, value, ok := strings.Cut(rest, "=")
	if !ok {
		return
	}
	switch name {
	case "Cwd":
		s.setCwd(unescapeValue(value))
	case "HasRichCommandDetection":
		s.mu.Lock()
		s.rich = strings.EqualFold(value, "true")
		s.active = true
		s.mu.Unlock()
	}
}

// commandLine reads E ; line [; nonce]: the line, escaped, and the secret of the terminal. A line without the right secret was
// printed by a program, not by the shell script, and is not believed.
func (s *shellState) commandLine(rest string) {
	text, nonce, hasNonce := cutLast(rest, ";")
	if !hasNonce {
		return
	}
	if s.nonce == "" || nonce != s.nonce {
		return
	}
	s.mu.Lock()
	if s.cur == nil {
		s.cur = &command{}
	}
	s.cur.text, s.cur.confidence, s.cur.trusted = unescapeValue(text), confHigh, true
	s.mu.Unlock()
}

// cutLast splits s at the last sep.
func cutLast(s, sep string) (before, after string, found bool) {
	i := strings.LastIndex(s, sep)
	if i < 0 {
		return s, "", false
	}
	return s[:i], s[i+len(sep):], true
}

// sequence takes A, B, C and D.
func (s *shellState) sequence(kind, rest string, ev emulator.OSCEvent) {
	s.mu.Lock()
	s.active = true
	s.gen++
	var done *cmdView
	var forget []emulator.MarkerID
	switch kind {
	case "A": // the prompt starts
		if s.cur != nil && s.cur.executed == 0 {
			forget = append(forget, s.cur.promptStart, s.cur.start)
			s.cur = nil
		}
		if s.cur == nil {
			s.cur = &command{}
		}
		if s.cur.promptStart != 0 {
			forget = append(forget, s.cur.promptStart)
		}
		s.cur.promptStart = ev.Marker
	case "B": // the command line starts
		if s.cur == nil {
			s.cur = &command{}
		}
		if s.cur.start != 0 {
			forget = append(forget, s.cur.start)
		}
		if s.cur.executed != 0 {
			forget = append(forget, s.cur.executed)
			s.cur.executed = 0
		}
		s.cur.start, s.cur.startX, s.cur.cwd = ev.Marker, ev.Col, s.cwd
	case "C": // the command was run
		if s.cur == nil || s.cur.start == 0 {
			forget = append(forget, ev.Marker)
			break
		}
		if s.cur.executed != 0 {
			forget = append(forget, s.cur.executed)
		}
		s.cur.executed, s.cur.executedX, s.cur.started = ev.Marker, ev.Col, s.now()
		s.readCommandLine(s.cur)
	case "D": // the command ended
		c := s.cur
		s.cur = nil
		if c == nil || c.start == 0 {
			forget = append(forget, ev.Marker)
			if c != nil {
				forget = append(forget, c.promptStart, c.start, c.executed)
			}
			break
		}
		if c.executed == 0 {
			// a command that was never run (the line was empty, or it was interrupted): nothing to keep
			forget = append(forget, ev.Marker, c.promptStart, c.start)
			break
		}
		c.end, c.endX = ev.Marker, ev.Col
		code, _, _ := strings.Cut(rest, ";")
		if n, err := strconv.Atoi(strings.TrimSpace(code)); err == nil {
			c.exit, c.hasExit = n, true
		}
		if !c.started.IsZero() {
			c.duration = s.now().Sub(c.started)
		}
		if strings.TrimSpace(c.text) == "" {
			forget = append(forget, ev.Marker, c.promptStart, c.start, c.executed)
			break
		}
		s.cmds = append(s.cmds, c)
		forget = append(forget, s.trimLocked()...)
		v := s.viewLocked(c)
		done = &v
	}
	finish, change := s.onFinish, s.onChange
	s.mu.Unlock()
	for _, id := range forget {
		if id != 0 {
			s.em.RemoveMarker(id)
		}
	}
	if done != nil && finish != nil {
		finish(*done)
	}
	if change != nil {
		change()
	}
}

// readCommandLine reads the command line from the screen when the shell did not report it: the text between the end of the
// prompt and the place the output starts. Called with the lock held.
func (s *shellState) readCommandLine(c *command) {
	if c.confidence == confHigh {
		return
	}
	lines := s.em.MarkerLines([]emulator.MarkerID{c.start, c.executed}, nil)
	if lines[0] < 0 || lines[1] < 0 {
		return
	}
	text := strings.TrimSpace(s.em.TextRange(lines[0], c.startX, lines[1], c.executedX))
	c.text = text
	c.confidence = confLow
	// VS Code: a single line that starts after the prompt (not at the left edge) is medium
	if c.startX > 0 && !strings.Contains(text, "\n") {
		c.confidence = confMedium
	}
}

// trimLocked drops the oldest commands when there are too many, and the ones whose lines left the scrollback; it returns their
// markers. Called with the lock held.
func (s *shellState) trimLocked() []emulator.MarkerID {
	var forget []emulator.MarkerID
	drop := func(c *command) { forget = append(forget, c.promptStart, c.start, c.executed, c.end) }
	if len(s.cmds) > maxCommands {
		n := len(s.cmds) - maxCommands
		for _, c := range s.cmds[:n] {
			drop(c)
		}
		s.cmds = slices.Delete(s.cmds, 0, n)
	}
	// the commands are in the order of their lines: the ones that left the scrollback are at the front
	if len(s.cmds) > 0 {
		ids := make([]emulator.MarkerID, len(s.cmds))
		for i, c := range s.cmds {
			ids[i] = c.start
		}
		lines := s.em.MarkerLines(ids, nil)
		n := 0
		for n < len(lines) && lines[n] < 0 {
			n++
		}
		if n > 0 {
			for _, c := range s.cmds[:n] {
				drop(c)
			}
			s.cmds = slices.Delete(s.cmds, 0, n)
		}
	}
	if len(s.marks) > 0 {
		lines := s.em.MarkerLines(s.marks, nil)
		keep := s.marks[:0]
		for i, id := range s.marks {
			if lines[i] >= 0 {
				keep = append(keep, id)
			} else {
				forget = append(forget, id)
			}
		}
		s.marks = keep
	}
	return forget
}

func (s *shellState) changed() {
	s.mu.Lock()
	f := s.onChange
	s.mu.Unlock()
	if f != nil {
		f()
	}
}

// viewLocked resolves the markers of a command. Called with the lock held.
func (s *shellState) viewLocked(c *command) cmdView {
	lines := s.em.MarkerLines([]emulator.MarkerID{c.promptStart, c.start, c.executed, c.end}, nil)
	return cmdView{command: *c, promptLine: lines[0], startLine: lines[1], execLine: lines[2], endLine: lines[3]}
}

// Commands are the finished commands whose command line is still in the buffer, oldest first, with their lines.
func (s *shellState) Commands() []cmdView {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.cmds) == 0 {
		return nil
	}
	ids := make([]emulator.MarkerID, 0, 4*len(s.cmds))
	for _, c := range s.cmds {
		ids = append(ids, c.promptStart, c.start, c.executed, c.end)
	}
	lines := s.em.MarkerLines(ids, nil)
	out := make([]cmdView, 0, len(s.cmds))
	for i, c := range s.cmds {
		v := cmdView{command: *c, promptLine: lines[4*i], startLine: lines[4*i+1], execLine: lines[4*i+2], endLine: lines[4*i+3]}
		if v.startLine < 0 {
			continue
		}
		out = append(out, v)
	}
	return out
}

// Current is the command being typed or run, nil when the shell shows no prompt (or sent none).
func (s *shellState) Current() *cmdView {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cur == nil || s.cur.start == 0 {
		return nil
	}
	v := s.viewLocked(s.cur)
	if v.startLine < 0 {
		return nil
	}
	return &v
}

// Marks are the lines of the marks the user can jump to, in order.
func (s *shellState) Marks() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.marks) == 0 {
		return nil
	}
	lines := s.em.MarkerLines(s.marks, nil)
	out := make([]int64, 0, len(lines))
	for _, l := range lines {
		if l >= 0 {
			out = append(out, l)
		}
	}
	slices.Sort(out)
	return out
}

// Cwd is the working folder the shell reported ("" for none).
func (s *shellState) Cwd() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cwd
}

// Cwds are the folders the shell reported in this session, in order of visit, each folder once (its latest visit).
func (s *shellState) Cwds() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for i := len(s.cwds) - 1; i >= 0; i-- {
		if !slices.Contains(out, s.cwds[i]) {
			out = append(out, s.cwds[i])
		}
	}
	slices.Reverse(out)
	return out
}

// Clear forgets the finished commands (the screen was cleared by a program that deletes the scrollback).
func (s *shellState) Clear() {
	s.mu.Lock()
	var forget []emulator.MarkerID
	for _, c := range s.cmds {
		forget = append(forget, c.promptStart, c.start, c.executed, c.end)
	}
	s.cmds = nil
	forget = append(forget, s.marks...)
	s.marks = nil
	s.gen++
	s.mu.Unlock()
	for _, id := range forget {
		if id != 0 {
			s.em.RemoveMarker(id)
		}
	}
}

// ---- reading the text ----

// outputLines are the logical lines of the output as the terminal had them when the command ended, for the quick fixes: from
// the line where the command was run to the line where it ended, which is read up to the column where the end was reported (the
// callbacks run after the chunk was parsed, and the prompt may be on that line already).
func (s *shellState) outputLines(v cmdView) []string {
	if v.execLine < 0 || v.endLine < 0 {
		return nil
	}
	text := s.em.TextRange(v.execLine, 0, v.endLine, v.endX)
	return strings.Split(text, "\n")
}

// output is the text a command wrote: the lines from the one where it was run to the one where it ended (the line of the
// next prompt is not part of it). A command with no output gives "".
func (s *shellState) output(v cmdView) string {
	if !v.hasOutput() {
		return ""
	}
	return s.em.TextRange(v.execLine, 0, v.endLine, 0)
}

// promptRows is the number of rows of the prompt up to and including the command line, without the empty rows at its top.
func (s *shellState) promptRows(v cmdView) int {
	if v.promptLine < 0 || v.startLine < 0 {
		return 1
	}
	first := v.promptLine
	for first < v.startLine {
		text, _ := s.em.LineText(int(first - s.em.FirstLine()))
		if text != "" {
			break
		}
		first++
	}
	return int(v.startLine-first) + 1
}

// commandRows is the number of rows the command line takes: from its first row to the one where the output starts (that row
// does not count when the output starts at its first column).
func (v *cmdView) commandRows() int {
	if v.startLine < 0 || v.execLine < 0 {
		return 1
	}
	n := int(max(v.execLine, v.startLine)-v.startLine) + 1
	if v.executedX == 0 {
		n--
	}
	return max(n, 1)
}

// ---- the text of the sequences ----

// unescapeValue undoes the escapes of the scripts: a doubled backslash is one, and \xAB is the byte AB (the scripts write the
// control characters and the semicolon so).
func unescapeValue(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			switch {
			case s[i+1] == '\\':
				b.WriteByte('\\')
				i++
				continue
			case s[i+1] == 'x' && i+3 < len(s) && isHex(s[i+2]) && isHex(s[i+3]):
				v, _ := strconv.ParseUint(s[i+2:i+4], 16, 8)
				b.WriteByte(byte(v))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	out := b.String()
	if !utf8.ValidString(out) {
		out = strings.ToValidUTF8(out, "�")
	}
	return out
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}
