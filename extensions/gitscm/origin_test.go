package gitscm

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/helper"
)

// The questions of a client's commands (step 24.25, SPEC §10.18 "As built (step 24.25)", D50): a command that a client of a hosted workspace
// runs in its view asks the password of git in that view, a command nobody asked for asks it in the window of the host, and two clients at
// once each get their own box. The origin travels from the command to the git it starts, over the helper's socket, and back with the
// question; the answer reaches git.

func TestTheCredentialPromptOfAClientsCommandIsShownInThatClientsView(t *testing.T) {
	w := newHTTPWorld(t)
	r := newWorldRig(t, w, map[string]any{"git.confirmSync": false})
	rp := r.repo(w.work.Dir)
	bare := filepath.Join(w.env.Work, "origin.git")

	const viewA, viewB extension.Origin = 7, 9
	// the source of the world has not seen what the clone pushed: it takes that first
	publish := func(name string) string {
		w.src.Git("pull", "-q", "--rebase", "origin", "main")
		return w.publish(name)
	}
	t.Run("a fetch of one client", func(t *testing.T) {
		head := publish("fetched.txt")
		r.api.RunAs(viewA, func() {
			if err := r.api.Run("git.fetch", nil); err != nil {
				t.Fatal(err)
			}
		})
		in := r.nextInput(0)
		if o := in.Options(); o.Origin != viewA || !o.Password || !strings.HasPrefix(o.Prompt, "Password for '") {
			t.Fatalf("the box is %+v, want a password box of the view %d", o, viewA)
		}
		in.Accept(typed)
		r.idle(rp)
		if got := w.work.Git("rev-parse", "origin/main"); got != head {
			t.Errorf("origin/main is %s, want %s: the answer did not reach git", got, head)
		}
	})
	t.Run("a push of another client", func(t *testing.T) {
		w.work.Git("merge", "-q", "--ff-only", "origin/main")
		w.work.Write("pushed.txt", "mine\n")
		head := w.work.Commit("pushed")
		r.refresh(rp)
		r.api.RunAs(viewB, func() {
			if err := r.api.Run("git.push", nil); err != nil {
				t.Fatal(err)
			}
		})
		in := r.nextInput(1)
		if o := in.Options(); o.Origin != viewB {
			t.Fatalf("the box is %+v, want one of the view %d", o, viewB)
		}
		in.Accept(typed)
		r.idle(rp)
		if got := w.env.Git(bare, "rev-parse", "main"); got != head {
			t.Errorf("the remote has %s, want %s", got, head)
		}
	})
	t.Run("a command of the host's window", func(t *testing.T) {
		publish("hosts.txt")
		if err := r.api.Run("git.fetch", nil); err != nil {
			t.Fatal(err)
		}
		in := r.nextInput(2)
		if o := in.Options(); o.Origin != extension.HostOrigin {
			t.Fatalf("the box is %+v, want one of the host's window", o)
		}
		in.Accept(typed)
		r.idle(rp)
	})
	t.Run("two clients that ask together each get a box on their own screen", func(t *testing.T) {
		// the commands of one repository run one after the other (its queue), so the second box comes when the first is answered
		publish("both.txt")
		for _, o := range []extension.Origin{viewA, viewB} {
			r.api.RunAs(o, func() {
				if err := r.api.Run("git.fetch", nil); err != nil {
					t.Fatal(err)
				}
			})
		}
		// the two fetches start their git at the same time, so either may ask first: what counts is that each box is on the screen of
		// one of the two clients and that each client gets one
		first := r.nextInput(3)
		got := []extension.Origin{first.Options().Origin}
		first.Accept(typed)
		second := r.nextInput(4)
		got = append(got, second.Options().Origin)
		second.Accept(typed)
		r.idle(rp)
		if !(got[0] == viewA && got[1] == viewB) && !(got[0] == viewB && got[1] == viewA) {
			t.Errorf("the boxes are for %v, want one for the view %d and one for the view %d", got, viewA, viewB)
		}
	})
}

// A password asked by a git typed in a terminal goes to the screen that typed last in that terminal (SPEC §10.25 "Clients of a shared
// workspace"): the helper's request carries the number of the terminal, and the Terminal extension remembered who typed in it.
func TestThePasswordOfAGitTypedInATerminalIsAskedOnTheScreenOfTheTypist(t *testing.T) {
	w := newHTTPWorld(t)
	r := newWorldRig(t, w, nil)
	r.api.Origins().Remember("terminal:4", 7)
	tests := []struct {
		name string
		req  helper.Request
		want extension.Origin
	}{
		{"a terminal that a client typed in", helper.Request{Terminal: "4"}, 7},
		{"a terminal nobody remembered", helper.Request{Terminal: "5"}, extension.HostOrigin},
		{"a command that a client started", helper.Request{Terminal: "4", Origin: 9}, 9},
		{"a command nobody started", helper.Request{}, extension.HostOrigin},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := r.x.originOfRequest(tc.req); got != tc.want {
				t.Errorf("the question is asked on %d, want %d", got, tc.want)
			}
		})
	}
}
