package buffer

import "testing"

func TestPositionCompare(t *testing.T) {
	tests := []struct {
		name string
		a, b Position
		want int
	}{
		{"same", Position{1, 2}, Position{1, 2}, 0},
		{"earlier column", Position{1, 2}, Position{1, 3}, -1},
		{"later column", Position{1, 3}, Position{1, 2}, 1},
		{"earlier line wins over column", Position{1, 9}, Position{2, 0}, -1},
		{"later line wins over column", Position{2, 0}, Position{1, 9}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.Compare(tt.b); got != tt.want {
				t.Errorf("Compare = %d, want %d", got, tt.want)
			}
			if got := tt.a.IsBefore(tt.b); got != (tt.want < 0) {
				t.Errorf("IsBefore = %v", got)
			}
			if got := tt.a.IsBeforeOrEqual(tt.b); got != (tt.want <= 0) {
				t.Errorf("IsBeforeOrEqual = %v", got)
			}
			if got := tt.a.Equal(tt.b); got != (tt.want == 0) {
				t.Errorf("Equal = %v", got)
			}
		})
	}
}

func TestPositionDeltaWith(t *testing.T) {
	p := Position{Line: 3, Col: 4}
	if got, want := p.Delta(-1, 2), (Position{2, 6}); got != want {
		t.Errorf("Delta = %+v, want %+v", got, want)
	}
	if got, want := p.With(0, 0), (Position{0, 0}); got != want {
		t.Errorf("With = %+v, want %+v", got, want)
	}
}

func TestMinMaxPosition(t *testing.T) {
	a, b := Position{1, 5}, Position{2, 0}
	for _, pair := range [][2]Position{{a, b}, {b, a}} {
		if got := MinPosition(pair[0], pair[1]); got != a {
			t.Errorf("MinPosition(%v, %v) = %v", pair[0], pair[1], got)
		}
		if got := MaxPosition(pair[0], pair[1]); got != b {
			t.Errorf("MaxPosition(%v, %v) = %v", pair[0], pair[1], got)
		}
	}
}
