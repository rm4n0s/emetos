package git

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// The cases of suite "parseGitmodules" of VS Code's git.test.ts.
func TestParseGitmodulesPortedFromVSCode(t *testing.T) {
	spdlog := func(name, path, url string) Submodule { return Submodule{Name: name, Path: path, URL: url} }
	for _, tc := range []struct {
		name, in string
		want     []Submodule
	}{
		{"empty", "", nil},
		{"sample", "[submodule \"deps/spdlog\"]\n\tpath = deps/spdlog\n\turl = https://github.com/gabime/spdlog.git\n",
			[]Submodule{spdlog("deps/spdlog", "deps/spdlog", "https://github.com/gabime/spdlog.git")}},
		{"big", "[submodule \"deps/spdlog\"]\n\tpath = deps/spdlog\n\turl = https://github.com/gabime/spdlog.git\n" +
			"[submodule \"deps/spdlog2\"]\n\tpath = deps/spdlog2\n\turl = https://github.com/gabime/spdlog.git\n" +
			"[submodule \"deps/spdlog3\"]\n\tpath = deps/spdlog3\n\turl = https://github.com/gabime/spdlog.git\n" +
			"[submodule \"deps/spdlog4\"]\n\tpath = deps/spdlog4\n\turl = https://github.com/gabime/spdlog4.git\n",
			[]Submodule{
				spdlog("deps/spdlog", "deps/spdlog", "https://github.com/gabime/spdlog.git"),
				spdlog("deps/spdlog2", "deps/spdlog2", "https://github.com/gabime/spdlog.git"),
				spdlog("deps/spdlog3", "deps/spdlog3", "https://github.com/gabime/spdlog.git"),
				spdlog("deps/spdlog4", "deps/spdlog4", "https://github.com/gabime/spdlog4.git"),
			}},
		{"whitespace #74844", "[submodule \"deps/spdlog\"]\n\tpath = deps/spdlog\n\turl  = https://github.com/gabime/spdlog.git\n",
			[]Submodule{spdlog("deps/spdlog", "deps/spdlog", "https://github.com/gabime/spdlog.git")}},
		{"whitespace again #108371", "[submodule \"deps/spdlog\"]\n\tpath= deps/spdlog\n\turl=https://github.com/gabime/spdlog.git\n",
			[]Submodule{spdlog("deps/spdlog", "deps/spdlog", "https://github.com/gabime/spdlog.git")}},
		// Emetos's own cases.
		{"a section without a url is skipped", "[submodule \"a\"]\n\tpath = a\n[submodule \"b\"]\n\tpath = b\n\turl = ../b\n", []Submodule{spdlog("b", "b", "../b")}},
		{"windows line endings", "[submodule \"a\"]\r\n\tpath = a\r\n\turl = ../a\r\n", []Submodule{spdlog("a", "a", "../a")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseGitmodules(tc.in); !reflect.DeepEqual(got, tc.want) && (len(got) != 0 || len(tc.want) != 0) {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

// The cases of suite "parseGitRemotes".
func TestParseRemotesConfigPortedFromVSCode(t *testing.T) {
	const vscode = "https://github.com/microsoft/vscode.git"
	for _, tc := range []struct {
		name, in string
		want     []Remote
	}{
		{"empty", "", nil},
		{"single remote", "[remote \"origin\"]\n\turl = " + vscode + "\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n",
			[]Remote{{Name: "origin", FetchURL: vscode, PushURL: vscode}}},
		{"single remote (multiple urls)", "[remote \"origin\"]\n\turl = " + vscode + "\n\turl = https://github.com/microsoft/vscode2.git\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n",
			[]Remote{{Name: "origin", FetchURL: vscode, PushURL: vscode}}},
		{"multiple remotes", "[remote \"origin\"]\n\turl = " + vscode + "\n\tpushurl = https://github.com/microsoft/vscode1.git\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n" +
			"[remote \"remote2\"]\n\turl = https://github.com/microsoft/vscode2.git\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n",
			[]Remote{
				{Name: "origin", FetchURL: vscode, PushURL: "https://github.com/microsoft/vscode1.git"},
				{Name: "remote2", FetchURL: "https://github.com/microsoft/vscode2.git", PushURL: "https://github.com/microsoft/vscode2.git"},
			}},
		{"remotes (white space)", " [remote \"origin\"]\n\turl  =  " + vscode + "\n\tpushurl=https://github.com/microsoft/vscode1.git\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n" +
			"[ remote\"remote2\"]\n\turl = https://github.com/microsoft/vscode2.git\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n",
			[]Remote{
				{Name: "origin", FetchURL: vscode, PushURL: "https://github.com/microsoft/vscode1.git"},
				{Name: "remote2", FetchURL: "https://github.com/microsoft/vscode2.git", PushURL: "https://github.com/microsoft/vscode2.git"},
			}},
		{"remotes (invalid section)", "[remote \"origin\"\n\turl = " + vscode + "\n\tpushurl = https://github.com/microsoft/vscode1.git\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n", nil},
		// Emetos's own cases.
		{"no_push makes the remote read-only", "[remote \"up\"]\n\turl = ../up\n\tpushurl = no_push\n",
			[]Remote{{Name: "up", FetchURL: "../up", PushURL: "no_push", ReadOnly: true}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseRemotesConfig(tc.in); !reflect.DeepEqual(got, tc.want) && (len(got) != 0 || len(tc.want) != 0) {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestParseRemoteVerbose(t *testing.T) {
	for _, tc := range []struct {
		name, in string
		want     []Remote
	}{
		{"none", "", nil},
		{"one", "origin\t/srv/repo.git (fetch)\norigin\t/srv/repo.git (push)\n", []Remote{{Name: "origin", FetchURL: "/srv/repo.git", PushURL: "/srv/repo.git"}}},
		{"two, with a push url and a space in a path", "origin\t/a b/repo.git (fetch)\norigin\t/other (push)\nfork\thttps://example.com/x.git (fetch)\nfork\thttps://example.com/x.git (push)\n",
			[]Remote{{Name: "origin", FetchURL: "/a b/repo.git", PushURL: "/other"}, {Name: "fork", FetchURL: "https://example.com/x.git", PushURL: "https://example.com/x.git"}}},
		{"no_push", "ro\t../ro (fetch)\nro\tno_push (push)\n", []Remote{{Name: "ro", FetchURL: "../ro", PushURL: "no_push", ReadOnly: true}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseRemoteVerbose(tc.in); !reflect.DeepEqual(got, tc.want) && (len(got) != 0 || len(tc.want) != 0) {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

// Suite "parseLsTree": VS Code reads the output of ls-tree -l line by line; Emetos reads ls-tree -l -z, whose records end with a
// NUL, so the same lines are joined with NULs here.
func TestParseLsTreePortedFromVSCode(t *testing.T) {
	lines := []string{
		"040000 tree 0274a81f8ee9ca3669295dc40f510bd2021d0043       -\t.vscode",
		"100644 blob 1d487c1817262e4f20efbfa1d04c18f51b0046f6  491570\tScreen Shot 2018-06-01 at 14.48.05.png",
		"100644 blob 686c16e4f019b734655a2576ce8b98749a9ffdb9  764420\tScreen Shot 2018-06-07 at 20.04.59.png",
		"100644 blob 257cc5642cb1a054f08cc83f2d943e56fd3ebe99       4\tboom.txt",
		"100644 blob 86dc360dd25f13fa50ffdc8259e9653921f4f2b7      11\tboomcaboom.txt",
		"100644 blob a68b14060589b16d7ac75f67b905c918c03c06eb      24\tfile.js",
		"100644 blob f7bcfb05af46850d780f88c069edcd57481d822d     201\tfile.md",
		"100644 blob ab8b86114a051f6490f1ec5e3141b9a632fb46b5       8\thello.js",
		"100644 blob 257cc5642cb1a054f08cc83f2d943e56fd3ebe99       4\twhat.js",
		"100644 blob be859e3f412fa86513cd8bebe8189d1ea1a3e46d      24\twhat.txt",
		"100644 blob 56ec42c9dc6fcf4534788f0fe34b36e09f37d085  261186\twhat.txt2",
	}
	want := []LsTreeEntry{
		{"040000", "tree", "0274a81f8ee9ca3669295dc40f510bd2021d0043", "-", ".vscode"},
		{"100644", "blob", "1d487c1817262e4f20efbfa1d04c18f51b0046f6", "491570", "Screen Shot 2018-06-01 at 14.48.05.png"},
		{"100644", "blob", "686c16e4f019b734655a2576ce8b98749a9ffdb9", "764420", "Screen Shot 2018-06-07 at 20.04.59.png"},
		{"100644", "blob", "257cc5642cb1a054f08cc83f2d943e56fd3ebe99", "4", "boom.txt"},
		{"100644", "blob", "86dc360dd25f13fa50ffdc8259e9653921f4f2b7", "11", "boomcaboom.txt"},
		{"100644", "blob", "a68b14060589b16d7ac75f67b905c918c03c06eb", "24", "file.js"},
		{"100644", "blob", "f7bcfb05af46850d780f88c069edcd57481d822d", "201", "file.md"},
		{"100644", "blob", "ab8b86114a051f6490f1ec5e3141b9a632fb46b5", "8", "hello.js"},
		{"100644", "blob", "257cc5642cb1a054f08cc83f2d943e56fd3ebe99", "4", "what.js"},
		{"100644", "blob", "be859e3f412fa86513cd8bebe8189d1ea1a3e46d", "24", "what.txt"},
		{"100644", "blob", "56ec42c9dc6fcf4534788f0fe34b36e09f37d085", "261186", "what.txt2"},
	}
	whole := strings.Join(lines, "\x00") + "\x00"
	if got := ParseLsTree([]byte(whole)); !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v", got)
	}
	// Emetos's own cases: a name with a newline, a tab and bytes that are not UTF-8, and a record cut short.
	odd := "100644 blob 257cc5642cb1a054f08cc83f2d943e56fd3ebe99       4\ttwo\nlines\tand tab\xff.txt\x00100644 blob 257cc5642cb1a054f08cc83f2d943e56fd3ebe99"
	got := ParseLsTree([]byte(odd))
	if len(got) != 1 || got[0].Path != "two\nlines\tand tab\xff.txt" {
		t.Errorf("odd = %+v", got)
	}
	for cut := 0; cut <= len(whole); cut++ {
		got := ParseLsTree([]byte(whole[:cut]))
		if len(got) > len(want) || (len(got) > 0 && !reflect.DeepEqual(got, want[:len(got)])) {
			t.Fatalf("cut at %d: %+v", cut, got)
		}
	}
}

// Suite "parseLsFiles", read as ls-files --stage -z.
func TestParseLsFilesPortedFromVSCode(t *testing.T) {
	lines := []string{
		"100644 7a73a41bfdf76d6f793007240d80983a52f15f97 0\t.vscode/settings.json",
		"100644 1d487c1817262e4f20efbfa1d04c18f51b0046f6 0\tScreen Shot 2018-06-01 at 14.48.05.png",
		"100644 686c16e4f019b734655a2576ce8b98749a9ffdb9 0\tScreen Shot 2018-06-07 at 20.04.59.png",
		"100644 257cc5642cb1a054f08cc83f2d943e56fd3ebe99 0\tboom.txt",
		"100644 86dc360dd25f13fa50ffdc8259e9653921f4f2b7 0\tboomcaboom.txt",
		"100644 a68b14060589b16d7ac75f67b905c918c03c06eb 0\tfile.js",
		"100644 f7bcfb05af46850d780f88c069edcd57481d822d 0\tfile.md",
		"100644 ab8b86114a051f6490f1ec5e3141b9a632fb46b5 0\thello.js",
		"100644 257cc5642cb1a054f08cc83f2d943e56fd3ebe99 0\twhat.js",
		"100644 be859e3f412fa86513cd8bebe8189d1ea1a3e46d 0\twhat.txt",
		"100644 56ec42c9dc6fcf4534788f0fe34b36e09f37d085 0\twhat.txt2",
	}
	whole := strings.Join(lines, "\x00") + "\x00"
	got := ParseLsFiles([]byte(whole))
	if len(got) != 11 || got[0] != (LsFilesEntry{"100644", "7a73a41bfdf76d6f793007240d80983a52f15f97", "0", ".vscode/settings.json"}) ||
		got[10] != (LsFilesEntry{"100644", "56ec42c9dc6fcf4534788f0fe34b36e09f37d085", "0", "what.txt2"}) || got[1].Path != "Screen Shot 2018-06-01 at 14.48.05.png" {
		t.Errorf("got %+v", got)
	}
	for _, tc := range []struct {
		name, in string
		want     []LsFilesEntry
	}{
		{"the stages of a conflict", "100644 aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa 1\tf\x00100644 bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb 2\tf\x00",
			[]LsFilesEntry{{"100644", strings.Repeat("a", 40), "1", "f"}, {"100644", strings.Repeat("b", 40), "2", "f"}}},
		{"a name with a newline", "100755 cccccccccccccccccccccccccccccccccccccccc 0\ta\nb\x00", []LsFilesEntry{{"100755", strings.Repeat("c", 40), "0", "a\nb"}}},
		{"a record cut short", "100644 cccccccccccccccccccccccccccccccccccccccc 0\tabc", nil},
		{"empty", "", nil},
	} {
		if got := ParseLsFiles([]byte(tc.in)); !reflect.DeepEqual(got, tc.want) && (len(got) != 0 || len(tc.want) != 0) {
			t.Errorf("%s: got %+v", tc.name, got)
		}
	}
	for cut := 0; cut <= len(whole); cut++ {
		got := ParseLsFiles([]byte(whole[:cut]))
		for i, e := range got {
			if e.Path != strings.SplitN(lines[i], "\t", 2)[1] {
				t.Fatalf("cut at %d: entry %d is %+v", cut, i, e)
			}
		}
	}
}

// Suite "splitInChunks".
func TestSplitInChunksPortedFromVSCode(t *testing.T) {
	words := []string{"hello", "there", "cool", "stuff"}
	digits := []string{"0", "01", "012", "0", "01", "012", "0", "01", "012"}
	for _, tc := range []struct {
		in   []string
		max  int
		want [][]string
	}{
		{words, 6, [][]string{{"hello"}, {"there"}, {"cool"}, {"stuff"}}},
		{words, 10, [][]string{{"hello", "there"}, {"cool", "stuff"}}},
		{words, 12, [][]string{{"hello", "there"}, {"cool", "stuff"}}},
		{words, 14, [][]string{{"hello", "there", "cool"}, {"stuff"}}},
		{words, 2000, [][]string{{"hello", "there", "cool", "stuff"}}},
		{digits, 1, [][]string{{"0"}, {"01"}, {"012"}, {"0"}, {"01"}, {"012"}, {"0"}, {"01"}, {"012"}}},
		{digits, 2, [][]string{{"0"}, {"01"}, {"012"}, {"0"}, {"01"}, {"012"}, {"0"}, {"01"}, {"012"}}},
		{digits, 3, [][]string{{"0", "01"}, {"012"}, {"0", "01"}, {"012"}, {"0", "01"}, {"012"}}},
		{digits, 4, [][]string{{"0", "01"}, {"012", "0"}, {"01"}, {"012", "0"}, {"01"}, {"012"}}},
		{digits, 5, [][]string{{"0", "01"}, {"012", "0"}, {"01", "012"}, {"0", "01"}, {"012"}}},
		{digits, 6, [][]string{{"0", "01", "012"}, {"0", "01", "012"}, {"0", "01", "012"}}},
		{digits, 7, [][]string{{"0", "01", "012", "0"}, {"01", "012", "0"}, {"01", "012"}}},
		{digits, 8, [][]string{{"0", "01", "012", "0"}, {"01", "012", "0", "01"}, {"012"}}},
		{digits, 9, [][]string{{"0", "01", "012", "0", "01"}, {"012", "0", "01", "012"}}},
		{nil, 5, nil},
	} {
		if got := SplitInChunks(tc.in, tc.max); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("SplitInChunks(%q, %d) = %q, want %q", tc.in, tc.max, got, tc.want)
		}
	}
}

func TestParseRefs(t *testing.T) {
	h1, h2, h3 := strings.Repeat("1", 40), strings.Repeat("2", 40), strings.Repeat("3", 40)
	line := func(f ...string) string { return strings.Join(f, "\x00") }
	out := strings.Join([]string{
		// refname, object, peeled, upstream, track, remotename, remoteref, date, peeled date, HEAD
		line("refs/heads/main", h1, "", "refs/remotes/origin/main", "[ahead 1, behind 2]", "origin", "refs/heads/main", "1704110400", "", "*"),
		line("refs/heads/topic", h2, "", "refs/remotes/origin/topic", "[gone]", "origin", "refs/heads/topic", "1704110460", "", " "),
		line("refs/heads/local", h2, "", "refs/heads/main", "[behind 3]", ".", "refs/heads/main", "1704110460", "", " "),
		line("refs/heads/solo", h2, "", "", "", "", "", "1704110460", "", " "),
		line("refs/heads/in-sync", h1, "", "refs/remotes/origin/main", "", "origin", "refs/heads/main", "1704110400", "", " "),
		line("refs/remotes/origin/main", h1, "", "", "", "", "", "1704110400", "", " "),
		line("refs/remotes/origin/HEAD", h1, "", "", "", "", "", "1704110400", "", " "),
		line("refs/remotes/up/feature/x", h3, "", "", "", "", "", "1704110500", "", " "),
		line("refs/tags/light", h1, "", "", "", "", "", "1704110400", "", " "),
		line("refs/tags/v1.0", h3, h1, "", "", "", "", "", "1704110400", " "),
		line("refs/stash", h2, "", "", "", "", "", "1704110460", "", " "),
		"refs/heads/cut-short\x00" + h1,
		"",
	}, "\n")
	got := ParseRefs([]byte(out))
	type short struct {
		name   string
		typ    RefType
		commit string
		ahead  int
		behind int
		gone   bool
		head   bool
		up     string
		remote string
	}
	var have []short
	for _, r := range got {
		s := short{r.Name, r.Type, r.Commit, r.Ahead, r.Behind, r.Gone, r.Head, "", r.Remote}
		if r.Upstream != nil {
			s.up = r.Upstream.Remote + "|" + r.Upstream.Name + "|" + r.Upstream.Ref
		}
		have = append(have, s)
	}
	want := []short{
		{"main", RefHead, h1, 1, 2, false, true, "origin|main|refs/remotes/origin/main", ""},
		{"topic", RefHead, h2, 0, 0, true, false, "", ""},
		{"local", RefHead, h2, 0, 3, false, false, ".|main|refs/heads/main", ""},
		{"solo", RefHead, h2, 0, 0, false, false, "", ""},
		{"in-sync", RefHead, h1, 0, 0, false, false, "origin|main|refs/remotes/origin/main", ""},
		{"origin/main", RefRemoteHead, h1, 0, 0, false, false, "", "origin"},
		{"origin/HEAD", RefRemoteHead, h1, 0, 0, false, false, "", "origin"},
		{"up/feature/x", RefRemoteHead, h3, 0, 0, false, false, "", "up"},
		{"light", RefTag, h1, 0, 0, false, false, "", ""},
		{"v1.0", RefTag, h1, 0, 0, false, false, "", ""},
	}
	if !reflect.DeepEqual(have, want) {
		t.Errorf("got  %+v\nwant %+v", have, want)
	}
	if got[0].CommitDate != time.Unix(1704110400, 0).UTC() || got[9].CommitDate != time.Unix(1704110400, 0).UTC() || got[9].Object != h3 {
		t.Errorf("dates or objects: %+v %+v", got[0], got[9])
	}
}

func TestParseStashes(t *testing.T) {
	h1, h2, p := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40)
	rec := func(hash, parents, ref, subject string) string {
		return hash + "\n" + parents + "\n" + ref + "\n" + subject + "\n1704110400\n1704110460\x00"
	}
	in := rec(h1, p+" "+h2, "stash@{0}", "On main: keep it") + rec(h2, p, "stash@{1}", "WIP on feature/x: abc1234 Subject: with colon") +
		rec(h2, p, "stash@{2}", "On (no branch): detached") + rec(h2, p, "stash@{3}", "custom subject")
	got := ParseStashes([]byte(in))
	if len(got) != 4 {
		t.Fatalf("got %+v", got)
	}
	for i, w := range []struct {
		index        int
		branch, desc string
		parents      int
		authorDate   int64
		hash         string
	}{
		{0, "main", "keep it", 2, 1704110400, h1},
		{1, "feature/x", "WIP (abc1234 Subject: with colon)", 1, 1704110400, h2},
		{2, "(no branch)", "detached", 1, 1704110400, h2},
		{3, "", "custom subject", 1, 1704110400, h2},
	} {
		s := got[i]
		if s.Index != w.index || s.Branch != w.branch || s.Description != w.desc || len(s.Parents) != w.parents || s.AuthorDate.Unix() != w.authorDate || s.Hash != w.hash {
			t.Errorf("stash %d = %+v", i, s)
		}
	}
	if len(ParseStashes([]byte(in[:len(in)-1]))) != 3 {
		t.Error("the last record, cut before its NUL, was not dropped")
	}
	if ParseStashes(nil) != nil {
		t.Error("empty output")
	}
}

func TestParseBlame(t *testing.T) {
	h1, h2 := strings.Repeat("1", 40), strings.Repeat("2", 40)
	out := h1 + " 1 1 2\nauthor Ada Example\nauthor-mail <ada@example.com>\nauthor-time 1704110400\nauthor-tz +0000\ncommitter Ada Example\n" +
		"committer-mail <ada@example.com>\ncommitter-time 1704110400\ncommitter-tz +0000\nsummary First commit\nboundary\nfilename a.txt\n" +
		h2 + " 3 3 1\nauthor Bo Example\nauthor-mail <bo@example.com>\nauthor-time 1704110460\nauthor-tz +0000\nsummary Second: with a colon\nprevious " + h1 + " a.txt\nfilename a.txt\n" +
		h1 + " 4 4 3\nfilename a.txt\n"
	want := []BlameCommit{
		{Hash: h1, Author: "Ada Example", AuthorEmail: "ada@example.com", AuthorTime: unix(1704110400), Summary: "First commit", Ranges: []LineRange{{1, 2}, {4, 6}}},
		{Hash: h2, Author: "Bo Example", AuthorEmail: "bo@example.com", AuthorTime: unix(1704110460), Summary: "Second: with a colon", Ranges: []LineRange{{3, 3}}},
	}
	if got := ParseBlame([]byte(out)); !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
	// Chunks cut anywhere give the same.
	for cut := 0; cut <= len(out); cut++ {
		var p BlameParser
		_, _ = p.Write([]byte(out[:cut]))
		_, _ = p.Write([]byte(out[cut:]))
		if !reflect.DeepEqual(p.Commits(), want) {
			t.Fatalf("two chunks cut at %d: %+v", cut, p.Commits())
		}
	}
	// A stream cut at every byte gives ranges that belong to the whole.
	for cut := 0; cut <= len(out); cut++ {
		got := ParseBlame([]byte(out[:cut]))
		for _, c := range got {
			if len(c.Ranges) > 2 {
				t.Fatalf("cut at %d: %+v", cut, c)
			}
		}
	}
	if got := ParseBlame(nil); len(got) != 0 {
		t.Errorf("empty = %+v", got)
	}
	t.Run("an empty e-mail and windows line ends", func(t *testing.T) {
		in := strings.ReplaceAll(h1+" 1 1 1\nauthor X\nauthor-mail <>\nauthor-time 5\nsummary s\nfilename f\n", "\n", "\r\n")
		got := ParseBlame([]byte(in))
		if len(got) != 1 || got[0].AuthorEmail != "" || got[0].Author != "X" {
			t.Errorf("got %+v", got)
		}
	})
}

func TestParseNameStatus(t *testing.T) {
	for _, tc := range []struct {
		name, in string
		want     []NameStatus
	}{
		{"empty", "", nil},
		{"the letters", "A\x00added.txt\x00M\x00mod.txt\x00D\x00gone.txt\x00T\x00type.txt\x00U\x00un.txt\x00",
			[]NameStatus{{'A', 0, "added.txt", ""}, {'M', 0, "mod.txt", ""}, {'D', 0, "gone.txt", ""}, {'T', 0, "type.txt", ""}, {'U', 0, "un.txt", ""}}},
		{"a rename and a copy", "R100\x00old name.txt\x00new name.txt\x00C075\x00src.txt\x00copy.txt\x00",
			[]NameStatus{{'R', 100, "new name.txt", "old name.txt"}, {'C', 75, "copy.txt", "src.txt"}}},
		{"odd names", "M\x00two\nlines \"q\" \xff.txt\x00", []NameStatus{{'M', 0, "two\nlines \"q\" \xff.txt", ""}}},
		{"cut after the status", "M\x00a.txt\x00M", []NameStatus{{'M', 0, "a.txt", ""}}},
		{"a rename cut after the old name", "R100\x00old\x00", nil},
		{"a path cut before its NUL", "M\x00a.txt\x00M\x00b.t", []NameStatus{{'M', 0, "a.txt", ""}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseNameStatus([]byte(tc.in)); !reflect.DeepEqual(got, tc.want) && (len(got) != 0 || len(tc.want) != 0) {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestParseRevParse(t *testing.T) {
	for _, tc := range []struct {
		name         string
		gitDir, top  string
		cwd          string
		want         RepoInfo
		wantErrRoute string
	}{
		{"a normal repository", ".git\n.git\ntrue\nfalse\nfalse\n", "/work/repo\n", "/work/repo",
			RepoInfo{WorkTree: "/work/repo", GitDir: "/work/repo/.git", CommonDir: "/work/repo/.git", InsideWorkTree: true}, ""},
		{"a subfolder gives absolute paths", "/work/repo/.git\n/work/repo/.git\ntrue\nfalse\nfalse\n", "/work/repo\n", "/work/repo/src",
			RepoInfo{WorkTree: "/work/repo", GitDir: "/work/repo/.git", CommonDir: "/work/repo/.git", InsideWorkTree: true}, ""},
		{"a linked worktree", "/work/repo/.git/worktrees/feature\n/work/repo/.git\ntrue\nfalse\nfalse\n", "/work/feature\n", "/work/feature",
			RepoInfo{WorkTree: "/work/feature", GitDir: "/work/repo/.git/worktrees/feature", CommonDir: "/work/repo/.git", InsideWorkTree: true}, ""},
		{"a submodule", "/work/repo/.git/modules/lib\n/work/repo/.git/modules/lib\ntrue\nfalse\nfalse\n", "/work/repo/lib\n/work/repo\n", "/work/repo/lib",
			RepoInfo{WorkTree: "/work/repo/lib", GitDir: "/work/repo/.git/modules/lib", CommonDir: "/work/repo/.git/modules/lib", SuperProject: "/work/repo", InsideWorkTree: true}, ""},
		{"a bare repository", ".\n.\nfalse\ntrue\nfalse\n", "", "/srv/bare.git",
			RepoInfo{GitDir: "/srv/bare.git", CommonDir: "/srv/bare.git", Bare: true}, ""},
		{"a shallow clone", ".git\n.git\ntrue\nfalse\ntrue\n", "/w\n", "/w", RepoInfo{WorkTree: "/w", GitDir: "/w/.git", CommonDir: "/w/.git", InsideWorkTree: true, Shallow: true}, ""},
		{"inside the git directory", ".\n.\nfalse\nfalse\nfalse\n", "", "/w/.git", RepoInfo{GitDir: "/w/.git", CommonDir: "/w/.git"}, ""},
		{"too few lines", ".git\n.git\n", "", "/w", RepoInfo{}, "ParseRevParse.GitOutputUnexpected"},
		{"empty output", "", "", "/w", RepoInfo{}, "ParseRevParse.GitOutputUnexpected"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseRevParse(tc.gitDir, tc.top, tc.cwd)
			if tc.wantErrRoute != "" {
				wantRoute(t, err, tc.wantErrRoute)
				return
			}
			if err != nil || got != tc.want {
				t.Errorf("got %+v, %v\nwant %+v", got, err, tc.want)
			}
		})
	}
}

func TestParseConfigList(t *testing.T) {
	in := "core.bare\nfalse\x00user.name\nAda Example\x00core.logallrefupdates\x00remote.origin.url\n/srv/a b.git\x00alias.x\n!echo \"hi\"\nsecond line\x00branch.main.merge\nrefs/heads/main\x00user.email\nada@exa"
	got := ParseConfigList([]byte(in))
	want := []ConfigEntry{
		{"core.bare", "false", true},
		{"user.name", "Ada Example", true},
		{"core.logallrefupdates", "", false},
		{"remote.origin.url", "/srv/a b.git", true},
		{"alias.x", "!echo \"hi\"\nsecond line", true},
		{"branch.main.merge", "refs/heads/main", true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v", got)
	}
	if ParseConfigList(nil) != nil {
		t.Error("empty output")
	}
}

func TestParseIgnored(t *testing.T) {
	rec := func(src, line, pattern, path string) string {
		return src + "\x00" + line + "\x00" + pattern + "\x00" + path + "\x00"
	}
	in := rec(".gitignore", "1", "*.log", "a.log") + rec(".gitignore", "2", "!keep.log", "keep.log") + rec(".git/info/exclude", "3", "build/", "build/out o.bin") + rec("", "", "", "x")
	if got := ParseIgnored([]byte(in)); !reflect.DeepEqual(got, []string{"a.log", "build/out o.bin"}) {
		t.Errorf("got %q", got)
	}
	if got := ParseIgnored([]byte(rec("g", "1", "*.log", "a.log") + "g\x001\x00*.log\x00partia")); !reflect.DeepEqual(got, []string{"a.log"}) {
		t.Errorf("cut path: got %q", got)
	}
}

// Every parser that reads a stream of records gives, for the stream cut at any byte, a prefix of what the whole gives.
func TestParsersGiveAPrefixForAStreamCutAtEveryByte(t *testing.T) {
	h := func(c string) string { return strings.Repeat(c, 40) }
	rec := func(parts ...string) string { return strings.Join(parts, "\x00") + "\x00" }
	for _, tc := range []struct {
		name  string
		whole string
		parse func(string) int // the number of records parsed
		count int
	}{
		{"name-status", rec("A", "a.txt", "M", "b c.txt", "R100", "old.txt", "new.txt", "D", "d.txt", "C080", "s", "t"), func(s string) int { return len(ParseNameStatus([]byte(s))) }, 5},
		{"stashes", rec(h("a")+"\n"+h("b")+"\nstash@{0}\nOn main: x\n1\n2", h("c")+"\n"+h("b")+"\nstash@{1}\nWIP on main: y\n3\n4"), func(s string) int { return len(ParseStashes([]byte(s))) }, 2},
		{"config", rec("a.b\nc", "d.e\nf g", "h.i", "j.k\nl"), func(s string) int { return len(ParseConfigList([]byte(s))) }, 4},
		{"check-ignore", rec("g", "1", "*.log", "a.log", "g", "2", "*.o", "b.o"), func(s string) int { return len(ParseIgnored([]byte(s))) }, 2},
		{"refs", strings.Join([]string{
			strings.Join([]string{"refs/heads/main", h("1"), "", "", "", "", "", "1", "", "*"}, "\x00"),
			strings.Join([]string{"refs/tags/v1", h("2"), h("1"), "", "", "", "", "", "1", " "}, "\x00"),
		}, "\n") + "\n", func(s string) int { return len(ParseRefs([]byte(s))) }, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if n := tc.parse(tc.whole); n != tc.count {
				t.Fatalf("the whole gives %d records, want %d", n, tc.count)
			}
			last := 0
			for cut := 0; cut <= len(tc.whole); cut++ {
				n := tc.parse(tc.whole[:cut])
				if n < last || n > tc.count {
					t.Fatalf("cut at %d gives %d records after %d", cut, n, last)
				}
				last = n
			}
		})
	}
}
