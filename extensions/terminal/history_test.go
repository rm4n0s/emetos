package terminal

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/rm4n0s/errors"
)

func testStore(t *testing.T, limit int) (*historyStore, string) {
	t.Helper()
	dir := t.TempDir()
	return newHistoryStore(func() (string, error) { return dir, nil }, func() int { return limit }), filepath.Join(dir, historyFile)
}

func TestHistoryStore(t *testing.T) {
	h, path := testStore(t, 3)
	for _, c := range []string{"one", "two", "three", "two", "four", "  ", ""} {
		if err := h.AddCommand(c, "bash"); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.AddCommand("zsh-cmd", "zsh"); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"/a", "/b", "/a", "", "/c", "/d"} {
		if err := h.AddDir(d); err != nil {
			t.Fatal(err)
		}
	}
	cmds, dirs, err := h.snapshot("")
	if err != nil {
		t.Fatal(err)
	}
	// the newest first; the limit of 3 keeps the last three
	if want := []string{"zsh-cmd", "four", "two"}; !reflect.DeepEqual(cmds, want) {
		t.Errorf("commands = %q, want %q", cmds, want)
	}
	if want := []string{"/d", "/c", "/a"}; !reflect.DeepEqual(dirs, want) {
		t.Errorf("dirs = %q, want %q", dirs, want)
	}
	if bash, _, _ := h.snapshot("bash"); !reflect.DeepEqual(bash, []string{"four", "two"}) {
		t.Errorf("bash commands = %q", bash)
	}
	// the file is private and another store reads it
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("file: %v %v", info, err)
	}
	other := newHistoryStore(h.dir, h.limit)
	if again, _, err := other.snapshot(""); err != nil || !reflect.DeepEqual(again, cmds) {
		t.Errorf("a second store reads %q, %v", again, err)
	}
	if err := h.RemoveCommand("four"); err != nil {
		t.Fatal(err)
	}
	if err := h.RemoveDir("/c"); err != nil {
		t.Fatal(err)
	}
	cmds, dirs, _ = h.snapshot("")
	if !reflect.DeepEqual(cmds, []string{"zsh-cmd", "two"}) || !reflect.DeepEqual(dirs, []string{"/d", "/a"}) {
		t.Errorf("after removing: %q %q", cmds, dirs)
	}
	if err := h.Clear(); err != nil {
		t.Fatal(err)
	}
	cmds, dirs, _ = h.snapshot("")
	if len(cmds) != 0 || len(dirs) != 2 {
		t.Errorf("after Clear: %q %q", cmds, dirs)
	}
}

func TestHistoryStoreLimitZeroKeepsNothing(t *testing.T) {
	h, path := testStore(t, 0)
	if err := h.AddCommand("x", "bash"); err != nil {
		t.Fatal(err)
	}
	if err := h.AddDir("/x"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Errorf("the file was written with a limit of 0")
	}
}

func TestHistoryStoreConcurrent(t *testing.T) {
	h, _ := testStore(t, 1000)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range 10 {
				if err := h.AddCommand(strings.Repeat("c", i+1)+string(rune('a'+j)), "bash"); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	if cmds, _, err := h.snapshot(""); err != nil || len(cmds) != 80 {
		t.Errorf("%d commands, %v; want 80", len(cmds), err)
	}
}

func TestHistoryStoreErrors(t *testing.T) {
	h, path := testStore(t, 10)
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := h.snapshot("")
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("reload.TerminalHistoryInvalid") {
		t.Fatalf("invalid file: %v", err)
	}
	// a change replaces a file that cannot be read, and reports it
	err = h.AddCommand("x", "bash")
	if _, ok := errors.FromError(err); !ok {
		t.Errorf("AddCommand over an invalid file: %v", err)
	}
	if cmds, _, err := h.snapshot(""); err != nil || !reflect.DeepEqual(cmds, []string{"x"}) {
		t.Errorf("after the change: %q %v", cmds, err)
	}
	// a file others can write is not read
	if err := os.Chmod(path, 0o666); err != nil {
		t.Fatal(err)
	}
	if _, _, err := h.snapshot(""); err == nil {
		t.Errorf("a world-writable file was read")
	} else if appErr, ok := errors.FromError(err); !ok || !appErr.HasRoute("StateFileUnsafeMode") {
		t.Errorf("unsafe mode: %v", err)
	}
	// no state folder
	bad := newHistoryStore(func() (string, error) { return "", errors.New("StateFileDirUnknown", "no folder") }, func() int { return 5 })
	if err := bad.AddCommand("x", "bash"); err == nil {
		t.Errorf("a store without a folder wrote")
	}
}

// The cases of the parsers are those of the shells' formats (VS Code's history.ts has no test of its own; these follow its
// code and the documented formats of each file).
func TestParseShellHistory(t *testing.T) {
	cases := []struct {
		name  string
		parse func(string) []string
		in    string
		want  []string
	}{
		{"bash: one a line, repeats once, trimmed", parseBashHistory, "ls\n  cd /tmp \nls\n\nmake\n", []string{"ls", "cd /tmp", "make"}},
		{"bash: an open quote joins lines", parseBashHistory, "echo 'a\nb'\nls\n", []string{"echo 'a\nb'", "ls"}},
		{"bash: a closed quote does not", parseBashHistory, "echo \"a\" 'b'\nls\n", []string{`echo "a" 'b'`, "ls"}},
		{"zsh: plain", parseZshHistory, "ls\ncd /tmp\nls\n", []string{"ls", "cd /tmp"}},
		{"zsh: a backslash continues", parseZshHistory, "echo a\\\nb\nls\n", []string{"echo a\nb", "ls"}},
		{"zsh: extended", parseZshHistory, ": 1700000000:0;ls -la\n: 1700000001:2;git status\n: 1700000002:0;ls -la\n", []string{"ls -la", "git status"}},
		{"zsh: extended with a continuation", parseZshHistory, ": 1700000000:0;echo a\\\nb\n: 1700000001:0;ls\n", []string{"echo a\nb", "ls"}},
		{"pwsh: plain", parsePwshHistory, "Get-ChildItem\nSet-Location /tmp\nGet-ChildItem\n", []string{"Get-ChildItem", "Set-Location /tmp"}},
		{"pwsh: a backtick continues", parsePwshHistory, "Get-ChildItem `\n-Force\nls\n", []string{"Get-ChildItem \n-Force", "ls"}},
		{"pwsh: two backticks end", parsePwshHistory, "echo ``\nls\n", []string{"echo ``", "ls"}},
		{"fish: cmd lines", parseFishHistory, "- cmd: ls -la\n  when: 1700000000\n- cmd:echo hi\n  when: 1\n- cmd: ls -la\n", []string{"ls -la", "echo hi"}},
		{"fish: \\n is a line break and \\\\n is not", parseFishHistory, "- cmd: echo a\\nb\n- cmd: echo a\\\\nb\n", []string{"echo a\nb", `echo a\\nb`}},
		{"fish: other lines are ignored", parseFishHistory, "# comment\n  - cmd: nested\n- cmd:\n", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.parse(tc.in); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestShellHistoryFile(t *testing.T) {
	home := t.TempDir()
	data := t.TempDir()
	writeFile(t, filepath.Join(home, ".bash_history"), "ls\ncd /\n")
	writeFile(t, filepath.Join(home, ".zsh_history"), ": 1:0;zls\n")
	writeFile(t, filepath.Join(home, ".local/share/fish/fish_history"), "- cmd: fls\n")
	writeFile(t, filepath.Join(data, "fish/fish_history"), "- cmd: xdg fish\n")
	writeFile(t, filepath.Join(home, ".local/share/powershell/PSReadline/ConsoleHost_history.txt"), "Get-Date\n")
	cases := []struct {
		shell, dataHome, label string
		want                   []string
	}{
		{"bash", "", "~/.bash_history", []string{"ls", "cd /"}},
		{"zsh", "", "~/.zsh_history", []string{"zls"}},
		{"fish", "", "~/.local/share/fish/fish_history", []string{"fls"}},
		{"fish", data, "$XDG_DATA_HOME/fish/fish_history", []string{"xdg fish"}},
		{"pwsh", "", "~/.local/share/powershell/PSReadline/ConsoleHost_history.txt", []string{"Get-Date"}},
	}
	for _, tc := range cases {
		got := shellHistoryFile(tc.shell, home, tc.dataHome)
		if got == nil || got.label != tc.label || !reflect.DeepEqual(got.commands, tc.want) {
			t.Errorf("%s (%q): %+v, want %q %q", tc.shell, tc.dataHome, got, tc.label, tc.want)
		}
	}
	if got := shellHistoryFile("sh", home, ""); got != nil {
		t.Errorf("sh has no history file: %+v", got)
	}
	if got := shellHistoryFile("bash", t.TempDir(), ""); got != nil {
		t.Errorf("no file: %+v", got)
	}
	if got := shellHistoryFile("bash", "", ""); got != nil {
		t.Errorf("no home: %+v", got)
	}
}
