package diff

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixturesDir holds VS Code's diff fixtures (reference/ is created by
// scripts/fetch-reference.sh and is not committed).
const fixturesDir = "../../../reference/vscode/src/vs/editor/test/node/diffing/fixtures"

// refinedFixtures are the fixtures whose expected line ranges are changed by
// the refinement to character changes: the character diff finds unchanged
// lines inside a block (class-replacement, shifting-twice, ts-confusing-2,
// ts-example1) or trims a line off its end (noisy-move1). The line diff alone
// (ComputeLines) must differ from the expectation there.
var refinedFixtures = map[string]bool{
	"class-replacement": true,
	"noisy-move1":       true,
	"shifting-twice":    true,
	"ts-confusing-2":    true,
	"ts-example1":       true,
}

// TestVSCodeFixtureLineRanges compares the line ranges of the lines diff
// (ComputeLines) with the "advanced" expectations of VS Code's fixtures. The
// expected ranges come from the whole pipeline, so the fixtures that the
// character refinement changes are only checked for validity.
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

// expectedFixture is advanced.expected.diff.json (the moves are not read: moved
// code is not detected, SPEC section 2).
type expectedFixture struct {
	Diffs []struct {
		OriginalRange string `json:"originalRange"`
		ModifiedRange string `json:"modifiedRange"`
		InnerChanges  []struct {
			OriginalRange string `json:"originalRange"`
			ModifiedRange string `json:"modifiedRange"`
		} `json:"innerChanges"`
	} `json:"diffs"`
}

// formatVSCodeRange writes a character range as VS Code's fixture test does,
// "[1,18 -> 1,19]" with " EOL" when the range ends at the end of its line,
// with UTF-16 columns (the columns of Range are bytes).
func formatVSCodeRange(r Range, lines []string) string {
	col := func(p Position) int {
		line := lines[p.Line-1]
		return utf16Len(line[:p.Column-1]) + 1
	}
	eol := ""
	if r.End.Column == len(lines[r.End.Line-1])+1 {
		eol = " EOL"
	}
	return fmt.Sprintf("[%d,%d -> %d,%d%s]", r.Start.Line, col(r.Start), r.End.Line, col(r.End), eol)
}

// TestVSCodeFixtures compares the whole result of Compute (the line blocks and
// their character changes) with the "advanced" expectations of VS Code's
// fixtures, as fixtures.test.ts builds them. The fixtures that ignore white
// space are the ones with "trimws" in their name. The moves of the
// expectations are left out.
func TestVSCodeFixtures(t *testing.T) {
	dirs, err := os.ReadDir(fixturesDir)
	if err != nil {
		t.Skipf("VS Code fixtures are not available (run scripts/fetch-reference.sh): %v", err)
	}
	compared := 0
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		compared++
		t.Run(d.Name(), func(t *testing.T) {
			dir := filepath.Join(fixturesDir, d.Name())
			original, modified := readFixtureTexts(t, dir)
			b, err := os.ReadFile(filepath.Join(dir, "advanced.expected.diff.json"))
			if err != nil {
				t.Fatal(err)
			}
			var want expectedFixture
			if err := json.Unmarshal(b, &want); err != nil {
				t.Fatal(err)
			}

			ignoreWS := strings.Contains(d.Name(), "trimws")
			got, err := Compute(context.Background(), original, modified, Options{IgnoreTrimWhitespace: ignoreWS})
			if err != nil {
				t.Fatalf("Compute: %v", err)
			}
			if got.HitTimeout {
				t.Error("HitTimeout is set")
			}
			if err := Validate(got.Changes, len(original), len(modified)); err != nil {
				t.Errorf("Validate: %v", err)
			}
			checkInner(t, got.Changes, original, modified)
			if !ignoreWS {
				// VS Code asserts that the character changes rebuild the text.
				if rebuilt := applyInner(original, modified, got.Changes); rebuilt != strings.Join(modified, "\n") {
					t.Error("the character changes do not rebuild the modified text")
				}
			}

			if len(got.Changes) != len(want.Diffs) {
				t.Fatalf("%d changes, want %d:\n%s", len(got.Changes), len(want.Diffs), format(got.Changes))
			}
			for i, c := range got.Changes {
				w := want.Diffs[i]
				if o := fmt.Sprintf("[%d,%d)", c.Original.StartLine, c.Original.EndLine); o != w.OriginalRange {
					t.Errorf("change %d: original %s, want %s", i, o, w.OriginalRange)
				}
				if m := fmt.Sprintf("[%d,%d)", c.Modified.StartLine, c.Modified.EndLine); m != w.ModifiedRange {
					t.Errorf("change %d: modified %s, want %s", i, m, w.ModifiedRange)
				}
				if len(c.Inner) != len(w.InnerChanges) {
					t.Errorf("change %d: %d character changes, want %d", i, len(c.Inner), len(w.InnerChanges))
					continue
				}
				for j, in := range c.Inner {
					if o := formatVSCodeRange(in.Original, original); o != w.InnerChanges[j].OriginalRange {
						t.Errorf("change %d, inner %d: original %s, want %s", i, j, o, w.InnerChanges[j].OriginalRange)
					}
					if m := formatVSCodeRange(in.Modified, modified); m != w.InnerChanges[j].ModifiedRange {
						t.Errorf("change %d, inner %d: modified %s, want %s", i, j, m, w.InnerChanges[j].ModifiedRange)
					}
				}
			}
		})
	}
	if compared < 50 {
		t.Errorf("only %d fixtures were compared", compared)
	}
}
