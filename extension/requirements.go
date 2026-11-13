package extension

import (
	"context"
	"os"
	"regexp"
	"runtime"
	"strings"

	"github.com/rm4n0s/errors"
)

// What an extension needs from the system (SPEC §10.36): the programs, services and devices it uses, each with a check that finds it
// the way the extension does when it uses it. Emetos shows them on the Extensions page and on the Check System page, and installs
// nothing by itself (D47): it tells what is found, what is missing and how to get it.

// RequirementKind says what kind of thing a Requirement is.
type RequirementKind string

// The kinds of requirement.
const (
	// RequirementProgram is a program the extension runs: a language server, a debugger, a tool, an interpreter.
	RequirementProgram RequirementKind = "program"
	// RequirementService is something that has to answer: the Docker Engine on its socket, the keyring on the session bus.
	RequirementService RequirementKind = "service"
	// RequirementDevice is a device of the computer: a camera.
	RequirementDevice RequirementKind = "device"
)

// RequirementState is what a check found.
type RequirementState string

// The states of a requirement.
const (
	// RequirementFound: it is there and can be used.
	RequirementFound RequirementState = "found"
	// RequirementMissing: it is not there.
	RequirementMissing RequirementState = "missing"
	// RequirementUnusable: something is there but cannot be used: too old, a path setting that names nothing, a socket that does
	// not answer.
	RequirementUnusable RequirementState = "unusable"
	// RequirementUnknown: the check could not tell (it took too long, it failed, or the settings turned the thing off).
	RequirementUnknown RequirementState = "unknown"
)

// Requirement is something an extension needs from the system, declared in its Manifest.
type Requirement struct {
	// ID names the requirement inside the extension: lowercase letters, digits and hyphens (gopls, docker-engine).
	ID string
	// Name is the name shown to the user (gopls, Docker Engine, Secret Service keyring).
	Name string
	// Kind is what kind of thing it is.
	Kind RequirementKind
	// Purpose says in a few words what the extension does with it ("completion, hover and diagnostics for Go").
	Purpose string
	// Optional is true when one feature of the extension needs it (dlv for debugging, a camera to scan a QR code), false when its
	// main features do (gopls, git, the Docker Engine). An optional requirement that is missing is a warning, not an error.
	Optional bool
	// Install says how to get it: the instructions and the usual commands for each family of systems.
	Install Install
	// InstallAction is the id of a command of the extension that installs it from inside Emetos (the Go tools); empty when there is
	// none.
	InstallAction string
	// PathSetting is the key of the setting that names the program, offered as "Set Path..."; empty when there is none.
	PathSetting string
	// Check finds the requirement the way the extension does when it uses it (the path setting, the folders it searches, the
	// version check). It runs on a goroutine of the host, never on the UI thread, at most once at a time for a requirement; it must
	// end soon after ctx does, and every process it starts must end with it. It must change nothing: no install, no prompt, no
	// unlocked keyring.
	Check func(ctx context.Context, env RequirementEnv) RequirementResult
}

// RequirementEnv is what a check knows of the window it is checked for.
type RequirementEnv struct {
	// Folder is the folder of the window's active workspace tab, empty without one.
	Folder string
	// Setting reads a setting of that tab: its value and true, or false when there is none. It may be called from any goroutine.
	// For a key nobody registered (the path settings of an extension that is turned off) it gives the value of the user's settings
	// file, never a folder's: a folder must not decide which program a check runs.
	Setting func(key string) (any, bool)
}

// StringSetting is the value of a string setting of the env, trimmed, "" when there is none or it is not a string.
func (e RequirementEnv) StringSetting(key string) string {
	if e.Setting == nil {
		return ""
	}
	v, _ := e.Setting(key)
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

// RequirementResult is what a check found.
type RequirementResult struct {
	// State is what was found.
	State RequirementState
	// Path is where it was found (a program's path, a socket's address), Version its version; either may be empty.
	Path, Version string
	// Detail says in one sentence what matters ("git 2.25.1 is older than 2.30", "found through go.goplsPath").
	Detail string
	// Install, when it has commands or instructions, replaces the requirement's Install for this result: the program the settings
	// chose among several (the Python language server).
	Install Install
	// PathSetting, when not empty, replaces the requirement's PathSetting for this result.
	PathSetting string
}

// Install says how to get a program (SPEC §10.34, §10.36: Emetos never installs one by itself, D47; it tells the user how).
type Install struct {
	// Docs is the address of the instructions.
	Docs string
	// Commands are the usual commands: one for each family of systems (FamilyFedora, FamilyDebian...), or for the language's own
	// installer (FamilyAny: cargo, npm, dotnet tool).
	Commands []InstallCommand
}

// Empty tells whether the Install says nothing.
func (i Install) Empty() bool { return i.Docs == "" && len(i.Commands) == 0 }

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
	FamilyMacOS  = "macos"
)

// families are the families an InstallCommand may name.
var families = map[string]bool{FamilyAny: true, FamilyFedora: true, FamilyDebian: true, FamilyArch: true, FamilySuse: true, FamilyMacOS: true}

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

// OSFamily tells the family of a Linux system from the text of its /etc/os-release: its ID and ID_LIKE, "" when it is none of
// ours.
func OSFamily(osRelease string) string {
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

// ReadOSRelease reads the file that names a Linux system, "" when there is none (macOS has none).
func ReadOSRelease() string {
	for _, path := range []string{"/etc/os-release", "/usr/lib/os-release"} {
		if data, err := os.ReadFile(path); err == nil {
			return string(data)
		}
	}
	return ""
}

// SystemFamily is the family of the running system: FamilyMacOS on macOS, else OSFamily of its os-release ("" when it is none of
// ours).
func SystemFamily() string {
	if runtime.GOOS == "darwin" {
		return FamilyMacOS
	}
	return OSFamily(ReadOSRelease())
}

// requirementIDPattern is the form of the id of a requirement inside its extension.
var requirementIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// checkRequirements checks the requirements a manifest declares: every id is valid and new, every requirement has a name, a known
// kind, a purpose and a check, and its install commands name known families and are not empty.
func checkRequirements(m Manifest) error {
	seen := map[string]bool{}
	for _, r := range m.Requirements {
		switch {
		case !requirementIDPattern.MatchString(r.ID):
			return errors.New("ExtensionRequirementInvalid", "the id of a requirement is lowercase letters, digits and hyphens", "id", m.ID, "requirement", r.ID)
		case seen[r.ID]:
			return errors.New("ExtensionRequirementDuplicate", "two requirements of the extension have the same id", "id", m.ID, "requirement", r.ID)
		case strings.TrimSpace(r.Name) == "":
			return errors.New("ExtensionRequirementInvalid", "a requirement needs a name", "id", m.ID, "requirement", r.ID)
		case r.Kind != RequirementProgram && r.Kind != RequirementService && r.Kind != RequirementDevice:
			return errors.New("ExtensionRequirementInvalid", "the kind of a requirement is program, service or device", "id", m.ID, "requirement", r.ID, "kind", string(r.Kind))
		case strings.TrimSpace(r.Purpose) == "":
			return errors.New("ExtensionRequirementInvalid", "a requirement needs a purpose", "id", m.ID, "requirement", r.ID)
		case r.Check == nil:
			return errors.New("ExtensionRequirementInvalid", "a requirement needs a check", "id", m.ID, "requirement", r.ID)
		}
		for _, c := range r.Install.Commands {
			if !families[c.Family] || strings.TrimSpace(c.Command) == "" {
				return errors.New("ExtensionRequirementInvalid", "an install command needs a known family (any, fedora, debian, arch, suse, macos) and a command", "id", m.ID, "requirement", r.ID, "family", c.Family)
			}
		}
		seen[r.ID] = true
	}
	return nil
}
