package workspace

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	"github.com/rm4n0s/errors"
)

func hitPaths(hits []QuickOpenHit) []string {
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.Path
	}
	return out
}

func TestQuickOpen(t *testing.T) {
	root := "/work/proj"
	idx := NewFileIndex(root)
	idx.SetFiles([]string{"main.go", "go.mod", "internal/server/server.go", "internal/server/util.go", "docs/guide.md", "docs/server.md"})
	abs := func(rel string) string { return filepath.Join(root, rel) }
	recent := []string{abs("docs/guide.md"), "/home/me/notes/server-notes.txt", abs("internal/server/util.go")}
	opts := QuickOpenOptions{IncludeHistory: true, Home: "/home/me"}
	tests := []struct {
		name      string
		idx       *FileIndex
		recent    []string
		query     string
		opts      QuickOpenOptions
		wantRecnt []string
		wantFiles []string // the start of the file results
		nFiles    int
	}{
		{"an empty query lists the recent files", idx, recent, "", opts,
			recent, nil, 0},
		{"recent files that match come first, and are not repeated", idx, recent, "server", opts,
			[]string{"/home/me/notes/server-notes.txt"}, []string{abs("docs/server.md"), abs("internal/server/server.go"), abs("internal/server/util.go")}, 3},
		{"history off", idx, recent, "server", QuickOpenOptions{IncludeHistory: false, Home: "/home/me"},
			nil, []string{abs("docs/server.md")}, 3},
		{"the match of a recent file is on its name unless the query has a slash", idx, recent, "util", opts,
			[]string{abs("internal/server/util.go")}, nil, 0},
		{"a query with a slash matches the folder of a recent file", idx, recent, "internal/server/u", opts,
			[]string{abs("internal/server/util.go")}, nil, 0},
		{"nothing matches", idx, recent, "qqqq", opts, nil, nil, 0},
		{"without a folder only the recent files are searched", nil, recent, "server", opts,
			[]string{"/home/me/notes/server-notes.txt"}, nil, 0},
		{"without a folder and an empty query", nil, recent, "", opts, recent, nil, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, err := QuickOpen(context.Background(), tc.idx, tc.recent, tc.query, tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			if got := hitPaths(res.Recent); !slices.Equal(got, tc.wantRecnt) {
				t.Errorf("recent %q, want %q", got, tc.wantRecnt)
			}
			files := hitPaths(res.Files)
			if len(files) != tc.nFiles && tc.nFiles != 0 || tc.nFiles == 0 && tc.wantFiles == nil && len(files) != 0 {
				t.Errorf("files %q, want %d", files, tc.nFiles)
			}
			if len(files) < len(tc.wantFiles) || !slices.Equal(files[:len(tc.wantFiles)], tc.wantFiles) {
				t.Errorf("files %q, want to start with %q", files, tc.wantFiles)
			}
		})
	}
}

func TestQuickOpenTexts(t *testing.T) {
	root := "/work/proj"
	idx := NewFileIndex(root)
	idx.SetFiles([]string{"main.go", "internal/server/server.go"})
	res, err := QuickOpen(context.Background(), idx, []string{"/work/proj/main.go", "/home/me/x/y.txt", "/etc/hosts"}, "", QuickOpenOptions{Home: "/home/me"})
	if err != nil {
		t.Fatal(err)
	}
	want := []struct{ name, desc string }{{"main.go", ""}, {"y.txt", "~/x"}, {"hosts", "/etc"}}
	for i, w := range want {
		if res.Recent[i].Name != w.name || res.Recent[i].Description != w.desc {
			t.Errorf("recent %d: %q %q, want %q %q", i, res.Recent[i].Name, res.Recent[i].Description, w.name, w.desc)
		}
	}
	res, err = QuickOpen(context.Background(), idx, nil, "srvgo", QuickOpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Files) != 1 || res.Files[0].Name != "server.go" || res.Files[0].Description != "internal/server" || res.Files[0].Path != "/work/proj/internal/server/server.go" {
		t.Errorf("files %+v", res.Files)
	}
	if len(res.Files[0].LabelMatch)+len(res.Files[0].DescriptionMatch) == 0 {
		t.Error("no highlights")
	}
	if !res.Complete {
		t.Error("a list set by SetFiles is complete")
	}
}

func TestQuickOpenHistorySortOrder(t *testing.T) {
	recent := []string{"/p/zz_server.go", "/p/server.go", "/p/a/server_util.go"}
	by := func(recency bool) []string {
		res, err := QuickOpen(context.Background(), nil, recent, "server", QuickOpenOptions{IncludeHistory: true, HistoryByRecency: recency})
		if err != nil {
			t.Fatal(err)
		}
		return hitPaths(res.Recent)
	}
	if got := by(true); !slices.Equal(got, recent) {
		t.Errorf("recency order %q", got)
	}
	if got := by(false); got[0] != "/p/server.go" {
		t.Errorf("by score %q, want the exact name first", got)
	}
}

func TestQuickOpenCancelled(t *testing.T) {
	idx := NewFileIndex("/work/proj")
	idx.SetFiles(generated(3*minChunk, 4))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := QuickOpen(ctx, idx, nil, "view", QuickOpenOptions{Workers: 2})
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("FileSearchCancelled") {
		t.Errorf("got %v", err)
	}
}
