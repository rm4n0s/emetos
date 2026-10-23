package editor

import (
	"slices"
	"strconv"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/folding"
	"github.com/rm4n0s/emetos/internal/core/regex"
)

// Ported from VS Code: src/vs/editor/contrib/folding/browser/folding.ts (the actions)

// foldArguments are the arguments of the folding commands: how many levels, the direction and
// the lines to work on instead of the first lines of the selections.
type foldArguments struct {
	levels    int
	hasLevels bool
	up        bool
	lines     []int
	hasLines  bool
}

// parseFoldArguments reads {"levels": n, "direction": "up" | "down", "selectionLines": [n, ...]}
// (the lines zero-based); anything else is no arguments.
func parseFoldArguments(args any) foldArguments {
	var a foldArguments
	m, ok := args.(map[string]any)
	if !ok {
		return a
	}
	if v, ok := m["levels"].(float64); ok {
		a.levels, a.hasLevels = int(v), true
	}
	if v, ok := m["direction"].(string); ok {
		a.up = v == "up"
	}
	if list, ok := m["selectionLines"].([]any); ok {
		a.hasLines = true
		for _, item := range list {
			if v, ok := item.(float64); ok {
				a.lines = append(a.lines, int(v))
			}
		}
	}
	return a
}

// foldingActions adds the folding commands to the table of the editor's commands.
func foldingActions(a map[string]action) {
	fold := func(id string, f func(vm *ViewModel, m *folding.Model, lines []int, args foldArguments)) {
		a[id] = func(vm *ViewModel, args any) error {
			if !vm.foldingEnabled() {
				return nil
			}
			parsed := parseFoldArguments(args)
			lines := parsed.lines
			if !parsed.hasLines {
				lines = vm.selectedLines()
			}
			vm.fold.busy++
			defer func() { vm.fold.busy-- }()
			f(vm, vm.fold.model, lines, parsed)
			return nil
		}
	}
	fold("editor.fold", func(_ *ViewModel, m *folding.Model, lines []int, args foldArguments) {
		folding.Fold(m, lines, args.levels, args.hasLevels, args.up)
	})
	fold("editor.unfold", func(_ *ViewModel, m *folding.Model, lines []int, args foldArguments) {
		folding.Unfold(m, lines, args.levels, args.up)
	})
	fold("editor.toggleFold", func(_ *ViewModel, m *folding.Model, lines []int, _ foldArguments) { folding.Toggle(m, lines) })
	fold("editor.foldRecursively", func(_ *ViewModel, m *folding.Model, lines []int, _ foldArguments) {
		folding.FoldRecursively(m, lines)
	})
	fold("editor.unfoldRecursively", func(_ *ViewModel, m *folding.Model, lines []int, _ foldArguments) {
		folding.UnfoldRecursively(m, lines)
	})
	fold("editor.toggleFoldRecursively", func(_ *ViewModel, m *folding.Model, lines []int, _ foldArguments) {
		folding.ToggleRecursively(m, lines)
	})
	fold("editor.foldAll", func(_ *ViewModel, m *folding.Model, _ []int, _ foldArguments) { folding.FoldAll(m) })
	fold("editor.unfoldAll", func(_ *ViewModel, m *folding.Model, _ []int, _ foldArguments) { folding.UnfoldAll(m) })
	for level := 1; level <= 7; level++ {
		fold("editor.foldLevel"+strconv.Itoa(level), func(_ *ViewModel, m *folding.Model, lines []int, _ foldArguments) {
			folding.FoldLevel(m, level, lines)
		})
	}
	fold("editor.foldAllExcept", func(_ *ViewModel, m *folding.Model, lines []int, _ foldArguments) {
		folding.FoldAllExcept(m, lines)
	})
	fold("editor.unfoldAllExcept", func(_ *ViewModel, m *folding.Model, lines []int, _ foldArguments) {
		folding.UnfoldAllExcept(m, lines)
	})
	fold("editor.foldAllBlockComments", func(vm *ViewModel, m *folding.Model, _ []int, _ foldArguments) {
		start := ""
		if vm.comments != nil {
			if l := vm.comments.Language(vm.languageCode); l != nil {
				start = l.BlockStart
			}
		}
		folding.FoldAllBlockComments(m, start)
	})
	fold("editor.foldAllMarkerRegions", func(vm *ViewModel, m *folding.Model, _ []int, _ foldArguments) {
		folding.FoldAllMarkerRegions(m, vm.markerStart())
	})
	fold("editor.unfoldAllMarkerRegions", func(vm *ViewModel, m *folding.Model, _ []int, _ foldArguments) {
		folding.UnfoldAllMarkerRegions(m, vm.markerStart())
	})

	jump := func(id string, target func(line int, m *folding.Model) (int, bool)) {
		a[id] = func(vm *ViewModel, args any) error {
			if !vm.foldingEnabled() {
				return nil
			}
			parsed := parseFoldArguments(args)
			lines := parsed.lines
			if !parsed.hasLines {
				lines = vm.selectedLines()
			}
			var sels []buffer.Selection
			for _, line := range lines {
				if to, ok := target(line, vm.fold.model); ok {
					sels = append(sels, buffer.NewCursor(buffer.Position{Line: to}))
				}
			}
			if len(sels) > 0 {
				vm.setSelections(sels)
			}
			return nil
		}
	}
	jump("editor.gotoParentFold", folding.ParentFoldLine)
	jump("editor.gotoPreviousFold", folding.PreviousFoldLine)
	jump("editor.gotoNextFold", folding.NextFoldLine)

	a["editor.createFoldingRangeFromSelection"] = func(vm *ViewModel, _ any) error {
		if !vm.foldingEnabled() {
			return nil
		}
		return vm.createFoldingRanges()
	}
	a["editor.removeManualFoldingRanges"] = func(vm *ViewModel, _ any) error {
		if !vm.foldingEnabled() {
			return nil
		}
		sels := vm.cursors.Selections()
		ranges := make([]buffer.Range, len(sels))
		for i, s := range sels {
			ranges[i] = s.Range()
		}
		vm.fold.busy++
		defer func() { vm.fold.busy-- }()
		return vm.fold.model.RemoveManualRanges(ranges)
	}
}

// markerStart is the pattern of the lines that start a region in the language of the document,
// or nil.
func (vm *ViewModel) markerStart() *regex.Regexp {
	if vm.fold.markers == nil {
		return nil
	}
	return vm.fold.markers.Start
}

// createFoldingRanges makes a folded region of the lines of each selection that spans more than
// one line, and puts the cursor on the first line of the last of them (Create Manual Folding
// Ranges from Selection).
func (vm *ViewModel) createFoldingRanges() error {
	var ranges []folding.Range
	var cursor *buffer.Position
	for _, s := range vm.cursors.Selections() {
		start, end := s.Start(), s.End()
		last := end.Line
		if end.Col == 0 {
			last--
		}
		if last > start.Line {
			ranges = append(ranges, folding.Range{Start: start.Line, End: last, Collapsed: true, Source: folding.UserDefined})
			p := buffer.Position{Line: start.Line}
			cursor = &p
		}
	}
	if len(ranges) == 0 {
		return nil
	}
	slices.SortFunc(ranges, func(a, b folding.Range) int { return a.Start - b.Start })
	m := vm.fold.model
	merged := folding.SanitizeAndMerge(m.Regions().Ranges(), ranges, vm.doc.Snapshot().LineCount()-1, nil)
	regions, err := folding.FromRanges(merged)
	if err != nil {
		return err
	}
	vm.fold.busy++
	defer func() { vm.fold.busy-- }()
	vm.setSelections([]buffer.Selection{buffer.NewCursor(*cursor)})
	m.UpdatePost(regions)
	return nil
}
