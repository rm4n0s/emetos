package pass

import (
	"fmt"
	"testing"
)

func TestAdd(t *testing.T) {
	if got := Add(2, 2); got != 4 {
		t.Fatalf("Add(2, 2) = %d, want 4", got)
	}
}

func TestTable(t *testing.T) {
	t.Run("small", func(t *testing.T) {
		if Add(1, 1) != 2 {
			t.Fatal("no")
		}
	})
	for _, tc := range []struct {
		name string
		a, b int
		want int
	}{{"two words", 1, 2, 3}, {"negative", -1, -2, -3}} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Add(tc.a, tc.b); got != tc.want {
				t.Errorf("Add(%d, %d) = %d, want %d", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

func BenchmarkAdd(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Add(i, i)
	}
}

func ExampleAdd() {
	fmt.Println(Add(1, 2))
	// Output: 3
}
