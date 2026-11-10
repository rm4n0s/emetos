//go:build linux

package git

import (
	"os"
	"strconv"
	"strings"
)

// isZombie tells whether the process has ended and waits to be reaped, from /proc/<pid>/stat; a process that cannot be read counts
// as one.
func isZombie(pid int) bool {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return true
	}
	s := string(data)
	if i := strings.LastIndex(s, ")"); i >= 0 && len(s) > i+2 {
		return s[i+2] == 'Z'
	}
	return false
}
