package cachehome

import "testing"

func TestDir(t *testing.T) {
	tests := []struct {
		name     string
		xdg, hom string
		want     string
		wantOK   bool
	}{
		{"XDG_CACHE_HOME wins", "/x/cache", "/home/ann", "/x/cache", true},
		{"HOME/.cache otherwise", "", "/home/ann", "/home/ann/.cache", true},
		{"a relative XDG_CACHE_HOME", "cache", "/home/ann", "cache", false},
		{"nothing is set", "", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_CACHE_HOME", tt.xdg)
			t.Setenv("HOME", tt.hom)
			got, ok := Dir()
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("Dir() = %q, %v; want %q, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
