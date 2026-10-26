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
	// OwnUI says that the extension shows the state of the server itself, with LanguageServers().Status and OnChange: Emetos
	// makes no status bar item for the server and shows no notification when its program is not found (the line in the
	// Output channel and the crash notification stay). The extension then tells the user about a missing program (SPEC
	// §10.17 "As built (step 14.1)").
	OwnUI bool
	// Category is the category of the command <extension id>.restartServer in the command palette ("Go" gives "Go: Restart
	// Language Server"); the name of the extension when empty. With several servers the first Category that is not empty
	// is used.
	Category string
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

// ServerState is the state of a language server in the scope of an extension.
type ServerState string

// The states of a language server (LanguageServers().Status).
const (
	// ServerIdle: declared, and waiting for the first document of its languages.
	ServerIdle ServerState = "idle"
	// ServerStarting: the program was started and the handshake is not done (a crashed server that is started again too).
	ServerStarting ServerState = "starting"
	// ServerRunning: initialized; its documents are open on it.
	ServerRunning ServerState = "running"
	// ServerFailed: it crashed five times in three minutes and is not started again until the user restarts it.
	ServerFailed ServerState = "failed"
	// ServerMissing: the program was not found; nothing is started again until the path setting changes or the server is
	// restarted.
	ServerMissing ServerState = "missing"
	// ServerStopped: the activation ended.
	ServerStopped ServerState = "stopped"
)

// ServerStatus is what an extension may know of one of its language servers in its scope.
type ServerStatus struct {
	// State is the state of the server.
	State ServerState
	// Starts counts the times the program was started in this scope, Crashes the crashes among the last five (a restart by
	// the user forgets them).
	Starts, Crashes int
	// Progress is the work the server reports with $/progress, as the status bar of a language client shows it ("Loading
	// packages 40%"): the last work that began and has not ended; empty when there is none.
	Progress string
}

// LanguageServers lets an extension follow and control the language servers its manifest declares. The id of a server is the
// ID of its LanguageServerSpec. The host starts and stops the servers (the activation does not); this is how the extension
// shows their state and offers their commands.
type LanguageServers interface {
	// Status returns the state of the server. A server that is declared but not made yet (the call is made in Activate) is
	// idle. It fails with ExtensionServerUnknown for an id the manifest does not declare. Call it on the UI thread.
	Status(serverID string) (ServerStatus, error)
	// OnChange calls fn, on the UI thread and after the change, whenever the state or the progress of the server changes,
	// with the new status, and returns the function that stops the calls. The calls stop with the activation. It fails with
	// ExtensionServerUnknown for an id the manifest does not declare. A panic in fn deactivates the extension.
	OnChange(serverID string, fn func(ServerStatus)) (cancel func(), err error)
	// Restart stops the server, if it runs, and starts it again, forgetting its crashes; an idle server is started and a
	// server whose program was not found is looked for again. It is what the command <extension id>.restartServer does. It
	// fails with ExtensionServerUnknown, and with ExtensionDeactivated after the activation ended.
	Restart(serverID string) error
	// ShowOutput shows the Output channel of the server (the channel is named after the server); with preserveFocus the
	// keyboard focus stays where it is. It fails with ExtensionServerUnknown.
	ShowOutput(serverID string, preserveFocus bool) error
}
