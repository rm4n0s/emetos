package gitscm

import (
	"context"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
	"github.com/rm4n0s/errors"
)

func TestTheStatusRequestRefusesWhatItCannotAnswer(t *testing.T) {
	e := gittest.NewEnv(t)
	a := e.Init("a")
	b := e.Init("b")
	r := newRig(t, e, rigOptions{folder: e.Work, settings: map[string]any{"git.repositoryScanMaxDepth": 1}})
	r.settle()
	tests := []struct {
		name  string
		data  []byte
		route string
	}{
		{"a root with no repository", []byte(`{"root":"/nowhere"}`), "handleStatus.GitscmNoRepository"},
		{"no root with two repositories open", nil, "handleStatus.GitscmNoRepository"},
		{"data that is not JSON", []byte(`[`), "handleStatus.GitscmStatusRequestInvalid"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := r.x.handleStatus(context.Background(), extension.Event{Topic: TopicStatus, Data: tc.data})
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tc.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
	// and it answers for the root it is given
	if v, err := r.x.handleStatus(context.Background(), extension.Event{Topic: TopicStatus, Data: []byte(`{"root":"` + b.Dir + `"}`)}); err != nil || v.(RepositoryState).Root != b.Dir {
		t.Errorf("answer = %v, %v", v, err)
	}
	_ = a
}
