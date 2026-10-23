package editor

import (
	"time"

	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/widgets"
)

// initFind makes what the editor needs to show a find widget: the focus nodes and the text of
// its inputs, and their scope of context keys. A node that has the focus puts the keys of the
// find widget in force (findInputFocussed, replaceInputFocussed) on top of the editor's.
func (s *editorState) initFind(vm *ViewModel) {
	opts := s.Widget().opts
	s.findNode = widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "FindInput"})
	s.replaceNode = widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "ReplaceInput"})
	s.findText = widgets.NewTextEditingController(vm.FindState().SearchString())
	s.replaceText = widgets.NewTextEditingController(vm.FindState().ReplaceString())
	if s.scope != nil {
		s.findScope = s.scope.Child()
		// The widget is part of the editor as far as its keys go: the keys of the editor's focus
		// apply, those of its text do not.
		s.findScope.Batch(func() {
			s.findScope.Set("editorFocus", true)
			s.findScope.Set("editorTextFocus", false)
			s.findScope.Set("textInputFocus", false)
			s.findScope.Set("findInputFocussed", false)
			s.findScope.Set("replaceInputFocussed", false)
		})
		if opts.Scopes != nil {
			s.unregisterFind = append(s.unregisterFind,
				opts.Scopes.Register(s.findNode, s.findScope), opts.Scopes.Register(s.replaceNode, s.findScope))
		}
	}
	s.useFind(vm)
}

// useFind connects a view model to the widget: it asks the widget to move the focus and follows
// the state of its find.
func (s *editorState) useFind(vm *ViewModel) {
	vm.SetFindMemory(s.Widget().opts.FindMemory)
	vm.SetFindUI(FindUI{
		FocusFind:    func() { s.focusInput(false) },
		FocusReplace: func() { s.focusInput(true) },
		FocusEditor:  func() { s.node.RequestFocus() },
		InputFocus:   func() (bool, bool) { return s.findNode.HasFocus(), s.replaceNode.HasFocus() },
	})
	s.findHandle = vm.AddFindListener(s.findStateChanged)
	s.findText.SetText(vm.FindState().SearchString())
	s.replaceText.SetText(vm.FindState().ReplaceString())
}

// releaseFind disconnects a view model from the widget.
func (s *editorState) releaseFind(vm *ViewModel) {
	vm.RemoveFindListener(s.findHandle)
	vm.SetFindUI(FindUI{})
	vm.SetFindMemory(nil)
}

// disposeFind lets go of the nodes and the controllers.
func (s *editorState) disposeFind() {
	s.releaseFind(s.Widget().vm)
	for _, unregister := range s.unregisterFind {
		unregister()
	}
	s.unregisterFind = nil
	s.findNode.Dispose()
	s.replaceNode.Dispose()
	s.findText.Dispose()
	s.replaceText.Dispose()
}

// findStateChanged follows the opening and closing of the widget in the context keys.
func (s *editorState) findStateChanged() { s.updateKeys() }

// focusInput gives the focus to an input of the find widget as soon as the widget is on screen,
// and selects its text, which the user is about to replace (a second Ctrl+F in the input selects
// it again).
func (s *editorState) focusInput(replace bool) {
	node, text := s.findNode, s.findText
	if replace {
		node, text = s.replaceNode, s.replaceText
	}
	if node.HasFocus() {
		text.SetSelection(services.NewTextSelection(0, len(text.Text())))
		return
	}
	scheduler := s.binding.Scheduler()
	scheduler.ScheduleFrame()
	scheduler.AddPostFrameCallback(func(time.Duration) {
		if s.Mounted() && s.Widget().vm.FindRevealed() {
			node.RequestFocus()
		}
	})
}

// findFocusChanged follows the focus of the inputs of the find widget: it sets the context
// keys and makes the editor's commands work while an input has the focus.
func (s *editorState) findFocusChanged() {
	findIn, replaceIn := s.findNode.HasFocus(), s.replaceNode.HasFocus()
	s.findFocused = findIn || replaceIn
	if s.findScope != nil {
		s.findScope.Batch(func() {
			s.findScope.Set("findInputFocussed", findIn)
			s.findScope.Set("replaceInputFocussed", replaceIn)
		})
	}
	s.refreshCommands()
}
