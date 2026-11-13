package extensionsui

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/gui/exthost"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// The requirements of the extensions on the page (SPEC §10.36 "On the Extensions page"): a line per requirement under the state line
// of its extension, with the buttons that help to meet a missing one, and Check System and Check Again in the header.

// Texts and metrics of the requirements.
const (
	checkAgainLabel   = "Check Again"
	checkSystemLabel  = "Check System"
	copyInstallLabel  = "Copy Install Command"
	installLabel      = "Install"
	setPathLabel      = "Set Path..."
	instructionsLabel = "Instructions"
	checkingText      = "Checking..."
	notCheckedText    = "Not checked yet"
	optionalText      = "optional"
	requirementIcon   = 14.0
	requirementIndent = 4.0
	requirementGap    = 6.0
)

// The actions of the buttons of a requirement, for RequirementButtonKey.
const (
	ActionCopyInstall  = "copy"
	ActionInstall      = "install"
	ActionSetPath      = "path"
	ActionInstructions = "docs"
)

// RequirementKey is the key of the line of a requirement.
func RequirementKey(extensionID, requirementID string) foundation.Key {
	return foundation.NewValueKey("extensions:req:" + extensionID + ":" + requirementID)
}

// RequirementButtonKey is the key of a button of a requirement; action is one of the Action constants.
func RequirementButtonKey(extensionID, requirementID, action string) foundation.Key {
	return foundation.NewValueKey("extensions:req:" + extensionID + ":" + requirementID + ":" + action)
}

// CheckAgainKey is the key of the header's Check Again button.
func CheckAgainKey() foundation.Key { return foundation.NewValueKey("extensions:checkAgain") }

// CheckSystemKey is the key of the header's Check System button.
func CheckSystemKey() foundation.Key { return foundation.NewValueKey("extensions:checkSystem") }

// RequirementLook says how a requirement is shown: the look of its icon.
type RequirementLook int

// The looks of a requirement.
const (
	// LookChecking: its check runs, or has not run yet.
	LookChecking RequirementLook = iota
	// LookFound: it is there.
	LookFound
	// LookError: a required one is missing or cannot be used.
	LookError
	// LookWarning: an optional one is missing or cannot be used.
	LookWarning
	// LookUnknown: the check could not tell.
	LookUnknown
)

// LookOf is the look of a requirement's row.
func LookOf(row exthost.RequirementRow) RequirementLook {
	if row.Checking || !row.Checked() {
		return LookChecking
	}
	switch row.Result.State {
	case extension.RequirementFound:
		return LookFound
	case extension.RequirementMissing, extension.RequirementUnusable:
		if row.Requirement.Optional {
			return LookWarning
		}
		return LookError
	}
	return LookUnknown
}

// RequirementText is the text of a requirement's line after its icon: the name, "optional" when it is, and the path and version,
// or the detail, or "Checking..." while its check runs.
func RequirementText(row exthost.RequirementRow) string {
	text := row.Requirement.Name
	if row.Requirement.Optional {
		text += " (" + optionalText + ")"
	}
	switch {
	case row.Checking:
		return text + "  •  " + checkingText
	case !row.Checked():
		return text + "  •  " + notCheckedText
	}
	res := row.Result
	var info string
	if res.State == extension.RequirementFound && res.Path != "" {
		info = res.Path
		if res.Version != "" {
			info += " " + res.Version
		}
		if res.Detail != "" {
			info += " (" + res.Detail + ")"
		}
	} else {
		info = res.Detail
		if info == "" {
			info = stateWord(res.State)
		}
	}
	if info == "" {
		return text
	}
	return text + "  •  " + info
}

// stateWord is a state in words, for a result with no detail.
func stateWord(s extension.RequirementState) string {
	switch s {
	case extension.RequirementFound:
		return "found"
	case extension.RequirementMissing:
		return "missing"
	case extension.RequirementUnusable:
		return "cannot be used"
	}
	return "could not be checked"
}

// RequirementHelp is what the buttons of a requirement act on: the install command for the system's family, the command of the
// extension that installs it, the path setting and the address of the instructions. The result's own Install and PathSetting (a
// requirement that follows a setting) win over the requirement's. All are empty for a requirement that has no buttons: one that is
// found, unknown or not checked.
type RequirementHelp struct {
	InstallCommand, InstallAction, PathSetting, Docs string
}

// HelpOf is the help of a requirement's row on a system of the family.
func HelpOf(row exthost.RequirementRow, family string) RequirementHelp {
	if row.Checking || !row.Checked() {
		return RequirementHelp{}
	}
	if s := row.Result.State; s != extension.RequirementMissing && s != extension.RequirementUnusable {
		return RequirementHelp{}
	}
	install := row.Requirement.Install
	if !row.Result.Install.Empty() {
		install = row.Result.Install
	}
	pathSetting := row.Requirement.PathSetting
	if row.Result.PathSetting != "" {
		pathSetting = row.Result.PathSetting
	}
	h := RequirementHelp{InstallAction: row.Requirement.InstallAction, PathSetting: pathSetting, Docs: install.Docs}
	if c, ok := install.CommandFor(family); ok {
		h.InstallCommand = c.Command
	}
	return h
}

// lookIcon is the Codicon and color of a look.
func lookIcon(look RequirementLook, colors kit.Colors) (int, ui.Color) {
	switch look {
	case LookFound:
		if c, ok := colors.Named("testing.iconPassed"); ok && c.Alpha() != 0 {
			return fonts.CodiconCheck, c
		}
		return fonts.CodiconCheck, colors.Foreground
	case LookError:
		return fonts.CodiconError, colors.ErrorForeground
	case LookWarning:
		return fonts.CodiconWarning, colors.EditorWarningForeground
	case LookUnknown:
		return fonts.CodiconQuestion, colors.DescriptionForeground
	}
	return fonts.CodiconLoading, colors.DescriptionForeground
}

// buildRequirements are the lines of the requirements of one extension; nil when it has none.
func buildRequirements(extensionID string, o Options, colors kit.Colors, style func(float64, ui.FontWeight, ui.Color) *painting.TextStyle) []widgets.Widget {
	if o.Requirements == nil {
		return nil
	}
	var out []widgets.Widget
	for _, row := range o.Requirements() {
		if row.ExtensionID != extensionID {
			continue
		}
		out = append(out, buildRequirement(row, o, colors, style))
	}
	return out
}

// buildRequirement is the line of one requirement, with its buttons under it when it has any.
func buildRequirement(row exthost.RequirementRow, o Options, colors kit.Colors, style func(float64, ui.FontWeight, ui.Color) *painting.TextStyle) widgets.Widget {
	look := LookOf(row)
	code, color := lookIcon(look, colors)
	textColor := colors.Foreground
	if look == LookChecking {
		textColor = colors.DescriptionForeground
	}
	line := widgets.NewRow([]widgets.Widget{
		widgets.NewIcon(fonts.CodiconIcon(code), widgets.IconOpts{Size: foundation.Some(requirementIcon), Color: foundation.Some(color)}),
		widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(requirementGap)}),
		widgets.NewFlexible(widgets.NewText(RequirementText(row), widgets.TextOpts{Style: style(detailSize, ui.FontWeightW400, textColor)})),
	}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart, MainAxisSize: rendering.MainAxisSizeMin})
	children := []widgets.Widget{line}
	if buttons := requirementButtons(row, o); len(buttons) > 0 {
		children = append(children, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(rowLineGap)}),
			widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(requirementIcon+requirementGap, 0, 0, 0), widgets.NewWrap(buttons, widgets.WrapOpts{Spacing: requirementGap, RunSpacing: requirementGap})))
	}
	return widgets.NewKeyedSubtree(
		widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(requirementIndent, 0, 0, 0),
			widgets.NewColumn(children, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart, MainAxisSize: rendering.MainAxisSizeMin})),
		widgets.WidgetOpts{Key: RequirementKey(row.ExtensionID, row.Requirement.ID)})
}

// requirementButtons are the buttons of a missing or unusable requirement: Copy Install Command, Install, Set Path... and
// Instructions, each when there is something for it to do.
func requirementButtons(row exthost.RequirementRow, o Options) []widgets.Widget {
	h := HelpOf(row, o.Family)
	ext, id := row.ExtensionID, row.Requirement.ID
	var out []widgets.Widget
	add := func(label, action, tooltip string, run func()) {
		out = append(out, kit.NewButton(label, kit.ButtonOpts{Key: RequirementButtonKey(ext, id, action), Secondary: true, Small: true, Tooltip: tooltip, OnPressed: run}))
	}
	if h.InstallCommand != "" && o.OnCopy != nil {
		cmd := h.InstallCommand
		add(copyInstallLabel, ActionCopyInstall, cmd, func() { o.OnCopy(cmd) })
	}
	if h.InstallAction != "" && o.OnInstall != nil {
		action := h.InstallAction
		add(installLabel, ActionInstall, "", func() { o.OnInstall(action) })
	}
	if h.PathSetting != "" && o.OnSetPath != nil {
		key := h.PathSetting
		add(setPathLabel, ActionSetPath, key, func() { o.OnSetPath(key) })
	}
	if h.Docs != "" && o.OnOpenURI != nil {
		docs := h.Docs
		add(instructionsLabel, ActionInstructions, docs, func() { o.OnOpenURI(docs) })
	}
	return out
}
