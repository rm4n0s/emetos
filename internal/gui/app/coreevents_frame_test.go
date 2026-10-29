package app

import (
	"testing"
	"time"

	"github.com/rm4n0s/glutter/scheduler"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/testutil"
	"github.com/rm4n0s/glutter/ui"
)

// What waits for the end of a frame runs at the end of the frame that is scheduled; with none scheduled it runs at once and
// asks for no frame of its own.
func TestAfterNextFrame(t *testing.T) {
	tests := []struct {
		name string
		// scheduled asks for a frame before the call.
		scheduled bool
		// ranAtOnce, ranAfterAFrame and frameAfterCall are what the test expects.
		ranAtOnce, ranAfterAFrame, frameAfterCall bool
	}{
		{name: "no frame is scheduled", ranAtOnce: true, ranAfterAFrame: true, frameAfterCall: false},
		{name: "a frame is scheduled", scheduled: true, ranAtOnce: false, ranAfterAFrame: true, frameAfterCall: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := testutil.New(t, ui.Size{Width: 100, Height: 100})
			var sched *scheduler.SchedulerBinding
			ts.PumpWidget(widgets.NewBuilder(func(bc widgets.BuildContext) widgets.Widget {
				sched = widgets.WidgetsBindingOf(bc).Scheduler()
				return widgets.NewSizedBox(nil)
			}))
			for range 3 {
				ts.Pump(16 * time.Millisecond)
			}
			if sched.HasScheduledFrame() {
				t.Fatal("a frame is scheduled in an idle window")
			}
			if tt.scheduled {
				sched.ScheduleFrame()
			}
			ran := false
			afterNextFrame(sched, func() { ran = true })
			if ran != tt.ranAtOnce {
				t.Errorf("ran at once = %v, want %v", ran, tt.ranAtOnce)
			}
			if got := sched.HasScheduledFrame(); got != tt.frameAfterCall {
				t.Errorf("a frame is scheduled after the call = %v, want %v", got, tt.frameAfterCall)
			}
			ts.Pump(16 * time.Millisecond)
			if ran != tt.ranAfterAFrame {
				t.Errorf("ran after a frame = %v, want %v", ran, tt.ranAfterAFrame)
			}
		})
	}
}
