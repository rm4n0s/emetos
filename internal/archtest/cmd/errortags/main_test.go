package main

import (
	"bytes"
	"strings"
	"testing"
)

const fixture = "../../testdata/error_tags"

func TestRun(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		status     int
		stdout     []string // lines that must be there
		notStdout  []string // lines that must not be
		stderrPart string
	}{
		{"every Tag", []string{fixture}, 1, []string{"LiteralRoute\tinternal/core/thing/thing.go:8\tDo", "MethodTag\tinternal/core/thing/thing.go:31\tThing.Method"}, nil, "extensions/demo/demo.go:5: the Tag is not a string literal"},
		{"the untested Tags without the listed ones", []string{"--untested", fixture}, 1,
			[]string{"MethodTag\tinternal/core/thing/thing.go:31\tThing.Method", "ToolFailed\tcmd/tool/main.go:5\tmain"}, []string{"OnlyNamed\tinternal/gui/other/other.go:5\tFail", "LiteralRoute"}, "not a string literal"},
		{"the untested Tags with the listed ones", []string{"--untested", "--all", fixture}, 1,
			[]string{"OnlyNamed\tinternal/gui/other/other.go:5\tFail"}, []string{"LiteralRoute"}, "not a string literal"},
		{"a folder that is not there", []string{"--untested", "../../testdata/does_not_exist"}, 2, nil, nil, "error-tags:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			status := run(tt.args, &stdout, &stderr)
			if status != tt.status {
				t.Errorf("status = %d, want %d (stderr %q)", status, tt.status, stderr.String())
			}
			lines := strings.Split(stdout.String(), "\n")
			has := func(line string) bool {
				for _, l := range lines {
					if l == line {
						return true
					}
				}
				return false
			}
			for _, want := range tt.stdout {
				if !has(want) {
					t.Errorf("stdout lacks %q:\n%s", want, stdout.String())
				}
			}
			for _, bad := range tt.notStdout {
				for _, l := range lines {
					if strings.HasPrefix(l, bad) {
						t.Errorf("stdout has %q", l)
					}
				}
			}
			if !strings.Contains(stderr.String(), tt.stderrPart) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tt.stderrPart)
			}
		})
	}
}
