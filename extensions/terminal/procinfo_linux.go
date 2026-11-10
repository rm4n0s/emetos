//go:build linux

package terminal

import (
	"os"
	"strconv"
	"strings"
)

// processName is the name of the program of the process pid, from /proc/<pid>/comm. ok is false when it cannot be read.
func processName(pid int) (name string, ok bool) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/comm")
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(data)), true
}

// processCwd is the folder the process pid is in, from /proc/<pid>/cwd. ok is false when it cannot be read.
func processCwd(pid int) (dir string, ok bool) {
	dir, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/cwd")
	return dir, err == nil
}
