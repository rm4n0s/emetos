package command

import "github.com/rm4n0s/emetos/internal/core/contextkey"

// The ids of the view containers and views of the window.
const (
	// ViewContainerExplorer is the Explorer, the first container of the activity bar.
	ViewContainerExplorer = "workbench.view.explorer"
	// ViewExplorerFolders is the view of the folder in the Explorer.
	ViewExplorerFolders = "workbench.explorer.fileView"
	// ViewExplorerEmpty is the view of the Explorer in a window that has no folder.
	ViewExplorerEmpty = "workbench.explorer.emptyView"
	// ViewOpenEditors is the Open Editors view of the Explorer.
	ViewOpenEditors = "workbench.explorer.openEditorsView"
	// ViewContainerSCM is the Source Control container (step 24.3, SPEC §10.30).
	ViewContainerSCM = "workbench.view.scm"
	// ViewSCMRepositories is the Repositories view of Source Control and ViewSCM its Changes view.
	ViewSCMRepositories = "workbench.scm.repositories"
	// ViewSCM is the Changes view of Source Control (VS Code's VIEW_PANE_ID).
	ViewSCM = "workbench.scm"
	// ViewSCMHistory is the Graph view of Source Control (VS Code's HISTORY_VIEW_PANE_ID), shown while a source control has a history
	// provider (step 24.20).
	ViewSCMHistory = "workbench.scm.history"
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

// codiconSourceControl is the code point of the Codicon "source-control".
const codiconSourceControl = 0xEA68

// codiconDebugAlt is the code point of the Codicon "debug-alt" (the icon of Run and Debug).
const codiconDebugAlt = 0xEB91

// codiconDebugConsole is the code point of the Codicon "debug-console".
const codiconDebugConsole = 0xEB9B

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
// The panel has the containers of the Problems, the Output and the Debug Console (the step for the
// Terminal adds its own), each with the one view that VS Code registers in it.
//
// Ported from VS Code: src/vs/workbench/contrib/files/browser/explorerViewlet.ts,
// src/vs/workbench/contrib/scm/browser/scm.contribution.ts, src/vs/workbench/contrib/markers/browser/markers.contribution.ts and
// src/vs/workbench/contrib/output/browser/output.contribution.ts
func DefaultViews() *ViewRegistry {
	r := NewViewRegistry()
	r.MustRegisterContainer(ViewContainer{ID: ViewContainerExplorer, Title: "Explorer", Icon: codiconFiles, Order: 0})
	// The Explorer has the Open Editors (hidden until it is shown), and either the folder or, in a window
	// with no folder, the empty view (workbenchState is "empty" or "folder").
	r.MustRegisterViews(
		View{ID: ViewOpenEditors, Name: "Open Editors", Container: ViewContainerExplorer, Order: 0, HideByDefault: true},
		View{ID: ViewExplorerEmpty, Name: "No Folder Opened", Container: ViewContainerExplorer, Order: 1,
			When: contextkey.Equals("workbenchState", "empty")},
		View{ID: ViewExplorerFolders, Name: "Folders", Container: ViewContainerExplorer, Order: 1,
			When: contextkey.NotEquals("workbenchState", "empty")},
		// the Outline starts folded, below the folder (step 12.7)
		View{ID: ViewOutline, Name: "Outline", Container: ViewContainerExplorer, Order: 2, Collapsed: true},
		// the Timeline starts folded, below the Outline (step 24.21)
		View{ID: ViewTimeline, Name: "Timeline", Container: ViewContainerExplorer, Order: 3, Collapsed: true},
	)
	// Source Control (order 2, after Search): the Repositories view is hidden until it is shown, and the Changes view is always
	// there (its welcome content says why it is empty). The Graph is shown while a source control has a history provider (step 24.20).
	r.MustRegisterContainer(ViewContainer{ID: ViewContainerSCM, Title: "Source Control", Icon: codiconSourceControl, Order: 2})
	r.MustRegisterViews(
		View{ID: ViewSCMRepositories, Name: "Repositories", Container: ViewContainerSCM, Order: 0, HideByDefault: true,
			When: when("scm.providerCount && scm.providerCount != 0")},
		View{ID: ViewSCM, Name: "Changes", Container: ViewContainerSCM, Order: 1},
		View{ID: ViewSCMHistory, Name: "Graph", Container: ViewContainerSCM, Order: 2, When: when("scm.historyProviderCount && scm.historyProviderCount != 0")},
	)
	// Run and Debug (order 3, after Source Control; step 25.4, SPEC §10.31): the welcome while debugUx is simple (no configuration
	// and no session), the views of a session otherwise. The Breakpoints show when one exists or a session ran. The weights are VS Code's.
	r.MustRegisterContainer(ViewContainer{ID: ViewContainerDebug, Title: "Run and Debug", Icon: codiconDebugAlt, Order: 3})
	r.MustRegisterViews(
		View{ID: ViewDebugWelcome, Name: "Run", Container: ViewContainerDebug, Order: 1, Weight: 40, When: contextkey.Equals("debugUx", "simple")},
		View{ID: ViewDebugVariables, Name: "Variables", Container: ViewContainerDebug, Order: 10, Weight: 40, When: contextkey.Equals("debugUx", "default")},
		View{ID: ViewDebugWatch, Name: "Watch", Container: ViewContainerDebug, Order: 20, Weight: 10, When: contextkey.Equals("debugUx", "default")},
		View{ID: ViewDebugCallStack, Name: "Call Stack", Container: ViewContainerDebug, Order: 30, Weight: 30, When: contextkey.Equals("debugUx", "default")},
		View{ID: ViewDebugLoadedScripts, Name: "Loaded Scripts", Container: ViewContainerDebug, Order: 35, Weight: 5, Collapsed: true,
			When: contextkey.And(contextkey.Has("loadedScriptsSupported"), contextkey.Equals("debugUx", "default"))},
		View{ID: ViewDebugBreakpoints, Name: "Breakpoints", Container: ViewContainerDebug, Order: 40, Weight: 20,
			When: contextkey.Or(contextkey.Has("breakpointsExist"), contextkey.Equals("debugUx", "default"), contextkey.Has("hasDebugged"))},
	)
	r.MustRegisterContainer(ViewContainer{ID: ViewContainerMarkers, Title: "Problems", Icon: codiconWarning, Order: 0, Location: ViewLocationPanel})
	r.MustRegisterViews(View{ID: ViewMarkers, Name: "Problems", Container: ViewContainerMarkers})
	r.MustRegisterContainer(ViewContainer{ID: ViewContainerOutput, Title: "Output", Icon: codiconOutput, Order: 1, Location: ViewLocationPanel})
	r.MustRegisterViews(View{ID: ViewOutput, Name: "Output", Container: ViewContainerOutput})
	// the Debug Console (order 2, after the Output): its tab shows while a debugger is available
	r.MustRegisterContainer(ViewContainer{ID: ViewContainerRepl, Title: "Debug Console", Icon: codiconDebugConsole, Order: 2, Location: ViewLocationPanel, HideIfEmpty: true})
	r.MustRegisterViews(View{ID: ViewRepl, Name: "Debug Console", Container: ViewContainerRepl, When: contextkey.Has("debuggersAvailable")})
	return r
}
