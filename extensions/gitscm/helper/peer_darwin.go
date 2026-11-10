//go:build darwin

package helper

import (
	"net"

	"github.com/rm4n0s/errors"
	"golang.org/x/sys/unix"
)

// peerUID is the user id of the process at the other end of the connection (LOCAL_PEERCRED, macOS's SO_PEERCRED). The error
// has the tag GitHelperPeerUnknown.
func peerUID(conn *net.UnixConn) (uint32, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return 0, errors.NewErr("GitHelperPeerUnknown", err)
	}
	var cred *unix.Xucred
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		cred, credErr = unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
	}); err != nil {
		return 0, errors.NewErr("GitHelperPeerUnknown", err)
	}
	if credErr != nil {
		return 0, errors.NewErr("GitHelperPeerUnknown", credErr)
	}
	return cred.Uid, nil
}
