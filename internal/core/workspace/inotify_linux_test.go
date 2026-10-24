package workspace

import "golang.org/x/sys/unix"

// realAddWatch is the system call the tests wrap.
var realAddWatch = unix.InotifyAddWatch
