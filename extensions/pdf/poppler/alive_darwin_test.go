package poppler

import "syscall"

// alive tells whether the process is running (macOS has no /proc: a zombie counts as running until init reads its end).
func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }
