package debug

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/rm4n0s/emetos/internal/core/dap"
)

// The Debug Console's model (SPEC §10.31 "The Debug Console", step 25.6): what a session printed and what was typed in the console,
// as a list of elements (output lines, evaluations, groups, objects) in the order they came. Each session has one, or shares the one of
// its parent (SessionOptions.Repl). It is plain data: the view reads it and follows its changes (Subscribe).
//
// Ported from VS Code: src/vs/workbench/contrib/debug/common/replModel.ts

// ReplSeverity is how important a line of the console is: it picks the color of the line (debugConsole.*Foreground).
type ReplSeverity int

const (
	// ReplIgnore is a line of the console's own (italic, "Console was cleared").
	ReplIgnore ReplSeverity = iota
	// ReplInfo is the output of the program (the categories stdout and the rest).
	ReplInfo
	// ReplWarning is the output of the category console (the adapter's own messages).
	ReplWarning
	// ReplError is the output of the category stderr.
	ReplError
)

// ReplSeverityOfCategory is the severity of the category of an output event: stderr is an error, console a warning, the rest information.
func ReplSeverityOfCategory(category string) ReplSeverity {
	switch category {
	case "stderr":
		return ReplError
	case "console":
		return ReplWarning
	}
	return ReplInfo
}

// ReplSource is where an output came from: the file and the place the adapter named, which the console shows as `name:line` at the
// end of the line.
type ReplSource struct {
	Source Source
	Line   int
	Column int
}

func (a *ReplSource) equal(b *ReplSource) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Column == b.Column && a.Line == b.Line && a.Source.Key() == b.Source.Key()
}

// ReplConfig is what the console reads of the settings: debug.console.collapseIdenticalLines and debug.console.maximumLines.
type ReplConfig struct {
	CollapseIdenticalLines bool
	MaximumLines           int
}

// DefaultReplConfig is VS Code's default of the two settings.
var DefaultReplConfig = ReplConfig{CollapseIdenticalLines: true, MaximumLines: 10000}

// ReplElement is an element of the console: an output, an input, a result, a group or a value.
type ReplElement interface {
	// ID identifies the element for as long as it lives; the view keeps what is open by it.
	ID() string
	// Text is the text of the element; with includeSource the name of the file it came from follows. The filter looks at it with the
	// source, copying uses it without.
	Text(includeSource bool) string
	// Source is where the element came from, or nil.
	Source() *ReplSource
}

var replCounter atomic.Int64

func nextReplID() string { return "topReplElement:" + strconv.FormatInt(replCounter.Add(1), 10) }

// ReplOutput is the general case of an output event: a text with its severity and source. A text that is repeated stands once with a
// count. With an Expression the output is also an object to open.
//
// Ported from VS Code: replModel.ts (ReplOutputElement)
type ReplOutput struct {
	id string
	// Value is the text; it ends in a line feed when it is a whole line.
	Value    string
	Severity ReplSeverity
	Src      *ReplSource
	// Expression is the object behind the output (an output event with a variablesReference), or nil.
	Expression Node
	// Session is the session that printed it.
	Session *Session
	count   atomic.Int32
}

// ID implements ReplElement.
func (o *ReplOutput) ID() string { return o.id }

// Source implements ReplElement.
func (o *ReplOutput) Source() *ReplSource { return o.Src }

// Count is how many times the text came in a row (1 without collapsing).
func (o *ReplOutput) Count() int { return int(o.count.Load()) }

// HasChildren tells whether the output stands for an object with children.
func (o *ReplOutput) HasChildren() bool { return o.Expression != nil && o.Expression.HasChildren() }

// Text implements ReplElement: the text repeated Count times, one after the other.
func (o *ReplOutput) Text(includeSource bool) string {
	v := o.Value
	for i := 1; i < o.Count(); i++ {
		if !strings.HasSuffix(v, "\n") {
			v += "\n"
		}
		v += o.Value
	}
	if includeSource && o.Src != nil {
		v += " " + o.Src.Source.Name
	}
	return v
}

// ReplVariable is a variable logged by the program with no text: a top-level value of the console.
//
// Ported from VS Code: replModel.ts (ReplVariableElement)
type ReplVariable struct {
	id         string
	Expression Node
	Severity   ReplSeverity
	Src        *ReplSource
	Session    *Session
}

// ID implements ReplElement.
func (v *ReplVariable) ID() string { return v.id }

// Source implements ReplElement.
func (v *ReplVariable) Source() *ReplSource { return v.Src }

// HasChildren tells whether the value can be opened.
func (v *ReplVariable) HasChildren() bool { return v.Expression.HasChildren() }

// Text implements ReplElement.
func (v *ReplVariable) Text(bool) string { return nodeText(v.Expression) }

// nodeText is VS Code's toString() of a variable (name:value) or of any other expression container (the value).
func nodeText(n Node) string {
	if x, ok := n.(*Variable); ok && x.Name != "" {
		return x.Name + ":" + x.Value()
	}
	return n.Value()
}

// ReplInput is a line the user typed in the console to evaluate.
//
// Ported from VS Code: replModel.ts (ReplEvaluationInput)
type ReplInput struct {
	id    string
	Value string
}

// ID implements ReplElement.
func (i *ReplInput) ID() string { return i.id }

// Source implements ReplElement.
func (i *ReplInput) Source() *ReplSource { return nil }

// Text implements ReplElement.
func (i *ReplInput) Text(bool) string { return i.Value }

// ReplResult is the result of evaluating a line of the console; it can be opened when the value has children.
//
// Ported from VS Code: replModel.ts (ReplEvaluationResult)
type ReplResult struct {
	// Original is the expression that was evaluated.
	Original string
	// Expression holds the value, its type and its children.
	Expression *Expression
}

// ID implements ReplElement.
func (r *ReplResult) ID() string { return r.Expression.ID() }

// Source implements ReplElement.
func (r *ReplResult) Source() *ReplSource { return nil }

// Text implements ReplElement: the value.
func (r *ReplResult) Text(bool) string { return r.Expression.Value() }

// HasChildren tells whether the value can be opened.
func (r *ReplResult) HasChildren() bool { return r.Expression.HasChildren() }

// Available is false when the evaluation failed.
func (r *ReplResult) Available() bool { return r.Expression.Available() }

// ReplGroup is a group of lines (console.group): its name, and the elements printed while it was open.
//
// Ported from VS Code: replModel.ts (ReplGroup)
type ReplGroup struct {
	id      string
	Session *Session
	// Name is the title of the group.
	Name string
	// AutoExpand is true for a group that starts open (group "start"), false for "startCollapsed".
	AutoExpand bool
	Src        *ReplSource

	mu       sync.Mutex
	children []ReplElement
	ended    bool
}

// ID implements ReplElement.
func (g *ReplGroup) ID() string { return g.id }

// Source implements ReplElement.
func (g *ReplGroup) Source() *ReplSource { return g.Src }

// Text implements ReplElement: the name.
func (g *ReplGroup) Text(includeSource bool) string {
	if includeSource && g.Src != nil {
		return g.Name + " " + g.Src.Source.Name
	}
	return g.Name
}

// Children are the elements of the group.
func (g *ReplGroup) Children() []ReplElement {
	g.mu.Lock()
	defer g.mu.Unlock()
	return slices.Clone(g.children)
}

// Ended tells whether the group was closed.
func (g *ReplGroup) Ended() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.ended
}

// lastOpenGroupLocked is the group that is open at the end of the children, or nil.
func (g *ReplGroup) lastOpenGroupLocked() *ReplGroup {
	if n := len(g.children); n > 0 {
		if last, ok := g.children[n-1].(*ReplGroup); ok && !last.Ended() {
			return last
		}
	}
	return nil
}

func (g *ReplGroup) addChild(child ReplElement) {
	g.mu.Lock()
	last := g.lastOpenGroupLocked()
	if last == nil {
		g.children = append(g.children, child)
	}
	g.mu.Unlock()
	if last != nil {
		last.addChild(child)
	}
}

func (g *ReplGroup) end() {
	g.mu.Lock()
	last := g.lastOpenGroupLocked()
	if last == nil {
		g.ended = true
	}
	g.mu.Unlock()
	if last != nil {
		last.end()
	}
}

// ReplData is a new element: the text of an output with its severity and source, and the object behind it when it has one (with no
// text the object alone is the element).
type ReplData struct {
	Output     string
	Expression Node
	Severity   ReplSeverity
	Source     *ReplSource
}

// ReplModel is the list of elements of a console. It is safe for use from several goroutines; its changes are told to the listeners
// (Subscribe) from the goroutine that made them, after the lock was released.
//
// Ported from VS Code: replModel.ts (ReplModel)
type ReplModel struct {
	cfg func() ReplConfig

	mu       sync.Mutex
	elements []ReplElement
	version  uint64

	subMu sync.Mutex
	next  int
	subs  map[int]func()
}

// NewReplModel makes an empty console; cfg gives the settings it reads when a line comes (DefaultReplConfig when nil).
func NewReplModel(cfg func() ReplConfig) *ReplModel {
	if cfg == nil {
		cfg = func() ReplConfig { return DefaultReplConfig }
	}
	return &ReplModel{cfg: cfg}
}

// Subscribe calls fn after the elements changed (a line was added, the count of a line changed, the console was cleared). fn must not
// block. The returned function stops it.
func (m *ReplModel) Subscribe(fn func()) (cancel func()) {
	m.subMu.Lock()
	defer m.subMu.Unlock()
	if m.subs == nil {
		m.subs = map[int]func(){}
	}
	m.next++
	id := m.next
	m.subs[id] = fn
	return func() {
		m.subMu.Lock()
		defer m.subMu.Unlock()
		delete(m.subs, id)
	}
}

func (m *ReplModel) fire() {
	m.subMu.Lock()
	ids := make([]int, 0, len(m.subs))
	for id := range m.subs {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	fns := make([]func(), 0, len(ids))
	for _, id := range ids {
		fns = append(fns, m.subs[id])
	}
	m.subMu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

// Elements returns the top-level elements, the oldest first. The slice is a copy.
func (m *ReplModel) Elements() []ReplElement {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.elements)
}

// Len is the number of top-level elements.
func (m *ReplModel) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.elements)
}

// Version counts the changes; a reader that kept the elements of a version knows they are current while it is the same.
func (m *ReplModel) Version() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.version
}

func (m *ReplModel) newOutput(s *Session, output string, sev ReplSeverity, src *ReplSource, expr Node) *ReplOutput {
	o := &ReplOutput{id: nextReplID(), Value: output, Severity: sev, Src: src, Expression: expr, Session: s}
	o.count.Store(1)
	return o
}

// Clear removes every element (VS Code's removeReplExpressions).
func (m *ReplModel) Clear() {
	m.mu.Lock()
	had := len(m.elements) > 0
	if had {
		m.elements = nil
		m.version++
	}
	m.mu.Unlock()
	if had {
		m.fire()
	}
}

// AddExpression adds what the user typed, evaluates it in the frame (in the session alone without one) with the context repl, and adds
// the result. The call returns when the adapter has answered or ctx is done; the input is in the console at once.
func (m *ReplModel) AddExpression(ctx context.Context, s *Session, frame *StackFrame, expression string) {
	m.addElement(&ReplInput{id: nextReplID(), Value: expression})
	result := NewExpression(expression)
	_ = result.Evaluate(ctx, s, frame, dap.ContextRepl, false) // a failure is the value of the result
	m.addElement(&ReplResult{Original: expression, Expression: result})
}

const clearANSISequence = "\x1b[2J"

// Append adds an output (or an object) to the console, as VS Code's appendToRepl: the text of an earlier line that was not ended
// goes on, identical lines are counted when debug.console.collapseIdenticalLines is on, and the clear sequence of a terminal clears the
// console.
func (m *ReplModel) Append(s *Session, d ReplData) {
	output := d.Output
	if i := strings.LastIndex(output, clearANSISequence); i >= 0 {
		m.Clear()
		m.Append(s, ReplData{Output: "Console was cleared", Severity: ReplIgnore})
		output = output[i+len(clearANSISequence):]
	}
	if d.Expression != nil {
		// with an output string, prefer it: the adapter may have formatted it, with ANSI colors for instance
		if output != "" {
			m.addElement(m.newOutput(s, output, d.Severity, d.Source, d.Expression))
		} else {
			m.addElement(&ReplVariable{id: nextReplID(), Expression: d.Expression, Severity: d.Severity, Src: d.Source, Session: s})
		}
		return
	}
	m.appendOutput(s, output, d.Severity, d.Source)
}

func (m *ReplModel) lastLocked() ReplElement {
	if n := len(m.elements); n > 0 {
		return m.elements[n-1]
	}
	return nil
}

func (m *ReplModel) appendOutput(s *Session, output string, sev ReplSeverity, src *ReplSource) {
	cfg := m.cfg()
	m.mu.Lock()
	prev, _ := m.lastLocked().(*ReplOutput)
	// the text goes on an earlier line that has no end yet
	if prev != nil && prev.Severity == sev && prev.Src.equal(src) &&
		!strings.HasSuffix(prev.Value, "\n") && !strings.HasSuffix(prev.Value, "\r\n") && prev.Count() == 1 {
		combined := prev.Value + output
		m.elements[len(m.elements)-1] = m.newOutput(s, combined, sev, src, nil)
		m.version++
		m.mu.Unlock()
		m.fire()
		if cfg.CollapseIdenticalLines && strings.HasSuffix(combined, "\n") {
			m.tryCollapseCompleteLine(sev, src)
		}
		if cfg.CollapseIdenticalLines && strings.Contains(combined, "\n") && len(splitIntoLines(combined)) > 1 {
			m.applyLineLevelCollapsing(s, sev, src)
		}
		return
	}
	m.mu.Unlock()
	if cfg.CollapseIdenticalLines && strings.Contains(output, "\n") {
		m.processMultiLineOutput(s, output, sev, src)
		return
	}
	if prev != nil && prev.Severity == sev && prev.Src.equal(src) && prev.Value == output && cfg.CollapseIdenticalLines {
		// only the count changes
		prev.count.Add(1)
		m.mu.Lock()
		m.version++
		m.mu.Unlock()
		m.fire()
		return
	}
	m.addElement(m.newOutput(s, output, sev, src, nil))
}

// tryCollapseCompleteLine collapses the last element into the one before it when both are the same whole line.
func (m *ReplModel) tryCollapseCompleteLine(sev ReplSeverity, src *ReplSource) {
	m.mu.Lock()
	n := len(m.elements)
	if n < 2 {
		m.mu.Unlock()
		return
	}
	last, ok1 := m.elements[n-1].(*ReplOutput)
	second, ok2 := m.elements[n-2].(*ReplOutput)
	if !ok1 || !ok2 || last.Severity != sev || second.Severity != sev || !last.Src.equal(src) || !second.Src.equal(src) ||
		last.Value != second.Value || last.Count() != 1 || !strings.HasSuffix(last.Value, "\n") {
		m.mu.Unlock()
		return
	}
	second.count.Add(int32(last.Count()))
	m.elements = m.elements[:n-1]
	m.version++
	m.mu.Unlock()
	m.fire()
}

// processMultiLineOutput adds the lines of an output one by one, counting a line that is the same as the one before.
func (m *ReplModel) processMultiLineOutput(s *Session, output string, sev ReplSeverity, src *ReplSource) {
	for _, line := range splitIntoLines(output) {
		if line == "" {
			continue
		}
		m.addLine(s, line, sev, src)
	}
}

// addLine adds a line, or counts it on the previous element when it is the same.
func (m *ReplModel) addLine(s *Session, line string, sev ReplSeverity, src *ReplSource) {
	m.mu.Lock()
	if prev, ok := m.lastLocked().(*ReplOutput); ok && prev.Severity == sev && prev.Src.equal(src) && prev.Value == line {
		prev.count.Add(1)
		m.version++
		m.mu.Unlock()
		m.fire()
		return
	}
	m.mu.Unlock()
	m.addElement(m.newOutput(s, line, sev, src, nil))
}

// splitIntoLines splits a text keeping the line feeds.
func splitIntoLines(text string) []string {
	var lines []string
	for start := 0; start < len(text); {
		i := strings.IndexByte(text[start:], '\n')
		if i < 0 {
			lines = append(lines, text[start:])
			break
		}
		lines = append(lines, text[start:start+i+1])
		start += i + 1
	}
	return lines
}

// applyLineLevelCollapsing splits the last element, when it holds several lines, into lines that are counted.
func (m *ReplModel) applyLineLevelCollapsing(s *Session, sev ReplSeverity, src *ReplSource) {
	m.mu.Lock()
	last, ok := m.lastLocked().(*ReplOutput)
	if !ok || last.Severity != sev || !last.Src.equal(src) {
		m.mu.Unlock()
		return
	}
	lines := splitIntoLines(last.Value)
	if len(lines) <= 1 {
		m.mu.Unlock()
		return
	}
	m.elements = m.elements[:len(m.elements)-1]
	m.mu.Unlock()
	for _, line := range lines {
		if line == "" {
			continue
		}
		m.addLine(s, line, sev, src)
	}
	m.mu.Lock()
	m.version++
	m.mu.Unlock()
	m.fire()
}

// StartGroup opens a group: what comes next is in it until EndGroup.
func (m *ReplModel) StartGroup(s *Session, name string, autoExpand bool, src *ReplSource) {
	g := &ReplGroup{id: "replGroup:" + strconv.FormatInt(replCounter.Add(1), 10), Session: s, Name: name, AutoExpand: autoExpand, Src: src}
	m.addElement(g)
}

// EndGroup closes the group that was opened last and is still open.
func (m *ReplModel) EndGroup() {
	m.mu.Lock()
	last, _ := m.lastLocked().(*ReplGroup)
	m.mu.Unlock()
	if last != nil {
		last.end()
	}
}

// addElement adds an element to the group that is open at the end, or at the end of the list; the oldest elements go when there are
// more than debug.console.maximumLines.
func (m *ReplModel) addElement(e ReplElement) {
	limit := m.cfg().MaximumLines
	m.mu.Lock()
	if last, ok := m.lastLocked().(*ReplGroup); ok && !last.Ended() {
		last.addChild(e)
	} else {
		m.elements = append(m.elements, e)
		if over := len(m.elements) - limit; limit > 0 && over > 0 {
			n := copy(m.elements, m.elements[over:])
			clear(m.elements[n:])
			m.elements = m.elements[:n]
		}
	}
	m.version++
	m.mu.Unlock()
	m.fire()
}
