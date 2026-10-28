package fakeapi

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rm4n0s/emetos/extension"
)

func TestFakeSearch(t *testing.T) {
	folder := t.TempDir()
	for name, text := range map[string]string{"a.txt": "one Needle\ntwo needles\n", "sub/b.txt": "nothing\nNEEDLE here\n", "c.md": "needle\n"} {
		path := filepath.Join(folder, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a := New(t, manifest(), extension.ScopeInfo{Kind: extension.ScopeWorkspace, Folder: folder})
	a.SetDocument("file://"+filepath.Join(folder, "c.md"), "changed\nand a needle\n")
	collect := func(q extension.TextSearchQuery, o extension.TextSearchOptions) (map[string][]string, extension.TextSearchSummary) {
		got := map[string][]string{}
		sum, err := a.Workspace().FindTextInFiles(context.Background(), q, o, func(f extension.TextSearchFile) {
			for _, m := range f.Matches {
				got[f.Path] = append(got[f.Path], m.Text)
			}
		})
		if err != nil {
			t.Fatal(err)
		}
		return got, sum
	}
	got, sum := collect(extension.TextSearchQuery{Pattern: "needle"}, extension.TextSearchOptions{})
	if len(got["a.txt"]) != 2 || len(got["sub/b.txt"]) != 1 || len(got["c.md"]) != 1 || sum.Files != 3 || sum.Matches != 4 {
		t.Errorf("results %v, summary %+v", got, sum)
	}
	got, _ = collect(extension.TextSearchQuery{Pattern: "needle", IsWordMatch: true, IsCaseSensitive: true}, extension.TextSearchOptions{})
	if len(got) != 1 || len(got["c.md"]) != 1 {
		t.Errorf("whole word, case: %v", got)
	}
	got, _ = collect(extension.TextSearchQuery{Pattern: "needle"}, extension.TextSearchOptions{Include: "sub", Exclude: "zzz"})
	if len(got) != 1 || len(got["sub/b.txt"]) != 1 {
		t.Errorf("include: %v", got)
	}
	_, sum = collect(extension.TextSearchQuery{Pattern: "needle"}, extension.TextSearchOptions{MaxResults: 2})
	if sum.Matches != 2 || !sum.LimitHit {
		t.Errorf("limit: %+v", sum)
	}
	if calls := a.SearchCalls(); len(calls) != 4 || calls[0].Query.Pattern != "needle" {
		t.Errorf("calls %+v", calls)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.Workspace().FindTextInFiles(cancelled, extension.TextSearchQuery{Pattern: "needle"}, extension.TextSearchOptions{}, func(extension.TextSearchFile) {}); err == nil {
		t.Error("a cancelled context should fail")
	}

	_, err := a.Workspace().FindTextInFiles(context.Background(), extension.TextSearchQuery{}, extension.TextSearchOptions{}, nil)
	route(t, err, "searchRegexp.FakeSearchPatternEmpty")
	_, err = a.Workspace().FindTextInFiles(context.Background(), extension.TextSearchQuery{Pattern: "(", IsRegExp: true}, extension.TextSearchOptions{}, nil)
	route(t, err, "searchRegexp.FakeSearchQueryInvalid")
	noFolder := New(t, manifest(), extension.ScopeInfo{})
	_, err = noFolder.Workspace().FindTextInFiles(context.Background(), extension.TextSearchQuery{Pattern: "x"}, extension.TextSearchOptions{}, nil)
	route(t, err, "workspace.FindTextInFiles.FakeNoFolder")
	_, err = noFolder.Workspace().ReplaceInFiles(context.Background(), nil)
	route(t, err, "workspace.ReplaceInFiles.FakeNoFolder")
	_, err = a.Workspace().ReplaceInFiles(context.Background(), []extension.FileReplacement{{Path: "../x"}})
	route(t, err, "workspace.ReplaceInFiles.FakePathInvalid")
}

func TestFakeReplace(t *testing.T) {
	folder := t.TempDir()
	if err := os.WriteFile(filepath.Join(folder, "a.txt"), []byte("one needle\nNeedle two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := New(t, manifest(), extension.ScopeInfo{Kind: extension.ScopeWorkspace, Folder: folder})
	r, err := a.Workspace().NewTextReplacer(extension.TextSearchQuery{Pattern: "needle"}, "pin", true)
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Replace("Needle"); got != "Pin" {
		t.Errorf("preserve case: %q", got)
	}
	rr, err := a.Workspace().NewTextReplacer(extension.TextSearchQuery{Pattern: `(\w+) (\w+)`, IsRegExp: true}, "$2 $1", false)
	if err != nil {
		t.Fatal(err)
	}
	if got := rr.Replace("ab cd"); got != "cd ab" {
		t.Errorf("groups: %q", got)
	}
	rep := func(l, c, el, ec int, expected, text string) extension.TextReplacement {
		return extension.TextReplacement{Range: extension.Range{Start: extension.Position{Line: l, Col: c}, End: extension.Position{Line: el, Col: ec}}, Expected: expected, Text: text}
	}
	report, err := a.Workspace().ReplaceInFiles(context.Background(), []extension.FileReplacement{
		{Path: "a.txt", Replacements: []extension.TextReplacement{rep(0, 4, 0, 10, "needle", "pin"), rep(1, 0, 1, 6, "WRONG!", "x")}},
		{Path: "missing.txt", Replacements: []extension.TextReplacement{rep(0, 0, 0, 1, "a", "b")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Replaced != 1 || report.Skipped != 1 || len(report.Files) != 1 || len(report.Failed) != 1 || report.Failed[0].Path != "missing.txt" {
		t.Errorf("report %+v", report)
	}
	if data, _ := os.ReadFile(filepath.Join(folder, "a.txt")); string(data) != "one pin\nNeedle two\n" {
		t.Errorf("file %q", data)
	}
	if len(a.ReplaceCalls()) != 1 {
		t.Errorf("calls %v", a.ReplaceCalls())
	}
}
