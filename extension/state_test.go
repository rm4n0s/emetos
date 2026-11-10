package extension

import (
	"encoding/json"
	"testing"

	"github.com/rm4n0s/errors"
)

// memState is a State in memory; with fail set it keeps nothing, as a scope without a state does.
type memState struct {
	values map[string]json.RawMessage
	fail   bool
}

func (m *memState) Get(key string) (json.RawMessage, bool) {
	v, ok := m.values[key]
	return v, ok
}

func (m *memState) Set(key string, value any) error {
	if m.fail {
		return errors.New("ExtensionStateUnavailable", "the scope keeps no state")
	}
	if value == nil {
		delete(m.values, key)
		return nil
	}
	b, err := json.Marshal(value)
	if err != nil {
		return errors.NewErr("ExtensionStateInvalid", err)
	}
	if m.values == nil {
		m.values = map[string]json.RawMessage{}
	}
	m.values[key] = b
	return nil
}

func TestANoticeIsToldTheFirstTimeOnly(t *testing.T) {
	type step struct {
		// forget calls Forget for the name; otherwise FirstTimeSaying is called with the message and must answer want.
		forget        bool
		what, message string
		want          bool
	}
	tests := []struct {
		name  string
		fail  bool
		steps []step
		keys  int // the values kept at the end
	}{
		{"told once", false, []step{{what: "missing:gopls", want: true}, {what: "missing:gopls"}, {what: "missing:gopls"}}, 1},
		{"each thing has its own answer", false, []step{{what: "missing:gopls", want: true}, {what: "missing:dlv", want: true}, {what: "missing:gopls"}, {what: "missing:dlv"}}, 2},
		{"told again after it was forgotten", false, []step{{what: "missing:git", want: true}, {forget: true, what: "missing:git"}, {what: "missing:git", want: true}, {what: "missing:git"}}, 1},
		{"forgetting what was never told keeps nothing", false, []step{{forget: true, what: "missing:git"}}, 0},
		{"another message about the same thing is told", false, []step{
			{what: "server:ra", message: "ra was not found", want: true}, {what: "server:ra", message: "ra was not found"},
			{what: "server:ra", message: "ra 1.2 is too old", want: true}, {what: "server:ra", message: "ra 1.2 is too old"},
			{what: "server:ra", message: "ra was not found", want: true},
		}, 1},
		{"a state that keeps nothing tells every time", true, []step{{what: "missing:gopls", want: true}, {what: "missing:gopls", want: true}, {forget: true, what: "missing:gopls"}}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &memState{fail: tt.fail}
			for i, st := range tt.steps {
				if st.forget {
					Forget(s, st.what)
					continue
				}
				got := FirstTimeSaying(s, st.what, st.message)
				if got != st.want {
					t.Errorf("step %d: FirstTimeSaying(%q, %q) = %v, want %v", i, st.what, st.message, got, st.want)
				}
			}
			if len(s.values) != tt.keys {
				t.Errorf("kept %v, want %d values", s.values, tt.keys)
			}
		})
	}
}

func TestFirstTimeIsFirstTimeSayingWithoutAMessage(t *testing.T) {
	s := &memState{}
	if !FirstTime(s, "missing:gopls") || FirstTime(s, "missing:gopls") || FirstTimeSaying(s, "missing:gopls", "") {
		t.Errorf("kept %v", s.values)
	}
	// the value is kept under a key of its own, apart from what the extension keeps under the name
	if _, ok := s.Get("told:missing:gopls"); !ok || len(s.values) != 1 {
		t.Errorf("kept %v", s.values)
	}
	Forget(s, "missing:gopls")
	if len(s.values) != 0 || !FirstTime(s, "missing:gopls") {
		t.Errorf("kept %v after Forget", s.values)
	}
}
