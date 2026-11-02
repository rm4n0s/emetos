package testing

import (
	"encoding/json"
	"slices"
	"time"

	"github.com/rm4n0s/errors"
)

// StateVersion is the version of the saved state of the results.
const StateVersion = 1

// stateFile is the JSON the window saves with the workspace's UI state (SPEC §10.10): the results that ended and are kept (the last
// MaxResults), newest first, each with its tests, their states, durations and messages, and the output of its tasks trimmed to
// MaxSavedOutput bytes in all, and the choices of the default profiles. The coverage is not saved: a restored result has none.
//
// Ported from VS Code: src/vs/workbench/contrib/testing/common/testResultStorage.ts (what a stored result holds)
type stateFile struct {
	Version  int            `json:"version"`
	Results  []stateResult  `json:"results,omitempty"`
	Defaults []stateDefault `json:"defaults,omitempty"`
}

type stateResult struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	CompletedAt int64       `json:"completedAt"` // Unix milliseconds
	Request     RunRequest  `json:"request"`
	Tasks       []stateTask `json:"tasks,omitempty"`
	Tests       []stateTest `json:"tests,omitempty"`
}

type stateTask struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Controller  string    `json:"controller"`
	Output      []byte    `json:"output,omitempty"`
	OutputStart int64     `json:"outputStart,omitempty"`
	Other       []Message `json:"other,omitempty"`
}

type stateTest struct {
	Item    Item        `json:"item"`
	Parent  string      `json:"parent,omitempty"`
	Own     State       `json:"own"`
	Shown   State       `json:"shown"`
	OwnDur  int64       `json:"ownDuration"`
	Dur     int64       `json:"duration"`
	Retired bool        `json:"retired,omitempty"`
	Tasks   []TaskState `json:"tasks,omitempty"`
}

type stateDefault struct {
	Controller string `json:"controller"`
	Label      string `json:"label"`
	Group      Group  `json:"group"`
	IsDefault  bool   `json:"isDefault"`
}

// SaveState encodes what is saved with the workspace's UI state. It fails with TestingStateEncodeFailed.
func (s *Service) SaveState() ([]byte, error) {
	s.mu.Lock()
	st := stateFile{Version: StateVersion}
	for _, r := range s.results {
		if r.completedAt.IsZero() || !r.persist {
			continue
		}
		if len(st.Results) == MaxResults {
			break
		}
		st.Results = append(st.Results, r.encodeLocked())
	}
	for k, v := range s.userDefault {
		st.Defaults = append(st.Defaults, stateDefault{Controller: k.controller, Label: k.label, Group: k.group, IsDefault: v})
	}
	marshal := s.opt.Marshal
	s.mu.Unlock()
	slices.SortFunc(st.Defaults, func(a, b stateDefault) int {
		switch {
		case a.Controller != b.Controller:
			return compareStrings(a.Controller, b.Controller)
		case a.Label != b.Label:
			return compareStrings(a.Label, b.Label)
		}
		return int(a.Group) - int(b.Group)
	})
	if marshal == nil {
		marshal = json.Marshal
	}
	data, err := marshal(st)
	if err != nil {
		return nil, errors.NewErr("TestingStateEncodeFailed", err)
	}
	return data, nil
}

func compareStrings(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// encodeLocked makes the saved form of a result, with the output of its tasks trimmed to the last MaxSavedOutput bytes in all.
func (r *Result) encodeLocked() stateResult {
	out := stateResult{ID: r.id, Name: r.name, CompletedAt: r.completedAt.UnixMilli(), Request: r.request.clone()}
	budget := MaxSavedOutput
	out.Tasks = make([]stateTask, len(r.tasks))
	starts := make([]int64, len(r.tasks))
	for i := len(r.tasks) - 1; i >= 0; i-- {
		t := r.tasks[i]
		keep := min(len(t.output), max(budget, 0))
		budget -= keep
		start := t.outStart + int64(len(t.output)-keep)
		starts[i] = start
		st := stateTask{ID: t.id, Name: t.name, Controller: t.controller, Output: slices.Clone(t.output[len(t.output)-keep:]), OutputStart: start}
		for _, m := range t.other {
			if m.Offset >= start {
				st.Other = append(st.Other, m)
			}
		}
		out.Tasks[i] = st
	}
	for _, id := range r.order {
		n := r.nodes[id]
		item := n.item
		item.Tags = slices.Clone(item.Tags)
		test := stateTest{Item: item, Parent: n.parent, Own: n.own, Shown: n.shown, OwnDur: int64(n.ownDur), Dur: int64(n.dur), Retired: n.retired, Tasks: make([]TaskState, len(n.tasks))}
		for i, ts := range n.tasks {
			test.Tasks[i] = TaskState{State: ts.State, Duration: ts.Duration}
			for _, m := range ts.Messages {
				if m.Type != MessageOutput || (i < len(starts) && m.Offset >= starts[i]) {
					test.Tasks[i].Messages = append(test.Tasks[i].Messages, m)
				}
			}
		}
		out.Tests = append(out.Tests, test)
	}
	return out
}

// RestoreState reads the results of a saved state and adds them to the list, in place of those it read before: the results of a run
// that goes on are left alone. A state that is not the JSON of this version fails with the tags TestingStateInvalid and
// TestingStateUnsupported and leaves the model as it was; a result that cannot be used (no id, a state that is none, a test with
// another parent than a test above it) is dropped.
func (s *Service) RestoreState(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	var st stateFile
	if err := json.Unmarshal(data, &st); err != nil {
		return errors.NewErr("TestingStateInvalid", err)
	}
	if st.Version != StateVersion {
		return errors.New("TestingStateUnsupported", "the saved test state has another version", "version", st.Version, "want", StateVersion)
	}
	s.mu.Lock()
	s.results = slices.DeleteFunc(s.results, func(r *Result) bool { return r.hydrated })
	s.userDefault = map[profileKey]bool{}
	for _, d := range st.Defaults {
		if d.Group.Valid() {
			s.userDefault[profileKey{d.Controller, d.Label, d.Group}] = d.IsDefault
		}
	}
	for _, ctl := range s.controllers {
		for _, p := range ctl.profiles {
			if v, ok := s.userDefault[p.key()]; ok {
				p.isDefault = v
			} else {
				p.isDefault = p.initial
			}
		}
		sortProfiles(ctl.profiles)
	}
	if len(st.Results) > MaxResults {
		st.Results = st.Results[:MaxResults]
	}
	seen := map[string]bool{}
	for _, r := range s.results {
		seen[r.id] = true
	}
	for i := len(st.Results) - 1; i >= 0; i-- {
		sr := st.Results[i]
		if res := s.decodeLocked(sr); res != nil && !seen[res.id] {
			seen[res.id] = true
			s.pushLocked(res)
		}
	}
	s.emitLocked(Event{Kind: EventProfiles})
	s.unlock()
	return nil
}

// decodeLocked makes the result of a saved one, or nil when it cannot be used.
func (s *Service) decodeLocked(sr stateResult) *Result {
	if sr.ID == "" || len(sr.Tests) > MaxSavedTests {
		return nil
	}
	r := &Result{
		svc: s, id: sr.ID, name: sr.Name, persist: true, request: sr.Request.clone(), completedAt: time.UnixMilli(sr.CompletedAt),
		insertOrder: -1, hydrated: true, nodes: map[string]*node{}, idle: make(chan struct{}),
	}
	for _, t := range sr.Tasks {
		r.tasks = append(r.tasks, &task{id: t.ID, name: t.Name, controller: t.Controller, output: t.Output, outStart: max(t.OutputStart, 0), other: t.Other})
	}
	for _, t := range sr.Tests {
		if t.Item.ID == "" || r.nodes[t.Item.ID] != nil || !t.Own.Valid() || !t.Shown.Valid() || len(t.Tasks) != len(r.tasks) {
			return nil
		}
		n := &node{item: t.Item, own: t.Own, shown: t.Shown, ownDur: time.Duration(t.OwnDur), dur: time.Duration(t.Dur), retired: t.Retired, tasks: t.Tasks}
		for _, ts := range n.tasks {
			if !ts.State.Valid() {
				return nil
			}
		}
		if t.Parent != "" {
			p := r.nodes[t.Parent]
			if p == nil {
				return nil
			}
			n.parent = t.Parent
			p.children = append(p.children, t.Item.ID)
		}
		r.nodes[t.Item.ID] = n
		r.order = append(r.order, t.Item.ID)
		r.counts[n.own]++
	}
	return r
}
