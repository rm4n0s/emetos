package search

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/glob"
)

// tree writes the files (path to text) in a new temporary folder and returns its path.
func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, text := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// defaults are the options of the default settings, for a folder.
func defaults(dir string) Options {
	return Options{
		Folder: dir, UseIgnoreFiles: true, FollowSymlinks: true,
		ExcludeSettings: glob.ExpressionFromSetting(map[string]any{"**/.git": true, "**/node_modules": true}),
	}
}

// found runs a search and lists its matches as "path:line:col:text" (line and column from 0), sorted.
func found(t *testing.T, q Query, o Options) ([]string, Summary) {
	t.Helper()
	var mu sync.Mutex
	var out []string
	sum, err := Run(context.Background(), q, o, func(r FileResult) {
		mu.Lock()
		defer mu.Unlock()
		for _, m := range r.Matches {
			out = append(out, fmt.Sprintf("%s:%d:%d:%s", r.Path, m.Line, m.Col, strings.ReplaceAll(m.Text, "\n", `\n`)))
		}
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	sort.Strings(out)
	return out, sum
}

// waitGoroutines waits until the number of goroutines is back at most to before, or fails.
func waitGoroutines(t *testing.T, before int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if n := runtimeGoroutines(); n <= before {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("goroutines left: %d, before the search %d", runtimeGoroutines(), before)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func equal(a, b []string) bool { return slices.Equal(a, b) }

func runtimeGoroutines() int { return runtime.NumGoroutine() }
