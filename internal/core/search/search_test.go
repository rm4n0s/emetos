package search

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"unicode/utf16"

	"github.com/rm4n0s/emetos/internal/core/glob"
)

func TestMatching(t *testing.T) {
	files := map[string]string{
		"a.txt": "Hello world\nhello WORLD\nwoRld hellos\nthe world's end\n",
		"b.go":  "package main\n\nfunc Hello() {}\nfunc hello_world() {}\n",
		"c.txt": "ünïcode Ünï here\nfoo\r\nbar foo\r\n",
		"d.txt": "alpha\nbeta\ngamma\n",
	}
	tests := []struct {
		name string
		q    Query
		want []string
	}{
		{"literal ignores case", Query{Pattern: "hello"}, []string{
			"a.txt:0:0:Hello", "a.txt:1:0:hello", "a.txt:2:6:hello", "b.go:2:5:Hello", "b.go:3:5:hello"}},
		{"case sensitive", Query{Pattern: "hello", IsCaseSensitive: true}, []string{"a.txt:1:0:hello", "a.txt:2:6:hello", "b.go:3:5:hello"}},
		{"smart case with a capital", Query{Pattern: "Hello", IsSmartCase: true}, []string{"a.txt:0:0:Hello", "b.go:2:5:Hello"}},
		{"smart case without a capital", Query{Pattern: "hello", IsSmartCase: true}, []string{
			"a.txt:0:0:Hello", "a.txt:1:0:hello", "a.txt:2:6:hello", "b.go:2:5:Hello", "b.go:3:5:hello"}},
		{"whole word", Query{Pattern: "hello", IsWordMatch: true}, []string{"a.txt:0:0:Hello", "a.txt:1:0:hello", "b.go:2:5:Hello"}},
		{"whole word ends at an apostrophe", Query{Pattern: "world", IsWordMatch: true, IsCaseSensitive: true}, []string{"a.txt:0:6:world", "a.txt:3:4:world"}},
		{"whole word with a symbol at the end", Query{Pattern: "Hello(", IsWordMatch: true}, []string{"b.go:2:5:Hello("}},
		{"regular expression", Query{Pattern: `h\w+o\b`, IsRegExp: true}, []string{"a.txt:0:0:Hello", "a.txt:1:0:hello", "b.go:2:5:Hello"}},
		{"regular expression with a group and case", Query{Pattern: `^(wo)Rld`, IsRegExp: true, IsCaseSensitive: true}, []string{"a.txt:2:0:woRld"}},
		{"regular expression whole word", Query{Pattern: `hel+o`, IsRegExp: true, IsWordMatch: true}, []string{"a.txt:0:0:Hello", "a.txt:1:0:hello", "b.go:2:5:Hello"}},
		{"regular expression end of a line with CRLF", Query{Pattern: `foo$`, IsRegExp: true}, []string{"c.txt:1:0:foo", "c.txt:2:4:foo"}},
		{"unicode case folding", Query{Pattern: "ÜNÏ"}, []string{"c.txt:0:0:ünï", "c.txt:0:10:Ünï"}},
		{"non ASCII text with case", Query{Pattern: "Ünï", IsCaseSensitive: true}, []string{"c.txt:0:10:Ünï"}},
		{"a line break in the text", Query{Pattern: "alpha\nbeta"}, []string{`d.txt:0:0:alpha\nbeta`}},
		{"a regular expression over lines", Query{Pattern: `beta\ngam`, IsRegExp: true}, []string{`d.txt:1:0:beta\ngam`}},
		{"a regular expression with \\W over lines", Query{Pattern: `alpha\W+beta`, IsRegExp: true}, []string{`d.txt:0:0:alpha\nbeta`}},
		{"a multiline regular expression with CRLF", Query{Pattern: `foo\nbar`, IsRegExp: true}, []string{`c.txt:1:0:foo\nbar`}},
		{"empty matches are not results", Query{Pattern: `x*`, IsRegExp: true, IsCaseSensitive: true}, nil},
		{"no match", Query{Pattern: "zzz"}, nil},
	}
	dir := tree(t, files)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := found(t, tt.q, defaults(dir))
			if !equal(got, tt.want) {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMatchRanges(t *testing.T) {
	dir := tree(t, map[string]string{"f.txt": "one\r\ntwo three\r\nfour\r\n" + strings.Repeat("x", 3000) + "NEEDLE" + strings.Repeat("y", 3000) + "\n"})
	var got []Match
	_, err := Run(context.Background(), Query{Pattern: `three\r?\nfour`, IsRegExp: true}, defaults(dir), func(r FileResult) { got = append(got, r.Matches...) })
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("matches: %+v", got)
	}
	m := got[0]
	if m.Line != 1 || m.Col != 4 || m.EndLine != 2 || m.EndCol != 4 || m.Preview != "two three" || m.PreviewStart != 4 || m.PreviewEnd != 9 || m.Text != "three\nfour" {
		t.Errorf("multiline match: %+v", m)
	}
	got = nil
	if _, err := Run(context.Background(), Query{Pattern: "needle"}, defaults(dir), func(r FileResult) { got = append(got, r.Matches...) }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("matches: %+v", got)
	}
	m = got[0]
	if m.Line != 3 || m.Col != 3000 || len(m.Preview) > MaxPreview || m.Preview[m.PreviewStart:m.PreviewEnd] != "NEEDLE" {
		t.Errorf("long line: line %d col %d preview %d bytes %q", m.Line, m.Col, len(m.Preview), m.Preview[m.PreviewStart:m.PreviewEnd])
	}
}

func TestFilters(t *testing.T) {
	files := map[string]string{
		"main.go":                 "needle\n",
		"util_test.go":            "needle\n",
		"src/a.js":                "needle\n",
		"src/lib/b.js":            "needle\n",
		"src/lib/deep/c.ts":       "needle\n",
		"docs/readme.md":          "needle\n",
		"node_modules/pkg/x.js":   "needle\n",
		"build/out.js":            "needle\n",
		".hidden/secret.txt":      "needle\n",
		".git/config":             "needle\n",
		"vendor/lib/not_this.txt": "needle\n",
	}
	tests := []struct {
		name             string
		include, exclude string
		settings         map[string]any
		want             []string
	}{
		{"everything but the excluded", "", "", map[string]any{"**/node_modules": true, "**/.git": true, "build": true},
			[]string{".hidden/secret.txt:0:0:needle", "docs/readme.md:0:0:needle", "main.go:0:0:needle", "src/a.js:0:0:needle", "src/lib/b.js:0:0:needle",
				"src/lib/deep/c.ts:0:0:needle", "util_test.go:0:0:needle", "vendor/lib/not_this.txt:0:0:needle"}},
		{"a file extension", "*.js", "", map[string]any{"**/node_modules": true, "**/.git": true},
			[]string{"build/out.js:0:0:needle", "src/a.js:0:0:needle", "src/lib/b.js:0:0:needle"}},
		{"a dot extension", ".ts", "", map[string]any{"**/.git": true}, []string{"src/lib/deep/c.ts:0:0:needle"}},
		{"two patterns", "*.md, main.go", "", nil, []string{"docs/readme.md:0:0:needle", "main.go:0:0:needle"}},
		{"a folder name anywhere", "lib", "", nil, []string{"src/lib/b.js:0:0:needle", "src/lib/deep/c.ts:0:0:needle", "vendor/lib/not_this.txt:0:0:needle"}},
		{"a folder of the folder", "./src", "", nil, []string{"src/a.js:0:0:needle", "src/lib/b.js:0:0:needle", "src/lib/deep/c.ts:0:0:needle"}},
		{"a folder of the folder is not found deeper", "./lib", "", nil, nil},
		{"braces keep their commas", "src/**/*.{js,ts}", "", nil, []string{"src/a.js:0:0:needle", "src/lib/b.js:0:0:needle", "src/lib/deep/c.ts:0:0:needle"}},
		{"exclude box", "", "*.js, vendor", map[string]any{"**/.git": true, "**/node_modules": true},
			[]string{".hidden/secret.txt:0:0:needle", "docs/readme.md:0:0:needle", "main.go:0:0:needle", "src/lib/deep/c.ts:0:0:needle", "util_test.go:0:0:needle"}},
		{"include and exclude", "./src", "*.ts", map[string]any{"**/.git": true}, []string{"src/a.js:0:0:needle", "src/lib/b.js:0:0:needle"}},
		{"exclude a folder of the folder", "", "./src/lib", map[string]any{"**/.git": true, "**/node_modules": true},
			[]string{".hidden/secret.txt:0:0:needle", "build/out.js:0:0:needle", "docs/readme.md:0:0:needle", "main.go:0:0:needle", "src/a.js:0:0:needle",
				"util_test.go:0:0:needle", "vendor/lib/not_this.txt:0:0:needle"}},
		{"a setting that is off", "*.md", "", map[string]any{"**/*.md": false}, []string{"docs/readme.md:0:0:needle"}},
	}
	dir := tree(t, files)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := defaults(dir)
			o.ExcludeSettings = glob.ExpressionFromSetting(tt.settings)
			o.Include = ParsePatterns(tt.include, dir)
			o.Exclude = ParsePatterns(tt.exclude, dir)
			got, _ := found(t, Query{Pattern: "needle"}, o)
			if !equal(got, tt.want) {
				t.Errorf("got %q\nwant %q", got, tt.want)
			}
		})
	}
}

func TestParsePatterns(t *testing.T) {
	tests := []struct {
		text string
		want []string
	}{
		{"", nil},
		{"*.go", []string{"**/*.go/**", "**/*.go"}},
		{".js", []string{"**/*.js/**", "**/*.js"}},
		{"foo/", []string{"**/foo/**", "**/foo"}},
		{"./src, ./docs/", []string{"src", "src/**", "docs", "docs/**"}},
		{"./", []string{"**"}},
		{"../up, ~/home", nil},
		{"/work/proj/src", []string{"src", "src/**"}},
		{"/elsewhere/src", nil},
		{"a/{b,c}, d", []string{"**/a/{b,c}/**", "**/a/{b,c}", "**/d/**", "**/d"}},
		{" , ,", nil},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			if got := ParsePatterns(tt.text, "/work/proj"); !equal(got, tt.want) {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIgnoreFiles(t *testing.T) {
	files := map[string]string{
		".gitignore":           "*.log\n/build\ntmp/\n!keep.log\ndocs/*.txt\n",
		"a.log":                "needle\n",
		"keep.log":             "needle\n",
		"build/x.txt":          "needle\n",
		"sub/build/y.md":       "needle\n",
		"tmp/z.txt":            "needle\n",
		"sub/tmp":              "needle\n",
		"docs/a.txt":           "needle\n",
		"docs/deep/b.txt":      "needle\n",
		"sub/.gitignore":       "secret\n!/sub-kept.txt\n*.txt\n!x.txt\n",
		"sub/secret":           "needle\n",
		"sub/other.txt":        "needle\n",
		"sub/x.txt":            "needle\n",
		"sub/deeper/secret":    "needle\n",
		"sub/deeper/note.txt":  "needle\n",
		"sub/deeper/k.md":      "needle\n",
		".ignore":              "ignored_by_ignore.md\n",
		"ignored_by_ignore.md": "needle\n",
		"plain.md":             "needle\n",
		".git/info/exclude":    "excluded_by_git.md\n",
		"excluded_by_git.md":   "needle\n",
	}
	dir := tree(t, files)
	on, _ := found(t, Query{Pattern: "needle"}, defaults(dir))
	off := defaults(dir)
	off.UseIgnoreFiles = false
	all, _ := found(t, Query{Pattern: "needle"}, off)
	wantOn := []string{
		"docs/deep/b.txt:0:0:needle", "keep.log:0:0:needle", "plain.md:0:0:needle", "sub/build/y.md:0:0:needle", "sub/deeper/k.md:0:0:needle",
		"sub/tmp:0:0:needle", "sub/x.txt:0:0:needle",
	}
	if !equal(on, wantOn) {
		t.Errorf("ignore files on:\n got %q\nwant %q", on, wantOn)
	}
	if len(all) <= len(on) {
		t.Errorf("the ignore files made no difference: %d and %d", len(all), len(on))
	}
	if !strings.Contains(strings.Join(all, " "), "a.log") || !strings.Contains(strings.Join(all, " "), "excluded_by_git.md") {
		t.Errorf("ignore files off should find ignored files: %q", all)
	}
}

func TestParentAndGlobalIgnoreFiles(t *testing.T) {
	root := tree(t, map[string]string{
		".gitignore":          "from_parent.md\n",
		"proj/from_parent.md": "needle\n",
		"proj/kept.md":        "needle\n",
		"proj/global.md":      "needle\n",
	})
	global := filepath.Join(t.TempDir(), "ignore")
	if err := os.WriteFile(global, []byte("global.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	proj := filepath.Join(root, "proj")
	tests := []struct {
		name   string
		parent bool
		global string
		want   []string
	}{
		{"neither", false, "", []string{"from_parent.md:0:0:needle", "global.md:0:0:needle", "kept.md:0:0:needle"}},
		{"parent", true, "", []string{"global.md:0:0:needle", "kept.md:0:0:needle"}},
		{"global", false, global, []string{"from_parent.md:0:0:needle", "kept.md:0:0:needle"}},
		{"both", true, global, []string{"kept.md:0:0:needle"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := defaults(proj)
			o.UseParentIgnoreFiles, o.GlobalIgnoreFile = tt.parent, tt.global
			got, _ := found(t, Query{Pattern: "needle"}, o)
			if !equal(got, tt.want) {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBinaryAndEncodings(t *testing.T) {
	utf16le := func(s string) string {
		u := utf16.Encode([]rune(s))
		b := []byte{0xFF, 0xFE}
		for _, c := range u {
			b = append(b, byte(c), byte(c>>8))
		}
		return string(b)
	}
	utf16beNoBOM := func(s string) string {
		var b []byte
		for _, c := range utf16.Encode([]rune(s)) {
			b = append(b, byte(c>>8), byte(c))
		}
		return string(b)
	}
	dir := tree(t, map[string]string{
		"text.txt":     "a needle\n",
		"binary.bin":   "needle\x00\x01\x02 more\n",
		"late-nul.bin": strings.Repeat("x", 9000) + "\x00 needle\n", // a NUL after the first 8 KiB: still text
		"bom.txt":      "\xef\xbb\xbfneedle at the start\n",
		"utf16.txt":    utf16le("one\nneedle é\n"),
		"latin.txt":    "caf\xe9 needle\n",
		"invalid.txt":  "bad \xff byte needle\n",
		"nobom16.txt":  utf16beNoBOM("needle in 16\n"),
	})
	tests := []struct {
		name     string
		encoding string
		want     []string
		binary   int
	}{
		{"utf8", "", []string{"bom.txt:0:0:needle", "invalid.txt:0:13:needle", "late-nul.bin:0:9002:needle", "latin.txt:0:7:needle", "text.txt:0:2:needle", "utf16.txt:1:0:needle"}, 2},
		{"utf8 spelled out", "utf8", []string{"bom.txt:0:0:needle", "invalid.txt:0:13:needle", "late-nul.bin:0:9002:needle", "latin.txt:0:7:needle", "text.txt:0:2:needle", "utf16.txt:1:0:needle"}, 2},
		{"utf16be configured", "utf16be", []string{"bom.txt:0:0:needle", "nobom16.txt:0:0:needle", "utf16.txt:1:0:needle"}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := defaults(dir)
			o.Encoding = tt.encoding
			got, sum := found(t, Query{Pattern: "needle"}, o)
			if tt.encoding == "utf16be" {
				// the files that are not UTF-16 decode to something else; only these must be found
				var keep []string
				for _, g := range got {
					if strings.HasPrefix(g, "bom.txt") || strings.HasPrefix(g, "nobom16") || strings.HasPrefix(g, "utf16.txt") {
						keep = append(keep, g)
					}
				}
				got = keep
			}
			if !equal(got, tt.want) {
				t.Errorf("got %q\nwant %q", got, tt.want)
			}
			if sum.Binary != tt.binary {
				t.Errorf("binary files left out: %d, want %d", sum.Binary, tt.binary)
			}
		})
	}
	t.Run("windows1252 configured", func(t *testing.T) {
		o := defaults(dir)
		o.Encoding = "windows1252"
		var preview string
		_, err := Run(context.Background(), Query{Pattern: "needle"}, o, func(r FileResult) {
			if r.Path == "latin.txt" {
				preview = r.Matches[0].Preview
			}
		})
		if err != nil {
			t.Fatal(err)
		}
		if preview != "café needle" {
			t.Errorf("preview %q", preview)
		}
	})
}

func TestSymlinks(t *testing.T) {
	dir := tree(t, map[string]string{
		"real/a.txt":  "needle\n",
		"other/b.txt": "needle\n",
	})
	outside := tree(t, map[string]string{"c.txt": "needle\n"})
	for _, l := range [][2]string{
		{filepath.Join(dir, "real"), filepath.Join(dir, "other", "loop")},      // other/loop -> real (fine)
		{dir, filepath.Join(dir, "real", "up")},                                // real/up -> the folder itself (a loop)
		{filepath.Join(dir, "other"), filepath.Join(dir, "real", "sibling")},   // real/sibling -> other, which has loop -> real
		{outside, filepath.Join(dir, "linked-outside")},                        // a folder outside
		{filepath.Join(dir, "real", "a.txt"), filepath.Join(dir, "file-link")}, // a link to a file
		{filepath.Join(dir, "nowhere"), filepath.Join(dir, "broken")},          // a broken link
	} {
		if err := os.Symlink(l[0], l[1]); err != nil {
			t.Fatal(err)
		}
	}
	follow := defaults(dir)
	got, _ := found(t, Query{Pattern: "needle"}, follow)
	want := []string{
		"file-link:0:0:needle", "linked-outside/c.txt:0:0:needle", "other/b.txt:0:0:needle", "other/loop/a.txt:0:0:needle", "real/a.txt:0:0:needle",
		"real/sibling/b.txt:0:0:needle",
	}
	if !equal(got, want) {
		t.Errorf("following links:\n got %q\nwant %q", got, want)
	}
	nofollow := defaults(dir)
	nofollow.FollowSymlinks = false
	got, _ = found(t, Query{Pattern: "needle"}, nofollow)
	if want := []string{"other/b.txt:0:0:needle", "real/a.txt:0:0:needle"}; !equal(got, want) {
		t.Errorf("not following links:\n got %q\nwant %q", got, want)
	}
}

func TestUnsavedAndOpenEditors(t *testing.T) {
	dir := tree(t, map[string]string{"a.txt": "disk needle\n", "b.txt": "disk needle\n", "c.txt": "nothing\n"})
	unsaved := map[string]string{
		filepath.Join(dir, "a.txt"):   "unsaved text\nand a needle here\n",
		filepath.Join(dir, "c.txt"):   "needle now\n",
		filepath.Join(dir, "new.txt"): "needle in a new file\n",
		"/elsewhere/outside.txt":      "needle outside the folder\n",
	}
	t.Run("the text of the editor replaces the file", func(t *testing.T) {
		o := defaults(dir)
		o.Unsaved = unsaved
		got, _ := found(t, Query{Pattern: "needle"}, o)
		want := []string{"a.txt:1:6:needle", "b.txt:0:5:needle", "c.txt:0:0:needle", "new.txt:0:0:needle"}
		if !equal(got, want) {
			t.Errorf("got %q, want %q", got, want)
		}
	})
	t.Run("only open editors", func(t *testing.T) {
		o := defaults(dir)
		o.Unsaved = unsaved
		o.OnlyPaths = []string{filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt"), filepath.Join(dir, "new.txt")}
		got, _ := found(t, Query{Pattern: "needle"}, o)
		want := []string{"a.txt:1:6:needle", "b.txt:0:5:needle", "new.txt:0:0:needle"}
		if !equal(got, want) {
			t.Errorf("got %q, want %q", got, want)
		}
		o.Include = ParsePatterns("a.txt", dir)
		got, _ = found(t, Query{Pattern: "needle"}, o)
		if want := []string{"a.txt:1:6:needle"}; !equal(got, want) {
			t.Errorf("with an include pattern: got %q, want %q", got, want)
		}
	})
	t.Run("the file list of the index", func(t *testing.T) {
		o := defaults(dir)
		o.Files = []string{"b.txt", "c.txt"}
		got, _ := found(t, Query{Pattern: "needle"}, o)
		if want := []string{"b.txt:0:5:needle"}; !equal(got, want) {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}

func TestLimitAndSummary(t *testing.T) {
	files := map[string]string{}
	for i := range 30 {
		files["f"+string(rune('a'+i%26))+string(rune('a'+i/26))+".txt"] = strings.Repeat("needle\n", 10)
	}
	dir := tree(t, files)
	t.Run("the limit stops the search", func(t *testing.T) {
		o := defaults(dir)
		o.MaxResults = 25
		n := 0
		sum, err := Run(context.Background(), Query{Pattern: "needle"}, o, func(r FileResult) { n += len(r.Matches) })
		if err != nil {
			t.Fatal(err)
		}
		if n != 25 || sum.Matches != 25 || !sum.LimitHit {
			t.Errorf("matches %d, summary %+v", n, sum)
		}
	})
	t.Run("all of it", func(t *testing.T) {
		n := 0
		sum, err := Run(context.Background(), Query{Pattern: "needle"}, defaults(dir), func(r FileResult) { n += len(r.Matches) })
		if err != nil {
			t.Fatal(err)
		}
		if n != 300 || sum.Matches != 300 || sum.Files != 30 || sum.LimitHit || sum.Searched != 30 {
			t.Errorf("matches %d, summary %+v", n, sum)
		}
	})
	t.Run("a file that is too large", func(t *testing.T) {
		o := defaults(dir)
		o.MaxFileSize = 10
		sum, err := Run(context.Background(), Query{Pattern: "needle"}, o, func(FileResult) {})
		if err != nil {
			t.Fatal(err)
		}
		if sum.TooLarge != 30 || sum.Matches != 0 {
			t.Errorf("summary %+v", sum)
		}
	})
}

func TestCallbacksAreNotConcurrent(t *testing.T) {
	files := map[string]string{}
	for i := range 200 {
		files[filepath.Join("d", string(rune('a'+i%26)), "f"+string(rune('a'+i/26))+".txt")] = "needle\n"
	}
	dir := tree(t, files)
	var inside, overlaps atomic.Int32
	_, err := Run(context.Background(), Query{Pattern: "needle"}, defaults(dir), func(FileResult) {
		if inside.Add(1) != 1 {
			overlaps.Add(1)
		}
		inside.Add(-1)
	})
	if err != nil {
		t.Fatal(err)
	}
	if overlaps.Load() != 0 {
		t.Errorf("onFile ran at the same time %d times", overlaps.Load())
	}
}

func TestCancellation(t *testing.T) {
	files := map[string]string{}
	for i := range 400 {
		files[filepath.Join("d", string(rune('a'+i%26)), "f"+string(rune('a'+i/26))+".txt")] = strings.Repeat("filler line\n", 50) + "needle\n"
	}
	dir := tree(t, files)
	before := runtimeGoroutines()
	ctx, cancel := context.WithCancel(context.Background())
	n := 0
	sum, err := Run(ctx, Query{Pattern: "needle"}, defaults(dir), func(FileResult) {
		n++
		if n == 5 {
			cancel()
		}
	})
	cancel()
	appErr := asError(t, err)
	if !appErr.HasRoute("Run.SearchCancelled") {
		t.Errorf("route: %s", appErr.Route())
	}
	if sum.Files >= 400 {
		t.Errorf("the search went on after the cancel: %+v", sum)
	}
	waitGoroutines(t, before)

	// a context that is done before the search starts
	ctx2, cancel2 := context.WithCancel(context.Background())
	cancel2()
	if _, err := Run(ctx2, Query{Pattern: "needle"}, defaults(dir), func(FileResult) {}); err == nil {
		t.Error("a cancelled context should give an error")
	}
	waitGoroutines(t, before)
}
