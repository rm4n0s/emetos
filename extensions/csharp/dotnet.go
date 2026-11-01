package csharp

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

// The .NET SDK (SPEC §10.34 "C#"): csharp-ls is a .NET program, and the program `csharp-ls` is a small launcher that looks for a .NET
// installation the way every .NET application does: DOTNET_ROOT, then the file /etc/dotnet/install_location, then the usual folders. The pack
// looks in the same places so that it can say "the .NET SDK was not found" before the server fails with a message of the launcher; it
// runs nothing to find it (`dotnet --version` would also make the SDK write to the user's home folder on its first run).

// systemDotnetDirs are the folders a .NET installation is in on the distributions: the ones the launcher of an application tries.
var systemDotnetDirs = []string{"/usr/share/dotnet", "/usr/lib/dotnet", "/usr/lib64/dotnet", "/usr/local/share/dotnet", "/opt/dotnet"}

// dotnetInstallLocations are the files that name the folder of the installation: for the architecture first, then for every one.
func dotnetInstallLocations() []string {
	arch := runtime.GOARCH
	switch arch {
	case "amd64":
		arch = "x64"
	case "386":
		arch = "x86"
	}
	return []string{"/etc/dotnet/install_location_" + arch, "/etc/dotnet/install_location"}
}

// findDotnet looks for the dotnet program: DOTNET_ROOT, PATH, the folder the install_location files name, the usual folders and, last, the
// ~/.dotnet folder of the installer script of Microsoft. It returns the path of the program and where it was found.
func (c *csharp) findDotnet(env langserver.Env) (path, from string, ok bool) {
	if root := env.Getenv("DOTNET_ROOT"); root != "" {
		if p := filepath.Join(root, "dotnet"); langserver.IsExecutable(p) {
			return p, "DOTNET_ROOT", true
		}
	}
	if p, found := langserver.FindExecutable("dotnet", filepath.SplitList(env.Path)); found {
		return p, "PATH", true
	}
	for _, file := range c.installLocations {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		line, _, _ := strings.Cut(strings.TrimSpace(string(data)), "\n")
		if p := filepath.Join(strings.TrimSpace(line), "dotnet"); strings.TrimSpace(line) != "" && langserver.IsExecutable(p) {
			return p, file, true
		}
	}
	dirs := append([]string{}, c.systemDotnet...)
	if env.Home != "" {
		dirs = append(dirs, filepath.Join(env.Home, ".dotnet"))
	}
	for _, dir := range dirs {
		if p := filepath.Join(dir, "dotnet"); langserver.IsExecutable(p) {
			return p, dir, true
		}
	}
	return "", "", false
}

// requireDotnet fails with CSharpDotnetNotFound when there is no .NET installation, with what to install in the message.
func (c *csharp) requireDotnet(env langserver.Env) error {
	if _, _, ok := c.findDotnet(env); ok {
		return nil
	}
	return errors.New("CSharpDotnetNotFound", "the .NET SDK was not found: csharp-ls needs the .NET 10 SDK (the dotnet program). Install it (Fedora: sudo dnf install dotnet-sdk-10.0; "+
		"Debian and Ubuntu: sudo apt install dotnet-sdk-10.0; Arch: sudo pacman -S dotnet-sdk), or set DOTNET_ROOT to the folder that has dotnet",
		"path", env.Path, "root", env.Getenv("DOTNET_ROOT"))
}

// toolDirs is Program.Dirs: the folder the .NET global tools are installed in (`dotnet tool install --global`) is ~/.dotnet/tools, or
// $DOTNET_CLI_HOME/.dotnet/tools when the variable is set; they are searched after PATH. ~/.dotnet/tools is Program.After of the config
// and is not listed here twice.
func toolDirs(env langserver.Env) (before, after []string) {
	if home := env.Getenv("DOTNET_CLI_HOME"); home != "" && home != env.Home {
		after = append(after, filepath.Join(home, ".dotnet", "tools"))
	}
	return nil, after
}
