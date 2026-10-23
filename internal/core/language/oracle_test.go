package language

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/regex"
	"github.com/rm4n0s/emetos/internal/core/textmate"
)

// colorizeToken is a token of colorize-results: its text and its scopes. The results carry the
// colors of the themes too, which are the business of the theme step (4.7).
type colorizeToken struct {
	C string `json:"c"`
	T string `json:"t"`
}

// expectedDifference is a range of lines of a fixture where Emetos is known to tokenize
// differently from VS Code.
type expectedDifference struct {
	from, to int
	reason   string
	seen     bool
}

// lineBreaks are the line breaks that VS Code's splitLines splits at.
var lineBreaks = regexp.MustCompile(`\r\n|\r|\n`)

// oracleFolder is where the VS Code oracle of colorize-tests is, from this package.
func oracleFolder(t *testing.T) string {
	t.Helper()
	dir := filepath.Join("..", "..", "..", "reference", "vscode", "extensions", "vscode-colorize-tests", "test")
	if _, err := os.Stat(filepath.Join(dir, "colorize-fixtures")); err != nil {
		t.Skip("reference/ is missing: run scripts/fetch-reference.sh")
	}
	return dir
}

// readExpectedDifferences reads testdata/expected-differences.txt: one range per line,
// "<fixture> <first line>-<last line> <reason>" (or "<fixture> <line> <reason>"), with the lines
// counted from 1 in the fixture. Blank lines and lines that start with # are skipped.
func readExpectedDifferences(t *testing.T) map[string][]*expectedDifference {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "expected-differences.txt"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]*expectedDifference{}
	for i, text := range strings.Split(string(data), "\n") {
		n := i + 1
		line := strings.TrimSpace(text)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.SplitN(line, " ", 3)
		if len(fields) < 3 || strings.TrimSpace(fields[2]) == "" {
			t.Fatalf("expected-differences.txt:%d: want <fixture> <lines> <reason>", n)
		}
		first, last, isRange := strings.Cut(fields[1], "-")
		if !isRange {
			last = first
		}
		from, err1 := strconv.Atoi(first)
		to, err2 := strconv.Atoi(last)
		if err1 != nil || err2 != nil || from < 1 || to < from {
			t.Fatalf("expected-differences.txt:%d: %q is not a range of lines", n, fields[1])
		}
		out[fields[0]] = append(out[fields[0]], &expectedDifference{from: from, to: to, reason: strings.TrimSpace(fields[2])})
	}
	return out
}

// oracleLanguages are the languages whose fixtures the oracle checks.
var oracleLanguages = map[string]bool{"go": true, "json": true, "jsonc": true, "jsonl": true, "snippets": true, "yaml": true, "markdown": true, "dockercompose": true}

// assignToLines gives the expected tokens to the lines of the fixture they are on: the tokens of
// a line are the ones that spell it, and an empty line has none.
func assignToLines(t *testing.T, lines []string, expected []colorizeToken) [][]colorizeToken {
	t.Helper()
	out := make([][]colorizeToken, len(lines))
	next := 0
	for i, line := range lines {
		got := ""
		for got != line {
			if next >= len(expected) || len(got) >= len(line) {
				t.Fatalf("the tokens of the results do not spell line %d: %q", i+1, line)
			}
			out[i] = append(out[i], expected[next])
			got += expected[next].C
			next++
		}
	}
	if next != len(expected) {
		t.Fatalf("%d tokens of the results are left after the last line", len(expected)-next)
	}
	return out
}

// tokenizeLikeVSCode tokenizes the lines as the Snapper of VS Code does: neighbouring tokens of
// the same scopes in a line become one, and tokens with no text are dropped.
func tokenizeLikeVSCode(g *textmate.Grammar, lines []string) [][]colorizeToken {
	out := make([][]colorizeToken, len(lines))
	var state *textmate.State
	for i, line := range lines {
		res := g.TokenizeLine(line, state, 0)
		state = res.State
		last := ""
		for j, tok := range res.Tokens {
			text := line[tok.Start:tok.End]
			scopes := strings.Join(tok.Scopes.Names(), " ")
			if j > 0 && scopes == last {
				out[i][len(out[i])-1].C += text
				continue
			}
			last = scopes
			out[i] = append(out[i], colorizeToken{C: text, T: scopes})
		}
		kept := out[i][:0]
		for _, tok := range out[i] {
			if tok.C != "" {
				kept = append(kept, tok)
			}
		}
		out[i] = kept
	}
	return out
}

func sameTokens(a, b []colorizeToken) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func describeTokens(tokens []colorizeToken) string {
	var sb strings.Builder
	for _, tok := range tokens {
		sb.WriteString("\n      " + strconv.Quote(tok.C) + " " + tok.T)
	}
	return sb.String()
}

// TestColorizeOracle tokenizes the fixtures of VS Code's colorize tests for the bundled
// languages and compares every token's text and scopes with colorize-results.
func TestColorizeOracle(t *testing.T) {
	dir := oracleFolder(t)
	expectedDifferences := readExpectedDifferences(t)
	r, err := Bundled()
	if err != nil {
		t.Fatal(err)
	}
	cache := regex.NewCache(1 << 20)
	names, err := os.ReadDir(filepath.Join(dir, "colorize-fixtures"))
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, entry := range names {
		fixture := entry.Name()
		content, err := os.ReadFile(filepath.Join(dir, "colorize-fixtures", fixture))
		if err != nil {
			t.Fatal(err)
		}
		lines := lineBreaks.Split(string(content), -1)
		lang := r.Detect(fixture, lines[0])
		if !oracleLanguages[lang] {
			continue
		}
		checked++
		t.Run(fixture, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(dir, "colorize-results", strings.Replace(fixture, ".", "_", 1)+".json"))
			if err != nil {
				t.Fatal(err)
			}
			var expected []colorizeToken
			if err := json.Unmarshal(data, &expected); err != nil {
				t.Fatal(err)
			}
			g, err := r.NewGrammar(lang, cache)
			if err != nil {
				t.Fatal(err)
			}
			want := assignToLines(t, lines, expected)
			got := tokenizeLikeVSCode(g, lines)
			known := expectedDifferences[fixture]
			for i := range lines {
				differs := !sameTokens(got[i], want[i])
				var entry *expectedDifference
				for _, e := range known {
					if i+1 >= e.from && i+1 <= e.to {
						entry = e
					}
				}
				switch {
				case differs && entry == nil:
					t.Errorf("line %d %q:\n    got:%s\n    want:%s", i+1, lines[i], describeTokens(got[i]), describeTokens(want[i]))
				case differs:
					entry.seen = true
				case entry != nil && i+1 == entry.to && !entry.seen:
					t.Errorf("lines %d-%d are listed as different (%s) but none is", entry.from, entry.to, entry.reason)
				}
			}
		})
	}
	if checked == 0 {
		t.Fatal("no fixture was checked")
	}
	var unknown []string
	for fixture := range expectedDifferences {
		if _, err := os.Stat(filepath.Join(dir, "colorize-fixtures", fixture)); err != nil {
			unknown = append(unknown, fixture)
		}
	}
	sort.Strings(unknown)
	for _, fixture := range unknown {
		t.Errorf("expected-differences.txt names %s, which is not a fixture", fixture)
	}
}
