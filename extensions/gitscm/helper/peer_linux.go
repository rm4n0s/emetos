//go:build linux

package helper

import (
	"net"
	"syscall"

	"github.com/rm4n0s/errors"
)

// peerUID is the user id of the process at the other end of the connection (SO_PEERCRED). The error has the tag GitHelperPeerUnknown.
func peerUID(conn *net.UnixConn) (uint32, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return 0, errors.NewErr("GitHelperPeerUnknown", err)
	}
	var cred *syscall.Ucred
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		cred, credErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return 0, errors.NewErr("GitHelperPeerUnknown", err)
	}
	if credErr != nil {
		return 0, errors.NewErr("GitHelperPeerUnknown", credErr)
	}
	return cred.Uid, nil
}
