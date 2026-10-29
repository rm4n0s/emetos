package kit

import (
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
)

// A rebuild asked for on the UI thread outside a frame's build runs at once; during the build, and from another goroutine, it is
// posted to the UI thread.
func TestRebuildSoon(t *testing.T) {
	tests := []struct {
		name string
		// call makes the call and says whether rebuild had run when it returned.
		call func(ts *gluttertest.Tester, b *widgets.WidgetsBinding, rebuild func()) (ranAtOnce bool)
		// wantAtOnce and wantAfterAPump say when the rebuild runs.
		wantAtOnce, wantAfterAPump bool
	}{
		{
			name: "on the UI thread between frames",
			call: func(_ *gluttertest.Tester, b *widgets.WidgetsBinding, rebuild func()) bool {
				return runsAtOnce(b, rebuild)
			},
			wantAtOnce: true, wantAfterAPump: true,
		},
		{
			name: "from another goroutine",
			call: func(ts *gluttertest.Tester, b *widgets.WidgetsBinding, rebuild func()) bool {
				done := make(chan struct{})
				go func() { RebuildSoon(b, rebuild); close(done) }()
				<-done
				return false
			},
			wantAtOnce: false, wantAfterAPump: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := gluttertest.New(t, gluttertest.Opts{Size: ui.Size{Width: 100, Height: 100}})
			var b *widgets.WidgetsBinding
			ts.PumpWidget(widgets.NewBuilder(func(bc widgets.BuildContext) widgets.Widget {
				b = widgets.WidgetsBindingOf(bc)
				return widgets.NewSizedBox(nil)
			}))
			ts.Pump(16 * time.Millisecond)
			ran := 0
			atOnce := tt.call(ts, b, func() { ran++ })
			if atOnce != tt.wantAtOnce {
				t.Errorf("ran at once = %v, want %v", atOnce, tt.wantAtOnce)
			}
			if tt.wantAtOnce && ran != 1 {
				t.Errorf("ran %d times at once, want 1", ran)
			}
			if !tt.wantAtOnce && ran != 0 {
				t.Errorf("ran %d times before the pump, want 0", ran)
			}
			ts.Pump(16 * time.Millisecond)
			if (ran == 1) != tt.wantAfterAPump {
				t.Errorf("ran %d times after a pump", ran)
			}
		})
	}
}

func runsAtOnce(b *widgets.WidgetsBinding, rebuild func()) bool {
	ran := false
	RebuildSoon(b, func() { ran = true; rebuild() })
	return ran
}

// During the build of a frame no state may be set, so the rebuild waits for the UI thread's next turn.
func TestRebuildSoonDuringBuildIsPosted(t *testing.T) {
	ts := gluttertest.New(t, gluttertest.Opts{Size: ui.Size{Width: 100, Height: 100}})
	ran, ranInBuild, builds := 0, -1, 0
	var setState widgets.StateSetter
	ts.PumpWidget(widgets.NewStatefulBuilder(func(bc widgets.BuildContext, set widgets.StateSetter) widgets.Widget {
		setState = set
		builds++
		if builds == 2 {
			RebuildSoon(widgets.WidgetsBindingOf(bc), func() { ran++ })
			ranInBuild = ran
		}
		return widgets.NewSizedBox(nil)
	}))
	ts.Pump(16 * time.Millisecond)
	setState(func() {})
	ts.Pump(16 * time.Millisecond)
	if builds < 2 {
		t.Fatalf("the widget was built %d times", builds)
	}
	if ranInBuild != 0 {
		t.Errorf("the rebuild ran %d times inside the build", ranInBuild)
	}
	ts.Pump(16 * time.Millisecond)
	if ran != 1 {
		t.Errorf("the rebuild ran %d times after the frame, want 1", ran)
	}
}
