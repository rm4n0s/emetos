//go:build linux

package containerapi

import "syscall"

// podmanProcAttr puts podman's service in a group of its own, and asks the kernel to send it SIGTERM when the thread that
// started it ends.
func podmanProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGTERM}
}
