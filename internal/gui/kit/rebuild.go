package kit

import (
	"github.com/rm4n0s/glutter/scheduler"
	"github.com/rm4n0s/glutter/widgets"
)

// RebuildSoon runs rebuild (a SetState of a widget that follows a model) where it makes the least frames: at once when the caller
// is the UI thread and no frame is being built, laid out or painted, else posted to the UI thread. A model that changes while the
// frame handles a key (the cursor moves, a status item changes) is then built in that frame; posted, the change would come after
// the frame and cost a frame of its own, which for a held key repeating at 50 a second is more than a software renderer can draw.
// Models change from any goroutine and during layout, where no state may be set, which is why the fallback is a post.
func RebuildSoon(binding *widgets.WidgetsBinding, rebuild func()) {
	if CanSetStateNow(binding) {
		rebuild()
		return
	}
	binding.Post(rebuild)
}

// CanSetStateNow is whether the caller may set the state of a widget: it is on the UI thread, and the build, layout and paint of a
// frame are not going on.
func CanSetStateNow(binding *widgets.WidgetsBinding) bool {
	return binding.UIThread().IsCurrent() && binding.Scheduler().SchedulerPhase() != scheduler.SchedulerPhasePersistentCallbacks
}
