package extension

import (
	"regexp"
	"strings"

	"github.com/rm4n0s/errors"
)

// serverIDPattern is the form of the id of a language server inside its extension: lowercase letters, digits and hyphens.
var serverIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// LanguageServerSpec declares a language server (SPEC §9.12 "Servers and their lifetime"). Emetos starts one server of this
// kind per window tab, when the first document of its languages opens there (or when the extension is activated, with
// OnActivation), in the folder of the tab, in a process group of its own, and stops it when the tab closes or the extension
// is turned off. Its messages go to an Output channel named Name; the language features it offers are merged with those of
// the providers an extension wrote in Go (Languages().Register...Provider).
type LanguageServerSpec struct {
	// ID names the server inside the extension: lowercase letters, digits and hyphens (gopls). Emetos qualifies it with the id
	// of the extension: emetos.golang.gopls is the id of the setting emetos.golang.gopls.trace.server.
	ID string
	// Name is the name shown to the user: the name of the Output channel and of the status bar item.
	Name string
	// Languages are the ids of the languages the server serves (go). Only documents of these languages are opened on the
	// server.
	Languages []string
	// Program is the program to run, found in PATH unless it has a slash; Args are its arguments.
	Program string
	Args    []string
	// Env are extra environment variables ("NAME=value") added to the environment of Emetos.
	Env []string
	// PathSetting is the key of a setting, written by the user, that holds the path of the program and so overrides Program
	// (go.goplsPath). Emetos registers it as a string setting (empty by default) while the extension is enabled in a tab. An
	// empty value runs Program.
	PathSetting string
	// InitializationOptions are sent in initialize as initializationOptions; they must encode to JSON.
	InitializationOptions any
	// ConfigurationSection is the key of the settings the server reads through workspace/configuration (go.gopls). A request
	// for the section of the server's own ID (gopls) or for one below it (gopls.ui) is answered with this key or the one below
	// it; any other section is answered with the setting of that name. A change of the section is sent with
	// workspace/didChangeConfiguration.
	ConfigurationSection string
	// WatchedFiles are globs, relative to the folder, of the files whose changes are sent to the server with
	// workspace/didChangeWatchedFiles, besides those the server registers itself.
	WatchedFiles []string
	// OnActivation starts the server when the extension is activated in a tab, without waiting for a document.
	OnActivation bool
}

// QualifiedServerID is the id of a server in Emetos: the id of the extension, a dot and the ID of the spec.
func QualifiedServerID(extensionID string, spec LanguageServerSpec) string {
	return extensionID + "." + spec.ID
}

// RestartServerCommand is the id of the command that restarts the servers of an extension in the tab (Emetos registers it
// for an extension that declares servers): the id of the extension and ".restartServer".
func RestartServerCommand(extensionID string) string { return extensionID + ".restartServer" }

// TraceSetting is the key of the setting that says how much of the traffic with the server is written to its Output
// channel: off, messages (the method and id of each message) or verbose (the messages with their bodies). Emetos
// registers it for every server.
func TraceSetting(extensionID string, spec LanguageServerSpec) string {
	return QualifiedServerID(extensionID, spec) + ".trace.server"
}

// checkLanguageServers checks the servers a manifest declares: every id is valid and new, and every server has a name,
// languages and a program.
func checkLanguageServers(m Manifest) error {
	seen := map[string]bool{}
	for _, sv := range m.Contributes.LanguageServers {
		switch {
		case !serverIDPattern.MatchString(sv.ID):
			return errors.New("ExtensionServerInvalid", "the id of a language server is lowercase letters, digits and hyphens", "id", m.ID, "server", sv.ID)
		case seen[sv.ID]:
			return errors.New("ExtensionServerDuplicate", "two language servers of the extension have the same id", "id", m.ID, "server", sv.ID)
		case strings.TrimSpace(sv.Name) == "":
			return errors.New("ExtensionServerInvalid", "a language server needs a name", "id", m.ID, "server", sv.ID)
		case len(sv.Languages) == 0:
			return errors.New("ExtensionServerInvalid", "a language server needs the languages it serves", "id", m.ID, "server", sv.ID)
		case strings.TrimSpace(sv.Program) == "":
			return errors.New("ExtensionServerInvalid", "a language server needs a program", "id", m.ID, "server", sv.ID)
		}
		seen[sv.ID] = true
	}
	return nil
}
