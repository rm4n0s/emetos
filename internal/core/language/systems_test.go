package language

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/regex"
	"github.com/rm4n0s/emetos/internal/core/textmate"
)

// scopedText tokenizes text with the grammar of a bundled language, line by line, and returns for
// each token its text and its scopes (one string, the scopes separated by spaces).
func scopedText(t *testing.T, lang, text string) []colorizeToken {
	t.Helper()
	r, err := Bundled()
	if err != nil {
		t.Fatal(err)
	}
	g, err := r.NewGrammarWith(lang, GrammarSettings{Cache: regex.NewCache(1 << 16), ScanTimeout: testScanTimeout})
	if err != nil {
		t.Fatal(err)
	}
	var out []colorizeToken
	var state *textmate.State
	for _, line := range strings.Split(text, "\n") {
		res := g.TokenizeLine(line, state, 0)
		state = res.State
		if res.StoppedEarly {
			t.Errorf("%s: the line %q was not tokenized to its end", lang, line)
		}
		for _, tok := range res.Tokens {
			out = append(out, colorizeToken{C: line[tok.Start:tok.End], T: strings.Join(tok.Scopes.Names(), " ")})
		}
	}
	return out
}

// TestSystemsLanguagesHaveTheScopesOfTheirTokens checks the languages that VS Code's colorize
// fixtures do not cover (Go module files, Git rebase and Ignore files) on small fixtures of Emetos's own:
// each case names a token and a scope that its scopes must have.
func TestSystemsLanguagesHaveTheScopesOfTheirTokens(t *testing.T) {
	tests := []struct {
		name, lang, text string
		want             []colorizeToken // the text of a token and a scope in its scopes
	}{
		{"go.mod", "go.mod", "module example.com/m\n\ngo 1.22\n\nrequire (\n\tgithub.com/a/b v1.2.3 // indirect\n)\n", []colorizeToken{
			{"module", "keyword.go.mod"},
			{"example.com/m", "string.unquoted.go.mod"},
			{"1.22", "string.unquoted.go.mod"},
			{"require", "keyword.go.mod"},
			{"v1.2.3", "constant.language.go.mod"},
			{"indirect", "comment.line.double-slash.go.mod"},
		}},
		{"go.mod replace", "go.mod", "replace a/b => ../b\nexclude a/c v0.1.0\n", []colorizeToken{
			{"replace", "keyword.go.mod"},
			{"=>", "operator.go.mod"},
			{"exclude", "keyword.go.mod"},
		}},
		{"go.work uses the go.mod grammar", "go.work", "go 1.22\n\nuse ./a\n", []colorizeToken{
			{"go", "keyword.go.mod"},
			{"1.22", "string.unquoted.go.mod"},
		}},
		{"go.sum", "go.sum", "github.com/a/b v1.2.3 h1:" + strings.Repeat("A", 43) + "=\ngithub.com/a/b v1.2.3/go.mod h1:" + strings.Repeat("b", 43) + "=\n", []colorizeToken{
			{"v1.2.3", "constant.language.go.sum"},
			{strings.Repeat("A", 43), "string.unquoted.go.sum"},
			{strings.Repeat("b", 43), "string.unquoted.go.sum"},
		}},
		{"git rebase", "git-rebase", "pick 1fc6c95 Patch A\nexec make test\nbreak\n# a comment\n", []colorizeToken{
			{"pick", "support.function.git-rebase"},
			{"1fc6c95", "constant.sha.git-rebase"},
			{"Patch A", "meta.commit-message.git-rebase"},
			{"exec", "support.function.git-rebase"},
			{"break", "support.function.git-rebase"},
			{"a comment", "comment.line.number-sign.git-rebase"},
		}},
		{"ignore", "ignore", "# build output\n/dist/\n!keep.txt\n*.log\n", []colorizeToken{
			{"# build output", "comment.line.number-sign.ignore"},
			{"!keep.txt", "source.ignore"}, // the grammar scopes comments only
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := scopedText(t, tc.lang, tc.text)
			for _, w := range tc.want {
				found := false
				for _, g := range got {
					if strings.Contains(g.C, w.C) && strings.Contains(" "+g.T+" ", " "+w.T+" ") {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("no token with %q has the scope %s:%s", w.C, w.T, describeTokens(got))
				}
			}
		})
	}
}

// TestCppIsTokenizedInTime is the timing test of the heaviest grammar (SPEC §8.1): every C and C++
// fixture of VS Code tokenizes within a time that an editor can bear, and the line that made the
// whitespace pattern of the grammar exponential (a string with many "*") does not time out.
func TestCppIsTokenizedInTime(t *testing.T) {
	const timingRuns = 5
	r, err := Bundled()
	if err != nil {
		t.Fatal(err)
	}
	cache := regex.NewCache(1 << 20)
	tokenize := func(t *testing.T, lang string, lines []string) (time.Duration, int) {
		t.Helper()
		g, err := r.NewGrammarWith(lang, GrammarSettings{Cache: cache, ScanTimeout: testScanTimeout})
		if err != nil {
			t.Fatal(err)
		}
		// the CPU time of the thread, not the time on the clock: the tests of other packages share the machine, and the time
		// this one waits for a CPU says nothing about the grammar
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		start := threadCPU(t)
		var state *textmate.State
		stopped := 0
		for _, line := range lines {
			res := g.TokenizeLine(line, state, 0)
			state = res.State
			if res.StoppedEarly {
				stopped++
			}
		}
		return threadCPU(t) - start, stopped
	}
	t.Run("a string of stars", func(t *testing.T) {
		lines := []string{"int main() {", `  if (t > 0) puts("\n` + strings.Repeat("*", 200) + `");`, "}"}
		d, stopped := tokenize(t, "cpp", lines)
		if stopped != 0 || (!raceEnabled && d > time.Second) {
			t.Errorf("%d lines stopped early, %v", stopped, d)
		}
	})
	t.Run("the fixtures", func(t *testing.T) {
		dir := oracleFolder(t)
		for _, name := range []string{"test.c", "test.cc", "test.cpp", "test.cu", "test-23630.cpp", "test-23850.cpp", "test-78769.cpp", "test-80644.cpp"} {
			content, err := os.ReadFile(filepath.Join(dir, "colorize-fixtures", name))
			if err != nil {
				t.Fatal(err)
			}
			lines := lineBreaks.Split(string(content), -1)
			// the best of a few runs: a busy machine still slows a run down (caches, frequency), a slow grammar slows every one
			d, stopped := tokenize(t, r.Detect(name, lines[0]), lines)
			for range timingRuns - 1 {
				if d/time.Duration(len(lines)) <= 20*time.Millisecond && stopped == 0 {
					break
				}
				if again, s := tokenize(t, r.Detect(name, lines[0]), lines); again < d {
					d, stopped = again, s
				}
			}
			if stopped != 0 {
				t.Errorf("%s: %d lines stopped early", name, stopped)
			}
			if per := d / time.Duration(len(lines)); !raceEnabled && per > 20*time.Millisecond {
				t.Errorf("%s: %v per line over %d lines", name, per, len(lines))
			}
		}
	})
}
