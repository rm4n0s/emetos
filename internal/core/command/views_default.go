package command

// The ids of the view containers and views of the window.
const (
	// ViewContainerExplorer is the Explorer, the first container of the activity bar.
	ViewContainerExplorer = "workbench.view.explorer"
	// ViewExplorerFolders is the view of the folder in the Explorer.
	ViewExplorerFolders = "workbench.explorer.fileView"
)

// codiconFiles is the code point of the Codicon "files" (fonts.CodiconFiles in gui/kit/fonts).
const codiconFiles = 0xEAF0

// DefaultViews makes a registry with the view containers and the views of the window (SPEC §10.4). The
// steps that build the views of a container (the Explorer in 6.14, Search, Source Control, Run and
// Debug) register their containers and views here, with the id, title and icon VS Code gives them;
// until then a view is a header with an empty body.
//
// Ported from VS Code: src/vs/workbench/contrib/files/browser/explorerViewlet.ts
func DefaultViews() *ViewRegistry {
	r := NewViewRegistry()
	r.MustRegisterContainer(ViewContainer{ID: ViewContainerExplorer, Title: "Explorer", Icon: codiconFiles, Order: 0})
	r.MustRegisterViews(View{ID: ViewExplorerFolders, Name: "Folders", Container: ViewContainerExplorer, Order: 1})
	return r
}
