package workspace

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The benchmarks of the quick open (SPEC §14, docs/PERFORMANCE.md): one operation of
// BenchmarkQuickOpen100k is one keystroke, the search of an index of 100,000 paths end to end (score
// every path, order the best, make the rows). BenchmarkIndexWalk lists a tree on disk.

var (
	benchDirs = []string{
		"internal", "cmd", "pkg", "core", "gui", "views", "explorer", "editor", "workbench", "kit",
		"buffer", "document", "settings", "keybinding", "workspace", "storage", "language", "theme",
		"tokens", "textmate", "find", "folding", "snippet", "emmet", "testdata", "goldens", "fixtures",
		"scripts", "docs", "third_party", "assets", "fonts", "icons", "commands", "menus", "dialogs",
		"notify", "output", "problems", "terminal", "search", "scm", "debug", "extensions", "tests",
		"util", "testutil", "proof", "reference", "vscode", "src", "vs", "base", "common", "browser",
		"contrib", "files", "services", "platform", "node", "electron-node", "api", "client", "server",
	}
	benchWords = []string{
		"body", "view", "model", "state", "widget", "tree", "list", "input", "button", "label",
		"scorer", "fuzzy", "query", "cache", "match", "filter", "compare", "sort", "index", "main",
		"options", "config", "service", "manager", "provider", "handler", "registry", "factory",
		"editor", "document", "buffer", "layout", "paint", "render", "theme", "color", "font",
		"keymap", "command", "action", "menu", "dialog", "notification", "toast", "status", "title",
		"explorer", "problems", "output", "terminal", "search", "replace", "folding", "snippet",
		"tokenizer", "grammar", "language", "selection", "cursor", "undo", "history", "watcher",
		"storage", "workspace", "settings", "extension", "debug", "adapter", "session", "client",
	}
	benchExts = []string{".go", ".go", ".go", ".go", "_test.go", "_test.go", ".json", ".md", ".ts", ".txt", ".svg", ".png"}
)

// benchPaths makes n deterministic, realistic relative paths (the same generator as the benchmarks of
// core/fuzzy).
func benchPaths(n int) []string {
	rng := rand.New(rand.NewPCG(1, 2))
	paths := make([]string, n)
	var sb strings.Builder
	for i := range paths {
		sb.Reset()
		for d, depth := 0, 2+rng.IntN(5); d < depth; d++ {
			sb.WriteString(benchDirs[rng.IntN(len(benchDirs))])
			sb.WriteByte('/')
		}
		sb.WriteString(benchWords[rng.IntN(len(benchWords))])
		if rng.IntN(3) == 0 {
			w := benchWords[rng.IntN(len(benchWords))]
			if rng.IntN(2) == 0 {
				sb.WriteString(strings.ToUpper(w[:1]) + w[1:])
			} else {
				sb.WriteString("_" + w)
			}
		}
		sb.WriteString(benchExts[rng.IntN(len(benchExts))])
		paths[i] = sb.String()
	}
	return paths
}

var benchQueries = []struct{ name, query string }{
	{"fuzzy edtvw", "edtvw"},
	{"path gui/exp/bo", "gui/exp/bo"},
	{"multi word view model", "view model"},
	{"one letter e", "e"},
	{"no match zqxjk", "zqxjk"},
}

func BenchmarkQuickOpen100k(b *testing.B) {
	idx := NewFileIndex("/work/proj")
	idx.SetFiles(benchPaths(100_000))
	for _, workers := range []int{1, 0} {
		label := "parallel"
		if workers == 1 {
			label = "one goroutine"
		}
		for _, bq := range benchQueries {
			b.Run(label+"/"+bq.name, func(b *testing.B) {
				b.ReportAllocs()
				matches := 0
				for range b.N {
					res, err := QuickOpen(context.Background(), idx, nil, bq.query, QuickOpenOptions{Workers: workers})
					if err != nil {
						b.Fatal(err)
					}
					matches = len(res.Files)
				}
				b.ReportMetric(float64(matches), "rows")
			})
		}
	}
}

// TestQuickOpen100kWithinTarget checks the target of SPEC §14 (a keystroke over 100,000 paths in under
// 30 ms) with a bound twenty times as big, so that a slow or busy machine and the race detector do not
// make it flaky: it catches a change that makes the search a lot slower. The real numbers are the
// benchmark's.
func TestQuickOpen100kWithinTarget(t *testing.T) {
	if testing.Short() {
		t.Skip("timing test")
	}
	const bound = 20 * 30 * time.Millisecond
	idx := NewFileIndex("/work/proj")
	idx.SetFiles(benchPaths(100_000))
	for _, bq := range benchQueries {
		t.Run(bq.name, func(t *testing.T) {
			best := time.Hour
			for range 3 { // the best of three: a busy machine slows single runs
				start := time.Now()
				if _, err := QuickOpen(context.Background(), idx, nil, bq.query, QuickOpenOptions{}); err != nil {
					t.Fatal(err)
				}
				best = min(best, time.Since(start))
			}
			t.Logf("%s: %v", bq.query, best)
			if best > bound {
				t.Errorf("%v for 100,000 paths, bound %v", best, bound)
			}
		})
	}
}

// walkTree makes a tree of about n files on disk: folders of 25 files, three levels deep, with an ignore
// file, an excluded folder and a few ignored files.
func walkTree(b testing.TB, n int) string {
	b.Helper()
	root := b.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("*.log\nbuild/\n"), 0o644); err != nil {
		b.Fatal(err)
	}
	made := 0
	for i := 0; made < n; i++ {
		dir := filepath.Join(root, fmt.Sprintf("d%02d", i%10), fmt.Sprintf("e%02d", (i/10)%10), fmt.Sprintf("f%03d", i))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			b.Fatal(err)
		}
		for j := range 25 {
			name := fmt.Sprintf("file%02d.go", j)
			if j%12 == 0 {
				name = fmt.Sprintf("file%02d.log", j) // ignored
			}
			if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
				b.Fatal(err)
			}
			made++
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "node_modules", "m"), 0o755); err != nil {
		b.Fatal(err)
	}
	return root
}

func BenchmarkIndexWalk(b *testing.B) {
	root := walkTree(b, 5000)
	b.ResetTimer()
	var files int
	for range b.N {
		x := NewFileIndex(root)
		x.Start(context.Background())
		x.Wait()
		files = len(x.Snapshot().Files)
		x.Close()
	}
	b.ReportMetric(float64(files), "files")
	b.ReportMetric(float64(files)*float64(b.N)/b.Elapsed().Seconds(), "files/s")
}
