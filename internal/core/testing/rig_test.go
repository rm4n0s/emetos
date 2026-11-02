package testing_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	ct "github.com/rm4n0s/emetos/internal/core/testing"
)

// rig is a service with one controller "c" whose tree is
//
//	c/pkg/a, c/pkg/b, c/pkg/sub/x, c/other
type rig struct {
	t    *testing.T
	svc  *ct.Service
	ctl  *ct.Controller
	now  time.Time
	mu   sync.Mutex
	evs  []ct.Event
	prof int
}

func id(path ...string) string { return ct.JoinID("c", path...) }

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{t: t, now: time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)}
	r.svc = ct.NewService(ct.Options{Now: func() time.Time {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.now = r.now.Add(time.Second)
		return r.now
	}, CancelDeadline: 200 * time.Millisecond})
	t.Cleanup(r.svc.Close)
	r.svc.Subscribe(func(e ct.Event) {
		r.mu.Lock()
		r.evs = append(r.evs, e)
		r.mu.Unlock()
	})
	ctl, err := r.svc.RegisterController(ct.ControllerOptions{ID: "c", Label: "C"})
	if err != nil {
		t.Fatal(err)
	}
	r.ctl = ctl
	for _, it := range []struct{ path, uri string }{
		{"pkg", "file:///w/pkg"}, {"pkg/a", "file:///w/pkg/a_test.go"}, {"pkg/b", "file:///w/pkg/b_test.go"},
		{"pkg/sub", "file:///w/pkg/sub"}, {"pkg/sub/x", "file:///w/pkg/sub/x_test.go"}, {"other", "file:///w/other_test.go"},
	} {
		parts := strings.Split(it.path, "/")
		r.put(ct.Item{ID: id(parts...), Label: parts[len(parts)-1], URI: it.uri})
	}
	return r
}

func (r *rig) put(it ct.Item) {
	r.t.Helper()
	if err := r.ctl.Upsert(it); err != nil {
		r.t.Fatal(err)
	}
}

// profile adds a run profile whose Run function is fn and returns its id.
func (r *rig) profile(spec ct.ProfileSpec) int {
	r.t.Helper()
	if spec.Label == "" {
		r.prof++
		spec.Label = "profile " + string(rune('A'+r.prof))
	}
	if spec.Group == 0 {
		spec.Group = ct.GroupRun
	}
	pid, err := r.ctl.AddProfile(spec)
	if err != nil {
		r.t.Fatal(err)
	}
	return pid
}

// events returns the events seen so far of the kind; the service delivers on the goroutine that changed it, so after a call returns
// in a test that uses one goroutine they are all there.
func (r *rig) events(kind ct.EventKind) []ct.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []ct.Event
	for _, e := range r.evs {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

// routeOf checks that err is an *errors.Error with the route.
func routeOf(t *testing.T, err error, route string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute(route) {
		t.Errorf("unexpected failure path: %s (want %s)", appErr.Route(), route)
	}
}

// runAll runs the whole tree of the controller with the profile.
func (r *rig) runAll(ctx context.Context, pid int) (*ct.Result, error) {
	return r.svc.RunTests(ctx, ct.RunRequest{
		Group:   ct.GroupRun,
		Targets: []ct.RunTarget{{ControllerID: "c", ProfileID: pid, TestIDs: []string{"c"}}},
	})
}

// plainError is an error that is not an *errors.Error, such as the ones a runner or a library gives.
type plainError string

func (e plainError) Error() string { return string(e) }
