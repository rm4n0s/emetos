package settingsui

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// Metrics of a row of the settings tree, from VS Code's settingsEditor2.css.
const (
	rowPadTop         = 12.0
	rowPadSide        = 14.0
	rowPadBottom      = 18.0
	indicatorLeft     = 5.0
	indicatorTop      = 15.0
	indicatorBottom   = 18.0
	indicatorWidth    = 2.0
	titleFontSize     = 13.0
	controlGap        = 9.0
	numberInputWidth  = 120.0
	stringInputMax    = 600.0
	dropdownWidth     = 300.0
	messageFontSize   = 12.0
	gearSize          = 22.0
	menuResetLabel    = "Reset Setting"
	menuCopyIDLabel   = "Copy Setting ID"
	menuCopyJSONLabel = "Copy Setting as JSON"
)

// rowHost is what a row needs from the editor that shows it.
type rowHost interface {
	// set writes a value of a setting; setText writes the value of the text of an input box. Both report to
	// the user a write that fails.
	set(key string, value any)
	setText(key, text string)
	// reset removes the setting from the file of the tab.
	reset(key string)
	// openJSON opens the settings file of the tab, at the key when it can.
	openJSON(key string)
	// copyText puts text on the clipboard.
	copyText(text string)
	// settingJSON is the text "key": value of the setting as it is shown.
	settingJSON(row settings.Row) string
	// codeFont is the family of the code spans of the descriptions.
	codeFont() string
}

// settingRow is one setting in the list: the title with the gear, the description and the control for the kind of
// the setting. Its height is that of its content.
type settingRow struct {
	widgets.StatefulWidget
	row  settings.Row
	host rowHost
}

func newSettingRow(key foundation.Key, row settings.Row, host rowHost) *settingRow {
	return &settingRow{StatefulWidget: widgets.NewStatefulWidgetBase(widgets.WidgetOpts{Key: key}), row: row, host: host}
}

// CreateState implements widgets.StatefulWidget.
func (r *settingRow) CreateState() widgets.State {
	return &settingRowState{menu: kit.NewContextMenuController()}
}

type settingRowState struct {
	widgets.StateOf[*settingRow]
	hovered, focused bool
	// input holds the text of the input box of a number or a string; shown is the text the value of the
	// setting last gave it, so that a change of the value (and only that) replaces what is typed.
	input *kit.InputBoxController
	shown string
	// message is why the text of the input box is not allowed.
	message string
	menu    *kit.ContextMenuController
	gear    kit.Bounds
}

func hasTextInput(k settings.RowKind) bool {
	return k == settings.KindNumber || k == settings.KindInteger || k == settings.KindString
}

// InitState makes the input box.
func (s *settingRowState) InitState() {
	row := s.Widget().row
	if hasTextInput(row.Kind) {
		s.shown = row.ValueText()
		s.input = kit.NewInputBoxController(s.shown)
	}
}

// DidUpdateWidget puts a new value of the setting in the input box.
func (s *settingRowState) DidUpdateWidget(old *settingRow) {
	row := s.Widget().row
	if s.input == nil {
		return
	}
	if want := row.ValueText(); want != s.shown {
		s.shown, s.message = want, ""
		s.input.SetValue(want)
	}
}

// Dispose releases the controller of the input box.
func (s *settingRowState) Dispose() {
	if s.input != nil {
		s.input.Dispose()
	}
}

func (s *settingRowState) set(f func()) {
	if s.Mounted() {
		s.SetState(f)
	}
}

// typed checks the text while it is typed: the message of a text the setting does not allow shows at once.
func (s *settingRowState) typed(text string) {
	row := s.Widget().row
	msg := ""
	if row.Kind != settings.KindString {
		_, msg = row.Parse(text)
	}
	if msg != s.message {
		s.set(func() { s.message = msg })
	}
}

// commit writes the text of the input box when it is allowed and is not the value already: on Enter and when the
// box loses the focus. A text that is not allowed stays in the box with its message, and nothing is written.
func (s *settingRowState) commit() {
	w := s.Widget()
	text := s.input.Value()
	if text == s.shown {
		return
	}
	if _, msg := w.row.Parse(text); msg != "" {
		s.set(func() { s.message = msg })
		return
	}
	w.host.setText(w.row.Key, text)
}

// menuItems are the items of the gear and of the context menu of the row.
func (s *settingRowState) menuItems() []kit.MenuItem {
	w := s.Widget()
	row := w.row
	return []kit.MenuItem{
		{ID: "reset", Label: menuResetLabel, Disabled: !row.Modified, OnRun: func() { w.host.reset(row.Key) }},
		kit.SeparatorItem(),
		{ID: "copyID", Label: menuCopyIDLabel, OnRun: func() { w.host.copyText(row.Key) }},
		{ID: "copyJSON", Label: menuCopyJSONLabel, OnRun: func() { w.host.copyText(w.host.settingJSON(row)) }},
	}
}

// showGearMenu opens the menu of the gear below the gear.
func (s *settingRowState) showGearMenu() {
	at := ui.Offset{}
	if r, ok := s.gear.Rect(); ok {
		at = ui.Offset{Dx: r.Left, Dy: r.Bottom}
	}
	s.menu.Show(at, s.menuItems())
}

// Build implements widgets.State.
func (s *settingRowState) Build(bc widgets.BuildContext) widgets.Widget {
	w := s.Widget()
	row := w.row
	colors := kit.ThemeOf(bc)
	parts := []widgets.Widget{s.titleRow(bc, colors)}
	if row.Kind != settings.KindBool {
		parts = append(parts, descriptionWidget(bc, row.Description, w.host.codeFont(), colors))
	}
	if row.Deprecated != "" {
		parts = append(parts, widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 3, 0, 0),
			textOf(bc, row.Deprecated, descriptionFontSize, colors.ErrorForeground, 0)))
	}
	if row.Invalid != "" {
		parts = append(parts, widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 3, 0, 0),
			textOf(bc, "The value in settings.json is not allowed: "+row.Invalid+" Fix in JSON.", descriptionFontSize, colors.ErrorForeground, 0)))
	}
	parts = append(parts, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(controlGap)}), s.control(bc, colors))
	content := widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(rowPadSide, rowPadTop, rowPadSide, rowPadBottom),
		widgets.NewColumn(parts, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart}))

	layers := []widgets.Widget{content}
	if row.Modified {
		layers = append(layers, widgets.NewPositioned(
			widgets.NewColoredBox(colors.SettingsModifiedItemIndicator, widgets.NewSizedBox(nil)),
			widgets.PositionedOpts{Left: foundation.Some(indicatorLeft), Top: foundation.Some(indicatorTop), Bottom: foundation.Some(indicatorBottom), Width: foundation.Some(indicatorWidth)}))
	}
	var background ui.Color
	var border painting.Border
	switch {
	case s.focused:
		background = colors.SettingsFocusedRowBackground
		side := painting.NewBorderSide(painting.BorderSideOpts{Color: foundation.Some(colors.SettingsFocusedRowBorder), Width: foundation.Some(1.0)})
		border = painting.Border{Top: side, Bottom: side}
	case s.hovered:
		background = colors.SettingsRowHoverBackground
	}
	body := widgets.NewContainer(widgets.NewStack(layers, widgets.StackOpts{Fit: rendering.StackFitPassthrough}), widgets.ContainerOpts{
		Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{Color: foundation.Some(background), Border: border}),
	})
	var tree widgets.Widget = widgets.NewMouseRegion(body, widgets.MouseRegionOpts{
		Opaque:  foundation.Some(false),
		OnEnter: func(*gestures.PointerEnterEvent) { s.set(func() { s.hovered = true }) },
		OnExit:  func(*gestures.PointerExitEvent) { s.set(func() { s.hovered = false }) },
	})
	tree = widgets.NewFocus(tree, widgets.FocusOpts{
		CanRequestFocus: foundation.Some(false), SkipTraversal: foundation.Some(true), DebugLabel: "SettingRow",
		OnFocusChange: func(has bool) { s.set(func() { s.focused = has }) },
	})
	return kit.NewContextMenuRegion(tree, s.menuItems, kit.ContextMenuOpts{Controller: s.menu})
}

// titleRow is "Category: Label" in semibold, the note of the other file that sets the setting, and the gear.
func (s *settingRowState) titleRow(bc widgets.BuildContext, colors kit.Colors) widgets.Widget {
	w := s.Widget()
	row := w.row
	base := kit.TextStyleOf(bc, titleFontSize, colors.SettingsHeaderForeground).CopyWith(painting.TextStyleOpts{FontWeight: ui.FontWeightW600})
	var spans []painting.InlineSpan
	if row.Category != "" {
		cat := base.CopyWith(painting.TextStyleOpts{Color: foundation.Some(colors.SettingsHeaderForeground.WithOpacity(0.9))})
		spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(row.Category + ": "), Style: cat}))
	}
	spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(row.Label), Style: base}))
	if len(row.AlsoModifiedIn) > 0 {
		note := kit.TextStyleOf(bc, titleFontSize, colors.Foreground).CopyWith(painting.TextStyleOpts{FontStyle: ui.FontStyleItalic})
		text := "  (Also modified in: "
		for i, name := range row.AlsoModifiedIn {
			if i > 0 {
				text += ", "
			}
			text += name
		}
		spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(text + ")"), Style: note}))
	}
	title := widgets.NewRichText(painting.NewTextSpan(painting.TextSpanOpts{Children: spans}))
	var gear widgets.Widget = widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(gearSize), Height: foundation.Some(gearSize)})
	if s.hovered || s.focused || s.menu.IsShowing() {
		gear = kit.NewBoundsProbe(kit.NewIconButton(fonts.CodiconGear, kit.IconButtonOpts{
			OnPressed: s.showGearMenu, Tooltip: "More Actions...",
		}), &s.gear)
	}
	return widgets.NewRow([]widgets.Widget{widgets.NewExpanded(title), gear}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart})
}

// control is the widget that edits the value, for the kind of the setting.
func (s *settingRowState) control(bc widgets.BuildContext, colors kit.Colors) widgets.Widget {
	w := s.Widget()
	row := w.row
	switch row.Kind {
	case settings.KindBool:
		return s.boolControl(bc, colors)
	case settings.KindNumber, settings.KindInteger:
		over := colors
		over.InputBackground, over.InputForeground, over.InputBorder = colors.SettingsNumberInputBackground, colors.SettingsNumberInputForeground, colors.SettingsNumberInputBorder
		return s.inputControl(bc, over, foundation.Some(numberInputWidth))
	case settings.KindString:
		over := colors
		over.InputBackground, over.InputForeground, over.InputBorder = colors.SettingsTextInputBackground, colors.SettingsTextInputForeground, colors.SettingsTextInputBorder
		return s.inputControl(bc, over, foundation.Some(stringInputMax))
	case settings.KindEnum:
		return s.enumControl(bc, colors)
	}
	return s.jsonLink(bc, colors)
}

// themed shows a control with colors of its own.
func themed(bc widgets.BuildContext, colors kit.Colors, child widgets.Widget) widgets.Widget {
	return kit.NewTheme(colors, child, kit.ThemeOpts{UIFont: kit.UIFontOf(bc)})
}

// boolControl is the check box with the description at its right; a click on the text flips it too.
func (s *settingRowState) boolControl(bc widgets.BuildContext, colors kit.Colors) widgets.Widget {
	w := s.Widget()
	row := w.row
	over := colors
	over.CheckboxBackground, over.CheckboxForeground, over.CheckboxBorder = colors.SettingsCheckboxBackground, colors.SettingsCheckboxForeground, colors.SettingsCheckboxBorder
	flip := func() { w.host.set(row.Key, !row.Checked()) }
	box := themed(bc, over, kit.NewCheckbox("", kit.CheckboxOpts{Checked: row.Checked(), OnChanged: func(bool) { flip() }}))
	text := widgets.NewMouseRegion(
		widgets.NewGestureDetector(descriptionWidget(bc, row.Description, w.host.codeFont(), colors), widgets.GestureDetectorOpts{
			Behavior: rendering.HitTestBehaviorOpaque, OnTap: flip,
		}),
		widgets.MouseRegionOpts{Cursor: services.SystemMouseCursors.Click})
	return widgets.NewRow([]widgets.Widget{
		box, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(controlGap)}), widgets.NewExpanded(text),
	}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart})
}

// inputControl is the input box of a number or a string with the message of a text that is not allowed below it.
func (s *settingRowState) inputControl(bc widgets.BuildContext, colors kit.Colors, width foundation.Opt[float64]) widgets.Widget {
	box := themed(bc, colors, kit.NewInputBox(nil, nil, kit.InputBoxOpts{
		Controller: s.input, Width: width, Invalid: s.message != "",
		OnChanged: s.typed,
		OnSubmit:  func(string) { s.commit() },
		OnFocusChange: func(has bool) {
			if !has {
				s.commit()
			}
		},
	}))
	parts := []widgets.Widget{box}
	if s.message != "" {
		parts = append(parts, s.validationMessage(bc, colors))
	}
	return widgets.NewColumn(parts, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart, MainAxisSize: rendering.MainAxisSizeMin})
}

// validationMessage is the box with the reason a text is not allowed, in the colors of an error of an input.
func (s *settingRowState) validationMessage(bc widgets.BuildContext, colors kit.Colors) widgets.Widget {
	fg := colors.InputValidationErrorForeground
	if fg.Alpha() == 0 {
		fg = colors.Foreground
	}
	return widgets.NewContainer(textOf(bc, s.message, messageFontSize, fg, 0), widgets.ContainerOpts{
		Margin:  painting.NewEdgeInsetsFromLTRB(0, 1, 0, 0),
		Padding: painting.NewEdgeInsetsAll(4.8),
		Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
			Color:  foundation.Some(colors.InputValidationErrorBackground),
			Border: painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(colors.InputValidationErrorBorder), Width: foundation.Some(1.0)}),
		}),
	})
}

// enumControl is the list of the values the setting allows.
func (s *settingRowState) enumControl(bc widgets.BuildContext, colors kit.Colors) widgets.Widget {
	w := s.Widget()
	row := w.row
	over := colors
	over.DropdownBackground, over.DropdownForeground, over.DropdownBorder = colors.SettingsDropdownBackground, colors.SettingsDropdownForeground, colors.SettingsDropdownBorder
	options := make([]kit.DropdownOption, len(row.Choices))
	for i, c := range row.Choices {
		options[i] = kit.DropdownOption{Label: c.Label, Description: c.Description}
	}
	return themed(bc, over, kit.NewDropdown(options, kit.DropdownOpts{
		Selected: row.ChoiceIndex(), Width: foundation.Some(dropdownWidth),
		OnChanged: func(i int) { w.host.set(row.Key, row.Choices[i].Value) },
	}))
}

// jsonLink is "Edit in settings.json", for the values the editor does not edit itself.
func (s *settingRowState) jsonLink(bc widgets.BuildContext, colors kit.Colors) widgets.Widget {
	w := s.Widget()
	style := kit.TextStyleOf(bc, descriptionFontSize, colors.TextLinkForeground).CopyWith(painting.TextStyleOpts{Decoration: foundation.Some(ui.TextDecorationUnderline)})
	return widgets.NewMouseRegion(
		widgets.NewGestureDetector(widgets.NewText(editInJSONLabel, widgets.TextOpts{Style: style}), widgets.GestureDetectorOpts{
			Behavior: rendering.HitTestBehaviorOpaque, OnTap: func() { w.host.openJSON(w.row.Key) },
		}),
		widgets.MouseRegionOpts{Cursor: services.SystemMouseCursors.Click})
}

// editInJSONLabel is the text of the link of a value the editor does not edit itself.
const editInJSONLabel = "Edit in settings.json"
