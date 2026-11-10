//go:build linux

package ipc

// maxSocketPath is the longest path of a Unix socket on Linux: sockaddr_un.sun_path holds 108 bytes, one of
// them the end of the string.
const maxSocketPath = 107
