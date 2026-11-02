package debug

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/dap"
	"github.com/rm4n0s/emetos/internal/core/debug"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// BreakpointsPane is the Breakpoints view: the exception filters of the focused session (of the one that was focused last when none
// runs) with their conditions, the function breakpoints, and the breakpoints of lines and columns, each with a check box that enables
// it, an icon that says what kind it is and whether the adapter verified it, its file with the folder dimmed after it, and the line
// in a badge. The list is flat; the setting debug.breakpointsView.presentation is registered and "tree" shows the same list
// (decision 1192).
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/breakpointsView.ts (BreakpointsView, BreakpointsRenderer,
// FunctionBreakpointsRenderer, ExceptionBreakpointsRenderer)
type BreakpointsPane struct {
	*base
	// folder makes the folder of a file shorter: the path relative to it.
	folder string
	// naming is the function breakpoint whose name is being typed.
	naming string
}

func newBreakpointsPane(opts Opts) *BreakpointsPane {
	b := &BreakpointsPane{}
	b.base = newBase(opts, "workbench.debug.breakPointsView", kit.TreeModelOpts{})
	return b
}

// SetFolder gives the view the folder of the workspace, which the folders of the files are shown relative to.
func (b *BreakpointsPane) SetFolder(folder string) { b.folder = folder }

func (b *BreakpointsPane) refresh(debug.Event) {
	b.rebuild()
	b.notify()
}

func (b *BreakpointsPane) rebuild() {
	svc := b.opts.Service
	m := svc.Model()
	b.rows = map[string]Row{}
	var items []kit.TreeItem
	add := func(row Row, label string) {
		b.rows[row.Key] = row
		items = append(items, kit.TreeItem{Key: row.Key, Data: row, Label: label})
	}
	sessionID := ""
	if f := m.Focus(); f.Session != nil {
		sessionID = f.Session.ID()
	}
	for _, e := range m.ExceptionBreakpoints(sessionID) {
		add(Row{Kind: RowException, Key: "ebp:" + e.ID, Exception: e}, e.Label)
	}
	pending := ""
	for _, f := range m.FunctionBreakpoints() {
		add(Row{Kind: RowFunctionBreakpoint, Key: "fbp:" + f.ID, Function: f}, f.Name)
		if f.Name == "" {
			pending = f.ID
		}
	}
	for _, bp := range m.Breakpoints(debug.BreakpointFilter{}) {
		add(Row{Kind: RowBreakpoint, Key: "bp:" + bp.ID, Breakpoint: bp}, filepath.Base(bp.Path)+":"+strconv.Itoa(bp.Line))
	}
	_ = b.model.SetItems(items)
	b.message = ""
	if len(items) == 0 {
		b.message = ""
	}
	// a function breakpoint without a name is the row the user is about to name
	if pending != "" && b.naming != pending {
		b.naming = pending
		b.nameFunction(pending, "")
	}
}

// nameFunction opens the input on a function breakpoint; an empty name removes a breakpoint that has none.
func (b *BreakpointsPane) nameFunction(id, current string) {
	key := "fbp:" + id
	b.StartEdit(key, current, func(text string, ok bool) {
		if b.naming == id {
			b.naming = ""
		}
		svc := b.opts.Service
		go func() {
			defer b.opts.Post(func() { b.rebuild(); b.notify() })
			if !ok || text == "" {
				if current == "" {
					_ = svc.RemoveFunctionBreakpoints(b.ctx, id)
				}
				return
			}
			_ = svc.UpdateFunctionBreakpoint(b.ctx, id, &text, nil, nil)
		}()
	})
}

// Edit opens the input on a function breakpoint (its name) or an exception filter (its condition, when it takes one).
func (b *BreakpointsPane) Edit(r Row) {
	svc := b.opts.Service
	switch r.Kind {
	case RowFunctionBreakpoint:
		b.nameFunction(r.Function.ID, r.Function.Name)
	case RowException:
		if !r.Exception.SupportsCondition {
			return
		}
		e := r.Exception
		b.StartEdit(r.Key, e.Condition, func(text string, ok bool) {
			if !ok {
				return
			}
			go func() {
				_ = svc.SetExceptionBreakpointCondition(b.ctx, e.ID, text)
				b.opts.Post(func() { b.rebuild(); b.notify() })
			}()
		})
	}
}

// Toggle enables or disables the breakpoint of a row (the check box, and Space).
func (b *BreakpointsPane) Toggle(r Row) {
	var id string
	var enabled bool
	switch r.Kind {
	case RowBreakpoint:
		id, enabled = r.Breakpoint.ID, r.Breakpoint.Enabled
	case RowFunctionBreakpoint:
		id, enabled = r.Function.ID, r.Function.Enabled
	case RowException:
		id, enabled = r.Exception.ID, r.Exception.Enabled
	default:
		return
	}
	svc := b.opts.Service
	go func() {
		_ = svc.EnableOrDisableBreakpoints(b.ctx, !enabled, id)
		b.opts.Post(func() { b.rebuild(); b.notify() })
	}()
}

// Remove removes the breakpoint of a row (Delete). An exception filter stays.
func (b *BreakpointsPane) Remove(r Row) {
	svc := b.opts.Service
	switch r.Kind {
	case RowBreakpoint:
		go func() { _ = svc.RemoveBreakpoints(b.ctx, r.Breakpoint.ID) }()
	case RowFunctionBreakpoint:
		go func() { _ = svc.RemoveFunctionBreakpoints(b.ctx, r.Function.ID) }()
	}
}

// iconOf is the Codicon and the color of the icon of a breakpoint: the kind (plain, conditional, log, function), dimmed while the
// breakpoint is disabled or unverified or the breakpoints are not activated.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/breakpointsView.ts (getBreakpointMessageAndIcon)
func iconOf(colors kit.Colors, activated, enabled, verified, supported bool, kind string) (code int, color ui.Color) {
	active := activated && enabled && verified
	colorID := "debugIcon.breakpointForeground"
	if !active {
		colorID = "debugIcon.breakpointDisabledForeground"
		if activated && enabled && !verified {
			colorID = "debugIcon.breakpointUnverifiedForeground"
		}
	}
	color = named(colors, colorID, colors.Foreground)
	switch kind {
	case "function":
		if verified {
			return fonts.CodiconDebugBreakpointFunction, color
		}
		return fonts.CodiconDebugBreakpointFunctionUnverified, color
	case "conditional":
		if verified {
			return fonts.CodiconDebugBreakpointConditional, color
		}
		return fonts.CodiconDebugBreakpointConditionalUnverified, color
	case "log":
		if verified {
			return fonts.CodiconDebugBreakpointLog, color
		}
		return fonts.CodiconDebugBreakpointLogUnverified, color
	}
	if !supported {
		return fonts.CodiconDebugBreakpointUnsupported, color
	}
	if !active {
		return fonts.CodiconCircle, color
	}
	return fonts.CodiconCircleFilled, color
}

// relDir is the folder of a path shown relative to the folder of the workspace; empty for a file directly in it.
func (b *BreakpointsPane) relDir(path string) string {
	dir := filepath.Dir(path)
	if b.folder != "" {
		if rel, err := filepath.Rel(b.folder, dir); err == nil && !strings.HasPrefix(rel, "..") {
			if rel == "." {
				return ""
			}
			return rel
		}
	}
	return dir
}

// Render builds the contents of a row.
func (b *BreakpointsPane) Render(bc widgets.BuildContext, n kit.TreeNode, st kit.RowState) widgets.Widget {
	row, ok := n.Data.(Row)
	if !ok {
		return kit.NewHighlightedLabel(bc, n.Label, st)
	}
	colors := kit.ThemeOf(bc)
	activated := b.opts.Service.Model().BreakpointsActivated()
	base := kit.TextStyleOf(bc, rowFontSize, st.Foreground)
	dim := base.CopyWith(painting.TextStyleOpts{Color: foundation.Some(st.Foreground.WithOpacity(0.6))})
	gap := func(w float64) widgets.Widget {
		return widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(w)})
	}
	check := func(on bool) widgets.Widget {
		return kit.NewCheckbox("", kit.CheckboxOpts{Checked: on, Selected: st.Selected && st.Active, OnChanged: func(bool) { b.Toggle(row) }})
	}
	icon := func(code int, c ui.Color) widgets.Widget {
		return widgets.NewIcon(fonts.CodiconIcon(code), widgets.IconOpts{Size: foundation.Some(16.0), Color: foundation.Some(c)})
	}
	text := func(s string, style *painting.TextStyle) painting.InlineSpan {
		return painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(s), Style: style})
	}
	line := func(spans ...painting.InlineSpan) widgets.Widget {
		return widgets.NewRichText(painting.NewTextSpan(painting.TextSpanOpts{Children: spans}),
			widgets.RichTextOpts{MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})
	}
	label := func(style *painting.TextStyle, s string) painting.InlineSpan { return text(s, style) }
	switch row.Kind {
	case RowException:
		e := row.Exception
		if b.edit != nil && b.edit.key == n.Key {
			return widgets.NewRow([]widgets.Widget{check(e.Enabled), gap(4), widgets.NewFlexible(line(label(base, e.Label+" "))), gap(4), widgets.NewExpanded(b.editBox())},
				widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
		}
		spans := []painting.InlineSpan{label(base, e.Label)}
		if e.Condition != "" {
			spans = append(spans, label(dim, "  "+e.Condition))
		} else if e.Description != "" {
			spans = append(spans, label(dim, "  "+e.Description))
		}
		return widgets.NewRow([]widgets.Widget{check(e.Enabled), gap(4), widgets.NewFlexible(line(spans...))},
			widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
	case RowFunctionBreakpoint:
		f := row.Function
		code, c := iconOf(colors, activated, f.Enabled, f.Verified, f.Supported, "function")
		cells := []widgets.Widget{icon(code, c), gap(2), check(f.Enabled), gap(4)}
		if b.edit != nil && b.edit.key == n.Key {
			cells = append(cells, widgets.NewExpanded(b.editBox()))
		} else {
			spans := []painting.InlineSpan{label(base, f.Name)}
			if f.Condition != "" {
				spans = append(spans, label(dim, "  "+f.Condition))
			}
			cells = append(cells, widgets.NewFlexible(line(spans...)))
		}
		return widgets.NewRow(cells, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
	case RowBreakpoint:
		bp := row.Breakpoint
		kind := ""
		switch {
		case bp.IsLogpoint():
			kind = "log"
		case bp.Condition != "" || bp.HitCondition != "":
			kind = "conditional"
		}
		code, c := iconOf(colors, activated, bp.Enabled, bp.Verified, bp.Supported, kind)
		spans := []painting.InlineSpan{label(base, filepath.Base(bp.Path))}
		if dir := b.relDir(bp.Path); dir != "" {
			spans = append(spans, label(dim, "  "+dir))
		}
		badge := strconv.Itoa(bp.Line)
		if bp.Column > 0 {
			badge += ":" + strconv.Itoa(bp.Column)
		}
		if bp.ModeLabel != "" {
			badge = bp.ModeLabel + ": " + badge
		}
		return widgets.NewRow([]widgets.Widget{icon(code, c), gap(2), check(bp.Enabled), gap(4), widgets.NewFlexible(line(spans...)), gap(6), lineBadge(bc, badge, st)},
			widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
	}
	return kit.NewHighlightedLabel(bc, n.Label, st)
}

var _ = dap.StopBreakpoint
