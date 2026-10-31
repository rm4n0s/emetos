package scm

import (
	"net/url"
	"path"
	"strings"
)

// Command is a command that the view runs on behalf of a provider: a click on a resource, Ctrl+Enter in the input, a button.
//
// Ported from VS Code: src/vs/editor/common/languages.ts (Command)
type Command struct {
	// ID is the id of the command; empty is no command.
	ID string
	// Title is the text of the button or the menu row.
	Title string
	// Tooltip is the text of the hover.
	Tooltip string
	// Args is the JSON-compatible argument the command is run with, or nil.
	Args any
}

// StatusCommand is an item of the status bar and of the header of a repository: the branch and the sync item of Git.
type StatusCommand struct {
	// Text is the label, with the Codicons of "$(name)".
	Text string
	// Tooltip is the text of the hover.
	Tooltip string
	// Command is the id of the command a click runs; empty for an item that only shows.
	Command string
	// Args is the argument of Command.
	Args any
}

// Decorations are the looks of a resource row (VS Code's ISCMResourceDecorations).
type Decorations struct {
	// Icon is the name of a Codicon or the path of an image.
	Icon string
	// Tooltip is the hover of the row, and what the sort by status compares.
	Tooltip string
	// StrikeThrough draws the name struck through.
	StrikeThrough bool
	// Faded draws the row dimmed.
	Faded bool
	// Color is the id of a theme color.
	Color string
	// Letter is the status letter the provider gave (ResourceDecorations.Letter), until the file decoration service has the
	// Git provider (TODO(step 24.6)).
	Letter string
}

// ActionButton is the button under the input (VS Code's ISCMActionButtonDescriptor).
type ActionButton struct {
	// Command is the primary command.
	Command Command
	// Secondary are the groups of the dropdown.
	Secondary [][]Command
	// Enabled says whether the button can be pressed.
	Enabled bool
	// Description is the tooltip.
	Description string
}

// ValidationLevel is the severity of a Validation; the values are VS Code's InputValidationType.
type ValidationLevel int

// The levels of a Validation.
const (
	// ValidationError is the most severe.
	ValidationError ValidationLevel = iota
	// ValidationWarning is a warning.
	ValidationWarning
	// ValidationInfo is a hint.
	ValidationInfo
)

// Validation is the message under the input.
type Validation struct {
	// Message is the text.
	Message string
	// Level is the severity.
	Level ValidationLevel
}

// Resource is a changed file of a group (VS Code's ISCMResource). The group fills in Path and Group when it takes the resource;
// the fields of a resource that a group holds are not changed.
type Resource struct {
	// URI is the file as file:///path.
	URI string
	// Path is the path of the file: URI without the scheme and decoded; the URI itself when it has none.
	Path string
	// Command is run on a click on the row; nil for none.
	Command *Command
	// ContextValue is the value of the context key scmResourceState for the menus of the resource.
	ContextValue string
	// Decorations are the looks of the row.
	Decorations Decorations
	// MultiDiffOriginalURI and MultiDiffModifiedURI are the sides of the multi-file diff editor.
	MultiDiffOriginalURI, MultiDiffModifiedURI string
	// Group is the group that holds the resource.
	Group *Group

	// lower is the Path in lower case and name the last component of the path, for the sort.
	lower, name string
}

// prepare fills in the fields that follow from the URI.
func (r *Resource) prepare() {
	r.Path = PathOfURI(r.URI)
	r.name = baseName(r.Path)
	r.lower = strings.ToLower(r.Path)
}

// Name is the file name of the resource: the last component of its path.
func (r *Resource) Name() string { return r.name }

// Dir is the folder of the resource: its path without the file name.
func (r *Resource) Dir() string {
	return strings.TrimSuffix(strings.TrimSuffix(r.Path, r.name), "/")
}

// PathOfURI is the path of a URI of the form file:///path (the form of SPEC §7.1: not percent-encoded; one that is encoded is
// decoded); the text itself when it has no file scheme.
func PathOfURI(uri string) string {
	rest, ok := strings.CutPrefix(uri, "file://")
	if !ok {
		return uri
	}
	if strings.Contains(rest, "%") {
		if p, err := url.PathUnescape(rest); err == nil {
			return p
		}
	}
	return rest
}

// baseName is the last component of a path.
func baseName(p string) string {
	p = strings.TrimRight(p, "/")
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

// relativePath is the path of target below base when target is a descendant of base (a path with no ".." and not empty), and
// false otherwise. Both are cleaned paths with forward slashes.
func relativePath(base, target string) (string, bool) {
	base, target = path.Clean(base), path.Clean(target)
	if base == target {
		return "", false
	}
	if base == "/" {
		return strings.TrimPrefix(target, "/"), strings.HasPrefix(target, "/")
	}
	rest, ok := strings.CutPrefix(target, base+"/")
	return rest, ok && rest != ""
}
