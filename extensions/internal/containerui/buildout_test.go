package containerui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestParseBuildOutput holds what the output of a build says about the Dockerfile: the position blocks of BuildKit, the parse errors
// of both builders, its checks as warnings, and no problem for output that has no position.
func TestParseBuildOutput(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want []buildProblem
	}{
		{"a failed instruction", `#5 [2/3] RUN nonexistent
#5 0.200 /bin/sh: nonexistent: not found
Dockerfile:5
--------------------
   3 |     WORKDIR /app
   4 |
   5 | >>> RUN nonexistent
   6 |
--------------------
ERROR: failed to solve: process "/bin/sh -c nonexistent" did not complete successfully: exit code: 127`,
			[]buildProblem{{file: "Dockerfile", line: 5, message: `process "/bin/sh -c nonexistent" did not complete successfully: exit code: 127`}}},
		{"a parse error with its block and the line after it", `Dockerfile:3
--------------------
   1 |     FROM busybox
   3 | >>> FOO bar
--------------------
ERROR: failed to solve: dockerfile parse error on line 3: unknown instruction: FOO
`, []buildProblem{{file: "Dockerfile", line: 3, message: "unknown instruction: FOO"}}},
		{"a parse error of the older BuildKit", "ERROR: failed to solve: dockerfile parse error on line 7: unknown instruction: BAD\n",
			[]buildProblem{{line: 7, message: "unknown instruction: BAD"}}},
		{"a parse error of the classic builder", "Error response from daemon: dockerfile parse error line 2: unknown instruction: X\n",
			[]buildProblem{{line: 2, message: "unknown instruction: X"}}},
		{"a file in a folder", "sub/Dockerfile.dev:12\n--------------------\n  12 | >>> COPY nothing /\n--------------------\nERROR: failed to solve: failed to compute cache key: \"/nothing\" not found\n",
			[]buildProblem{{file: "sub/Dockerfile.dev", line: 12, message: `failed to compute cache key: "/nothing" not found`}}},
		{"the checks of BuildKit are warnings", `1 warning found (use docker --debug to expand):
 - FromAsCasing: 'as' and 'FROM' keywords' casing do not match (line 1)
 - LegacyKeyValueFormat: "ENV key=value" should be used instead of legacy "ENV key value" format (line 4)
`, []buildProblem{
			{line: 1, message: "FromAsCasing: 'as' and 'FROM' keywords' casing do not match", warning: true},
			{line: 4, message: `LegacyKeyValueFormat: "ENV key=value" should be used instead of legacy "ENV key value" format`, warning: true},
		}},
		{"Buildah: a failed instruction has no line", "STEP 1/2: FROM busybox\nSTEP 2/2: RUN false\nError: building at STEP \"RUN false\": while running runtime: exit status 1\n",
			[]buildProblem{{step: "RUN false", message: `the step "RUN false" failed: while running runtime: exit status 1`}}},
		{"Podman: a parse error of the Containerfile", "Error: parsing Containerfile: dockerfile parse error on line 4: unknown instruction: FRM\n",
			[]buildProblem{{line: 4, message: "unknown instruction: FRM"}}},
		{"an error with no position is no problem", "ERROR: failed to solve: failed to read dockerfile: open Dockerfile: no such file or directory\n", nil},
		{"a time and a path are not a position", "12:30\nsome text\nDone: 5\n", nil},
		{"a position with no block after it", "main.go:12\nsomething else\n", nil},
		{"no output", "", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseBuildOutput(strings.Split(tc.out, "\n"))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

// TestResolveBuildFile finds the file a build message names next to the Dockerfile, in the context and in the folder, in that order.
func TestResolveBuildFile(t *testing.T) {
	dir := t.TempDir()
	write := func(rel string) string {
		p := dir + "/" + rel
		mustWrite(t, p, "x")
		return p
	}
	dockerfile := write("app/Dockerfile")
	other := write("app/Dockerfile.dev")
	rootFile := write("Dockerfile.root")
	tests := []struct {
		name, file, want string
	}{
		{"next to the Dockerfile", "Dockerfile.dev", other},
		{"in the folder", "Dockerfile.root", rootFile},
		{"an absolute path", other, other},
		{"a file that is not there is the Dockerfile", "missing", dockerfile},
	}
	for _, tc := range tests {
		if got := resolveBuildFile(tc.file, dockerfile, dir+"/app", dir); got != tc.want {
			t.Errorf("%s: %s, want %s", tc.name, got, tc.want)
		}
	}
}

// TestLineEndColumn is where a marker ends: after the text of the line, counted in UTF-16 units.
func TestLineEndColumn(t *testing.T) {
	p := t.TempDir() + "/Dockerfile"
	mustWrite(t, p, "FROM busybox   \r\n\r\nRUN echo \U0001F600\n")
	for line, want := range map[int]int{1: 13, 2: 2, 3: 12, 4: 2, 0: 2} {
		if got := lineEndColumn(p, line); got != want {
			t.Errorf("line %d: %d, want %d", line, got, want)
		}
	}
	if got := lineEndColumn(p+"x", 1); got != 2 {
		t.Errorf("a file that is not there: %d", got)
	}
}

// TestLineOfInstruction finds the line of the instruction a builder named, in the way a Dockerfile can be written.
func TestLineOfInstruction(t *testing.T) {
	file := filepath.Join(t.TempDir(), "Containerfile")
	text := "# a comment\nFROM busybox\n\nRUN echo  one \\\n    && echo two\nCOPY a   b /\nRUN false\nRUN false\n"
	if err := os.WriteFile(file, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		step string
		want int
	}{
		{"FROM busybox", 2},
		{"RUN echo one && echo two", 4}, // the instruction goes over two lines: its first line
		{"COPY a b /", 6},
		{"RUN false", 7}, // the first of two that are the same
		{"RUN nothing", 0},
		{"# a comment", 0},
	} {
		if got := lineOfInstruction(file, tc.step); got != tc.want {
			t.Errorf("%q: line %d, want %d", tc.step, got, tc.want)
		}
	}
	if got := lineOfInstruction(filepath.Join(t.TempDir(), "none"), "RUN x"); got != 0 {
		t.Errorf("a file that is not there: %d", got)
	}
}
