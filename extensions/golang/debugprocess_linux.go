//go:build linux

package golang

import "strconv"

// executableOf is the file `go version` reads to tell whether the process is a Go program: the executable that the kernel keeps
// for it.
func executableOf(p process) string {
	return "/proc/" + strconv.Itoa(p.pid) + "/exe"
}
