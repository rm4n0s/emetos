package terminal

import (
	"net"
	"path/filepath"
	"testing"
)

func TestPortHoldersInAMissingProc(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("no loopback: %v", err)
	}
	defer func() { _ = ln.Close() }()
	port := ln.Addr().(*net.TCPAddr).Port
	if got := portHolders(filepath.Join(t.TempDir(), "none"), port); got != nil {
		t.Errorf("holders in a missing folder = %v", got)
	}
}
