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
	// OpenURI opens a URI with the desktop (the OpenURI portal): a web address in the browser, a mail address in the mail
	// program. It fails with ExtensionOpenURIUnavailable when the scope cannot, and returns the portal's error otherwise.
	OpenURI(ctx context.Context, uri string) error
	// OpenWindowTab opens the window tab of a kind of window tab (the id of a kind in a manifest's WindowTabKinds, this
	// extension's or another one's) and makes it the active one; when a tab of the kind is open already it is shown instead of a
	// second one (SPEC §10.29: Relay's open command). It fails with ExtensionOpenWindowTabUnavailable when the scope cannot, and
	// returns the window's error for a kind that is unknown or turned off. Call it on the UI thread.
	OpenWindowTab(kindID string) error
	// NewHeadlessView makes a second, off-screen view of the workspace, size pixels large (HeadlessView). The view starts
	// with the editors the workspace has open in front of the user. It fails with ExtensionHeadlessSize for a size that is
	// not allowed (positive, at most MaxHeadlessSide a side and MaxHeadlessPixels in all), ExtensionHeadlessUnavailable in
	// a scope that has no workspace to show (a window tab of an extension, a workspace that is not on screen yet) and
	// ExtensionDeactivated after the activation ended. Call it on the UI thread.
	NewHeadlessView(size Size) (HeadlessView, error)
	// FindTextInFiles searches the files of the folder for the query and calls onFile with the matches of each file that has
	// some, as the search goes on (SPEC §10.27). The goroutine rules:
	//   - It blocks until the search is over, so it is called from a goroutine of the extension and never from the UI
	//     thread (it asks the UI thread for the text of the editors that have unsaved changes and would wait for itself).
	//   - onFile is called on goroutines of the search, not on the UI thread, and never at the same time as itself; it must
	//     not touch widgets, and hands what it gets to the UI thread with API.Post (once a frame at most, for a long list).
	//     It must not block for long. A panic in it is not caught.
	//   - When ctx ends the search stops and the call returns the error of the context (the tag SearchCancelled); onFile is
	//     not called after the call returned. The matches are in the order of the text in each file, and the files in no
	//     fixed order.
	// It fails with ExtensionNoFolder in a scope with no folder, ExtensionDeactivated after the activation ended,
	// SearchPatternEmpty for an empty pattern, and the error of the regular expression (RegexUnbalancedParen, RegexUnterminatedClass, ...)
	// for one that does not compile.
	FindTextInFiles(ctx context.Context, query TextSearchQuery, opts TextSearchOptions, onFile func(TextSearchFile)) (TextSearchSummary, error)
	// NewTextReplacer makes the replacer of a search: the replacement for a match, as VS Code's search makes it. For a regular
	// expression the replacement may have $1 and $&, \n and \t and the case operations \u \U \l \L; for a plain text it is
	// used as it is; preserveCase makes it follow the case of the text it replaces. It may be called from any goroutine. It
	// fails as FindTextInFiles does for the query.
	NewTextReplacer(query TextSearchQuery, replacement string, preserveCase bool) (TextReplacer, error)
	// ReplaceInFiles makes replacements in files of the folder that no editor has open (those of an open editor are made
	// with Documents().Apply, which is one undo step of the document). Each file is read as the editor reads it, changed,
	// and written again with the same encoding, byte order mark and line ending, atomically; a replacement whose range no
	// longer has the expected text is skipped. A file that cannot be changed is listed in the report and left as it was; the
	// others are still done. It fails with ExtensionNoFolder, ExtensionDeactivated and ExtensionFilePathInvalid (a path that
	// leaves the folder), and with the error of ctx. It may be called from any goroutine.
	ReplaceInFiles(ctx context.Context, files []FileReplacement) (ReplaceReport, error)
}
