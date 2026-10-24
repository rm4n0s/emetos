package workspace

import (
	"slices"
	"testing"
)

func TestRecentFiles(t *testing.T) {
	tests := []struct {
		name  string
		limit int
		do    func(r *RecentFiles)
		want  []string
	}{
		{"most recent first", 5, func(r *RecentFiles) { r.Add("/a"); r.Add("/b"); r.Add("/c") }, []string{"/c", "/b", "/a"}},
		{"a repeat moves to the front", 5, func(r *RecentFiles) { r.Add("/a"); r.Add("/b"); r.Add("/a") }, []string{"/a", "/b"}},
		{"the oldest goes when full", 2, func(r *RecentFiles) { r.Add("/a"); r.Add("/b"); r.Add("/c") }, []string{"/c", "/b"}},
		{"an empty path is not added", 5, func(r *RecentFiles) { r.Add(""); r.Add("/a") }, []string{"/a"}},
		{"remove", 5, func(r *RecentFiles) { r.Add("/a"); r.Add("/b"); r.Remove("/a"); r.Remove("/none") }, []string{"/b"}},
		{"rename keeps the place and follows folders", 5, func(r *RecentFiles) {
			r.Add("/d/x")
			r.Add("/other")
			r.Add("/d")
			r.Rename("/d", "/e")
		}, []string{"/e", "/other", "/e/x"}},
		{"clear", 5, func(r *RecentFiles) { r.Add("/a"); r.Clear() }, nil},
		{"the default limit", 0, func(r *RecentFiles) {
			for i := range DefaultRecentLimit + 10 {
				r.Add(string(rune('A'+i%26)) + string(rune('a'+i/26)))
			}
		}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRecentFiles(tc.limit)
			tc.do(r)
			got := r.List()
			if tc.name == "the default limit" {
				if len(got) != DefaultRecentLimit || r.Len() != DefaultRecentLimit {
					t.Errorf("%d files, want %d", len(got), DefaultRecentLimit)
				}
				return
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
