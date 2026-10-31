package helper

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/rm4n0s/errors"
)

// The names of the scripts in the folder of the process.
const (
	// ScriptAskpass is GIT_ASKPASS.
	ScriptAskpass = "askpass.sh"
	// ScriptSSHAskpass is SSH_ASKPASS.
	ScriptSSHAskpass = "ssh-askpass.sh"
	// ScriptEditor is GIT_EDITOR.
	ScriptEditor = "editor.sh"
)

// maxSocketPath is the longest path of a Unix socket the kernel takes (sun_path is 108 bytes with the final NUL; 104 is the limit of
// the older systems and leaves room).
const maxSocketPath = 104

// folderPattern matches the name of a folder of a process.
var folderPattern = regexp.MustCompile(`^git-([0-9]+)$`)

// folderLock orders the changes to the folder of the process: the servers of the window tabs of one process share it, the scripts in it
// and the removal when the last socket is gone.
var folderLock sync.Mutex

// Root is the folder that holds the folders of the processes: $XDG_RUNTIME_DIR/emetos when the runtime folder is an absolute path,
// else stateDir, the folder of Emetos's state ($XDG_STATE_HOME/emetos; SPEC §10.30 "Passwords and editors").
func Root(runtimeDir, stateDir string) string {
	if filepath.IsAbs(runtimeDir) {
		return filepath.Join(runtimeDir, "emetos")
	}
	return stateDir
}

// FolderOf is the folder of a process under the root: <root>/git-<pid>.
func FolderOf(root string, pid int) string { return filepath.Join(root, "git-"+strconv.Itoa(pid)) }

// prepareFolder makes the folder of the process (mode 0700, owned by the user) and writes the three scripts in it for the binary. The
// folder may exist already when another window tab of this process made it. The error has the tags GitHelperFolderUnusable,
// GitHelperFolderCreateFailed and GitHelperScriptWriteFailed.
func prepareFolder(root string, pid int, binary string) (string, error) {
	folderLock.Lock()
	defer folderLock.Unlock()
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", errors.NewErr("GitHelperFolderCreateFailed", err).SetMetadata("folder", root)
	}
	dir := FolderOf(root, pid)
	if err := os.Mkdir(dir, 0o700); err != nil && !os.IsExist(err) {
		return "", errors.NewErr("GitHelperFolderCreateFailed", err).SetMetadata("folder", dir)
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return "", errors.NewErr("GitHelperFolderCreateFailed", err).SetMetadata("folder", dir)
	}
	if !info.IsDir() || info.Mode().Perm()&0o077 != 0 || !ownedByUser(info) {
		return "", errors.New("GitHelperFolderUnusable", "the folder of the helper is not a private folder of the user", "folder", dir, "mode", info.Mode().String())
	}
	for _, s := range []struct{ name, flag string }{
		{ScriptAskpass, FlagAskpass}, {ScriptSSHAskpass, FlagSSHAskpass}, {ScriptEditor, FlagEditor},
	} {
		if err := writeScript(filepath.Join(dir, s.name), ScriptText(binary, s.flag)); err != nil {
			return "", err
		}
	}
	return dir, nil
}

// ownedByUser says whether the file belongs to the user that runs the process.
func ownedByUser(info os.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == os.Getuid()
}

// ScriptText is the text of a script: three lines that start the binary with the flag and the arguments git gave. The script holds no
// secret. The path of the binary is in single quotes, with a quote in it written '\”, so any path is read as it is.
func ScriptText(binary, flag string) string {
	return "#!/bin/sh\nexec " + shellQuote(binary) + " " + flag + " \"$@\"\n"
}

// shellQuote quotes s for a POSIX shell in single quotes.
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// writeScript writes the script with mode 0700 through a temporary file and a rename, unless it holds this text already.
func writeScript(path, text string) error {
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, []byte(text)) {
		if info, err := os.Stat(path); err == nil && info.Mode().Perm() == 0o700 {
			return nil
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".script-*")
	if err != nil {
		return errors.NewErr("GitHelperScriptWriteFailed", err).SetMetadata("script", filepath.Base(path))
	}
	name := tmp.Name()
	_, werr := tmp.WriteString(text)
	cerr := tmp.Close()
	if werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Chmod(name, 0o700)
	}
	if werr == nil {
		werr = os.Rename(name, path)
	}
	if werr != nil {
		_ = os.Remove(name)
		return errors.NewErr("GitHelperScriptWriteFailed", werr).SetMetadata("script", filepath.Base(path))
	}
	return nil
}

// RemoveStale removes the folders of processes that are gone from the root: a folder git-<pid> of the user, not a link, whose process
// does not exist any more. The folder of a live process (this one included) stays. It returns the folders it removed and the number of
// folders it could not remove.
func RemoveStale(root string, self int) (removed []string, failed int) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, 0
	}
	for _, e := range entries {
		m := folderPattern.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		pid, err := strconv.Atoi(m[1])
		if err != nil || pid == self || pid <= 0 || processExists(pid) {
			continue
		}
		path := filepath.Join(root, e.Name())
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() || !ownedByUser(info) {
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			failed++
			continue
		}
		removed = append(removed, path)
	}
	return removed, failed
}

// processExists says whether a process with the id exists: the signal 0 finds it, EPERM means it belongs to someone else.
func processExists(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// releaseFolder removes the socket of a server and, when no other socket is left in the folder of the process, the scripts and the
// folder.
func releaseFolder(dir, socket string) {
	folderLock.Lock()
	defer folderLock.Unlock()
	_ = os.Remove(socket)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sock") {
			return
		}
	}
	_ = os.RemoveAll(dir)
}
