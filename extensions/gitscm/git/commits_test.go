package git

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func unix(sec int64) time.Time { return time.Unix(sec, 0).UTC() }

// The cases of suite "parseGitCommit" of VS Code's git.test.ts, with the same recorded outputs.
func TestParseCommitsPortedFromVSCode(t *testing.T) {
	const (
		h1, h2  = "52c293a05038d865604c2284aa8698bd087915a1", "52c293a05038d865604c2284aa8698bd087915a2"
		p1, p2  = "8e5a374372b8393906c7e380dbb09349c5385554", "8e5a374372b8393906c7e380dbb09349c5385555"
		p3      = "df27d8c75b129ab9b178b386077da2822101b217"
		john    = "John Doe\njohn.doe@mail.com\n1580811030\n1580811031\n"
		janeRec = "Jane Doe\njane.doe@mail.com\n1580811032\n1580811033\n"
	)
	john1 := Commit{Hash: h1, Message: "This is a commit message.", Parents: []string{p1}, AuthorDate: unix(1580811030), AuthorName: "John Doe",
		AuthorEmail: "john.doe@mail.com", CommitDate: unix(1580811031), RefNames: []string{"main", "branch"}}
	jane := Commit{Hash: h2, Message: "This is another commit message.", Parents: []string{p2}, AuthorDate: unix(1580811032), AuthorName: "Jane Doe",
		AuthorEmail: "jane.doe@mail.com", CommitDate: unix(1580811033), RefNames: []string{"main", "branch"}}
	withStat := func(c Commit, files, ins, del int) Commit { c.ShortStat = &ShortStat{files, ins, del}; return c }
	for _, tc := range []struct {
		name string
		in   string
		want []Commit
	}{
		{"single parent commit", h1 + "\n" + john + p1 + "\nmain,branch\nThis is a commit message.\x00", []Commit{john1}},
		{"multiple parent commits", h1 + "\n" + john + p1 + " " + p3 + "\nmain\nThis is a commit message.\x00",
			[]Commit{func() Commit { c := john1; c.Parents = []string{p1, p3}; c.RefNames = []string{"main"}; return c }()}},
		{"no parent commits", h1 + "\n" + john + "\nmain\nThis is a commit message.\x00",
			[]Commit{func() Commit { c := john1; c.Parents = nil; c.RefNames = []string{"main"}; return c }()}},
		{"commit with shortstat", h1 + "\n" + john + p1 + "\nmain,branch\nThis is a commit message.\x00\n 1 file changed, 2 insertions(+), 3 deletion(-)",
			[]Commit{withStat(john1, 1, 2, 3)}},
		{"commit with shortstat (no insertions)", h1 + "\n" + john + p1 + "\nmain,branch\nThis is a commit message.\x00\n 1 file changed, 3 deletion(-)",
			[]Commit{withStat(john1, 1, 0, 3)}},
		{"commit with shortstat (no deletions)", h1 + "\n" + john + p1 + "\nmain,branch\nThis is a commit message.\x00\n 1 file changed, 2 insertions(+)",
			[]Commit{withStat(john1, 1, 2, 0)}},
		{"commit list", h1 + "\n" + john + p1 + "\nmain,branch\nThis is a commit message.\x00\n" + h2 + "\n" + janeRec + p2 + "\nmain,branch\nThis is another commit message.\x00",
			[]Commit{john1, jane}},
		{"commit list with shortstat", h1 + "\n" + john + p1 + "\nmain,branch\nThis is a commit message.\x00\n 5 file changed, 12 insertions(+), 13 deletion(-)\n" +
			h2 + "\n" + janeRec + p2 + "\nmain,branch\nThis is another commit message.\x00\n 6 file changed, 22 insertions(+), 23 deletion(-)",
			[]Commit{withStat(john1, 5, 12, 13), withStat(jane, 6, 22, 23)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseCommits([]byte(tc.in))
			// VS Code's expectations have no co-authors; both are empty.
			for i := range got {
				if len(got[i].CoAuthors) != 0 {
					t.Errorf("commit %d has co-authors %v", i, got[i].CoAuthors)
				}
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got  %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

func TestParseCommitsOfARealLog(t *testing.T) {
	// The output of git log -z --format=CommitFormat --shortstat for a root commit with a body and a commit that decorates
	// HEAD, as git prints it.
	out := "08eb97d2554227" + "1da0648e270a7248842" + "26c9f94\nA\na@b\n1793393220\n1793393220\na0e7e592e12c62bff2d528ab96862002c2890bf7\nHEAD -> main, tag: v1\ntwo\n\x00\n" +
		" 1 file changed, 1 insertion(+)\n" +
		"a0e7e592e12c62bff2d528ab966862002c2890bf\nA\na@b\n1793393220\n1793393220\n\n\none\n\nbody\n\x00\n 1 file changed, 1 insertion(+)\n"
	got := ParseCommits([]byte(out))
	if len(got) != 2 {
		t.Fatalf("commits = %d: %+v", len(got), got)
	}
	if got[0].Message != "two" || !reflect.DeepEqual(got[0].RefNames, []string{"HEAD -> main", "tag: v1"}) || got[0].ShortStat == nil || got[0].ShortStat.Insertions != 1 {
		t.Errorf("first = %+v", got[0])
	}
	if got[1].Message != "one\n\nbody" || got[1].Parents != nil || got[1].RefNames != nil || got[1].ShortStat == nil {
		t.Errorf("second = %+v", got[1])
	}
}

func TestParseCommitsWithOddMessages(t *testing.T) {
	rec := func(hash, msg string) string {
		return hash + "\nAda Example\nada@example.com\n1\n2\n\n\n" + msg + "\n\x00"
	}
	h := func(c string) string { return strings.Repeat(c, 40) }
	for _, tc := range []struct {
		name string
		in   string
		want []string
	}{
		{"unicode and quotes", rec(h("a"), "Add \"héllo\" ☃ 'x'"), []string{"Add \"héllo\" ☃ 'x'"}},
		{"a message with blank lines and a stat-like line", rec(h("b"), "Subject\n\n 3 files changed, 1 insertion(+)\n\nmore"), []string{"Subject\n\n 3 files changed, 1 insertion(+)\n\nmore"}},
		{"an empty message", rec(h("c"), ""), []string{""}},
		{"bytes that are not UTF-8", rec(h("d"), "bad \xff\xfe bytes"), []string{"bad \xff\xfe bytes"}},
		{"crlf", rec(h("e"), "line one\r\nline two"), []string{"line one\r\nline two"}},
		{"two commits", rec(h("a"), "one") + rec(h("b"), "two"), []string{"one", "two"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, c := range ParseCommits([]byte(tc.in)) {
				got = append(got, c.Message)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("messages = %q, want %q", got, tc.want)
			}
		})
	}
	t.Run("a record without a valid hash is skipped", func(t *testing.T) {
		in := "not a hash\nAda\na@b\n1\n2\n\n\nmsg\n\x00" + rec(strings.Repeat("a", 40), "kept")
		got := ParseCommits([]byte(in))
		if len(got) != 1 || got[0].Message != "kept" {
			t.Errorf("got %+v", got)
		}
	})
	t.Run("a sha-256 hash", func(t *testing.T) {
		got := ParseCommits([]byte(rec(strings.Repeat("f", 64), "x")))
		if len(got) != 1 || len(got[0].Hash) != 64 {
			t.Errorf("got %+v", got)
		}
	})
}

// A log cut at every byte gives a prefix of the commits of the whole; the last may lose its shortstat but nothing else.
func TestParseCommitsCutAtEveryByte(t *testing.T) {
	h := func(c string) string { return strings.Repeat(c, 40) }
	rec := func(hash, name, msg, stat string) string {
		return hash + "\n" + name + "\nada@example.com\n1580811030\n1580811031\n" + h("0") + "\nHEAD -> main\n" + msg + "\n\x00\n" + stat
	}
	whole := rec(h("a"), "Ada", "First\n\nbody ☃", " 2 files changed, 3 insertions(+), 4 deletions(-)\n") +
		rec(h("b"), "Bo", "Second", "") +
		rec(h("c"), "Cy", "Third", " 1 file changed, 1 insertion(+)\n")
	want := ParseCommits([]byte(whole))
	if len(want) != 3 || want[0].ShortStat == nil || want[1].ShortStat != nil || want[2].ShortStat == nil {
		t.Fatalf("whole = %+v", want)
	}
	for cut := 0; cut <= len(whole); cut++ {
		got := ParseCommits([]byte(whole[:cut]))
		if len(got) > len(want) {
			t.Fatalf("cut at %d: %d commits", cut, len(got))
		}
		for i, c := range got {
			w := want[i]
			if i == len(got)-1 {
				// The stat of the last may be missing, or end after the insertions (the line "2 insertions(+)" is complete
				// when the stream ends there, as VS Code's recordings are).
				w.ShortStat = c.ShortStat
				if s, ws := c.ShortStat, want[i].ShortStat; s != nil && (ws == nil || s.Files != ws.Files || s.Insertions > ws.Insertions || s.Deletions > ws.Deletions) {
					t.Fatalf("cut at %d: stat %+v, want %+v", cut, s, ws)
				}
			}
			if !reflect.DeepEqual(c, w) {
				t.Fatalf("cut at %d, commit %d:\n got %+v\nwant %+v", cut, i, c, w)
			}
		}
	}
	// In two chunks cut at every byte, the result is the whole.
	for cut := 0; cut <= len(whole); cut++ {
		var got []Commit
		p := NewCommitParser(func(c Commit) error { got = append(got, c); return nil })
		_, _ = p.Write([]byte(whole[:cut]))
		_, _ = p.Write([]byte(whole[cut:]))
		_ = p.Close()
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("two chunks cut at %d: %d commits", cut, len(got))
		}
	}
}

func TestStreamCommitsStopsOnTheCallbackError(t *testing.T) {
	h := strings.Repeat("a", 40)
	one := h + "\nAda\na@b\n1\n2\n\n\nmsg\n\x00"
	stop := &stopError{}
	var n int
	err := StreamCommits(strings.NewReader(one+one+one), func(Commit) error {
		n++
		if n == 2 {
			return stop
		}
		return nil
	})
	if err != stop || n != 2 {
		t.Errorf("err = %v after %d commits", err, n)
	}
}

type stopError struct{}

func (*stopError) Error() string { return "stop" }

// The cases of suite "parseGitFileLog".
func TestParseFileLogPortedFromVSCode(t *testing.T) {
	header := func(hash string, date int, parent, refNames, message string) string {
		return hash + "\nJohn Doe\njohn@doe.com\n" + itoa(date) + "\n" + itoa(date) + "\n" + parent + "\n" + refNames + "\n" + message + "\n\x00\n"
	}
	commits := []string{
		header("3d54fbf697580e1c5984306761039cf7e39d5e10", 1767398400, "d0eecfaa720105f947e2d1aa2754bb2142e391bc", "HEAD -> main", "Make binary"),
		header("d0eecfaa720105f947e2d1aa2754bb2142e391bc", 1767312000, "428a48b6308ea84d416f21edd625c1729df96e3b", "", "Rename file"),
		header("428a48b6308ea84d416f21edd625c1729df96e3b", 1767225600, "", "", "Add file"),
	}
	shortStats := []string{" 1 file changed, 0 insertions(+), 0 deletions(-)\n", " 1 file changed, 0 insertions(+), 0 deletions(-)\n", " 1 file changed, 1 insertion(+)\n"}

	t.Run("resolves the path of a followed file at each commit", func(t *testing.T) {
		numStats := []string{"-\t-\tnew.txt\x00", "0\t0\t\x00old name.txt\x00new.txt\x00", "1\t0\told name.txt\x00"}
		var with, without strings.Builder
		for i := range commits {
			with.WriteString(commits[i] + numStats[i] + shortStats[i])
			without.WriteString(commits[i] + shortStats[i])
		}
		got := ParseFileLog([]byte(with.String()))
		want := ParseCommits([]byte(without.String()))
		for i, p := range []string{"new.txt", "new.txt", "old name.txt"} {
			want[i].FilePath = p
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got  %+v\nwant %+v", got, want)
		}
	})

	t.Run("leaves the path unset for commits without changes to the file", func(t *testing.T) {
		out := commits[0] + shortStats[0] + commits[1] + "0\t0\tnew.txt\x00" + shortStats[1]
		var paths []string
		for _, c := range ParseFileLog([]byte(out)) {
			paths = append(paths, c.FilePath)
		}
		if !reflect.DeepEqual(paths, []string{"", "new.txt"}) {
			t.Errorf("paths = %q", paths)
		}
	})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	return string(d)
}

// The cases of suite "parseCoAuthors".
func TestParseCoAuthorsPortedFromVSCode(t *testing.T) {
	for _, tc := range []struct {
		name, msg string
		want      []CoAuthor
	}{
		{"no co-authors", "This is a commit message.", nil},
		{"single co-author", "Fix bug\n\nCo-authored-by: Jane Doe <jane@example.com>", []CoAuthor{{"Jane Doe", "jane@example.com"}}},
		{"multiple co-authors", "Fix bug\n\nCo-authored-by: Jane Doe <jane@example.com>\nCo-authored-by: Bob Smith <bob@example.com>",
			[]CoAuthor{{"Jane Doe", "jane@example.com"}, {"Bob Smith", "bob@example.com"}}},
		{"case insensitive", "Fix bug\n\nco-authored-by: Jane Doe <jane@example.com>", []CoAuthor{{"Jane Doe", "jane@example.com"}}},
		{"AI co-author (Copilot)", "Fix bug\n\nCo-authored-by: Copilot <copilot@github.com>", []CoAuthor{{"Copilot", "copilot@github.com"}}},
		{"mixed with other trailers", "Fix bug\n\nSigned-off-by: Admin <admin@corp.com>\nCo-authored-by: Jane Doe <jane@example.com>", []CoAuthor{{"Jane Doe", "jane@example.com"}}},
		{"duplicate co-authors (squash merge)", strings.Join([]string{
			"Fix bug (#123)", "", "* First change", "", "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>", "",
			"* Second change", "", "Co-authored-by: Jane Doe <jane@example.com>", "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>", "",
			"---------", "", "Co-authored-by: Copilot <223556219+copilot@users.noreply.github.com>", "Co-authored-by: Jane <JANE@example.com>"}, "\n"),
			[]CoAuthor{{"Copilot", "223556219+Copilot@users.noreply.github.com"}, {"Jane Doe", "jane@example.com"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseCoAuthors(tc.msg); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
