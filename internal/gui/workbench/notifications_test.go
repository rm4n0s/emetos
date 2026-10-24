package workbench

import (
	"io"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/notify"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

var notifySize = ui.Size{Width: 760, Height: 480}

// notifyHarness is a workbench with the notifications of a service over it. The service reads a clock that
// advance moves together with the fake time of the tester.
type notifyHarness struct {
	*harness
	svc *notify.Service
	p   *notify.Presenter
	now time.Time
}

func newNotifyHarness(t *testing.T, colors kit.Colors, tune func(*Options)) *notifyHarness {
	t.Helper()
	n := &notifyHarness{now: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	n.svc = notify.NewService(slog.New(slog.NewTextHandler(io.Discard, nil)), func() time.Time { return n.now })
	n.p = notify.NewPresenter(n.svc)
	n.harness = newHarness(t, notifySize, colors, DefaultLayoutState(), nil, func(o *Options) {
		o.Notifications = n.p
		o.StatusLeft = []Widget{statusText("main")}
		o.StatusRight = []Widget{statusText("Ln 3, Col 14"), statusText("Go")}
		if tune != nil {
			tune(o)
		}
	})
	return n
}

// advance lets time pass for the service and for the tester, in steps so that the timers run in order.
func (n *notifyHarness) advance(d time.Duration) {
	n.t.Helper()
	for d > 0 {
		step := min(d, 500*time.Millisecond)
		n.now = n.now.Add(step)
		n.ts.Pump(step)
		d -= step
	}
}

// texts says whether every text is on screen.
func (n *notifyHarness) has(text string) bool { return n.ts.Exists(gluttertest.ByText(text)) }

// iconButton finds the icon button with a tooltip.
func iconButton(tooltip string) gluttertest.Finder {
	return gluttertest.ByPredicate("icon button "+tooltip, func(w Widget) bool {
		b, ok := w.(*kit.IconButton)
		return ok && b.Tooltip() == tooltip
	})
}

// labelButton finds the button with a label.
func labelButton(label string) gluttertest.Finder {
	return gluttertest.ByPredicate("button "+label, func(w Widget) bool {
		b, ok := w.(*kit.Button)
		return ok && b.Label() == label
	})
}

// yesNo are two buttons.
func yesNo(run func(string)) []notify.Action {
	return []notify.Action{{Label: "Yes", Run: func() { run("Yes") }}, {Label: "No", Run: func() { run("No") }}}
}

func TestToastsShowAtTheBottomRightWithTheNewestOnTop(t *testing.T) {
	n := newNotifyHarness(t, kit.DarkModern(), nil)
	n.svc.Info("", "first")
	n.advance(time.Second)
	n.svc.Warn("", "second")
	n.settle()
	if !n.has("first") || !n.has("second") {
		t.Fatal("the toasts are not on screen")
	}
	first, second := n.ts.TopLeft(gluttertest.ByText("first")), n.ts.TopLeft(gluttertest.ByText("second"))
	if second.Dy >= first.Dy {
		t.Errorf("the newest toast is at y=%v and the older at y=%v: the newest goes on top", second.Dy, first.Dy)
	}
	if len(n.ts.Elements(gluttertest.ByType[*notificationRow]())) != 2 {
		t.Fatal("two rows")
	}
	if got := n.key("notificationToastsVisible"); got != true {
		t.Errorf("notificationToastsVisible = %v", got)
	}
	noErrors(t, n.harness)
}

func TestToastGeometry(t *testing.T) {
	n := newNotifyHarness(t, kit.DarkModern(), nil)
	n.svc.Info("", "only")
	n.settle()
	row := gluttertest.ByType[*notificationRow]()
	tl, size := n.ts.TopLeft(row), n.ts.Size(row)
	// the toast has a border of 1 px around the row: right edge 7 px from the window, bottom 7 px above the status bar
	if got, want := tl.Dx+size.Width, notifySize.Width-7-1; got != want && got != notifySize.Width-7 {
		t.Errorf("the right edge of the toast is at %v, window %v", got, notifySize.Width)
	}
	if got := tl.Dy + size.Height; got > notifySize.Height-StatusBarHeight-6 || got < notifySize.Height-StatusBarHeight-9 {
		t.Errorf("the bottom of the toast is at %v, the status bar starts at %v", got, notifySize.Height-StatusBarHeight)
	}
	if size.Width < 440 || size.Width > 450 {
		t.Errorf("the toast is %v wide", size.Width)
	}
}

func TestAtMostThreeToastsShowAndTheOlderOneWaits(t *testing.T) {
	n := newNotifyHarness(t, kit.DarkModern(), nil)
	var ids []uint64
	for _, m := range []string{"one", "two", "three", "four"} {
		ids = append(ids, n.svc.Info("", m))
		n.advance(time.Second)
	}
	n.settle()
	if n.has("one") || !n.has("two") || !n.has("three") || !n.has("four") {
		t.Errorf("one %v two %v three %v four %v: the newest three show", n.has("one"), n.has("two"), n.has("three"), n.has("four"))
	}
	if len(n.ts.Elements(gluttertest.ByType[*notificationRow]())) != 3 {
		t.Error("three rows")
	}
	n.svc.Dismiss(ids[2])
	n.settle()
	if !n.has("one") || n.has("three") {
		t.Error("the toast that waited shows when one goes away")
	}
	// all of them are in the center
	if len(n.svc.Items()) != 3 {
		t.Errorf("%d items", len(n.svc.Items()))
	}
	noErrors(t, n.harness)
}

func TestFourNotificationsAtOnceGiveThreeToasts(t *testing.T) {
	n := newNotifyHarness(t, kit.DarkModern(), nil)
	for _, m := range []string{"one", "two", "three", "four"} {
		n.svc.Info("", m)
	}
	n.settle()
	if got := len(n.ts.Elements(gluttertest.ByType[*notificationRow]())); got != 3 {
		t.Errorf("%d toasts, want 3 (the spam protection)", got)
	}
}

func TestAnActionRunsAndClosesTheToast(t *testing.T) {
	cases := []struct {
		name     string
		click    string
		keep     bool
		wantRan  []string
		wantOpen bool
	}{
		{"the first button", "Yes", false, []string{"Yes"}, false},
		{"the second button", "No", false, []string{"No"}, false},
		{"a button that keeps it open", "Yes", true, []string{"Yes"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n := newNotifyHarness(t, kit.DarkModern(), nil)
			var ran []string
			actions := yesNo(func(l string) { ran = append(ran, l) })
			actions[0].KeepOpen, actions[1].KeepOpen = tc.keep, tc.keep
			n.svc.Notify(notify.Notification{Message: "Do it?", Actions: actions})
			n.settle()
			if !n.ts.Exists(labelButton("Yes")) || !n.ts.Exists(labelButton("No")) {
				t.Fatal("the buttons are not there")
			}
			if b := n.ts.Element(labelButton("Yes")).Widget().(*kit.Button); b.Secondary() {
				t.Error("the first button is primary")
			}
			if b := n.ts.Element(labelButton("No")).Widget().(*kit.Button); !b.Secondary() {
				t.Error("the second button is secondary")
			}
			n.ts.Tap(labelButton(tc.click))
			n.settle()
			if !slices.Equal(ran, tc.wantRan) {
				t.Errorf("ran %v, want %v", ran, tc.wantRan)
			}
			if got := len(n.svc.Items()) == 1; got != tc.wantOpen {
				t.Errorf("the notification is still there = %v, want %v", got, tc.wantOpen)
			}
			if got := n.has("Do it?"); got != tc.wantOpen {
				t.Errorf("the toast is on screen = %v, want %v", got, tc.wantOpen)
			}
		})
	}
}

func TestAnActionRunsItsCommand(t *testing.T) {
	n := newNotifyHarness(t, kit.DarkModern(), nil)
	n.commands.MustRegister(command.Meta{ID: "test.fix"})
	var got any
	if _, err := n.commands.SetHandler("test.fix", func(_ command.Context, args any) error { got = args; return nil }); err != nil {
		t.Fatal(err)
	}
	n.commands.MustRegister(command.Meta{ID: "test.broken"})
	if _, err := n.commands.SetHandler("test.broken", func(command.Context, any) error { return errors.New("Boom", "it broke") }); err != nil {
		t.Fatal(err)
	}
	n.svc.Notify(notify.Notification{Message: "Fix?", Actions: []notify.Action{{Label: "Fix", Command: "test.fix", Args: "arg"}}})
	n.settle()
	n.ts.Tap(labelButton("Fix"))
	n.settle()
	if got != "arg" {
		t.Errorf("the command got %v", got)
	}
	if len(n.svc.Items()) != 0 {
		t.Error("the notification stays after its action")
	}
	// a command that fails is reported
	n.advance(time.Second)
	n.svc.Notify(notify.Notification{Message: "Break?", Actions: []notify.Action{{Label: "Break", Command: "test.broken"}}})
	n.settle()
	n.ts.Tap(labelButton("Break"))
	n.settle()
	items := n.svc.Items()
	if len(items) != 1 || items[0].Severity != notify.Error || items[0].Message != "Cannot run test.broken: it broke" {
		t.Errorf("items: %+v", items)
	}
	noErrors(t, n.harness)
}

func TestCloseButtonShowsOnHoverAndDismisses(t *testing.T) {
	n := newNotifyHarness(t, kit.DarkModern(), nil)
	n.svc.Info("", "plain")
	n.settle()
	if n.ts.Exists(iconButton("Clear Notification")) {
		t.Fatal("the close button shows without the pointer over the toast")
	}
	n.ts.Hover(n.ts.Center(gluttertest.ByText("plain")))
	n.settle()
	if !n.ts.Exists(iconButton("Clear Notification")) {
		t.Fatal("the close button does not show under the pointer")
	}
	n.ts.Tap(iconButton("Clear Notification"))
	n.settle()
	if len(n.svc.Items()) != 0 || n.has("plain") {
		t.Error("the close button did not dismiss the notification")
	}
	noErrors(t, n.harness)
}

func TestAToastWithAnActionShowsItsCloseButtonAlways(t *testing.T) {
	n := newNotifyHarness(t, kit.DarkModern(), nil)
	n.svc.Notify(notify.Notification{Message: "m", Actions: yesNo(func(string) {})})
	n.settle()
	if !n.ts.Exists(iconButton("Clear Notification")) {
		t.Error("an expanded toast shows its toolbar")
	}
	if n.ts.Exists(iconButton("Collapse Notification")) {
		t.Error("a toast with actions cannot be collapsed")
	}
}

func TestToastsGoAwayAfterTheirTimeout(t *testing.T) {
	cases := []struct {
		name string
		n    notify.Notification
		stay time.Duration
	}{
		{"info", notify.Notification{Severity: notify.Info, Message: "m"}, 10 * time.Second},
		{"warning", notify.Notification{Severity: notify.Warning, Message: "m"}, 12 * time.Second},
		{"error", notify.Notification{Severity: notify.Error, Message: "m"}, 15 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n := newNotifyHarness(t, kit.DarkModern(), nil)
			n.svc.Notify(tc.n)
			n.settle()
			n.advance(tc.stay - time.Second)
			if !n.has("m") {
				t.Fatal("the toast went away early")
			}
			n.advance(2 * time.Second)
			if n.has("m") {
				t.Error("the toast stays after its time")
			}
			if len(n.svc.Items()) != 1 {
				t.Error("the notification is not in the center")
			}
			if n.key("notificationToastsVisible") != false {
				t.Error("notificationToastsVisible is still on")
			}
		})
	}
}

func TestAToastUnderThePointerStays(t *testing.T) {
	n := newNotifyHarness(t, kit.DarkModern(), nil)
	n.svc.Info("", "held")
	n.settle()
	n.ts.Hover(n.ts.Center(gluttertest.ByText("held")))
	n.advance(time.Minute)
	if !n.has("held") {
		t.Fatal("the toast went away under the pointer")
	}
	// the pointer leaves: the toast goes within one timeout
	n.ts.Hover(ui.Offset{Dx: 5, Dy: 100})
	n.advance(notify.PurgeTimeout(notify.Info) + time.Second)
	if n.has("held") {
		t.Error("the toast stays after the pointer left")
	}
}

func TestProgressBarShowsWhileProgressRuns(t *testing.T) {
	n := newNotifyHarness(t, kit.DarkModern(), nil)
	id := n.svc.Notify(notify.Notification{Message: "Working", Progress: &notify.Progress{Total: 100, Worked: 30}})
	n.settle()
	bars := gluttertest.ByType[*kit.ProgressBar]()
	if len(n.ts.Elements(bars)) != 1 {
		t.Fatal("no progress bar")
	}
	if n.ts.Exists(iconButton("Clear Notification")) {
		t.Error("a notification with a running progress cannot be closed")
	}
	// the toast stays while the progress runs
	n.advance(time.Minute)
	if !n.has("Working") {
		t.Fatal("the toast went away with its progress running")
	}
	n.svc.UpdateProgress(id, notify.Progress{Done: true})
	n.settle()
	if n.ts.Exists(bars) {
		t.Error("the progress bar stays after the progress is done")
	}
	n.advance(notify.PurgeTimeout(notify.Info) + time.Second)
	if n.has("Working") {
		t.Error("the toast stays after its progress ended")
	}
	noErrors(t, n.harness)
}

func TestAnInfiniteProgressShowsABar(t *testing.T) {
	n := newNotifyHarness(t, kit.DarkModern(), nil)
	n.svc.Notify(notify.Notification{Message: "Working", Progress: &notify.Progress{Infinite: true}})
	n.settle()
	els := n.ts.Elements(gluttertest.ByType[*kit.ProgressBar]())
	if len(els) != 1 {
		t.Fatalf("%d bars", len(els))
	}
	noErrors(t, n.harness)
}

func TestExpandAndCollapse(t *testing.T) {
	n := newNotifyHarness(t, kit.DarkModern(), nil)
	n.svc.Notify(notify.Notification{Message: "with a source", Source: "Git"})
	n.settle()
	if n.has("Source: Git") {
		t.Fatal("the source shows while the notification is collapsed")
	}
	n.ts.Hover(n.ts.Center(gluttertest.ByText("with a source")))
	n.settle()
	n.ts.Tap(iconButton("Expand Notification"))
	n.settle()
	if !n.has("Source: Git") {
		t.Fatal("the source does not show when it is expanded")
	}
	// an expanded notification is sticky
	n.advance(time.Minute)
	if !n.has("with a source") {
		t.Error("an expanded toast went away")
	}
	n.ts.Tap(iconButton("Collapse Notification"))
	n.settle()
	if n.has("Source: Git") {
		t.Error("the source shows after it was collapsed")
	}
	noErrors(t, n.harness)
}

func TestALongMessageCanBeExpanded(t *testing.T) {
	long := "This is a long message that does not fit on one line of a notification, so it has to be cut with an ellipsis until it is expanded"
	n := newNotifyHarness(t, kit.DarkModern(), nil)
	n.svc.Info("", long)
	n.svc.Info("", "short")
	n.advance(time.Second)
	n.ts.Hover(n.ts.Center(gluttertest.ByText("short")))
	n.settle()
	if n.ts.Exists(iconButton("Expand Notification")) {
		t.Error("a short message cannot be expanded")
	}
	before := n.ts.Size(gluttertest.ByText(long)).Height
	n.ts.Hover(n.ts.Center(gluttertest.ByText(long)))
	n.settle()
	if !n.ts.Exists(iconButton("Expand Notification")) {
		t.Fatal("a long message can be expanded")
	}
	n.ts.Tap(iconButton("Expand Notification"))
	n.settle()
	if after := n.ts.Size(gluttertest.ByText(long)).Height; after <= before {
		t.Errorf("the message is %v high after it was expanded, %v before", after, before)
	}
}

func TestDoNotDisturbHidesToastsExceptErrors(t *testing.T) {
	n := newNotifyHarness(t, kit.DarkModern(), nil)
	n.svc.Info("", "before")
	n.settle()
	if err := n.commands.Run(command.Context{Ctx: t.Context(), Keys: n.keys}, "notifications.toggleDoNotDisturbMode", nil); err != nil {
		t.Fatal(err)
	}
	n.settle()
	if n.has("before") {
		t.Error("turning do not disturb on leaves a toast")
	}
	n.advance(2 * time.Second)
	n.svc.Info("", "an info")
	n.svc.Warn("", "a warning")
	n.svc.Error("", "an error")
	n.settle()
	if n.has("an info") || n.has("a warning") || !n.has("an error") {
		t.Errorf("info %v warning %v error %v", n.has("an info"), n.has("a warning"), n.has("an error"))
	}
	if len(n.svc.Items()) != 4 {
		t.Errorf("%d notifications in the center, want 4", len(n.svc.Items()))
	}
	n.run("notifications.toggleDoNotDisturbMode")
	n.advance(2 * time.Second)
	n.svc.Info("", "again")
	n.settle()
	if !n.has("again") {
		t.Error("toasts are back when do not disturb is off")
	}
}

func TestCenterShowsAllNotificationsNewestFirst(t *testing.T) {
	n := newNotifyHarness(t, kit.DarkModern(), nil)
	n.svc.Info("", "one")
	n.advance(time.Second)
	n.svc.Warn("Git", "two")
	n.advance(time.Second)
	n.svc.Error("", "three")
	n.advance(time.Second)
	n.svc.Info("", "four")
	n.settle()
	n.run("notifications.showList")
	if n.key("notificationCenterVisible") != true || n.key("notificationToastsVisible") != false {
		t.Errorf("keys: center %v toasts %v", n.key("notificationCenterVisible"), n.key("notificationToastsVisible"))
	}
	for _, m := range []string{"one", "two", "three", "four", "NOTIFICATIONS"} {
		if !n.has(m) {
			t.Errorf("%q is not in the center", m)
		}
	}
	ys := []float64{}
	for _, m := range []string{"four", "three", "two", "one"} {
		ys = append(ys, n.ts.TopLeft(gluttertest.ByText(m)).Dy)
	}
	if !slices.IsSorted(ys) {
		t.Errorf("the rows are not newest first: %v", ys)
	}
	// no toast while the center is open
	n.advance(2 * time.Second)
	n.svc.Info("", "five")
	n.settle()
	if got := len(n.ts.Elements(gluttertest.ByType[*notificationRow]())); got != 5 {
		t.Errorf("%d rows in the center, want 5", got)
	}
	n.run("notifications.hideList")
	if n.has("five") || n.key("notificationCenterVisible") != false {
		t.Error("the center did not close")
	}
	noErrors(t, n.harness)
}

func TestEmptyCenter(t *testing.T) {
	n := newNotifyHarness(t, kit.DarkModern(), nil)
	n.run("notifications.showList")
	if !n.has("NO NEW NOTIFICATIONS") || n.has("NOTIFICATIONS") {
		t.Error("the header of an empty center says there is nothing")
	}
	if n.ts.Exists(gluttertest.ByType[*notificationRow]()) {
		t.Error("an empty center has rows")
	}
	if !n.ts.Exists(iconButton("Clear All Notifications")) || !n.ts.Exists(iconButton("Enable Do Not Disturb Mode")) || !n.ts.Exists(iconButton("Hide Notification Center")) {
		t.Error("the header of the center has its three buttons")
	}
	noErrors(t, n.harness)
}

func TestCenterClearAllDismissesAndCloses(t *testing.T) {
	n := newNotifyHarness(t, kit.DarkModern(), nil)
	n.svc.Info("", "one")
	n.svc.Error("", "two")
	n.run("notifications.showList")
	n.ts.Tap(iconButton("Clear All Notifications"))
	n.settle()
	if len(n.svc.Items()) != 0 || n.p.CenterVisible() {
		t.Errorf("%d items, center visible %v", len(n.svc.Items()), n.p.CenterVisible())
	}

	// the command does the same
	n.advance(time.Second)
	n.svc.Info("", "three")
	n.run("notifications.showList")
	n.run("notifications.clearAll")
	if len(n.svc.Items()) != 0 || n.p.CenterVisible() {
		t.Error("the command did not clear and close")
	}
	noErrors(t, n.harness)
}

func TestClearAllLeavesNothingToClearWhenOnlyProgressIsLeft(t *testing.T) {
	n := newNotifyHarness(t, kit.DarkModern(), nil)
	n.svc.Notify(notify.Notification{Message: "working", Progress: &notify.Progress{Infinite: true}})
	n.run("notifications.showList")
	before := len(n.svc.Items())
	n.ts.Tap(iconButton("Clear All Notifications"))
	n.settle()
	if len(n.svc.Items()) != before {
		t.Error("the disabled button cleared a notification that has a running progress")
	}
}

func TestCenterDoNotDisturbToggle(t *testing.T) {
	n := newNotifyHarness(t, kit.DarkModern(), nil)
	n.run("notifications.showList")
	n.ts.Tap(iconButton("Enable Do Not Disturb Mode"))
	n.settle()
	if !n.svc.DoNotDisturb() {
		t.Fatal("the button did not turn do not disturb on")
	}
	if !n.ts.Exists(iconButton("Disable Do Not Disturb Mode")) {
		t.Error("the button does not say it turns it off now")
	}
	n.ts.Tap(iconButton("Disable Do Not Disturb Mode"))
	n.settle()
	if n.svc.DoNotDisturb() {
		t.Error("the button did not turn it off")
	}
}

func TestCenterHideButtonAndRowButtons(t *testing.T) {
	n := newNotifyHarness(t, kit.DarkModern(), nil)
	var ran []string
	n.svc.Notify(notify.Notification{Message: "ask", Actions: yesNo(func(l string) { ran = append(ran, l) })})
	n.svc.Info("", "plain")
	n.run("notifications.showList")
	// the buttons of an item work in the center too
	n.ts.Tap(labelButton("No"))
	n.settle()
	if !slices.Equal(ran, []string{"No"}) || len(n.svc.Items()) != 1 {
		t.Errorf("ran %v, %d items", ran, len(n.svc.Items()))
	}
	n.ts.Tap(iconButton("Hide Notification Center"))
	n.settle()
	if n.p.CenterVisible() {
		t.Error("the hide button did not close the center")
	}
}

func TestCommandsToggleTheCenterAndHideToasts(t *testing.T) {
	n := newNotifyHarness(t, kit.DarkModern(), nil)
	n.svc.Info("", "toast")
	n.settle()
	n.run("notifications.hideToasts")
	if n.has("toast") || n.key("notificationToastsVisible") != false || len(n.svc.Items()) != 1 {
		t.Error("hideToasts leaves the notification in the center only")
	}
	n.run("notifications.toggleList")
	if !n.p.CenterVisible() {
		t.Error("toggleList opens")
	}
	n.run("notifications.toggleList")
	if n.p.CenterVisible() {
		t.Error("toggleList closes")
	}
}

func TestFocusToastsFocusesTheNewestToast(t *testing.T) {
	n := newNotifyHarness(t, kit.DarkModern(), nil)
	n.svc.Info("", "older")
	n.advance(time.Second)
	n.svc.Notify(notify.Notification{Message: "newer", Actions: yesNo(func(string) {})})
	n.settle()
	if n.key("notificationFocus") == true {
		t.Fatal("a notification has the focus already")
	}
	n.run("notifications.focusToasts")
	if n.key("notificationFocus") != true {
		t.Error("notificationFocus is off after focusing the toasts")
	}
	if node := widgetsFocus(n.ts.Element(gluttertest.ByText("newer"))); node == nil || node.Parent() == nil {
		t.Error("nothing has the focus")
	}
	// a toast that has the focus does not go away
	n.advance(time.Minute)
	if !n.has("newer") {
		t.Error("a toast with the focus went away")
	}
	// no toast: nothing happens
	n.run("notifications.hideToasts")
	n.run("notifications.focusToasts")
}

func TestTheHostGivesTheCommandsBack(t *testing.T) {
	n := newNotifyHarness(t, kit.DarkModern(), nil)
	for _, id := range []string{"notifications.showList", "notifications.hideList", "notifications.toggleList", "notifications.hideToasts",
		"notifications.focusToasts", "notifications.clearAll", "notifications.toggleDoNotDisturbMode"} {
		if !n.commands.HasHandler(id) {
			t.Errorf("%s has no handler", id)
		}
	}
	n.ts.PumpWidget(gluttertestEmpty())
	n.settle()
	if n.commands.HasHandler("notifications.showList") {
		t.Error("the handler stays after the host is gone")
	}
}

func TestAHostWithoutTheCommandsReportsTheMistake(t *testing.T) {
	// a registry that has the commands of the layout but not those of the notifications
	reg := command.NewRegistry()
	for _, m := range command.DefaultRegistry().All() {
		if strings.HasPrefix(m.ID, "workbench.") {
			reg.MustRegister(m)
		}
	}
	n := newNotifyHarness(t, kit.DarkModern(), func(o *Options) { o.Commands = reg })
	errs := n.ts.TakeErrors()
	var found bool
	for _, err := range errs {
		var appErr *errors.Error
		if errors.As(err, &appErr) && appErr.HasRoute("notificationsHostState.bindCommands.NotificationCommandMissing") {
			found = true
			if _, ok := appErr.Metadata["command"]; !ok {
				t.Errorf("the error does not say which command: %v", appErr.Metadata)
			}
		}
	}
	if !found {
		t.Errorf("no error has the tag NotificationCommandMissing: %v", errs)
	}
}

func TestNotificationsThatCameBeforeTheWindowShow(t *testing.T) {
	n := &notifyHarness{now: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	n.svc = notify.NewService(slog.New(slog.NewTextHandler(io.Discard, nil)), func() time.Time { return n.now })
	n.svc.Error("Emetos", "Cannot open a.txt")
	n.p = notify.NewPresenter(n.svc)
	n.harness = newHarness(t, notifySize, kit.DarkModern(), DefaultLayoutState(), nil, func(o *Options) { o.Notifications = n.p })
	if !n.has("Cannot open a.txt") {
		t.Error("a notification that came before the window shows when the window is up")
	}
}

func TestGoldenNotifications(t *testing.T) {
	for _, th := range goldenThemes {
		t.Run(th.name+" toasts", func(t *testing.T) {
			n := newNotifyHarness(t, th.colors(), nil)
			n.svc.Notify(notify.Notification{Message: "Info with actions", Actions: yesNo(func(string) {})})
			n.advance(time.Second)
			n.svc.Error("", "An error")
			n.advance(time.Second)
			n.svc.Notify(notify.Notification{Severity: notify.Warning, Source: "Language", Message: "Indexing the workspace", Progress: &notify.Progress{Total: 100, Worked: 40}})
			n.settle()
			testutil.MatchesGolden(n.ts, "notifications_toasts_"+th.name)
			noErrors(t, n.harness)
		})
		t.Run(th.name+" center", func(t *testing.T) {
			n := newNotifyHarness(t, th.colors(), nil)
			n.svc.Notify(notify.Notification{Message: "Info with actions", Actions: yesNo(func(string) {})})
			n.advance(time.Second)
			n.svc.Notify(notify.Notification{Severity: notify.Warning, Source: "Language", Message: "A warning with a source, expanded"})
			n.advance(time.Second)
			n.svc.Error("", "An error that has a message so long that it does not fit on one line of the notification center")
			n.advance(time.Second)
			n.svc.Notify(notify.Notification{Message: "Indexing the workspace", Progress: &notify.Progress{Total: 100, Worked: 40}})
			n.settle()
			items := n.svc.Items()
			n.svc.SetExpanded(items[1].ID, true)
			n.run("notifications.showList")
			testutil.MatchesGolden(n.ts, "notifications_center_"+th.name)
			noErrors(t, n.harness)
		})
		t.Run(th.name+" empty center", func(t *testing.T) {
			n := newNotifyHarness(t, th.colors(), nil)
			n.run("notifications.showList")
			testutil.MatchesGolden(n.ts, "notifications_center_empty_"+th.name)
			noErrors(t, n.harness)
		})
	}
}
