package terminal

import (
	"strings"

	"github.com/rm4n0s/glutter/rendering"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
)

// renderGrid is the render object of one terminal: it takes all the space it is given, tells the terminal how many columns and
// rows fit, and paints the cells (SPEC §10.25 "Speed"). It is a repaint boundary, so Glutter's raster cache keeps the picture of a
// terminal that did not change, and a frame with no change draws nothing.
type renderGrid struct {
	rendering.RenderBox
	pv *paneView
}

func newRenderGrid(pv *paneView) *renderGrid {
	r := &renderGrid{pv: pv}
	rendering.InitRenderBox(&r.RenderBox, r)
	return r
}

// Attach implements rendering.RenderObject.
func (r *renderGrid) Attach(owner *rendering.PipelineOwner) {
	r.RenderBox.Attach(owner)
	r.pv.mounted(r)
}

// Detach implements rendering.RenderObject.
func (r *renderGrid) Detach() {
	r.pv.unmounted(r)
	r.RenderBox.Detach()
}

// SizedByParent implements rendering.RenderObject.
func (r *renderGrid) SizedByParent() bool { return true }

// IsRepaintBoundary implements rendering.RenderObject.
func (r *renderGrid) IsRepaintBoundary() bool { return true }

// HitTestSelf implements rendering.RenderBox.
func (r *renderGrid) HitTestSelf(gui.Offset) bool { return true }

// ComputeMinIntrinsicWidth implements rendering.RenderBox.
func (r *renderGrid) ComputeMinIntrinsicWidth(float64) float64 { return 0 }

// ComputeMaxIntrinsicWidth implements rendering.RenderBox.
func (r *renderGrid) ComputeMaxIntrinsicWidth(float64) float64 { return 0 }

// ComputeMinIntrinsicHeight implements rendering.RenderBox.
func (r *renderGrid) ComputeMinIntrinsicHeight(float64) float64 { return 0 }

// ComputeMaxIntrinsicHeight implements rendering.RenderBox.
func (r *renderGrid) ComputeMaxIntrinsicHeight(float64) float64 { return 0 }

// ComputeDryLayout implements rendering.RenderBox.
func (r *renderGrid) ComputeDryLayout(c rendering.BoxConstraints) gui.Size {
	return c.Constrain(c.Biggest())
}

// PerformLayout implements rendering.RenderObject: the terminal learns how many cells fit.
func (r *renderGrid) PerformLayout() { r.pv.layout(r.Size()) }

// Paint implements rendering.RenderObject.
func (r *renderGrid) Paint(ctx *rendering.PaintingContext, offset gui.Offset) {
	r.pv.paint(ctx.Canvas(), offset, r.Size())
}

// gridWidget is the widget of renderGrid.
type gridWidget struct {
	widgets.LeafRenderObjectWidget
	pv *paneView
}

func newGridWidget(pv *paneView) *gridWidget {
	return &gridWidget{LeafRenderObjectWidget: widgets.NewLeafRenderObjectWidgetBase(), pv: pv}
}

// CreateRenderObject implements widgets.RenderObjectWidget.
func (w *gridWidget) CreateRenderObject(bc widgets.BuildContext) rendering.RenderObject {
	w.pv.setContext(bc)
	return newRenderGrid(w.pv)
}

// UpdateRenderObject implements widgets.RenderObjectWidget.
func (w *gridWidget) UpdateRenderObject(bc widgets.BuildContext, ro rendering.RenderObject) {
	r := ro.(*renderGrid)
	r.pv = w.pv
	w.pv.setContext(bc)
	r.MarkNeedsLayout()
	r.MarkNeedsPaint()
}

// devState is what dev.terminals reports about a terminal on screen (SPEC §15.2): the tests read the text the glyph atlas
// drew, which no text widget holds.
type devState struct {
	// Text is the rows on screen, trailing spaces removed, joined by newlines.
	Text string `json:"text"`
	// Selected is the selected text.
	Selected string `json:"selected"`
	// Scroll is how many lines above the bottom the view is, and Scrollback how many lines can be scrolled back.
	Scroll     int `json:"scroll"`
	Scrollback int `json:"scrollback"`
	// Cols and Rows are the size of the terminal in cells.
	Cols int `json:"cols"`
	Rows int `json:"rows"`
	// Focused tells that the pane has the keyboard focus, Alt that the program is on the alternate screen.
	Focused bool `json:"focused"`
	Alt     bool `json:"alt"`
	// Title is the name of the tab of the terminal.
	Title string `json:"title"`
	// Pid is the process of the program.
	Pid int `json:"pid"`
	// CellW and CellH are the size of a cell and PadLeft the space left of the first column, in logical pixels, so that a
	// test can click a cell.
	CellW   float64 `json:"cellW"`
	CellH   float64 `json:"cellH"`
	PadLeft float64 `json:"padLeft"`
}

// DevTerminal answers dev.terminals for the app: the state of the terminal this render object draws.
func (r *renderGrid) DevTerminal() any {
	pv := r.pv
	st := devState{
		Selected: pv.selectedText(), Scroll: pv.scroll, Scrollback: pv.frame.Scrollback, Cols: pv.cols, Rows: pv.rows,
		Focused: pv.focused, Alt: pv.frame.Modes.AltScreen, Pid: pv.term.Pid(), PadLeft: padLeft,
	}
	if pv.factory != nil {
		a, _ := pv.atlases()
		st.CellW, st.CellH = a.m.w, a.m.h
	}
	if f := pv.shown; f != nil {
		rows := make([]string, f.Rows)
		for y := range rows {
			rows[y] = f.RowText(y)
		}
		st.Text = strings.TrimRight(strings.Join(rows, "\n"), "\n")
	}
	return st
}
