// Package references is VS Code's built-in extension "references-view" (SPEC §10.28, D30): Find All References and Find All
// Implementations of the symbol at the cursor, and a view in the side bar that lists the results by file, with the history of
// the searches. The language servers and the providers answer through Languages().References and Implementations.
//
// Ported from VS Code: extensions/references-view/package.json, src/extension.ts, src/references/index.ts,
// src/references/model.ts, src/tree.ts, src/navigation.ts and src/utils.ts. Not built: Show Call Hierarchy and Show Type
// Hierarchy, the highlights of the matches in the editors, dragging a result out of the view, and the setting
// references.preferredLocation.
package references

import (
	"context"

	"github.com/rm4n0s/emetos/extension"
)

// The ids of the extension. The commands are those of VS Code's extension; the container, the view and the context keys
// start with the id of the extension, as every extension's do (SPEC §10.15).
const (
	// ID is the id of the extension.
	ID = "emetos.references"
	// ContainerID and ViewID are the container of the side bar and its view (VS Code's references-view and
	// references-view.tree).
	ContainerID = "emetos.references.container"
	ViewID      = "emetos.references.tree"

	// FindReferences and FindImplementations ask the language server about the symbol at the cursor.
	FindReferences      = "references-view.findReferences"
	FindImplementations = "references-view.findImplementations"
	// Find is the legacy name of FindReferences.
	Find = "references-view.find"
	// ClearHistory forgets the searches; Clear empties the view; Refresh runs the last search again; PickFromHistory chooses an
	// earlier search to run again.
	ClearHistory    = "references-view.clearHistory"
	Clear           = "references-view.clear"
	Refresh         = "references-view.refresh"
	PickFromHistory = "references-view.pickFromHistory"
	// RemoveReferenceItem (Dismiss), Copy, CopyAll, CopyPath and Refind (Rerun) act on a row of the view: the one named by the
	// argument {"key": ...}, else the selected one.
	RemoveReferenceItem = "references-view.removeReferenceItem"
	Copy                = "references-view.copy"
	CopyAll             = "references-view.copyAll"
	CopyPath            = "references-view.copyPath"
	Refind              = "references-view.refind"
	// Next and Prev go to the next and the previous location (F4 and Shift+F4).
	Next = "references-view.next"
	Prev = "references-view.prev"
	// CollapseAll folds the files of the view (VS Code's built-in action of every tree view).
	CollapseAll = "workbench.actions.treeView.references-view.tree.collapseAll"
)

// The context keys the extension sets (VS Code's reference-list.isActive, reference-list.hasResult, reference-list.hasHistory,
// reference-list.source and references-view.canNavigate).
const (
	keyActive      = "emetos.references.isActive"
	keyHasResult   = "emetos.references.hasResult"
	keyHasHistory  = "emetos.references.hasHistory"
	keySource      = "emetos.references.source"
	keyCanNavigate = "emetos.references.canNavigate"
)

// New is the Factory of the extension.
func New() extension.Extension { return &references{} }

// references is the extension of one scope. Everything but the reading of the files is used on the UI thread.
type references struct {
	api    extension.API
	ctx    context.Context
	folder string
	view   *viewState
}

// Manifest implements extension.Extension.
func (*references) Manifest() extension.Manifest {
	const category = "References"
	cmd := func(id, title, cat string) extension.CommandSpec {
		return extension.CommandSpec{ID: id, Title: title, Category: cat}
	}
	item := func(id, title string) extension.CommandSpec { return cmd(id, title, "") }
	never := func(id string) extension.MenuItem {
		return extension.MenuItem{Menu: extension.MenuCommandPalette, Command: id, When: "false"}
	}
	palette := func(id, when string) extension.MenuItem {
		return extension.MenuItem{Menu: extension.MenuCommandPalette, Command: id, When: when}
	}
	return extension.Manifest{
		ID: ID, Name: "Reference Search View", Version: "0.1.0",
		Description: "Reference Search results as separate, stable view in the sidebar",
		Contributes: extension.Contributes{
			Commands: []extension.CommandSpec{
				cmd(FindReferences, "Find All References", category),
				cmd(FindImplementations, "Find All Implementations", category),
				cmd(ClearHistory, "Clear History", category),
				cmd(Clear, "Clear", category),
				cmd(Refresh, "Refresh", category),
				cmd(PickFromHistory, "Show History", category),
				item(RemoveReferenceItem, "Dismiss"),
				item(Copy, "Copy"),
				item(CopyAll, "Copy All"),
				item(CopyPath, "Copy Path"),
				item(Refind, "Rerun"),
				{ID: Next, Title: "Go to Next Reference", When: keyCanNavigate},
				{ID: Prev, Title: "Go to Previous Reference", When: keyCanNavigate},
				item(CollapseAll, "Collapse All"),
			},
			Menus: []extension.MenuItem{
				{Menu: extension.MenuEditorContext, Command: FindReferences, When: "editorHasReferenceProvider", Group: "0_navigation", Order: 1},
				{Menu: extension.MenuEditorContext, Command: FindImplementations, When: "editorHasImplementationProvider", Group: "0_navigation", Order: 2},
				never(RemoveReferenceItem), never(Copy), never(CopyAll), never(CopyPath), never(Refind), never(Next), never(Prev), never(CollapseAll),
				palette(FindReferences, "editorHasReferenceProvider"),
				palette(Clear, keyHasResult),
				palette(ClearHistory, keyActive+" && !"+keyHasResult),
				palette(Refresh, keyHasResult),
				palette(PickFromHistory, keyActive),
			},
			Keybindings: []extension.Keybinding{
				{Command: FindReferences, Key: "shift+alt+f12", When: "editorHasReferenceProvider"},
				{Command: Next, Key: "f4", When: keyHasResult},
				{Command: Prev, Key: "shift+f4", When: keyHasResult},
			},
			ViewContainers: []extension.ViewContainerSpec{{ID: ContainerID, Title: "References", Icon: "references", Order: 70}},
			Views:          []extension.ViewSpec{{ID: ViewID, Name: "References", Container: ContainerID}},
			ViewActions: []extension.ViewActionSpec{
				{View: ViewID, Command: Refresh, Icon: "refresh", Tooltip: "Refresh", When: keyHasResult, Order: 2},
				{View: ViewID, Command: Clear, Icon: "clear-all", Tooltip: "Clear", When: keyHasResult, Order: 3},
				{View: ViewID, Command: ClearHistory, Icon: "clear-all", Tooltip: "Clear History", When: keyHasHistory + " && !" + keyHasResult, Order: 3},
				{View: ViewID, Command: CollapseAll, Icon: "collapse-all", Tooltip: "Collapse All", When: keyHasResult, Order: 4},
			},
		},
	}
}

// Activate implements extension.Extension.
func (r *references) Activate(ctx context.Context, api extension.API) error {
	r.api, r.ctx, r.folder = api, ctx, api.Scope().Folder
	r.view = newViewState(r)
	if err := r.view.provide(); err != nil {
		return err
	}
	handlers := map[string]extension.CommandHandler{
		FindReferences: func(context.Context, any) error { r.view.findLocations(titleReferences, kindReferences); return nil },
		FindImplementations: func(context.Context, any) error {
			r.view.findLocations(titleImplementations, kindImplementations)
			return nil
		},
		ClearHistory:        func(context.Context, any) error { r.view.clearHistory(); return nil },
		Clear:               func(context.Context, any) error { r.view.clearInput(); return nil },
		Refresh:             func(context.Context, any) error { r.view.refresh(); return nil },
		PickFromHistory:     func(context.Context, any) error { return r.view.pickFromHistory() },
		RemoveReferenceItem: func(_ context.Context, args any) error { r.view.dismiss(args); return nil },
		Copy:                func(_ context.Context, args any) error { return r.view.copyItem(args) },
		CopyAll:             func(_ context.Context, args any) error { return r.view.copyAll(args) },
		CopyPath:            func(_ context.Context, args any) error { return r.view.copyPath(args) },
		Refind:              func(_ context.Context, args any) error { r.view.refind(args); return nil },
		Next:                func(context.Context, any) error { r.view.navigate(+1); return nil },
		Prev:                func(context.Context, any) error { r.view.navigate(-1); return nil },
		CollapseAll:         func(context.Context, any) error { r.view.collapseAll(); return nil },
	}
	for id, h := range handlers {
		if err := api.Commands().Handle(id, h); err != nil {
			return err
		}
	}
	// the legacy name of Find All References
	return api.Commands().Register(extension.CommandSpec{ID: Find}, func(context.Context, any) error {
		r.view.findLocations(titleReferences, kindReferences)
		return nil
	})
}
