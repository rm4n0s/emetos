//go:build !linux

package terminal

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// portHoldersTimeout ends lsof when it does not answer.
const portHoldersTimeout = 5 * time.Second

// portHolders are the processes that listen on a TCP port of this computer, found with lsof (macOS has no /proc): `lsof -t` prints
// the process ids of the listeners on the port. proc is the /proc of Linux and is not used. Only the processes of the user are shown to
// a user who is not root, which are the ones a fix may kill. lsof that is missing or fails finds no process.
func portHolders(_ string, port int) []int {
	ctx, cancel := context.WithTimeout(context.Background(), portHoldersTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/sbin/lsof", "-nP", "-iTCP:"+strconv.Itoa(port), "-sTCP:LISTEN", "-t").Output() //nolint:gosec // a fixed system program
	if err != nil {
		return nil // lsof exits with 1 when nothing matches
	}
	return parsePIDs(string(out))
}

// parsePIDs reads the process ids of `lsof -t`, one for each line, once each.
func parsePIDs(out string) []int {
	var pids []int
	seen := map[int]bool{}
	for _, line := range strings.Fields(out) {
		pid, err := strconv.Atoi(line)
		if err != nil || pid <= 0 || seen[pid] {
			continue
		}
		seen[pid] = true
		pids = append(pids, pid)
	}
	return pids
}
