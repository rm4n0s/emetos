package textmate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// firstMateRegistry is a registry over grammars read from files.
type firstMateRegistry map[string]*RawGrammar

// Grammar implements Registry.
func (r firstMateRegistry) Grammar(scopeName string) *RawGrammar { return r[scopeName] }

// referenceDir is the folder of the vscode-textmate references.
func referenceDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join("..", "..", "..", "reference", "vscode-textmate", "test-cases")
	if _, err := os.Stat(dir); err != nil {
		t.Skip("reference/ is missing: run scripts/fetch-reference.sh")
	}
	return dir
}

type testToken struct {
	Value  string   `json:"value"`
	Scopes []string `json:"scopes"`
}

type testLine struct {
	Line   string      `json:"line"`
	Tokens []testToken `json:"tokens"`
}

type testCase struct {
	Desc              string     `json:"desc"`
	Grammars          []string   `json:"grammars"`
	GrammarPath       string     `json:"grammarPath"`
	GrammarScopeName  string     `json:"grammarScopeName"`
	GrammarInjections []string   `json:"grammarInjections"`
	Lines             []testLine `json:"lines"`
}

// notYet are the test cases of vscode-textmate that need features of step 4.4, with the feature.
var notYet = map[string]string{
	"first-mate:TEST #42": "injections (the PHP grammar injects its embedded code)",
	"first-mate:TEST #45": "injections (the PHP grammar injects its embedded code)",
	"first-mate:TEST #47": "injections given by the registry (grammarInjections)",
	"first-mate:TEST #49": "injections given by the registry (grammarInjections)",
	"first-mate:TEST #64": "injections (the ERB grammar injects its tags into HTML)",
}

// runSuite runs the test cases of a tests.json of vscode-textmate: grammars are loaded from the
// files the test lists, and the lines are tokenized one after the other.
func runSuite(t *testing.T, file string) (ran, skipped int) {
	t.Helper()
	base := filepath.Join(referenceDir(t), file)
	data, err := os.ReadFile(filepath.Join(base, "tests.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []testCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		if reason, ok := notYet[file+":"+tc.Desc]; ok {
			t.Logf("%s %s: skipped, %s", file, tc.Desc, reason)
			skipped++
			continue
		}
		ran++
		t.Run(file+" "+tc.Desc, func(t *testing.T) {
			reg := firstMateRegistry{}
			scope := tc.GrammarScopeName
			for _, path := range tc.Grammars {
				content, err := os.ReadFile(filepath.Join(base, path))
				if err != nil {
					t.Fatal(err)
				}
				raw, err := ParseGrammar(content)
				if err != nil {
					t.Fatalf("%s: %v", path, err)
				}
				reg[raw.ScopeName] = raw
				if scope == "" && path == tc.GrammarPath {
					scope = raw.ScopeName
				}
			}
			g, err := NewGrammar(scope, reg, GrammarOptions{})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				for _, p := range g.Problems() {
					t.Logf("problem: %v", p)
				}
			})
			var state *State
			for _, tl := range tc.Lines {
				res := g.TokenizeLine(tl.Line, state, 0)
				var got []testToken
				for _, tok := range res.Tokens {
					got = append(got, testToken{Value: tl.Line[tok.Start:tok.End], Scopes: tok.Scopes.Names()})
				}
				want := tl.Tokens
				if len(tl.Line) > 0 {
					// The tests have empty tokens that vscode-textmate drops.
					want = nil
					for _, tok := range tl.Tokens {
						if tok.Value != "" {
							want = append(want, tok)
						}
					}
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("line %q\n got  %v\n want %v", tl.Line, got, want)
				}
				state = res.State
			}
		})
	}
	return ran, skipped
}

func TestFirstMateSuite(t *testing.T) {
	ran, skipped := runSuite(t, "first-mate")
	t.Logf("first-mate: %d run, %d skipped", ran, skipped)
	if ran+skipped != 64 {
		t.Errorf("the suite has %d cases, want 64", ran+skipped)
	}
}
