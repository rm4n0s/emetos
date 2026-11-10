//go:build !linux

package display

// inotifyAvailable is true where there is no inotify to run out of: the workspace watcher polls there without a warning.
func inotifyAvailable() bool { return true }
