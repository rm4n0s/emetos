package kit

import (
	"math"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
)

func near(a, b float64) bool { return math.Abs(a-b) < 0.5 }

func TestInfiniteProgressBit(t *testing.T) {
	cases := []struct {
		phase        float64
		start, width float64
	}{
		{0, 0, 0.02},
		{0.25, 0.24, 0.04},
		{0.5, 0.48, 0.06},
		{0.75, 0.73, 0.04},
		{1, 0.98, 0.02},
		{-1, 0, 0.02},
		{2, 0.98, 0.02},
	}
	for _, tc := range cases {
		start, width := InfiniteProgressBit(tc.phase)
		if math.Abs(start-tc.start) > 1e-9 || math.Abs(width-tc.width) > 1e-9 {
			t.Errorf("phase %v: start %v width %v, want %v %v", tc.phase, start, width, tc.start, tc.width)
		}
	}
}

func TestDiscreteProgress(t *testing.T) {
	cases := []struct {
		total, worked, want float64
	}{
		{100, 0, 0}, {100, 50, 0.5}, {200, 50, 0.25}, {100, 150, 1}, {0, 40, 0.4}, {-5, 40, 0.4}, {10, -3, 0},
	}
	for _, tc := range cases {
		if got := DiscreteProgress(tc.total, tc.worked); math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("total %v worked %v: %v, want %v", tc.total, tc.worked, got, tc.want)
		}
	}
}

// bitOf is the place of the bit of the bar in the window: left and width.
func bitOf(ts *gluttertest.Tester) (left, width float64) {
	bit := gluttertest.Descendant(gluttertest.ByType[*ProgressBar](), gluttertest.ByType[*widgets.DecoratedBox]())
	return ts.TopLeft(bit).Dx, ts.Size(bit).Width
}

func pumpBar(t *testing.T, colors Colors, o ProgressBarOpts) *gluttertest.Tester {
	t.Helper()
	return pumpThemed(t, colors, ui.Size{Width: 232, Height: 40}, padded(widgets.NewSizedBox(NewProgressBar(o), widgets.SizedBoxOpts{Width: foundation.Some(200.0)})))
}

func TestProgressBarInfiniteAnimation(t *testing.T) {
	ts := pumpBar(t, DarkModern(), ProgressBarOpts{Infinite: true})
	origin := ts.TopLeft(gluttertest.ByType[*ProgressBar]()).Dx
	if h := ts.Size(gluttertest.ByType[*ProgressBar]()).Height; h != progressBarHeight {
		t.Errorf("height %v", h)
	}
	cases := []struct {
		at           time.Duration // time since the start
		start, width float64       // fractions of the 200 px
	}{
		{0, 0, 0.02},
		{time.Second, 0.24, 0.04},
		{2 * time.Second, 0.48, 0.06},
		{3 * time.Second, 0.73, 0.04},
		{5 * time.Second, 0.24, 0.04}, // the second trip
	}
	var now time.Duration
	for _, tc := range cases {
		ts.Pump(tc.at - now)
		now = tc.at
		left, width := bitOf(ts)
		if !near(left-origin, tc.start*200) || !near(width, tc.width*200) {
			t.Errorf("at %v: bit at %.1f, %.1f wide, want %.1f, %.1f", tc.at, left-origin, width, tc.start*200, tc.width*200)
		}
	}
	if errs := ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestProgressBarInfiniteLongRunning(t *testing.T) {
	ts := pumpBar(t, DarkModern(), ProgressBarOpts{Infinite: true})
	origin := ts.TopLeft(gluttertest.ByType[*ProgressBar]()).Dx
	// 9.5 s: not yet long running, phase 0.375 of the trip
	ts.Pump(9500 * time.Millisecond)
	left, _ := bitOf(ts)
	wantStart, _ := InfiniteProgressBit(0.375)
	if !near(left-origin, wantStart*200) {
		t.Errorf("at 9.5 s: bit at %.2f, want %.2f", left-origin, wantStart*200)
	}
	// 10.18 s: phase 0.545, but in steps of 1 % of the trip: 0.54
	ts.Pump(680 * time.Millisecond)
	left, _ = bitOf(ts)
	phase := math.Mod(10.18, 4) / 4
	exact, _ := InfiniteProgressBit(phase)
	stepped, _ := InfiniteProgressBit(math.Floor(phase*100) / 100)
	if exact == stepped {
		t.Fatal("the test needs a phase between two steps")
	}
	if !near(left-origin, stepped*200) {
		t.Errorf("at 10.18 s: bit at %.2f, want the step %.2f (not %.2f)", left-origin, stepped*200, exact*200)
	}
}

func TestProgressBarDiscrete(t *testing.T) {
	worked, total := 0.0, 100.0
	var setState widgets.StateSetter
	content := widgets.NewStatefulBuilder(func(_ widgets.BuildContext, set widgets.StateSetter) widgets.Widget {
		setState = set
		return padded(widgets.NewSizedBox(NewProgressBar(ProgressBarOpts{Total: total, Worked: worked}), widgets.SizedBoxOpts{Width: foundation.Some(200.0)}))
	})
	ts := pumpThemed(t, DarkModern(), ui.Size{Width: 232, Height: 40}, content)
	width := func() float64 {
		_, w := bitOf(ts)
		return w
	}
	if w := width(); w != 0 {
		t.Errorf("width %v with nothing worked", w)
	}
	steps := []struct {
		name          string
		total, worked float64
		after         time.Duration
		want          float64
	}{
		{"half done, halfway through the transition", 100, 50, 50 * time.Millisecond, 50},
		{"half done, after the transition", 100, 50, 60 * time.Millisecond, 100},
		{"a quarter of 200", 200, 50, 100 * time.Millisecond, 50},
		{"more than the total fills the bar", 200, 500, 100 * time.Millisecond, 200},
		{"nothing worked", 200, 0, 100 * time.Millisecond, 0},
	}
	for _, s := range steps {
		setState(func() { total, worked = s.total, s.worked })
		ts.Pump(time.Millisecond)
		ts.Pump(s.after)
		if got := width(); !near(got, s.want) {
			t.Errorf("%s: width %.1f, want %.1f", s.name, got, s.want)
		}
	}
	if errs := ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestProgressBarSwitchesBetweenTheModes(t *testing.T) {
	infinite := true
	var setState widgets.StateSetter
	content := widgets.NewStatefulBuilder(func(_ widgets.BuildContext, set widgets.StateSetter) widgets.Widget {
		setState = set
		return padded(widgets.NewSizedBox(NewProgressBar(ProgressBarOpts{Infinite: infinite, Total: 10, Worked: 10}), widgets.SizedBoxOpts{Width: foundation.Some(200.0)}))
	})
	ts := pumpThemed(t, DarkModern(), ui.Size{Width: 232, Height: 40}, content)
	ts.Pump(2 * time.Second)
	if _, w := bitOf(ts); !near(w, 12) {
		t.Errorf("infinite bit %.1f wide", w)
	}
	setState(func() { infinite = false })
	ts.Pump(time.Millisecond)
	ts.Pump(200 * time.Millisecond)
	if _, w := bitOf(ts); !near(w, 200) {
		t.Errorf("discrete bar %.1f wide, want it full", w)
	}
	// the trip is stopped: time passing changes nothing
	ts.Pump(3 * time.Second)
	if _, w := bitOf(ts); !near(w, 200) {
		t.Errorf("discrete bar %.1f wide", w)
	}
	if errs := ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestProgressBarColor(t *testing.T) {
	for _, theme := range []struct {
		name   string
		colors Colors
	}{{"Dark Modern", DarkModern()}, {"Light Modern", LightModern()}} {
		t.Run(theme.name, func(t *testing.T) {
			ts := pumpBar(t, theme.colors, ProgressBarOpts{Total: 100, Worked: 100})
			ts.Pump(200 * time.Millisecond)
			bit := gluttertest.Descendant(gluttertest.ByType[*ProgressBar](), gluttertest.ByType[*widgets.DecoratedBox]())
			d := ts.Widget(bit).(*widgets.DecoratedBox).Decoration().(*painting.BoxDecoration)
			if got := d.Color.OrElse(ui.Color{}); got != theme.colors.ProgressBarBackground || got.Alpha() == 0 {
				t.Errorf("color %v, want %v", got, theme.colors.ProgressBarBackground)
			}
		})
	}
}
