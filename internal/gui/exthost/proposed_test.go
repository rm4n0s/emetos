package exthost

import (
	"context"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension"
)

// The proposed change (step 13.3): Editors().OpenProposedChange shows the diff editor of a change and waits for the answer.

// proposeResult is what a call of OpenProposedChange returned.
type proposeResult struct {
	res extension.ProposedResult
	err error
}

// propose calls OpenProposedChange from a goroutine, as an extension does, and returns the channel of its result. The window
// side runs when the test pumps.
func propose(tb *tab, ctx context.Context, change extension.ProposedChange) <-chan proposeResult {
	out := make(chan proposeResult, 1)
	a := apiOf(tb)
	go func() {
		res, err := a.Editors().OpenProposedChange(ctx, change)
		out <- proposeResult{res, err}
	}()
	return out
}

// shown pumps until the window was asked to show the n-th proposed change.
func shown(t *testing.T, tb *tab, n int) *fakeProposal {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for len(tb.editors.proposals) < n {
		if time.Now().After(deadline) {
			t.Fatalf("the window was asked to show %d proposed changes, want %d", len(tb.editors.proposals), n)
		}
		tb.pump()
		time.Sleep(time.Millisecond)
	}
	return tb.editors.proposals[n-1]
}

// result waits for the result of a call.
func result(t *testing.T, ch <-chan proposeResult) proposeResult {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("OpenProposedChange did not return")
		return proposeResult{}
	}
}

// pending checks that a call has not returned.
func pending(t *testing.T, ch <-chan proposeResult) {
	t.Helper()
	select {
	case r := <-ch:
		t.Fatalf("OpenProposedChange returned too early: %+v", r)
	case <-time.After(30 * time.Millisecond):
	}
}

func TestAProposedChangeIsShownAndAcceptedWithTheTextOfTheRightSide(t *testing.T) {
	tests := []struct {
		name   string
		answer extension.ProposedResult
		want   extension.ProposedResult
	}{
		{"accept as proposed", extension.ProposedResult{Accepted: true, Text: "new\n"}, extension.ProposedResult{Accepted: true, Text: "new\n"}},
		{"accept with the text edited", extension.ProposedResult{Accepted: true, Text: "newer\n", Edited: true}, extension.ProposedResult{Accepted: true, Text: "newer\n", Edited: true}},
		{"reject", extension.ProposedResult{}, extension.ProposedResult{}},
		{"close is a rejection", extension.ProposedResult{}, extension.ProposedResult{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tb := edTab(t, &thingKind{}, nil)
			tb.activate()
			change := extension.ProposedChange{URI: "file:///work/main.go", Original: "old\n", Proposed: "new\n", Title: "main.go (proposed)"}
			ch := propose(tb, context.Background(), change)
			p := shown(t, tb, 1)
			if p.change != change {
				t.Errorf("the window got %+v, want %+v", p.change, change)
			}
			pending(t, ch)
			p.reply(tc.answer)
			got := result(t, ch)
			if got.err != nil || got.res != tc.want {
				t.Errorf("the call returned %+v, %v, want %+v", got.res, got.err, tc.want)
			}
			if p.closed != 0 {
				t.Error("the tab of an answered change was closed by the extension")
			}
		})
	}
}

func TestACancelledProposedChangeClosesItsTab(t *testing.T) {
	tb := edTab(t, &thingKind{}, nil)
	tb.activate()
	ctx, cancel := context.WithCancel(context.Background())
	ch := propose(tb, ctx, extension.ProposedChange{URI: "file:///work/main.go", Proposed: "new\n"})
	p := shown(t, tb, 1)
	cancel()
	got := result(t, ch)
	route(t, got.err, "editorsAPI.OpenProposedChange.ExtensionProposedChangeCancelled")
	tb.pump()
	if p.closed != 1 {
		t.Errorf("the tab was closed %d times, want 1", p.closed)
	}
	p.reply(extension.ProposedResult{Accepted: true}) // an answer that comes late changes nothing
}

func TestAProposedChangeEndsWhenTheExtensionIsDeactivated(t *testing.T) {
	tb := edTab(t, &thingKind{}, nil)
	tb.activate()
	a := apiOf(tb)
	ch := propose(tb, context.Background(), extension.ProposedChange{URI: "file:///work/main.go", Proposed: "new\n"})
	p := shown(t, tb, 1)
	a.(*api).inst.kill()
	got := result(t, ch)
	route(t, got.err, "editorsAPI.OpenProposedChange.ExtensionProposedChangeCancelled")
	tb.pump()
	if p.closed != 1 {
		t.Errorf("the tab was closed %d times, want 1", p.closed)
	}
	_, err := a.Editors().OpenProposedChange(context.Background(), extension.ProposedChange{URI: "file:///work/main.go"})
	route(t, err, "editorsAPI.OpenProposedChange.ExtensionDeactivated")
}

func TestAProposedChangeCancelledBeforeTheWindowShowsItIsClosedWhenItComes(t *testing.T) {
	tb := edTab(t, &thingKind{}, nil)
	tb.activate()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ch := propose(tb, ctx, extension.ProposedChange{URI: "file:///work/main.go", Proposed: "new\n"})
	got := result(t, ch)
	route(t, got.err, "editorsAPI.OpenProposedChange.ExtensionProposedChangeCancelled")
	p := shown(t, tb, 1) // the window gets the request later, and closes the tab at once
	deadline := time.Now().Add(5 * time.Second)
	for p.closed == 0 && time.Now().Before(deadline) {
		tb.pump()
		time.Sleep(time.Millisecond)
	}
	if p.closed != 1 {
		t.Errorf("the tab was closed %d times, want 1", p.closed)
	}
}

func TestAProposedChangeThatCannotBeShownFails(t *testing.T) {
	tb := edTab(t, &thingKind{}, nil)
	tb.activate()
	a := apiOf(tb)
	_, err := a.Editors().OpenProposedChange(context.Background(), extension.ProposedChange{Proposed: "x"})
	route(t, err, "editorsAPI.OpenProposedChange.ExtensionProposedChangeInvalid")

	tb.editors.failPropose = context.DeadlineExceeded
	ch := propose(tb, context.Background(), extension.ProposedChange{URI: "file:///work/main.go"})
	deadline := time.Now().Add(5 * time.Second)
	var got proposeResult
	for done := false; !done && time.Now().Before(deadline); {
		tb.pump()
		select {
		case got = <-ch:
			done = true
		case <-time.After(time.Millisecond):
		}
	}
	if got.err != context.DeadlineExceeded {
		t.Errorf("the error of the window = %v, want it returned as it is", got.err)
	}
}

func TestSeveralProposedChangesWaitIndependently(t *testing.T) {
	tb := edTab(t, &thingKind{}, nil)
	tb.activate()
	first := propose(tb, context.Background(), extension.ProposedChange{URI: "file:///work/a.go", Proposed: "a"})
	p1 := shown(t, tb, 1)
	second := propose(tb, context.Background(), extension.ProposedChange{URI: "file:///work/b.go", Proposed: "b"})
	p2 := shown(t, tb, 2)
	p2.reply(extension.ProposedResult{Accepted: true, Text: "b"})
	if got := result(t, second); !got.res.Accepted || got.res.Text != "b" {
		t.Errorf("the second call returned %+v", got)
	}
	pending(t, first)
	p1.reply(extension.ProposedResult{})
	if got := result(t, first); got.res.Accepted {
		t.Errorf("the first call returned %+v", got)
	}
}
