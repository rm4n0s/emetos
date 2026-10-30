package scm

import "slices"

// The arguments of the commands that the menus of the Source Control view run. VS Code gives them as separate arguments (the
// provider for the menus of a repository; the selected groups, resources and the resources below the selected folders for the
// menus of the groups, the resources and the folders; RepositoryPaneActionRunner.runAction); the extension host turns them into
// the source control, the group and the resource states of the extension. Emetos gives the command one list of objects, which
// extension.SCMArgs reads: {"sourceControl": handle, "group": id, "uri": uri, "contextValue": value}.
//
// Ported from VS Code: src/vs/workbench/contrib/scm/browser/scmViewPane.ts (RepositoryPaneActionRunner) and
// src/vs/workbench/api/common/extHostSCM.ts (the argument processor)

// The members of an argument; extension.SCMArgSourceControl and its siblings have the same values.
const (
	argSourceControl = "sourceControl"
	argGroup         = "group"
	argURI           = "uri"
	argContextValue  = "contextValue"
)

// SourceArg is the argument that stands for a source control.
func SourceArg(s *Source) map[string]any { return map[string]any{argSourceControl: s.key} }

// GroupArg is the argument that stands for a group.
func GroupArg(g *Group) map[string]any {
	a := map[string]any{argSourceControl: g.src.key, argGroup: g.id}
	if v := g.ContextValue(); v != "" {
		a[argContextValue] = v
	}
	return a
}

// ResourceArg is the argument that stands for a resource.
func ResourceArg(r *Resource) map[string]any {
	a := map[string]any{argSourceControl: r.Group.src.key, argGroup: r.Group.id, argURI: r.URI}
	if r.ContextValue != "" {
		a[argContextValue] = r.ContextValue
	}
	return a
}

// SourceMenuArgs are the arguments of a command run by a menu of a repository (SCMTitle, SCMSourceControlInline,
// SCMSourceControl): the source control.
func SourceMenuArgs(s *Source) []any { return []any{SourceArg(s)} }

// Row is a row of the Changes view in a selection or under the pointer: a group, a resource, or a folder of the tree view.
// Exactly one field is set.
type Row struct {
	// Group is a group header.
	Group *Group
	// Resource is a resource row.
	Resource *Resource
	// Folder is a folder row of the tree view; Group is the group it is in.
	Folder *Node
	// FolderGroup is the group of Folder.
	FolderGroup *Group
}

// SelectionMenuArgs are the arguments of a command run by a menu of a group, a resource or a folder, for the row that the
// pointer or the menu key is on and the rows that are selected: when the row is one of the selected rows the command works on
// all of them, else on the row alone. A group is its argument, a resource is its argument and a folder is the arguments of the
// resources below it, in the order of the tree (VS Code's runAction: selection or [context], each node collected, flattened).
func SelectionMenuArgs(on Row, selection []Row) []any {
	rows := []Row{on}
	if slices.Contains(selection, on) {
		rows = selection
	}
	var out []any
	for _, r := range rows {
		switch {
		case r.Folder != nil:
			for _, res := range r.Folder.Resources() {
				out = append(out, ResourceArg(res))
			}
		case r.Resource != nil:
			out = append(out, ResourceArg(r.Resource))
		case r.Group != nil:
			out = append(out, GroupArg(r.Group))
		}
	}
	return out
}
