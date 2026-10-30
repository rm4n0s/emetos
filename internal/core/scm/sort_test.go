package scm

import "testing"

func TestSortedResources(t *testing.T) {
	tests := []struct {
		name  string
		key   SortKey
		input []string
		tips  map[string]string
		want  []string
	}{
		{
			"path: the files of a folder come before the folders below it, and case does not matter for the folders",
			SortByPath,
			[]string{"/repo/b.txt", "/repo/a/x.txt", "/repo/A.txt", "/repo/a.txt", "/repo/src/b/c.go", "/repo/src/b.go", "/repo/Src/a.go"},
			nil,
			[]string{"/repo/A.txt", "/repo/a.txt", "/repo/b.txt", "/repo/a/x.txt", "/repo/Src/a.go", "/repo/src/b.go", "/repo/src/b/c.go"},
		},
		{
			// bytes: the path components are compared in lower case by their code points, so "e" (0x65) goes before "é" (0xC3 0xA9)
			"path: unicode folders",
			SortByPath,
			[]string{"/repo/é/a.go", "/repo/e/b.go", "/repo/日本/c.go", "/repo/z/d.go"},
			nil,
			[]string{"/repo/e/b.go", "/repo/z/d.go", "/repo/é/a.go", "/repo/日本/c.go"},
		},
		{
			"path: numbers in the last name are compared as numbers",
			SortByPath,
			[]string{"/repo/f/file10.go", "/repo/f/file2.go", "/repo/f/File1.go"},
			nil,
			[]string{"/repo/f/File1.go", "/repo/f/file2.go", "/repo/f/file10.go"},
		},
		{
			"name: numeric, case and accent insensitive; equal names keep the order they were given",
			SortByName,
			[]string{"/repo/z/file10.go", "/repo/z/file2.go", "/repo/y/File1.go", "/repo/a.go", "/repo/Ünï.go", "/repo/zeta.go", "/repo/m/a.go", "/repo/b/a.go"},
			nil,
			[]string{"/repo/a.go", "/repo/m/a.go", "/repo/b/a.go", "/repo/y/File1.go", "/repo/z/file2.go", "/repo/z/file10.go", "/repo/Ünï.go", "/repo/zeta.go"},
		},
		{
			"name: foo1 and foo01 are told apart by their code points",
			SortByName,
			[]string{"/repo/foo1", "/repo/foo01"},
			nil,
			[]string{"/repo/foo01", "/repo/foo1"},
		},
		{
			"status: by the tooltip, then by path",
			SortByStatus,
			[]string{"/repo/b.go", "/repo/a.go", "/repo/c.go", "/repo/d/e.go", "/repo/f.go"},
			map[string]string{"/repo/b.go": "Modified", "/repo/a.go": "Modified", "/repo/c.go": "Deleted", "/repo/d/e.go": "Deleted", "/repo/f.go": ""},
			[]string{"/repo/f.go", "/repo/c.go", "/repo/d/e.go", "/repo/a.go", "/repo/b.go"},
		},
		{"an empty group", SortByPath, nil, nil, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rs := resources(tt.input...)
			for i := range rs {
				rs[i].Decorations.Tooltip = tt.tips[tt.input[i]]
			}
			_, _, g := newGroup(t, rs)
			got := paths(g.Sorted(tt.key))
			if !equalStrings(got, tt.want) {
				t.Errorf("sorted by %s:\n got %v\nwant %v", tt.key, got, tt.want)
			}
			// the order is kept for the same version, and made again for the next
			if again := g.Sorted(tt.key); len(again) > 0 && &again[0] != &g.Sorted(tt.key)[0] {
				t.Error("the sorted list was made again for the same version of the resources")
			}
		})
	}
}

func TestSortedIsRemadeAfterSetResources(t *testing.T) {
	_, _, g := newGroup(t, resources("/repo/b.go", "/repo/a.go"))
	if got := paths(g.Sorted(SortByPath)); !equalStrings(got, []string{"/repo/a.go", "/repo/b.go"}) {
		t.Fatalf("got %v", got)
	}
	g.SetResources(resources("/repo/z.go", "/repo/c.go", "/repo/a.go"))
	if got := paths(g.Sorted(SortByPath)); !equalStrings(got, []string{"/repo/a.go", "/repo/c.go", "/repo/z.go"}) {
		t.Errorf("got %v", got)
	}
	if got := paths(g.Sorted(SortByName)); !equalStrings(got, []string{"/repo/a.go", "/repo/c.go", "/repo/z.go"}) {
		t.Errorf("by name: got %v", got)
	}
}

func TestComparePaths(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"/a/b", "/a/b", 0},
		{"/a/b", "/a/c", -1},
		{"/a/B", "/a/b", -1}, // equal as names; told apart by code points
		{"/a/x", "/a/b/c", -1},
		{"/a/b/c", "/a/x", 1},
		{"/A/x", "/a/y", -1},
		{"/home/u/proj", "/home/u/proj2", -1},
		{"/home/u/proj/sub", "/home/u/proj2", 1}, // the last component of one path ends first
	}
	for _, tt := range tests {
		if got := ComparePaths(tt.a, tt.b); got != tt.want {
			t.Errorf("ComparePaths(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestParseSortKey(t *testing.T) {
	for in, want := range map[string]SortKey{"name": SortByName, "status": SortByStatus, "path": SortByPath, "": SortByPath, "other": SortByPath} {
		if got := ParseSortKey(in); got != want {
			t.Errorf("ParseSortKey(%q) = %q, want %q", in, got, want)
		}
	}
}
