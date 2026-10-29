package terminal

import (
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// panelRig shows the whole view of the panel (the terminals, the tabs list, the menu) over the fake API.
type panelRig struct {
	*extRig
	ts      *gluttertest.Tester
	rebuild func()
	seen    int
}

// newPanelRig activates the extension with the test shell and shows its view in a window of the tests.
func newPanelRig(t *testing.T, th uitest.Theme, size gui.Size) *panelRig {
	t.Helper()
	return newPanelRigAt(t, th, size, t.TempDir(), nil)
}

// newPanelRigAt is newPanelRig in a folder that was used before, with the fake API prepared by before (the state of an earlier
// session, say).
func newPanelRigAt(t *testing.T, th uitest.Theme, size gui.Size, dir string, before func(*fakeapi.API)) *panelRig {
	t.Helper()
	r := &panelRig{extRig: newExtRigAt(t, dir, before, "")}
	root := widgets.NewStatefulBuilder(func(bc widgets.BuildContext, set widgets.StateSetter) widgets.Widget {
		r.rebuild = func() { set(func() {}) }
		return r.ext.build(bc)
	})
	r.ts = uitest.Pump(t, th, size, root)
	r.settle()
	return r
}

// settle runs what the extension posted and the frames that follow, and builds the view again when the extension asked for it.
func (r *panelRig) settle() {
	for range 8 {
		r.api.Pump()
		if n, _, _ := r.view().Snapshot(); n != r.seen {
			r.seen = n
			r.rebuild()
		}
		r.ts.FlushPosted()
		r.ts.Pump(20 * time.Millisecond)
	}
}

func (r *panelRig) view() *viewSnapshot { return &viewSnapshot{r} }

// viewSnapshot reads the numbers of the view.
type viewSnapshot struct{ r *panelRig }

func (v *viewSnapshot) Snapshot() (int, int, string) { return v.r.api.View(ViewID).Snapshot() }

// waitShows waits until every terminal shows the text.
func (r *panelRig) waitShows(t *testing.T, text string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		r.settle()
		all := true
		for _, e := range r.ext.all() {
			if !strings.Contains(allText(e.term), text) {
				all = false
			}
		}
		if all {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("not every terminal shows %q", text)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestAViewThatHasNoTerminalMakesOne(t *testing.T) {
	r := newPanelRig(t, uitest.Themes()[0], gui.Size{Width: 900, Height: 300})
	r.settle()
	r.waitShows(t, "ready")
	if r.ext.count() != 1 {
		t.Fatalf("%d terminals", r.ext.count())
	}
	if reveals := r.api.View(ViewID).Reveals; len(reveals) == 0 || !reveals[len(reveals)-1] {
		t.Errorf("the first terminal did not take the focus: %v", reveals)
	}
}

func TestGoldenTheTabsListAndASplit(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := newPanelRig(t, th, gui.Size{Width: 1000, Height: 300})
			r.waitShows(t, "ready")
			r.run(CmdNew, nil)
			r.run(CmdSplit, nil)
			r.waitShows(t, "ready")
			for i, name := range []string{"build", "server", "logs"} {
				r.ext.all()[i].term.SetName(name)
			}
			r.ext.all()[1].term.Send([]byte("colors\r"))
			r.ext.all()[2].term.Send([]byte("echo a split terminal\r"))
			waitText(t, r.ext.all()[1].term, "truecolor")
			waitText(t, r.ext.all()[2].term, "a split terminal")
			time.Sleep(100 * time.Millisecond)
			r.api.Pump()
			r.settle()
			r.ts.Screenshot()
			uitest.MatchesGolden(r.ts, "panel_tabs_split_"+th.Name)
		})
	}
}

func TestGoldenTheTabsListWithIconsColorsAndAMove(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := newPanelRig(t, th, gui.Size{Width: 1000, Height: 260})
			r.waitShows(t, "ready")
			r.run(CmdNew, nil)
			r.run(CmdNew, nil)
			r.run(CmdNew, nil)
			r.waitShows(t, "ready")
			es := r.ext.panelEntries()
			for i, name := range []string{"build", "server", "logs", "moved to a tab"} {
				es[i].term.SetName(name)
			}
			r.ext.setColor(es[0], "terminal.ansiRed")
			r.ext.setIcon(es[1], "bug")
			r.ext.setColor(es[1], "terminal.ansiGreen")
			r.ext.setIcon(es[2], "rocket")
			r.ext.setColor(es[2], "terminal.ansiBlue")
			es[0].term.Send([]byte("colors\r"))
			waitText(t, es[0].term, "truecolor")
			r.run(CmdFocusPrevious, nil)
			r.settle()
			// the active tab is the first; the last one moves to an editor tab and leaves the list
			r.ext.moveToEditor(es[3])
			r.settle()
			r.ext.focusEntry(es[0], false)
			r.settle()
			r.ts.Screenshot()
			uitest.MatchesGolden(r.ts, "panel_tabs_icons_"+th.Name)
		})
	}
}

func TestGoldenASessionBeforeExitAndAfterStart(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			size := gui.Size{Width: 1000, Height: 280}
			dir := t.TempDir()
			r := newPanelRigAt(t, th, size, dir, nil)
			r.waitShows(t, "ready")
			r.run(CmdNew, nil)
			r.waitShows(t, "ready")
			es := r.ext.panelEntries()
			es[0].term.SetName("build")
			es[1].term.SetName("server")
			r.ext.setColor(es[1], "terminal.ansiMagenta")
			es[0].term.Send([]byte("echo make all\rcolors\r"))
			es[1].term.Send([]byte("echo listening on 8080\r"))
			waitText(t, es[0].term, "truecolor")
			waitText(t, es[1].term, "listening on 8080\n$")
			r.ext.focusEntry(es[1], false)
			r.settle()
			r.ts.Screenshot()
			uitest.MatchesGolden(r.ts, "session_before_exit_"+th.Name)
			s := r.save()
			after := newPanelRigAt(t, th, size, dir, func(api *fakeapi.API) { api.SetViewState(ViewID, s.view) })
			after.waitShows(t, "History restored\nready")
			after.ts.Screenshot()
			uitest.MatchesGolden(after.ts, "session_after_start_"+th.Name)
		})
	}
}
