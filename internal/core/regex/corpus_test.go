package regex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

// grammarPatterns reads the regular expressions of the grammars of the reference folder: the
// strings of every match, begin, end and while. The map is from the pattern to the
// grammars that have it.
func grammarPatterns(t *testing.T) map[patternUse][]string {
	t.Helper()
	root := filepath.Join("..", "..", "..", "reference")
	if _, err := os.Stat(filepath.Join(root, "vscode", "extensions")); err != nil {
		t.Skip("reference/ is missing: run scripts/fetch-reference.sh")
	}
	files, err := filepath.Glob(filepath.Join(root, "vscode", "extensions", "*", "syntaxes", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	more, err := filepath.Glob(filepath.Join(root, "vscode-go", "extension", "syntaxes", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, more...)
	if len(files) < 50 {
		t.Fatalf("only %d grammar files found", len(files))
	}
	out := map[patternUse][]string{}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var doc any
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		name := filepath.Base(f)
		var walk func(v any)
		walk = func(v any) {
			switch v := v.(type) {
			case map[string]any:
				for k, x := range v {
					if s, ok := x.(string); ok && (k == "match" || k == "begin" || k == "end" || k == "while") {
						out[patternUse{s, k == "end" || k == "while"}] = append(out[patternUse{s, k == "end" || k == "while"}], name)
						continue
					}
					walk(x)
				}
			case []any:
				for _, x := range v {
					walk(x)
				}
			}
		}
		walk(doc)
	}
	return out
}

// patternUse is a pattern of a grammar. A pattern of an end or while is written with references
// to the captures of its begin, which the tokenizer replaces by the text captured.
type patternUse struct {
	pattern string
	isEnd   bool
}

var backReference = regexp.MustCompile(`\\(\d+)`)

// unsupported are the patterns of the grammars that cannot be translated, with the reason.
var unsupported = map[string]string{}

func TestEveryPatternOfTheGrammarsCompiles(t *testing.T) {
	patterns := grammarPatterns(t)
	cache := NewCache(1 << 20)
	var failures []string
	var slowest time.Duration
	slowPattern := ""
	for use, grammars := range patterns {
		pattern := use.pattern
		if use.isEnd {
			pattern = backReference.ReplaceAllString(pattern, "x")
		}
		start := time.Now()
		_, err := cache.Compile(pattern, Options{})
		if d := time.Since(start); d > slowest {
			slowest, slowPattern = d, pattern
		}
		if _, known := unsupported[use.pattern]; known {
			if err == nil {
				t.Errorf("%s is listed as unsupported but compiles", clip(use.pattern))
			}
			continue
		}
		if err == nil {
			continue
		}
		sort.Strings(grammars)
		failures = append(failures, grammars[0]+": "+clip(use.pattern)+"\n    "+clip(err.Error()))
	}
	t.Logf("%d patterns, slowest compile %v (%s)", len(patterns), slowest, clip(strings.ReplaceAll(slowPattern, "\n", " ")))
	sort.Strings(failures)
	for _, f := range failures {
		t.Error(f)
	}
}
