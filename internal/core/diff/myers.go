package diff

// Ported from VS Code: src/vs/editor/common/diff/defaultLinesDiffComputer/algorithms/myersDiffAlgorithm.ts

// snakePath is a step of the path of Myers' algorithm: a run of equal elements
// starting at (x, y), and the path before it.
type snakePath struct {
	prev   *snakePath
	x, y   int
	length int
}

// diagonals is an array that is indexed by the diagonal k of Myers' algorithm,
// which can be negative. Reading an index that was never written gives the
// zero value.
type diagonals[T any] struct {
	pos, neg []T
}

func (a *diagonals[T]) get(k int) T {
	var zero T
	if k < 0 {
		k = -k - 1
		if k >= len(a.neg) {
			return zero
		}
		return a.neg[k]
	}
	if k >= len(a.pos) {
		return zero
	}
	return a.pos[k]
}

func (a *diagonals[T]) set(k int, v T) {
	if k < 0 {
		k = -k - 1
		a.neg = grow(a.neg, k)
		a.neg[k] = v
		return
	}
	a.pos = grow(a.pos, k)
	a.pos[k] = v
}

// grow makes s long enough for the index i, doubling its length.
func grow[T any](s []T, i int) []T {
	if i < len(s) {
		return s
	}
	n := max(10, len(s))
	for n <= i {
		n *= 2
	}
	t := make([]T, n)
	copy(t, s)
	return t
}

// myers is VS Code's MyersDiffAlgorithm: an O(ND) diff algorithm that has a
// quadratic space worst case. It returns the changes and whether the time
// limit was reached (the changes are then the trivial diff).
func myers(b *budget, seq1, seq2 sequence) ([]sequenceDiff, bool, error) {
	// These are common special cases; the early return improves performance.
	if seq1.length() == 0 || seq2.length() == 0 {
		return trivialDiff(seq1, seq2), false, nil
	}

	seqX, seqY := seq1, seq2 // text on the x axis, text on the y axis
	lenX, lenY := seqX.length(), seqY.length()

	xAfterSnake := func(x, y int) int {
		for x < lenX && y < lenY && seqX.element(x) == seqY.element(y) {
			x++
			y++
		}
		return x
	}

	// v[k]: X value of the longest d-line that ends in diagonal k. A d-line is
	// a path from (0,0) to (x,y) that uses exactly d non-diagonals; diagonal k
	// is the set of points (x,y) with x-y = k.
	var v diagonals[int]
	v.set(0, xAfterSnake(0, 0))

	var paths diagonals[*snakePath]
	if v.get(0) != 0 {
		paths.set(0, &snakePath{nil, 0, 0, v.get(0)})
	}

	d := 0
	k := 0
loop:
	for {
		d++
		expired, err := b.expired()
		if err != nil {
			return nil, false, err
		}
		if expired {
			return trivialDiff(seqX, seqY), true, nil
		}
		// The paper has `for k = -d; k <= d; k += 2`, but diagonals that
		// cannot influence the result are ignored.
		lowerBound := -min(d, lenY+d%2)
		upperBound := min(d, lenX+d%2)
		for k = lowerBound; k <= upperBound; k += 2 {
			// The X values of the (d-1)-lines give the X value of the longest
			// d-lines.
			maxXOfDLineTop := -1 // a vertical non-diagonal (adds a symbol in seqX)
			if k != upperBound {
				maxXOfDLineTop = v.get(k + 1)
			}
			maxXOfDLineLeft := -1 // a horizontal non-diagonal (deletes a symbol in seqX)
			if k != lowerBound {
				maxXOfDLineLeft = v.get(k-1) + 1
			}
			x := min(max(maxXOfDLineTop, maxXOfDLineLeft), lenX)
			y := x - k
			if x > lenX || y > lenY {
				// This diagonal is irrelevant for the result.
				continue
			}
			newMaxX := xAfterSnake(x, y)
			v.set(k, newMaxX)
			var lastPath *snakePath
			if x == maxXOfDLineTop {
				lastPath = paths.get(k + 1)
			} else {
				lastPath = paths.get(k - 1)
			}
			if newMaxX != x {
				paths.set(k, &snakePath{lastPath, x, y, newMaxX - x})
			} else {
				paths.set(k, lastPath)
			}

			if v.get(k) == lenX && v.get(k)-k == lenY {
				break loop
			}
		}
	}

	path := paths.get(k)
	var result []sequenceDiff
	lastPos1, lastPos2 := lenX, lenY

	for {
		endX, endY := 0, 0
		if path != nil {
			endX = path.x + path.length
			endY = path.y + path.length
		}
		if endX != lastPos1 || endY != lastPos2 {
			result = append(result, sequenceDiff{offsetRange{endX, lastPos1}, offsetRange{endY, lastPos2}})
		}
		if path == nil {
			break
		}
		lastPos1, lastPos2 = path.x, path.y
		path = path.prev
	}

	reverse(result)
	return result, false, nil
}
