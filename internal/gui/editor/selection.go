package editor

import "math"

// cornerStyle is how a corner of a selection row is drawn.
type cornerStyle int

const (
	// cornerExtern is a corner that turns away from the neighboring row: rounded.
	cornerExtern cornerStyle = iota
	// cornerIntern is a corner next to a row that reaches further out: the selection fills
	// in the angle with a reversed rounded corner.
	cornerIntern
	// cornerFlat is a corner that lines up with the neighboring row: square.
	cornerFlat
)

// endStyle is the corners at one end of a row, above and below.
type endStyle struct {
	Top, Bottom cornerStyle
}

// selectionRow is the part of a selection on one row: its horizontal extent and, when the
// selection has rounded corners, the style of the corners of its two ends.
type selectionRow struct {
	// Row is the row of the projection.
	Row int
	// Left and Right are the x of the ends, from the left edge of the text.
	Left, Right float64
	// Start is the style of the left end; End the style of the right end.
	Start, End endStyle
}

// styleSelectionRows sets the corner styles of the consecutive rows of one selection: a
// corner is flat when it lines up with the row above or below (within a quarter of a
// character), intern when the neighbor reaches past it, and extern otherwise.
//
// Ported from VS Code: src/vs/editor/browser/viewParts/selections/selections.ts (_enrichVisibleRangesWithStyle)
func styleSelectionRows(rows []selectionRow, charWidth float64) {
	epsilon := charWidth / 4
	for i := range rows {
		cur := &rows[i]
		start := endStyle{cornerExtern, cornerExtern}
		end := endStyle{cornerExtern, cornerExtern}
		if i > 0 {
			prev := rows[i-1]
			switch {
			case math.Abs(cur.Left-prev.Left) < epsilon:
				start.Top = cornerFlat
			case cur.Left > prev.Left:
				start.Top = cornerIntern
			}
			switch {
			case math.Abs(cur.Right-prev.Right) < epsilon:
				end.Top = cornerFlat
			case prev.Left < cur.Right && cur.Right < prev.Right:
				end.Top = cornerIntern
			}
		}
		if i+1 < len(rows) {
			next := rows[i+1]
			switch {
			case math.Abs(cur.Left-next.Left) < epsilon:
				start.Bottom = cornerFlat
			case next.Left < cur.Left && cur.Left < next.Right:
				start.Bottom = cornerIntern
			}
			switch {
			case math.Abs(cur.Right-next.Right) < epsilon:
				end.Bottom = cornerFlat
			case cur.Right < next.Right:
				end.Bottom = cornerIntern
			}
		}
		cur.Start, cur.End = start, end
	}
}

// Radii of the pieces a selection is drawn with.
const (
	// selectionRadius is the radius of a rounded corner.
	selectionRadius = 3.0
	// innerCornerWidth is the width of the pieces that fill in a reversed corner.
	innerCornerWidth = 10.0
)

// selectionPiece is one rectangle of a selection. Background pieces are drawn in the color
// of the editor over the pieces of the selection, to cut out a reversed rounded corner.
type selectionPiece struct {
	Left, Right float64
	// TopInset and BottomInset shrink the piece, so that several selections are told apart.
	TopInset, BottomInset float64
	// Background says the piece has the color of the editor, not of the selection.
	Background bool
	// Round says which corners are rounded.
	TopLeft, TopRight, BottomRight, BottomLeft bool
}

// selectionPieces draws the rows of a selection as pieces, per row, the corners first (they
// must be below the pieces of the rest of the selection, as in VS Code). rounded is the
// editor.roundedSelection setting and multiple tells that the editor has several selections:
// the first row then starts one pixel lower and the last one ends one pixel higher.
//
// Ported from VS Code: src/vs/editor/browser/viewParts/selections/selections.ts (_actualRenderOneSelection)
func selectionPieces(rows []selectionRow, rounded, multiple bool) (corners, rest [][]selectionPiece) {
	corners = make([][]selectionPiece, len(rows))
	rest = make([][]selectionPiece, len(rows))
	for i, r := range rows {
		var top, bottom float64
		if multiple {
			if i == 0 {
				top = 1
			}
			if i != 0 && i == len(rows)-1 {
				bottom = 1
			}
		}
		if rounded {
			if r.Start.Top == cornerIntern || r.Start.Bottom == cornerIntern {
				corners[i] = append(corners[i],
					selectionPiece{Left: r.Left - innerCornerWidth, Right: r.Left, TopInset: top, BottomInset: bottom},
					selectionPiece{Left: r.Left - innerCornerWidth, Right: r.Left, TopInset: top, BottomInset: bottom, Background: true,
						TopRight: r.Start.Top == cornerIntern, BottomRight: r.Start.Bottom == cornerIntern})
			}
			if r.End.Top == cornerIntern || r.End.Bottom == cornerIntern {
				corners[i] = append(corners[i],
					selectionPiece{Left: r.Right, Right: r.Right + innerCornerWidth, TopInset: top, BottomInset: bottom},
					selectionPiece{Left: r.Right, Right: r.Right + innerCornerWidth, TopInset: top, BottomInset: bottom, Background: true,
						TopLeft: r.End.Top == cornerIntern, BottomLeft: r.End.Bottom == cornerIntern})
			}
		}
		p := selectionPiece{Left: r.Left, Right: r.Right, TopInset: top, BottomInset: bottom}
		if rounded {
			p.TopLeft = r.Start.Top == cornerExtern
			p.BottomLeft = r.Start.Bottom == cornerExtern
			p.TopRight = r.End.Top == cornerExtern
			p.BottomRight = r.End.Bottom == cornerExtern
		}
		rest[i] = []selectionPiece{p}
	}
	return corners, rest
}
