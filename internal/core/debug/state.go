package debug

import (
	"encoding/json"
	"sync"

	"github.com/rm4n0s/errors"
)

// StateVersion is the version of the saved debug state.
const StateVersion = 1

// MaxStateBreakpoints bounds the breakpoints a saved state may hold, so that a damaged file cannot make the model huge.
const MaxStateBreakpoints = 10000

// InputMemoryCapacity is how many inputs' last values are kept (VS Code's LAST_INPUT_CACHE_SIZE).
const InputMemoryCapacity = 5

// stateFile is the JSON the window saves with the workspace's UI state (SPEC §10.10): the breakpoints with their conditions, hit
// counts, log messages and enablement, the function breakpoints, the choices among the exception filters, the watch expressions, the
// selected configuration, the switch that activates the breakpoints, the history of the Debug Console and the last values of inputs.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/common/debugStorage.ts
type stateFile struct {
	Version     int                `json:"version"`
	Breakpoints []stateBreakpoint  `json:"breakpoints,omitempty"`
	Functions   []stateFunction    `json:"functionBreakpoints,omitempty"`
	Exceptions  []stateException   `json:"exceptionBreakpoints,omitempty"`
	Watch       []stateWatch       `json:"watchExpressions,omitempty"`
	Activated   *bool              `json:"breakpointsActivated,omitempty"`
	Selected    *stateSelection    `json:"selected,omitempty"`
	Recent      []DynamicSelection `json:"recentDynamicConfigurations,omitempty"`
	ReplHistory []string           `json:"replHistory,omitempty"`
	Inputs      []stateInput       `json:"inputs,omitempty"`
}

type stateBreakpoint struct {
	ID           string          `json:"id"`
	Path         string          `json:"path"`
	Line         int             `json:"line"`
	Column       int             `json:"column,omitempty"`
	Disabled     bool            `json:"disabled,omitempty"`
	Condition    string          `json:"condition,omitempty"`
	HitCondition string          `json:"hitCondition,omitempty"`
	LogMessage   string          `json:"logMessage,omitempty"`
	Mode         string          `json:"mode,omitempty"`
	ModeLabel    string          `json:"modeLabel,omitempty"`
	TriggeredBy  string          `json:"triggeredBy,omitempty"`
	AdapterData  json.RawMessage `json:"adapterData,omitempty"`
}

type stateFunction struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Disabled     bool   `json:"disabled,omitempty"`
	Condition    string `json:"condition,omitempty"`
	HitCondition string `json:"hitCondition,omitempty"`
}

type stateException struct {
	ID                   string `json:"id"`
	Filter               string `json:"filter"`
	Label                string `json:"label"`
	Description          string `json:"description,omitempty"`
	ConditionDescription string `json:"conditionDescription,omitempty"`
	SupportsCondition    bool   `json:"supportsCondition,omitempty"`
	Enabled              bool   `json:"enabled,omitempty"`
	Condition            string `json:"condition,omitempty"`
	Fallback             bool   `json:"fallback,omitempty"`
}

type stateWatch struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type stateSelection struct {
	Launch  string `json:"launch,omitempty"`
	Name    string `json:"name,omitempty"`
	Type    string `json:"type,omitempty"`
	Dynamic bool   `json:"dynamic,omitempty"`
}

type stateInput struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// SaveState encodes what is saved with the workspace's UI state.
func (s *Service) SaveState() ([]byte, error) {
	m := s.model
	m.mu.Lock()
	st := stateFile{Version: StateVersion}
	for _, b := range m.lines {
		st.Breakpoints = append(st.Breakpoints, stateBreakpoint{
			ID: b.id, Path: b.path, Line: b.line, Column: b.column, Disabled: !b.enabled, Condition: b.condition,
			HitCondition: b.hitCondition, LogMessage: b.logMessage, Mode: b.mode, ModeLabel: b.modeLabel, TriggeredBy: b.triggeredBy,
			AdapterData: b.adapterData,
		})
	}
	for _, b := range m.funcs {
		st.Functions = append(st.Functions, stateFunction{ID: b.id, Name: b.name, Disabled: !b.enabled, Condition: b.condition, HitCondition: b.hitCondition})
	}
	for _, e := range m.exceptions {
		st.Exceptions = append(st.Exceptions, stateException{
			ID: e.id, Filter: e.filter, Label: e.label, Description: e.description, ConditionDescription: e.conditionDescription,
			SupportsCondition: e.supportsCondition, Enabled: e.enabled, Condition: e.condition, Fallback: e.fallback || len(e.supported) > 0,
		})
	}
	for _, w := range m.watch {
		if w.Name != "" {
			st.Watch = append(st.Watch, stateWatch{ID: w.id, Name: w.Name})
		}
	}
	if !m.activated {
		off := false
		st.Activated = &off
	}
	if n := len(m.repl); n > 0 {
		st.ReplHistory = append([]string(nil), m.repl...)
	}
	m.mu.Unlock()

	sel := s.launches.Selected()
	if sel.Launch != nil || sel.Name != "" {
		ss := &stateSelection{Name: sel.Name}
		if sel.Launch != nil {
			ss.Launch = sel.Launch.Key()
		}
		if sel.Dynamic {
			ss.Dynamic, ss.Type = true, sel.Type
		}
		st.Selected = ss
	}
	st.Recent = s.launches.RecentDynamic()
	st.Inputs = append(st.Inputs, s.memory().entries()...)
	data, err := json.Marshal(st)
	if err != nil {
		return nil, errors.NewErr("DebugStateEncodeFailed", err)
	}
	return data, nil
}

// RestoreState replaces the breakpoints, watch expressions and selection with those of a saved state. Entries that cannot be used (a
// breakpoint with no path or a line below 1) are dropped; a state that is not the JSON of this version fails with the tags
// DebugStateInvalid and DebugStateUnsupported and leaves the model as it was.
func (s *Service) RestoreState(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	var st stateFile
	if err := json.Unmarshal(data, &st); err != nil {
		return errors.NewErr("DebugStateInvalid", err)
	}
	if st.Version != StateVersion {
		return errors.New("DebugStateUnsupported", "the saved debug state has another version", "version", st.Version, "want", StateVersion)
	}
	if len(st.Breakpoints) > MaxStateBreakpoints {
		st.Breakpoints = st.Breakpoints[:MaxStateBreakpoints]
	}
	m := s.model
	m.mu.Lock()
	m.lines, m.funcs, m.exceptions, m.watch = nil, nil, nil, nil
	seen := map[string]bool{}
	for _, b := range st.Breakpoints {
		if b.Path == "" || b.Line < 1 {
			continue
		}
		id := b.ID
		if id == "" || seen[id] {
			id = newID()
		}
		seen[id] = true
		m.lines = append(m.lines, &lineBP{
			bpBase: bpBase{id: id, enabled: !b.Disabled, condition: b.Condition, hitCondition: b.HitCondition, logMessage: b.LogMessage, mode: b.Mode, modeLabel: b.ModeLabel},
			path:   b.Path, line: b.Line, column: max(b.Column, 0), triggeredBy: b.TriggeredBy, adapterData: b.AdapterData,
		})
	}
	m.sortAndDedupLocked()
	for _, f := range st.Functions {
		id := f.ID
		if id == "" || seen[id] {
			id = newID()
		}
		seen[id] = true
		m.funcs = append(m.funcs, &funcBP{bpBase: bpBase{id: id, enabled: !f.Disabled, condition: f.Condition, hitCondition: f.HitCondition}, name: f.Name})
	}
	for _, e := range st.Exceptions {
		if e.Filter == "" {
			continue
		}
		id := e.ID
		if id == "" || seen[id] {
			id = newID()
		}
		seen[id] = true
		m.exceptions = append(m.exceptions, &exceptionBP{
			bpBase: bpBase{id: id, enabled: e.Enabled, condition: e.Condition}, filter: e.Filter, label: e.Label, description: e.Description,
			conditionDescription: e.ConditionDescription, supportsCondition: e.SupportsCondition, fallback: e.Fallback,
		})
	}
	for _, w := range st.Watch {
		if w.Name == "" {
			continue
		}
		id := w.ID
		if id == "" || seen[id] {
			id = newID()
		}
		seen[id] = true
		m.watch = append(m.watch, newExpression(w.Name, id))
	}
	m.activated = st.Activated == nil || *st.Activated
	m.repl = append([]string(nil), st.ReplHistory...)
	if len(m.repl) > MaxReplHistory {
		m.repl = m.repl[len(m.repl)-MaxReplHistory:]
	}
	m.mu.Unlock()

	mem := s.memory()
	mem.reset()
	for _, in := range st.Inputs {
		mem.Set(in.Key, in.Value)
	}
	s.launches.restore(st.Selected, st.Recent)
	m.emit(Event{Kind: EventBreakpoints})
	m.emit(Event{Kind: EventWatch})
	m.emit(Event{Kind: EventConfiguration})
	return nil
}

// LRUInputMemory is the InputMemory the service uses when the window gives none: the last values of the five inputs used last.
type LRUInputMemory struct {
	mu   sync.Mutex
	list []stateInput // the oldest first
}

// Get returns the value remembered under key.
func (l *LRUInputMemory) Get(key string) (string, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, e := range l.list {
		if e.Key == key {
			return e.Value, true
		}
	}
	return "", false
}

// Set remembers a value; the input used least recently is forgotten beyond InputMemoryCapacity.
func (l *LRUInputMemory) Set(key, value string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	kept := l.list[:0:0]
	for _, e := range l.list {
		if e.Key != key {
			kept = append(kept, e)
		}
	}
	kept = append(kept, stateInput{Key: key, Value: value})
	if len(kept) > InputMemoryCapacity {
		kept = kept[len(kept)-InputMemoryCapacity:]
	}
	l.list = kept
}

func (l *LRUInputMemory) entries() []stateInput {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]stateInput(nil), l.list...)
}

func (l *LRUInputMemory) reset() {
	l.mu.Lock()
	l.list = nil
	l.mu.Unlock()
}

// memory is the memory of the inputs: the window's, or the service's own.
func (s *Service) memory() *LRUInputMemory { return s.inputs }

// restore keeps a saved selection until the launches are read, or applies it when they are.
func (l *Launches) restore(sel *stateSelection, recent []DynamicSelection) {
	l.mu.Lock()
	l.recent = append([]DynamicSelection(nil), recent...)
	l.want = sel
	loaded := len(l.launches) > 0
	l.mu.Unlock()
	if loaded {
		l.applyWanted()
	}
}

// applyWanted selects the saved selection among the launches that are there.
func (l *Launches) applyWanted() {
	l.mu.Lock()
	want := l.want
	l.want = nil
	l.mu.Unlock()
	if want == nil {
		return
	}
	var launch *Launch
	if want.Launch != "" {
		launch = l.Launch(want.Launch)
	}
	if want.Dynamic {
		l.Select(launch, want.Name, SelectOptions{Dynamic: &DynamicSelection{Type: want.Type, Name: want.Name}})
		return
	}
	l.Select(launch, want.Name, SelectOptions{})
}
