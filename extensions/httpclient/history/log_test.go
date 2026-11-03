package history

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/httpclient/format"
)

// memState is a state in memory that refuses what the real one refuses: a value over 64 KiB.
type memState struct {
	mu   sync.Mutex
	vals map[string]json.RawMessage
}

func newState() *memState { return &memState{vals: map[string]json.RawMessage{}} }

func (m *memState) Get(key string) (json.RawMessage, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.vals[key]
	return v, ok
}

func (m *memState) Set(key string, value any) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if value == nil {
		delete(m.vals, key)
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(data) > 64*1024 {
		return errors.New("TestStateValueTooLarge", "a value of the state is over 64 KiB", "key", key, "size", len(data))
	}
	m.vals[key] = data
	return nil
}

func (m *memState) keys() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.vals)
}

func sent(i int) Sent {
	return Sent{
		Time:    time.Date(2026, 11, 3, 10, i, 0, 0, time.UTC),
		Request: format.Request{Name: fmt.Sprintf("r%d", i), Method: "get", URL: fmt.Sprintf("https://h/%d", i)},
		Status:  200, StatusText: "OK", Duration: 12 * time.Millisecond, Size: 100,
	}
}

func TestAddAndList(t *testing.T) {
	st := newState()
	l := Open(st, Options{})
	for i := 0; i < 25; i++ {
		e, err := l.Add(sent(i))
		if err != nil {
			t.Fatal(err)
		}
		if e.Seq != i || e.Method != "GET" {
			t.Fatalf("entry %d = %+v", i, e)
		}
	}
	got := l.List()
	if len(got) != 25 || got[0].URL != "https://h/24" || got[24].URL != "https://h/0" {
		t.Fatalf("List = %d entries, first %q, last %q", len(got), got[0].URL, got[len(got)-1].URL)
	}
	// a new log over the same state sees the same entries: they are kept
	if again := Open(st, Options{}).List(); len(again) != 25 {
		t.Errorf("after a restart: %d entries", len(again))
	}
	if e, ok := l.Get(13); !ok || e.URL != "https://h/13" {
		t.Errorf("Get(13) = %+v, %v", e, ok)
	}
	if _, ok := l.Get(99); ok {
		t.Error("Get found an entry that is not there")
	}
}

func TestTheLimit(t *testing.T) {
	for _, tc := range []struct {
		name               string
		limit, added, want int
	}{
		{"under", 50, 20, 20},
		{"exactly", 20, 20, 20},
		{"over", 15, 33, 15},
		{"over by a chunk", 10, 55, 10},
		{"none kept", 0, 5, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := newState()
			l := Open(st, Options{Limit: func() int { return tc.limit }})
			for i := 0; i < tc.added; i++ {
				if _, err := l.Add(sent(i)); err != nil {
					t.Fatal(err)
				}
			}
			got := l.List()
			if len(got) != tc.want {
				t.Fatalf("%d entries, want %d", len(got), tc.want)
			}
			if tc.want > 0 && got[0].Seq != tc.added-1 {
				t.Errorf("newest = %d", got[0].Seq)
			}
			if tc.want > 0 && got[len(got)-1].Seq != tc.added-tc.want {
				t.Errorf("oldest = %d, want %d", got[len(got)-1].Seq, tc.added-tc.want)
			}
			// the state holds the chunks of the entries kept and nothing of the others
			if most := tc.limit/ChunkSize + 3; st.keys() > most {
				t.Errorf("the state holds %d keys, want at most %d", st.keys(), most)
			}
		})
	}
}

func TestALowerLimitShowsFewerAtOnce(t *testing.T) {
	limit := 100
	l := Open(newState(), Options{Limit: func() int { return limit }})
	for i := 0; i < 40; i++ {
		if _, err := l.Add(sent(i)); err != nil {
			t.Fatal(err)
		}
	}
	limit = 7
	if got := l.List(); len(got) != 7 || got[0].Seq != 39 {
		t.Errorf("%d entries, newest %d", len(got), got[0].Seq)
	}
}

func TestClear(t *testing.T) {
	st := newState()
	l := Open(st, Options{})
	for i := 0; i < 23; i++ {
		if _, err := l.Add(sent(i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := l.Clear(); err != nil {
		t.Fatal(err)
	}
	if got := l.List(); len(got) != 0 {
		t.Fatalf("%d entries after Clear", len(got))
	}
	e, err := l.Add(sent(99))
	if err != nil {
		t.Fatal(err)
	}
	if e.Seq != 23 {
		t.Errorf("the numbers go on: %d", e.Seq)
	}
	if got := l.List(); len(got) != 1 || got[0].Seq != 23 {
		t.Errorf("after Clear and Add: %+v", got)
	}
	if err := l.Clear(); err != nil {
		t.Fatal(err)
	}
	if st.keys() != 1 {
		t.Errorf("the state holds %d keys after Clear, want the counters only", st.keys())
	}
}

func TestSecretsAreMaskedInTheStoredEntry(t *testing.T) {
	st := newState()
	l := Open(st, Options{})
	s := sent(1)
	s.Request.Headers = []format.Pair{{Name: "Authorization", Value: "Bearer abc.def", Enabled: true}}
	s.Request.URL = "https://h/x?token=zzz"
	s.Request.Auth = &format.Auth{Type: format.AuthBearer, Token: "literal-token"}
	s.Request.Body = &format.Body{Type: format.BodyJSON, File: "b.body.json"}
	s.Body = `{"password":"hunter2","name":"ada"}`
	if _, err := l.Add(s); err != nil {
		t.Fatal(err)
	}
	for key, raw := range st.vals {
		for _, secret := range []string{"abc.def", "zzz", "literal-token", "hunter2"} {
			if strings.Contains(string(raw), secret) {
				t.Errorf("the state key %s holds %q: %s", key, secret, raw)
			}
		}
	}
	got := l.List()[0]
	if got.Request.Headers[0].Value != "Bearer "+Masked || got.Body != `{"password":"`+Masked+`","name":"ada"}` {
		t.Errorf("entry = %+v", got)
	}
}

func TestALargeEntryLosesItsBodyThenItsRequest(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		body                       string
		headers                    int
		wantBody, wantReq, wantCut bool
	}{
		{"small", "{}", 1, true, true, false},
		{"a large body", strings.Repeat("x", 9000), 1, false, true, true},
		{"many headers", "", 400, false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := Open(newState(), Options{})
			s := sent(1)
			for i := 0; i < tc.headers; i++ {
				s.Request.Headers = append(s.Request.Headers, format.Pair{Name: fmt.Sprintf("X-H-%d", i), Value: "value-value", Enabled: true})
			}
			s.Body = tc.body
			e, err := l.Add(s)
			if err != nil {
				t.Fatal(err)
			}
			if (e.Body != "") != (tc.wantBody && tc.body != "") || (e.Request != nil) != tc.wantReq || e.Truncated != tc.wantCut {
				t.Errorf("body %v request %v truncated %v", e.Body != "", e.Request != nil, e.Truncated)
			}
			if data, _ := json.Marshal(e); len(data) > MaxEntryBytes {
				t.Errorf("the entry takes %d bytes", len(data))
			}
		})
	}
}

func TestEveryChunkFitsTheStateEvenWithLargeEntries(t *testing.T) {
	l := Open(newState(), Options{})
	for i := 0; i < 3*ChunkSize; i++ {
		s := sent(i)
		s.Body = strings.Repeat("y", 100000)
		for j := 0; j < 100; j++ {
			s.Request.Headers = append(s.Request.Headers, format.Pair{Name: fmt.Sprintf("X-%d", j), Value: strings.Repeat("v", 60), Enabled: true})
		}
		if _, err := l.Add(s); err != nil {
			t.Fatalf("entry %d: %v", i, err)
		}
	}
}

func TestAddRefusesARequestWithoutAMethodOrURL(t *testing.T) {
	l := Open(newState(), Options{})
	for _, tc := range []struct{ name, method, url string }{
		{"no method", "", "https://h"}, {"no URL", "GET", "  "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := sent(1)
			s.Request.Method, s.Request.URL = tc.method, tc.url
			_, err := l.Add(s)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute("Log.Add.HttpHistoryEntryInvalid") {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
}
