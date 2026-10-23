package editor

import (
	"math"
	"time"

	"github.com/rm4n0s/errors"
)

// framer runs a function once after every frame, for as long as it returns true; the widget
// gives the view model one that asks for the frames.
type framer func(step func() bool)

// SetFramer gives the view model the means to run a function after each frame, which the dev
// commands that type and scroll use to spread their work over frames.
func (vm *ViewModel) SetFramer(f framer) { vm.framer = f }

// devActions are the editor's side of the dev commands (SPEC §15.2): they are bound only where
// the registry has them, which is in debug builds and in builds with the emetos_dev tag.
func devActions(a map[string]action) {
	typeText := a["type"]
	// dev.type types a text, a character in every frame, as a person types, so that the frame
	// stats show the frame of a keystroke. The argument is the text or {"text": "..."}.
	a["dev.type"] = func(vm *ViewModel, args any) error {
		text, ok := textArgument(args)
		if !ok {
			return errors.New("DevTypeInvalid", "dev.type needs a text or {\"text\": ...}")
		}
		runes := []rune(text)
		return vm.everyFrame(func() bool {
			if len(runes) == 0 {
				return false
			}
			_ = vm.Run(typeText, string(runes[0]))
			runes = runes[1:]
			return len(runes) > 0
		})
	}
	// dev.scroll scrolls down by rows in every frame for a number of frames (negative rows go
	// up). The argument is a number of rows, or {"rows": ..., "frames": ...}; one frame by
	// default.
	a["dev.scroll"] = func(vm *ViewModel, args any) error {
		vm.keepSessions, vm.skipReveal = true, true // the command only scrolls
		rows, frames, ok := scrollArguments(args)
		if !ok {
			return errors.New("DevScrollInvalid", "dev.scroll needs a number of rows or {\"rows\": ..., \"frames\": ...}")
		}
		return vm.everyFrame(func() bool {
			if frames <= 0 || vm.scroller == nil {
				return false
			}
			frames--
			delta := rows * vm.Options().LineHeight
			vm.scroller(min(max(vm.scrollTop+delta, 0), math.MaxFloat64))
			return frames > 0
		})
	}
}

// scrollArguments reads the argument of dev.scroll.
func scrollArguments(args any) (rows float64, frames int, ok bool) {
	switch v := args.(type) {
	case float64:
		return v, 1, true
	case map[string]any:
		rows, ok = v["rows"].(float64)
		if !ok {
			return 0, 0, false
		}
		frames = 1
		if f, has := v["frames"].(float64); has {
			frames = int(f)
		}
		return rows, frames, true
	}
	return 0, 0, false
}

// everyFrame runs step after each frame until it returns false; the first call is after the
// next frame.
func (vm *ViewModel) everyFrame(step func() bool) error {
	if vm.framer == nil {
		return errors.New("DevEditorNotShown", "the editor is not on screen")
	}
	vm.framer(step)
	return nil
}

// runPerFrame is the framer of the widget: it asks for a frame and runs the step after it, and
// goes on while the step wants more.
func (s *editorState) runPerFrame(step func() bool) {
	scheduler := s.binding.Scheduler()
	var tick func(time.Duration)
	tick = func(time.Duration) {
		if !s.Mounted() || !step() {
			return
		}
		scheduler.ScheduleFrame()
		scheduler.AddPostFrameCallback(tick)
	}
	scheduler.ScheduleFrame()
	scheduler.AddPostFrameCallback(tick)
}
