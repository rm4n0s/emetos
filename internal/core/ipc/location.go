package ipc

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/storage"
)

// maxSocketPath is the longest path of a Unix socket on Linux: sockaddr_un.sun_path holds 108 bytes, one of
// them the end of the string.
const maxSocketPath = 107

// Location is where the socket of the instance and its lock file are.
type Location struct {
	// Socket is the path of the Unix socket.
	Socket string
	// Lock is the path of the lock file that is held while a launch decides whether it is the instance.
	Lock string
}

// Locations gives the socket and the lock file of the user uid: $XDG_RUNTIME_DIR/emetos-<uid>.sock and
// emetos-<uid>.lock (SPEC §10.11). Without a runtime folder (the variable is unset) they are in
// <temporary folder>/emetos-<uid>/, a folder that is made with mode 0700 and that must belong to the user and
// be private, as ssh does for its agent socket. The tags are IPCRuntimeDirInvalid (the folder cannot be made,
// is not the user's or not private) and IPCSocketPathTooLong (the path does not fit in the address of a Unix
// socket).
func Locations(paths storage.Paths, uid int) (Location, error) {
	var loc Location
	if paths.RuntimeDir != "" {
		loc = Location{
			Socket: paths.SocketPath(uid),
			Lock:   filepath.Join(paths.RuntimeDir, "emetos-"+strconv.Itoa(uid)+".lock"),
		}
	} else {
		dir := filepath.Join(os.TempDir(), "emetos-"+strconv.Itoa(uid))
		if err := ensurePrivateDir(dir, uid); err != nil {
			return Location{}, err
		}
		loc = Location{Socket: filepath.Join(dir, "emetos.sock"), Lock: filepath.Join(dir, "emetos.lock")}
	}
	if len(loc.Socket) > maxSocketPath {
		return Location{}, errors.New("IPCSocketPathTooLong", "the path of the socket does not fit in the address of a Unix socket",
			"path", loc.Socket, "length", len(loc.Socket), "limit", maxSocketPath)
	}
	return loc, nil
}

// ensurePrivateDir makes dir with mode 0700 when it is not there, and checks that it is a folder (not a link)
// of the user uid that nobody else can use.
func ensurePrivateDir(dir string, uid int) error {
	if err := os.Mkdir(dir, 0o700); err != nil && !os.IsExist(err) {
		return errors.NewErr("IPCRuntimeDirInvalid", err).SetMetadata("dir", dir)
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return errors.NewErr("IPCRuntimeDirInvalid", err).SetMetadata("dir", dir)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	switch {
	case !info.IsDir():
		return errors.New("IPCRuntimeDirInvalid", "the folder of the socket is not a folder", "dir", dir)
	case !ok || int(st.Uid) != uid:
		return errors.New("IPCRuntimeDirInvalid", "the folder of the socket belongs to another user", "dir", dir)
	case info.Mode().Perm()&0o077 != 0:
		return errors.New("IPCRuntimeDirInvalid", "the folder of the socket is not private (mode 0700)", "dir", dir, "mode", info.Mode().Perm().String())
	}
	return nil
}

// validLocation checks the paths of a Location a caller made itself.
func validLocation(loc Location) error {
	for _, p := range []string{loc.Socket, loc.Lock} {
		if !filepath.IsAbs(p) || strings.ContainsRune(p, 0) {
			return errors.New("IPCSocketPathInvalid", "the path of the socket and of the lock are absolute", "path", p)
		}
	}
	if len(loc.Socket) > maxSocketPath {
		return errors.New("IPCSocketPathTooLong", "the path of the socket does not fit in the address of a Unix socket",
			"path", loc.Socket, "length", len(loc.Socket), "limit", maxSocketPath)
	}
	return nil
}
