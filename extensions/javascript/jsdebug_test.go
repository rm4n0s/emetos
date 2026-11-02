package javascript

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/rm4n0s/errors"
)

// The download of js-debug and the search for it (SPEC §10.31 "The debuggers", JavaScript and TypeScript; D4, D40): the release is fetched
// from a local httptest server that serves fake archives, into folders of the test only, and everything about the archive is checked
// before anything of it is unpacked.

// member is a member of a fake archive.
type member struct {
	name string
	// typ is the tar type flag; 0 is a regular file.
	typ  byte
	body string
	link string
	mode int64
	// size makes the header say a size other than the body's (to test a limit without writing the bytes).
	size int64
}

// archiveOf makes the gzip of a tar of the members.
func archiveOf(t *testing.T, members ...member) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for _, m := range members {
		typ := m.typ
		if typ == 0 {
			typ = tar.TypeReg
		}
		mode := m.mode
		if mode == 0 {
			mode = 0o644
		}
		h := &tar.Header{Name: m.name, Typeflag: typ, Mode: mode, Linkname: m.link, Size: int64(len(m.body))}
		if typ != tar.TypeReg {
			h.Size = 0
		}
		if m.size != 0 {
			h.Size = m.size
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if typ == tar.TypeReg && m.size == 0 {
			if _, err := tw.Write([]byte(m.body)); err != nil {
				t.Fatal(err)
			}
		}
		if m.size != 0 {
			// the archive ends here: the header said more than the archive holds
			if err := zw.Close(); err != nil {
				t.Fatal(err)
			}
			return buf.Bytes()
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// goodMembers are the members of a js-debug release as the real one has them.
func goodMembers() []member {
	return []member{
		{name: "js-debug/", typ: tar.TypeDir},
		{name: "js-debug/src/", typ: tar.TypeDir},
		{name: "js-debug/src/dapDebugServer.js", body: "// js-debug\n"},
		{name: "js-debug/src/bootloader.js", body: "// the bootloader\n", mode: 0o755},
		{name: "js-debug/LICENSE", body: "MIT\n"},
	}
}

// serve serves the bytes at /js-debug.tar.gz with the status and counts the requests; the returned release asks for exactly these bytes.
func serve(t *testing.T, body []byte, status int) (release, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path != "/js-debug.tar.gz" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
		if status == http.StatusOK {
			_, _ = w.Write(body)
		}
	}))
	t.Cleanup(srv.Close)
	sum := sha256.Sum256(body)
	return release{Version: "9.9.9", URL: srv.URL + "/js-debug.tar.gz", SHA256: hex.EncodeToString(sum[:]), Size: int64(len(body))}, &hits
}

// routeOf returns the error of the call as the package's error and fails when it is not one.
func routeOf(t *testing.T, err error) *errors.Error {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("err = %v (%T), want an *errors.Error", err, err)
	}
	return appErr
}

func TestTheReleaseIsDownloadedCheckedAndUnpacked(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data", "js-debug")
	rel, hits := serve(t, archiveOf(t, goodMembers()...), http.StatusOK)
	where, err := download(t.Context(), http.DefaultClient, rel, dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "9.9.9", "js-debug"); where != want {
		t.Errorf("folder = %s, want %s", where, want)
	}
	for name, mode := range map[string]os.FileMode{"src/dapDebugServer.js": 0o644, "src/bootloader.js": 0o755, "LICENSE": 0o644} {
		info, err := os.Stat(filepath.Join(where, filepath.FromSlash(name)))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if info.Mode().Perm() != mode {
			t.Errorf("%s has the mode %v, want %v", name, info.Mode().Perm(), mode)
		}
	}
	if hits.Load() != 1 {
		t.Errorf("%d requests, want 1", hits.Load())
	}
	// the work folder is gone and the release is the only thing in the folder
	list, err := os.ReadDir(dir)
	if err != nil || len(list) != 1 || list[0].Name() != "9.9.9" {
		t.Errorf("the data folder holds %v (%v), want only 9.9.9", list, err)
	}
	// found through the search, with a path or without
	if p, ok, err := locate("", dir); err != nil || !ok || p != filepath.Join(where, "src", "dapDebugServer.js") {
		t.Errorf("locate = %s %v %v", p, ok, err)
	}
	// the same version again replaces the folder
	if _, err := download(t.Context(), http.DefaultClient, rel, dir); err != nil {
		t.Errorf("a second download: %v", err)
	}
}

func TestADownloadThatCannotBeTrustedIsRefusedBeforeAnythingIsUnpacked(t *testing.T) {
	good := archiveOf(t, goodMembers()...)
	tests := []struct {
		name  string
		build func(t *testing.T) release
		route string
	}{
		{"a checksum that is not the pinned one", func(t *testing.T) release {
			rel, _ := serve(t, good, http.StatusOK)
			rel.SHA256 = strings.Repeat("0", 64)
			return rel
		}, "download->fetch.JsDebugChecksumMismatch"},
		{"an archive that was changed after it was pinned", func(t *testing.T) release {
			rel, _ := serve(t, good, http.StatusOK)
			other := append([]byte{}, good...)
			other[len(other)/2] ^= 0xff
			sum := sha256.Sum256(other) // the pin is the SHA-256 of other bytes of the same length
			rel.SHA256 = hex.EncodeToString(sum[:])
			return rel
		}, "download->fetch.JsDebugChecksumMismatch"},
		{"an archive longer than the pinned size", func(t *testing.T) release {
			rel, _ := serve(t, good, http.StatusOK)
			rel.Size = int64(len(good)) - 3
			return rel
		}, "download->fetch.JsDebugSizeMismatch"},
		{"an archive shorter than the pinned size", func(t *testing.T) release {
			rel, _ := serve(t, good, http.StatusOK)
			rel.Size = int64(len(good)) + 3
			return rel
		}, "download->fetch.JsDebugSizeMismatch"},
		{"a server that says no", func(t *testing.T) release {
			rel, _ := serve(t, good, http.StatusForbidden)
			return rel
		}, "download->fetch.JsDebugDownloadFailed"},
		{"a file that is not there", func(t *testing.T) release {
			rel, _ := serve(t, good, http.StatusOK)
			rel.URL = strings.TrimSuffix(rel.URL, "js-debug.tar.gz") + "other.tar.gz"
			return rel
		}, "download->fetch.JsDebugDownloadFailed"},
		{"a server that is not there", func(t *testing.T) release {
			rel, _ := serve(t, good, http.StatusOK)
			srv := httptest.NewServer(http.NotFoundHandler())
			rel.URL = srv.URL + "/js-debug.tar.gz"
			srv.Close()
			return rel
		}, "download->fetch.JsDebugDownloadFailed"},
		{"bytes that are not a gzip (with the right checksum)", func(t *testing.T) release {
			rel, _ := serve(t, []byte("this is not an archive at all"), http.StatusOK)
			return rel
		}, "download->unpack.JsDebugArchiveBroken"},
		{"a gzip that is cut off (with the right checksum)", func(t *testing.T) release {
			rel, _ := serve(t, good[:len(good)/2], http.StatusOK)
			return rel
		}, "download->unpack.JsDebugArchiveBroken"},
		{"a gzip of something that is not a tar (with the right checksum)", func(t *testing.T) release {
			var buf bytes.Buffer
			zw := gzip.NewWriter(&buf)
			_, _ = zw.Write(bytes.Repeat([]byte("not a tar file "), 100))
			_ = zw.Close()
			rel, _ := serve(t, buf.Bytes(), http.StatusOK)
			return rel
		}, "download->unpack.JsDebugArchiveBroken"},
		{"a release that has no server program", func(t *testing.T) release {
			rel, _ := serve(t, archiveOf(t, member{name: "js-debug/LICENSE", body: "MIT\n"}), http.StatusOK)
			return rel
		}, "download.JsDebugArchiveIncomplete"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parent := t.TempDir()
			dir := filepath.Join(parent, "js-debug")
			// an earlier release that must stay as it is
			old := writeFile(t, dir, "1.0.0/js-debug/src/dapDebugServer.js", "// old\n", 0o644)
			_, err := download(t.Context(), http.DefaultClient, tt.build(t), dir)
			appErr := routeOf(t, err)
			if !appErr.HasRoute(tt.route) {
				t.Errorf("route = %s, want it to have %s", appErr.Route(), tt.route)
			}
			if data, rerr := os.ReadFile(old); rerr != nil || string(data) != "// old\n" {
				t.Errorf("the earlier release was touched: %q %v", data, rerr)
			}
			list, rerr := os.ReadDir(dir)
			if rerr != nil || len(list) != 1 || list[0].Name() != "1.0.0" {
				t.Errorf("the data folder holds %v (%v), want only 1.0.0: nothing of the failed download stays", list, rerr)
			}
		})
	}
}

func TestAnArchiveThatLeavesItsFolderIsRefused(t *testing.T) {
	tests := []struct {
		name    string
		members []member
	}{
		{"an absolute name", []member{{name: "/etc/js-debug-evil", body: "x"}}},
		{"an absolute folder", []member{{name: "/tmp/js-debug-evil/", typ: tar.TypeDir}}},
		{"a name that climbs out", []member{{name: "../escape", body: "x"}}},
		{"a name that climbs out after going in", []member{{name: "js-debug/../../escape", body: "x"}}},
		{"a name that is only dots", []member{{name: "..", typ: tar.TypeDir}}},
		{"a folder that climbs out", []member{{name: "../folder/", typ: tar.TypeDir}}},
		{"a symbolic link to an absolute path", []member{{name: "js-debug/link", typ: tar.TypeSymlink, link: "/etc/passwd"}}},
		{"a symbolic link that climbs out", []member{{name: "js-debug/link", typ: tar.TypeSymlink, link: "../../outside"}}},
		{"a symbolic link in a folder that climbs out", []member{{name: "a/b/link", typ: tar.TypeSymlink, link: "../../../outside"}}},
		{"a symbolic link with no target", []member{{name: "js-debug/link", typ: tar.TypeSymlink, link: ""}}},
		{"a file written through a link to a folder above", []member{
			{name: "a/", typ: tar.TypeDir},
			{name: "a/up", typ: tar.TypeSymlink, link: "../.."},
			{name: "a/up/escape", body: "x"},
		}},
		{"a hard link", []member{{name: "js-debug/hard", typ: tar.TypeLink, link: "/etc/passwd"}}},
		{"a device", []member{{name: "js-debug/dev", typ: tar.TypeChar}}},
		{"a fifo", []member{{name: "js-debug/fifo", typ: tar.TypeFifo}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := t.TempDir()
			dir := filepath.Join(base, "data", "js-debug")
			rel, _ := serve(t, archiveOf(t, append(tt.members, goodMembers()...)...), http.StatusOK)
			_, err := download(t.Context(), http.DefaultClient, rel, dir)
			appErr := routeOf(t, err)
			if !appErr.HasRoute("JsDebugArchiveUnsafe") {
				t.Errorf("route = %s, want JsDebugArchiveUnsafe", appErr.Route())
			}
			// nothing was written outside the folder of the download, and nothing stays inside it
			var found []string
			err = filepath.WalkDir(base, func(p string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !d.IsDir() {
					found = append(found, p)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(found) != 0 {
				t.Errorf("files are left: %v", found)
			}
			for _, outside := range []string{filepath.Join(base, "escape"), filepath.Join(base, "data", "escape"), filepath.Join(base, "outside")} {
				if _, err := os.Lstat(outside); err == nil {
					t.Errorf("%s was written", outside)
				}
			}
		})
	}
}

func TestAnArchiveThatIsTooBigIsRefused(t *testing.T) {
	many := make([]member, 0, maxEntries+1)
	for i := 0; i <= maxEntries; i++ {
		many = append(many, member{name: "js-debug/d" + strconv.Itoa(i) + "/", typ: tar.TypeDir})
	}
	tests := []struct {
		name    string
		members []member
	}{
		{"more members than a release has", many},
		{"a file bigger than a file of a release", []member{{name: "js-debug/big.bin", size: maxEntryBytes + 1}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "js-debug")
			rel, _ := serve(t, archiveOf(t, tt.members...), http.StatusOK)
			_, err := download(t.Context(), http.DefaultClient, rel, dir)
			if appErr := routeOf(t, err); !appErr.HasRoute("unpack.JsDebugArchiveTooLarge") {
				t.Errorf("route = %s, want unpack.JsDebugArchiveTooLarge", appErr.Route())
			}
		})
	}
}

func TestALinkThatStaysInsideItsFolderIsKept(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "js-debug")
	members := append(goodMembers(), member{name: "js-debug/src/server.js", typ: tar.TypeSymlink, link: "dapDebugServer.js"})
	rel, _ := serve(t, archiveOf(t, members...), http.StatusOK)
	where, err := download(t.Context(), http.DefaultClient, rel, dir)
	if err != nil {
		t.Fatal(err)
	}
	if target, err := os.Readlink(filepath.Join(where, "src", "server.js")); err != nil || target != "dapDebugServer.js" {
		t.Errorf("link = %q %v", target, err)
	}
}

func TestThePinnedReleaseAgreesWithTheToolsFile(t *testing.T) {
	// scripts/tool-versions.txt pins the same archive for the tests; the two are changed together
	data, err := os.ReadFile(filepath.Join("..", "..", "scripts", "tool-versions.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var line string
	for _, l := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(l, "tgz") && strings.Contains(l, " js-debug ") {
			line = l
		}
	}
	rel := pinned()
	fields := strings.Fields(line)
	if len(fields) < 6 || fields[2] != rel.Version || fields[3] != rel.SHA256 || fields[4] != rel.URL {
		t.Errorf("tool-versions.txt has %q, the package pins %+v", line, rel)
	}
	if !strings.HasSuffix(rel.URL, "/v"+rel.Version+"/js-debug-dap-v"+rel.Version+".tar.gz") {
		t.Errorf("url = %s", rel.URL)
	}
}

func TestJsDebugIsFoundWhereTheUserOrTheDownloadPutIt(t *testing.T) {
	base := t.TempDir()
	data := filepath.Join(base, "data")
	release := fakeJsDebug(t, filepath.Join(base, "release"))
	direct := writeFile(t, base, "elsewhere/dapDebugServer.js", "//\n", 0o644)
	downloaded := fakeJsDebug(t, filepath.Join(data, JsDebugVersion))
	older := fakeJsDebug(t, filepath.Join(data, "1.2.0"))
	newer := fakeJsDebug(t, filepath.Join(data, "1.130.0"))
	tests := []struct {
		name    string
		setting string
		data    string
		want    string
		found   bool
		route   string
	}{
		{"the program itself", direct, "", direct, true, ""},
		{"the folder with src", filepath.Join(base, "release", "js-debug"), "", release, true, ""},
		{"the folder that holds js-debug", filepath.Join(base, "release"), "", release, true, ""},
		{"a setting wins over the download", direct, data, direct, true, ""},
		{"the download of the pinned version", "", data, downloaded, true, ""},
		{"nothing set and no data folder", "", "", "", false, ""},
		{"nothing set and nothing downloaded", "", filepath.Join(base, "nothing"), "", false, ""},
		{"a setting that is not js-debug", filepath.Join(base, "data"), "", "", false, "locate.JsDebugPathInvalid"},
		{"a setting that is a file of another name", writeFile(t, base, "x.js", "", 0o644), "", "", false, "locate.JsDebugPathInvalid"},
		{"a setting that is not there", filepath.Join(base, "missing"), "", "", false, "locate.JsDebugPathInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, found, err := locate(tt.setting, tt.data)
			if tt.route != "" {
				if appErr := routeOf(t, err); !appErr.HasRoute(tt.route) {
					t.Errorf("route = %s, want %s", appErr.Route(), tt.route)
				}
				return
			}
			if err != nil || found != tt.found || got != tt.want {
				t.Errorf("locate = %q %v %v, want %q %v", got, found, err, tt.want, tt.found)
			}
		})
	}
	// without the pinned version, the newest other release
	if err := os.RemoveAll(filepath.Join(data, JsDebugVersion)); err != nil {
		t.Fatal(err)
	}
	if got, found, err := locate("", data); err != nil || !found || got != newer {
		t.Errorf("locate with older releases = %q %v %v, want %q (not %q)", got, found, err, newer, older)
	}
}

func TestAMemberNameIsCheckedBeforeItIsUsed(t *testing.T) {
	root := t.TempDir()
	tests := []struct {
		name string
		ok   bool
	}{
		{"js-debug/src/a.js", true},
		{"./js-debug/a.js", true},
		{"js-debug/../js-debug/a.js", true},
		{"a/b/../../c", true},
		{"", false},
		{"/a", false},
		{"//a", false},
		{"..", false},
		{"../a", false},
		{"a/../../a", false},
		{"a\x00b", false},
	}
	for _, tt := range tests {
		got, err := memberPath(root, tt.name)
		if (err == nil) != tt.ok {
			t.Errorf("memberPath(%q) = %q, %v, want ok = %v", tt.name, got, err, tt.ok)
			continue
		}
		if err == nil && !within(root, got) {
			t.Errorf("memberPath(%q) = %q is not below %q", tt.name, got, root)
		}
		if err != nil && !routeOf(t, err).HasRoute("memberPath.JsDebugArchiveUnsafe") {
			t.Errorf("memberPath(%q) failed with %s", tt.name, routeOf(t, err).Route())
		}
	}
}

func TestAFolderOrFileThatCannotBeWrittenFailsTheInstall(t *testing.T) {
	good := archiveOf(t, goodMembers()...)
	tests := []struct {
		name  string
		run   func(t *testing.T, base string) error
		route string
	}{
		{"the data folder is below a file", func(t *testing.T, base string) error {
			file := writeFile(t, base, "file", "x", 0o644)
			rel, _ := serve(t, good, http.StatusOK)
			_, err := download(t.Context(), http.DefaultClient, rel, filepath.Join(file, "js-debug"))
			return err
		}, "download.JsDebugInstallFailed"},
		{"the folder of the download cannot be written to", func(t *testing.T, base string) error {
			if os.Geteuid() == 0 {
				t.Skip("root writes everywhere")
			}
			dir := filepath.Join(base, "locked")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(dir, 0o500); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
			rel, _ := serve(t, good, http.StatusOK)
			_, err := download(t.Context(), http.DefaultClient, rel, dir)
			return err
		}, "download.JsDebugInstallFailed"},
		{"the file of the archive cannot be made", func(t *testing.T, base string) error {
			rel, _ := serve(t, good, http.StatusOK)
			return fetch(t.Context(), http.DefaultClient, rel, filepath.Join(base, "missing", "release.tar.gz"))
		}, "fetch.JsDebugInstallFailed"},
		{"the archive is not there", func(t *testing.T, base string) error {
			return unpack(filepath.Join(base, "nothing.tar.gz"), filepath.Join(base, "out"))
		}, "unpack.JsDebugInstallFailed"},
		{"the folder to unpack into is below a file", func(t *testing.T, base string) error {
			file := writeFile(t, base, "file", "x", 0o644)
			archive := writeFile(t, base, "a.tar.gz", string(good), 0o644)
			return unpack(archive, filepath.Join(file, "out"))
		}, "unpack.JsDebugInstallFailed"},
		{"a folder where a file is", func(t *testing.T, base string) error {
			archive := writeFile(t, base, "a.tar.gz", string(archiveOf(t, member{name: "a", body: "x"}, member{name: "a/", typ: tar.TypeDir})), 0o644)
			return unpack(archive, filepath.Join(base, "out"))
		}, "unpack.JsDebugInstallFailed"},
		{"a file below a file", func(t *testing.T, base string) error {
			archive := writeFile(t, base, "a.tar.gz", string(archiveOf(t, member{name: "a", body: "x"}, member{name: "a/b", body: "y"})), 0o644)
			return unpack(archive, filepath.Join(base, "out"))
		}, "safeParent.JsDebugInstallFailed"},
		{"a file twice", func(t *testing.T, base string) error {
			archive := writeFile(t, base, "a.tar.gz", string(archiveOf(t, member{name: "a", body: "x"}, member{name: "a", body: "y"})), 0o644)
			return unpack(archive, filepath.Join(base, "out"))
		}, "writeMember.JsDebugInstallFailed"},
		{"a link twice", func(t *testing.T, base string) error {
			archive := writeFile(t, base, "a.tar.gz", string(archiveOf(t, member{name: "a", typ: tar.TypeSymlink, link: "b"}, member{name: "a", typ: tar.TypeSymlink, link: "c"})), 0o644)
			return unpack(archive, filepath.Join(base, "out"))
		}, "linkMember.JsDebugInstallFailed"},
		{"a file that is cut off in the archive", func(t *testing.T, base string) error {
			// the tar says 100 bytes and has 5: the gzip is complete, the tar is not
			var buf bytes.Buffer
			zw := gzip.NewWriter(&buf)
			tw := tar.NewWriter(zw)
			if err := tw.WriteHeader(&tar.Header{Name: "a", Typeflag: tar.TypeReg, Mode: 0o644, Size: 100}); err != nil {
				t.Fatal(err)
			}
			_, _ = tw.Write([]byte("12345"))
			_ = zw.Close()
			archive := writeFile(t, base, "a.tar.gz", buf.String(), 0o644)
			return unpack(archive, filepath.Join(base, "out"))
		}, "writeMember.JsDebugArchiveBroken"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.run(t, t.TempDir())
			if appErr := routeOf(t, err); !appErr.HasRoute(tt.route) {
				t.Errorf("route = %s, want it to have %s", appErr.Route(), tt.route)
			}
		})
	}
}

func TestADataFolderThatIsUnknownIsToldWithItsTag(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", "")
	_, err := dataDirFromEnv()
	if appErr := routeOf(t, err); !appErr.HasRoute("dataDirFromEnv.JsDebugDataDirUnknown") {
		t.Errorf("route = %s", appErr.Route())
	}
}
