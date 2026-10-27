package markdown

import (
	"math/rand"
	"testing"
)

// naivePrefix is the sum the index must give.
func naivePrefix(vals []float64, i int) float64 {
	sum := 0.0
	for _, v := range vals[:i] {
		sum += v
	}
	return sum
}

// naiveFind is the row the distance falls in, by walking the rows.
func naiveFind(vals []float64, offset float64) int {
	at := 0.0
	for i, v := range vals {
		if at+v > offset {
			return i
		}
		at += v
	}
	return len(vals) - 1
}

func TestHeightIndexAgainstTheNaiveSums(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for _, n := range []int{1, 2, 3, 7, 8, 9, 100, 1000} {
		vals := make([]float64, n)
		for i := range vals {
			vals[i] = float64(1 + rng.Intn(50))
		}
		h := newHeightIndex(vals)
		for round := range 200 {
			if round%2 == 0 {
				i := rng.Intn(n)
				vals[i] = float64(1 + rng.Intn(80))
				h.Set(i, vals[i])
			}
			i := rng.Intn(n + 1)
			if got, want := h.Prefix(i), naivePrefix(vals, i); got != want {
				t.Fatalf("n=%d Prefix(%d) = %v, want %v", n, i, got, want)
			}
			offset := rng.Float64() * (naivePrefix(vals, n) + 20)
			if got, want := h.Find(offset), naiveFind(vals, offset); got != want {
				t.Fatalf("n=%d Find(%v) = %d, want %d", n, offset, got, want)
			}
		}
		if h.Total() != naivePrefix(vals, n) || h.Len() != n {
			t.Errorf("n=%d total %v len %d", n, h.Total(), h.Len())
		}
	}
}

func TestHeightIndexEdges(t *testing.T) {
	empty := newHeightIndex(nil)
	if empty.Find(10) != -1 || empty.Total() != 0 || empty.Prefix(3) != 0 {
		t.Errorf("empty index: find %d total %v", empty.Find(10), empty.Total())
	}
	h := newHeightIndex([]float64{10, 20, 30})
	tests := []struct {
		offset float64
		want   int
	}{{-5, 0}, {0, 0}, {9.9, 0}, {10, 1}, {29.9, 1}, {30, 2}, {59.9, 2}, {60, 2}, {1e9, 2}}
	for _, tt := range tests {
		if got := h.Find(tt.offset); got != tt.want {
			t.Errorf("Find(%v) = %d, want %d", tt.offset, got, tt.want)
		}
	}
}
