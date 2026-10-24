package kit

import (
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/theme"
)

// Colors are the workbench colors the widgets read, named as VS Code names them in a color
// theme's "colors". ColorsOf takes them from a theme of the theme service.
type Colors struct {
	EditorBackground                  ui.Color
	EditorForeground                  ui.Color
	EditorLineHighlightBackground     ui.Color
	EditorLineHighlightBorder         ui.Color
	EditorSelectionBackground         ui.Color
	EditorInactiveSelectionBackground ui.Color
	EditorCursorForeground            ui.Color
	EditorLineNumberForeground        ui.Color
	EditorLineNumberActiveForeground  ui.Color
	EditorGutterBackground            ui.Color
	EditorWhitespaceForeground        ui.Color
	EditorIndentGuideBackground1      ui.Color
	// EditorIndentGuideBackground are the colors of the indent guides by level, and
	// EditorIndentGuideActiveBackground those of the active guide.
	EditorIndentGuideBackground, EditorIndentGuideActiveBackground [6]ui.Color
	// EditorBracketPairGuideBackground and EditorBracketPairGuideActiveBackground are the
	// colors of the bracket pair guides by level; a transparent one is made of the color of the
	// brackets of that level.
	EditorBracketPairGuideBackground, EditorBracketPairGuideActiveBackground [6]ui.Color
	EditorRulerForeground                                                    ui.Color
	// EditorFoldBackground is the background of a folded line, EditorFoldPlaceholderForeground the
	// color of the "…" after it and EditorGutterFoldingControlForeground the color of the folding
	// controls in the gutter.
	EditorFoldBackground, EditorFoldPlaceholderForeground, EditorGutterFoldingControlForeground ui.Color
	EditorBracketMatchBackground                                                                ui.Color
	EditorBracketMatchBorder                                                                    ui.Color
	EditorBracketMatchForeground                                                                ui.Color
	// EditorBracketHighlightForeground are the colors of the levels of brackets, from the
	// first; a level past the sixth takes the color of the one that repeats it. A transparent
	// color leaves the bracket in the color of its token.
	EditorBracketHighlightForeground [6]ui.Color
	// EditorBracketUnexpectedForeground is the color of a closing bracket that closes
	// nothing and of an opening bracket that is never closed.
	EditorBracketUnexpectedForeground ui.Color
	ScrollbarSliderBackground         ui.Color
	ScrollbarSliderHoverBackground    ui.Color
	ScrollbarSliderActiveBackground   ui.Color
	ScrollbarShadow                   ui.Color
	// ScrollbarBackground is the color of the track of a scrollbar (transparent in the
	// bundled themes) and SashHoverBorder the color of a sash under the pointer or dragged.
	ScrollbarBackground, SashHoverBorder ui.Color
	FocusBorder                          ui.Color
	Foreground                           ui.Color
	WidgetShadow                         ui.Color

	// EditorFindMatchBackground and its border color the current match of the find widget,
	// EditorFindMatchHighlightBackground and its border the other matches and
	// EditorFindRangeHighlightBackground and its border the lines of the current match and the
	// range a search is limited to.
	EditorFindMatchBackground, EditorFindMatchBorder                   ui.Color
	EditorFindMatchHighlightBackground, EditorFindMatchHighlightBorder ui.Color
	EditorFindRangeHighlightBackground, EditorFindRangeHighlightBorder ui.Color
	// EditorSelectionHighlightBackground and its border color the text that equals the selection;
	// EditorWordHighlightTextBackground and its border the other occurrences of the word at the
	// cursor.
	EditorSelectionHighlightBackground, EditorSelectionHighlightBorder ui.Color
	EditorWordHighlightTextBackground, EditorWordHighlightTextBorder   ui.Color
	// EditorSnippetTabstopHighlightBackground and its border color the tab stops of a snippet that
	// is being filled in; the Final ones color the final tab stop ($0).
	EditorSnippetTabstopHighlightBackground, EditorSnippetTabstopHighlightBorder           ui.Color
	EditorSnippetFinalTabstopHighlightBackground, EditorSnippetFinalTabstopHighlightBorder ui.Color
	// MinimapBackground (transparent: the editor's), MinimapForegroundOpacity (the alpha is the
	// opacity of the blocks), MinimapSelection, MinimapFindMatch and MinimapSelectionOccurrence
	// (the marks of the selection, the find matches and the text equal to the selection) and the
	// three colors of the minimap's slider.
	MinimapBackground, MinimapForegroundOpacity                                ui.Color
	MinimapSelection, MinimapFindMatch, MinimapSelectionOccurrence             ui.Color
	MinimapSliderBackground, MinimapSliderHoverBackground, MinimapSliderActive ui.Color
	// EditorStickyScrollBackground, EditorStickyScrollGutterBackground and
	// EditorStickyScrollHoverBackground are the backgrounds of sticky scroll (its lines, its gutter
	// and a line under the pointer), EditorStickyScrollBorder its bottom border and
	// EditorStickyScrollShadow its shadow.
	EditorStickyScrollBackground, EditorStickyScrollGutterBackground, EditorStickyScrollHoverBackground ui.Color
	EditorStickyScrollBorder, EditorStickyScrollShadow                                                  ui.Color
	// EditorOverviewRulerFindMatch and EditorOverviewRulerRangeHighlight are the marks of the
	// matches and of the range highlight on the vertical scrollbar.
	EditorOverviewRulerFindMatch, EditorOverviewRulerRangeHighlight ui.Color
	// EditorWidgetBackground, EditorWidgetForeground and WidgetBorder are the colors of widgets
	// over the editor (find).
	EditorWidgetBackground, EditorWidgetForeground, WidgetBorder ui.Color
	// IconForeground is the color of icons; ToolbarHoverBackground the background of an icon
	// button under the pointer.
	IconForeground, ToolbarHoverBackground ui.Color
	// The colors of input boxes and of the toggles in them.
	InputBackground, InputForeground, InputBorder, InputPlaceholderForeground         ui.Color
	InputOptionActiveBorder, InputOptionActiveBackground, InputOptionActiveForeground ui.Color
	InputOptionHoverBackground                                                        ui.Color
	InputValidationErrorBorder                                                        ui.Color
	// The colors of lists: the focused row (ListActiveSelectionBackground and Foreground), a row
	// under the pointer and the outline of the focused row.
	ListActiveSelectionBackground, ListActiveSelectionForeground, ListHoverBackground, ListFocusOutline ui.Color
	// The colors of the rows of a list or tree beyond those: the focused row of an active one (text,
	// background and, when it is selected too, its outline), the selected row of one that does not have
	// the focus (text and background), and its focused row (background and outline); the strokes of a
	// tree's indent guides, the active ones and the others. A transparent one (the usual default)
	// leaves the row as it is.
	ListFocusForeground, ListFocusBackground, ListFocusAndSelectionOutline ui.Color
	ListInactiveSelectionForeground, ListInactiveSelectionBackground       ui.Color
	ListInactiveFocusBackground, ListInactiveFocusOutline                  ui.Color
	TreeIndentGuidesStroke, TreeInactiveIndentGuidesStroke                 ui.Color
	// The colors of the drag and drop of lists and trees: the background of a row that something is
	// dropped on and the line between two rows (ListDropBackground, ListDropBetweenBackground); of the
	// find: the text of the matches in a label (ListHighlightForeground, and ListFocusHighlightForeground
	// in a row that is focused in an active list), the background and border of the matches in the filter
	// mode (ListFilterMatchBackground, ListFilterMatchBorder); and the find box (ListFilterWidgetBackground,
	// its outline and the outline when nothing matches, and its shadow).
	ListDropBackground, ListDropBetweenBackground            ui.Color
	ListHighlightForeground, ListFocusHighlightForeground    ui.Color
	ListFilterMatchBackground, ListFilterMatchBorder         ui.Color
	ListFilterWidgetBackground, ListFilterWidgetOutline      ui.Color
	ListFilterWidgetNoMatchesOutline, ListFilterWidgetShadow ui.Color
	// The colors of the breadcrumbs bar: its items, the item that is hovered or has its picker open
	// (Focus and ActiveSelection), the background of the bar and the one of the picker.
	// DescriptionForeground is the color of descriptive text, and the Button colors are those of
	// buttons: text, background, background under the pointer and border.
	DescriptionForeground                                                   ui.Color
	ButtonForeground, ButtonBackground, ButtonHoverBackground, ButtonBorder ui.Color
	BreadcrumbForeground, BreadcrumbBackground, BreadcrumbFocusForeground   ui.Color
	BreadcrumbActiveSelectionForeground, BreadcrumbPickerBackground         ui.Color
	// The colors of secondary buttons: text, background, background under the pointer and border.
	ButtonSecondaryForeground, ButtonSecondaryBackground, ButtonSecondaryHoverBackground, ButtonSecondaryBorder ui.Color
	// DisabledForeground is the color of the text of disabled elements, ToolbarActiveBackground the
	// background of a toolbar action while the pointer is pressed on it and ActionBarToggledBackground
	// the one of a toggled action.
	DisabledForeground, ToolbarActiveBackground, ActionBarToggledBackground ui.Color
	// The colors of hovers (tooltips): background, text and border.
	EditorHoverWidgetBackground, EditorHoverWidgetForeground, EditorHoverWidgetBorder ui.Color
	// The colors of menus: text, background and border, the focused item (text, background and
	// border) and the separator.
	MenuForeground, MenuBackground, MenuBorder                            ui.Color
	MenuSelectionForeground, MenuSelectionBackground, MenuSelectionBorder ui.Color
	MenuSeparatorBackground                                               ui.Color
	// The colors of the menu bar: the titles are in TitleBarActiveForeground, and the title that is
	// open, focused or under the pointer has the Selection colors.
	TitleBarActiveForeground                                                       ui.Color
	MenubarSelectionForeground, MenubarSelectionBackground, MenubarSelectionBorder ui.Color
	// The colors of the validation messages of an input box and of the border it takes with one, by
	// severity: background, text (transparent where the theme sets none: the text then keeps the
	// workbench's foreground) and border. InputValidationErrorBorder is above.
	InputValidationInfoBackground, InputValidationInfoForeground, InputValidationInfoBorder          ui.Color
	InputValidationWarningBackground, InputValidationWarningForeground, InputValidationWarningBorder ui.Color
	InputValidationErrorBackground, InputValidationErrorForeground                                   ui.Color
	// The colors of the dropdown: the field (DropdownBackground, DropdownForeground, DropdownBorder)
	// and its list (DropdownListBackground, transparent where the theme sets none: the list then
	// takes DropdownBackground).
	DropdownBackground, DropdownForeground, DropdownBorder, DropdownListBackground ui.Color
	// The colors of checkboxes: background, text (the check mark), border, the background and border
	// in a row that is selected, and the background and text of a disabled one.
	CheckboxBackground, CheckboxForeground, CheckboxBorder ui.Color
	CheckboxSelectBackground, CheckboxSelectBorder         ui.Color
	CheckboxDisabledBackground, CheckboxDisabledForeground ui.Color
	// BadgeBackground and BadgeForeground color a count badge, ProgressBarBackground a progress bar.
	BadgeBackground, BadgeForeground, ProgressBarBackground ui.Color
	// The colors of the icon of a dialog by type (error, warning, information).
	ProblemsErrorIconForeground, ProblemsWarningIconForeground, ProblemsInfoIconForeground ui.Color
	// TextLinkForeground is the color of a link in text.
	TextLinkForeground ui.Color
	// The colors of toasts and of the notification center: the text and background of a notification,
	// its border as a toast and the border of the center, the header of the center (text and
	// background), the line between notifications of the center, and the icons by severity.
	NotificationsForeground, NotificationsBackground, NotificationToastBorder, NotificationCenterBorder   ui.Color
	NotificationCenterHeaderForeground, NotificationCenterHeaderBackground, NotificationsBorder           ui.Color
	NotificationsErrorIconForeground, NotificationsWarningIconForeground, NotificationsInfoIconForeground ui.Color
	// The colors of the parts of the workbench (gui/workbench). The status bar: text, background
	// and border with a folder open, and the same three with none (the NoFolder colors). The
	// activity bar: background and border. The side bar: background, text (transparent where the
	// theme sets none: the text then keeps the workbench's foreground) and border, and the same
	// for its title. The panel: background and border, and for its title the border under it, the
	// text of the active and of the other tabs and the line under the active tab. The title bar:
	// background and border, which the menu bar area has.
	StatusBarForeground, StatusBarBackground, StatusBarBorder                         ui.Color
	StatusBarNoFolderForeground, StatusBarNoFolderBackground, StatusBarNoFolderBorder ui.Color
	ActivityBarBackground, ActivityBarBorder                                          ui.Color
	// The icons of the activity bar: the active one, the others, the line beside the active one (and
	// its focus outline), and the background of the active one. The header of a view in the side
	// bar: background, text and the line above it.
	ActivityBarForeground, ActivityBarInactiveForeground, ActivityBarActiveBorder    ui.Color
	ActivityBarActiveFocusBorder, ActivityBarActiveBackground                        ui.Color
	SideBarSectionHeaderBackground, SideBarSectionHeaderForeground                   ui.Color
	SideBarSectionHeaderBorder                                                       ui.Color
	SideBarBackground, SideBarForeground, SideBarBorder                              ui.Color
	SideBarTitleBackground, SideBarTitleForeground, SideBarTitleBorder               ui.Color
	PanelBackground, PanelBorder, PanelTitleBorder                                   ui.Color
	PanelTitleActiveForeground, PanelTitleInactiveForeground, PanelTitleActiveBorder ui.Color
	// The count on a tab of the panel (the Problems badge).
	PanelTitleBadgeBackground, PanelTitleBadgeForeground ui.Color
	TitleBarActiveBackground, TitleBarBorder             ui.Color
	// The colors of tabs (kit.Tabs): the background and the text of a tab by whether it is the active
	// tab and whether its group is the active group (the Unfocused colors are those of the tabs of
	// another group), the tab under the pointer (a transparent hover color leaves the tab as it is),
	// the line at the right of every tab, the line after the last pinned tab, the line along the
	// bottom and along the top of the active tab, and the line along the top of a tab with unsaved
	// changes.
	TabActiveBackground, TabUnfocusedActiveBackground, TabInactiveBackground, TabUnfocusedInactiveBackground ui.Color
	TabActiveForeground, TabInactiveForeground, TabUnfocusedActiveForeground, TabUnfocusedInactiveForeground ui.Color
	TabHoverBackground, TabUnfocusedHoverBackground, TabHoverForeground, TabUnfocusedHoverForeground         ui.Color
	TabBorder, TabLastPinnedBorder, TabActiveBorder, TabUnfocusedActiveBorder                                ui.Color
	TabActiveBorderTop, TabUnfocusedActiveBorderTop                                                          ui.Color
	TabActiveModifiedBorder, TabInactiveModifiedBorder                                                       ui.Color
	TabUnfocusedActiveModifiedBorder, TabUnfocusedInactiveModifiedBorder                                     ui.Color
	// The colors of dragging a tab: the line between two tabs where the tab would go
	// (TabDragAndDropBorder) and the background of the part of an editor area it would go to
	// (EditorGroupDropBackground).
	TabDragAndDropBorder, EditorGroupDropBackground ui.Color
	// The colors of the header of an editor group: the strip behind the tabs and the line under it, the
	// header that shows one title (workbench.editor.showTabs "single") and the line under it.
	EditorGroupHeaderTabsBackground, EditorGroupHeaderTabsBorder ui.Color
	EditorGroupHeaderNoTabsBackground, EditorGroupHeaderBorder   ui.Color
	// The colors of the grid of editor groups: the line between two groups, the background of an
	// empty group (transparent leaves the editor's) and the border of an empty group that has the
	// focus.
	EditorGroupBorder, EditorGroupEmptyBackground, EditorGroupFocusedEmptyBorder ui.Color
}

// ColorsOf gives the colors of a theme: for each field, the color the theme sets or else the
// default of the color registry. A color that has neither (editor.lineHighlightBackground in
// most themes) is transparent.
func ColorsOf(ct *theme.ColorTheme) Colors {
	color := func(id string) ui.Color {
		c, ok := ct.Color(id)
		if !ok {
			return ui.NewColor(0)
		}
		return ui.NewColor(c.ARGB())
	}
	return Colors{
		EditorBackground:                       color("editor.background"),
		EditorForeground:                       color("editor.foreground"),
		EditorLineHighlightBackground:          color("editor.lineHighlightBackground"),
		EditorLineHighlightBorder:              color("editor.lineHighlightBorder"),
		EditorSelectionBackground:              color("editor.selectionBackground"),
		EditorInactiveSelectionBackground:      color("editor.inactiveSelectionBackground"),
		EditorCursorForeground:                 color("editorCursor.foreground"),
		EditorLineNumberForeground:             color("editorLineNumber.foreground"),
		EditorLineNumberActiveForeground:       color("editorLineNumber.activeForeground"),
		EditorGutterBackground:                 color("editorGutter.background"),
		EditorWhitespaceForeground:             color("editorWhitespace.foreground"),
		EditorIndentGuideBackground1:           color("editorIndentGuide.background1"),
		EditorIndentGuideBackground:            guideColors(color, "editorIndentGuide.background"),
		EditorIndentGuideActiveBackground:      guideColors(color, "editorIndentGuide.activeBackground"),
		EditorBracketPairGuideBackground:       guideColors(color, "editorBracketPairGuide.background"),
		EditorBracketPairGuideActiveBackground: guideColors(color, "editorBracketPairGuide.activeBackground"),
		EditorRulerForeground:                  color("editorRuler.foreground"),
		EditorFoldBackground:                   color("editor.foldBackground"),
		EditorFoldPlaceholderForeground:        color("editor.foldPlaceholderForeground"),
		EditorGutterFoldingControlForeground:   color("editorGutter.foldingControlForeground"),
		EditorBracketMatchBackground:           color("editorBracketMatch.background"),
		EditorBracketMatchBorder:               color("editorBracketMatch.border"),
		EditorBracketMatchForeground:           color("editorBracketMatch.foreground"),
		EditorBracketHighlightForeground: [6]ui.Color{
			color("editorBracketHighlight.foreground1"), color("editorBracketHighlight.foreground2"),
			color("editorBracketHighlight.foreground3"), color("editorBracketHighlight.foreground4"),
			color("editorBracketHighlight.foreground5"), color("editorBracketHighlight.foreground6"),
		},
		EditorBracketUnexpectedForeground: color("editorBracketHighlight.unexpectedBracket.foreground"),
		ScrollbarSliderBackground:         color("scrollbarSlider.background"),
		ScrollbarSliderHoverBackground:    color("scrollbarSlider.hoverBackground"),
		ScrollbarSliderActiveBackground:   color("scrollbarSlider.activeBackground"),
		ScrollbarShadow:                   color("scrollbar.shadow"),
		ScrollbarBackground:               color("scrollbar.background"),
		SashHoverBorder:                   color("sash.hoverBorder"),
		FocusBorder:                       color("focusBorder"),
		Foreground:                        color("foreground"),
		WidgetShadow:                      color("widget.shadow"),

		EditorFindMatchBackground: color("editor.findMatchBackground"), EditorFindMatchBorder: color("editor.findMatchBorder"),
		EditorFindMatchHighlightBackground: color("editor.findMatchHighlightBackground"), EditorFindMatchHighlightBorder: color("editor.findMatchHighlightBorder"),
		EditorFindRangeHighlightBackground: color("editor.findRangeHighlightBackground"), EditorFindRangeHighlightBorder: color("editor.findRangeHighlightBorder"),
		EditorSelectionHighlightBackground: color("editor.selectionHighlightBackground"), EditorSelectionHighlightBorder: color("editor.selectionHighlightBorder"),
		EditorWordHighlightTextBackground: color("editor.wordHighlightTextBackground"), EditorWordHighlightTextBorder: color("editor.wordHighlightTextBorder"),
		EditorSnippetTabstopHighlightBackground: color("editor.snippetTabstopHighlightBackground"), EditorSnippetTabstopHighlightBorder: color("editor.snippetTabstopHighlightBorder"),
		EditorSnippetFinalTabstopHighlightBackground: color("editor.snippetFinalTabstopHighlightBackground"), EditorSnippetFinalTabstopHighlightBorder: color("editor.snippetFinalTabstopHighlightBorder"),
		MinimapBackground: color("minimap.background"), MinimapForegroundOpacity: color("minimap.foregroundOpacity"),
		MinimapSelection: color("minimap.selectionHighlight"), MinimapFindMatch: color("minimap.findMatchHighlight"),
		MinimapSelectionOccurrence: color("minimap.selectionOccurrenceHighlight"),
		MinimapSliderBackground:    color("minimapSlider.background"), MinimapSliderHoverBackground: color("minimapSlider.hoverBackground"),
		MinimapSliderActive:          color("minimapSlider.activeBackground"),
		EditorStickyScrollBackground: color("editorStickyScroll.background"), EditorStickyScrollGutterBackground: color("editorStickyScrollGutter.background"),
		EditorStickyScrollHoverBackground: color("editorStickyScrollHover.background"),
		EditorStickyScrollBorder:          color("editorStickyScroll.border"), EditorStickyScrollShadow: color("editorStickyScroll.shadow"),
		EditorOverviewRulerFindMatch: color("editorOverviewRuler.findMatchForeground"), EditorOverviewRulerRangeHighlight: color("editorOverviewRuler.rangeHighlightForeground"),
		EditorWidgetBackground: color("editorWidget.background"), EditorWidgetForeground: color("editorWidget.foreground"), WidgetBorder: color("widget.border"),
		IconForeground: color("icon.foreground"), ToolbarHoverBackground: color("toolbar.hoverBackground"),
		InputBackground: color("input.background"), InputForeground: color("input.foreground"), InputBorder: color("input.border"),
		InputPlaceholderForeground: color("input.placeholderForeground"),
		InputOptionActiveBorder:    color("inputOption.activeBorder"), InputOptionActiveBackground: color("inputOption.activeBackground"),
		InputOptionActiveForeground: color("inputOption.activeForeground"), InputOptionHoverBackground: color("inputOption.hoverBackground"),
		InputValidationErrorBorder:    color("inputValidation.errorBorder"),
		ListActiveSelectionBackground: color("list.activeSelectionBackground"), ListActiveSelectionForeground: color("list.activeSelectionForeground"),
		ListHoverBackground: color("list.hoverBackground"), ListFocusOutline: color("list.focusOutline"),
		ListFocusForeground: color("list.focusForeground"), ListFocusBackground: color("list.focusBackground"),
		ListFocusAndSelectionOutline:    color("list.focusAndSelectionOutline"),
		ListInactiveSelectionForeground: color("list.inactiveSelectionForeground"), ListInactiveSelectionBackground: color("list.inactiveSelectionBackground"),
		ListInactiveFocusBackground: color("list.inactiveFocusBackground"), ListInactiveFocusOutline: color("list.inactiveFocusOutline"),
		TreeIndentGuidesStroke: color("tree.indentGuidesStroke"), TreeInactiveIndentGuidesStroke: color("tree.inactiveIndentGuidesStroke"),
		ListDropBackground: color("list.dropBackground"), ListDropBetweenBackground: color("list.dropBetweenBackground"),
		ListHighlightForeground: color("list.highlightForeground"), ListFocusHighlightForeground: color("list.focusHighlightForeground"),
		ListFilterMatchBackground: color("list.filterMatchBackground"), ListFilterMatchBorder: color("list.filterMatchBorder"),
		ListFilterWidgetBackground: color("listFilterWidget.background"), ListFilterWidgetOutline: color("listFilterWidget.outline"),
		ListFilterWidgetNoMatchesOutline: color("listFilterWidget.noMatchesOutline"), ListFilterWidgetShadow: color("listFilterWidget.shadow"),
		DescriptionForeground: color("descriptionForeground"),
		ButtonForeground:      color("button.foreground"), ButtonBackground: color("button.background"),
		ButtonHoverBackground: color("button.hoverBackground"), ButtonBorder: color("button.border"),
		BreadcrumbForeground: color("breadcrumb.foreground"), BreadcrumbBackground: color("breadcrumb.background"),
		BreadcrumbFocusForeground:           color("breadcrumb.focusForeground"),
		BreadcrumbActiveSelectionForeground: color("breadcrumb.activeSelectionForeground"),
		BreadcrumbPickerBackground:          color("breadcrumbPicker.background"),

		ButtonSecondaryForeground: color("button.secondaryForeground"), ButtonSecondaryBackground: color("button.secondaryBackground"),
		ButtonSecondaryHoverBackground: color("button.secondaryHoverBackground"), ButtonSecondaryBorder: color("button.secondaryBorder"),
		DisabledForeground: color("disabledForeground"), ToolbarActiveBackground: color("toolbar.activeBackground"),
		ActionBarToggledBackground:  color("actionBar.toggledBackground"),
		EditorHoverWidgetBackground: color("editorHoverWidget.background"), EditorHoverWidgetForeground: color("editorHoverWidget.foreground"),
		EditorHoverWidgetBorder: color("editorHoverWidget.border"),

		MenuForeground: color("menu.foreground"), MenuBackground: color("menu.background"), MenuBorder: color("menu.border"),
		MenuSelectionForeground: color("menu.selectionForeground"), MenuSelectionBackground: color("menu.selectionBackground"),
		MenuSelectionBorder: color("menu.selectionBorder"), MenuSeparatorBackground: color("menu.separatorBackground"),
		TitleBarActiveForeground:   color("titleBar.activeForeground"),
		MenubarSelectionForeground: color("menubar.selectionForeground"), MenubarSelectionBackground: color("menubar.selectionBackground"),
		MenubarSelectionBorder: color("menubar.selectionBorder"),

		InputValidationInfoBackground: color("inputValidation.infoBackground"), InputValidationInfoForeground: color("inputValidation.infoForeground"),
		InputValidationInfoBorder:        color("inputValidation.infoBorder"),
		InputValidationWarningBackground: color("inputValidation.warningBackground"), InputValidationWarningForeground: color("inputValidation.warningForeground"),
		InputValidationWarningBorder:   color("inputValidation.warningBorder"),
		InputValidationErrorBackground: color("inputValidation.errorBackground"), InputValidationErrorForeground: color("inputValidation.errorForeground"),
		DropdownBackground: color("dropdown.background"), DropdownForeground: color("dropdown.foreground"),
		DropdownBorder: color("dropdown.border"), DropdownListBackground: color("dropdown.listBackground"),
		CheckboxBackground: color("checkbox.background"), CheckboxForeground: color("checkbox.foreground"), CheckboxBorder: color("checkbox.border"),
		CheckboxSelectBackground: color("checkbox.selectBackground"), CheckboxSelectBorder: color("checkbox.selectBorder"),
		CheckboxDisabledBackground: color("checkbox.disabled.background"), CheckboxDisabledForeground: color("checkbox.disabled.foreground"),
		BadgeBackground: color("badge.background"), BadgeForeground: color("badge.foreground"), ProgressBarBackground: color("progressBar.background"),
		ProblemsErrorIconForeground: color("problemsErrorIcon.foreground"), ProblemsWarningIconForeground: color("problemsWarningIcon.foreground"),
		ProblemsInfoIconForeground: color("problemsInfoIcon.foreground"),
		TextLinkForeground:         color("textLink.foreground"),
		NotificationsForeground:    color("notifications.foreground"), NotificationsBackground: color("notifications.background"),
		NotificationToastBorder: color("notificationToast.border"), NotificationCenterBorder: color("notificationCenter.border"),
		NotificationCenterHeaderForeground: color("notificationCenterHeader.foreground"),
		NotificationCenterHeaderBackground: color("notificationCenterHeader.background"), NotificationsBorder: color("notifications.border"),
		NotificationsErrorIconForeground: color("notificationsErrorIcon.foreground"), NotificationsWarningIconForeground: color("notificationsWarningIcon.foreground"),
		NotificationsInfoIconForeground: color("notificationsInfoIcon.foreground"),

		StatusBarForeground: color("statusBar.foreground"), StatusBarBackground: color("statusBar.background"), StatusBarBorder: color("statusBar.border"),
		StatusBarNoFolderForeground: color("statusBar.noFolderForeground"), StatusBarNoFolderBackground: color("statusBar.noFolderBackground"),
		StatusBarNoFolderBorder: color("statusBar.noFolderBorder"),
		ActivityBarBackground:   color("activityBar.background"), ActivityBarBorder: color("activityBar.border"),
		ActivityBarForeground: color("activityBar.foreground"), ActivityBarInactiveForeground: color("activityBar.inactiveForeground"),
		ActivityBarActiveBorder: color("activityBar.activeBorder"), ActivityBarActiveFocusBorder: color("activityBar.activeFocusBorder"),
		ActivityBarActiveBackground:    color("activityBar.activeBackground"),
		SideBarSectionHeaderBackground: color("sideBarSectionHeader.background"), SideBarSectionHeaderForeground: color("sideBarSectionHeader.foreground"),
		SideBarSectionHeaderBorder: color("sideBarSectionHeader.border"),
		SideBarBackground:          color("sideBar.background"), SideBarForeground: color("sideBar.foreground"), SideBarBorder: color("sideBar.border"),
		SideBarTitleBackground: color("sideBarTitle.background"), SideBarTitleForeground: color("sideBarTitle.foreground"),
		SideBarTitleBorder: color("sideBarTitle.border"),
		PanelBackground:    color("panel.background"), PanelBorder: color("panel.border"), PanelTitleBorder: color("panelTitle.border"),
		PanelTitleActiveForeground: color("panelTitle.activeForeground"), PanelTitleInactiveForeground: color("panelTitle.inactiveForeground"),
		PanelTitleActiveBorder:    color("panelTitle.activeBorder"),
		PanelTitleBadgeBackground: color("panelTitleBadge.background"), PanelTitleBadgeForeground: color("panelTitleBadge.foreground"),
		TitleBarActiveBackground: color("titleBar.activeBackground"), TitleBarBorder: color("titleBar.border"),

		TabActiveBackground: color("tab.activeBackground"), TabUnfocusedActiveBackground: color("tab.unfocusedActiveBackground"),
		TabInactiveBackground: color("tab.inactiveBackground"), TabUnfocusedInactiveBackground: color("tab.unfocusedInactiveBackground"),
		TabActiveForeground: color("tab.activeForeground"), TabInactiveForeground: color("tab.inactiveForeground"),
		TabUnfocusedActiveForeground: color("tab.unfocusedActiveForeground"), TabUnfocusedInactiveForeground: color("tab.unfocusedInactiveForeground"),
		TabHoverBackground: color("tab.hoverBackground"), TabUnfocusedHoverBackground: color("tab.unfocusedHoverBackground"),
		TabHoverForeground: color("tab.hoverForeground"), TabUnfocusedHoverForeground: color("tab.unfocusedHoverForeground"),
		TabBorder: color("tab.border"), TabLastPinnedBorder: color("tab.lastPinnedBorder"),
		TabActiveBorder: color("tab.activeBorder"), TabUnfocusedActiveBorder: color("tab.unfocusedActiveBorder"),
		TabActiveBorderTop: color("tab.activeBorderTop"), TabUnfocusedActiveBorderTop: color("tab.unfocusedActiveBorderTop"),
		TabActiveModifiedBorder: color("tab.activeModifiedBorder"), TabInactiveModifiedBorder: color("tab.inactiveModifiedBorder"),
		TabUnfocusedActiveModifiedBorder: color("tab.unfocusedActiveModifiedBorder"), TabUnfocusedInactiveModifiedBorder: color("tab.unfocusedInactiveModifiedBorder"),
		EditorGroupHeaderTabsBackground: color("editorGroupHeader.tabsBackground"), EditorGroupHeaderTabsBorder: color("editorGroupHeader.tabsBorder"),
		EditorGroupHeaderNoTabsBackground: color("editorGroupHeader.noTabsBackground"), EditorGroupHeaderBorder: color("editorGroupHeader.border"),
		EditorGroupBorder: color("editorGroup.border"), EditorGroupEmptyBackground: color("editorGroup.emptyBackground"),
		EditorGroupFocusedEmptyBorder: color("editorGroup.focusedEmptyBorder"),
		TabDragAndDropBorder:          color("tab.dragAndDropBorder"), EditorGroupDropBackground: color("editorGroup.dropBackground"),
	}
}

// DarkModern are the colors of the bundled Dark Modern theme, which a widget shown on its own
// (with no Theme above it) uses.
func DarkModern() Colors { return bundledColors(theme.DefaultDark) }

// LightModern are the colors of the bundled Light Modern theme.
func LightModern() Colors { return bundledColors(theme.DefaultLight) }

func bundledColors(name string) Colors {
	t, err := theme.LoadBundled(name)
	if err != nil {
		// The theme is embedded in the program, so this is a broken build.
		panic(err)
	}
	return ColorsOf(theme.NewColorTheme(t, theme.DefaultColorRegistry()))
}

// Theme is the inherited widget that gives its subtree the workbench colors and, for the editors,
// the theme that colors tokens.
type Theme struct {
	widgets.InheritedWidget
	colors Colors
	tokens *theme.TokenTheme
	uiFont string
}

// ThemeOpts are the options of NewTheme.
type ThemeOpts struct {
	// Tokens gives the colors and font styles of the tokens of a language (SPEC §8.4). Editors
	// below a Theme without it show their text in the editor's foreground color.
	Tokens *theme.TokenTheme
	// UIFont is the family of the workbench's text; without it the desktop's UI font is used
	// (SPEC D14). A test gives the family of its test font.
	UIFont string
}

// NewTheme makes a Theme with the given colors for the child.
func NewTheme(colors Colors, child widgets.Widget, opts ...ThemeOpts) *Theme {
	t := &Theme{InheritedWidget: widgets.NewInheritedWidgetBase(child), colors: colors}
	if len(opts) > 0 {
		t.tokens = opts[0].Tokens
		t.uiFont = opts[0].UIFont
	}
	return t
}

// Colors are the colors the Theme gives.
func (t *Theme) Colors() Colors { return t.colors }

// UpdateShouldNotify implements widgets.InheritedWidgetInterface: dependents are rebuilt
// when a color or the token theme changed.
func (t *Theme) UpdateShouldNotify(old widgets.Widget) bool {
	o := old.(*Theme)
	return t.colors != o.colors || t.tokens != o.tokens || t.uiFont != o.uiFont
}

// ThemeOf returns the colors of the nearest Theme above bc, and rebuilds the caller when
// they change. Without a Theme it returns DarkModern, so a widget can be shown on its own.
func ThemeOf(bc widgets.BuildContext) Colors {
	if t, ok := widgets.DependOn[*Theme](bc); ok {
		return t.colors
	}
	return DarkModern()
}

// TokenThemeOf returns the token theme of the nearest Theme above bc, or nil when there is none
// or it has none, and rebuilds the caller when it changes.
func TokenThemeOf(bc widgets.BuildContext) *theme.TokenTheme {
	if t, ok := widgets.DependOn[*Theme](bc); ok {
		return t.tokens
	}
	return nil
}

// guideColors reads the six colors of a family of guide colors (id1 to id6).
func guideColors(color func(string) ui.Color, prefix string) [6]ui.Color {
	var out [6]ui.Color
	for i := range out {
		out[i] = color(prefix + string(rune('1'+i)))
	}
	return out
}
