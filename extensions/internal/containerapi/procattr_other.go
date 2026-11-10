//go:build !linux

package containerapi

import "syscall"

// podmanProcAttr puts podman's service in a group of its own. macOS has no Pdeathsig: a service that outlives Emetos (a
// crash, a SIGKILL) stays until its own --time or the user ends it; Stop ends it in every other case.
func podmanProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}
