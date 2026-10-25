package todo

import (
	"testing"
	"time"

	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui/uitest"
)

// widgetOf builds the widget of the view as the box does.
func widgetOf(r *rig) widgets.Widget {
	return widgets.NewBuilder(func(bc widgets.BuildContext) widgets.Widget { return r.api.View(ViewID).Content.Build(bc) })
}

// timeout is a channel that fires after a few seconds, for a test that waits for a goroutine.
func timeout() <-chan time.Time { return time.After(5 * time.Second) }

// TestGoldenTheTodoView looks at the view with the comments of a small project, in both themes.
func TestGoldenTheTodoView(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := newRig(t, project)
			ts := uitest.Pump(t, th, gui.Size{Width: 300, Height: 220}, widgetOf(r))
			uitest.MatchesGolden(ts, "todo_view_"+th.Name)
		})
	}
}
