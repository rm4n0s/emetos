package extension

import (
	"context"
	"strings"
)

// TagMultiDiffGone is the tag of the error that a MultiDiffProvider's Resources returns for a set that is not there any more (the
// repository was closed, the commit was dropped). The window closes the tab of the set without a word, as it drops the diff of a
// repository that is gone; any other error is shown in the tab. An extension makes the error with its own tag name and passes the tag
// "MultiDiffGone"; the window finds it with errors.Is on a value of this tag.
const TagMultiDiffGone = "MultiDiffGone"

// MultiDiffKindPattern is what the Kind of a MultiDiffProvider looks like: a word of letters, digits, dots and hyphens that starts with
// a letter ("git.changes").
const MultiDiffKindPattern = `^[A-Za-z][A-Za-z0-9.-]*$`

// MultiDiffResource is one file of a multi-file diff editor (SPEC §10.30 "Diffs of changes"; VS Code's MultiDiffEditorItem with the
// parts of the Source Control resource that its header shows).
type MultiDiffResource struct {
	// Original and Modified are the two documents of the file: a file (file:///path), an open untitled document, or a provided one
	// (ContentURI). An empty one is a file that does not exist on that side (an added file has no original, a deleted file no
	// modified); at least one is set. The documents are read when the section of the file comes near the viewport, off the UI thread.
	Original, Modified string
	// Path is the path of the file as the header shows it, with slashes and relative to the root of the set: the last element is the
	// name and the rest the folder.
	Path string
	// File is the file that Open File shows and that gives the header its icon and its decorations: a file URI; empty for none.
	File string
	// Letter, Color and Tooltip are the mark of the status of the file at the end of its header: "M" in the theme color with the id
	// Color ("gitDecoration.modifiedResourceForeground"), with the tooltip "Modified".
	Letter, Color, Tooltip string
	// Context holds the context keys the when-clauses of the file's actions are read with (MenuMultiDiffFileToolbar): for Git
	// {"scmProvider": "git", "scmResourceGroup": "index"}.
	Context map[string]any
	// Args is the value the commands of the file's actions are run with, as the command of a menu of the Source Control view is run
	// with SCMArgs: for Git the list of one object that names the resource.
	Args any
}

// MultiDiffProvider makes the files of the sets of one kind, and is how a multi-file diff editor comes back after a restart
// (SPEC §10.30): the saved tab remembers only the Kind and the Key, and the window asks the provider for the files when the extension
// has started and registered it. It is also how the editor follows its set: Changed makes the window ask again.
type MultiDiffProvider struct {
	// Kind names the provider, unique among the providers of the window tab ("git.changes").
	Kind string
	// Resources makes the files of the set with the Key, sorted as they are shown. It runs off the UI thread, each time the set is
	// opened and each time Changed says it changed, and must stop when ctx does. An error with the tag TagMultiDiffGone closes the
	// tab; any other is shown in the tab with its message.
	Resources func(ctx context.Context, key string) ([]MultiDiffResource, error)
}

// MultiDiffRegistration is a registered MultiDiffProvider.
type MultiDiffRegistration interface {
	// Changed tells the window that the files of the sets with these keys may have changed (the group of changes got another file):
	// the open editors of them ask the provider again, off the UI thread, and keep the sections of the files that are still there. A
	// key that is not open costs nothing.
	Changed(keys ...string)
	// Dispose takes the provider away; it happens by itself when the activation ends. The tabs of its sets stay open with the files
	// they have.
	Dispose()
}

// MultiDiffSpec describes a multi-file diff editor to open (Documents().OpenMultiDiff; VS Code's _workbench.openMultiDiffEditor).
type MultiDiffSpec struct {
	// Title is the name of the tab ("Git: Changes", "a1b2c3d - Fix the loader"); the tab adds the number of files.
	Title string
	// Kind and Key name the set: the provider of the Kind makes its files, and the saved tab comes back through them. The same Kind
	// and Key is one tab: opening it again shows the open one and asks the provider again. Both are empty for a fixed set.
	Kind, Key string
	// Resources is a fixed set, used when Kind is empty. A tab of a fixed set is not saved with the window.
	Resources []MultiDiffResource
	// Reveal, when set, is the Modified document (or the Original of a deleted file) of the file to scroll to.
	Reveal string
	// Preview opens the editor as a preview tab, which the next preview replaces; false keeps it.
	Preview bool
	// PreserveFocus keeps the keyboard where it is.
	PreserveFocus bool
}

// Valid reports whether the spec names a provider's set (Kind and Key) or gives a fixed set, and not both or neither.
func (s MultiDiffSpec) Valid() bool {
	if s.Kind != "" {
		return s.Key != "" && len(s.Resources) == 0
	}
	return s.Key == "" && len(s.Resources) > 0
}

// FileName is the name of the file of a resource as its header shows it: the last element of Path.
func (r MultiDiffResource) FileName() string {
	if i := strings.LastIndexByte(r.Path, '/'); i >= 0 {
		return r.Path[i+1:]
	}
	return r.Path
}

// Folder is the folder of the file of a resource as its header shows it: Path without the last element; empty for a file at
// the root.
func (r MultiDiffResource) Folder() string {
	if i := strings.LastIndexByte(r.Path, '/'); i >= 0 {
		return r.Path[:i]
	}
	return ""
}
