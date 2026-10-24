package diff

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixturesDir holds VS Code's diff fixtures (reference/ is created by
// scripts/fetch-reference.sh and is not committed).
const fixturesDir = "../../../reference/vscode/src/vs/editor/test/node/diffing/fixtures"

// refinedFixtures are the fixtures whose expected line ranges are changed by
// the refinement to character changes, which is step 9.2: the character diff
// finds unchanged lines inside a block (class-replacement, shifting-twice,
// ts-confusing-2, ts-example1) or trims a line off its end (noisy-move1).
// The line diff alone must differ from the expectation there.
var refinedFixtures = map[string]bool{
	"class-replacement": true,
	"noisy-move1":       true,
	"shifting-twice":    true,
	"ts-confusing-2":    true,
	"ts-example1":       true,
}

// TestVSCodeFixtureLineRanges compares the line ranges of the lines diff with
// the "advanced" expectations of VS Code's fixtures. The expected ranges come
// from the whole pipeline, so the fixtures that the character refinement
// changes are only checked for validity.
func TestVSCodeFixtureLineRanges(t *testing.T) {
	dirs, err := os.ReadDir(fixturesDir)
	if err != nil {
		t.Skipf("VS Code fixtures are not available (run scripts/fetch-reference.sh): %v", err)
	}
	compared := 0
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		t.Run(d.Name(), func(t *testing.T) {
			dir := filepath.Join(fixturesDir, d.Name())
			original, modified := readFixtureTexts(t, dir)
			want := readExpectedRanges(t, dir)

			got, err := ComputeLines(context.Background(), original, modified,
				Options{IgnoreTrimWhitespace: strings.Contains(d.Name(), "trimws")})
			if err != nil {
				t.Fatalf("ComputeLines: %v", err)
			}
			if err := Validate(got.Changes, len(original), len(modified)); err != nil {
				t.Errorf("Validate: %v", err)
			}
			if !strings.Contains(d.Name(), "trimws") {
				if applied := Apply(original, modified, got.Changes); !equalLines(applied, modified) {
					t.Error("Apply does not give the modified text")
				}
			}
			if refinedFixtures[d.Name()] {
				return
			}
			compared++
			if f := format(got.Changes); f != want {
				t.Errorf("changes = %s\nwant      %s", f, want)
			}
		})
	}
	if compared < 50 {
		t.Errorf("only %d fixtures were compared", compared)
	}
}

// readFixtureTexts reads 1.* and 2.* of a fixture folder as lines, as VS
// Code's fixture test does (all line breaks become \n).
func readFixtureTexts(t *testing.T, dir string) (original, modified []string) {
	t.Helper()
	read := func(prefix string) []string {
		files, err := filepath.Glob(filepath.Join(dir, prefix+".*"))
		if err != nil || len(files) == 0 {
			t.Fatalf("no %s.* in %s: %v", prefix, dir, err)
		}
		b, err := os.ReadFile(files[0])
		if err != nil {
			t.Fatal(err)
		}
		s := strings.ReplaceAll(strings.ReplaceAll(string(b), "\r\n", "\n"), "\r", "\n")
		return strings.Split(s, "\n")
	}
	return read("1"), read("2")
}

// readExpectedRanges reads the line ranges of advanced.expected.diff.json in
// the format of format.
func readExpectedRanges(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "advanced.expected.diff.json"))
	if err != nil {
		t.Fatal(err)
	}
	var expected struct {
		Diffs []struct {
			OriginalRange string `json:"originalRange"`
			ModifiedRange string `json:"modifiedRange"`
		} `json:"diffs"`
	}
	if err := json.Unmarshal(b, &expected); err != nil {
		t.Fatal(err)
	}
	parts := make([]string, len(expected.Diffs))
	for i, d := range expected.Diffs {
		parts[i] = d.OriginalRange + "->" + d.ModifiedRange
	}
	return strings.Join(parts, " ")
}
