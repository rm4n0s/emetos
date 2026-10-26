package claude

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/rm4n0s/emetos/extensions/agents/claude/claudetest"
)

func TestAuthStatus(t *testing.T) {
	tests := []struct {
		name string
		mode string
		want AuthInfo
	}{
		{"logged in", "in", AuthInfo{LoggedIn: true, Method: "claude.ai", Provider: "firstParty", Email: "user@example.invalid", Organization: "Example", Subscription: "max"}},
		{"not logged in", "out", AuthInfo{LoggedIn: false, Method: "none", Provider: "firstParty"}},
		{"logged in, output that is not JSON", "garbage", AuthInfo{LoggedIn: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, "idle.jsonl")
			t.Setenv(claudetest.EnvAuth, tt.mode)
			got, err := r.adapter.AuthStatus(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("AuthStatus = %+v, want %+v", got, tt.want)
			}
			recs := r.records()
			if len(recs) != 1 || recs[0].Kind != "auth" || !reflect.DeepEqual(recs[0].Argv, []string{"auth", "status"}) {
				t.Errorf("the program was run as %+v", recs)
			}
		})
	}
}

func TestAuthStatusFailures(t *testing.T) {
	t.Run("program missing", func(t *testing.T) {
		a := &Adapter{Path: filepath.Join(t.TempDir(), "missing")}
		_, err := a.AuthStatus(context.Background())
		wantRoute(t, err, "Adapter.AuthStatus->Adapter.Find.ProgramNotFound")
	})
	t.Run("an exit status that is neither 0 nor 1", func(t *testing.T) {
		r := newRig(t, "idle.jsonl")
		t.Setenv(claudetest.EnvAuth, "crash")
		_, err := r.adapter.AuthStatus(context.Background())
		appErr := routeOf(t, err, "Adapter.AuthStatus.AuthStatusFailed")
		if appErr.Metadata["exit"] != 2 {
			t.Errorf("metadata = %v", appErr.Metadata)
		}
	})
	t.Run("a program that takes too long is killed", func(t *testing.T) {
		r := newRig(t, "idle.jsonl")
		t.Setenv(claudetest.EnvAuth, "hang")
		r.adapter.authTimeout = 300e6
		_, err := r.adapter.AuthStatus(context.Background())
		wantRoute(t, err, "Adapter.AuthStatus.AuthStatusFailed")
	})
	t.Run("a context that ended", func(t *testing.T) {
		r := newRig(t, "idle.jsonl")
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := r.adapter.AuthStatus(ctx)
		wantRoute(t, err, "Adapter.AuthStatus.AuthStatusFailed")
	})
	t.Run("a program that cannot start", func(t *testing.T) {
		r := newRig(t, "idle.jsonl")
		r.adapter.Dir = filepath.Join(r.dir, "no-such-folder")
		_, err := r.adapter.AuthStatus(context.Background())
		wantRoute(t, err, "Adapter.AuthStatus.ProcessStartFailed")
	})
}
