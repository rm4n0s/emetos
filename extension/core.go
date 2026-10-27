package extension

import (
	"encoding/json"
	"slices"
	"time"

	"github.com/rm4n0s/errors"
)

// The privileges of a Manifest (SPEC §10.19 "Privileges"). The host grants them only to the extensions on its fixed
// allowlist (emetos.relay); they lift a rule inside the extension's own scope and never reach another scope.
const (
	// PrivilegeEventsPublishAny lets the extension publish any topic in its scope, not only the topics under its id and
	// the ones its manifest declares. The Publisher of such an event is still the extension, so a receiver tells it from a
	// core event (whose Publisher is CorePublisher).
	PrivilegeEventsPublishAny = "events.publishAny"
	// PrivilegeCommandsExecute lets the extension run commands, those of Emetos and those of every extension, in its
	// scope, with the core request topic TopicCommandsExecute. The commands that reach outside the workspace stay refused
	// (RelayCommandRefused).
	PrivilegeCommandsExecute = "commands.execute"
)

// RelayID is the id of the extension that the host grants the privileges to.
const RelayID = "emetos.relay"

// HasPrivilege tells whether the manifest declares the privilege. Declaring is not being granted: the host refuses an
// extension that is not on its allowlist and declares one, so an extension that runs has what it declared.
func (m Manifest) HasPrivilege(privilege string) bool {
	return slices.Contains(m.Privileges, privilege)
}

// checkPrivileges checks the names in Manifest.Privileges. The error has the tag ExtensionPrivilegeUnknown.
func checkPrivileges(m Manifest) error {
	for _, p := range m.Privileges {
		if p != PrivilegeEventsPublishAny && p != PrivilegeCommandsExecute {
			return errors.New("ExtensionPrivilegeUnknown", "a privilege is events.publishAny or commands.execute", "id", m.ID, "privilege", p)
		}
	}
	return nil
}

// The core request topics (SPEC §10.19 "Core request topics"): Emetos itself answers them, in the scope of every
// workspace window tab, for every extension. A window tab of an extension has no answer to them (EventNoHandler).
// Their errors reach the requester as they are, with the tag of the failure (not wrapped in EventHandlerFailed).
const (
	// TopicCommandsList answers CommandsList.
	TopicCommandsList = "emetos.commands.list"
	// TopicCommandsExecute takes CommandsExecuteRequest and answers CommandsExecuteReply. It needs PrivilegeCommandsExecute.
	TopicCommandsExecute = "emetos.commands.execute"
	// TopicFilesList takes FilesListRequest and answers FilesListReply.
	TopicFilesList = "emetos.files.list"
	// TopicFilesStat takes FilesPathRequest and answers FileEntry.
	TopicFilesStat = "emetos.files.stat"
	// TopicFilesRead takes FilesPathRequest and answers FilesReadReply.
	TopicFilesRead = "emetos.files.read"
	// TopicDocumentOpen takes DocumentOpenRequest and answers DocumentInfo.
	TopicDocumentOpen = "emetos.document.open"
	// TopicDocumentRead takes DocumentURIRequest and answers DocumentContent.
	TopicDocumentRead = "emetos.document.read"
	// TopicDocumentEdit takes DocumentEditRequest and answers DocumentEditReply.
	TopicDocumentEdit = "emetos.document.edit"
	// TopicDocumentSave takes DocumentURIRequest and answers DocumentSaved.
	TopicDocumentSave = "emetos.document.save"
	// TopicDocumentClose takes DocumentCloseRequest and answers DocumentClosed.
	TopicDocumentClose = "emetos.document.close"
	// TopicEditorReveal takes EditorRevealRequest and answers EditorRevealReply.
	TopicEditorReveal = "emetos.editor.reveal"
)

// CoreRequestTopics lists the core request topics.
func CoreRequestTopics() []string {
	return []string{
		TopicCommandsList, TopicCommandsExecute, TopicFilesList, TopicFilesStat, TopicFilesRead,
		TopicDocumentOpen, TopicDocumentRead, TopicDocumentEdit, TopicDocumentSave, TopicDocumentClose, TopicEditorReveal,
	}
}

// The limits of the core request topics.
const (
	// MaxFilesReadSize is the most bytes TopicFilesRead returns; a larger file fails with CoreFileTooLarge. A request may
	// ask for less (FilesPathRequest.MaxBytes).
	MaxFilesReadSize = 2 << 20
	// DefaultFilesListLimit and MaxFilesListLimit are the entries a TopicFilesList reply holds when the request names no
	// limit, and at most.
	DefaultFilesListLimit = 1000
	MaxFilesListLimit     = 5000
	// MaxFilesListDepth is the deepest a TopicFilesList may go; the default depth is 1 (the children of the folder).
	MaxFilesListDepth = 16
	// MaxDocumentReadSize is the most bytes of text TopicDocumentRead returns (CoreDocumentTooLarge above it).
	MaxDocumentReadSize = 8 << 20
	// MaxDocumentEdits and MaxDocumentEditBytes are the most edits, and the most bytes of replacement text, of one
	// TopicDocumentEdit (CoreEditTooLarge above them).
	MaxDocumentEdits     = 10000
	MaxDocumentEditBytes = 4 << 20
)

// The reasons a command is refused to the clients of a workspace (CommandInfo.RefusedClass), SPEC §10.19 "Confinement".
const (
	// RefusedWindowTab: the commands of the workspace tabs and the window tabs (emetos.workspaceTab.*, emetos.windowTab.*).
	RefusedWindowTab = "windowTab"
	// RefusedQuit: quitting Emetos and closing the window.
	RefusedQuit = "quit"
	// RefusedWindow: new windows and switching windows.
	RefusedWindow = "window"
	// RefusedFolder: opening, replacing or closing the folder, and the lists of recent folders and files.
	RefusedFolder = "folder"
	// RefusedProfile: the profiles.
	RefusedProfile = "profile"
	// RefusedSettings: commands that open or write the settings, the keybindings or the themes of the user or the profile
	// (the settings of the workspace are allowed), and the links between window tabs.
	RefusedSettings = "settings"
	// RefusedRemote: the commands of the extensions that share a workspace or control another one.
	RefusedRemote = "remote"
)

// CommandInfo is one command in the reply of TopicCommandsList.
type CommandInfo struct {
	ID       string `json:"id"`
	Title    string `json:"title,omitempty"`
	Category string `json:"category,omitempty"`
	// Description says what the command does; empty for most.
	Description string `json:"description,omitempty"`
	// Enabled tells whether the command has a handler in the workspace and its precondition holds now.
	Enabled bool `json:"enabled"`
	// Refused is true for a command that clients may not run (TopicCommandsExecute fails with RelayCommandRefused);
	// RefusedClass is one of the Refused constants and Reason says why in words.
	Refused      bool   `json:"refused,omitempty"`
	RefusedClass string `json:"refusedClass,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

// CommandsList is the reply of TopicCommandsList: every command of the workspace, those of Emetos and those of the
// extensions, ordered by id. The request carries no payload (null or {}).
type CommandsList struct {
	Commands []CommandInfo `json:"commands"`
}

// CommandsExecuteRequest is the payload of TopicCommandsExecute. Args is any JSON value (the argument the command takes).
type CommandsExecuteRequest struct {
	ID   string          `json:"id"`
	Args json.RawMessage `json:"args,omitempty"`
}

// CommandsExecuteReply is the reply of TopicCommandsExecute. A command returns no value, so Result is always null: the
// reply says the command ran, and its error is the error of the request.
type CommandsExecuteReply struct {
	Result json.RawMessage `json:"result"`
}

// The kinds of FileEntry.
const (
	// EntryFile, EntryDir and EntrySymlink are a regular file, a folder and a symbolic link (listed, never followed).
	// EntryOther is a fifo, a socket or a device, which no topic reads.
	EntryFile    = "file"
	EntryDir     = "dir"
	EntrySymlink = "symlink"
	EntryOther   = "other"
)

// FileEntry is a file, a folder or a link of the folder of the workspace.
type FileEntry struct {
	// Path is relative to the folder, with forward slashes ("." is the folder itself); Name is its last element.
	Path string `json:"path"`
	Name string `json:"name"`
	// Kind is EntryFile, EntryDir, EntrySymlink or EntryOther. TopicFilesStat follows a link that stays inside the folder
	// and reports what it points to, with Symlink true.
	Kind    string    `json:"kind"`
	Symlink bool      `json:"symlink,omitempty"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
}

// FilesListRequest is the payload of TopicFilesList. Path is a folder relative to the workspace folder ("" or "." for the
// folder itself); Depth is how deep to go (1, the default, lists the children; at most MaxFilesListDepth); the entries are
// in the order of a walk that sorts each folder by name and lists a folder before what is in it. Offset skips that many
// entries and Limit caps the reply (DefaultFilesListLimit, at most MaxFilesListLimit): the reply says More when the walk
// has entries left, and the next request asks with Offset + len(Entries).
type FilesListRequest struct {
	Path   string `json:"path,omitempty"`
	Depth  int    `json:"depth,omitempty"`
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// FilesListReply is the reply of TopicFilesList.
type FilesListReply struct {
	Entries []FileEntry `json:"entries"`
	More    bool        `json:"more,omitempty"`
}

// FilesPathRequest is the payload of TopicFilesStat and TopicFilesRead: a path relative to the workspace folder, with
// forward slashes, no "..", not absolute. MaxBytes, for a read, lowers MaxFilesReadSize.
type FilesPathRequest struct {
	Path     string `json:"path"`
	MaxBytes int64  `json:"maxBytes,omitempty"`
}

// FilesReadReply is the reply of TopicFilesRead: the bytes of the file as a string, which must be valid UTF-8 (a byte
// order mark and the line endings stay as they are).
type FilesReadReply struct {
	Path    string    `json:"path"`
	Text    string    `json:"text"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
}

// DocumentOpenRequest is the payload of TopicDocumentOpen: a file of the workspace folder (relative, as for the files
// topics) to open in the workspace, with the cursor at Line and Col (zero-based, Col in bytes, as in the events).
type DocumentOpenRequest struct {
	Path string `json:"path"`
	Line int    `json:"line,omitempty"`
	Col  int    `json:"col,omitempty"`
}

// DocumentURIRequest is the payload of TopicDocumentRead and TopicDocumentSave: the URI of a document that is open in
// the workspace (the URI of DocumentInfo).
type DocumentURIRequest struct {
	URI string `json:"uri"`
}

// DocumentContent is the reply of TopicDocumentRead: the text of the open document, with its unsaved changes, and the
// version it has (DocumentInfo.Version) to send with an edit.
type DocumentContent struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
	// Dirty tells that the document has changes that are not saved.
	Dirty bool `json:"dirty,omitempty"`
}

// DocumentEditRequest is the payload of TopicDocumentEdit: replacements of ranges of the document, made against the text
// of version Version. The ranges are in that text, as in DocumentChange; they must not overlap. The edits are one step of
// the undo history. A Version that is not the document's current one fails with RelayStaleVersion.
type DocumentEditRequest struct {
	URI     string       `json:"uri"`
	Version int          `json:"version"`
	Edits   []TextChange `json:"edits"`
}

// DocumentEditReply is the reply of TopicDocumentEdit: the version after the edit (the same version when nothing changed).
type DocumentEditReply struct {
	URI     string `json:"uri"`
	Version int    `json:"version"`
	Changed bool   `json:"changed"`
}

// DocumentCloseRequest is the payload of TopicDocumentClose. A document with unsaved changes is closed only with Discard.
type DocumentCloseRequest struct {
	URI     string `json:"uri"`
	Discard bool   `json:"discard,omitempty"`
}

// EditorRevealRequest is the payload of TopicEditorReveal: show the open document in the editor of the workspace with the
// cursor at Line and Col (zero-based, Col in bytes).
type EditorRevealRequest struct {
	URI  string `json:"uri"`
	Line int    `json:"line,omitempty"`
	Col  int    `json:"col,omitempty"`
}

// EditorRevealReply is the reply of TopicEditorReveal.
type EditorRevealReply struct {
	URI string `json:"uri"`
}
