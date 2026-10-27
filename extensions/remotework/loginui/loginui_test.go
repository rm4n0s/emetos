package loginui

import (
	"context"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/internal/sshlogin"
)

var size = gui.Size{Width: 640, Height: 420}

// screen is a tab that shows a prompter: what the 15.4 tab does with Build.
type screen struct {
	t       *testing.T
	ts      *gluttertest.Tester
	pr      *Prompter
	posted  chan func()
	rebuild func()
}

func newScreen(t *testing.T, th uitest.Theme) *screen {
	t.Helper()
	s := &screen{t: t, posted: make(chan func(), 16)}
	s.pr = NewPrompter(func(f func()) { s.posted <- f }, func() {
		if s.rebuild != nil {
			s.rebuild()
		}
	})
	host := widgets.NewStatefulBuilder(func(bc widgets.BuildContext, set widgets.StateSetter) widgets.Widget {
		s.rebuild = func() { set(func() {}) }
		children := []widgets.Widget{widgets.NewColoredBox(ui.Theme(bc).EditorBackground, widgets.NewSizedBox(nil))}
		if d := s.pr.Build(bc); d != nil {
			children = append(children, d)
		}
		return widgets.NewStack(children, widgets.StackOpts{Fit: 1})
	})
	s.ts = uitest.Pump(t, th, size, host)
	return s
}

// pump runs what the login goroutine posted to the UI thread.
func (s *screen) pump() {
	s.t.Helper()
	for {
		select {
		case f := <-s.posted:
			f()
			s.ts.Pump()
		default:
			s.ts.Pump()
			return
		}
	}
}

// waitFor pumps until the condition holds.
func (s *screen) waitFor(what string, cond func() bool) {
	s.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		s.pump()
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			s.t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func (s *screen) has(text string) bool { return s.ts.Exists(gluttertest.ByTextContaining(text)) }

func TestPassphrasePrompt(t *testing.T) {
	s := newScreen(t, uitest.Themes()[0])
	ask := s.pr.Passphrase("/keys/id_ed25519")
	type result struct {
		pass []byte
		ok   bool
	}
	run := func(attempt int) chan result {
		ch := make(chan result, 1)
		go func() { p, ok := ask(attempt); ch <- result{p, ok} }()
		return ch
	}

	ch := run(1)
	s.waitFor("the question", s.pr.Showing)
	s.pump()
	if !s.has("/keys/id_ed25519") || s.has("is wrong") {
		t.Error("the first question names the file and says nothing is wrong")
	}
	s.ts.EnterText(gluttertest.ByType[*ui.InputBox](), "secret words")
	s.ts.Tap(gluttertest.ByText("OK"))
	s.waitFor("the answer", func() bool { return len(ch) == 1 })
	if r := <-ch; !r.ok || string(r.pass) != "secret words" {
		t.Errorf("answer = %q, %v", r.pass, r.ok)
	}
	s.waitFor("the question to go", func() bool { return !s.pr.Showing() })

	// the second attempt says the last one was wrong; Enter answers; Cancel is a refusal
	ch = run(2)
	s.waitFor("the question", s.pr.Showing)
	s.pump()
	if !s.has("That passphrase is wrong") {
		t.Error("the second question does not say the passphrase was wrong")
	}
	s.ts.Tap(gluttertest.ByText("Cancel"))
	s.waitFor("the cancel", func() bool { return len(ch) == 1 })
	if r := <-ch; r.ok || r.pass != nil {
		t.Errorf("answer = %q, %v", r.pass, r.ok)
	}
}

func TestPassphraseBoxIsMasked(t *testing.T) {
	s := newScreen(t, uitest.Themes()[0])
	go s.pr.Passphrase("k")(1)
	s.waitFor("the question", s.pr.Showing)
	s.pump()
	s.ts.EnterText(gluttertest.ByType[*ui.InputBox](), "hunter2")
	s.pump()
	if s.has("hunter2") {
		t.Error("the passphrase is shown on the screen")
	}
	s.pr.Close()
}

func TestConfirmHost(t *testing.T) {
	host := sshlogin.FirstUse{Address: "192.168.1.9:47123", Type: "ssh-ed25519", Fingerprint: "SHA256:abcdEFGH0123456789abcdEFGH0123456789abcdEFG"}
	for _, tc := range []struct {
		button string
		want   bool
	}{{"Trust", true}, {"Cancel", false}} {
		t.Run(tc.button, func(t *testing.T) {
			s := newScreen(t, uitest.Themes()[0])
			ch := make(chan bool, 1)
			go func() {
				ok, err := s.pr.ConfirmHost()(context.Background(), host)
				if err != nil {
					t.Error(err)
				}
				ch <- ok
			}()
			s.waitFor("the question", s.pr.Showing)
			s.pump()
			if !s.has(host.Fingerprint) || !s.has("192.168.1.9:47123") || !s.has("compare") {
				t.Error("the question does not show the fingerprint and the instruction")
			}
			s.ts.Tap(gluttertest.ByText(tc.button))
			s.waitFor("the answer", func() bool { return len(ch) == 1 })
			if got := <-ch; got != tc.want {
				t.Errorf("answer = %v, want %v", got, tc.want)
			}
			s.waitFor("the question to go", func() bool { return !s.pr.Showing() })
		})
	}
}

func TestConfirmHostEndsWithItsContext(t *testing.T) {
	s := newScreen(t, uitest.Themes()[0])
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		_, err := s.pr.ConfirmHost()(ctx, sshlogin.FirstUse{Address: "h:1", Fingerprint: "SHA256:x"})
		errc <- err
	}()
	s.waitFor("the question", s.pr.Showing)
	cancel()
	s.waitFor("the end", func() bool { return len(errc) == 1 })
	wantRoute(t, <-errc, "ask.LoginUICancelled")
	s.waitFor("the question to go", func() bool { return !s.pr.Showing() })
}

func TestClosedPrompterRefusesQuestions(t *testing.T) {
	s := newScreen(t, uitest.Themes()[0])
	go func() { _, _ = s.pr.ConfirmHost()(context.Background(), sshlogin.FirstUse{Address: "h:1"}) }()
	s.waitFor("the question", s.pr.Showing)
	s.pr.Close()
	s.waitFor("the question to go", func() bool { return !s.pr.Showing() })
	ch := make(chan bool, 1)
	go func() { ok, _ := s.pr.ConfirmHost()(context.Background(), sshlogin.FirstUse{Address: "h:2"}); ch <- ok }()
	s.waitFor("the refusal", func() bool { return len(ch) == 1 })
	if <-ch {
		t.Error("a closed prompter trusted a host")
	}
	if s.pr.Showing() {
		t.Error("a question is showing")
	}
}

func TestAQuestionReplacesTheOneOnScreen(t *testing.T) {
	s := newScreen(t, uitest.Themes()[0])
	first := make(chan bool, 1)
	go func() {
		ok, _ := s.pr.ConfirmHost()(context.Background(), sshlogin.FirstUse{Address: "one:1"})
		first <- ok
	}()
	s.waitFor("the first question", func() bool { s.pump(); return s.has("one:1") })
	go func() { _, _ = s.pr.ConfirmHost()(context.Background(), sshlogin.FirstUse{Address: "two:2"}) }()
	s.waitFor("the second question", func() bool { s.pump(); return s.has("two:2") })
	s.waitFor("the first to be refused", func() bool { return len(first) == 1 })
	if <-first {
		t.Error("the replaced question was trusted")
	}
	s.pr.Close()
}

func TestChangedKeyWarning(t *testing.T) {
	err := errors.New("SSHLoginHostKeyChanged", "changed", "address", "192.168.1.9:47123", "pinned", "SHA256:OLD", "presented", "SHA256:NEW")
	host, ok := ChangedFromError(err)
	if !ok || host != (ChangedHost{Address: "192.168.1.9:47123", Pinned: "SHA256:OLD", Presented: "SHA256:NEW"}) {
		t.Fatalf("ChangedFromError = %+v, %v", host, ok)
	}
	if _, ok := ChangedFromError(errors.New("Other", "x")); ok {
		t.Error("another error is not a changed key")
	}
	if _, ok := ChangedFromError(context.Canceled); ok {
		t.Error("a foreign error is not a changed key")
	}

	for _, tc := range []struct {
		button string
		forget bool
	}{{"Forget the Old Key", true}, {"Close", false}} {
		t.Run(tc.button, func(t *testing.T) {
			s := newScreen(t, uitest.Themes()[0])
			var forgot []ChangedHost
			if s.pr.ShowChanged(errors.New("Other", "x"), nil) {
				t.Fatal("it showed a warning for another error")
			}
			if !s.pr.ShowChanged(err, func(h ChangedHost) { forgot = append(forgot, h) }) {
				t.Fatal("no warning")
			}
			s.pump()
			for _, want := range []string{"The key of this computer changed", "SHA256:OLD", "SHA256:NEW", "nothing was sent"} {
				if !s.has(want) {
					t.Errorf("the warning does not say %q", want)
				}
			}
			if s.ts.Exists(gluttertest.ByText("Trust")) {
				t.Error("the warning offers to trust the new key")
			}
			s.ts.Tap(gluttertest.ByText(tc.button))
			s.pump()
			if (len(forgot) == 1) != tc.forget {
				t.Errorf("forget called %d times", len(forgot))
			}
			if s.pr.Showing() {
				t.Error("the warning stays")
			}
		})
	}
}

func TestGoldenTheDialogs(t *testing.T) {
	th := uitest.Themes()[0]
	t.Run("passphrase", func(t *testing.T) {
		s := newScreen(t, th)
		go s.pr.Passphrase("/home/user/.ssh/id_ed25519")(2)
		s.waitFor("the question", s.pr.Showing)
		s.pump()
		s.ts.EnterText(gluttertest.ByType[*ui.InputBox](), "secret")
		s.pump()
		uitest.MatchesGolden(s.ts, "dialog_passphrase_dark")
		s.pr.Close()
	})
	t.Run("first use", func(t *testing.T) {
		s := newScreen(t, th)
		go func() {
			_, _ = s.pr.ConfirmHost()(context.Background(), sshlogin.FirstUse{Address: "192.168.1.9:47123", Type: "ssh-ed25519", Fingerprint: "SHA256:r3iiNRXIdlSVBezAaA0zSYWm4nb/0n7xonM1qm6qiOQ"})
		}()
		s.waitFor("the question", s.pr.Showing)
		s.pump()
		uitest.MatchesGolden(s.ts, "dialog_first_use_dark")
		s.pr.Close()
	})
	t.Run("changed key", func(t *testing.T) {
		s := newScreen(t, th)
		s.pr.ShowChanged(errors.New("SSHLoginHostKeyChanged", "changed", "address", "192.168.1.9:47123",
			"pinned", "SHA256:r3iiNRXIdlSVBezAaA0zSYWm4nb/0n7xonM1qm6qiOQ", "presented", "SHA256:BEX3O+cF6MFz2KetzzzRxhZbIipFFWUMrV7D1iY6l14"), func(ChangedHost) {})
		s.pump()
		uitest.MatchesGolden(s.ts, "dialog_changed_key_dark")
	})
}

func wantRoute(t *testing.T, err error, route string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("error %T (%v) is not an *errors.Error", err, err)
	}
	if !appErr.HasRoute(route) {
		t.Errorf("route = %s, want %s", appErr.Route(), route)
	}
}
