package nativedebug

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

// The adapters of the debuggers: gdb -i dap (an executable that speaks DAP on its standard input and output) and lldb-dap (the same, or a
// server that runs already). Each is looked for in the configuration, then the setting, then PATH; a missing one is told to the user.

// gdbInstall and lldbInstall say how to get the programs. Emetos runs none of the commands (D40): the notification copies one.
var (
	gdbInstall = langserver.Install{
		Docs: "https://sourceware.org/gdb/",
		Commands: []langserver.InstallCommand{
			{Family: langserver.FamilyFedora, Command: "sudo dnf install gdb"},
			{Family: langserver.FamilyDebian, Command: "sudo apt install gdb"},
			{Family: langserver.FamilyArch, Command: "sudo pacman -S gdb"},
			{Family: langserver.FamilySuse, Command: "sudo zypper install gdb"},
		},
	}
	lldbInstall = langserver.Install{
		Docs: "https://lldb.llvm.org/",
		Commands: []langserver.InstallCommand{
			{Family: langserver.FamilyFedora, Command: "sudo dnf install lldb"},
			{Family: langserver.FamilyDebian, Command: "sudo apt install lldb"},
			{Family: langserver.FamilyArch, Command: "sudo pacman -S lldb"},
			{Family: langserver.FamilySuse, Command: "sudo zypper install lldb"},
		},
	}
)

// adapter is the descriptor factory of the three types. It may run on any goroutine.
func (d *Debugger) adapter(ctx context.Context, session extension.DebugSession, _ *extension.DebugAdapter) (*extension.DebugAdapter, error) {
	if session.Type() == TypeGDB {
		return d.gdbAdapter(ctx, session)
	}
	return d.lldbAdapter(session)
}

// findProgram finds a program: explicit (a path, or a name looked for in PATH) when it is not empty, else the first of the names in
// PATH. problem says why an explicit program cannot be used; a program that is nowhere gives "" and "".
func (d *Debugger) findProgram(explicit, folder string, names ...string) (path, problem string) {
	env := langserver.Env{Folder: folder}
	env.Home, _ = os.UserHomeDir()
	pathDirs := filepath.SplitList(os.Getenv("PATH"))
	if explicit = strings.TrimSpace(explicit); explicit != "" {
		expanded := env.Expand(explicit)
		if !strings.Contains(expanded, "/") {
			if p, ok := langserver.FindExecutable(expanded, pathDirs); ok {
				return p, ""
			}
			return "", expanded + " is not a program in PATH"
		}
		if !filepath.IsAbs(expanded) && folder != "" {
			expanded = filepath.Join(folder, expanded)
		}
		info, err := os.Stat(expanded)
		switch {
		case err != nil:
			return "", expanded + " does not exist"
		case info.IsDir():
			return "", expanded + " is a folder"
		case !langserver.IsExecutable(expanded):
			return "", expanded + " is not executable"
		}
		return filepath.Clean(expanded), ""
	}
	for _, name := range names {
		if p, ok := langserver.FindExecutable(name, pathDirs); ok {
			return p, ""
		}
	}
	return "", ""
}

// gdbVersionPattern finds the number in a version.
var gdbVersionPattern = regexp.MustCompile(`\d+(?:\.\d+)+`)

// parseGdbVersion reads the version from the first line `gdb --version` prints: "GNU gdb (Fedora Linux) 18.1-1.fc44" is 18.1 and "GNU
// gdb (Ubuntu 12.1-0ubuntu1~22.04.2) 12.1" is 12.1; the number that counts is the one the line ends with, not one in the name of the
// distribution (Red Hat Enterprise Linux 8.2).
func parseGdbVersion(output string) (string, bool) {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if v := gdbVersionPattern.FindString(fields[len(fields)-1]); v != "" {
			return v, true
		}
		v := gdbVersionPattern.FindString(line)
		return v, v != ""
	}
	return "", false
}

// gdbAdapter finds gdb, checks that it is version 14 or newer and gives `gdb -i dap`. It fails with GdbPathInvalid for a program that was
// named and cannot be run, GdbNotFound, GdbVersionFailed and GdbTooOld; the user is told in each case.
func (d *Debugger) gdbAdapter(ctx context.Context, session extension.DebugSession) (*extension.DebugAdapter, error) {
	cfg := session.Configuration()
	folder := d.folderOf(session.Folder())
	explicit, _ := cfg["debuggerPath"].(string)
	if strings.TrimSpace(explicit) == "" {
		explicit = setting[string](d, GDBPathSetting)
	}
	path, problem := d.findProgram(explicit, folder, "gdb")
	if problem != "" {
		d.tell("gdb cannot be used: "+problem+". Check gdb.path or the debuggerPath of the configuration.", gdbInstall)
		return nil, errors.New("GdbPathInvalid", "gdb cannot be used: "+problem, "program", explicit)
	}
	if path == "" {
		d.tell("gdb was not found. It debugs C, C++ and Rust programs (gdb 14 or newer). Install it, or set gdb.path if it is somewhere else.", gdbInstall)
		return nil, errors.New("GdbNotFound", "gdb was not found in PATH; the debugger needs gdb 14 or newer")
	}
	version, err := langserver.QueryVersion(ctx, path, langserver.VersionCheck{Args: []string{"--version"}, Parse: parseGdbVersion}, folder, nil)
	if err != nil {
		reason := err.Error()
		if appErr, ok := errors.FromError(err); ok {
			reason = appErr.Message
		}
		return nil, errors.New("GdbVersionFailed", path+" did not tell its version: "+reason, "program", path)
	}
	if langserver.CompareVersions(version, MinGDBVersion) < 0 {
		d.tell("gdb "+version+" ("+path+") is too old: the debugger needs gdb "+MinGDBVersion+" or newer (it speaks the Debugger Adapter Protocol). "+
			"Install a newer gdb, or set gdb.path.", gdbInstall)
		return nil, errors.New("GdbTooOld", "gdb "+version+" is too old: the debugger needs gdb "+MinGDBVersion+" or newer", "program", path, "version", version, "minimum", MinGDBVersion)
	}
	args := append([]string{"-i", "dap"}, stringList(setting[[]any](d, GDBArgumentsSetting))...)
	args = append(args, stringList(cfg["debuggerArgs"])...)
	return &extension.DebugAdapter{
		Kind: extension.DebugAdapterExecutable, Command: path, Args: args, Cwd: folder,
		Env: stringMap(setting[map[string]any](d, GDBEnvironmentSetting)), ConsoleOutput: true,
	}, nil
}

// lldbAdapter gives lldb-dap: a server at debugAdapterHostname and debugAdapterPort when the configuration names a port, else the
// program (debugAdapterExecutable, lldb-dap.executable-path, lldb-dap or lldb-vscode in PATH) with the arguments of the setting and of
// debugAdapterArgs. It fails with LldbDapPathInvalid and LldbDapNotFound; the user is told.
func (d *Debugger) lldbAdapter(session extension.DebugSession) (*extension.DebugAdapter, error) {
	cfg := session.Configuration()
	folder := d.folderOf(session.Folder())
	if port, ok := intValue(cfg["debugAdapterPort"]); ok && port > 0 {
		host, _ := cfg["debugAdapterHostname"].(string)
		if strings.TrimSpace(host) == "" {
			host = "127.0.0.1"
		}
		return &extension.DebugAdapter{Kind: extension.DebugAdapterServer, Host: strings.TrimSpace(host), Port: port}, nil
	}
	explicit, _ := cfg["debugAdapterExecutable"].(string)
	if strings.TrimSpace(explicit) == "" {
		explicit = setting[string](d, LLDBPathSetting)
	}
	path, problem := d.findProgram(explicit, folder, "lldb-dap", "lldb-vscode")
	if problem != "" {
		d.tell("lldb-dap cannot be used: "+problem+". Check lldb-dap.executable-path or the debugAdapterExecutable of the configuration.", lldbInstall)
		return nil, errors.New("LldbDapPathInvalid", "lldb-dap cannot be used: "+problem, "program", explicit)
	}
	if path == "" {
		d.tell("lldb-dap was not found. It debugs C, C++ and Rust programs and comes with LLDB. Install it, or set lldb-dap.executable-path if it is somewhere else.", lldbInstall)
		return nil, errors.New("LldbDapNotFound", "lldb-dap (or lldb-vscode) was not found in PATH")
	}
	args := append(stringList(setting[[]any](d, LLDBArgumentsSetting)), stringList(cfg["debugAdapterArgs"])...)
	return &extension.DebugAdapter{
		Kind: extension.DebugAdapterExecutable, Command: path, Args: args, Cwd: folder,
		Env: stringMap(setting[map[string]any](d, LLDBEnvironmentSetting)), ConsoleOutput: true,
	}, nil
}

// intValue reads a number or a string of digits.
func intValue(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), n == float64(int(n))
	case int:
		return n, true
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(n))
		return i, err == nil
	}
	return 0, false
}

// tell tells the user that a debugger cannot be used, with a button that copies the command that installs it for this system. It may
// be called from any goroutine.
func (d *Debugger) tell(message string, install langserver.Install) {
	var actions []extension.NotificationAction
	if cmd, ok := install.CommandFor(langserver.Family(d.osRelease())); ok {
		actions = append(actions, extension.NotificationAction{Label: "Copy Install Command", Run: func() { d.copyInstall(cmd.Command) }})
	}
	d.api.Notifications().Warn(message, actions...)
}

// copyInstall puts the install command on the clipboard. UI thread.
func (d *Debugger) copyInstall(command string) {
	if err := d.api.Clipboard().WriteText(command); err != nil {
		d.api.Notifications().Error("The command cannot be copied: " + err.Error() + ". Run it yourself: " + command)
		return
	}
	d.api.Notifications().Info("Copied: " + command)
}
