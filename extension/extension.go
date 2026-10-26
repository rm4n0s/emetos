package extension

import (
	"context"
	"io/fs"
	"strings"
)

// Factory makes an Extension. Emetos calls it once to read the Manifest, and again for every scope in which the
// extension is activated, so two window tabs never share an instance. It must not start anything: all the work is in
// Activate.
type Factory func() Extension

// Extension is what an extension's package provides.
type Extension interface {
	// Manifest describes the extension. It is read before any activation, and what it declares (commands, menu items,
	// keybindings) is there before the extension is activated.
	Manifest() Manifest
	// Activate starts the extension in one scope. ctx is cancelled when the extension is deactivated there; api is
	// bound to the scope and stops working then. Contributions made through api are removed at deactivation. An error
	// from Activate, or a panic, deactivates the extension in this scope for the rest of the session.
	Activate(ctx context.Context, api API) error
}

// Manifest describes an extension (SPEC §10.15).
type Manifest struct {
	// ID is unique and written in lowercase letters, digits and hyphens joined by dots: emetos.markdown, sqltools. The
	// settings of the extension are registered under it as prefix (sqltools.connections).
	ID string
	// Name is the name shown to the user. It is required.
	Name string
	// Version is the version of the extension, such as 0.1.0.
	Version string
	// Description says in one sentence what the extension does.
	Description string
	// ActivationEvents say when the extension is activated in a scope; see OnStartup, OnLanguage, OnFile, OnCommand,
	// OnView and OnWindowTab. An extension with none is never activated by itself. The commands in Contributes are
	// activation events of their own: running one activates the extension.
	ActivationEvents []string
	// Contributes are the contributions the extension declares, so that they are there before it is activated.
	Contributes Contributes
	// Events are the event topics the extension declares (SPEC §10.15 "Events").
	Events EventDecl
}

// Contributes are the contributions a Manifest declares. Emetos installs them in a scope when the extension is enabled
// there and removes them when it is disabled or the scope closes.
type Contributes struct {
	// Commands are listed in the command palette. A command whose handler is not set yet (see Commands.Handle)
	// activates the extension when it is run.
	Commands []CommandSpec
	// Menus are items of menus; the Command of each is a command of the extension or of the editor.
	Menus []MenuItem
	// Keybindings are default keybindings of commands.
	Keybindings []Keybinding
	// ViewContainers are the containers of the side bar the extension adds: an icon in the activity bar each.
	ViewContainers []ViewContainerSpec
	// Views are the views of the side bar, in the containers of the extension or of Emetos. Showing one activates the
	// extension (the activation event OnView).
	Views []ViewSpec
	// PanelViews are the tabs of the panel. Showing one activates the extension.
	PanelViews []PanelViewSpec
	// ViewActions are the title actions of the views.
	ViewActions []ViewActionSpec
	// EditorKinds are the kinds of editor tab the extension adds. Opening or restoring a tab of a kind activates the
	// extension (the activation event OnEditor).
	EditorKinds []EditorKindSpec
	// EditorActions are the icons at the right end of the tab strip of every editor group.
	EditorActions []EditorActionSpec
	// FileViews are the micro-tabs of the editor tab of the files they select. Showing one activates the extension.
	FileViews []FileViewSpec
	// WindowTabKinds are the kinds of window tab the extension adds: an entry of the "+" menu of the strip each. Opening or
	// restoring a tab of a kind activates the extension, in the scope of that tab (the activation event OnWindowTab).
	WindowTabKinds []WindowTabKindSpec
	// Languages is the language pack of the extension: a file system (an embed.FS, rooted with fs.Sub) that has the
	// package.json of a VS Code extension at its root, with the languages (contributes.languages), the grammars
	// (contributes.grammars) and the settings defaults of languages (contributes.configurationDefaults) it contributes, and
	// the files they name (the language configurations, the grammars). It is nil for an extension that adds no language.
	// Emetos adds the languages to its registry while the extension is enabled in at least one window tab, before any file
	// opens, and takes them away when the last tab that has the extension enabled closes or turns it off (SPEC §10.15
	// "As built (step 11.4)"). It needs no activation: the languages are there whether or not the extension was activated.
	Languages fs.FS
	// LanguageServers are the language servers the extension runs, one per window tab (SPEC §9.12 "Servers and their
	// lifetime"). They are started by the host, not by Activate; the extension gets the server's Output channel, status and
	// restart command for free. Declaring one adds the command <extension id>.restartServer.
	LanguageServers []LanguageServerSpec
}

// The activation events of a Manifest.
const (
	// OnStartup activates the extension when a scope is made.
	OnStartup = "onStartup"
	// onLanguagePrefix and its siblings are the prefixes of the activation events that take an argument.
	onLanguagePrefix  = "onLanguage:"
	onFilePrefix      = "onFile:"
	onCommandPrefix   = "onCommand:"
	onViewPrefix      = "onView:"
	onEditorPrefix    = "onEditor:"
	onWindowTabPrefix = "onWindowTab:"
)

// OnLanguage is the event of a document of the language with the id (go, markdown) being open in the scope.
func OnLanguage(languageID string) string { return onLanguagePrefix + languageID }

// OnFile is the event of a file whose path relative to the folder of the scope matches the glob being in the folder:
// OnFile("go.mod") for a go.mod in the root, OnFile("**/go.mod") for one anywhere. It never happens in a scope with
// no folder.
func OnFile(glob string) string { return onFilePrefix + glob }

// OnCommand is the event of the command with the id being run.
func OnCommand(commandID string) string { return onCommandPrefix + commandID }

// OnView is the event of the view with the id being shown (from the step that adds views).
func OnView(viewID string) string { return onViewPrefix + viewID }

// OnEditor is the event of a tab of the editor kind with the id being shown (a restored tab, or one that is opened); an
// extension that declares the kind is activated by it without listing it.
func OnEditor(kindID string) string { return onEditorPrefix + kindID }

// OnWindowTab is the event of a window tab of the kind with the id being opened or restored; an extension that declares the
// kind is activated by it without listing it.
func OnWindowTab(kind string) string { return onWindowTabPrefix + kind }

// SplitEvent splits an activation event into its name and argument: "onLanguage:go" is ("onLanguage", "go"), and
// "onStartup" is ("onStartup", "").
func SplitEvent(event string) (name, arg string) {
	name, arg, _ = strings.Cut(event, ":")
	return name, arg
}

// ScopeKind tells what a scope is.
type ScopeKind string

// The kinds of scope.
const (
	// ScopeWorkspace is a window tab that holds a workspace: a folder, or none.
	ScopeWorkspace ScopeKind = "workspace"
	// ScopeWindowTab is a window tab of an extension (a kind of Contributes.WindowTabKinds). Only the extension that owns
	// the kind is activated in it; it has no folder, no documents and no editor area.
	ScopeWindowTab ScopeKind = "windowTab"
)

// ScopeInfo describes the scope an API is bound to.
type ScopeInfo struct {
	// Kind is what the scope is.
	Kind ScopeKind
	// Name is the name of the window tab: the base name of the folder, or "No Folder"; the title of the kind for the tab
	// of an extension.
	Name string
	// Folder is the absolute path of the folder of a workspace scope, or empty.
	Folder string
	// WindowTab is the id of the kind of window tab of a ScopeWindowTab scope; empty for a workspace.
	WindowTab string
	// ID names the scope in the links between window tabs (SPEC §10.15 "Event sharing"): the cleaned absolute folder of
	// a workspace, "<extension id>:<tab id>" for the tab of an extension. It is empty for a scope that cannot be named
	// in a link, such as a workspace tab with no folder: it sends nothing to other tabs and receives nothing from them.
	ID string
}
