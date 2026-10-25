package icons_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/assets/icons"
)

const (
	appID       = "dev.emetos.Emetos"
	desktopFile = "../" + appID + ".desktop"
	script      = "../../scripts/install-desktop.sh"
)

// iconSizes are the sizes the script installs: every picture of assets/icons.
var iconSizes = []int{16, 22, 24, 32, 48, 64, 128, 256, 512}

// desktopKeys reads the keys of the "Desktop Entry" group of the desktop file.
func desktopKeys(t *testing.T, path string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]string{}
	in := false
	for _, line := range strings.Split(string(data), "\n") {
		switch {
		case strings.HasPrefix(line, "["):
			in = line == "[Desktop Entry]"
		case in && strings.Contains(line, "="):
			k, v, _ := strings.Cut(line, "=")
			keys[k] = v
		}
	}
	return keys
}

func TestDesktopEntry(t *testing.T) {
	keys := desktopKeys(t, desktopFile)
	for _, tc := range []struct {
		key, want string
		contains  bool
	}{
		{"Type", "Application", false},
		{"Name", "Emetos", false},
		{"Exec", "emetos %F", false},
		{"Icon", appID, false},
		{"StartupWMClass", appID, false},
		{"Terminal", "false", false},
		{"Categories", "Development;", true},
		{"Categories", "TextEditor;", true},
		{"Categories", "IDE;", true},
		{"MimeType", "text/plain;", true},
		{"MimeType", "inode/directory;", true},
	} {
		got, ok := keys[tc.key]
		if !ok || (!tc.contains && got != tc.want) || (tc.contains && !strings.Contains(got, tc.want)) {
			t.Errorf("%s = %q (present %v), want %q (contains: %v)", tc.key, got, ok, tc.want, tc.contains)
		}
	}
}

func TestDesktopEntryIsValid(t *testing.T) {
	validate, err := exec.LookPath("desktop-file-validate")
	if err != nil {
		t.Skip("desktop-file-validate is not installed")
	}
	if out, err := exec.Command(validate, desktopFile).CombinedOutput(); err != nil {
		t.Errorf("desktop-file-validate: %v\n%s", err, out)
	}
}

// rig is a private data folder, a folder of fake programs for PATH, and a way to run the script in them.
type rig struct {
	t    *testing.T
	home string // HOME
	data string // XDG_DATA_HOME
	bin  string // fake programs, first in PATH
	log  string // the calls the fake cache tools recorded
}

func newRig(t *testing.T) *rig {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is not installed")
	}
	dir := t.TempDir()
	r := &rig{t: t, home: filepath.Join(dir, "home"), data: filepath.Join(dir, "data"), bin: filepath.Join(dir, "bin"), log: filepath.Join(dir, "calls.log")}
	for _, d := range []string{r.home, r.bin} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

// program makes an executable in the fake PATH folder that appends its name and arguments to the log.
func (r *rig) program(name string) string {
	r.t.Helper()
	p := filepath.Join(r.bin, name)
	script := "#!/bin/sh\necho \"" + name + " $*\" >> " + r.log + "\n"
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		r.t.Fatal(err)
	}
	return p
}

// run runs the script with only the given environment: no variable of the real session reaches it. The PATH is
// the fake folder and /usr/bin:/bin (the tools the script needs); without cacheTools the fake update-desktop-database
// and gtk-update-icon-cache are not there, and the real ones are hidden by a PATH of links to the other tools.
func (r *rig) run(env []string, args ...string) (string, int) {
	r.t.Helper()
	cmd := exec.Command("bash", append([]string{script}, args...)...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			r.t.Fatal(err)
		}
		code = ee.ExitCode()
	}
	return string(out), code
}

// toolsOnly makes a folder with links to just the programs the script uses, so that a real
// update-desktop-database or gtk-update-icon-cache of the machine is not found.
func (r *rig) toolsOnly() string {
	r.t.Helper()
	dir := filepath.Join(r.t.TempDir(), "tools")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		r.t.Fatal(err)
	}
	for _, name := range []string{"bash", "sed", "cp", "chmod", "mkdir", "rm", "rmdir", "dirname", "basename", "cat"} {
		p, err := exec.LookPath(name)
		if err != nil {
			r.t.Skipf("%s is not installed", name)
		}
		if err := os.Symlink(p, filepath.Join(dir, name)); err != nil {
			r.t.Fatal(err)
		}
	}
	return dir
}

func (r *rig) env(path string) []string {
	return []string{"PATH=" + path, "HOME=" + r.home, "XDG_DATA_HOME=" + r.data}
}

func (r *rig) calls() string {
	b, err := os.ReadFile(r.log)
	if err != nil {
		return ""
	}
	return string(b)
}

func TestInstallDesktop(t *testing.T) {
	t.Run("installs the entry and every icon, and refreshes the caches", func(t *testing.T) {
		r := newRig(t)
		emetos := r.program("emetos")
		r.program("update-desktop-database")
		r.program("gtk-update-icon-cache")
		out, code := r.run(r.env(r.bin + ":/usr/bin:/bin"))
		if code != 0 {
			t.Fatalf("exit %d: %s", code, out)
		}
		keys := desktopKeys(t, filepath.Join(r.data, "applications", appID+".desktop"))
		if keys["Exec"] != emetos+" %F" || keys["Icon"] != appID {
			t.Errorf("installed entry has Exec %q and Icon %q, want %q and %q", keys["Exec"], keys["Icon"], emetos+" %F", appID)
		}
		for _, size := range iconSizes {
			p := filepath.Join(r.data, "icons", "hicolor", sizeDir(size), "apps", appID+".png")
			got, err := os.ReadFile(p)
			want, _ := icons.PNG(size)
			if err != nil || string(got) != string(want) {
				t.Errorf("icon of %d px: %v, same as assets/icons: %v", size, err, string(got) == string(want))
			}
		}
		calls := r.calls()
		if !strings.Contains(calls, "update-desktop-database -q "+filepath.Join(r.data, "applications")) ||
			!strings.Contains(calls, "gtk-update-icon-cache -q -f -t "+filepath.Join(r.data, "icons", "hicolor")) {
			t.Errorf("the cache tools were called as:\n%s", calls)
		}
		if _, err := os.Stat(filepath.Join(r.home, ".local")); !os.IsNotExist(err) {
			t.Errorf("the home folder was written to (stat error %v)", err)
		}
	})

	t.Run("works without the cache tools", func(t *testing.T) {
		r := newRig(t)
		tools := r.toolsOnly()
		emetos := filepath.Join(r.bin, "emetos")
		if err := os.WriteFile(emetos, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		out, code := r.run(r.env(tools), "--exec", emetos)
		if code != 0 {
			t.Fatalf("exit %d: %s", code, out)
		}
		if _, err := os.Stat(filepath.Join(r.data, "applications", appID+".desktop")); err != nil {
			t.Error(err)
		}
		if r.calls() != "" {
			t.Errorf("a cache tool ran: %s", r.calls())
		}
	})

	t.Run("keeps the plain name when emetos is not found and says so", func(t *testing.T) {
		r := newRig(t)
		out, code := r.run(r.env(r.toolsOnly()))
		if code != 0 || !strings.Contains(out, "emetos was not found") {
			t.Fatalf("exit %d: %s", code, out)
		}
		if got := desktopKeys(t, filepath.Join(r.data, "applications", appID+".desktop"))["Exec"]; got != "emetos %F" {
			t.Errorf("Exec = %q, want the plain name", got)
		}
	})

	t.Run("uses HOME/.local/share when XDG_DATA_HOME is not absolute", func(t *testing.T) {
		r := newRig(t)
		env := []string{"PATH=" + r.toolsOnly(), "HOME=" + r.home, "XDG_DATA_HOME=relative/data"}
		if out, code := r.run(env); code != 0 {
			t.Fatalf("exit %d: %s", code, out)
		}
		if _, err := os.Stat(filepath.Join(r.home, ".local", "share", "applications", appID+".desktop")); err != nil {
			t.Error(err)
		}
	})

	for _, tc := range []struct {
		name string
		env  func(r *rig) []string
		args []string
		code int
		msg  string
	}{
		{"no usable data folder", func(r *rig) []string { return []string{"PATH=" + r.toolsOnly(), "HOME=relative"} }, nil, 1, "absolute"},
		{"a program that is not there", func(r *rig) []string { return r.env(r.toolsOnly()) }, []string{"--exec", "/nonexistent/emetos"}, 1, "not an executable"},
		{"a path with a space", func(r *rig) []string { return r.env(r.toolsOnly()) }, []string{"--exec", "/tmp/a b/emetos"}, 1, "absolute and made of"},
		{"an unknown option", func(r *rig) []string { return r.env(r.toolsOnly()) }, []string{"--nope"}, 64, "usage"},
		{"--exec without a value", func(r *rig) []string { return r.env(r.toolsOnly()) }, []string{"--exec"}, 64, "usage"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			out, code := r.run(tc.env(r), tc.args...)
			if code != tc.code || !strings.Contains(out, tc.msg) {
				t.Errorf("exit %d (want %d), output %q (want it to contain %q)", code, tc.code, out, tc.msg)
			}
			if _, err := os.Stat(filepath.Join(r.data, "applications")); !os.IsNotExist(err) {
				t.Errorf("a failed run created the applications folder (stat error %v)", err)
			}
		})
	}
}

func TestUninstallDesktop(t *testing.T) {
	r := newRig(t)
	tools := r.toolsOnly()
	emetos := filepath.Join(r.bin, "emetos")
	if err := os.WriteFile(emetos, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Another icon in the same theme folders must survive.
	other := filepath.Join(r.data, "icons", "hicolor", "48x48", "apps", "other.png")
	if err := os.MkdirAll(filepath.Dir(other), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, code := r.run(r.env(tools), "--exec", emetos); code != 0 {
		t.Fatalf("install: exit %d: %s", code, out)
	}
	out, code := r.run(r.env(tools), "--uninstall")
	if code != 0 {
		t.Fatalf("uninstall: exit %d: %s", code, out)
	}
	var left []string
	if err := filepath.WalkDir(r.data, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			left = append(left, strings.TrimPrefix(p, r.data+string(filepath.Separator)))
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if want := "icons/hicolor/48x48/apps/other.png"; len(left) != 1 || left[0] != want {
		t.Errorf("files left after uninstall: %v, want only %s", left, want)
	}
	// A second uninstall finds nothing and succeeds.
	if out, code := r.run(r.env(tools), "--uninstall"); code != 0 {
		t.Errorf("second uninstall: exit %d: %s", code, out)
	}
}

func sizeDir(size int) string {
	n := strconv.Itoa(size)
	return n + "x" + n
}
