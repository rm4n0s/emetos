//go:build linux && amd64

package realinput

// sysMemfdCreate is the number of the memfd_create system call; the syscall package does not define it.
const sysMemfdCreate = 319
