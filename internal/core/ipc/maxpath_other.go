//go:build !linux

package ipc

// maxSocketPath is the longest path of a Unix socket on macOS and the BSDs: sockaddr_un.sun_path holds 104 bytes, one of
// them the end of the string.
const maxSocketPath = 103
