package debug

import (
	"sort"
	"strings"
	"sync"

	"github.com/rm4n0s/emetos/internal/core/dap"
)

// unverifiedDirty is the message of a breakpoint in a file with unsaved changes.
const unverifiedDirty = "Unverified breakpoint. File is modified, please restart debug session."

// Focus is the session, thread and stack frame the views show (VS Code's view model).
type Focus struct {
	Session *Session
	Thread  *Thread
	Frame   *StackFrame
}

type breakpointMode struct {
	mode, label, firstFromDebugType, description string
	appliesTo                                    []string
}

// Model is the data of Run and Debug: the sessions, the breakpoints of every kind, the watch expressions and the focus. It is
// safe for use from several goroutines. The mutators announce their change to the subscribers after releasing the lock.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/common/debugModel.ts (DebugModel) and common/debugViewModel.ts
type Model struct {
	mu   sync.Mutex
	subs subscribers

	valuesMu sync.Mutex
	values   map[string]string

	sessions   []*Session
	lines      []*lineBP
	funcs      []*funcBP
	exceptions []*exceptionBP
	activated  bool
	dirty      map[string]bool
	watch      []*Expression
	modes      []*breakpointMode
	focus      Focus
	repl       []string
}

// NewModel makes an empty model: no sessions, no breakpoints, breakpoints activated.
func NewModel() *Model {
	return &Model{values: map[string]string{}, activated: true, dirty: map[string]bool{}}
}

func (m *Model) emit(ev Event) { m.subs.emit(ev) }

// Subscribe registers a listener of the changes of the model. It is called from the goroutine that made the change, after the lock
// was released, so it may read the model; it must not block and must not make requests (the window hands the work to its UI thread).
func (m *Model) Subscribe(fn func(Event)) (cancel func()) { return m.subs.add(fn) }

// swapValue records the value of the node with an id and returns the one before.
func (m *Model) swapValue(id, v string) string {
	m.valuesMu.Lock()
	defer m.valuesMu.Unlock()
	prev := m.values[id]
	m.values[id] = v
	return prev
}

func (m *Model) forgetValues() {
	m.valuesMu.Lock()
	clear(m.values)
	m.valuesMu.Unlock()
}

// ---- sessions

// Session returns the session with an id, or nil.
func (m *Model) Session(id string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sessions {
		if s.id == id {
			return s
		}
	}
	return nil
}

// Sessions returns the sessions that are not inactive (all, with includeInactive), the children after their parent.
func (m *Model) Sessions(includeInactive bool) []*Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessionsLocked(includeInactive)
}

func (m *Model) sessionsLocked(includeInactive bool) []*Session {
	out := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		if includeInactive || s.stateLocked() != Inactive {
			out = append(out, s)
		}
	}
	return out
}

// shouldDispose tells whether an inactive session is removed when a new one comes in.
func (m *Model) shouldDisposeLocked(s, n *Session) bool {
	if s.stateLocked() != Inactive {
		return false
	}
	if s.cfg.Name() == n.cfg.Name() {
		return true
	}
	if n.parent != nil {
		return false
	}
	root := s
	for root.parent != nil {
		root = root.parent
	}
	return root.stateLocked() == Inactive && root.cfg.Name() == n.cfg.Name()
}

// addSession puts a session in the list: inactive sessions of the same configuration go, the name is made unique, and a child goes
// after its parent.
func (m *Model) addSession(s *Session) {
	m.mu.Lock()
	kept := m.sessions[:0:0]
	for _, o := range m.sessions {
		if o.id == s.id || m.shouldDisposeLocked(o, s) {
			continue
		}
		kept = append(kept, o)
	}
	m.sessions = kept
	i := 1
	for m.labelTakenLocked(s) {
		i++
		s.name = s.cfg.Name() + " " + itoa(i)
	}
	index := -1
	if s.parent != nil {
		for j := len(m.sessions) - 1; j >= 0; j-- {
			if m.sessions[j].parent == s.parent || m.sessions[j] == s.parent {
				index = j
				break
			}
		}
	}
	if index >= 0 {
		m.sessions = append(m.sessions[:index+1], append([]*Session{s}, m.sessions[index+1:]...)...)
	} else {
		m.sessions = append(m.sessions, s)
	}
	m.mu.Unlock()
	m.emit(Event{Kind: EventSessions, Session: s})
	m.emit(Event{Kind: EventCallStack, Session: s})
}

func (m *Model) labelTakenLocked(s *Session) bool {
	for _, o := range m.sessions {
		if o != s && o.labelLocked() == s.labelLocked() {
			return true
		}
	}
	return false
}

// ---- focus

// Focus returns the focused session, thread and stack frame.
func (m *Model) Focus() Focus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.focus
}

// setFocus changes the focus and announces it.
func (m *Model) setFocus(f Focus) {
	m.mu.Lock()
	changed := m.focus != f
	m.focus = f
	m.mu.Unlock()
	if changed {
		m.emit(Event{Kind: EventFocus, Session: f.Session, Thread: f.Thread})
	}
}

// ---- breakpoints

func (m *Model) lineVerifiedLocked(b *lineBP) bool {
	if b.data != nil {
		return b.data.Verified && !m.dirty[b.path]
	}
	return true
}

func (m *Model) lineLocked(b *lineBP) int {
	if m.lineVerifiedLocked(b) && b.data != nil && b.data.Line != 0 {
		return b.data.Line
	}
	return b.line
}

func (m *Model) columnLocked(b *lineBP) int {
	if m.lineVerifiedLocked(b) && b.data != nil && b.data.Column != 0 {
		return b.data.Column
	}
	return b.column
}

func (m *Model) sourcePathLocked(b *lineBP) string {
	if m.lineVerifiedLocked(b) && b.data != nil && b.data.Source != nil && b.data.Source.Path != "" {
		return b.data.Source.Path
	}
	return b.path
}

func (m *Model) snapshotLineLocked(b *lineBP) Breakpoint {
	bp := Breakpoint{
		ID: b.id, Path: b.path, SourcePath: m.sourcePathLocked(b), Line: m.lineLocked(b), Column: m.columnLocked(b),
		RequestedLine: b.line, RequestedColumn: b.column, Enabled: b.enabled, Condition: b.condition,
		HitCondition: b.hitCondition, LogMessage: b.logMessage, Mode: b.mode, ModeLabel: b.modeLabel,
		TriggeredBy: b.triggeredBy, Verified: m.lineVerifiedLocked(b), Pending: b.data == nil && b.triggeredBy != "",
		Supported: true, AdapterData: b.adapterData,
	}
	if b.data != nil {
		if m.lineVerifiedLocked(b) {
			bp.EndLine, bp.EndColumn = b.data.EndLine, b.data.EndColumn
		}
		if b.data.Source != nil && len(b.data.Source.AdapterData) > 0 {
			bp.AdapterData = b.data.Source.AdapterData
		}
		switch {
		case b.logMessage != "" && !b.data.SupportsLogPoints,
			b.condition != "" && !b.data.SupportsConditionalBreakpoints,
			b.hitCondition != "" && !b.data.SupportsHitConditionalBreakpoints:
			bp.Supported = false
		}
		bp.Message = b.data.Message
	}
	if m.dirty[bp.SourcePath] {
		bp.Message = unverifiedDirty
	}
	return bp
}

func (m *Model) snapshotFuncLocked(b *funcBP) FunctionBreakpoint {
	fb := FunctionBreakpoint{
		ID: b.id, Name: b.name, Enabled: b.enabled, Condition: b.condition, HitCondition: b.hitCondition,
		Verified: b.verified(), Message: b.message(), Supported: true,
	}
	if b.data != nil {
		fb.Supported = b.data.SupportsFunctionBreakpoints
	}
	return fb
}

func snapshotException(b *exceptionBP) ExceptionBreakpoint {
	return ExceptionBreakpoint{
		ID: b.id, Filter: b.filter, Label: b.label, Description: b.description, ConditionDescription: b.conditionDescription,
		SupportsCondition: b.supportsCondition, Enabled: b.enabled, Condition: b.condition, Fallback: b.fallback,
	}
}

// Breakpoints returns the breakpoints of lines and columns that match the filter, in the order of the files, lines and columns.
func (m *Model) Breakpoints(f BreakpointFilter) []Breakpoint {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Breakpoint{}
	for _, b := range m.lines {
		if f.Path != "" && m.sourcePathLocked(b) != f.Path {
			continue
		}
		if f.OriginalPath != "" && b.path != f.OriginalPath {
			continue
		}
		if f.Line != 0 && m.lineLocked(b) != f.Line {
			continue
		}
		if f.Column != 0 && m.columnLocked(b) != f.Column {
			continue
		}
		if f.EnabledOnly && (!m.activated || !b.enabled) {
			continue
		}
		if f.TriggeredOnly && b.triggeredBy == "" {
			continue
		}
		out = append(out, m.snapshotLineLocked(b))
	}
	return out
}

// Breakpoint returns the breakpoint of a line or column with an id.
func (m *Model) Breakpoint(id string) (Breakpoint, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, b := range m.lines {
		if b.id == id {
			return m.snapshotLineLocked(b), true
		}
	}
	return Breakpoint{}, false
}

// BreakpointsActivated tells whether breakpoints are activated ("Toggle Activate Breakpoints").
func (m *Model) BreakpointsActivated() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.activated
}

// setActivated is Service.SetBreakpointsActivated's part in the model.
func (m *Model) setActivated(on bool) {
	m.mu.Lock()
	m.activated = on
	m.mu.Unlock()
	m.emit(Event{Kind: EventBreakpoints})
}

// SetDocumentDirty tells the model that the file at path has unsaved changes (or has not): its breakpoints are unverified while it
// has them (VS Code's textFileService.isDirty).
func (m *Model) SetDocumentDirty(path string, dirty bool) {
	m.mu.Lock()
	was := m.dirty[path]
	if dirty {
		m.dirty[path] = true
	} else {
		delete(m.dirty, path)
	}
	has := false
	for _, b := range m.lines {
		if m.sourcePathLocked(b) == path || b.path == path {
			has = true
			break
		}
	}
	m.mu.Unlock()
	if was != dirty && has {
		m.emit(Event{Kind: EventBreakpoints, Breakpoints: BreakpointsChange{SessionOnly: true}})
	}
}

// addBreakpoints adds breakpoints of a file; breakpoints at the same line and column as one that is there are dropped.
func (m *Model) addBreakpoints(path string, data []BreakpointData) []Breakpoint {
	m.mu.Lock()
	var added []*lineBP
	for _, d := range data {
		id := d.ID
		if id == "" {
			id = newID()
		}
		b := &lineBP{
			bpBase: bpBase{id: id, enabled: !d.Disabled, condition: d.Condition, hitCondition: d.HitCondition, logMessage: d.LogMessage, mode: d.Mode, modeLabel: d.ModeLabel},
			path:   path, line: d.Line, column: d.Column, triggeredBy: d.TriggeredBy,
		}
		added = append(added, b)
	}
	m.lines = append(m.lines, added...)
	m.activated = true
	m.sortAndDedupLocked()
	var out []Breakpoint
	var ids []string
	for _, b := range added {
		if !m.hasLineLocked(b) {
			continue // dropped as a duplicate
		}
		out = append(out, m.snapshotLineLocked(b))
		ids = append(ids, b.id)
	}
	m.mu.Unlock()
	m.emit(Event{Kind: EventBreakpoints, Breakpoints: BreakpointsChange{Added: ids}})
	return out
}

func (m *Model) hasLineLocked(b *lineBP) bool {
	for _, o := range m.lines {
		if o == b {
			return true
		}
	}
	return false
}

// removeBreakpoints removes breakpoints of lines and columns by id and returns the removed.
func (m *Model) removeBreakpoints(ids []string) []Breakpoint {
	m.mu.Lock()
	drop := map[string]bool{}
	for _, id := range ids {
		drop[id] = true
	}
	var removed []Breakpoint
	var removedIDs []string
	kept := m.lines[:0:0]
	for _, b := range m.lines {
		if drop[b.id] {
			removed = append(removed, m.snapshotLineLocked(b))
			removedIDs = append(removedIDs, b.id)
			continue
		}
		kept = append(kept, b)
	}
	m.lines = kept
	m.mu.Unlock()
	if len(removed) > 0 {
		m.emit(Event{Kind: EventBreakpoints, Breakpoints: BreakpointsChange{Removed: removedIDs}})
	}
	return removed
}

// updateBreakpoints changes breakpoints of lines and columns; it returns the ids that exist.
func (m *Model) updateBreakpoints(updates map[string]BreakpointUpdate) []string {
	m.mu.Lock()
	var changed []string
	for _, b := range m.lines {
		u, ok := updates[b.id]
		if !ok {
			continue
		}
		if u.Line != 0 {
			b.line = u.Line
		}
		if u.Column != nil {
			b.column = *u.Column
		}
		if u.Condition != nil {
			b.condition = *u.Condition
		}
		if u.HitCondition != nil {
			b.hitCondition = *u.HitCondition
		}
		if u.LogMessage != nil {
			b.logMessage = *u.LogMessage
		}
		if u.Mode != nil {
			b.mode = *u.Mode
			if u.ModeLabel != nil {
				b.modeLabel = *u.ModeLabel
			} else {
				b.modeLabel = ""
			}
		}
		if u.TriggeredBy != nil {
			b.triggeredBy = *u.TriggeredBy
			b.triggered = nil
		}
		changed = append(changed, b.id)
	}
	// Breakpoints that moved onto the same place are one.
	before := make([]string, 0, len(m.lines))
	for _, b := range m.lines {
		before = append(before, b.id)
	}
	m.sortAndDedupLocked()
	var removed []string
	if len(m.lines) != len(before) {
		present := map[string]bool{}
		for _, b := range m.lines {
			present[b.id] = true
		}
		for _, id := range before {
			if !present[id] {
				removed = append(removed, id)
			}
		}
		kept := changed[:0:0]
		for _, id := range changed {
			if present[id] {
				kept = append(kept, id)
			}
		}
		changed = kept
	}
	m.mu.Unlock()
	m.emit(Event{Kind: EventBreakpoints, Breakpoints: BreakpointsChange{Changed: changed, Removed: removed}})
	return changed
}

// sortAndDedupLocked orders the breakpoints by the name of their file, the file, the line and the column, and drops the second of
// two at one place.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/common/debugModel.ts (sortAndDeDup)
func (m *Model) sortAndDedupLocked() {
	sort.SliceStable(m.lines, func(i, j int) bool {
		a, b := m.lines[i], m.lines[j]
		pa, pb := m.sourcePathLocked(a), m.sourcePathLocked(b)
		if pa != pb {
			if ba, bb := baseName(pa), baseName(pb); ba != bb {
				return strings.ToLower(ba) < strings.ToLower(bb) || (strings.EqualFold(ba, bb) && ba < bb)
			}
			return pa < pb
		}
		la, lb := m.lineLocked(a), m.lineLocked(b)
		if la != lb {
			return la < lb
		}
		return m.columnLocked(a) < m.columnLocked(b)
	})
	seen := map[string]bool{}
	kept := m.lines[:0:0]
	for _, b := range m.lines {
		key := m.sourcePathLocked(b) + ":" + itoa(m.lineLocked(b)) + ":" + itoa(m.columnLocked(b))
		if seen[key] {
			continue
		}
		seen[key] = true
		kept = append(kept, b)
	}
	m.lines = kept
}

// setEnablement enables or disables one breakpoint of any kind by id and returns whether there is one.
func (m *Model) setEnablement(id string, enable bool) bool {
	m.mu.Lock()
	found := false
	var changed []string
	for _, b := range m.lines {
		if b.id == id {
			found = true
			if b.enabled != enable {
				changed = append(changed, id)
			}
			b.enabled = enable
		}
	}
	for _, b := range m.funcs {
		if b.id == id {
			found = true
			if b.enabled != enable {
				changed = append(changed, id)
			}
			b.enabled = enable
		}
	}
	for _, b := range m.exceptions {
		if b.id == id {
			found = true
			b.enabled = enable
		}
	}
	if found && enable {
		m.activated = true
	}
	m.mu.Unlock()
	if found {
		m.emit(Event{Kind: EventBreakpoints, Breakpoints: BreakpointsChange{Changed: changed}})
	}
	return found
}

// enableAll enables or disables all the breakpoints of lines and functions (the exception filters keep their choice).
func (m *Model) enableAll(enable bool) {
	m.mu.Lock()
	var changed []string
	for _, b := range m.lines {
		if b.enabled != enable {
			changed = append(changed, b.id)
		}
		b.enabled = enable
	}
	for _, b := range m.funcs {
		if b.enabled != enable {
			changed = append(changed, b.id)
		}
		b.enabled = enable
	}
	if enable {
		m.activated = true
	}
	m.mu.Unlock()
	m.emit(Event{Kind: EventBreakpoints, Breakpoints: BreakpointsChange{Changed: changed}})
}

// setTriggered notes that the breakpoint that triggers the breakpoint id was hit in the session.
func (m *Model) setTriggered(id, sessionID string, did bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, b := range m.lines {
		if b.id != id {
			continue
		}
		if did {
			if b.triggered == nil {
				b.triggered = map[string]bool{}
			}
			b.triggered[sessionID] = true
		} else {
			delete(b.triggered, sessionID)
		}
	}
}

// clearTriggered forgets, for a session, which triggered breakpoints it has enabled.
func (m *Model) clearTriggered(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, b := range m.lines {
		if b.triggeredBy != "" {
			delete(b.triggered, sessionID)
		}
	}
}

// setBreakpointSessionData stores what a session answered for breakpoints, by id of the model's breakpoint; nil data removes the
// session's data from all of them.
func (m *Model) setBreakpointSessionData(sessionID string, caps dap.Capabilities, data map[string]dap.Breakpoint) {
	m.mu.Lock()
	apply := func(id string, set func(*SessionData)) {
		if data == nil {
			set(nil)
			return
		}
		if d, ok := data[id]; ok {
			set(sessionDataOf(d, caps))
		}
	}
	for _, b := range m.lines {
		apply(b.id, func(d *SessionData) { b.setSessionData(sessionID, d) })
		if d, ok := b.sessionData[sessionID]; ok && b.adapterData == nil && d.Source != nil && len(d.Source.AdapterData) > 0 {
			b.adapterData = d.Source.AdapterData
		}
	}
	for _, b := range m.funcs {
		apply(b.id, func(d *SessionData) { b.setSessionData(sessionID, d) })
	}
	for _, b := range m.exceptions {
		apply(b.id, func(d *SessionData) { b.setSessionData(sessionID, d) })
	}
	m.sortAndDedupLocked()
	m.mu.Unlock()
	m.emit(Event{Kind: EventBreakpoints, Breakpoints: BreakpointsChange{SessionOnly: true}})
}

// ---- function breakpoints

// FunctionBreakpoints returns the function breakpoints in the order they were added.
func (m *Model) FunctionBreakpoints() []FunctionBreakpoint {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]FunctionBreakpoint, 0, len(m.funcs))
	for _, b := range m.funcs {
		out = append(out, m.snapshotFuncLocked(b))
	}
	return out
}

func (m *Model) addFunctionBreakpoint(opts FunctionBreakpointOptions, id string) FunctionBreakpoint {
	if id == "" {
		id = newID()
	}
	m.mu.Lock()
	b := &funcBP{bpBase: bpBase{id: id, enabled: !opts.Disabled, condition: opts.Condition, hitCondition: opts.HitCondition}, name: opts.Name}
	m.funcs = append(m.funcs, b)
	snap := m.snapshotFuncLocked(b)
	m.mu.Unlock()
	m.emit(Event{Kind: EventBreakpoints, Breakpoints: BreakpointsChange{Added: []string{id}}})
	return snap
}

func (m *Model) updateFunctionBreakpoint(id string, name, condition, hitCondition *string) bool {
	m.mu.Lock()
	found := false
	for _, b := range m.funcs {
		if b.id != id {
			continue
		}
		found = true
		if name != nil {
			b.name = *name
		}
		if condition != nil {
			b.condition = *condition
		}
		if hitCondition != nil {
			b.hitCondition = *hitCondition
		}
	}
	m.mu.Unlock()
	if found {
		m.emit(Event{Kind: EventBreakpoints, Breakpoints: BreakpointsChange{Changed: []string{id}}})
	}
	return found
}

// removeFunctionBreakpoints removes the function breakpoint with an id, or all of them when id is empty.
func (m *Model) removeFunctionBreakpoints(id string) {
	m.mu.Lock()
	var removed []string
	kept := m.funcs[:0:0]
	for _, b := range m.funcs {
		if id == "" || b.id == id {
			removed = append(removed, b.id)
			continue
		}
		kept = append(kept, b)
	}
	m.funcs = kept
	m.mu.Unlock()
	m.emit(Event{Kind: EventBreakpoints, Breakpoints: BreakpointsChange{Removed: removed}})
}

// ---- exception breakpoints

// ExceptionBreakpoints returns the exception filters of a session, or, for an empty session id, those of the session that was
// focused last (shown when no session runs).
func (m *Model) ExceptionBreakpoints(sessionID string) []ExceptionBreakpoint {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.exceptionsLocked(sessionID)
}

func (m *Model) exceptionsLocked(sessionID string) []ExceptionBreakpoint {
	out := []ExceptionBreakpoint{}
	for _, b := range m.exceptions {
		if sessionID != "" && b.supported[sessionID] || sessionID == "" && b.fallback {
			out = append(out, snapshotException(b))
		}
	}
	return out
}

// setExceptionBreakpointsForSession adds the filters an adapter offers that the model does not know yet (enabled when the adapter
// says default) and marks all of them as supported by the session.
func (m *Model) setExceptionBreakpointsForSession(sessionID string, filters []dap.ExceptionBreakpointsFilter) {
	m.mu.Lock()
	changed := false
	for _, f := range filters {
		var found *exceptionBP
		for _, e := range m.exceptions {
			if e.matches(f) {
				found = e
			}
		}
		if found == nil {
			changed = true
			found = &exceptionBP{
				bpBase: bpBase{id: newID(), enabled: f.Default}, filter: f.Filter, label: f.Label, description: f.Description,
				conditionDescription: f.ConditionDescription, supportsCondition: f.SupportsCondition,
			}
			m.exceptions = append(m.exceptions, found)
		}
		if found.supported == nil {
			found.supported = map[string]bool{}
		}
		found.supported[sessionID] = true
	}
	m.mu.Unlock()
	if changed {
		m.emit(Event{Kind: EventBreakpoints})
	}
}

func (m *Model) removeExceptionBreakpointsForSession(sessionID string) {
	m.mu.Lock()
	for _, e := range m.exceptions {
		delete(e.supported, sessionID)
	}
	m.mu.Unlock()
}

// setExceptionFallbackSession remembers the filters of a session as the ones to show when no session runs.
func (m *Model) setExceptionFallbackSession(sessionID string) {
	m.mu.Lock()
	for _, e := range m.exceptions {
		e.fallback = e.supported[sessionID]
	}
	m.mu.Unlock()
}

func (m *Model) setExceptionCondition(id, condition string) bool {
	m.mu.Lock()
	found := false
	for _, e := range m.exceptions {
		if e.id == id {
			e.condition = condition
			found = true
		}
	}
	m.mu.Unlock()
	if found {
		m.emit(Event{Kind: EventBreakpoints, Breakpoints: BreakpointsChange{Changed: []string{id}}})
	}
	return found
}

// ---- breakpoint modes

// BreakpointModes returns the modes of breakpoints the adapters offered for a kind of breakpoint (source, exception, data,
// instruction).
func (m *Model) BreakpointModes(kind string) []dap.BreakpointMode {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []dap.BreakpointMode
	for _, md := range m.modes {
		for _, a := range md.appliesTo {
			if a == kind {
				out = append(out, dap.BreakpointMode{Mode: md.mode, Label: md.label, Description: md.description, AppliesTo: append([]string(nil), md.appliesTo...)})
				break
			}
		}
	}
	return out
}

func (m *Model) registerBreakpointModes(debugType string, modes []dap.BreakpointMode) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, mode := range modes {
		var rec *breakpointMode
		for _, r := range m.modes {
			if r.mode == mode.Mode && r.label == mode.Label {
				rec = r
			}
		}
		if rec != nil {
			for _, target := range mode.AppliesTo {
				present := false
				for _, a := range rec.appliesTo {
					present = present || a == target
				}
				if !present {
					rec.appliesTo = append(rec.appliesTo, target)
				}
			}
			continue
		}
		var dup *breakpointMode
		for _, r := range m.modes {
			if r.label == mode.Label {
				dup = r
			}
		}
		label := mode.Label
		if dup != nil {
			dup.label = dup.label + " (" + dup.firstFromDebugType + ")"
			label = mode.Label + " (" + debugType + ")"
		}
		m.modes = append(m.modes, &breakpointMode{
			mode: mode.Mode, label: label, firstFromDebugType: debugType, description: mode.Description,
			appliesTo: append([]string(nil), mode.AppliesTo...),
		})
	}
}

// ---- watch expressions

// WatchExpressions returns the watch expressions in order.
func (m *Model) WatchExpressions() []*Expression {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*Expression(nil), m.watch...)
}

// AddWatchExpression adds a watch expression (an empty name is one the user is still typing) and returns it.
func (m *Model) AddWatchExpression(name string) *Expression {
	e := newExpression(name, newID())
	m.mu.Lock()
	m.watch = append(m.watch, e)
	m.mu.Unlock()
	m.emit(Event{Kind: EventWatch, Expression: e})
	return e
}

// RenameWatchExpression changes the text of a watch expression; its value is forgotten until the next evaluation.
func (m *Model) RenameWatchExpression(id, name string) {
	m.mu.Lock()
	var found *Expression
	for _, w := range m.watch {
		if w.id == id {
			found = w
		}
	}
	m.mu.Unlock()
	if found == nil {
		return
	}
	found.Name = name
	m.emit(Event{Kind: EventWatch, Expression: found})
}

// RemoveWatchExpressions removes the watch expression with an id, or all of them when id is empty.
func (m *Model) RemoveWatchExpressions(id string) {
	m.mu.Lock()
	kept := m.watch[:0:0]
	for _, w := range m.watch {
		if id == "" || w.id == id {
			continue
		}
		kept = append(kept, w)
	}
	m.watch = kept
	m.mu.Unlock()
	m.emit(Event{Kind: EventWatch})
}

// MoveWatchExpression moves a watch expression to a position of the list.
func (m *Model) MoveWatchExpression(id string, position int) {
	m.mu.Lock()
	var moved *Expression
	rest := make([]*Expression, 0, len(m.watch))
	for _, w := range m.watch {
		if w.id == id {
			moved = w
			continue
		}
		rest = append(rest, w)
	}
	if moved == nil {
		m.mu.Unlock()
		return
	}
	position = max(0, min(position, len(rest)))
	m.watch = append(rest[:position:position], append([]*Expression{moved}, rest[position:]...)...)
	m.mu.Unlock()
	m.emit(Event{Kind: EventWatch})
}

// ---- Debug Console history

// ReplHistory returns the expressions the user typed in the Debug Console, the oldest first.
func (m *Model) ReplHistory() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.repl...)
}

// MaxReplHistory is how many expressions of the Debug Console's history are kept.
const MaxReplHistory = 100

// AddReplHistory adds an expression to the history of the Debug Console (a repeat of the last one is not added).
func (m *Model) AddReplHistory(expr string) {
	if strings.TrimSpace(expr) == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if n := len(m.repl); n > 0 && m.repl[n-1] == expr {
		return
	}
	m.repl = append(m.repl, expr)
	if len(m.repl) > MaxReplHistory {
		m.repl = m.repl[len(m.repl)-MaxReplHistory:]
	}
}
