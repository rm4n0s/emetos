package search

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// The tree of the speed test and the benchmarks (SPEC §14 "Search"): 20,000 files of 500 MB in 200 folders, made once per
// test binary in a temporary folder and removed when the binary ends. A file has lines of words; one file in twenty has the
// word quokka once.
const (
	speedFiles   = 20000
	speedFolders = 200
	speedBytes   = 500 << 20
	speedNeedle  = "quokka"
)

var (
	speedOnce sync.Once
	speedDir  string
	speedErr  error
)

// speedTree returns the folder of the generated tree, making it the first time.
func speedTree(tb testing.TB) string {
	tb.Helper()
	speedOnce.Do(func() { speedDir, speedErr = makeTree(speedFiles, speedFolders, speedBytes) })
	if speedErr != nil {
		tb.Fatal(speedErr)
	}
	return speedDir
}

// makeTree writes files files of about total bytes in folders folders. The text is one block of random lines, from which each
// file takes a part, so that making the tree is as fast as writing it.
func makeTree(files, folders int, total int64) (string, error) {
	dir, err := os.MkdirTemp("", "emetos-search-speed-*")
	if err != nil {
		return "", err
	}
	rng := rand.New(rand.NewPCG(1, 2))
	words := make([]string, 3000)
	for i := range words {
		n := 2 + rng.IntN(9)
		b := make([]byte, n)
		for j := range b {
			b[j] = byte('a' + rng.IntN(26))
		}
		words[i] = string(b)
	}
	var block strings.Builder
	for block.Len() < 4<<20 {
		for range 6 + rng.IntN(8) {
			block.WriteString(words[rng.IntN(len(words))])
			block.WriteByte(' ')
		}
		block.WriteByte('\n')
	}
	text := block.String()
	avg := int(total) / files
	for f := range folders {
		sub := filepath.Join(dir, fmt.Sprintf("d%03d", f), "src")
		if err := os.MkdirAll(sub, 0o755); err != nil {
			return dir, err
		}
		for i := range files / folders {
			n := files/folders*f + i
			size := avg/2 + rng.IntN(avg)
			start := rng.IntN(len(text) - size)
			part := text[start : start+size]
			if k := strings.IndexByte(part, '\n'); k >= 0 {
				part = part[k+1:] // begin at a line
			}
			if n%20 == 0 {
				k := len(part) / 2
				part = part[:k] + speedNeedle + " " + part[k:]
			}
			name := filepath.Join(sub, fmt.Sprintf("f%05d.txt", n))
			if err := os.WriteFile(name, []byte(part), 0o644); err != nil {
				return dir, err
			}
		}
	}
	return dir, nil
}

func TestMain(m *testing.M) {
	code := m.Run()
	if speedDir != "" {
		_ = os.RemoveAll(speedDir)
	}
	os.Exit(code)
}

// TestSpeedFirstResults holds the row "Search" of SPEC §14: the first results of a literal search of the generated tree come
// within 100 ms, off the caller's thread. It logs the whole search and the same search by GNU grep -rF.
func TestSpeedFirstResults(t *testing.T) {
	if testing.Short() {
		t.Skip("the tree of 500 MB is not made in a short run")
	}
	dir := speedTree(t)
	o := Options{
		Folder: dir, UseIgnoreFiles: true, FollowSymlinks: true,
		ExcludeSettings: defaults(dir).ExcludeSettings,
	}
	budget := 100 * time.Millisecond
	if raceEnabled {
		budget *= 10 // the race detector slows the walk and the reads
	}
	for _, tt := range []struct {
		name string
		q    Query
	}{
		{"case sensitive", Query{Pattern: speedNeedle, IsCaseSensitive: true}},
		{"ignoring case", Query{Pattern: strings.ToUpper(speedNeedle)}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// the first run warms the page cache, as the first search of a session does not need to be measured by the
			// target (SPEC §14 says warm cache)
			if _, err := Run(context.Background(), tt.q, o, func(FileResult) {}); err != nil {
				t.Fatal(err)
			}
			var first time.Duration
			start := time.Now()
			files := 0
			sum, err := Run(context.Background(), tt.q, o, func(FileResult) {
				if files == 0 {
					first = time.Since(start)
				}
				files++
			})
			total := time.Since(start)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("%s: first result after %v, all %d files with %d matches after %v (searched %d files)", tt.name, first, sum.Files, sum.Matches, total, sum.Searched)
			if first > budget {
				t.Errorf("the first result came after %v, the budget is %v", first, budget)
			}
			if sum.Searched != speedFiles || sum.Files != speedFiles/20 {
				t.Errorf("searched %d files (want %d), %d with matches (want %d)", sum.Searched, speedFiles, sum.Files, speedFiles/20)
			}
		})
	}
	if grep, err := exec.LookPath("grep"); err == nil && !raceEnabled {
		if out, err := exec.Command(grep, "--version").Output(); err == nil && strings.Contains(string(out), "GNU grep") {
			cmd := exec.Command(grep, "-rF", "--binary-files=without-match", speedNeedle, dir)
			start := time.Now()
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("grep: %v", err)
			}
			t.Logf("GNU grep -rF: %d lines in %v", strings.Count(string(out), "\n"), time.Since(start))
			cmd = exec.Command(grep, "-rFi", "--binary-files=without-match", strings.ToUpper(speedNeedle), dir)
			start = time.Now()
			out, err = cmd.Output()
			if err != nil {
				t.Fatalf("grep: %v", err)
			}
			t.Logf("GNU grep -rFi: %d lines in %v", strings.Count(string(out), "\n"), time.Since(start))
		}
	}
}

func BenchmarkRun(b *testing.B) {
	dir := speedTree(b)
	base := Options{Folder: dir, UseIgnoreFiles: true, FollowSymlinks: true, ExcludeSettings: defaults(dir).ExcludeSettings}
	benches := []struct {
		name    string
		q       Query
		include string
	}{
		{"literal over 20000 files", Query{Pattern: speedNeedle, IsCaseSensitive: true}, ""},
		{"literal ignoring case over 20000 files", Query{Pattern: strings.ToUpper(speedNeedle)}, ""},
		{"regular expression over 1 folder", Query{Pattern: `qu+okk?a\b`, IsRegExp: true}, "./d000"},
		{"multiline regular expression over 1 folder", Query{Pattern: `quokka \w+\n`, IsRegExp: true}, "./d000"},
	}
	for _, bb := range benches {
		b.Run(bb.name, func(b *testing.B) {
			o := base
			o.Include = ParsePatterns(bb.include, dir)
			b.ReportAllocs()
			for b.Loop() {
				if _, err := Run(context.Background(), bb.q, o, func(FileResult) {}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
