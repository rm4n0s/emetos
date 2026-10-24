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
		{"replace keeps the order and drops repeats, empties and the paths past the limit", 3, func(r *RecentFiles) {
			r.Add("/old")
			r.Replace([]string{"/a", "", "/b", "/a", "/c", "/d"})
		}, []string{"/a", "/b", "/c"}},
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

func TestRecentFilesOnChange(t *testing.T) {
	tests := []struct {
		name string
		do   func(r *RecentFiles)
		want int
	}{
		{"add", func(r *RecentFiles) { r.Add("/a") }, 1},
		{"the first again changes nothing", func(r *RecentFiles) { r.Add("/a"); r.Add("/a") }, 1},
		{"remove of a path that is not there changes nothing", func(r *RecentFiles) { r.Add("/a"); r.Remove("/b") }, 1},
		{"remove", func(r *RecentFiles) { r.Add("/a"); r.Remove("/a") }, 2},
		{"rename of nothing changes nothing", func(r *RecentFiles) { r.Add("/a"); r.Rename("/b", "/c") }, 1},
		{"rename", func(r *RecentFiles) { r.Add("/a"); r.Rename("/a", "/c") }, 2},
		{"clear of an empty list changes nothing", func(r *RecentFiles) { r.Clear() }, 0},
		{"clear", func(r *RecentFiles) { r.Add("/a"); r.Clear() }, 2},
		{"replace", func(r *RecentFiles) { r.Replace([]string{"/a"}) }, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRecentFiles(5)
			n := 0
			stop := r.OnChange(func() { n++ })
			tc.do(r)
			if n != tc.want {
				t.Errorf("%d notifications, want %d", n, tc.want)
			}
			stop()
			r.Add("/zzz")
			if n != tc.want {
				t.Error("notified after the listener was removed")
			}
		})
	}
}
