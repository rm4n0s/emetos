package tasks

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Problem is a line of the output of a task that names a place in a file: file:line:col: message, as compilers, vet and
// linters print it.
type Problem struct {
	// Path is the file as the line wrote it.
	Path string
	// Line and Column count from 1; Column is 1 when the line had none.
	Line, Column int
	// Message is the text after the place.
	Message string
}

// problemPattern is the form of such a line. The path has no spaces, so that a line of prose with a colon is not one.
var problemPattern = regexp.MustCompile(`^\s*(?:vet: )?([^\s:][^\s:]*):(\d+):(?:(\d+):)?\s*(.*\S)\s*$`)

// parseProblem reads a line of output; false when it names no place.
func parseProblem(line string) (Problem, bool) {
	m := problemPattern.FindStringSubmatch(line)
	if m == nil {
		return Problem{}, false
	}
	ln, err := strconv.Atoi(m[2])
	if err != nil || ln < 1 {
		return Problem{}, false
	}
	col := 1
	if m[3] != "" {
		if c, err := strconv.Atoi(m[3]); err == nil && c > 0 {
			col = c
		}
	}
	if numberOnly.MatchString(m[4]) {
		return Problem{}, false // a place and nothing else: a time of day, a line that was cut
	}
	return Problem{Path: m[1], Line: ln, Column: col, Message: m[4]}, true
}

// numberOnly is a message that is a number and colons.
var numberOnly = regexp.MustCompile(`^[\d:]+$`)

// isWarning tells whether the message of a problem is a warning.
func isWarning(message string) bool {
	m := strings.ToLower(message)
	return strings.HasPrefix(m, "warning") || strings.Contains(m, "warning:")
}

// resolve gives the path of a problem inside the folder: relative paths are relative to it. A path that leaves the folder is
// not a problem of the task.
func resolve(folder, path string) (string, bool) {
	full := path
	if !filepath.IsAbs(full) {
		full = filepath.Join(folder, path)
	}
	full = filepath.Clean(full)
	rel, err := filepath.Rel(folder, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return full, true
}

// isFile tells whether the path is a regular file.
func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
