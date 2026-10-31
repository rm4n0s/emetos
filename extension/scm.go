package extension

// The Source Control API (SPEC §10.30 "The SCM API", step 24.2; after vscode.scm, SourceControl, SourceControlResourceGroup and
// SourceControlResourceState of vscode.d.ts and extHostSCM.ts). As in VS Code the workbench owns the Source Control UI and an
// extension is a provider of it: it describes a source control, its groups and their resources, and the host draws the view, the
// input, the badge and the status bar items from that description and runs the commands the extension names.
//
// Everything here may be called from any goroutine, except where a comment says that Emetos calls the extension (the callbacks
// run on the UI thread). Nothing but Create and CreateGroup can fail: a call on an object of a deactivated extension, or on one
// that was disposed, does nothing.

// SourceControls creates the source controls of an extension: Git has one for each repository it opened.
type SourceControls interface {
	// Create adds a source control to the Source Control view of the scope. spec.ID names the kind of provider ("git") and is
	// what the context key scmProvider holds for the menus of the source control; it may be used for several source controls with
	// different roots, and a second Create with the same ID and the same RootURI by the extension fails with
	// ExtensionSCMDuplicate. It fails with ExtensionSCMInvalid for an empty ID or label, ExtensionSCMUnavailable in a scope that
	// has no Source Control model and ExtensionDeactivated after the activation ended. The source control is disposed with the
	// activation.
	Create(spec SourceControlSpec) (SourceControl, error)
}

// SourceControlSpec describes a source control (VS Code's createSourceControl(id, label, rootUri)).
type SourceControlSpec struct {
	// ID is the id of the provider: "git". The menus read it as the context key scmProvider.
	ID string
	// Label is the human readable name of the source control ("Git"); it is the name of the repository when RootURI is empty.
	Label string
	// RootURI is the folder of the repository as file:///path, or empty for a source control that has none. The repository of an
	// open file is the one with the deepest root that holds it.
	RootURI string
	// ContextValue is what the context key scmProviderContext holds for the menus of this source control (VS Code's
	// contextValue): "repository", "submodule".
	ContextValue string
}

// SourceControl is a source control the extension created.
type SourceControl interface {
	// Handle is the id the host gave the source control, unique in the scope ("scm3"): it is the member "sourceControl" of the
	// arguments that the menus of the view give a command (SCMArgs), and tells two source controls of one provider apart.
	Handle() string
	// SetCount sets the number on the badge of the repository and of the Source Control icon of the activity bar
	// (scm.countBadge). A negative count (the default) is VS Code's undefined: the number of resources of all the groups.
	SetCount(count int)
	// SetStatusBarCommands sets the items that show the state of the repository (the branch and the sync item): in the header of
	// the repository and, for the active repository, in the status bar. The first is at the left end.
	SetStatusBarCommands(commands []StatusCommand)
	// SetActiveRepositoryName sets the name that the title bar and the context key scmActiveRepositoryName show for the source
	// control while it is the active one (VS Code's activeRepositoryName: the name of a worktree); empty goes back to the name of
	// the root folder.
	SetActiveRepositoryName(name string)
	// SetBusy shows the progress bar under the header of the repository while a command runs.
	SetBusy(busy bool)
	// Input is the commit message box of the source control.
	Input() SCMInput
	// SetAcceptInputCommand sets the command that Ctrl+Enter in the input runs (scm.acceptInput); an empty ID clears it.
	SetAcceptInputCommand(command Command)
	// SetActionButton sets the button under the input, or removes it with nil.
	SetActionButton(button *ActionButton)
	// CreateGroup adds a group of resources below the previous ones. It fails with ExtensionSCMGroupInvalid for an empty ID,
	// ExtensionSCMGroupDuplicate for an ID the source control has already, ExtensionSCMDisposed for a source control that was
	// disposed and ExtensionDeactivated after the activation ended.
	CreateGroup(spec ResourceGroupSpec) (ResourceGroup, error)
	// Dispose removes the source control and its groups from the view. It happens by itself when the activation ends.
	Dispose()
	// TODO(step 24.11): AddQuickDiffProvider and RefreshQuickDiff.
	// TODO(step 24.20): SetHistoryProvider.
}

// ValidationLevel is the severity of a Validation (VS Code's SourceControlInputBoxValidationType).
type ValidationLevel int

// The levels of a Validation, in VS Code's order.
const (
	// ValidationError is shown in the colors inputValidation.error*.
	ValidationError ValidationLevel = iota
	// ValidationWarning is shown in the colors inputValidation.warning*.
	ValidationWarning
	// ValidationInfo is shown in the colors inputValidation.info*.
	ValidationInfo
)

// Validation is the message shown under the input for the text in it.
type Validation struct {
	// Message is the text; Markdown is not drawn as such yet.
	Message string
	// Level is the severity.
	Level ValidationLevel
}

// SCMInput is the commit message box of a source control (VS Code's SourceControlInputBox).
type SCMInput interface {
	// Value is the text now in the box.
	Value() string
	// SetValue replaces the text. OnChange is called, as it is for the user's typing.
	SetValue(value string)
	// OnChange calls fn, on the UI thread, with the new text each time it changes, and returns the function that stops it. A
	// panic in fn deactivates the extension.
	OnChange(fn func(value string)) (cancel func())
	// SetPlaceholder sets the text shown while the box is empty ("Message (Ctrl+Enter to commit on \"main\")").
	SetPlaceholder(placeholder string)
	// SetValidation sets the function that checks the text on each change; it returns nil for no message. fn is called on the UI
	// thread and must be fast; a panic in it deactivates the extension and shows no message. nil removes the check.
	SetValidation(fn func(value string) *Validation)
	// SetVisible shows or hides the box (it is shown by default).
	SetVisible(visible bool)
	// SetEnabled enables or disables the box (it is enabled by default).
	SetEnabled(enabled bool)
	// Focus gives the box the keyboard focus, in the view.
	Focus()
}

// StatusCommand is an item that shows the state of a repository: the branch item and the sync item of Git.
type StatusCommand struct {
	// Text is the label; "$(git-branch) main" has the Codicon git-branch before the text.
	Text string
	// Tooltip is the text of the hover.
	Tooltip string
	// Command is the id of the command a click runs; empty for an item that only shows.
	Command string
	// Args are the JSON-compatible arguments of Command.
	Args any
}

// ActionButton is the button under the input (VS Code's SourceControlActionButton).
type ActionButton struct {
	// Command is the primary command; its Title is the label of the button and may start with "$(icon) ".
	Command Command
	// Secondary are the groups of commands in the dropdown of the button, a separator between the groups.
	Secondary [][]Command
	// Enabled says whether the button can be pressed.
	Enabled bool
	// Description is the text of the tooltip.
	Description string
}

// ResourceGroupSpec describes a group of resources (VS Code's createResourceGroup(id, label)).
type ResourceGroupSpec struct {
	// ID is the id of the group ("index", "workingTree", "merge"); the menus read it as the context key scmResourceGroup.
	ID string
	// Label is the title of the group ("Staged Changes").
	Label string
	// HideWhenEmpty hides the group while it has no resources.
	HideWhenEmpty bool
	// ContextValue is what the context key scmResourceGroupState holds for the menus of the group.
	ContextValue string
}

// ResourceGroup is a group of resources of a source control (VS Code's SourceControlResourceGroup).
type ResourceGroup interface {
	// ID is the id the group was created with.
	ID() string
	// SetResources replaces the resources of the group in one call. The host draws them lazily: 100,000 are fine.
	SetResources(resources []ResourceState)
	// SetLabel changes the title of the group.
	SetLabel(label string)
	// SetHideWhenEmpty says whether the group is hidden while it has no resources.
	SetHideWhenEmpty(hide bool)
	// Dispose removes the group; it happens by itself with the source control.
	Dispose()
}

// ResourceState is a changed file of a group (VS Code's SourceControlResourceState).
type ResourceState struct {
	// URI is the file as file:///path.
	URI string
	// Command is run on a click on the row (the diff of the change); nil for none.
	Command *Command
	// ContextValue is what the context key scmResourceState holds for the menus of the resource.
	ContextValue string
	// Decorations say how the row looks.
	Decorations ResourceDecorations
	// MultiFileDiffEditorOriginalURI and MultiFileDiffEditorModifiedURI are the two sides of the file in the multi-file diff
	// editor (Open All Changes); empty when the file has no such side.
	MultiFileDiffEditorOriginalURI, MultiFileDiffEditorModifiedURI string
}

// ResourceDecorations are the looks of a resource row (VS Code's SourceControlResourceDecorations).
type ResourceDecorations struct {
	// Icon is the name of a Codicon or the path of an image file.
	Icon string
	// Tooltip is the hover of the row; it is also what the sort by status compares ("Modified", "Deleted").
	Tooltip string
	// StrikeThrough draws the name struck through (a deleted file).
	StrikeThrough bool
	// Faded draws the row dimmed.
	Faded bool
	// Color is the id of a theme color the letter or the icon of the row is drawn in ("gitDecoration.modifiedResourceForeground").
	Color string
	// Letter is the status letter of the row ("M", "A", "D", "U"; at most two characters), drawn in Color. VS Code has no such
	// field: its rows get the letter from the file decoration service, which the Git extension feeds in step 24.6 and which then
	// replaces this field (decision 459). Until then the answer of that service wins when there is one.
	Letter string
}

// The ids of the menus of the Source Control view and of the places that Git adds to; the values are the MenuId names of VS
// Code's workbench, as the other Menu constants (the contribution points of a VS Code package.json, scm/title and the like, are
// named by the keys of its menus).
const (
	// MenuSCMTitle is the title bar of the Source Control view and of each repository header (scm/title).
	MenuSCMTitle = "SCMTitle"
	// MenuSCMRepository is the actions inline in the header of a repository (scm/repository).
	MenuSCMRepository = "SCMSourceControlInline"
	// MenuSCMSourceControl is the context menu of a repository (scm/sourceControl).
	MenuSCMSourceControl = "SCMSourceControl"
	// MenuSCMResourceGroupContext is the actions and the context menu of a group (scm/resourceGroup/context).
	MenuSCMResourceGroupContext = "SCMResourceGroupContext"
	// MenuSCMResourceStateContext is the actions and the context menu of a resource (scm/resourceState/context).
	MenuSCMResourceStateContext = "SCMResourceContext"
	// MenuSCMResourceFolderContext is the actions and the context menu of a folder in the tree view (scm/resourceFolder/context).
	MenuSCMResourceFolderContext = "SCMResourceFolderContext"
	// MenuSCMChangeTitle is the title bar of the peek of a change (scm/change/title).
	MenuSCMChangeTitle = "SCMChangeContext"
	// MenuSCMHistoryTitle is the title bar of the Graph view (scm/history/title).
	MenuSCMHistoryTitle = "SCMHistoryTitle"
	// MenuSCMHistoryItemContext is the context menu of a commit of the Graph (scm/historyItem/context).
	MenuSCMHistoryItemContext = "SCMHistoryItemContext"
	// MenuSCMHistoryItemRefContext is the context menu of a branch or tag badge of the Graph (scm/historyItemRef/context).
	MenuSCMHistoryItemRefContext = "SCMHistoryItemRefContext"
	// MenuTimelineItemContext is the context menu of an item of the Timeline view (timeline/item/context).
	MenuTimelineItemContext = "TimelineItemContext"
	// MenuEditorContent is the prominent buttons over the content of an editor (editor/content).
	MenuEditorContent = "EditorContent"
	// MenuEditorTitle is the actions in the title bar of an editor (editor/title).
	MenuEditorTitle = "EditorTitle"
	// MenuDiffEditorGutterHunk is the toolbar in the gutter of a hunk of the diff editor (diffEditor/gutter/hunk).
	MenuDiffEditorGutterHunk = "DiffEditorHunkToolbar"
	// MenuDiffEditorGutterSelection is the toolbar in the gutter of a selection of the diff editor (diffEditor/gutter/selection).
	MenuDiffEditorGutterSelection = "DiffEditorSelectionToolbar"
)

// SubmenuSpec declares a submenu (VS Code's contributes.submenus): a menu that menu items can be put in with their Menu, and
// that another menu opens with a MenuItem whose Submenu is its ID. Git has Commit, Changes, Pull/Push, Branch, Remote, Stash and
// Tags.
type SubmenuSpec struct {
	// ID is the id of the submenu: letters, digits, dots, hyphens and underscores, starting with a letter. It must not be under
	// the id of another extension, nor under emetos. unless it is under the id of the extension itself.
	ID string
	// Label is the title of the row that opens it.
	Label string
}

// The members of the arguments that a menu of the Source Control view gives a command (SCMArgs).
const (
	// SCMArgSourceControl is the Handle of the source control.
	SCMArgSourceControl = "sourceControl"
	// SCMArgGroup is the ID of the group of a group or a resource.
	SCMArgGroup = "group"
	// SCMArgURI is the URI of a resource.
	SCMArgURI = "uri"
	// SCMArgContextValue is the ContextValue of a group or a resource.
	SCMArgContextValue = "contextValue"
)

// SCMArg is one of the arguments that a command gets from a menu of the Source Control view: a source control, a group or a
// resource. VS Code passes them as separate arguments (the source control for scm/title, the group, the resource states of the
// selection); Emetos gives the command one value, a list of objects {"sourceControl": "scm1", "group": "index", "uri":
// "file:///a.go", "contextValue": "..."} in the order VS Code passes them, which SCMArgs reads.
type SCMArg struct {
	// SourceControl is the Handle of the source control the object belongs to.
	SourceControl string
	// Group is the ID of the group; empty for a source control.
	Group string
	// URI is the URI of the resource; empty for a source control and a group.
	URI string
	// ContextValue is the ContextValue of the group or the resource; empty for a source control.
	ContextValue string
}

// IsSourceControl tells whether the argument is a source control.
func (a SCMArg) IsSourceControl() bool { return a.Group == "" && a.URI == "" }

// IsGroup tells whether the argument is a group.
func (a SCMArg) IsGroup() bool { return a.Group != "" && a.URI == "" }

// IsResource tells whether the argument is a resource.
func (a SCMArg) IsResource() bool { return a.URI != "" }

// SCMArgs reads the arguments a menu of the Source Control view gave a command: the objects of the list, in order; nothing for
// any other value (the command was run from the palette or by a key).
func SCMArgs(args any) []SCMArg {
	var list []any
	switch v := args.(type) {
	case []any:
		list = v
	case []map[string]any:
		for _, m := range v {
			list = append(list, m)
		}
	default:
		return nil
	}
	var out []SCMArg
	for _, e := range list {
		m, ok := e.(map[string]any)
		if !ok {
			continue
		}
		var a SCMArg
		a.SourceControl, _ = m[SCMArgSourceControl].(string)
		a.Group, _ = m[SCMArgGroup].(string)
		a.URI, _ = m[SCMArgURI].(string)
		a.ContextValue, _ = m[SCMArgContextValue].(string)
		out = append(out, a)
	}
	return out
}
