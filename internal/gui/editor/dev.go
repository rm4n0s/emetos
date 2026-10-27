package editor

import (
	"math"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/buffer"
)

// framer runs a function once after every frame, for as long as it returns true; the widget
// gives the view model one that asks for the frames.
type framer func(step func() bool)

// SetFramer gives the view model the means to run a function after each frame, which the dev
// commands that type and scroll use to spread their work over frames.
func (vm *ViewModel) SetFramer(f framer) { vm.framer = f }

// DevState is what the editor shows of the modes that live in the view model, for the dev server (dev.editor).
type DevState struct {
	// InSnippet is the context key inSnippetMode, TabStops the tab stops of the session that are decorations of the document
	// (0 when no session runs: nothing is drawn then).
	InSnippet bool
	TabStops  int
	// SuggestVisible and SignatureVisible tell whether the suggest widget and the parameter hints show.
	SuggestVisible, SignatureVisible bool
	// HoverVisible tells whether the hover box shows.
	HoverVisible bool
}

// DevState reads the modes of the editor.
func (vm *ViewModel) DevState() DevState {
	st := DevState{InSnippet: vm.SnippetState().InSnippet, SuggestVisible: vm.SuggestVisible(), SignatureVisible: vm.sig.shown != nil, HoverVisible: vm.hover.shown != nil}
	whole := buffer.Range{End: buffer.Position{Line: vm.doc.Snapshot().LineCount()}}
	st.TabStops = len(vm.doc.DecorationsIn(whole, vm.owner(snippetOwner)))
	return st
}

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
			vm.sg.runKind = runEdit
			_ = vm.Run(typeText, string(runes[0]))
			runes = runes[1:]
			return len(runes) > 0
		})
	}
	// dev.showHover shows the hover at a position, {"line": ..., "column": ...} counted from 1 (the cursor when missing), and
	// keeps it until the cursor moves; it fails with the tag DevShowHoverEmpty when nothing is there.
	a["dev.showHover"] = func(vm *ViewModel, args any) error {
		vm.keepSessions, vm.skipReveal = true, true // the command only shows
		pos := vm.cursors.Primary().Position()
		if m, ok := args.(map[string]any); ok {
			line, hasLine := m["line"].(float64)
			col, hasCol := m["column"].(float64)
			if hasLine && hasCol {
				pos = vm.doc.Snapshot().ValidatePosition(buffer.Position{Line: int(line) - 1, Col: int(col) - 1})
			}
		}
		if !vm.ShowHoverAt(pos) {
			return errors.New("DevShowHoverEmpty", "there is nothing to show a hover for at the position", "line", pos.Line+1, "column", pos.Col+1)
		}
		return nil
	}
	// dev.triggerParameterHints opens the parameter hints at the cursor as Trigger Parameter Hints does (Ctrl+Shift+Space), for the
	// proofs.
	a["dev.triggerParameterHints"] = a["editor.action.triggerParameterHints"]
	// dev.triggerSuggest opens the suggest widget as Trigger Suggest does (Ctrl+Space), for the proofs.
	a["dev.triggerSuggest"] = a[triggerSuggestCommand]
	// dev.toggleSuggestDetails opens or closes the details pane of the suggest widget.
	a["dev.toggleSuggestDetails"] = a["toggleSuggestionDetails"]
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
