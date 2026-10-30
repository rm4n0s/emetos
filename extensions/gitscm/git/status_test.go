package git

import (
	"reflect"
	"strings"
	"testing"
)

// The cases are ported from VS Code's git.test.ts (suite "GitStatusParser"). VS Code names the new path of a rename "rename"
// and the old one "path"; here Path is the new path and OrigPath the old one, so the expectations are swapped for the renames.
func TestStatusParserPortedFromVSCode(t *testing.T) {
	q := func(path string) StatusEntry { return StatusEntry{X: '?', Y: '?', Path: path} }
	for _, tc := range []struct {
		name   string
		chunks []string
		want   []StatusEntry
	}{
		{"empty parser", nil, nil},
		{"empty parser 2", []string{""}, nil},
		{"simple", []string{"?? file.txt\x00"}, []StatusEntry{q("file.txt")}},
		{"simple 2", []string{"?? file.txt\x00", "?? file2.txt\x00", "?? file3.txt\x00"}, []StatusEntry{q("file.txt"), q("file2.txt"), q("file3.txt")}},
		{"empty lines", []string{"", "?? file.txt\x00", "", "", "?? file2.txt\x00", "", "?? file3.txt\x00", ""}, []StatusEntry{q("file.txt"), q("file2.txt"), q("file3.txt")}},
		{"combined", []string{"?? file.txt\x00?? file2.txt\x00?? file3.txt\x00"}, []StatusEntry{q("file.txt"), q("file2.txt"), q("file3.txt")}},
		{"split 1", []string{"?? file.txt\x00?? file2", ".txt\x00?? file3.txt\x00"}, []StatusEntry{q("file.txt"), q("file2.txt"), q("file3.txt")}},
		{"split 2", []string{"?? file.txt", "\x00?? file2.txt\x00?? file3.txt\x00"}, []StatusEntry{q("file.txt"), q("file2.txt"), q("file3.txt")}},
		{"split 3", []string{"?? file.txt\x00?? file2.txt\x00?? file3.txt", "\x00"}, []StatusEntry{q("file.txt"), q("file2.txt"), q("file3.txt")}},
		{"rename", []string{"R  newfile.txt\x00file.txt\x00?? file2.txt\x00?? file3.txt\x00"},
			[]StatusEntry{{X: 'R', Y: ' ', Path: "newfile.txt", OrigPath: "file.txt"}, q("file2.txt"), q("file3.txt")}},
		{"rename split", []string{"R  newfile.txt\x00fil", "e.txt\x00?? file2.txt\x00?? file3.txt\x00"},
			[]StatusEntry{{X: 'R', Y: ' ', Path: "newfile.txt", OrigPath: "file.txt"}, q("file2.txt"), q("file3.txt")}},
		{"rename split 3", []string{"?? file2.txt\x00R  new", "file.txt\x00fil", "e.txt\x00?? file3.txt\x00"},
			[]StatusEntry{q("file2.txt"), {X: 'R', Y: ' ', Path: "newfile.txt", OrigPath: "file.txt"}, q("file3.txt")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var p StatusParser
			for _, c := range tc.chunks {
				if n, err := p.Write([]byte(c)); n != len(c) || err != nil {
					t.Fatalf("Write = %d, %v", n, err)
				}
			}
			if got := p.Entries(); !reflect.DeepEqual(got, tc.want) && (len(got) != 0 || len(tc.want) != 0) {
				t.Errorf("entries = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestStatusParserOddNamesAndCodes(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want []StatusEntry
	}{
		{"spaces", " M my file.txt\x00", []StatusEntry{{X: ' ', Y: 'M', Path: "my file.txt"}}},
		{"quotes and backslashes", "A  \"quoted\" and \\slash.txt\x00", []StatusEntry{{X: 'A', Y: ' ', Path: `"quoted" and \slash.txt`}}},
		{"a newline in the name", "?? two\nlines.txt\x00", []StatusEntry{{X: '?', Y: '?', Path: "two\nlines.txt"}}},
		{"bytes that are not UTF-8", "?? bad\xff\xfename.txt\x00", []StatusEntry{{X: '?', Y: '?', Path: "bad\xff\xfename.txt"}}},
		{"unicode", "?? héllo wörld ☃.txt\x00", []StatusEntry{{X: '?', Y: '?', Path: "héllo wörld ☃.txt"}}},
		{"copy", "C  copy.txt\x00orig.txt\x00", []StatusEntry{{X: 'C', Y: ' ', Path: "copy.txt", OrigPath: "orig.txt"}}},
		{"rename in the work tree", " R new.txt\x00old.txt\x00", []StatusEntry{{X: ' ', Y: 'R', Path: "new.txt", OrigPath: "old.txt"}}},
		{"a nested repository is skipped", "?? nested/\x00?? file.txt\x00", []StatusEntry{{X: '?', Y: '?', Path: "file.txt"}}},
		{"ignored", "!! build/out.o\x00", []StatusEntry{{X: '!', Y: '!', Path: "build/out.o"}}},
		{"empty output", "", nil},
		{"a record cut before its NUL", "?? file.txt", nil},
		{"a rename cut before its second path", "R  new.txt\x00old", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseStatus([]byte(tc.in))
			if !reflect.DeepEqual(got, tc.want) && (len(got) != 0 || len(tc.want) != 0) {
				t.Errorf("ParseStatus = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// A stream cut at every byte gives a prefix of the entries of the whole, never a damaged entry.
func TestStatusParserCutAtEveryByte(t *testing.T) {
	whole := "?? a.txt\x00 M b c.txt\x00R  new d.txt\x00old d.txt\x00UU conflict.txt\x00C  c2.txt\x00c1.txt\x00A  é.txt\x00"
	want := ParseStatus([]byte(whole))
	if len(want) != 6 {
		t.Fatalf("whole = %+v", want)
	}
	for cut := 0; cut <= len(whole); cut++ {
		got := ParseStatus([]byte(whole[:cut]))
		if len(got) > len(want) || (len(got) > 0 && !reflect.DeepEqual(got, want[:len(got)])) {
			t.Fatalf("cut at %d: %+v is not a prefix of %+v", cut, got, want)
		}
		// Fed in two chunks cut at the same byte, the result is the whole.
		var p StatusParser
		_, _ = p.Write([]byte(whole[:cut]))
		_, _ = p.Write([]byte(whole[cut:]))
		if !reflect.DeepEqual(p.Entries(), want) {
			t.Fatalf("two chunks cut at %d: %+v, want %+v", cut, p.Entries(), want)
		}
	}
	// One byte at a time.
	var p StatusParser
	for i := 0; i < len(whole); i++ {
		_, _ = p.Write([]byte{whole[i]})
	}
	if !reflect.DeepEqual(p.Entries(), want) {
		t.Errorf("byte by byte: %+v", p.Entries())
	}
}

// Every XY code of VS Code's Status enum maps to the group and status VS Code gives it (Repository.updateModelState).
func TestStatusEntryStatuses(t *testing.T) {
	for _, tc := range []struct {
		xy                 string
		merge, index, work Status
	}{
		{"??", StatusNone, StatusNone, StatusUntracked},
		{"!!", StatusNone, StatusNone, StatusIgnored},
		{"DD", StatusBothDeleted, StatusNone, StatusNone},
		{"AU", StatusAddedByUs, StatusNone, StatusNone},
		{"UD", StatusDeletedByThem, StatusNone, StatusNone},
		{"UA", StatusAddedByThem, StatusNone, StatusNone},
		{"DU", StatusDeletedByUs, StatusNone, StatusNone},
		{"AA", StatusBothAdded, StatusNone, StatusNone},
		{"UU", StatusBothModified, StatusNone, StatusNone},
		{"M ", StatusNone, StatusIndexModified, StatusNone},
		{"A ", StatusNone, StatusIndexAdded, StatusNone},
		{"D ", StatusNone, StatusIndexDeleted, StatusNone},
		{"R ", StatusNone, StatusIndexRenamed, StatusNone},
		{"C ", StatusNone, StatusIndexCopied, StatusNone},
		{" M", StatusNone, StatusNone, StatusModified},
		{" D", StatusNone, StatusNone, StatusDeleted},
		{" A", StatusNone, StatusNone, StatusIntentToAdd},
		{" R", StatusNone, StatusNone, StatusIntentToRename},
		{" T", StatusNone, StatusNone, StatusTypeChanged},
		{"MM", StatusNone, StatusIndexModified, StatusModified},
		{"AM", StatusNone, StatusIndexAdded, StatusModified},
		{"AD", StatusNone, StatusIndexAdded, StatusDeleted},
		{"RM", StatusNone, StatusIndexRenamed, StatusModified},
		{"  ", StatusNone, StatusNone, StatusNone},
	} {
		e := StatusEntry{X: tc.xy[0], Y: tc.xy[1]}
		merge, index, work := e.Statuses()
		if merge != tc.merge || index != tc.index || work != tc.work {
			t.Errorf("%q: merge %d index %d work %d, want %d %d %d", tc.xy, merge, index, work, tc.merge, tc.index, tc.work)
		}
	}
	// The values are VS Code's enum, in its order.
	order := []Status{StatusIndexModified, StatusIndexAdded, StatusIndexDeleted, StatusIndexRenamed, StatusIndexCopied, StatusModified,
		StatusDeleted, StatusUntracked, StatusIgnored, StatusIntentToAdd, StatusIntentToRename, StatusTypeChanged, StatusAddedByUs,
		StatusAddedByThem, StatusDeletedByUs, StatusDeletedByThem, StatusBothAdded, StatusBothDeleted, StatusBothModified}
	for i, s := range order {
		if int(s) != i {
			t.Errorf("status %d has the value %d", i, s)
		}
	}
	if StatusNone >= 0 {
		t.Errorf("StatusNone = %d", StatusNone)
	}
}

func TestStatusParserLargeStream(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 20000; i++ {
		b.WriteString(" M dir/file")
		b.WriteString(strings.Repeat("x", i%7))
		b.WriteString(".go\x00")
	}
	if n := len(ParseStatus([]byte(b.String()))); n != 20000 {
		t.Errorf("entries = %d", n)
	}
}
