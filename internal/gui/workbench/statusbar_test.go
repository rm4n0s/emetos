package workbench

import (
	"slices"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// The tests of the widget of the status bar (step 6.20): the items of the registry, their commands, hovers and
// colors, and what happens when the bar is too narrow.

// statusHarness is a workbench with a status bar of a registry. The commands it runs are recorded.
type statusHarness struct {
	*harness
	bar   *StatusBar
	ran   []string
	args  []any
	fails map[string]error
}

func newStatusHarness(t *testing.T, size ui.Size, colors kit.Colors, noFolder bool, fill func(*StatusBar)) *statusHarness {
	t.Helper()
	sh := &statusHarness{bar: NewStatusBar(), fails: map[string]error{}}
	if fill != nil {
		fill(sh.bar)
	}
	sh.harness = newHarness(t, size, colors, DefaultLayoutState(), nil, func(o *Options) {
		o.NoFolder = noFolder
		o.StatusBar = &StatusBarOptions{
			Bar: sh.bar,
			Keybinding: func(id string) string {
				if id == "test.keyed" {
					return "Ctrl+G"
				}
				return ""
			},
			OnError: func(id string, err error) { sh.fails[id] = err },
		}
		for _, id := range []string{"test.click", "test.keyed", "test.args", "test.fail"} {
			o.Commands.MustRegister(command.Meta{ID: id})
			id := id
			if _, err := o.Commands.SetHandler(id, func(_ command.Context, args any) error {
				sh.ran = append(sh.ran, id)
				sh.args = append(sh.args, args)
				if id == "test.fail" {
					return errors.New("StatusTestCommandFailed", "it failed")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		}
	})
	return sh
}

func (sh *statusHarness) item(id string) gluttertest.Finder {
	return gluttertest.ByKey(StatusItemKey(id))
}

func (sh *statusHarness) click(id string) {
	sh.t.Helper()
	sh.ts.TapAt(sh.ts.Center(sh.item(id)), gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse})
	sh.settle()
}

func (sh *statusHarness) rectOf(id string) ui.Rect {
	f := sh.item(id)
	tl, sz := sh.ts.TopLeft(f), sh.ts.Size(f)
	return ui.NewRectFromLTWH(tl.Dx, tl.Dy, sz.Width, sz.Height)
}

var testSize960 = ui.Size{Width: 960, Height: 400}

func TestStatusBarItemsAreOrderedAndAligned(t *testing.T) {
	sh := newStatusHarness(t, testSize960, kit.DarkModern(), false, func(b *StatusBar) {
		b.Add("left.low", StatusLeft, 1, StatusEntry{Text: "low"})
		b.Add("left.high", StatusLeft, 50, StatusEntry{Text: "high"})
		b.Add("right.first", StatusRight, 100.5, StatusEntry{Text: "Ln 1, Col 1"})
		b.Add("right.second", StatusRight, 100.1, StatusEntry{Text: "Go"})
		b.Add("right.bell", StatusRight, BellPriority(), StatusEntry{Text: "$(bell)"})
	})
	bar := sh.rect(PartStatusBar)
	if bar.Height() != StatusBarHeight {
		t.Errorf("the bar is %v high", bar.Height())
	}
	high, low := sh.rectOf("left.high"), sh.rectOf("left.low")
	first, second, bell := sh.rectOf("right.first"), sh.rectOf("right.second"), sh.rectOf("right.bell")
	if !(high.Left < low.Left && high.Left <= bar.Left+statusEdgeInset+0.5) {
		t.Errorf("left items: high %v low %v", high, low)
	}
	if !(first.Right <= second.Left+0.5 && second.Right <= bell.Left+0.5) {
		t.Errorf("right items: %v %v %v", first, second, bell)
	}
	if bell.Right < bar.Right-statusEdgeInset-0.5 || bell.Right > bar.Right+0.5 {
		t.Errorf("the last item ends at %v, the bar at %v", bell.Right, bar.Right)
	}
	for _, id := range []string{"left.high", "left.low", "right.first", "right.second", "right.bell"} {
		if r := sh.rectOf(id); r.Height() != StatusBarHeight || r.Top != bar.Top {
			t.Errorf("%s is %v high at %v", id, r.Height(), r.Top)
		}
	}
	noErrors(t, sh.harness)
}

func TestStatusBarItemsSpaceAndPadding(t *testing.T) {
	sh := newStatusHarness(t, testSize960, kit.DarkModern(), false, func(b *StatusBar) {
		b.Add("a", StatusLeft, 3, StatusEntry{Text: "aaaa"})
		b.Add("b", StatusLeft, 2, StatusEntry{Text: "bbbb"})
		b.Add("c", StatusLeft, 1, StatusEntry{Text: "cccc"})
	})
	// the box of an item with a hover background: the first item reaches the corner (8 px padding), the
	// others have 3 px margins around a box with 5 px padding, so the items follow each other
	box := func(id string) ui.Size {
		return sh.ts.Size(gluttertest.Descendant(sh.item(id), gluttertest.ByType[*widgets.Container]()))
	}
	a, b, c := sh.rectOf("a"), sh.rectOf("b"), sh.rectOf("c")
	if b.Left != a.Right || c.Left != b.Right {
		t.Errorf("items are not side by side: %v %v %v", a, b, c)
	}
	if a.Width() != b.Width() {
		t.Errorf("same text and 8 px each side: widths %v %v", a.Width(), b.Width())
	}
	if d := box("a").Width - box("b").Width; d != 2*(statusEdgePadding-statusItemPadding) {
		t.Errorf("the box of the first item is %v wider than another's", d)
	}
	if box("b").Width != box("c").Width {
		t.Errorf("boxes %v %v", box("b"), box("c"))
	}
	if box("b").Height != StatusBarHeight {
		t.Errorf("the box is %v high", box("b").Height)
	}
}

func TestClickingAnItemRunsItsCommandWithItsArguments(t *testing.T) {
	sh := newStatusHarness(t, testSize960, kit.DarkModern(), false, func(b *StatusBar) {
		b.Add("plain", StatusLeft, 3, StatusEntry{Text: "plain"})
		b.Add("click", StatusLeft, 2, StatusEntry{Text: "click", Command: "test.click"})
		b.Add("args", StatusRight, 2, StatusEntry{Text: "args", Command: "test.args", Args: map[string]any{"n": 3.0}})
		b.Add("fail", StatusRight, 1, StatusEntry{Text: "fail", Command: "test.fail"})
	})
	sh.click("plain")
	if len(sh.ran) != 0 {
		t.Fatalf("an item with no command ran %v", sh.ran)
	}
	sh.click("click")
	sh.click("args")
	if !slices.Equal(sh.ran, []string{"test.click", "test.args"}) {
		t.Fatalf("ran %v", sh.ran)
	}
	if m, ok := sh.args[1].(map[string]any); !ok || m["n"] != 3.0 {
		t.Errorf("arguments %v", sh.args[1])
	}
	// a command that fails is reported
	sh.click("fail")
	if sh.fails["test.fail"] == nil {
		t.Error("a failing command was not reported")
	}
	noErrors(t, sh.harness)
}

func TestAnItemFollowsTheRegistry(t *testing.T) {
	sh := newStatusHarness(t, testSize960, kit.DarkModern(), false, nil)
	acc := sh.bar.Add("x", StatusRight, 1, StatusEntry{Text: "one", Command: "test.click"})
	sh.settle()
	before := sh.rectOf("x").Width()
	acc.Update(StatusEntry{Text: "a much longer text", Command: "test.click"})
	sh.settle()
	if sh.rectOf("x").Width() <= before {
		t.Error("the item did not grow with its text")
	}
	if !sh.ts.Exists(gluttertest.ByText("a much longer text")) {
		t.Error("the new text is not on screen")
	}
	acc.SetVisible(false)
	sh.settle()
	if sh.ts.Exists(sh.item("x")) {
		t.Error("a hidden item is on screen")
	}
	acc.SetVisible(true)
	acc.Update(StatusEntry{Text: ""})
	sh.settle()
	if sh.ts.Exists(sh.item("x")) {
		t.Error("an item with no text is on screen")
	}
	acc.Update(StatusEntry{Text: "back"})
	sh.settle()
	if !sh.ts.Exists(sh.item("x")) {
		t.Error("the item did not come back")
	}
	acc.Dispose()
	sh.settle()
	if sh.ts.Exists(sh.item("x")) {
		t.Error("a disposed item is on screen")
	}
}

func TestItemsShowIconsAndText(t *testing.T) {
	sh := newStatusHarness(t, testSize960, kit.DarkModern(), false, func(b *StatusBar) {
		b.Add("problems", StatusLeft, 50, StatusEntry{Text: "$(error) 0 $(warning) 2"})
		b.Add("busy", StatusRight, 1, StatusEntry{Text: "\\$(bell) text", ShowProgress: true})
		b.Add("unknown", StatusRight, 2, StatusEntry{Text: "$(no-such-icon)"})
	})
	if !sh.ts.Exists(gluttertest.ByText(" 0 ")) || !sh.ts.Exists(gluttertest.ByText(" 2")) {
		t.Error("the text between the icons is not on screen")
	}
	if n := sh.ts.Count(gluttertest.Descendant(sh.item("problems"), gluttertest.ByType[*widgets.Icon]())); n != 2 {
		t.Errorf("the problems item has %d icons, want 2", n)
	}
	if n := sh.ts.Count(gluttertest.Descendant(sh.item("busy"), gluttertest.ByType[*widgets.Icon]())); n != 1 {
		t.Errorf("the busy item has %d icons: the loading one", n)
	}
	if !sh.ts.Exists(gluttertest.ByText("$(bell) text")) {
		t.Error("an escaped icon is text")
	}
	if !sh.ts.Exists(gluttertest.ByText("$(no-such-icon)")) {
		t.Error("an icon that does not exist is shown as written")
	}
	noErrors(t, sh.harness)
}

func TestTheTooltipShowsAfterTheDelayWithTheKey(t *testing.T) {
	sh := newStatusHarness(t, testSize960, kit.DarkModern(), false, func(b *StatusBar) {
		b.Add("keyed", StatusRight, 2, StatusEntry{Text: "keyed", Tooltip: "Go to Line", Command: "test.keyed"})
		b.Add("nokey", StatusRight, 1, StatusEntry{Text: "nokey", Tooltip: "Plain tip", Command: "test.click"})
		b.Add("none", StatusLeft, 1, StatusEntry{Text: "none"})
	})
	sh.ts.Hover(sh.ts.Center(sh.item("keyed")))
	sh.ts.Pump(kit.DefaultHoverDelay - 50*time.Millisecond)
	if sh.ts.Exists(gluttertest.ByText("Go to Line (Ctrl+G)")) {
		t.Fatal("the tooltip showed before the delay")
	}
	sh.ts.Pump(100 * time.Millisecond)
	if !sh.ts.Exists(gluttertest.ByText("Go to Line (Ctrl+G)")) {
		t.Fatal("the tooltip with the key is not on screen")
	}
	// moving to the next item shows its tooltip at once (the hovers share a delegate)
	sh.ts.Hover(sh.ts.Center(sh.item("nokey")))
	sh.ts.Pump(50 * time.Millisecond)
	if !sh.ts.Exists(gluttertest.ByText("Plain tip")) {
		t.Error("the next tooltip waited for the delay")
	}
	sh.ts.Hover(sh.ts.Center(sh.item("none")))
	sh.ts.Pump(kit.DefaultHoverDelay + 100*time.Millisecond)
	if sh.ts.Exists(gluttertest.ByText("Plain tip")) {
		t.Error("the tooltip stays")
	}
	noErrors(t, sh.harness)
}

func TestStatusItemColors(t *testing.T) {
	for _, th := range goldenThemes {
		c := th.colors()
		tests := []struct {
			name              string
			kind              StatusKind
			clickable         bool
			hover, pressed    bool
			wantFg, wantBg    ui.Color
			wantHasBackground bool
		}{
			{"standard", StatusStandard, true, false, false, c.StatusBarForeground, ui.NewColor(0), false},
			{"hover on a clickable item", StatusStandard, true, true, false, c.StatusBarItemHoverForeground, c.StatusBarItemHoverBackground, true},
			{"hover on an item with no command", StatusStandard, false, true, false, c.StatusBarForeground, ui.NewColor(0), false},
			{"pressed", StatusStandard, true, true, true, c.StatusBarItemHoverForeground, c.StatusBarItemActiveBackground, true},
			{"warning", StatusWarning, false, false, false, c.StatusBarItemWarningForeground, c.StatusBarItemWarningBackground, true},
			{"warning hover", StatusWarning, true, true, false, c.StatusBarItemWarningHoverForeground, c.StatusBarItemWarningHoverBackground, true},
			{"error", StatusError, false, false, false, c.StatusBarItemErrorForeground, c.StatusBarItemErrorBackground, true},
			{"error hover", StatusError, true, true, false, c.StatusBarItemErrorHoverForeground, c.StatusBarItemErrorHoverBackground, true},
			{"prominent", StatusProminent, false, false, false, c.StatusBarItemProminentForeground, c.StatusBarItemProminentBackground, true},
			{"prominent hover", StatusProminent, true, true, false, c.StatusBarItemProminentHoverForeground, c.StatusBarItemProminentHoverBackground, true},
			{"remote", StatusRemote, true, false, false, c.StatusBarItemRemoteForeground, c.StatusBarItemRemoteBackground, true},
			{"remote hover", StatusRemote, true, true, false, c.StatusBarItemRemoteHoverForeground, c.StatusBarItemRemoteHoverBackground, true},
		}
		for _, tt := range tests {
			t.Run(th.name+" "+tt.name, func(t *testing.T) {
				fg, bg, has := statusItemColors(c, tt.kind, tt.clickable, tt.hover, tt.pressed)
				if fg != tt.wantFg || bg != tt.wantBg || has != tt.wantHasBackground {
					t.Errorf("got %v %v %v, want %v %v %v", fg, bg, has, tt.wantFg, tt.wantBg, tt.wantHasBackground)
				}
			})
		}
	}
}

func TestAnItemsOwnColor(t *testing.T) {
	tests := []struct {
		color   string
		ok      bool
		wantBg  ui.Color
		whiteFg bool
	}{
		{"#4a90d9", true, ui.NewColor(0xFF4A90D9), true},
		{"#d9534f", true, ui.NewColor(0xFFD9534F), true},
		{"#fff", true, ui.NewColor(0xFFFFFFFF), false},
		{"#ffeb3b", true, ui.NewColor(0xFFFFEB3B), false},
		{"#000", true, ui.NewColor(0xFF000000), true},
		{"", false, ui.NewColor(0), false},
		{"red", false, ui.NewColor(0), false},
		{"#12345", false, ui.NewColor(0), false},
	}
	for _, tt := range tests {
		t.Run(tt.color, func(t *testing.T) {
			got, ok := ownStatusColor(tt.color)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if !ok {
				return
			}
			white := got.fg == ui.NewColor(0xFFFFFFFF)
			if got.bg != tt.wantBg || white != tt.whiteFg {
				t.Errorf("got bg %v, white text %v; want %v, %v", got.bg, white, tt.wantBg, tt.whiteFg)
			}
		})
	}
}

func TestHoverAndPressChangeTheBackgroundOfAnItem(t *testing.T) {
	sh := newStatusHarness(t, testSize960, kit.DarkModern(), false, func(b *StatusBar) {
		b.Add("x", StatusLeft, 1, StatusEntry{Text: "hover me", Command: "test.click"})
		b.Add("y", StatusLeft, 0, StatusEntry{Text: "not me"})
	})
	colors := kit.DarkModern()
	// the background of an item is a colored box under its text
	filled := func(id string, c ui.Color) bool {
		for _, el := range sh.ts.Elements(gluttertest.Descendant(sh.item(id), gluttertest.ByType[*widgets.Container]())) {
			if w, ok := el.Widget().(*widgets.Container); ok && w.Color().OrElse(ui.NewColor(0)) == c {
				return true
			}
		}
		return false
	}
	if filled("x", colors.StatusBarItemHoverBackground) {
		t.Fatal("the item has a background before the pointer is over it")
	}
	sh.ts.Hover(sh.ts.Center(sh.item("x")))
	sh.settle()
	if !filled("x", colors.StatusBarItemHoverBackground) {
		t.Error("no hover background under the pointer")
	}
	g := sh.ts.StartGesture(sh.ts.Center(sh.item("x")), gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse, Buttons: foundation.Some(gestures.PrimaryButton)})
	sh.settle()
	if !filled("x", colors.StatusBarItemActiveBackground) {
		t.Error("no pressed background while the button is down")
	}
	g.Up()
	sh.settle()
	sh.ts.Hover(sh.ts.Center(sh.item("y")))
	sh.settle()
	if filled("x", colors.StatusBarItemHoverBackground) || filled("y", colors.StatusBarItemHoverBackground) {
		t.Error("an item with no command, or one the pointer left, has a hover background")
	}
}

func TestTheBarClipsTheLeftSideAndKeepsTheRight(t *testing.T) {
	sh := newStatusHarness(t, ui.Size{Width: 300, Height: 300}, kit.DarkModern(), false, func(b *StatusBar) {
		b.Add("l1", StatusLeft, 2, StatusEntry{Text: "a long text on the left side of the bar", Command: "test.click"})
		b.Add("l2", StatusLeft, 1, StatusEntry{Text: "another text on the left"})
		b.Add("r1", StatusRight, 2, StatusEntry{Text: "Ln 12, Col 5", Command: "test.click"})
		b.Add("r2", StatusRight, 1, StatusEntry{Text: "Go", Command: "test.click"})
	})
	bar := sh.rect(PartStatusBar)
	r1, r2 := sh.rectOf("r1"), sh.rectOf("r2")
	if r2.Right > bar.Right+0.5 || r2.Right < bar.Right-statusEdgeInset-0.5 || r1.Right > r2.Left+0.5 {
		t.Errorf("the right side is not at the right end: %v %v in %v", r1, r2, bar)
	}
	// the left items start at the left end and are cut off by the right side
	l1 := sh.rectOf("l1")
	if l1.Left > bar.Left+statusEdgeInset+0.5 {
		t.Errorf("the first left item starts at %v", l1.Left)
	}
	// clicking the part of an item that is visible works, clicking where the right side is does not reach it
	sh.click("r1")
	if len(sh.ran) != 1 {
		t.Errorf("ran %v", sh.ran)
	}
	noErrors(t, sh.harness)
	// a bar narrower than the right side alone still lays out
	// (a window of 120 px has no room for the title bar either: the test shows it without)
	sh.ts.SetViewConfiguration(func(c *ui.ViewConfiguration) { c.Window = ui.WindowInfo{} })
	sh.ts.SetViewSize(ui.Size{Width: 120, Height: 300})
	sh.settle()
	noErrors(t, sh.harness)
}

func TestTheBarHidesWithTheSetting(t *testing.T) {
	sh := newStatusHarness(t, testSize960, kit.DarkModern(), false, func(b *StatusBar) {
		b.Add("x", StatusRight, 1, StatusEntry{Text: "x", Command: "test.click"})
	})
	if !sh.shown(PartStatusBar) || !sh.ts.Exists(sh.item("x")) {
		t.Fatal("the bar is not shown")
	}
	sh.run("workbench.action.toggleStatusbarVisibility")
	if sh.shown(PartStatusBar) || sh.ts.Exists(sh.item("x")) {
		t.Error("the bar is shown after it was hidden")
	}
	sh.run("workbench.action.toggleStatusbarVisibility")
	if !sh.ts.Exists(sh.item("x")) {
		t.Error("the bar did not come back")
	}
}

func TestGoldenStatusBar(t *testing.T) {
	fill := func(b *StatusBar) {
		b.Add("status.problems", StatusLeft, 50, StatusEntry{Text: "$(error) 2 $(warning) 1", Tooltip: "Errors: 2, Warnings: 1", Command: "test.click"})
		b.Add("status.message", StatusLeft, BellPriority(), StatusEntry{Text: "(Ctrl+K) was pressed. Waiting for second key of chord..."})
		b.Add("status.editor.selection", StatusRight, 100.5, StatusEntry{Text: "Ln 12, Col 5 (7 selected)", Command: "test.click"})
		b.Add("status.editor.indentation", StatusRight, 100.4, StatusEntry{Text: "Spaces: 4", Command: "test.click"})
		b.Add("status.editor.encoding", StatusRight, 100.3, StatusEntry{Text: "UTF-8", Command: "test.click"})
		b.Add("status.editor.eol", StatusRight, 100.2, StatusEntry{Text: "LF", Command: "test.click"})
		b.Add("status.editor.mode", StatusRight, 100.1, StatusEntry{Text: "Go", Command: "test.click"})
		b.Add("status.notifications", StatusRight, BellPriority(), BellState{Total: 1, New: 1}.Entry())
	}
	kinds := func(b *StatusBar) {
		fill(b)
		b.Add("kind.error", StatusLeft, 40, StatusEntry{Text: "$(error) Error", Kind: StatusError, Command: "test.click"})
		b.Add("kind.warning", StatusLeft, 30, StatusEntry{Text: "$(warning) Warning", Kind: StatusWarning, Command: "test.click"})
		b.Add("kind.prominent", StatusLeft, 20, StatusEntry{Text: "Tab Moves Focus", Kind: StatusProminent, Command: "test.click"})
	}
	// the Sharing item of step 17e.4 while the workspace is hosted: the remote look, at the left end before the problems item
	hosting := func(b *StatusBar) {
		fill(b)
		b.Add("extension.emetos.hosting.status", StatusLeft, 100, StatusEntry{Text: "$(broadcast) Hosting · 2", Kind: StatusRemote, Command: "test.click"})
	}
	cases := []struct {
		name     string
		fill     func(*StatusBar)
		noFolder bool
		hover    string
	}{
		{"items", fill, false, ""},
		{"hosting", hosting, false, ""},
		{"no_folder", fill, true, ""},
		{"kinds", kinds, false, ""},
		{"hover", fill, false, "status.editor.encoding"},
	}
	for _, th := range goldenThemes {
		for _, tc := range cases {
			t.Run(th.name+" "+tc.name, func(t *testing.T) {
				sh := newStatusHarness(t, ui.Size{Width: 960, Height: 300}, th.colors(), tc.noFolder, tc.fill)
				if tc.hover != "" {
					sh.ts.Hover(sh.ts.Center(sh.item(tc.hover)))
					sh.settle()
				}
				testutil.MatchesGolden(sh.ts, "statusbar_"+tc.name+"_"+th.name)
				noErrors(t, sh.harness)
			})
		}
	}
}
