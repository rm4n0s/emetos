//go:build !linux

package golang

import (
	"path/filepath"
	"strings"
)

// executableOf is the file `go version` reads to tell whether the process is a Go program: macOS has no /proc, but its ps gives the
// full path of the executable as the name (comm). When that is cut at the width of the column, or is not a path, the first word of
// the command line stands in for it if it is an absolute path; else the process is left out.
func executableOf(p process) string {
	if name := strings.TrimSpace(p.name); filepath.IsAbs(name) {
		return name
	}
	if first, _, _ := strings.Cut(strings.TrimSpace(p.cmd), " "); filepath.IsAbs(first) {
		return first
	}
	return ""
}
