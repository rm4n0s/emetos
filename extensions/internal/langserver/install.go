package langserver

import (
	"os"
	"strings"
)

// Install says how to get a program (SPEC §10.34: Emetos never installs a server, D47; it tells the user how).
type Install struct {
	// Docs is the address of the instructions of the server, shown in the dialog How to Install.
	Docs string
	// Commands are the usual commands: one for each family of systems (FamilyFedora, FamilyDebian...), or for the language's own
	// installer (FamilyAny: cargo, npm, dotnet tool).
	Commands []InstallCommand
}

// InstallCommand is a command that installs a program.
type InstallCommand struct {
	// Family is the family of systems the command is for (the Family constants), FamilyAny for every system.
	Family string
	// Command is the command line, ready to paste in a terminal.
	Command string
}

// The families of systems an InstallCommand is for.
const (
	FamilyAny    = "any"
	FamilyFedora = "fedora"
	FamilyDebian = "debian"
	FamilyArch   = "arch"
	FamilySuse   = "suse"
)

// Family tells the family of the system from the text of /etc/os-release: its ID and ID_LIKE, "" when it is none of ours.
func Family(osRelease string) string {
	var ids []string
	for _, line := range strings.Split(osRelease, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || (key != "ID" && key != "ID_LIKE") {
			continue
		}
		ids = append(ids, strings.Fields(strings.ToLower(strings.Trim(value, `"'`)))...)
	}
	for _, id := range ids {
		switch id {
		case "fedora", "rhel", "centos", "rocky", "almalinux", "nobara":
			return FamilyFedora
		case "debian", "ubuntu", "linuxmint", "pop", "raspbian", "neon":
			return FamilyDebian
		case "arch", "manjaro", "endeavouros", "cachyos":
			return FamilyArch
		case "suse", "opensuse", "opensuse-leap", "opensuse-tumbleweed", "sles":
			return FamilySuse
		}
	}
	return ""
}

// ReadOSRelease reads the file that names the system, "" when there is none.
func ReadOSRelease() string {
	for _, path := range []string{"/etc/os-release", "/usr/lib/os-release"} {
		if data, err := os.ReadFile(path); err == nil {
			return string(data)
		}
	}
	return ""
}

// CommandFor chooses the command for the system: the one for its family, else the one for every system, else the first. It reports
// false when there is no command.
func (i Install) CommandFor(family string) (InstallCommand, bool) {
	if family != "" {
		for _, c := range i.Commands {
			if c.Family == family {
				return c, true
			}
		}
	}
	for _, c := range i.Commands {
		if c.Family == FamilyAny {
			return c, true
		}
	}
	if len(i.Commands) > 0 {
		return i.Commands[0], true
	}
	return InstallCommand{}, false
}
