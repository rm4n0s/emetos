package app

import (
	"math"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
)

// bindGroupCommands takes over the commands of the editor groups in the registry while the shell is on
// screen. The registry must have them (command.DefaultRegistry does): a missing one is a mistake of the
// caller and panics with the CommandNotFound of command.Registry.SetHandler.
//
// Ported from VS Code: src/vs/workbench/browser/parts/editor/editorCommands.ts and
// src/vs/workbench/browser/parts/editor/editorActions.ts
func (s *shellState) bindGroupCommands() {
	reg := PeekWorkspace(s.Context()).Commands
	handlers := s.editorCommands()
	for _, more := range []map[string]command.Handler{s.gridCommands(), s.pathCommands(), s.navCommands(), s.switchCommands(), s.settingsCommands(), s.keybindingsCommands(), s.extensionsCommands(), s.fileViewCommands(), s.themeCommands(), s.profileCommands(), s.diffCommands(), s.compareCommands(), s.mergeCommands()} {
		for id, h := range more {
			handlers[id] = h
		}
	}
	removes, err := setGroupHandlers(reg, handlers)
	if err != nil {
		// the commands are registered by the program itself: a missing one is a broken build
		panic(err)
	}
	s.group.removeCommands = append(s.group.removeCommands, removes...)
}

// editorCommands are the commands of the editors of a group: close, keep and pin (switching between them is in
// switchCommands). The commands
// that act on an editor take it from their argument when it names one ({"groupId", "editorIndex"}, the
// context that the menu of a tab passes, as in VS Code), else they act on the active editor of the
// active group.
func (s *shellState) editorCommands() map[string]command.Handler {
	// notSticky filters the editors that the commands that close many leave alone when sticky.
	notSticky := func(m *editorgroup.Model, editors []editorgroup.EditorInput) []editorgroup.EditorInput {
		var out []editorgroup.EditorInput
		for _, e := range editors {
			if !m.IsSticky(e) {
				out = append(out, e)
			}
		}
		return out
	}
	// targetIn is the editors of a group that a close command closes (the sticky ones stay), those that keep
	// accepts.
	targetIn := func(g *editorgroup.Group, keep func(editorgroup.EditorInput) bool) editorgroup.CloseTarget {
		t := editorgroup.CloseTarget{Group: g}
		for _, e := range notSticky(g.Model, g.Model.Editors(editorgroup.Sequential, false)) {
			if keep == nil || keep(e) {
				t.Editors = append(t.Editors, e)
			}
		}
		return t
	}
	closeInGroup := func(g *editorgroup.Group, keep func(editorgroup.EditorInput) bool) {
		s.closeConfirmed([]editorgroup.CloseTarget{targetIn(g, keep)}, nil)
	}
	// onEditor runs f for the group and the editor of the argument
	onEditor := func(f func(g *editorgroup.Group, e editorgroup.EditorInput)) command.Handler {
		return func(_ command.Context, args any) error {
			if g, e := s.commandTarget(args); g != nil && e != nil {
				f(g, e)
			}
			return nil
		}
	}
	// onGroup runs f for the group of the argument
	onGroup := func(f func(g *editorgroup.Group)) command.Handler {
		return func(_ command.Context, args any) error {
			if g, _ := s.commandTarget(args); g != nil {
				f(g)
			}
			return nil
		}
	}
	return map[string]command.Handler{
		"workbench.action.closeActiveEditor": onEditor(func(g *editorgroup.Group, e editorgroup.EditorInput) {
			s.closeEditors(g, []editorgroup.EditorInput{e})
		}),
		"workbench.action.closeEditorsInGroup": onGroup(func(g *editorgroup.Group) { closeInGroup(g, nil) }),
		// all the editors of all the groups
		"workbench.action.closeAllEditors": func(command.Context, any) error {
			var targets []editorgroup.CloseTarget
			for _, g := range s.group.grid.Groups(editorgroup.MostRecentlyActiveGroups) {
				targets = append(targets, targetIn(g, nil))
			}
			s.closeConfirmed(targets, nil)
			return nil
		},
		"workbench.action.closeUnmodifiedEditors": onGroup(func(g *editorgroup.Group) {
			closeInGroup(g, func(e editorgroup.EditorInput) bool { return !e.Dirty() })
		}),
		// the editor stays, kept (a preview that others replaced would be lost)
		"workbench.action.closeOtherEditors": onEditor(func(g *editorgroup.Group, keep editorgroup.EditorInput) {
			g.Model.Pin(keep)
			closeInGroup(g, func(e editorgroup.EditorInput) bool { return e != keep })
		}),
		// the active editor is kept, while the editors on the right of the one named are closed
		"workbench.action.closeEditorsToTheRight": onEditor(func(g *editorgroup.Group, from editorgroup.EditorInput) {
			m := g.Model
			if active := m.ActiveEditor(); active != nil {
				m.Pin(active)
			}
			editors := m.Editors(editorgroup.Sequential, false)
			s.closeEditors(g, notSticky(m, editors[m.IndexOf(from)+1:]))
		}),
		"workbench.action.keepEditor":  onEditor(func(g *editorgroup.Group, e editorgroup.EditorInput) { g.Model.Pin(e) }),
		"workbench.action.pinEditor":   onEditor(func(g *editorgroup.Group, e editorgroup.EditorInput) { g.Model.Stick(e) }),
		"workbench.action.unpinEditor": onEditor(func(g *editorgroup.Group, e editorgroup.EditorInput) { g.Model.Unstick(e) }),
	}
}

// commandTarget is the group and the editor a command of the editors acts on: those that the argument
// names ({"groupId": n, "editorIndex": n}; the menu of a tab passes it), or the active editor of the
// active group (nil editor in an empty group). It gives nil, nil for an argument that names a group
// or an editor that is not there any more, so that the command does nothing.
//
// Ported from VS Code: src/vs/workbench/browser/parts/editor/editorCommandsContext.ts
// (resolveCommandsContext)
func (s *shellState) commandTarget(args any) (*editorgroup.Group, editorgroup.EditorInput) {
	if m, ok := args.(map[string]any); ok {
		if id, ok := m["groupId"].(float64); ok {
			g := s.group.grid.Group(int(id))
			if g == nil {
				return nil, nil
			}
			index, ok := m["editorIndex"].(float64)
			if !ok {
				return g, g.Model.ActiveEditor()
			}
			e := g.Model.EditorAt(int(index))
			if e == nil {
				return nil, nil
			}
			return g, e
		}
	}
	g := s.activeGroup()
	return g, g.Model.ActiveEditor()
}

// tabContext is the argument that makes a command act on the tab of an editor of a group: the
// context that VS Code's menu of a tab passes.
func tabContext(group *editorgroup.Group, index int) map[string]any {
	return map[string]any{"groupId": float64(group.ID()), "editorIndex": float64(index)}
}

// gridCommands are the commands of the grid of groups: split, focus, move and copy editors, move, join,
// close and size groups, and the layouts.
func (s *shellState) gridCommands() map[string]command.Handler {
	grid := func() *editorgroup.Grid { return s.group.grid }
	// done gives the keyboard focus to the active group once the command changed the grid
	run := func(f func() error) command.Handler {
		return func(command.Context, any) error {
			err := f()
			s.askFocusSoon()
			return err
		}
	}
	activate := func(g *editorgroup.Group) error {
		if g == nil {
			return nil
		}
		return grid().Activate(g)
	}
	handlers := map[string]command.Handler{}

	// splits
	split := func(dir func() editorgroup.Direction) command.Handler {
		return func(_ command.Context, args any) error {
			g, e := s.commandTarget(args)
			if g == nil {
				return nil
			}
			_, err := grid().SplitEditor(g, e, dir())
			s.askFocusSoon()
			return err
		}
	}
	fixed := func(d editorgroup.Direction) func() editorgroup.Direction {
		return func() editorgroup.Direction { return d }
	}
	orthogonal := func() editorgroup.Direction {
		if s.sideBySideDirection() == editorgroup.DirRight {
			return editorgroup.DirDown
		}
		return editorgroup.DirRight
	}
	handlers["workbench.action.splitEditor"] = split(s.sideBySideDirection)
	handlers["workbench.action.splitEditorOrthogonal"] = split(orthogonal)
	handlers["workbench.action.splitEditorLeft"] = split(fixed(editorgroup.DirLeft))
	handlers["workbench.action.splitEditorRight"] = split(fixed(editorgroup.DirRight))
	handlers["workbench.action.splitEditorUp"] = split(fixed(editorgroup.DirUp))
	handlers["workbench.action.splitEditorDown"] = split(fixed(editorgroup.DirDown))

	// new empty groups
	for id, dir := range map[string]editorgroup.Direction{
		"workbench.action.newGroupLeft": editorgroup.DirLeft, "workbench.action.newGroupRight": editorgroup.DirRight,
		"workbench.action.newGroupAbove": editorgroup.DirUp, "workbench.action.newGroupBelow": editorgroup.DirDown,
	} {
		handlers[id] = run(func() error {
			nu, err := grid().AddGroup(s.activeGroup(), dir)
			if err != nil {
				return err
			}
			return activate(nu)
		})
	}

	// focus a group by its place, its neighbor or its order
	focusFind := func(find func(g *editorgroup.Group) *editorgroup.Group) command.Handler {
		return run(func() error { return activate(find(s.activeGroup())) })
	}
	byDirection := func(d editorgroup.Direction, wrap bool) func(g *editorgroup.Group) *editorgroup.Group {
		return func(g *editorgroup.Group) *editorgroup.Group {
			found := grid().FindByDirection(g, d, wrap)
			if found == nil && !wrap {
				return g
			}
			return found
		}
	}
	byLocation := func(l editorgroup.Location, wrap bool) func(g *editorgroup.Group) *editorgroup.Group {
		return func(g *editorgroup.Group) *editorgroup.Group { return grid().FindByLocation(g, l, wrap) }
	}
	handlers["workbench.action.focusFirstEditorGroup"] = focusFind(byLocation(editorgroup.LocationFirst, true))
	handlers["workbench.action.focusLastEditorGroup"] = focusFind(byLocation(editorgroup.LocationLast, true))
	handlers["workbench.action.focusNextGroup"] = focusFind(byLocation(editorgroup.LocationNext, true))
	handlers["workbench.action.focusPreviousGroup"] = focusFind(byLocation(editorgroup.LocationPrevious, true))
	handlers["workbench.action.navigateEditorGroups"] = focusFind(byLocation(editorgroup.LocationNext, true))
	handlers["workbench.action.focusLeftGroup"] = focusFind(byDirection(editorgroup.DirLeft, true))
	handlers["workbench.action.focusRightGroup"] = focusFind(byDirection(editorgroup.DirRight, true))
	handlers["workbench.action.focusAboveGroup"] = focusFind(byDirection(editorgroup.DirUp, true))
	handlers["workbench.action.focusBelowGroup"] = focusFind(byDirection(editorgroup.DirDown, true))
	handlers["workbench.action.focusLeftGroupWithoutWrap"] = focusFind(byDirection(editorgroup.DirLeft, false))
	handlers["workbench.action.focusRightGroupWithoutWrap"] = focusFind(byDirection(editorgroup.DirRight, false))
	handlers["workbench.action.focusAboveGroupWithoutWrap"] = focusFind(byDirection(editorgroup.DirUp, false))
	handlers["workbench.action.focusBelowGroupWithoutWrap"] = focusFind(byDirection(editorgroup.DirDown, false))
	for i, id := range []string{
		"workbench.action.focusSecondEditorGroup", "workbench.action.focusThirdEditorGroup", "workbench.action.focusFourthEditorGroup",
		"workbench.action.focusFifthEditorGroup", "workbench.action.focusSixthEditorGroup", "workbench.action.focusSeventhEditorGroup",
		"workbench.action.focusEighthEditorGroup",
	} {
		n := i + 2
		handlers[id] = run(func() error {
			g, err := grid().FocusIndex(n, s.sideBySideDirection())
			if err != nil {
				return err
			}
			return activate(g)
		})
	}

	// move or copy the active editor to a group
	moveCopy := func(move bool, to editorgroup.Target, value int, args any) error {
		src, editor := s.commandTarget(args)
		if src == nil || editor == nil {
			return nil
		}
		target, err := grid().MoveOrCopyTarget(src, to, value, s.sideBySideDirection())
		if err != nil || target == nil {
			return err
		}
		if move {
			if _, err := grid().MoveEditor(src, editor, target, editorgroup.MoveOptions{}); err != nil {
				return err
			}
		} else if target != src {
			if _, _, err := grid().CopyEditor(src, editor, target, editorgroup.MoveOptions{}); err != nil {
				return err
			}
		}
		return activate(target)
	}
	for suffix, to := range map[string]editorgroup.Target{
		"Previous": editorgroup.TargetPrevious, "Next": editorgroup.TargetNext, "Above": editorgroup.TargetUp,
		"Below": editorgroup.TargetDown, "Left": editorgroup.TargetLeft, "Right": editorgroup.TargetRight,
		"First": editorgroup.TargetFirst, "Last": editorgroup.TargetLast,
	} {
		handlers["workbench.action.moveEditorTo"+suffix+"Group"] = func(_ command.Context, args any) error {
			err := moveCopy(true, to, 0, args)
			s.askFocusSoon()
			return err
		}
		handlers["workbench.action.splitEditorTo"+suffix+"Group"] = func(_ command.Context, args any) error {
			err := moveCopy(false, to, 0, args)
			s.askFocusSoon()
			return err
		}
	}
	handlers["moveActiveEditor"] = func(_ command.Context, args any) error { return s.moveCopyActive(true, args) }
	handlers["copyActiveEditor"] = func(_ command.Context, args any) error { return s.moveCopyActive(false, args) }

	// move and copy the active group
	for id, dir := range map[string]editorgroup.Direction{
		"Left": editorgroup.DirLeft, "Right": editorgroup.DirRight, "Up": editorgroup.DirUp, "Down": editorgroup.DirDown,
	} {
		handlers["workbench.action.moveActiveEditorGroup"+id] = run(func() error {
			src := s.activeGroup()
			target := s.groupTarget(src, dir)
			if target == nil {
				return nil
			}
			if err := grid().MoveGroup(src, target, dir); err != nil {
				return err
			}
			return activate(src)
		})
		handlers["workbench.action.duplicateActiveEditorGroup"+id] = run(func() error {
			src := s.activeGroup()
			nu, err := grid().CopyGroup(src, src, dir)
			if err != nil {
				return err
			}
			return activate(nu)
		})
	}

	// join groups
	handlers["workbench.action.joinTwoGroups"] = run(func() error {
		src := s.activeGroup()
		for _, dir := range []editorgroup.Direction{editorgroup.DirRight, editorgroup.DirDown, editorgroup.DirLeft, editorgroup.DirUp} {
			if target := grid().FindByDirection(src, dir, false); target != nil && target != src {
				_, err := grid().MergeGroup(src, target)
				return err
			}
		}
		return nil
	})
	handlers["workbench.action.joinAllGroups"] = run(func() error { return grid().MergeAllGroups(s.activeGroup()) })

	// close groups
	handlers["workbench.action.closeGroup"] = run(func() error {
		_, err := grid().RemoveGroup(s.activeGroup())
		return err
	})
	handlers["workbench.action.closeEditorsAndGroup"] = run(func() error {
		g := s.activeGroup()
		var err error
		s.closeConfirmed([]editorgroup.CloseTarget{{Group: g, Editors: g.Model.Editors(editorgroup.Sequential, false)}}, func(closed bool) {
			if closed && grid().Group(g.ID()) == g && g.Empty() {
				// the answer may come after the command returned, so the failure is logged as well
				if _, err = grid().RemoveGroup(g); err != nil {
					PeekWorkspace(s.Context()).Process.Logger.Warn("cannot remove the group", "err", err)
				}
			}
		})
		return err
	})
	handlers["workbench.action.closeAllGroups"] = run(func() error {
		var targets []editorgroup.CloseTarget
		for _, g := range grid().Groups(editorgroup.MostRecentlyActiveGroups) {
			targets = append(targets, editorgroup.CloseTarget{Group: g, Editors: g.Model.Editors(editorgroup.Sequential, false)})
		}
		var err error
		s.closeConfirmed(targets, func(closed bool) {
			if !closed {
				return
			}
			// a group that is empty goes, whatever workbench.editor.closeEmptyGroups says
			for _, g := range grid().Groups(editorgroup.MostRecentlyActiveGroups) {
				if grid().Group(g.ID()) == g && g.Empty() && grid().Count() > 1 {
					if _, e := grid().RemoveGroup(g); e != nil {
						PeekWorkspace(s.Context()).Process.Logger.Warn("cannot remove the group", "err", e)
						if err == nil {
							err = e
						}
					}
				}
			}
		})
		return err
	})
	handlers["workbench.action.closeEditorsInOtherGroups"] = run(func() error {
		skip := s.activeGroup()
		var targets []editorgroup.CloseTarget
		for _, g := range grid().Groups(editorgroup.MostRecentlyActiveGroups) {
			if g != skip {
				targets = append(targets, editorgroup.CloseTarget{Group: g, Editors: g.Model.Editors(editorgroup.Sequential, true)})
			}
		}
		s.closeConfirmed(targets, nil)
		return nil
	})
	handlers["workbench.action.closeEditorInAllGroups"] = run(func() error {
		active := s.activeGroup().Model.ActiveEditor()
		if active == nil {
			return nil
		}
		var targets []editorgroup.CloseTarget
		for _, g := range grid().Groups(editorgroup.MostRecentlyActiveGroups) {
			if g.Model.Contains(active) {
				targets = append(targets, editorgroup.CloseTarget{Group: g, Editors: []editorgroup.EditorInput{g.Model.EditorAt(g.Model.IndexOf(active))}})
			}
		}
		s.closeConfirmed(targets, nil)
		return nil
	})

	// sizes
	handlers["workbench.action.evenEditorWidths"] = func(command.Context, any) error { grid().DistributeSizes(); return nil }
	handlers["workbench.action.toggleMaximizeEditorGroup"] = run(func() error { return grid().ToggleMaximize(s.activeGroup()) })

	// layouts
	two, three := []editorgroup.LayoutNode{{}, {}}, []editorgroup.LayoutNode{{}, {}, {}}
	for id, layout := range map[string]editorgroup.Layout{
		"workbench.action.editorLayoutSingle":           {Orientation: editorgroup.Horizontal, Groups: []editorgroup.LayoutNode{{}}},
		"workbench.action.editorLayoutTwoColumns":       {Orientation: editorgroup.Horizontal, Groups: two},
		"workbench.action.editorLayoutThreeColumns":     {Orientation: editorgroup.Horizontal, Groups: three},
		"workbench.action.editorLayoutTwoRows":          {Orientation: editorgroup.Vertical, Groups: two},
		"workbench.action.editorLayoutThreeRows":        {Orientation: editorgroup.Vertical, Groups: three},
		"workbench.action.editorLayoutTwoByTwoGrid":     {Orientation: editorgroup.Horizontal, Groups: []editorgroup.LayoutNode{{Groups: two}, {Groups: two}}},
		"workbench.action.editorLayoutTwoColumnsBottom": {Orientation: editorgroup.Vertical, Groups: []editorgroup.LayoutNode{{}, {Groups: two}}},
		"workbench.action.editorLayoutTwoRowsRight":     {Orientation: editorgroup.Horizontal, Groups: []editorgroup.LayoutNode{{}, {Groups: two}}},
	} {
		handlers[id] = run(func() error { return grid().ApplyLayout(layout) })
	}
	handlers["layoutEditorGroups"] = func(_ command.Context, args any) error {
		layout, err := parseLayout(args)
		if err != nil {
			return err
		}
		err = grid().ApplyLayout(layout)
		s.askFocusSoon()
		return err
	}
	return handlers
}

// groupTarget is the group a group moves next to in a direction: the neighbor on that side, or else
// on the sides across it (VS Code allows the target in the other places to support more scenarios of
// moving the group).
func (s *shellState) groupTarget(src *editorgroup.Group, dir editorgroup.Direction) *editorgroup.Group {
	sides := []editorgroup.Direction{dir, editorgroup.DirUp, editorgroup.DirDown}
	if dir == editorgroup.DirUp || dir == editorgroup.DirDown {
		sides = []editorgroup.Direction{dir, editorgroup.DirLeft, editorgroup.DirRight}
	}
	for _, d := range sides {
		if target := s.group.grid.FindByDirection(src, d, false); target != nil {
			return target
		}
	}
	return nil
}

// askFocusSoon asks for the keyboard focus to go to the active group after the next frame.
func (s *shellState) askFocusSoon() {
	if s.Mounted() {
		s.askFocus(widgets.WidgetsBindingOf(s.Context()))
	}
}

// moveCopyActive is the command moveActiveEditor (isMove) or copyActiveEditor: the argument {"to": ...,
// "by": "tab"|"group", "value": n} says where the active editor goes. By default it moves one tab to
// the right.
//
// Ported from VS Code: editorCommands.ts (moveCopySelectedEditors)
func (s *shellState) moveCopyActive(isMove bool, args any) error {
	to, by, value := editorgroup.TargetRight, "tab", 1
	if m, ok := args.(map[string]any); ok {
		if v, ok := m["to"].(string); ok && v != "" {
			to = editorgroup.Target(v)
		}
		if v, ok := m["by"].(string); ok && v != "" {
			by = v
		}
		if v, ok := m["value"].(float64); ok {
			value = int(v)
		}
	}
	if by != "tab" && by != "group" {
		return errors.New("EditorMoveArgumentInvalid", "the argument \"by\" of moveActiveEditor and copyActiveEditor is tab or group", "by", by)
	}
	src := s.activeGroup()
	editor := src.Model.ActiveEditor()
	if editor == nil {
		return nil
	}
	grid := s.group.grid
	if by == "tab" {
		if !isMove {
			return nil
		}
		return moveTab(src.Model, editor, to, value)
	}
	target, err := grid.MoveOrCopyTarget(src, to, value, s.sideBySideDirection())
	if err != nil || target == nil {
		return err
	}
	if isMove {
		if _, err := grid.MoveEditor(src, editor, target, editorgroup.MoveOptions{}); err != nil {
			return err
		}
	} else if target != src {
		if _, _, err := grid.CopyEditor(src, editor, target, editorgroup.MoveOptions{}); err != nil {
			return err
		}
	}
	err = grid.Activate(target)
	s.askFocusSoon()
	return err
}

// moveTab moves an editor within its group: to the first or last place, a number of places left or
// right, to the center, or to a place counted from 1.
//
// Ported from VS Code: editorCommands.ts (moveTab)
func moveTab(m *editorgroup.Model, editor editorgroup.EditorInput, to editorgroup.Target, value int) error {
	index := m.IndexOf(editor)
	switch to {
	case editorgroup.TargetFirst:
		index = 0
	case editorgroup.TargetLast:
		index = m.Count() - 1
	case editorgroup.TargetLeft:
		index -= value
	case editorgroup.TargetRight:
		index += value
	case editorgroup.TargetCenter:
		index = int(math.Round(float64(m.Count())/2)) - 1
	case editorgroup.TargetPosition:
		index = value - 1
	case editorgroup.TargetUp, editorgroup.TargetDown, editorgroup.TargetPrevious, editorgroup.TargetNext:
	default:
		return errors.New("EditorMoveArgumentInvalid", "the argument \"to\" of moveActiveEditor is not known", "to", string(to))
	}
	index = min(max(index, 0), m.Count()-1)
	m.MoveEditor(editor, index)
	m.Pin(editor)
	return nil
}

// parseLayout reads the argument of layoutEditorGroups: {"orientation": 0|1, "groups": [{"size": n,
// "groups": [...]}, ...]}. A wrong argument fails with the tag LayoutArgumentInvalid.
func parseLayout(args any) (editorgroup.Layout, error) {
	m, ok := args.(map[string]any)
	if !ok {
		return editorgroup.Layout{}, errors.New("LayoutArgumentInvalid", "layoutEditorGroups needs {\"orientation\": 0, \"groups\": [{}, {}]}")
	}
	var layout editorgroup.Layout
	if o, present := m["orientation"]; present {
		f, ok := o.(float64)
		if !ok || (f != 0 && f != 1) {
			return layout, errors.New("LayoutArgumentInvalid", "the orientation of a layout is 0 (horizontal) or 1 (vertical)", "orientation", o)
		}
		layout.Orientation = editorgroup.Orientation(int(f))
	}
	nodes, err := parseLayoutNodes(m["groups"])
	if err != nil {
		return layout, err
	}
	layout.Groups = nodes
	return layout, nil
}

func parseLayoutNodes(raw any) ([]editorgroup.LayoutNode, error) {
	list, ok := raw.([]any)
	if !ok || len(list) == 0 {
		return nil, errors.New("LayoutArgumentInvalid", "the groups of a layout are a list of at least one group")
	}
	nodes := make([]editorgroup.LayoutNode, len(list))
	for i, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, errors.New("LayoutArgumentInvalid", "a group of a layout is an object", "index", i)
		}
		if size, present := m["size"]; present {
			f, ok := size.(float64)
			if !ok || f < 0 {
				return nil, errors.New("LayoutArgumentInvalid", "the size of a group of a layout is a number", "index", i)
			}
			nodes[i].Size = f
		}
		if sub, present := m["groups"]; present {
			children, err := parseLayoutNodes(sub)
			if err != nil {
				return nil, err
			}
			nodes[i].Groups = children
		}
	}
	return nodes, nil
}

// setGroupHandlers binds handlers to commands of a registry and returns the functions that take them
// off again. It fails with the CommandNotFound of command.Registry.SetHandler for a command the registry does not know
// (the handlers bound so far are taken off).
func setGroupHandlers(reg *command.Registry, handlers map[string]command.Handler) ([]func(), error) {
	var removes []func()
	for id, h := range handlers {
		remove, err := reg.SetHandler(id, h)
		if err != nil {
			for _, r := range removes {
				r()
			}
			return nil, err
		}
		removes = append(removes, remove)
	}
	return removes, nil
}
