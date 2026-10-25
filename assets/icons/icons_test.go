package icons

import "testing"

func TestPNGHasEverySizeOfTheIcon(t *testing.T) {
	for _, tc := range []struct {
		size int
		ok   bool
	}{{16, true}, {22, true}, {24, true}, {32, true}, {48, true}, {64, true}, {128, true}, {256, true}, {512, true}, {0, false}, {17, false}, {-16, false}} {
		data, ok := PNG(tc.size)
		if ok != tc.ok || (ok && len(data) < 8) {
			t.Errorf("PNG(%d) = %d bytes, %v; want %v", tc.size, len(data), ok, tc.ok)
		}
		if ok && string(data[1:4]) != "PNG" {
			t.Errorf("PNG(%d) is not a PNG file", tc.size)
		}
	}
}
