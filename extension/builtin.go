package extension

import (
	"context"
)

// OutputChannel is a channel of the Output panel that an extension writes to (SPEC §10.13). Its lines are those of the
// log of the channel: the last 10,000 are kept.
type OutputChannel interface {
	// Name is the name in the list of channels.
	Name() string
	// AppendLine adds a line; a text with line breaks is several lines. It may be called from any goroutine.
	AppendLine(text string)
	// Clear empties the channel.
	Clear()
	// Show selects the channel in the Output panel and shows the panel; with preserveFocus the keyboard focus stays where
	// it is.
	Show(preserveFocus bool)
	// Dispose takes the channel away with its lines. A channel that the activation did not dispose goes when it ends.
	Dispose()
}

// Output gives the extension channels of the Output panel.
type Output interface {
	// Channel makes the channel with the name, or fails with ExtensionOutputChannelInvalid for an empty name and
	// ExtensionOutputChannelDuplicate when the extension has it open. Two window tabs that open the same name share the
	// lines, because the Output panel is the window's; the channel goes when the last of them lets go of it.
	Channel(name string) (OutputChannel, error)
}

// Severity is how serious a Marker is.
type Severity int

// The severities of a marker.
const (
	// SeverityError is an error.
	SeverityError Severity = iota
	// SeverityWarning is a warning.
	SeverityWarning
	// SeverityInfo is information.
	SeverityInfo
	// SeverityHint is a suggestion; the Problems panel does not show hints.
	SeverityHint
)

// Marker is a problem found in a file, as the Problems panel shows it. Lines and columns count from 1; a column counts
// UTF-16 units (SPEC §10.13).
type Marker struct {
	// Severity is how serious the problem is; an error when zero.
	Severity Severity
	// Message describes the problem. A marker with no message is dropped.
	Message string
	// Source is the tool that found the problem ("go vet"); the name of the extension when empty.
	Source string
	// Code is the code of the problem; may be empty.
	Code string
	// StartLine, StartColumn, EndLine and EndColumn are the range; a missing end is the start.
	StartLine, StartColumn, EndLine, EndColumn int
}

// Problems gives the extension markers in the Problems panel. The markers belong to the extension: another extension
// cannot replace or clear them, and they go when the activation ends.
type Problems interface {
	// Set replaces the markers the owner has for the file with the URI (file:///path/to/file) with markers; none removes
	// them. The owner is a name of the extension's own ("go vet"). It fails with ExtensionProblemsOwnerInvalid for an empty
	// owner and ExtensionProblemsURIInvalid for a URI that is not a file. It may be called from any goroutine.
	Set(owner, uri string, markers []Marker) error
	// Clear removes every marker of the owner.
	Clear(owner string)
}

// FileList is the files of the folder of the workspace.
type FileList struct {
	// Files are the paths relative to the folder, with forward slashes, of the files the quick open lists (the exclusions
	// of files.exclude, search.exclude and the ignore files apply).
	Files []string
	// Complete tells that the walk of the folder has finished; until it has, the list grows and TopicFilesChanged says so
	// with FileListed.
	Complete bool
}

// MaxReadFileSize is the largest file Workspace().ReadFile reads.
const MaxReadFileSize = 2 << 20

// Workspace gives the extension the folder of the workspace, read only.
type Workspace interface {
	// Files lists the files of the folder. It fails with ExtensionNoFolder in a scope with no folder.
	Files() (FileList, error)
	// ReadFile reads a file of the folder from disk, by its path relative to the folder, as UTF-8 text. It sees no unsaved
	// changes (Documents().Read does). It fails with ExtensionNoFolder, ExtensionFilePathInvalid for a path that leaves
	// the folder (a .. or a link that points out of it), ExtensionFileTooLarge above MaxReadFileSize and
	// ExtensionFileNotText for a file that is not valid UTF-8; the error of the file system is wrapped otherwise. Unlike
	// Documents().Read it may be called from any goroutine.
	ReadFile(ctx context.Context, path string) (string, error)
}
