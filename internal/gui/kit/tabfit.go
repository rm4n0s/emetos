package kit

// The widths of the tabs of a strip that shrinks its tabs before it scrolls (the workspace tabs of the title bar,
// SPEC §10.14). This file has no widgets: the layout is plain arithmetic, tested on its own.

// Widths of a workspace tab (SPEC §10.14): a tab is as wide as its label needs, at least 120 px and at most 240 px; when
// the tabs do not fit they shrink, the widest first, down to 120 px, and only then does the strip scroll.
const (
	// WorkspaceTabMinWidth is the narrowest a workspace tab gets.
	WorkspaceTabMinWidth = 120.0
	// WorkspaceTabMaxWidth is the widest a workspace tab gets.
	WorkspaceTabMaxWidth = 240.0
)

// FitTabWidths gives the width of each tab of a strip that is available wide. natural is the width each tab wants (its
// label, icon and close button); the result is that width clamped to minW and maxW, and, when the sum is more than
// available, reduced by taking the excess from the widest tabs first (they are levelled down together until the sum
// fits), never below minW. scrolls is true when the tabs, all at minW, still do not fit: the strip then scrolls
// horizontally. An available width of 0 or less, or no tabs, gives the clamped natural widths.
func FitTabWidths(natural []float64, available, minW, maxW float64) (widths []float64, scrolls bool) {
	widths = make([]float64, len(natural))
	sum := 0.0
	for i, n := range natural {
		w := n
		w = max(minW, min(w, maxW))
		widths[i] = w
		sum += w
	}
	if available <= 0 || sum <= available {
		return widths, false
	}
	if float64(len(widths))*minW >= available {
		for i := range widths {
			widths[i] = minW
		}
		return widths, float64(len(widths))*minW > available
	}
	// level the widest tabs down: find the cap c such that the sum of min(w, c) equals the available width
	lo, hi := minW, maxW
	for range 40 {
		mid := (lo + hi) / 2
		total := 0.0
		for _, w := range widths {
			total += min(w, mid)
		}
		if total > available {
			hi = mid
		} else {
			lo = mid
		}
	}
	for i, w := range widths {
		widths[i] = min(w, lo)
	}
	return widths, false
}
