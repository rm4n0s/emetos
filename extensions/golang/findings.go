package golang

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/rm4n0s/emetos/extension"
)

// The places that the output of the tools names (SPEC §10.17 "As built (step 14.3)"): `file:line:col: message` for a failed test,
// a build error, go vet and the lint tools; the frame of a panic. They become the markers of the Problems panel.

// Ported from VS Code Go: extension/src/util.ts (runTool: the expression that reads a line of the output of a tool, the severity,
// and the paths made absolute against the folder the tool ran in).

// finding is a place a tool named.
type finding struct {
	// Path is the absolute path of the file, Line and Col the place (from 1; Col is 0 when the tool gave none).
	Path      string
	Line, Col int
	Message   string
	// Code is the code of the rule ("SA4006", "errcheck"); empty when the tool gave none.
	Code string
	// Error says that the finding is an error, not a warning.
	Error bool
}

// placeRe reads `file.go:12:3: message`, `./file.go:12: message` and `vet: file.go:12:3: message`, and
// `file.go:12:3: error: message` (the severity word, which is dropped).
var placeRe = regexp.MustCompile(`^\s*(vet:\s+)?(\S+?\.go):(\d+)(?::(\d+))?:\s*(.*?)\s*$`)

// frameRe reads a frame of a panic: `	/path/to/file.go:12 +0x1d`.
var frameRe = regexp.MustCompile(`^\s+(/\S+?\.go):(\d+)(?: \+0x[0-9a-f]+)?\s*$`)

// trailingCodeRe reads the rule at the end of a message: `message (SA4006)`, `message (errcheck)`.
var trailingCodeRe = regexp.MustCompile(`\s+\(([A-Za-z][\w.-]*)\)$`)

// leadingCodeRe reads the rule at the start of a message, as revive prints it: `[exported] message`.
var leadingCodeRe = regexp.MustCompile(`^\[([\w.-]+)\]\s+`)

// absolute makes the path of the output absolute against the folder the program ran in.
func absolute(dir, path string) string {
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	return filepath.Clean(path)
}

// parseFindings reads the lines of the output of a tool that ran in dir: every line that names a place is a finding. withCode
// reads the rule from the message (the lint tools). warnings makes the findings warnings, except those of go vet's own `vet:`
// lines (the package does not type-check), which are errors.
func parseFindings(lines []string, dir string, withCode, warnings bool) []finding {
	var out []finding
	for _, l := range lines {
		m := placeRe.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		line, err := strconv.Atoi(m[3])
		if err != nil || line < 1 {
			continue
		}
		col, _ := strconv.Atoi(m[4])
		f := finding{Path: absolute(dir, m[2]), Line: line, Col: col, Message: m[5], Error: !warnings || m[1] != ""}
		if rest, ok := strings.CutPrefix(f.Message, "error: "); ok {
			f.Message, f.Error = rest, true
		} else if rest, ok := strings.CutPrefix(f.Message, "warning: "); ok {
			f.Message, f.Error = rest, false
		}
		if withCode {
			if c := leadingCodeRe.FindStringSubmatch(f.Message); c != nil {
				f.Code, f.Message = c[1], strings.TrimPrefix(f.Message, c[0])
			} else if c := trailingCodeRe.FindStringSubmatch(f.Message); c != nil {
				f.Code, f.Message = c[1], strings.TrimSuffix(f.Message, c[0])
			}
		}
		if f.Message == "" {
			continue
		}
		out = append(out, f)
	}
	return out
}

// markerOf makes the marker of a finding. The tools give a place, not a range, so the marker is empty at that place (the column is
// 1 when the tool gave none).
func markerOf(f finding, source string) extension.Marker {
	sev := extension.SeverityWarning
	if f.Error {
		sev = extension.SeverityError
	}
	col := max(f.Col, 1)
	return extension.Marker{
		Severity: sev, Message: f.Message, Source: source, Code: f.Code,
		StartLine: f.Line, StartColumn: col, EndLine: f.Line, EndColumn: col,
	}
}

// failureFindings reads the places in the output of a failed test: the lines `file_test.go:12: message` that the testing package
// prints (the lines that continue the message, indented further, are joined to it), the places of a build error, and the frame of a
// panic in a file under root (the first, which is the code that panicked) with the text of the panic.
func failureFindings(lines []string, dir, root, code string) []finding {
	var out []finding
	last := -1 // the finding that a continuation line belongs to
	panicText := ""
	panicked := false
	for _, l := range lines {
		if m := placeRe.FindStringSubmatch(l); m != nil {
			line, err := strconv.Atoi(m[3])
			if err == nil && line >= 1 {
				col, _ := strconv.Atoi(m[4])
				out = append(out, finding{Path: absolute(dir, m[2]), Line: line, Col: col, Message: m[5], Code: code, Error: true})
				last = len(out) - 1
				continue
			}
		}
		if rest, ok := strings.CutPrefix(l, "panic: "); ok && panicText == "" {
			panicText = "panic: " + rest
			last = -1
			continue
		}
		if m := frameRe.FindStringSubmatch(l); m != nil {
			if panicText != "" && !panicked && within(root, m[1]) {
				line, _ := strconv.Atoi(m[2])
				out = append(out, finding{Path: filepath.Clean(m[1]), Line: line, Message: panicText, Code: code, Error: true})
				panicked = true
			}
			last = -1
			continue
		}
		// a continuation of the message of the last place: the testing package indents it by 8 spaces
		if last >= 0 && strings.HasPrefix(l, "        ") && strings.TrimSpace(l) != "" {
			if out[last].Message != "" {
				out[last].Message += " "
			}
			out[last].Message += strings.TrimSpace(l)
			continue
		}
		last = -1
	}
	filtered := out[:0]
	for _, f := range out {
		if f.Message != "" && within(root, f.Path) {
			filtered = append(filtered, f)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

// within tells whether the path is root or below it; an empty root accepts every path.
func within(root, path string) bool {
	if root == "" {
		return true
	}
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
