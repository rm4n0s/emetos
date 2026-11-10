//go:build !linux

package git

import (
	"os/exec"
	"strconv"
	"strings"
)

// isZombie tells whether the process has ended and waits to be reaped, from the state `ps` shows (a letter Z); a process that ps does
// not know counts as one.
func isZombie(pid int) bool {
	out, err := exec.Command("/bin/ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return true
	}
	return strings.HasPrefix(strings.TrimSpace(string(out)), "Z")
}
