package javascript

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

// js-debug, the debugger of JavaScript and TypeScript (SPEC §10.31 "The debuggers", D40): where it is, and the download of the release
// Emetos pins. Emetos never fetches it by itself: "JavaScript Debugger: Download" does, on the user's click, and the bytes are checked
// against the size and the SHA-256 pinned here before anything is unpacked.

// The release of js-debug that "JavaScript Debugger: Download" fetches: the DAP-only bundle of microsoft/vscode-js-debug (MIT), published
// on 2026-09-23. scripts/tool-versions.txt pins the same archive for the tests; change both together.
const (
	// JsDebugVersion is the version of the pinned release.
	JsDebugVersion = "1.140.0"
	// JsDebugSHA256 is the SHA-256 of the archive, as GitHub publishes it for the release asset.
	JsDebugSHA256 = "27dab92937ec1ab35821ae955aac867544fe06a1b6307229049f2d789af10968"
	// JsDebugSize is the size of the archive in bytes.
	JsDebugSize = 1249709
	// jsDebugReleaseURL is where the archive is.
	jsDebugReleaseURL = "https://github.com/microsoft/vscode-js-debug/releases/download/v" + JsDebugVersion + "/js-debug-dap-v" + JsDebugVersion + ".tar.gz"
)

// serverFile is the program of js-debug that serves DAP, below the folder js-debug of the release.
const serverFile = "src/dapDebugServer.js"

// release says what to download and what it must be: the tests give their own.
type release struct {
	// Version is the version of the release, the name of its folder in the data folder.
	Version string
	// URL is the address of the archive.
	URL string
	// SHA256 is the lower-case hex digest the archive must have, and Size its length in bytes.
	SHA256 string
	Size   int64
}

// pinned is the release this version of Emetos downloads.
func pinned() release {
	return release{Version: JsDebugVersion, URL: jsDebugReleaseURL, SHA256: JsDebugSHA256, Size: JsDebugSize}
}

// The limits of an archive that passed the checksum; a pinned archive is far below them, so a bigger one is not the one that was pinned.
const (
	maxEntries       = 4096
	maxUnpackedBytes = 256 << 20
	maxEntryBytes    = 64 << 20
	// downloadTimeout ends a download that takes longer.
	downloadTimeout = 10 * time.Minute
)

// dataDirFromEnv is the folder Emetos keeps downloaded tools in: $XDG_DATA_HOME/emetos/js-debug, or ~/.local/share/emetos/js-debug when
// the variable is unset or not absolute. The tests of the packages redirect XDG_DATA_HOME to a folder of their own.
func dataDirFromEnv() (string, error) {
	base := os.Getenv("XDG_DATA_HOME")
	if !filepath.IsAbs(base) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", errors.NewErr("JsDebugDataDirUnknown", err)
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(filepath.Clean(base), "emetos", "js-debug"), nil
}

// download fetches the release into the folder dir/<version>: the archive is read into a file next to it with its size and SHA-256
// counted, compared with the pinned ones, and only then unpacked, into a folder of its own that is renamed when it is complete. It
// returns the folder that holds src/dapDebugServer.js. It fails with:
//
//	JsDebugDownloadFailed          the request failed or the server did not answer 200
//	JsDebugSizeMismatch            the archive is longer or shorter than the pinned size
//	JsDebugChecksumMismatch        the SHA-256 of the archive is not the pinned one
//	JsDebugArchiveBroken           the archive is not a gzip of a tar
//	JsDebugArchiveUnsafe           a member is absolute, leaves the folder, or is a link that points outside it, or is not a file or folder
//	JsDebugArchiveTooLarge         the archive has more members, or more bytes, than a release does
//	JsDebugArchiveIncomplete       the release has no src/dapDebugServer.js
//	JsDebugInstallFailed           a file cannot be written
func download(ctx context.Context, client *http.Client, rel release, dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", errors.NewErr("JsDebugInstallFailed", err).SetMetadata("path", dir)
	}
	work, err := os.MkdirTemp(dir, ".download-")
	if err != nil {
		return "", errors.NewErr("JsDebugInstallFailed", err).SetMetadata("path", dir)
	}
	defer os.RemoveAll(work) //nolint:errcheck // a leftover temporary folder is not an error of the download

	archive := filepath.Join(work, "release.tar.gz")
	if err := fetch(ctx, client, rel, archive); err != nil {
		return "", err
	}
	staging := filepath.Join(work, "unpacked")
	if err := unpack(archive, staging); err != nil {
		return "", err
	}
	if info, err := os.Stat(filepath.Join(staging, "js-debug", filepath.FromSlash(serverFile))); err != nil || !info.Mode().IsRegular() {
		return "", errors.New("JsDebugArchiveIncomplete", "the release has no "+serverFile, "version", rel.Version)
	}
	dst := filepath.Join(dir, rel.Version)
	if err := os.RemoveAll(dst); err != nil {
		return "", errors.NewErr("JsDebugInstallFailed", err).SetMetadata("path", dst)
	}
	if err := os.Rename(staging, dst); err != nil {
		return "", errors.NewErr("JsDebugInstallFailed", err).SetMetadata("path", dst)
	}
	return filepath.Join(dst, "js-debug"), nil
}

// fetch downloads the archive to the file, counting its bytes and its SHA-256, and compares them with the release's.
func fetch(ctx context.Context, client *http.Client, rel release, file string) error {
	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rel.URL, nil)
	if err != nil {
		return errors.NewErr("JsDebugDownloadFailed", err).SetMetadata("url", rel.URL)
	}
	resp, err := client.Do(req)
	if err != nil {
		return errors.NewErr("JsDebugDownloadFailed", err).SetMetadata("url", rel.URL)
	}
	defer resp.Body.Close() //nolint:errcheck // the body was read or abandoned
	if resp.StatusCode != http.StatusOK {
		return errors.New("JsDebugDownloadFailed", "the server answered "+resp.Status, "url", rel.URL, "status", resp.StatusCode)
	}
	out, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return errors.NewErr("JsDebugInstallFailed", err).SetMetadata("path", file)
	}
	sum := sha256.New()
	// one byte more than the pinned size tells a download that is longer from one that is just as long
	n, err := io.Copy(io.MultiWriter(out, sum), io.LimitReader(resp.Body, rel.Size+1))
	if cerr := out.Close(); err == nil && cerr != nil {
		err = cerr
	}
	if err != nil {
		return errors.NewErr("JsDebugDownloadFailed", err).SetMetadata("url", rel.URL)
	}
	if n != rel.Size {
		return errors.New("JsDebugSizeMismatch", "the download is not the size of the release that is pinned", "size", n, "want", rel.Size, "url", rel.URL)
	}
	if got := hex.EncodeToString(sum.Sum(nil)); got != rel.SHA256 {
		return errors.New("JsDebugChecksumMismatch", "the SHA-256 of the download is not the one of the release that is pinned", "got", got, "want", rel.SHA256, "url", rel.URL)
	}
	return nil
}

// unpack unpacks the gzip of a tar into the folder dst, which it makes. A member is a folder or a regular file, or a symbolic link that
// stays inside dst; a name that is absolute or leaves dst, a link that points outside it, a hard link and a special file are refused
// (JsDebugArchiveUnsafe) before anything of the member is written, and nothing is written through a link. Modes are normalized: a file is
// 0644, or 0755 when its owner could run it; a folder is 0755.
func unpack(archive, dst string) error {
	f, err := os.Open(archive)
	if err != nil {
		return errors.NewErr("JsDebugInstallFailed", err).SetMetadata("path", archive)
	}
	defer f.Close() //nolint:errcheck // read only
	zr, err := gzip.NewReader(f)
	if err != nil {
		return errors.NewErr("JsDebugArchiveBroken", err).SetMetadata("path", archive)
	}
	defer zr.Close() //nolint:errcheck // read only
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return errors.NewErr("JsDebugInstallFailed", err).SetMetadata("path", dst)
	}
	root, err := filepath.EvalSymlinks(dst)
	if err != nil {
		return errors.NewErr("JsDebugInstallFailed", err).SetMetadata("path", dst)
	}
	tr := tar.NewReader(zr)
	var entries int
	var total int64
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return errors.NewErr("JsDebugArchiveBroken", err).SetMetadata("path", archive)
		}
		entries++
		if entries > maxEntries {
			return errors.New("JsDebugArchiveTooLarge", "the archive has more members than a release does", "limit", maxEntries)
		}
		target, err := memberPath(root, h.Name)
		if err != nil {
			return err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return errors.NewErr("JsDebugInstallFailed", err).SetMetadata("path", target)
			}
		case tar.TypeReg:
			if h.Size < 0 || h.Size > maxEntryBytes {
				return errors.New("JsDebugArchiveTooLarge", "a member of the archive is larger than a file of a release", "name", h.Name, "size", h.Size)
			}
			if total += h.Size; total > maxUnpackedBytes {
				return errors.New("JsDebugArchiveTooLarge", "the archive unpacks to more bytes than a release does", "limit", maxUnpackedBytes)
			}
			if err := writeMember(root, target, tr, h); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := linkMember(root, target, h); err != nil {
				return err
			}
		default:
			return errors.New("JsDebugArchiveUnsafe", "a member of the archive is neither a file, a folder nor a link", "name", h.Name, "type", string(h.Typeflag))
		}
	}
}

// memberPath is where the member goes: root joined with its name, which must be a relative name that stays below root. A name
// that is absolute, or has a .. that leaves, or has a NUL, fails with JsDebugArchiveUnsafe.
func memberPath(root, name string) (string, error) {
	switch {
	case name == "" || strings.ContainsRune(name, 0):
		return "", errors.New("JsDebugArchiveUnsafe", "a member of the archive has no usable name", "name", name)
	case path.IsAbs(name) || filepath.IsAbs(name) || strings.HasPrefix(name, "/") || strings.HasPrefix(name, `\`):
		return "", errors.New("JsDebugArchiveUnsafe", "a member of the archive has an absolute name", "name", name)
	}
	clean := path.Clean(name)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", errors.New("JsDebugArchiveUnsafe", "a member of the archive leaves the folder it unpacks into", "name", name)
	}
	target := filepath.Join(root, filepath.FromSlash(clean))
	if !within(root, target) {
		return "", errors.New("JsDebugArchiveUnsafe", "a member of the archive leaves the folder it unpacks into", "name", name)
	}
	return target, nil
}

// within tells whether target is root or below it.
func within(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// safeParent makes the folder the member goes in and checks that no part of it is a link that leaves root: the member is never written
// through a link.
func safeParent(root, target string) error {
	parent := filepath.Dir(target)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return errors.NewErr("JsDebugInstallFailed", err).SetMetadata("path", parent)
	}
	real, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return errors.NewErr("JsDebugInstallFailed", err).SetMetadata("path", parent)
	}
	if !within(root, real) {
		return errors.New("JsDebugArchiveUnsafe", "a member of the archive would be written through a link that leaves the folder", "path", target)
	}
	return nil
}

// writeMember writes a regular file.
func writeMember(root, target string, r io.Reader, h *tar.Header) error {
	if err := safeParent(root, target); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if h.Mode&0o100 != 0 {
		mode = 0o755
	}
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return errors.NewErr("JsDebugInstallFailed", err).SetMetadata("path", target)
	}
	src := &trackedReader{r: io.LimitReader(r, h.Size)}
	n, err := io.Copy(out, src)
	if cerr := out.Close(); err == nil && cerr != nil {
		err = cerr
	}
	if src.err != nil {
		return errors.NewErr("JsDebugArchiveBroken", src.err).SetMetadata("name", h.Name)
	}
	if err != nil {
		return errors.NewErr("JsDebugInstallFailed", err).SetMetadata("path", target)
	}
	if n != h.Size {
		return errors.New("JsDebugArchiveBroken", "a member of the archive is shorter than its header says", "name", h.Name)
	}
	return nil
}

// trackedReader remembers the error of the archive it reads, so that a file that cannot be written is told from an archive that is cut off.
type trackedReader struct {
	r   io.Reader
	err error
}

// Read implements io.Reader.
func (t *trackedReader) Read(p []byte) (int, error) {
	n, err := t.r.Read(p)
	if err != nil && err != io.EOF {
		t.err = err
	}
	return n, err
}

// linkMember makes a symbolic link whose target is relative and stays inside root.
func linkMember(root, target string, h *tar.Header) error {
	if h.Linkname == "" || strings.ContainsRune(h.Linkname, 0) || path.IsAbs(h.Linkname) || filepath.IsAbs(h.Linkname) {
		return errors.New("JsDebugArchiveUnsafe", "a link of the archive points to an absolute or empty path", "name", h.Name, "link", h.Linkname)
	}
	if err := safeParent(root, target); err != nil {
		return err
	}
	if !within(root, filepath.Join(filepath.Dir(target), filepath.FromSlash(h.Linkname))) {
		return errors.New("JsDebugArchiveUnsafe", "a link of the archive points outside the folder it unpacks into", "name", h.Name, "link", h.Linkname)
	}
	if err := os.Symlink(filepath.FromSlash(h.Linkname), target); err != nil {
		return errors.NewErr("JsDebugInstallFailed", err).SetMetadata("path", target)
	}
	return nil
}

// locate finds js-debug's server program. A path (the setting debug.javascript.adapterPath) is the program itself, the folder that has
// src/dapDebugServer.js, or a folder with js-debug/src/dapDebugServer.js (the release unpacked as it is); with none, the release that
// "JavaScript Debugger: Download" put in the data folder: the pinned version, else the newest other. found is false and err nil when
// there is none; a path that is no js-debug fails with JsDebugPathInvalid.
func locate(setting, dataDir string) (program string, found bool, err error) {
	if setting != "" {
		p, ok := serverIn(setting)
		if !ok {
			return "", false, errors.New("JsDebugPathInvalid", "debug.javascript.adapterPath is "+setting+", which is not js-debug: it holds no "+serverFile+" (or js-debug/"+serverFile+")", "path", setting)
		}
		return p, true, nil
	}
	if dataDir == "" {
		return "", false, nil
	}
	if p, ok := serverIn(filepath.Join(dataDir, JsDebugVersion)); ok {
		return p, true, nil
	}
	list, rerr := os.ReadDir(dataDir)
	if rerr != nil {
		return "", false, nil //nolint:nilerr // no data folder: nothing was downloaded
	}
	names := make([]string, 0, len(list))
	for _, e := range list {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			names = append(names, e.Name())
		}
	}
	sort.Slice(names, func(a, b int) bool { return langserver.CompareVersions(names[a], names[b]) > 0 })
	for _, n := range names {
		if p, ok := serverIn(filepath.Join(dataDir, n)); ok {
			return p, true, nil
		}
	}
	return "", false, nil
}

// serverIn finds dapDebugServer.js given the program, or a folder that holds it directly or in a folder js-debug.
func serverIn(p string) (string, bool) {
	info, err := os.Stat(p)
	if err != nil {
		return "", false
	}
	if info.Mode().IsRegular() {
		return p, filepath.Base(p) == "dapDebugServer.js"
	}
	for _, rel := range []string{serverFile, path.Join("js-debug", serverFile)} {
		c := filepath.Join(p, filepath.FromSlash(rel))
		if fi, err := os.Stat(c); err == nil && fi.Mode().IsRegular() {
			return c, true
		}
	}
	return "", false
}

// downloadCommand is the handler of "JavaScript Debugger: Download". UI thread.
func (j *javascript) downloadCommand(context.Context, any) error {
	j.startDownload()
	return nil
}

// startDownload downloads the pinned release on a goroutine of its own and tells the user how it went. It is only ever called by a
// click: the command, or the button of the notification that says js-debug is missing. A second call while one runs only says so. It may
// be called from any goroutine.
func (j *javascript) startDownload() {
	if !j.downloading.CompareAndSwap(false, true) {
		j.api.Notifications().Info("js-debug is being downloaded already.")
		return
	}
	j.api.Notifications().Info("Downloading js-debug " + j.release.Version + " from github.com/microsoft/vscode-js-debug.")
	go func() {
		defer j.downloading.Store(false)
		dir, err := j.dataDir()
		var where string
		if err == nil {
			where, err = download(j.ctx, j.client, j.release, dir)
		}
		j.api.Post(func() {
			if err != nil {
				j.api.Log().Error("the download of js-debug failed", "err", err)
				j.api.Notifications().Error("js-debug could not be downloaded: " + describe(err))
				return
			}
			j.api.Log().Info("js-debug was downloaded", "version", j.release.Version, "path", where)
			j.api.Notifications().Info("js-debug " + j.release.Version + " was downloaded to " + where + ". Start a debug session to use it.")
		})
	}()
}

// describe is the message of an error for the user: the message of an error of this package, else its text.
func describe(err error) string {
	if appErr, ok := errors.FromError(err); ok {
		return appErr.Message + " (" + appErr.Tag + ")"
	}
	return err.Error()
}
