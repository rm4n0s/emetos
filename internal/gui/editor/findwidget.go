package editor

import (
	"strconv"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/find"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// Ported from VS Code: src/vs/editor/contrib/find/browser/findWidget.ts

// Metrics of the find widget (findWidget.css).
const (
	findWidgetWidth       = 419.0
	findWidgetFontSize    = 12.0
	findWidgetMatchesWide = 69.0
	findWidgetToggleWide  = 22.0
	findWidgetRowGap      = 3.0
	findWidgetCorner      = 4.0
)

// noResults is the text of the match count when there is none.
const noResults = "No results"

// findWidget is the find and replace widget of an editor: the find input with its switches (match
// case, whole word, regular expression), the number of the match, the buttons that go to the
// previous and next match, find in selection and close, and the replace row (SPEC §9.8).
type findWidget struct {
	widgets.StatefulWidget
	vm                    *ViewModel
	findNode, replaceNode *widgets.FocusNode
	// find and replace hold the text of the inputs; the editor owns them, to select their text.
	find, replace *widgets.TextEditingController
	// onFocus is told when an input gets or loses the focus.
	onFocus func()
	// onError is told about the errors of the commands the buttons run (replacing in a read-only
	// document).
	onError func(error)
}

func newFindWidget(vm *ViewModel, findNode, replaceNode *widgets.FocusNode, find, replace *widgets.TextEditingController, onFocus func(), onError func(error)) *findWidget {
	return &findWidget{
		StatefulWidget: widgets.NewStatefulWidgetBase(), vm: vm, findNode: findNode, replaceNode: replaceNode,
		find: find, replace: replace, onFocus: onFocus, onError: onError,
	}
}

// CreateState implements widgets.StatefulWidget.
func (w *findWidget) CreateState() widgets.State { return &findWidgetState{} }

type findWidgetState struct {
	widgets.StateOf[*findWidget]
	handle foundation.ListenerHandle
}

// InitState follows the find state.
func (s *findWidgetState) InitState() {
	s.handle = s.Widget().vm.AddFindListener(s.stateChanged)
}

// DidUpdateWidget follows the find state of another view model.
func (s *findWidgetState) DidUpdateWidget(old *findWidget) {
	if old.vm == s.Widget().vm {
		return
	}
	old.vm.RemoveFindListener(s.handle)
	s.handle = s.Widget().vm.AddFindListener(s.stateChanged)
	s.sync()
}

// Dispose stops following the find state.
func (s *findWidgetState) Dispose() { s.Widget().vm.RemoveFindListener(s.handle) }

// sync makes the inputs show the strings of the state.
func (s *findWidgetState) sync() {
	st := s.Widget().vm.FindState()
	if find := s.Widget().find; find.Text() != st.SearchString() {
		find.SetText(st.SearchString())
	}
	if replace := s.Widget().replace; replace.Text() != st.ReplaceString() {
		replace.SetText(st.ReplaceString())
	}
}

func (s *findWidgetState) stateChanged() {
	if !s.Mounted() {
		return
	}
	s.sync()
	s.SetState(func() {})
}

func (s *findWidgetState) focusChanged(bool) {
	if f := s.Widget().onFocus; f != nil {
		f()
	}
}

func (s *findWidgetState) findChanged(text string) {
	s.Widget().vm.FindState().Change(find.NewState{SearchString: find.Some(text)}, true, true)
}

func (s *findWidgetState) replaceChanged(text string) {
	s.Widget().vm.FindState().Change(find.NewState{ReplaceString: find.Some(text)}, false, true)
}

// run runs an editor command.
func (s *findWidgetState) run(id string) {
	if err := s.Widget().vm.RunCommand(id, nil); err != nil && s.Widget().onError != nil {
		s.Widget().onError(err)
	}
}

// toggle changes a switch of the state; the focus stays where it was, since the buttons of the
// widget do not take it.
func (s *findWidgetState) toggle(change find.NewState) {
	s.Widget().vm.FindState().Change(change, false, true)
}

// matchesLabel is the text of the match count (VS Code's _updateMatchesCount).
func matchesLabel(st *find.State) string {
	if st.MatchesCount() <= 0 {
		return noResults
	}
	count := strconv.Itoa(st.MatchesCount())
	if st.MatchesCount() >= find.MatchesLimit {
		count += "+"
	}
	position := strconv.Itoa(st.MatchesPosition())
	if position == "0" {
		position = "?"
	}
	return position + " of " + count
}

// Build implements widgets.State.
func (s *findWidgetState) Build(bc widgets.BuildContext) widgets.Widget {
	vm := s.Widget().vm
	st := vm.FindState()
	if !st.IsRevealed() {
		return widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(0.0), Height: foundation.Some(0.0)})
	}
	colors := kit.ThemeOf(bc)
	style := kit.TextStyleOf(bc, findWidgetFontSize, colors.EditorWidgetForeground)
	readOnly := vm.ReadOnly()
	nonEmpty := st.SearchString() != ""
	noMatches := nonEmpty && st.MatchesCount() == 0 && !st.IsSearching()

	findToggle := func(code int, checked bool, change func(bool) find.NewState) widgets.Widget {
		return kit.NewToggle(code, kit.ToggleOpts{NotFocusable: true, Checked: checked, OnChanged: func(v bool) { s.toggle(change(v)) }})
	}
	findInput := kit.NewInputBox(s.Widget().find, s.Widget().findNode, kit.InputBoxOpts{
		Placeholder: "Find", Invalid: noMatches, OnChanged: s.findChanged, OnFocusChange: s.focusChanged,
		Trailing: []widgets.Widget{
			findToggle(fonts.CodiconCaseSensitive, st.MatchCase(), func(v bool) find.NewState { return find.NewState{MatchCase: find.Some(v)} }),
			findToggle(fonts.CodiconWholeWord, st.WholeWord(), func(v bool) find.NewState { return find.NewState{WholeWord: find.Some(v)} }),
			findToggle(fonts.CodiconRegex, st.IsRegex(), func(v bool) find.NewState { return find.NewState{IsRegex: find.Some(v)} }),
		},
	})
	count := widgets.NewContainer(
		widgets.NewText(matchesLabel(st), widgets.TextOpts{Style: style, MaxLines: foundation.Some(1), Overflow: painting.TextOverflowClip}),
		// as wide as its text, and at least as wide as VS Code makes it (min-width)
		widgets.ContainerOpts{Constraints: foundation.Some(rendering.NewBoxConstraints(rendering.BoxConstraintsOpts{MinWidth: foundation.Some(findWidgetMatchesWide)})),
			Alignment: painting.AlignmentCenter,
			Padding:   painting.NewEdgeInsetsFromLTRB(2, 0, 0, 0), Margin: painting.NewEdgeInsetsFromLTRB(findWidgetRowGap, 0, 0, 0)})
	button := func(code int, enabled bool, id string) widgets.Widget {
		opts := kit.IconButtonOpts{Disabled: !enabled}
		if enabled {
			opts.OnPressed = func() { s.run(id) }
		}
		return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(findWidgetRowGap, 0, 0, 0), kit.NewIconButton(code, opts))
	}
	inSelection := widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(findWidgetRowGap+1, 0, 0, 0),
		kit.NewToggle(fonts.CodiconListSelection, kit.ToggleOpts{NotFocusable: true, Checked: len(st.SearchScope()) > 0, OnChanged: func(bool) { s.run("toggleFindInSelection") }}))
	findRow := widgets.NewRow([]widgets.Widget{
		widgets.NewExpanded(findInput),
		count,
		button(fonts.CodiconArrowUp, nonEmpty && st.CanNavigateBack(), "editor.action.previousMatchFindAction"),
		button(fonts.CodiconArrowDown, nonEmpty && st.CanNavigateForward(), "editor.action.nextMatchFindAction"),
		inSelection,
		button(fonts.CodiconClose, true, "closeFindWidget"),
	}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})

	rows := []widgets.Widget{findRow}
	if st.IsReplaceRevealed() {
		replaceInput := kit.NewInputBox(s.Widget().replace, s.Widget().replaceNode, kit.InputBoxOpts{
			Placeholder: "Replace", OnChanged: s.replaceChanged, OnFocusChange: s.focusChanged,
			Trailing: []widgets.Widget{
				findToggle(fonts.CodiconPreserveCase, st.PreserveCase(), func(v bool) find.NewState { return find.NewState{PreserveCase: find.Some(v)} }),
			},
		})
		replaceRow := widgets.NewRow([]widgets.Widget{
			widgets.NewExpanded(replaceInput),
			button(fonts.CodiconReplace, nonEmpty && !readOnly, "editor.action.replaceOne"),
			button(fonts.CodiconReplaceAll, nonEmpty && !readOnly, "editor.action.replaceAll"),
			// as wide as the buttons after the count in the find row, so that the inputs line up
			widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(findWidgetToggleWide + 2*findWidgetRowGap + 1)}),
		}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
		rows = append(rows, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(findWidgetRowGap)}), replaceRow)
	}
	// The chevron that shows and hides the replace row.
	chevron := fonts.CodiconChevronRight
	if st.IsReplaceRevealed() {
		chevron = fonts.CodiconChevronDown
	}
	toggleReplace := kit.NewIconButton(chevron, kit.IconButtonOpts{Disabled: readOnly, OnPressed: func() {
		s.toggle(find.NewState{IsReplaceRevealed: find.Some(!st.IsReplaceRevealed())})
	}})
	content := widgets.NewRow([]widgets.Widget{
		widgets.NewAlign(widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 0, 0, 0), toggleReplace), widgets.AlignOpts{Alignment: painting.AlignmentTopCenter}),
		widgets.NewExpanded(widgets.NewColumn(rows, widgets.ColumnOpts{MainAxisSize: rendering.MainAxisSizeMin})),
	}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart})

	border := painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(colors.WidgetBorder), Width: foundation.Some(1.0)})
	if colors.WidgetBorder.Alpha() == 0 {
		border = painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(ui.NewColor(0)), Width: foundation.Some(0.0)})
	}
	box := widgets.NewContainer(content, widgets.ContainerOpts{
		Padding: painting.NewEdgeInsetsFromLTRB(2, findWidgetRowGap, 4, findWidgetRowGap),
		Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
			Color: foundation.Some(colors.EditorWidgetBackground), Border: border, BorderRadius: painting.NewBorderRadiusCircular(findWidgetCorner),
			BoxShadow: []painting.BoxShadow{painting.NewBoxShadow(painting.BoxShadowOpts{Color: foundation.Some(colors.WidgetShadow), Offset: ui.Offset{Dy: 2}, BlurRadius: 8})},
		}),
	})
	return widgets.NewLayoutBuilder(func(_ widgets.BuildContext, c rendering.BoxConstraints) widgets.Widget {
		width := min(findWidgetWidth, max(c.MaxWidth, 0))
		return widgets.NewDefaultTextStyle(style, widgets.NewGestureDetector(
			widgets.NewSizedBox(box, widgets.SizedBoxOpts{Width: foundation.Some(width)}),
			widgets.GestureDetectorOpts{Behavior: rendering.HitTestBehaviorOpaque}))
	})
}
