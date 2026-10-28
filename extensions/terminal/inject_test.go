package terminal

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
)

// The cases are those of VS Code's terminalEnvironment.test.ts (getShellIntegrationInjection), with the paths of Linux and
// the application name emetos; the cases the test file skips on Linux (pwsh) are run here with what the source does.
//
// Ported from VS Code: src/vs/platform/terminal/test/node/terminalEnvironment.test.ts

const testRoot = "/scripts"

func testInject() injectOpts {
	return injectOpts{enabled: true, root: testRoot, home: "/home/user", tmp: "/tmp", username: "user", appName: "emetos", skipStickyBit: true}
}

func TestShellIntegrationInjection(t *testing.T) {
	disabled := testInject()
	disabled.enabled = false
	customZ := testInject()
	customZ.zdotdir = "/custom/zsh/dotdir"
	withNonce := testInject()
	withNonce.nonce = "n0nce"
	pwshArgs := []string{"-noexit", "-command", `. "` + testRoot + `/shellIntegration.ps1"`}
	pwshLogin := []string{"-l", "-noexit", "-command", `. "` + testRoot + `/shellIntegration.ps1"`}
	fishCmd := `set --global TERM_PROGRAM vscode; source "` + testRoot + `/shellIntegration.fish"; set --global TERM_PROGRAM emetos`
	cases := []struct {
		name     string
		shell    string
		args     []string
		opts     injectOpts
		wantArgs []string
		wantEnv  map[string]string
		reason   string
	}{
		{name: "the setting is off", shell: "bash", opts: disabled, reason: injectDisabled},
		{name: "a profile with the setting off and arguments", shell: "bash", args: []string{"-l"}, opts: disabled, reason: injectDisabled},
		{name: "no program", shell: "", opts: testInject(), reason: injectNoExecutable},
		{name: "an unsupported shell", shell: "julia", args: []string{"-i"}, opts: withNonce, reason: injectUnsupported},
		{name: "an unsupported shell is found by its base name", shell: "/usr/bin/sh", opts: testInject(), reason: injectUnsupported},

		{name: "bash with no arguments", shell: "bash", opts: testInject(),
			wantArgs: []string{"--init-file", testRoot + "/shellIntegration-bash.sh"}, wantEnv: map[string]string{"VSCODE_INJECTION": "1", "VSCODE_STABLE": "1"}},
		{name: "bash by its path", shell: "/usr/bin/bash", args: []string{}, opts: testInject(),
			wantArgs: []string{"--init-file", testRoot + "/shellIntegration-bash.sh"}, wantEnv: map[string]string{"VSCODE_INJECTION": "1", "VSCODE_STABLE": "1"}},
		{name: "bash with the nonce", shell: "bash", opts: withNonce,
			wantArgs: []string{"--init-file", testRoot + "/shellIntegration-bash.sh"},
			wantEnv:  map[string]string{"VSCODE_INJECTION": "1", "VSCODE_STABLE": "1", "VSCODE_NONCE": "n0nce"}},
		{name: "bash login sets the variable and keeps the init file", shell: "bash", args: []string{"-l"}, opts: testInject(),
			wantArgs: []string{"--init-file", testRoot + "/shellIntegration-bash.sh"},
			wantEnv:  map[string]string{"VSCODE_INJECTION": "1", "VSCODE_STABLE": "1", "VSCODE_SHELL_LOGIN": "1"}},
		{name: "bash --login with the interactive flag", shell: "bash", args: []string{"--login", "-i"}, opts: testInject(),
			wantArgs: []string{"--init-file", testRoot + "/shellIntegration-bash.sh"},
			wantEnv:  map[string]string{"VSCODE_INJECTION": "1", "VSCODE_STABLE": "1", "VSCODE_SHELL_LOGIN": "1"}},
		{name: "bash with other arguments", shell: "bash", args: []string{"-l", "-x"}, opts: testInject(), reason: injectUnsupportedArg},
		{name: "bash --norc is not combined", shell: "bash", args: []string{"--norc"}, opts: testInject(), reason: injectUnsupportedArg},

		{name: "zsh with no arguments", shell: "zsh", opts: testInject(), wantArgs: []string{"-i"}},
		{name: "zsh login", shell: "zsh", args: []string{"-l"}, opts: testInject(), wantArgs: []string{"-il"}},
		{name: "zsh keeps the arguments of the script itself", shell: "zsh", args: []string{"-il"}, opts: testInject(), wantArgs: []string{"-il"}},
		{name: "zsh with an unknown argument", shell: "zsh", args: []string{"-l", "-fake"}, opts: testInject(), reason: injectUnsupportedArg},
		{name: "zsh with a custom ZDOTDIR", shell: "zsh", opts: customZ, wantArgs: []string{"-i"}},

		{name: "fish with no arguments", shell: "fish", opts: testInject(), wantArgs: []string{"--init-command", fishCmd}, wantEnv: map[string]string{"VSCODE_INJECTION": "1"}},
		{name: "fish login", shell: "fish", args: []string{"--login"}, opts: testInject(), wantArgs: []string{"-l", "--init-command", fishCmd}, wantEnv: map[string]string{"VSCODE_INJECTION": "1"}},
		{name: "fish with its own arguments", shell: "fish", args: argsFish, opts: testInject(), wantArgs: []string{"--init-command", fishCmd}, wantEnv: map[string]string{"VSCODE_INJECTION": "1"}},
		{name: "fish with other arguments", shell: "fish", args: []string{"-N"}, opts: testInject(), reason: injectUnsupportedArg},

		{name: "pwsh with no arguments", shell: "pwsh", opts: testInject(), wantArgs: pwshArgs, wantEnv: map[string]string{"VSCODE_INJECTION": "1", "VSCODE_STABLE": "1"}},
		{name: "pwsh -NoLogo, case does not matter", shell: "pwsh", args: []string{"-NoLogo"}, opts: testInject(), wantArgs: pwshArgs, wantEnv: map[string]string{"VSCODE_INJECTION": "1", "VSCODE_STABLE": "1"}},
		{name: "pwsh -NOL", shell: "pwsh", args: []string{"-NOL"}, opts: testInject(), wantArgs: pwshArgs, wantEnv: map[string]string{"VSCODE_INJECTION": "1", "VSCODE_STABLE": "1"}},
		{name: "pwsh -l", shell: "pwsh", args: []string{"-l"}, opts: testInject(), wantArgs: pwshLogin, wantEnv: map[string]string{"VSCODE_INJECTION": "1", "VSCODE_STABLE": "1"}},
		{name: "pwsh -l -NoLogo", shell: "pwsh", args: []string{"-l", "-NoLogo"}, opts: testInject(), wantArgs: pwshLogin, wantEnv: map[string]string{"VSCODE_INJECTION": "1", "VSCODE_STABLE": "1"}},
		{name: "pwsh -NoLogo -login", shell: "pwsh", args: []string{"-NoLogo", "-login"}, opts: testInject(), wantArgs: pwshLogin, wantEnv: map[string]string{"VSCODE_INJECTION": "1", "VSCODE_STABLE": "1"}},
		{name: "pwsh with an unrecognized argument", shell: "pwsh", args: []string{"-l", "-NoLogo", "-i"}, opts: testInject(), reason: injectUnsupportedArg},
		{name: "pwsh -i", shell: "pwsh", args: []string{"-i"}, opts: testInject(), reason: injectUnsupportedArg},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := slices.Clone(tc.args)
			inj, reason := shellIntegrationInjection(tc.shell, tc.args, tc.opts)
			if reason != tc.reason {
				t.Fatalf("reason = %q, want %q", reason, tc.reason)
			}
			if !slices.Equal(tc.args, before) {
				t.Errorf("the arguments of the profile were changed: %q", tc.args)
			}
			if reason != "" {
				return
			}
			if !slices.Equal(inj.args, tc.wantArgs) {
				t.Errorf("args = %q, want %q", inj.args, tc.wantArgs)
			}
			if tc.shell != "zsh" {
				if !maps.Equal(inj.env, tc.wantEnv) {
					t.Errorf("env = %v, want %v", inj.env, tc.wantEnv)
				}
				if len(inj.files) != 0 {
					t.Errorf("files = %v, want none", inj.files)
				}
			}
		})
	}
}

// TestShellIntegrationZsh checks the ZDOTDIR of zsh: the folder is named after the user and the application, the user's
// ZDOTDIR is passed on, and the four scripts go to the four dot files.
func TestShellIntegrationZsh(t *testing.T) {
	customZ := testInject()
	customZ.zdotdir = "/custom/zsh/dotdir"
	noHome := testInject()
	noHome.home = ""
	unknownUser := testInject()
	unknownUser.username = ""
	cases := []struct {
		name     string
		opts     injectOpts
		userZdot string
		zdotdir  string
	}{
		{"the home folder is the user's ZDOTDIR", testInject(), "/home/user", "/tmp/user-emetos-zsh"},
		{"a ZDOTDIR of the environment wins", customZ, "/custom/zsh/dotdir", "/tmp/user-emetos-zsh"},
		{"with no home folder the tilde is passed", noHome, "~", "/tmp/user-emetos-zsh"},
		{"an unknown user", unknownUser, "/home/user", "/tmp/unknown-emetos-zsh"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inj, reason := shellIntegrationInjection("zsh", nil, tc.opts)
			if reason != "" {
				t.Fatalf("reason = %q", reason)
			}
			wantEnv := map[string]string{"VSCODE_INJECTION": "1", "ZDOTDIR": tc.zdotdir, "USER_ZDOTDIR": tc.userZdot}
			if !maps.Equal(inj.env, wantEnv) {
				t.Errorf("env = %v, want %v", inj.env, wantEnv)
			}
			if len(inj.files) != 4 {
				t.Fatalf("files = %v, want 4", inj.files)
			}
			wantFiles := [][2]string{
				{testRoot + "/shellIntegration-rc.zsh", tc.zdotdir + "/.zshrc"}, {testRoot + "/shellIntegration-profile.zsh", tc.zdotdir + "/.zprofile"},
				{testRoot + "/shellIntegration-env.zsh", tc.zdotdir + "/.zshenv"}, {testRoot + "/shellIntegration-login.zsh", tc.zdotdir + "/.zlogin"},
			}
			for i, w := range wantFiles {
				if inj.files[i].src != w[0] || inj.files[i].dst != w[1] {
					t.Errorf("file %d = %v, want %v", i, inj.files[i], w)
				}
			}
		})
	}
}

func TestWriteScripts(t *testing.T) {
	base := t.TempDir()
	root, err := writeScripts(base)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := shellScripts.ReadDir(scriptsDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 7 {
		t.Errorf("the embedded scripts are %d, want 7", len(entries))
	}
	for _, e := range entries {
		want, err := shellScripts.ReadFile(scriptsDir + "/" + e.Name())
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(root, e.Name()))
		if err != nil || string(got) != string(want) {
			t.Errorf("%s: err %v, content differs: %v", e.Name(), err, string(got) != string(want))
		}
	}
	// a script that was changed or removed is written again, and a second call changes nothing else
	bash := filepath.Join(root, "shellIntegration-bash.sh")
	if err := os.WriteFile(bash, []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "shellIntegration.fish")); err != nil {
		t.Fatal(err)
	}
	again, err := writeScripts(base)
	if err != nil || again != root {
		t.Fatalf("second call = %q, %v", again, err)
	}
	got, err := os.ReadFile(bash)
	if err != nil || strings.Contains(string(got), "tampered") {
		t.Errorf("the changed script was not repaired: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "shellIntegration.fish")); err != nil {
		t.Errorf("the removed script was not written again: %v", err)
	}
}

func TestWriteScriptsFails(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := writeScripts(filepath.Join(blocker, "cache")) // a folder cannot be made under a file
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute("writeScripts.TerminalScriptsWriteFailed") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
	// a script that cannot be replaced
	base := t.TempDir()
	root, err := writeScripts(base)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "shellIntegration.fish")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o700) })
	if os.Geteuid() != 0 {
		_, err = writeScripts(base)
		appErr, ok = errors.FromError(err)
		if !ok || !appErr.HasRoute("writeScripts.TerminalScriptsWriteFailed") {
			t.Errorf("a read-only folder: err = %v", err)
		}
	}
}

func TestPrepareZdotdir(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "rc"), []byte("# rc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	zdot := filepath.Join(dir, "zdot")
	inj := injection{zdotdir: zdot, files: []fileCopy{{src: filepath.Join(src, "rc"), dst: filepath.Join(zdot, ".zshrc")}}}
	if err := prepareZdotdir(inj, false); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(zdot)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSticky == 0 || info.Mode().Perm() != 0o700 {
		t.Errorf("mode = %v, want the sticky bit and 0700", info.Mode())
	}
	if got, err := os.ReadFile(filepath.Join(zdot, ".zshrc")); err != nil || string(got) != "# rc\n" {
		t.Errorf(".zshrc = %q, %v", got, err)
	}
	// a second call works over the folder that is there
	if err := prepareZdotdir(inj, false); err != nil {
		t.Errorf("second call: %v", err)
	}
	// nothing to do for another shell
	if err := prepareZdotdir(injection{}, false); err != nil {
		t.Errorf("no zdotdir: %v", err)
	}

	for _, tc := range []struct {
		name string
		inj  injection
		skip bool
	}{
		{"the folder cannot be made", injection{zdotdir: filepath.Join(src, "rc", "z"), files: nil}, false},
		{"a script is missing", injection{zdotdir: zdot, files: []fileCopy{{src: filepath.Join(src, "none"), dst: filepath.Join(zdot, ".zshrc")}}}, true},
		{"a file cannot be written", injection{zdotdir: zdot, files: []fileCopy{{src: filepath.Join(src, "rc"), dst: filepath.Join(src, "rc", "x")}}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := prepareZdotdir(tc.inj, tc.skip)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute("prepareZdotdir.TerminalZdotdirFailed") {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
}
