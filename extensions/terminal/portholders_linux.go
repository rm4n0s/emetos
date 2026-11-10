//go:build linux

package terminal

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// portHolders are the processes that listen on a TCP port of this computer, found in /proc (no lsof): the sockets in
// /proc/net/tcp and tcp6 that listen on the port, and the processes that hold them open. Only the processes of the user can be
// read, which are the ones a fix may kill.
func portHolders(proc string, port int) []int {
	inodes := map[string]bool{}
	for _, f := range []string{"net/tcp", "net/tcp6"} {
		data, err := os.ReadFile(filepath.Join(proc, f))
		if err != nil {
			continue
		}
		for i, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if i == 0 || len(fields) < 10 || fields[3] != "0A" { // 0A is LISTEN
				continue
			}
			_, hexPort, ok := strings.Cut(fields[1], ":")
			if !ok {
				continue
			}
			if p, err := strconv.ParseUint(hexPort, 16, 32); err == nil && int(p) == port {
				inodes[fields[9]] = true
			}
		}
	}
	if len(inodes) == 0 {
		return nil
	}
	entries, err := os.ReadDir(proc)
	if err != nil {
		return nil
	}
	var pids []int
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		fds, err := os.ReadDir(filepath.Join(proc, e.Name(), "fd"))
		if err != nil {
			continue
		}
		for _, fd := range fds {
			link, err := os.Readlink(filepath.Join(proc, e.Name(), "fd", fd.Name()))
			if err != nil {
				continue
			}
			if rest, ok := strings.CutPrefix(link, "socket:["); ok && inodes[strings.TrimSuffix(rest, "]")] {
				pids = append(pids, pid)
				break
			}
		}
	}
	return pids
}
