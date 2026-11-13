package extension

import (
	"encoding/json"
	"strings"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/widgets"
)

// File editors (SPEC §10.37 "File editors", D52): an extension can be the editor of a file, as VS Code's read-only custom editors
// are. A file whose base name matches the globs of a default file editor opens in it instead of the text editor; "Reopen Editor
// With..." switches between the text editor and the file editors that match, and workbench.editorAssociations chooses.

// FileEditorPriority says when a file editor is used.
type FileEditorPriority string

// The priorities of a file editor.
const (
	// FileEditorDefault: a file that matches opens in the editor.
	FileEditorDefault FileEditorPriority = "default"
	// FileEditorOption: the editor is used only when the user chooses it (Reopen Editor With..., workbench.editorAssociations).
	FileEditorOption FileEditorPriority = "option"
)

// onFileEditorPrefix is the prefix of the activation event of a file editor.
const onFileEditorPrefix = "onFileEditor:"

// OnFileEditor is the event of a file being shown in the file editor with the id; an extension that declares the editor is
// activated by it without listing it.
func OnFileEditor(editorID string) string { return onFileEditorPrefix + editorID }

// FileEditorSpec declares a file editor.
type FileEditorSpec struct {
	// ID identifies the editor. It starts with the id of the extension and a dot (emetos.mediapreview.image).
	ID string
	// Name is what Reopen Editor With shows ("Image Preview").
	Name string
	// Icon is the name of the Codicon of the tab; may be empty (the file's icon is shown then).
	Icon string
	// Globs are patterns of file names, matched without regard to case against the base name (*.png, *.{jpg,jpeg}).
	Globs []string
	// Priority is FileEditorDefault or FileEditorOption.
	Priority FileEditorPriority
}

// FileEditorChange tells a file editor that its file changed on disk.
type FileEditorChange struct {
	// Deleted is true when the file is gone.
	Deleted bool
}

// FileEditorFile is the file a file editor shows. Its methods run on the UI thread.
type FileEditorFile interface {
	// Path is the absolute path of the file, URI its file:// URI, and Folder the folder of the workspace (empty for none).
	Path() string
	URI() string
	Folder() string
	// State is what SaveState gave when the tab was saved with the window, for a tab that comes back; nil for a new tab.
	State() json.RawMessage
	// OnChange calls fn, on the UI thread, when the file changes or is deleted on disk; the calls stop with the returned function
	// and when the tab closes.
	OnChange(fn func(FileEditorChange)) (cancel func())
}

// FileEditorTab is the tab of a file editor. Its methods run on the UI thread.
type FileEditorTab interface {
	// SetBusy puts the mark of work that goes on (a file being read) on the tab.
	SetBusy(busy bool)
	// Refresh builds the widget of the tab again.
	Refresh()
	// SetStatus sets the status bar items of the tab, shown while the tab is the active editor of the window and hidden when it
	// is not (the size of an image and its zoom). Their IDs are the tab's own; nil takes them away.
	SetStatus(items []StatusItem)
	// Group is the id of the group that shows the tab, or 0.
	Group() int
}

// FileEditorContent is what a file editor puts in its tab. All the functions run on the UI thread, and a panic in any of them
// deactivates the extension in the scope.
type FileEditorContent struct {
	// Build makes the widget of the tab, inside a box that gives it the constraints of the editor area, clips it, keeps the
	// keyboard focus and catches a panic.
	Build func(bc widgets.BuildContext) widgets.Widget
	// SaveState returns the state of the tab that is kept with the window (a value that encodes to JSON, nil for none): the zoom,
	// the page, the place in a book. FileEditorFile.State gives it back when the tab comes back, and a split tab starts with it.
	SaveState func() (any, error)
	// OnClose is called when the tab is closed for good, or reopened in another editor.
	OnClose func()
}

// FileEditorProvider is how an extension makes the content of the tabs of one file editor.
type FileEditorProvider struct {
	// Open makes the content of a tab for a file, once for each tab, on the UI thread. An error shows in the tab, which offers
	// the text editor then.
	Open func(file FileEditorFile, tab FileEditorTab) (FileEditorContent, error)
}

// FileEditors gives the extension the editors of files it declares.
type FileEditors interface {
	// Provide sets how the tabs of a file editor the manifest declares are made, and makes the tabs of it that wait for the
	// extension. It fails with ExtensionFileEditorNotDeclared when the manifest does not declare the editor,
	// ExtensionFileEditorProvided when the activation provided it already, ExtensionFileEditorInvalid when Open is nil and
	// ExtensionDeactivated after the activation ended.
	Provide(editorID string, p FileEditorProvider) error
}

// checkFileEditors checks the file editors a manifest declares: the ids are under the id of the extension and new (among its editor
// kinds and file views too), each has a name, globs that parse and a known priority.
func checkFileEditors(m Manifest) error {
	prefix := m.ID + "."
	ids := map[string]bool{}
	for _, k := range m.Contributes.EditorKinds {
		ids[k.ID] = true
	}
	for _, v := range m.Contributes.FileViews {
		ids[v.ID] = true
	}
	for _, e := range m.Contributes.FileEditors {
		switch {
		case !strings.HasPrefix(e.ID, prefix) || len(e.ID) == len(prefix):
			return errors.New("ExtensionFileEditorInvalid", "the id of a file editor starts with the id of the extension and a dot", "id", m.ID, "editor", e.ID)
		case ids[e.ID]:
			return errors.New("ExtensionFileEditorDuplicate", "two editors of the extension have the same id", "id", m.ID, "editor", e.ID)
		case strings.TrimSpace(e.Name) == "":
			return errors.New("ExtensionFileEditorInvalid", "a file editor needs a name", "id", m.ID, "editor", e.ID)
		case len(e.Globs) == 0:
			return errors.New("ExtensionFileEditorInvalid", "a file editor needs the globs of the files it is for", "id", m.ID, "editor", e.ID)
		case e.Priority != FileEditorDefault && e.Priority != FileEditorOption:
			return errors.New("ExtensionFileEditorInvalid", "the priority of a file editor is default or option", "id", m.ID, "editor", e.ID, "priority", string(e.Priority))
		}
		for _, g := range e.Globs {
			if !globWellFormed(g) {
				return errors.New("ExtensionFileEditorInvalid", "a glob of a file editor is empty, has a slash or has unbalanced brackets or braces", "id", m.ID, "editor", e.ID, "glob", g)
			}
		}
		ids[e.ID] = true
	}
	return nil
}

// globWellFormed tells whether a glob of file names can be matched: not empty, no slash (it is matched against a base name), and its
// brackets and braces balanced and not nested in a way that cannot close.
func globWellFormed(g string) bool {
	if strings.TrimSpace(g) == "" || strings.ContainsAny(g, `/\`) {
		return false
	}
	braces, bracket := 0, false
	for _, r := range g {
		switch {
		case bracket:
			if r == ']' {
				bracket = false
			}
		case r == '[':
			bracket = true
		case r == ']':
			return false
		case r == '{':
			braces++
		case r == '}':
			braces--
			if braces < 0 {
				return false
			}
		}
	}
	return braces == 0 && !bracket
}
