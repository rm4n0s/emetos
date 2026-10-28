package terminal

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rm4n0s/errors"
)

// The shell integration scripts of VS Code (SPEC §10.25 "Shell integration"), copied by scripts/update-assets.sh and embedded:
// bash starts from one with --init-file, fish and PowerShell source one, zsh reads four of them from a ZDOTDIR.
//
//go:embed shellintegration
var shellScripts embed.FS

// scriptsDir is the folder of the embedded scripts.
const scriptsDir = "shellintegration"

// The shells whose scripts exist (VS Code's ShellIntegrationExecutable).
const (
	shellBash = "bash"
	shellZsh  = "zsh"
	shellFish = "fish"
	shellPwsh = "pwsh"
)

// The reasons why the scripts are not injected (VS Code's ShellIntegrationInjectionFailureReason).
const (
	injectNone           = ""
	injectDisabled       = "terminal.integrated.shellIntegration.enabled is off"
	injectNoExecutable   = "there is no program"
	injectUnsupportedArg = "the arguments of the profile cannot be combined with the script"
	injectUnsupported    = "the shell has no shell integration script"
)

// injectOpts say how the scripts are injected.
type injectOpts struct {
	// enabled is terminal.integrated.shellIntegration.enabled.
	enabled bool
	// root is the folder the scripts were written to (see writeScripts).
	root string
	// nonce is the secret of this terminal: the scripts print it with the command line (OSC 633 ; E), so that only they can
	// say what the command line is (VS Code's VSCODE_NONCE).
	nonce string
	// home is the home folder of the user, the default of ZDOTDIR; zdotdir is the ZDOTDIR of the environment ("" for none).
	home, zdotdir string
	// tmp is the folder of the ZDOTDIR for zsh (the real path of the temporary folder), username the user's name in its name.
	tmp, username string
	// appName is the name of the application in the name of that folder (VS Code uses its applicationName).
	appName string
	// skipStickyBit leaves the sticky bit of the ZDOTDIR alone and makes no folder (the tests).
	skipStickyBit bool
}

// injection is what the scripts need: the arguments that replace the profile's, the variables added to the environment and,
// for zsh, the files to copy into its ZDOTDIR.
type injection struct {
	args  []string
	env   map[string]string
	files []fileCopy
	// zdotdir is the folder the files go to.
	zdotdir string
}

// fileCopy is a script to copy, from the folder of the scripts to a place in the ZDOTDIR.
type fileCopy struct{ src, dst string }

// Shell integration arguments of VS Code (terminalEnvironment.ts, shellIntegrationArgs). The {0} is the folder of the scripts.
// Fish checks TERM_PROGRAM and leaves the shell when it is not "vscode" (shellIntegration.fish, its first lines), and Emetos
// sets TERM_PROGRAM=emetos for the programs it runs: so the fish command says "vscode" for the script alone and puts "emetos"
// back after it (decision 190).
var (
	argsPwsh      = []string{"-noexit", "-command", `. "{0}/shellIntegration.ps1"`}
	argsPwshLogin = []string{"-l", "-noexit", "-command", `. "{0}/shellIntegration.ps1"`}
	argsZsh       = []string{"-i"}
	argsZshLogin  = []string{"-il"}
	argsBash      = []string{"--init-file", "{0}/shellIntegration-bash.sh"}
	argsFish      = []string{"--init-command", `set --global TERM_PROGRAM vscode; source "{0}/shellIntegration.fish"; set --global TERM_PROGRAM emetos`}
	argsFishLogin = []string{"-l", "--init-command", `set --global TERM_PROGRAM vscode; source "{0}/shellIntegration.fish"; set --global TERM_PROGRAM emetos`}
)

var (
	pwshLoginArgs   = []string{"-login", "-l"}
	shLoginArgs     = []string{"--login", "-l"}
	shInteractive   = []string{"-i", "--interactive"}
	pwshImpliedArgs = []string{"-nol", "-nologo"}
)

// lowerIn tells whether the lower case of s is one of the list.
func lowerIn(s string, list []string) bool { return slices.Contains(list, strings.ToLower(s)) }

// arePwshLoginArgs is VS Code's arePwshLoginArgs for a list of arguments.
func arePwshLoginArgs(a []string) bool {
	switch len(a) {
	case 1:
		return lowerIn(a[0], pwshLoginArgs)
	case 2:
		return (lowerIn(a[0], pwshLoginArgs) || lowerIn(a[1], pwshLoginArgs)) && (lowerIn(a[0], pwshImpliedArgs) || lowerIn(a[1], pwshImpliedArgs))
	}
	return false
}

// arePwshImpliedArgs is VS Code's arePwshImpliedArgs: no arguments, or -nologo.
func arePwshImpliedArgs(a []string) bool {
	return len(a) == 0 || len(a) == 1 && lowerIn(a[0], pwshImpliedArgs)
}

// areShellLoginArgs is VS Code's areZshBashFishLoginArgs: --login or -l, with the interactive flag allowed besides.
func areShellLoginArgs(a []string) bool {
	var rest []string
	for _, x := range a {
		if !lowerIn(x, shInteractive) {
			rest = append(rest, x)
		}
	}
	return len(rest) == 1 && lowerIn(rest[0], shLoginArgs)
}

// withRoot replaces {0} in the last argument by the folder of the scripts, on a copy.
func withRoot(args []string, root string) []string {
	out := slices.Clone(args)
	out[len(out)-1] = strings.ReplaceAll(out[len(out)-1], "{0}", root)
	return out
}

// shellIntegrationInjection decides how the scripts are injected into a shell: the port of VS Code's
// getShellIntegrationInjection for Linux. shell is the name of the program (its base name), args the arguments of the profile.
// ok is false, with the reason, when the scripts cannot be used: the setting is off, the shell has no script or its arguments
// are not ones the script can be combined with.
//
// Ported from VS Code: src/vs/platform/terminal/node/terminalEnvironment.ts (getShellIntegrationInjection)
func shellIntegrationInjection(shell string, args []string, o injectOpts) (inj injection, reason string) {
	if !o.enabled {
		return injection{}, injectDisabled
	}
	if shell == "" {
		return injection{}, injectNoExecutable
	}
	inj.env = map[string]string{"VSCODE_INJECTION": "1"}
	if o.nonce != "" {
		inj.env["VSCODE_NONCE"] = o.nonce
	}
	switch filepath.Base(shell) {
	case shellBash:
		switch {
		case len(args) == 0:
			inj.args = argsBash
		case areShellLoginArgs(args):
			inj.env["VSCODE_SHELL_LOGIN"] = "1"
			inj.args = argsBash
		default:
			return injection{}, injectUnsupportedArg
		}
		inj.args = withRoot(inj.args, o.root)
		inj.env["VSCODE_STABLE"] = "1"
	case shellFish:
		switch {
		case len(args) == 0:
			inj.args = argsFish
		case areShellLoginArgs(args):
			inj.args = argsFishLogin
		case slices.Equal(args, argsFish) || slices.Equal(args, argsFishLogin):
			inj.args = args
		default:
			return injection{}, injectUnsupportedArg
		}
		inj.args = withRoot(inj.args, o.root)
	case shellPwsh:
		switch {
		case arePwshImpliedArgs(args):
			inj.args = argsPwsh
		case arePwshLoginArgs(args):
			inj.args = argsPwshLogin
		default:
			return injection{}, injectUnsupportedArg
		}
		inj.args = withRoot(inj.args, o.root)
		inj.env["VSCODE_STABLE"] = "1"
	case shellZsh:
		switch {
		case len(args) == 0:
			inj.args = argsZsh
		case areShellLoginArgs(args):
			inj.args = argsZshLogin
		case slices.Equal(args, argsZsh) || slices.Equal(args, argsZshLogin):
			inj.args = args
		default:
			return injection{}, injectUnsupportedArg
		}
		inj.args = slices.Clone(inj.args)
		app := o.appName
		if app == "" {
			app = "emetos"
		}
		user := o.username
		if user == "" {
			user = "unknown"
		}
		inj.zdotdir = filepath.Join(o.tmp, user+"-"+app+"-zsh")
		inj.env["ZDOTDIR"] = inj.zdotdir
		userZdotdir := o.zdotdir
		if userZdotdir == "" {
			userZdotdir = o.home
		}
		if userZdotdir == "" {
			userZdotdir = "~"
		}
		inj.env["USER_ZDOTDIR"] = userZdotdir
		for _, f := range []struct{ src, dst string }{
			{"shellIntegration-rc.zsh", ".zshrc"}, {"shellIntegration-profile.zsh", ".zprofile"},
			{"shellIntegration-env.zsh", ".zshenv"}, {"shellIntegration-login.zsh", ".zlogin"},
		} {
			inj.files = append(inj.files, fileCopy{src: filepath.Join(o.root, f.src), dst: filepath.Join(inj.zdotdir, f.dst)})
		}
	default:
		return injection{}, injectUnsupported
	}
	return inj, injectNone
}

// scriptsHash names the version of the embedded scripts, so that a new Emetos writes them to a folder of its own and never
// reads half of the old ones.
func scriptsHash() string {
	h := sha256.New()
	entries, err := fs.ReadDir(shellScripts, scriptsDir)
	if err != nil {
		return "none" // the embedded folder cannot be missing; the hash only names a folder
	}
	for _, e := range entries {
		data, err := shellScripts.ReadFile(scriptsDir + "/" + e.Name())
		if err != nil {
			continue
		}
		h.Write([]byte(e.Name()))
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// writeScripts writes the embedded scripts to a folder under base (the cache folder of Emetos) and returns it. A script that is
// already there with the right content is left alone, so the call is cheap; one that is missing or different is written again,
// so the folder is repaired if something removed a file. The errors have the tag TerminalScriptsWriteFailed.
func writeScripts(base string) (string, error) {
	root := filepath.Join(base, "shell-integration", scriptsHash())
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", errors.NewErr("TerminalScriptsWriteFailed", err).SetMetadata("path", root, "step", "mkdir")
	}
	entries, err := fs.ReadDir(shellScripts, scriptsDir)
	if err != nil {
		return "", errors.NewErr("TerminalScriptsWriteFailed", err).SetMetadata("path", root, "step", "list")
	}
	for _, e := range entries {
		want, err := shellScripts.ReadFile(scriptsDir + "/" + e.Name())
		if err != nil {
			return "", errors.NewErr("TerminalScriptsWriteFailed", err).SetMetadata("path", root, "step", "read")
		}
		dst := filepath.Join(root, e.Name())
		if have, err := os.ReadFile(dst); err == nil && string(have) == string(want) {
			continue
		}
		if err := writeFileAtomic(dst, want, 0o644); err != nil {
			return "", errors.NewErr("TerminalScriptsWriteFailed", err).SetMetadata("path", dst, "step", "write")
		}
	}
	return root, nil
}

// writeFileAtomic writes data to a temporary file next to path and renames it over path, so that a shell that starts while the
// file is written sees the old file or the new one, never a part.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}

// prepareZdotdir makes the ZDOTDIR of zsh and copies the four scripts into it, as VS Code does: the folder has the sticky bit
// and mode 0700 (mode 01700), so nobody else can replace the files zsh will run. The errors have the tags
// TerminalZdotdirFailed.
func prepareZdotdir(inj injection, skipStickyBit bool) error {
	if inj.zdotdir == "" {
		return nil
	}
	if !skipStickyBit {
		if err := os.MkdirAll(inj.zdotdir, 0o700); err != nil {
			return errors.NewErr("TerminalZdotdirFailed", err).SetMetadata("path", inj.zdotdir, "step", "mkdir")
		}
		if err := os.Chmod(inj.zdotdir, os.ModeSticky|0o700); err != nil {
			return errors.NewErr("TerminalZdotdirFailed", err).SetMetadata("path", inj.zdotdir, "step", "chmod")
		}
	}
	for _, f := range inj.files {
		data, err := os.ReadFile(f.src)
		if err != nil {
			return errors.NewErr("TerminalZdotdirFailed", err).SetMetadata("path", f.src, "step", "read")
		}
		if err := writeFileAtomic(f.dst, data, 0o644); err != nil {
			return errors.NewErr("TerminalZdotdirFailed", err).SetMetadata("path", f.dst, "step", "copy")
		}
	}
	return nil
}
