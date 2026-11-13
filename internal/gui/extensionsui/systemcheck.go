package extensionsui

import (
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/gui/exthost"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// The page "System Check" (SPEC §10.36 "The Check System page", step 34.5): every requirement of the enabled extensions, grouped by
// what its check found, with Check Again and Copy Report, which copies the page as Markdown to paste into an issue.

// Texts of the page.
const (
	systemCheckTitle   = "System Check"
	groupProblemsTitle = "Missing or cannot be used"
	groupUnknownTitle  = "Not checked"
	groupFoundTitle    = "Found"
	copyReportLabel    = "Copy Report"
	noFolderText       = "no folder"
	nothingToCheck     = "No enabled extension needs anything from the system."
	emptyGroupText     = "None."
)

// CopyReportKey is the key of the Copy Report button.
func CopyReportKey() foundation.Key { return foundation.NewValueKey("systemCheck:copyReport") }

// SystemCheckAgainKey is the key of the page's Check Again button.
func SystemCheckAgainKey() foundation.Key { return foundation.NewValueKey("systemCheck:checkAgain") }

// SystemCheckGroupKey is the key of the heading of a group: "problems", "unknown" or "found".
func SystemCheckGroupKey(group string) foundation.Key {
	return foundation.NewValueKey("systemCheck:group:" + group)
}

// SystemCheckLeftOutKey is the key of the line that counts the turned-off extensions.
func SystemCheckLeftOutKey() foundation.Key { return foundation.NewValueKey("systemCheck:leftOut") }

// SystemCheckGroups are the requirements of the enabled extensions grouped as the page shows them.
type SystemCheckGroups struct {
	// Problems are the missing and unusable ones, the required ones first; Unknown those the check could not tell, and those still
	// being checked; Found the ones that are there. Each keeps the order of the extensions and their manifests.
	Problems, Unknown, Found []exthost.RequirementRow
	// Missing, Unusable, NotChecked and Checking count the rows of those states (Checking: being checked or not checked yet).
	Missing, Unusable, NotChecked, Checking int
	// LeftOut is how many turned-off extensions with requirements were left out.
	LeftOut int
}

// GroupRequirements groups the rows of the requirements, leaving out the extensions whose ids are in disabled.
func GroupRequirements(rows []exthost.RequirementRow, disabled []string) SystemCheckGroups {
	var g SystemCheckGroups
	left := map[string]bool{}
	var required, optional []exthost.RequirementRow
	for _, r := range rows {
		if slices.Contains(disabled, r.ExtensionID) {
			left[r.ExtensionID] = true
			continue
		}
		if r.Checking || !r.Checked() {
			g.Checking++
			g.Unknown = append(g.Unknown, r)
			continue
		}
		switch r.Result.State {
		case extension.RequirementFound:
			g.Found = append(g.Found, r)
		case extension.RequirementMissing, extension.RequirementUnusable:
			if r.Result.State == extension.RequirementMissing {
				g.Missing++
			} else {
				g.Unusable++
			}
			if r.Requirement.Optional {
				optional = append(optional, r)
			} else {
				required = append(required, r)
			}
		default:
			g.NotChecked++
			g.Unknown = append(g.Unknown, r)
		}
	}
	g.Problems = append(required, optional...)
	g.LeftOut = len(left)
	return g
}

// Total is how many requirements the groups hold.
func (g SystemCheckGroups) Total() int { return len(g.Problems) + len(g.Unknown) + len(g.Found) }

// SummaryText is the summary of the page: "2 missing, 1 cannot be used, 23 found", "Checking... 12 of 32 done" while checks run.
func SummaryText(g SystemCheckGroups) string {
	total := g.Total()
	if total == 0 {
		return nothingToCheck
	}
	if g.Checking > 0 {
		return checkingText + " " + strconv.Itoa(total-g.Checking) + " of " + strconv.Itoa(total) + " done"
	}
	var parts []string
	add := func(n int, what string) {
		if n > 0 {
			parts = append(parts, strconv.Itoa(n)+" "+what)
		}
	}
	add(g.Missing, "missing")
	add(g.Unusable, "cannot be used")
	add(g.NotChecked, "not checked")
	parts = append(parts, strconv.Itoa(len(g.Found))+" found")
	return strings.Join(parts, ", ")
}

// LeftOutText is the line under the groups that counts the turned-off extensions; "" when none was left out.
func LeftOutText(n int) string {
	switch n {
	case 0:
		return ""
	case 1:
		return "1 turned-off extension is not checked; see the Extensions page."
	}
	return strconv.Itoa(n) + " turned-off extensions are not checked; see the Extensions page."
}

// ResultWord is the result of a row in one or two words, for the report.
func ResultWord(r exthost.RequirementRow) string {
	if r.Checking || !r.Checked() {
		return "checking"
	}
	return stateWord(r.Result.State)
}

// ReportMarkdown is what Copy Report copies: the system line and the summary, then a table per group, then the turned-off
// extensions. It holds the paths and versions the checks found and their details, and no setting value or environment of its own.
func ReportMarkdown(system string, g SystemCheckGroups) string {
	var b strings.Builder
	b.WriteString("# Emetos System Check\n\n")
	b.WriteString("System: " + cell(system) + "\n\n")
	b.WriteString(SummaryText(g) + "\n")
	for _, grp := range []struct {
		title string
		rows  []exthost.RequirementRow
	}{{groupProblemsTitle, g.Problems}, {groupUnknownTitle, g.Unknown}, {groupFoundTitle, g.Found}} {
		b.WriteString("\n## " + grp.title + "\n\n")
		if len(grp.rows) == 0 {
			b.WriteString(emptyGroupText + "\n")
			continue
		}
		b.WriteString("| Requirement | Extension | Result | Path | Version | Detail |\n| --- | --- | --- | --- | --- | --- |\n")
		for _, r := range grp.rows {
			name := r.Requirement.Name
			if r.Requirement.Optional {
				name += " (" + optionalText + ")"
			}
			b.WriteString("| " + strings.Join([]string{cell(name), cell(r.ExtensionName), ResultWord(r), cell(r.Result.Path), cell(r.Result.Version), cell(r.Result.Detail)}, " | ") + " |\n")
		}
	}
	if t := LeftOutText(g.LeftOut); t != "" {
		b.WriteString("\n" + strings.TrimSuffix(t, "; see the Extensions page.") + ".\n")
	}
	return b.String()
}

// cell is a text made safe for a cell of a Markdown table: no line breaks, and the pipes escaped.
func cell(s string) string {
	s = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(s)
	return strings.ReplaceAll(s, "|", `\|`)
}

// SystemCheckOptions are the options of NewSystemCheckPage.
type SystemCheckOptions struct {
	// Requirements gives the requirements of every extension with what their checks found (exthost.Requirements.Rows).
	Requirements func() []exthost.RequirementRow
	// Disabled gives the ids of the turned-off extensions, whose requirements are left out.
	Disabled func() []string
	// Subscribe calls fn when the rows or the turned-off extensions may have changed; each returns the function that stops it.
	Subscribe []func(fn func()) (unsubscribe func())
	// OnShown is called when the page is first shown: it starts the checks that have not run.
	OnShown func()
	// OnCheckAgain is called by Check Again.
	OnCheckAgain func()
	// System names the system ("Fedora Linux 45 (Workstation Edition), fedora, linux/amd64"); Folder gives the folder the checks
	// run for ("" for none), shown by its name with the whole path in a hover.
	System string
	Folder func() string
	// Actions are what the buttons of the requirements do; Copy Report uses its OnCopy.
	Actions RequirementActions
}

// SystemCheckPage is the page "System Check".
type SystemCheckPage struct {
	widgets.StatefulWidget
	opts SystemCheckOptions
}

// NewSystemCheckPage makes the page.
func NewSystemCheckPage(opts SystemCheckOptions) *SystemCheckPage {
	return &SystemCheckPage{StatefulWidget: widgets.NewStatefulWidgetBase(), opts: opts}
}

// CreateState implements widgets.StatefulWidget.
func (p *SystemCheckPage) CreateState() widgets.State { return &systemCheckState{} }

type systemCheckState struct {
	widgets.StateOf[*SystemCheckPage]
	stops []func()
}

// InitState follows the results and starts the checks.
func (s *systemCheckState) InitState() {
	s.follow()
	if f := s.Widget().opts.OnShown; f != nil {
		f()
	}
}

func (s *systemCheckState) follow() {
	for _, sub := range s.Widget().opts.Subscribe {
		if sub != nil {
			s.stops = append(s.stops, sub(func() {
				if s.Mounted() {
					s.SetState(func() {})
				}
			}))
		}
	}
}

func (s *systemCheckState) unfollow() {
	for _, stop := range s.stops {
		if stop != nil {
			stop()
		}
	}
	s.stops = nil
}

// DidUpdateWidget follows the sources of the new widget.
func (s *systemCheckState) DidUpdateWidget(old *SystemCheckPage) {
	s.unfollow()
	s.follow()
}

// Dispose stops following.
func (s *systemCheckState) Dispose() { s.unfollow() }

// groups are the groups of the page now.
func (o SystemCheckOptions) groups() SystemCheckGroups {
	var rows []exthost.RequirementRow
	if o.Requirements != nil {
		rows = o.Requirements()
	}
	var disabled []string
	if o.Disabled != nil {
		disabled = o.Disabled()
	}
	return GroupRequirements(rows, disabled)
}

// Build implements widgets.State.
func (s *systemCheckState) Build(bc widgets.BuildContext) widgets.Widget {
	o := s.Widget().opts
	colors := kit.ThemeOf(bc)
	style := func(size float64, weight ui.FontWeight, color ui.Color) *painting.TextStyle {
		return painting.NewTextStyle(painting.TextStyleOpts{
			FontFamily: kit.UIFontOf(bc), FontFamilyFallback: []string{"sans-serif"},
			FontSize: foundation.Some(size), FontWeight: weight, Color: foundation.Some(color),
		})
	}
	text := func(t string, st *painting.TextStyle) widgets.Widget {
		return widgets.NewText(t, widgets.TextOpts{Style: st})
	}
	gap := func(h float64) widgets.Widget {
		return widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(h)})
	}
	dim := style(detailSize, ui.FontWeightW400, colors.DescriptionForeground)
	g := o.groups()
	folder := ""
	if o.Folder != nil {
		folder = o.Folder()
	}
	// the line names the folder; its whole path is in the hover
	folderLine := text("Folder: "+noFolderText, dim)
	if folder != "" {
		folderLine = widgets.NewAlign(kit.NewHover(text("Folder: "+filepath.Base(folder), dim), kit.HoverOpts{Text: folder}), widgets.AlignOpts{Alignment: painting.AlignmentTopLeft})
	}
	var header []widgets.Widget
	if o.OnCheckAgain != nil {
		header = append(header, kit.NewButton(checkAgainLabel, kit.ButtonOpts{Key: SystemCheckAgainKey(), Secondary: true, Small: true, Tooltip: "Look again for what the extensions need", OnPressed: o.OnCheckAgain}))
	}
	if o.Actions.OnCopy != nil {
		system, copyText := o.System, o.Actions.OnCopy
		header = append(header, kit.NewButton(copyReportLabel, kit.ButtonOpts{Key: CopyReportKey(), Secondary: true, Small: true, Tooltip: "Copy the page as Markdown, to paste into an issue",
			OnPressed: func() { copyText(ReportMarkdown(system, s.Widget().opts.groups())) }}))
	}
	children := []widgets.Widget{
		text(systemCheckTitle, style(titleSize, ui.FontWeightW600, colors.Foreground)),
		gap(6),
		text("System: "+o.System, dim),
		gap(rowLineGap),
		folderLine,
		gap(10),
		text(SummaryText(g), style(nameSize, ui.FontWeightW600, colors.Foreground)),
		gap(10),
	}
	if len(header) > 0 {
		children = append(children, widgets.NewWrap(header, widgets.WrapOpts{Spacing: buttonGap, RunSpacing: requirementGap}), gap(14))
	}
	if g.Total() > 0 {
		for _, grp := range []struct {
			key, title string
			rows       []exthost.RequirementRow
		}{{"problems", groupProblemsTitle, g.Problems}, {"unknown", groupUnknownTitle, g.Unknown}, {"found", groupFoundTitle, g.Found}} {
			children = append(children, widgets.NewKeyedSubtree(
				widgets.NewDecoratedBox(
					painting.NewBoxDecoration(painting.BoxDecorationOpts{Border: painting.Border{Bottom: painting.NewBorderSide(painting.BorderSideOpts{Color: foundation.Some(colors.PanelBorder), Width: foundation.Some(1.0)})}}),
					widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 0, 0, 4), text(grp.title+" ("+strconv.Itoa(len(grp.rows))+")", style(nameSize, ui.FontWeightW600, colors.Foreground)))),
				widgets.WidgetOpts{Key: SystemCheckGroupKey(grp.key)}), gap(8))
			if len(grp.rows) == 0 {
				children = append(children, text(emptyGroupText, dim), gap(14))
				continue
			}
			for _, r := range grp.rows {
				under := text(r.ExtensionName+"  •  "+r.Requirement.Purpose, dim)
				children = append(children, buildRequirement(r, []widgets.Widget{under}, o.Actions, colors, style), gap(8))
			}
			children = append(children, gap(6))
		}
	}
	if t := LeftOutText(g.LeftOut); t != "" {
		children = append(children, widgets.NewKeyedSubtree(text(t, dim), widgets.WidgetOpts{Key: SystemCheckLeftOutKey()}))
	}
	content := widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(pageSide, pageTop, pageSide, pageTop),
		widgets.NewColumn(children, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch, MainAxisSize: rendering.MainAxisSizeMin}))
	limited := widgets.NewAlign(widgets.NewConstrainedBox(rendering.NewBoxConstraints(rendering.BoxConstraintsOpts{MaxWidth: foundation.Some(maxWidth)}), content),
		widgets.AlignOpts{Alignment: painting.AlignmentTopLeft})
	return widgets.NewColoredBox(colors.EditorBackground, kit.NewScrollableElement(limited, kit.ScrollableElementOpts{Axes: kit.ScrollAxisVertical}))
}
