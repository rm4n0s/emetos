package timeline

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"

	"github.com/rm4n0s/emetos/internal/core/timeline"
)

// at is a time n minutes after the epoch of the tests; the pane's clock is epoch+1000 minutes.
func at(n int) time.Time { return epoch.Add(time.Duration(n) * time.Minute) }

var epoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// fakeProvider is a provider whose answers a test controls: each call is recorded and, unless gate is set, answered by answer.
type fakeProvider struct {
	mu     sync.Mutex
	calls  []call
	answer func(uri string, o timeline.Options) (timeline.Page, error)
	gate   chan struct{}
}

type call struct {
	URI  string
	Opts timeline.Options
	Ctx  context.Context
}

func (f *fakeProvider) Provide(ctx context.Context, uri string, o timeline.Options) (timeline.Page, error) {
	f.mu.Lock()
	f.calls = append(f.calls, call{uri, o, ctx})
	gate, answer := f.gate, f.answer
	f.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
		}
	}
	if answer == nil {
		return timeline.Page{}, nil
	}
	return answer(uri, o)
}

func (f *fakeProvider) Calls() []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]call(nil), f.calls...)
}

// rig is a pane over a service with fake providers, driven from the test goroutine: the answers of the providers are posted and run
// by settle, as the UI thread would.
type rig struct {
	t       *testing.T
	svc     *timeline.Service
	pane    *Pane
	mu      sync.Mutex
	posted  []func()
	visible bool
	scm     int
	pageSz  int
	runs    []ran
	excl    [][]string
	changes int
	after   []func()
	ts      *gluttertest.Tester
}

type ran struct {
	ID   string
	Args any
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{t: t, svc: timeline.New(), visible: true, scm: 1, pageSz: 3}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	r.pane = NewPane(PaneOpts{
		Ctx: ctx, Service: r.svc,
		Post: func(f func()) {
			r.mu.Lock()
			r.posted = append(r.posted, f)
			r.mu.Unlock()
		},
		Now:             func() time.Time { return epoch.Add(1000 * time.Minute) },
		PageSize:        func() int { return r.pageSz },
		PageOnScroll:    func() bool { return true },
		SCMProviders:    func() int { return r.scm },
		Visible:         func() bool { return r.visible },
		Run:             func(id string, args any) { r.runs = append(r.runs, ran{id, args}) },
		ExcludedChanged: func(ids []string) { r.excl = append(r.excl, ids) },
	})
	r.pane.OnChange(func() { r.changes++ })
	t.Cleanup(r.pane.Dispose)
	r.pane.VisibilityChanged()
	return r
}

// run runs what the providers posted so far.
func (r *rig) run() {
	for {
		r.mu.Lock()
		list := r.posted
		r.posted = nil
		r.mu.Unlock()
		if len(list) == 0 {
			return
		}
		for _, f := range list {
			f()
		}
	}
}

// settle runs the posted work until no request is pending.
func (r *rig) settle() {
	r.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		r.run()
		if len(r.pane.pending) == 0 {
			return
		}
		if time.Now().After(deadline) {
			r.t.Fatalf("requests still pending: %v", r.pending())
		}
		time.Sleep(time.Millisecond)
	}
}

func (r *rig) pending() []string {
	var out []string
	for id := range r.pane.pending {
		out = append(out, id)
	}
	return out
}

// provider registers a provider for the scheme file.
func (r *rig) provider(id string, f *fakeProvider) *timeline.Registration {
	r.t.Helper()
	reg, err := r.svc.Register("test.ext", timeline.Descriptor{ID: id, Label: id, Schemes: []string{"file"}}, f)
	if err != nil {
		r.t.Fatal(err)
	}
	return reg
}

// labels are the texts of the rows.
func (r *rig) labels() []string {
	var out []string
	for _, row := range r.pane.Rows() {
		out = append(out, row.Text())
	}
	return out
}

// items makes n items from the newest, with the IDs, labels and times of newest-1... and a cursor when more is set.
func items(prefix string, from, n int) []timeline.Item {
	var out []timeline.Item
	for i := 0; i < n; i++ {
		k := from + i
		out = append(out, timeline.Item{ID: prefix + itoa(k), Label: prefix + itoa(k), Timestamp: at(1000 - k*10)})
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// pager answers from a list of items, limit and cursor (the position as a decimal text) like a real provider.
func pager(all []timeline.Item) func(string, timeline.Options) (timeline.Page, error) {
	return func(_ string, o timeline.Options) (timeline.Page, error) {
		start := 0
		if o.Cursor != "" {
			for _, c := range o.Cursor {
				start = start*10 + int(c-'0')
			}
		}
		end := len(all)
		if o.Limit > 0 {
			end = min(end, start+o.Limit)
		}
		page := timeline.Page{Items: append([]timeline.Item(nil), all[start:end]...)}
		if end < len(all) {
			page.Cursor = itoa(end)
		}
		return page, nil
	}
}
