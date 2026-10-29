package redis

import (
	"strings"
	"testing"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extension/ui/uitest"
)

// notifyingView is a view handle whose Refresh also builds the widget under test again.
type notifyingView struct {
	extension.ViewHandle
	changed *foundation.ChangeNotifier
}

// Refresh implements extension.ViewHandle.
func (v notifyingView) Refresh() {
	v.ViewHandle.Refresh()
	v.changed.NotifyListeners()
}

// showPanel shows a tab of the panel in a window; it builds again when the extension refreshes the view.
func (r *rig) showPanel(th uitest.Theme, view string, w, h float64) *gluttertest.Tester {
	r.t.Helper()
	changed := foundation.NewChangeNotifier()
	r.ext.panelTab = view
	r.ext.views.panel = notifyingView{r.ext.views.panel, changed}
	body := widgets.NewListenableBuilder(changed, func(bc widgets.BuildContext, _ widgets.Widget) widgets.Widget { return r.ext.buildPanel(bc) }, nil)
	return uitest.Pump(r.t, th, gui.Size{Width: w, Height: h}, body)
}

// showInfo shows the server information tab in a window.
func (r *rig) showInfo(th uitest.Theme, tab *infoTab, w, h float64) *gluttertest.Tester {
	r.t.Helper()
	changed := foundation.NewChangeNotifier()
	tab.tab = refreshTab{tab.tab, changed}
	body := widgets.NewListenableBuilder(changed, func(bc widgets.BuildContext, _ widgets.Widget) widgets.Widget { return tab.build(bc) }, nil)
	return uitest.Pump(r.t, th, gui.Size{Width: w, Height: h}, body)
}

// consoleWithReplies is a console that ran a few commands.
func (r *rig) consoleWithReplies() *console {
	c := r.openConsole()
	for _, line := range []string{"PING", `GET greeting`, "HGETALL user:42:profile", "SMEMBERS user:42:tags", "GET user:7:profile", "DBSIZE"} {
		r.run(c, line)
	}
	return c
}

func TestTheConsoleTabHasAnInputAndShowsTheReplies(t *testing.T) {
	store := fixtureServer()
	store.resp3 = true
	r := newRig(t, store)
	c := r.consoleWithReplies()
	ts := r.showPanel(uitest.Themes()[0], ConsoleViewID, 900, 760)
	if !ts.Exists(gluttertest.ByTextContaining("db0> HGETALL user:42:profile", gluttertest.FindOpts{FindRichText: true})) {
		t.Error("the command is not echoed")
	}
	for _, want := range []string{`1# "name" => "Ada"`, "1~ \"go\"", "(error) WRONGTYPE", "(integer) 9", "Run", "Clear", "Copy", "Cancel"} {
		if !hasText(ts, want) {
			t.Errorf("no %q on screen", want)
		}
	}
	// typing a command and pressing Run
	ts.EnterText(gluttertest.ByType[*ui.InputBox](), "ECHO hi")
	ts.Pump()
	if c.input.Value() != "ECHO hi" {
		t.Fatalf("input = %q", c.input.Value())
	}
	ts.Tap(gluttertest.At(gluttertest.ByTextContaining("Run"), 0))
	r.api.WaitFor("the command", func() bool { return !c.busy() })
	ts.Pump()
	if c.input.Value() != "" || !hasText(ts, `"hi"`) {
		t.Errorf("input %q, reply on screen %v", c.input.Value(), hasText(ts, `"hi"`))
	}
	// Clear empties the list
	ts.Tap(gluttertest.At(gluttertest.ByTextContaining("Clear"), 0))
	ts.Pump()
	if len(c.entries) != 0 || !hasText(ts, "Type a command below") {
		t.Errorf("%d entries after Clear", len(c.entries))
	}
}

func TestTheConsoleOffersTheCommandsThatStartWithWhatIsTyped(t *testing.T) {
	r := newRig(t, nil)
	c := r.openConsole()
	r.ext.ensureCommandDocs(r.conn.ID)
	r.api.WaitFor("the commands", func() bool { return len(r.ext.commandDocs(r.conn.ID)) == len(fakeDocs) })
	ts := r.showPanel(uitest.Themes()[0], ConsoleViewID, 900, 300)
	ts.EnterText(gluttertest.ByType[*ui.InputBox](), "get")
	ts.Pump()
	if !hasText(ts, "GETDEL") || !hasText(ts, "GETEX") {
		t.Fatalf("no suggestions on screen for %q", c.input.Value())
	}
	ts.Tap(gluttertest.ByTextContaining("GETDEL"))
	ts.Pump()
	if c.input.Value() != "GETDEL " {
		t.Errorf("input = %q", c.input.Value())
	}
}

func TestTheConsoleTabWithoutAConnectionSaysSo(t *testing.T) {
	r := newRig(t, nil)
	for _, view := range []string{ConsoleViewID, PubSubViewID, MonitorViewID} {
		ts := r.showPanel(uitest.Themes()[0], view, 700, 200)
		if !hasText(ts, "No Redis connection is open") {
			t.Errorf("%s: no message", view)
		}
	}
}

func TestThePickerListsTheOpenConnections(t *testing.T) {
	r := newRig(t, nil)
	c := r.openConsole()
	ts := r.showPanel(uitest.Themes()[0], ConsoleViewID, 900, 300)
	if !hasText(ts, "cache") {
		t.Error("the connection is not named")
	}
	_ = c
	if err := r.hub.Disconnect(r.conn.ID); err != nil {
		t.Fatal(err)
	}
	r.api.Pump()
	ts.Pump()
	if !hasText(ts, "No Redis connection is open") {
		t.Error("the tab does not follow the connection that closed")
	}
}

func TestThePubSubTabShowsTheMessages(t *testing.T) {
	r := newRig(t, nil)
	tab := r.openPubSub()
	r.subscribe(tab, "news", false)
	r.store.publish("news", "first")
	r.store.publish("news", "second")
	r.waitMessages(tab, 2)
	ts := r.showPanel(uitest.Themes()[0], PubSubViewID, 900, 360)
	for _, want := range []string{"2 messages", "Subscribe", "PSubscribe", "Publish", "Pause", "Clear", "news", "second"} {
		if !hasText(ts, want) {
			t.Errorf("no %q on screen", want)
		}
	}
	ts.Tap(gluttertest.ByTextContaining("Pause"))
	ts.Pump()
	if !tab.paused || !hasText(ts, "paused") || !hasText(ts, "Resume") {
		t.Errorf("paused = %v", tab.paused)
	}
	// subscribing from the tab
	ts.EnterText(gluttertest.At(gluttertest.ByType[*ui.InputBox](), 0), "alerts")
	ts.Tap(gluttertest.At(gluttertest.ByText("Subscribe"), 0))
	r.api.WaitFor("the subscription", func() bool { return len(tab.items) == 2 })
	ts.Pump()
	if !hasText(ts, "alerts") {
		t.Error("the subscription is not listed")
	}
}

func TestTheMonitorTabShowsTheCommandsAndTheWarning(t *testing.T) {
	r := newRig(t, nil)
	tab := r.openMonitor()
	ts := r.showPanel(uitest.Themes()[0], MonitorViewID, 900, 360)
	for _, want := range []string{"slows it down", "Stopped", "Start"} {
		if !hasText(ts, want) {
			t.Errorf("no %q on screen", want)
		}
	}
	tab.start()
	r.api.WaitFor("the monitor", func() bool { return tab.running() })
	r.store.pushMonitor(monitorLine(1700000000.5, 0, "127.0.0.1:50001", `"set" "session:1" "x"`))
	r.api.WaitFor("a line", func() bool { return tab.lines.len() == 1 })
	ts.Pump()
	for _, want := range []string{"Running", "1 command", "Stop", `"set" "session:1" "x"`, "127.0.0.1:50001"} {
		if !hasText(ts, want) {
			t.Errorf("no %q on screen", want)
		}
	}
}

func TestTheServerInformationTabDrawsItsGridAndViews(t *testing.T) {
	r := newRig(t, nil)
	tab := r.openInfo()
	ts := r.showInfo(uitest.Themes()[0], tab, 900, 420)
	for _, want := range []string{"cache", "Info", "Slow Log", "Clients", "Memory", "redis_version", "8.2.2", "Section"} {
		if !hasText(ts, want) {
			t.Errorf("no %q on screen", want)
		}
	}
	ts.Tap(gluttertest.At(gluttertest.ByText("Clients"), 0))
	r.api.WaitFor("the clients", func() bool { return tab.view == infoViewClients && !tab.loading })
	ts.Pump()
	if !hasText(ts, "127.0.0.1:50000") || !strings.Contains(r.store.commands()[len(r.store.commands())-1], "CLIENT LIST") {
		t.Error("the clients are not shown")
	}
	if err := r.hub.Disconnect(r.conn.ID); err != nil {
		t.Fatal(err)
	}
	r.api.Pump()
	tab.tab.Refresh()
	ts.Pump()
	if !hasText(ts, "The connection is closed") {
		t.Error("a closed connection is not said")
	}
}
