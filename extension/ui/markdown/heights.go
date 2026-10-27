package markdown

// heightIndex holds the height of every row of a long list and answers two questions in O(log n): how far down a row starts
// (the sum of the rows before it) and which row a distance falls in. It is a Fenwick tree over the heights, so a row that was
// measured (or estimated again) changes one height without summing the list again.
type heightIndex struct {
	vals []float64
	tree []float64
}

// newHeightIndex makes an index of the heights, in O(n).
func newHeightIndex(heights []float64) *heightIndex {
	h := &heightIndex{vals: append([]float64(nil), heights...), tree: make([]float64, len(heights)+1)}
	for i, v := range h.vals {
		h.tree[i+1] += v
		if j := i + 1 + ((i + 1) & -(i + 1)); j < len(h.tree) {
			h.tree[j] += h.tree[i+1]
		}
	}
	return h
}

// Len is the number of rows.
func (h *heightIndex) Len() int { return len(h.vals) }

// Get is the height of row i.
func (h *heightIndex) Get(i int) float64 { return h.vals[i] }

// Set changes the height of row i.
func (h *heightIndex) Set(i int, v float64) {
	delta := v - h.vals[i]
	if delta == 0 {
		return
	}
	h.vals[i] = v
	for j := i + 1; j < len(h.tree); j += j & -j {
		h.tree[j] += delta
	}
}

// Prefix is the sum of the heights of the rows before row i (how far down row i starts); Prefix(Len()) is the total.
func (h *heightIndex) Prefix(i int) float64 {
	sum := 0.0
	for j := min(max(i, 0), len(h.vals)); j > 0; j -= j & -j {
		sum += h.tree[j]
	}
	return sum
}

// Total is the sum of all the heights.
func (h *heightIndex) Total() float64 { return h.Prefix(len(h.vals)) }

// Find is the row that the distance falls in: the last row i with Prefix(i) <= offset, so a distance exactly on the boundary
// of two rows is in the second. A distance before the first row gives 0, one past the end the last row; an empty index gives -1.
func (h *heightIndex) Find(offset float64) int {
	n := len(h.vals)
	if n == 0 {
		return -1
	}
	if offset <= 0 {
		return 0
	}
	pos, rest := 0, offset
	step := 1
	for step*2 <= n {
		step *= 2
	}
	for ; step > 0; step >>= 1 {
		if next := pos + step; next <= n && h.tree[next] <= rest {
			pos = next
			rest -= h.tree[next]
		}
	}
	return min(pos, n-1)
}
