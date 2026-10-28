package references

import (
	"slices"
	"testing"

	"github.com/rm4n0s/emetos/extension"
)

// at makes a location of one line: the range is [col, col+len) on the line.
func at(uri string, line, col, width int) extension.Location {
	return extension.Location{URI: uri, Range: extension.Range{
		Start: extension.Position{Line: line, Col: col}, End: extension.Position{Line: line, Col: col + width},
	}}
}

// keys lists the keys of the files of a model with the number of the locations of each.
func shape(m *model) []string {
	var out []string
	for _, f := range m.files {
		out = append(out, f.uri)
		for _, r := range f.refs {
			out = append(out, "  "+r.key())
		}
	}
	return out
}

func TestTheModelGroupsAndSortsTheLocations(t *testing.T) {
	// Ported from VS Code: references/model.ts (ReferencesModel constructor, _compareLocations): by URI as strings, then by start.
	m := newModel([]extension.Location{
		at("file:///b.go", 4, 2, 3), at("file:///a.go", 9, 1, 3), at("file:///a.go", 2, 8, 3), at("file:///a.go", 2, 1, 3),
	})
	want := []string{
		"file:///a.go", "  r:file:///a.go@2:1-2:4", "  r:file:///a.go@2:8-2:11", "  r:file:///a.go@9:1-9:4",
		"file:///b.go", "  r:file:///b.go@4:2-4:5",
	}
	if got := shape(m); !slices.Equal(got, want) {
		t.Errorf("model = %q\nwant    %q", got, want)
	}
}

func TestTheMessageCountsResultsAndFiles(t *testing.T) {
	// the strings of VS Code's ReferencesModel.message
	tests := []struct {
		name string
		locs []extension.Location
		want string
	}{
		{"none", nil, "No results."},
		{"one in one file", []extension.Location{at("file:///a", 0, 0, 1)}, "1 result in 1 file"},
		{"one in two files is impossible, two in one file", []extension.Location{at("file:///a", 0, 0, 1), at("file:///a", 1, 0, 1)}, "2 results in 1 file"},
		{"two in two files", []extension.Location{at("file:///a", 0, 0, 1), at("file:///b", 1, 0, 1)}, "2 results in 2 files"},
		{"three in two files", []extension.Location{at("file:///a", 0, 0, 1), at("file:///a", 1, 0, 1), at("file:///b", 1, 0, 1)}, "3 results in 2 files"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := newModel(tc.locs).message(); got != tc.want {
				t.Errorf("message = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNearestPicksTheLocationForTheRequest(t *testing.T) {
	// Ported from VS Code: references/model.ts (ReferencesModel.nearest): the location that holds the position, else the first
	// one after it, else the last one before it, else the file with the longest common prefix.
	m := newModel([]extension.Location{
		at("file:///src/a.go", 3, 4, 5), at("file:///src/a.go", 10, 0, 2), at("file:///src/sub/b.go", 1, 1, 1), at("file:///lib/c.go", 0, 0, 1),
	})
	tests := []struct {
		name string
		uri  string
		pos  extension.Position
		want string
	}{
		{"inside a location", "file:///src/a.go", extension.Position{Line: 3, Col: 6}, "r:file:///src/a.go@3:4-3:9"},
		{"at the start of a location", "file:///src/a.go", extension.Position{Line: 10, Col: 0}, "r:file:///src/a.go@10:0-10:2"},
		{"at the end of a location", "file:///src/a.go", extension.Position{Line: 3, Col: 9}, "r:file:///src/a.go@3:4-3:9"},
		{"between two locations: the one after", "file:///src/a.go", extension.Position{Line: 5, Col: 0}, "r:file:///src/a.go@10:0-10:2"},
		{"after every location: the last", "file:///src/a.go", extension.Position{Line: 20, Col: 0}, "r:file:///src/a.go@10:0-10:2"},
		{"another file: the longest prefix", "file:///src/sub/zzz.go", extension.Position{}, "r:file:///src/sub/b.go@1:1-1:2"},
		{"another file: the other folder", "file:///lib/x.go", extension.Position{}, "r:file:///lib/c.go@0:0-0:1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := m.nearest(tc.uri, tc.pos)
			if got == nil || got.key() != tc.want {
				t.Errorf("nearest = %v, want %s", got, tc.want)
			}
		})
	}
	if newModel(nil).nearest("file:///x", extension.Position{}) != nil {
		t.Error("an empty model has a nearest item")
	}
}

func TestNextAndPreviousWalkRoundTheLocations(t *testing.T) {
	// Ported from VS Code: references/model.ts (ReferencesModel.next, previous, _move)
	m := newModel([]extension.Location{at("file:///a", 1, 0, 1), at("file:///a", 2, 0, 1), at("file:///b", 3, 0, 1)})
	a1, a2, b1 := m.files[0].refs[0], m.files[0].refs[1], m.files[1].refs[0]
	tests := []struct {
		name     string
		from     item
		next, up item
	}{
		{"inside a file", a1, a2, b1},
		{"over the end of a file", a2, b1, a1},
		{"the last wraps to the first", b1, a1, a2},
		{"from a file", m.files[0], b1, b1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := m.next(tc.from); got != tc.next {
				t.Errorf("next = %v, want %v", got.key(), tc.next.key())
			}
			if got := m.previous(tc.from); got != tc.up {
				t.Errorf("previous = %v, want %v", got.key(), tc.up.key())
			}
		})
	}
	one := newModel([]extension.Location{at("file:///a", 1, 0, 1)})
	if only := one.files[0].refs[0]; one.next(only) != only || one.previous(only) != only {
		t.Error("the only location is not its own neighbour")
	}
}

func TestRemoveTakesAnItemAndAnEmptyFileOut(t *testing.T) {
	// Ported from VS Code: references/model.ts (ReferencesModel.remove)
	m := newModel([]extension.Location{at("file:///a", 1, 0, 1), at("file:///a", 2, 0, 1), at("file:///b", 3, 0, 1)})
	m.remove(m.files[0].refs[0])
	if got := m.message(); got != "2 results in 2 files" {
		t.Errorf("after one location: %q", got)
	}
	m.remove(m.files[0].refs[0]) // the last one of a.go: the file goes too
	if len(m.files) != 1 || m.files[0].uri != "file:///b" {
		t.Errorf("files = %v", shape(m))
	}
	m.remove(m.files[0])
	if got := m.message(); got != "No results." {
		t.Errorf("after the last file: %q", got)
	}
}

func TestFindAndLocationOfItems(t *testing.T) {
	m := newModel([]extension.Location{at("file:///a", 1, 2, 3), at("file:///a", 5, 0, 1)})
	ref := m.files[0].refs[1]
	if m.find(ref.key()) != ref || m.find(m.files[0].key()) != m.files[0] || m.find("r:nothing") != nil {
		t.Error("find does not find")
	}
	if got := m.location(m.files[0]); got.URI != "file:///a" || got.Range.Start != (extension.Position{Line: 1, Col: 2}) {
		t.Errorf("the location of a file = %+v", got)
	}
	if got := m.location(ref); got != ref.loc {
		t.Errorf("the location of a location = %+v", got)
	}
}
