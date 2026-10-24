package command

// The ids of the view containers and views of the window.
const (
	// ViewContainerExplorer is the Explorer, the first container of the activity bar.
	ViewContainerExplorer = "workbench.view.explorer"
	// ViewExplorerFolders is the view of the folder in the Explorer.
	ViewExplorerFolders = "workbench.explorer.fileView"
	// ViewContainerMarkers is the Problems tab of the panel.
	ViewContainerMarkers = "workbench.panel.markers"
	// ViewMarkers is the view of the Problems tab.
	ViewMarkers = "workbench.panel.markers.view"
	// ViewContainerOutput is the Output tab of the panel.
	ViewContainerOutput = "workbench.panel.output"
	// ViewOutput is the view of the Output tab.
	ViewOutput = "workbench.panel.output"
)

// codiconFiles is the code point of the Codicon "files" (fonts.CodiconFiles in gui/kit/fonts).
const codiconFiles = 0xEAF0

// codiconWarning and codiconOutput are the code points of the Codicons "warning" and "output".
const (
	codiconWarning = 0xEA6C
	codiconOutput  = 0xEB9D
)

// DefaultViews makes a registry with the view containers and the views of the window (SPEC §10.4). The
// steps that build the views of a container (the Explorer in 6.14, Search, Source Control, Run and
// Debug) register their containers and views here, with the id, title and icon VS Code gives them;
// until then a view is a header with an empty body.
//
// The panel has the containers of the Problems and the Output (steps for the Debug Console and the
// Terminal add theirs), each with the one view that VS Code registers in it.
//
// Ported from VS Code: src/vs/workbench/contrib/files/browser/explorerViewlet.ts,
// src/vs/workbench/contrib/markers/browser/markers.contribution.ts and
// src/vs/workbench/contrib/output/browser/output.contribution.ts
func DefaultViews() *ViewRegistry {
	r := NewViewRegistry()
	r.MustRegisterContainer(ViewContainer{ID: ViewContainerExplorer, Title: "Explorer", Icon: codiconFiles, Order: 0})
	r.MustRegisterViews(View{ID: ViewExplorerFolders, Name: "Folders", Container: ViewContainerExplorer, Order: 1})
	r.MustRegisterContainer(ViewContainer{ID: ViewContainerMarkers, Title: "Problems", Icon: codiconWarning, Order: 0, Location: ViewLocationPanel})
	r.MustRegisterViews(View{ID: ViewMarkers, Name: "Problems", Container: ViewContainerMarkers})
	r.MustRegisterContainer(ViewContainer{ID: ViewContainerOutput, Title: "Output", Icon: codiconOutput, Order: 1, Location: ViewLocationPanel})
	r.MustRegisterViews(View{ID: ViewOutput, Name: "Output", Container: ViewContainerOutput})
	return r
}
