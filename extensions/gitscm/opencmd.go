package gitscm

import (
	"context"
	"path/filepath"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/git"
)

// The commands that open the changes of files (SPEC §10.30 "Diffs of changes"): Open Changes, Open File, Open File (HEAD), Open All
// Changes, the click on a row of the Source Control view, Close All Diff Editors and Close All Unmodified Editors, Reveal in
// Explorer View and Open Containing Folder.
//
// Ported from VS Code: extensions/git/src/commands.ts (openChange, openFile, openFile2, openHEADFile, openChanges, openResource,
// closeDiffEditors, closeUnmodifiedEditors, revealInExplorer, revealFileInOS, getSCMResource) and
// extensions/git/src/repository.ts (Resource.open, ResourceCommandResolver.resolveChangeCommand)

// CommandOpenResource is the command of the click on a row of the Source Control view (VS Code's git.openResource): the diff of the change
// or, with git.openDiffOnClick off, the file. The view gives it the resource ({"sourceControl", "group", "uri"}) and how the click
// opens it ("preserveFocus", "pinned").
const CommandOpenResource = "git.openResource"

// openCommand is the command every resource of the view runs on a click. It has no arguments of its own: the view adds the resource.
var openCommand = &extension.Command{ID: CommandOpenResource, Title: "Open"}

// target is a resource found for a command: the repository, the group it is in and the resource.
type target struct {
	repo  *repository
	group string
	res   resource
}

// groupResources are the resources of a group of a repository.
func (r *repository) groupResources(group string) []resource {
	switch group {
	case groupMerge:
		return r.res.merge
	case groupIndex:
		return r.res.index
	case groupWorking:
		return r.res.working
	case groupUntracked:
		return r.res.untracked
	}
	return nil
}

// findResource finds the resource of a path in a group.
func (r *repository) findResource(group, path string) (resource, bool) {
	for _, res := range r.groupResources(group) {
		if res.path == path {
			return res, true
		}
	}
	return resource{}, false
}

// sidesOf resolves the documents of a resource of the repository.
func (r *repository) sidesOf(group string, res resource) sides {
	return resolveSides(r.id, r.root, group, res, r.submodules)
}

// resourceTargets are the resources a menu gave a command (the objects of the list that name a resource).
func (x *gitscm) resourceTargets(args any) []target {
	var out []target
	for _, a := range extension.SCMArgs(args) {
		if t, ok := x.targetOfArg(a); ok {
			out = append(out, t)
		}
	}
	return out
}

// targetOfArg finds the resource an argument names.
func (x *gitscm) targetOfArg(a extension.SCMArg) (target, bool) {
	if !a.IsResource() {
		return target{}, false
	}
	r := x.model.byHandle(a.SourceControl)
	path, ok := uriPath(a.URI)
	if r == nil || !ok {
		return target{}, false
	}
	res, ok := r.findResource(a.Group, path)
	if !ok {
		return target{}, false
	}
	return target{repo: r, group: a.Group, res: res}, true
}

// uriArg reads the URI a command was given: a string, or an object with a "uri" (the arguments of the editor title menu) or a "path"
// (absolute, or relative to the folder of the window tab: what a keybinding or a dev command gives).
func (x *gitscm) uriArg(args any) string {
	switch v := args.(type) {
	case string:
		return v
	case []any:
		// the argument of a file's action in the multi-file diff editor has the shape of SCMArgs: a list of one object
		if len(v) > 0 {
			return x.uriArg(v[0])
		}
	case map[string]any:
		if s, _ := v["uri"].(string); s != "" {
			return s
		}
		if p, _ := v["path"].(string); p != "" {
			if !filepath.IsAbs(p) {
				p = filepath.Join(x.folder, p)
			}
			return fileURI(p)
		}
	}
	return ""
}

// fileOfURI is the path of the file a URI stands for: a file, or a version of a file in git.
func (x *gitscm) fileOfURI(uri string) (string, bool) {
	if p, ok := uriPath(uri); ok {
		return p, true
	}
	if g, ok := parseGitURI(uri); ok {
		for _, r := range x.model.order {
			if r.id == g.RepoID {
				return g.filePath(r.root), true
			}
		}
	}
	return "", false
}

// scmResource finds the resource of a file that has changes, as the commands find the file they work on: the changes in the work tree,
// then the staged ones, then the conflicts. An empty uri is the active text editor.
//
// Ported from VS Code: extensions/git/src/commands.ts (getSCMResource)
func (x *gitscm) scmResource(uri string) (target, bool) {
	if uri == "" {
		if doc, ok := x.api.Documents().Active(); ok {
			uri = doc.URI
		}
	}
	path, ok := x.fileOfURI(uri)
	if !ok {
		return target{}, false
	}
	r := x.model.forPath(path)
	if r == nil {
		return target{}, false
	}
	for _, g := range []string{groupWorking, groupIndex, groupMerge} {
		if res, ok := r.findResource(g, path); ok {
			return target{repo: r, group: g, res: res}, true
		}
	}
	return target{}, false
}

// openOptions say how an editor opens.
type openOptions struct {
	// preview opens it as a preview tab, preserveFocus leaves the keyboard where it is.
	preview, preserveFocus bool
}

// openChange opens the change of a resource: the diff editor, or when the change has only one side that document with the title.
//
// Ported from VS Code: extensions/git/src/repository.ts (ResourceCommandResolver.resolveChangeCommand)
func (x *gitscm) openChange(t target, o openOptions) {
	s := t.repo.sidesOf(t.group, t.res)
	var err error
	switch {
	case s.Left != "":
		err = x.api.Documents().OpenDiff(extension.DiffSpec{Left: s.Left, Right: s.Right, Title: s.Title, Preview: o.preview, PreserveFocus: o.preserveFocus})
	case s.Right != "":
		// with git.mergeEditor on, a file with both modified or both added opens the merge editor; else the file with its markers, which the
		// window offers the merge editor for (VS Code's Open Merge)
		merge := x.cfg.mergeEditor && (t.res.status == git.StatusBothModified || t.res.status == git.StatusBothAdded)
		err = x.api.Documents().OpenDocument(extension.OpenSpec{URI: s.Right, Title: s.Title, Preview: o.preview, PreserveFocus: o.preserveFocus, MergeEditor: merge})
	case t.res.status == git.StatusAddedByUs || t.res.status == git.StatusAddedByThem:
		// VS Code asks to open nothing here; the file with its markers is what there is to see
		err = x.api.Documents().OpenDocument(extension.OpenSpec{URI: fileURI(t.res.path), Preview: o.preview, PreserveFocus: o.preserveFocus})
	}
	if err != nil {
		x.log.Warn("cannot open the changes of a file", "path", t.res.path, "err", err)
		x.api.Notifications().Error("Cannot open the changes of " + filepath.Base(t.res.path) + ": " + err.Error())
	}
}

// openFile opens the file of a resource (never a diff).
func (x *gitscm) openFile(path string, o openOptions, sel *extension.Selection) {
	err := x.api.Documents().OpenDocument(extension.OpenSpec{URI: fileURI(path), Preview: o.preview, PreserveFocus: o.preserveFocus, Selection: sel})
	if err != nil {
		x.log.Warn("cannot open a file", "path", path, "err", err)
		x.api.Notifications().Error("Cannot open " + filepath.Base(path) + ": " + err.Error())
	}
}

// cmdOpenResource is the click on a row: the diff, or with git.openDiffOnClick off the file.
//
// Ported from VS Code: extensions/git/src/repository.ts (ResourceCommandResolver.resolveDefaultCommand)
func (x *gitscm) cmdOpenResource(_ context.Context, args any) error {
	m, ok := args.(map[string]any)
	if !ok {
		return nil
	}
	a := extension.SCMArg{}
	a.SourceControl, _ = m[extension.SCMArgSourceControl].(string)
	a.Group, _ = m[extension.SCMArgGroup].(string)
	a.URI, _ = m[extension.SCMArgURI].(string)
	t, ok := x.targetOfArg(a)
	if !ok {
		return nil
	}
	pinned, _ := m["pinned"].(bool)
	preserve, _ := m["preserveFocus"].(bool)
	o := openOptions{preview: !pinned, preserveFocus: preserve}
	if x.cfg.openDiffOnClick {
		x.openChange(t, o)
	} else {
		x.openFile(t.res.path, o, nil)
	}
	return nil
}

// cmdOpenChange is Open Changes: for the resources of the menu, else the file of the editor or the argument.
func (x *gitscm) cmdOpenChange(_ context.Context, args any) error {
	targets := x.resourceTargets(args)
	if len(targets) == 0 {
		if t, ok := x.scmResource(x.uriArg(args)); ok {
			targets = []target{t}
		}
	}
	for _, t := range targets {
		x.openChange(t, openOptions{preview: true})
	}
	return nil
}

// cmdOpenFile is Open File: the files of the resources of a menu (not the deleted ones), else the file of the argument, else the file of
// the changes of the active editor; it keeps the selection of the editor it comes from when that shows the same file.
func (x *gitscm) cmdOpenFile(_ context.Context, args any) error {
	var paths []string
	fromResource := false
	if targets := x.resourceTargets(args); len(targets) > 0 {
		fromResource = true
		for _, t := range targets {
			if t.res.status != git.StatusDeleted && t.res.status != git.StatusIndexDeleted {
				paths = append(paths, t.res.path)
			}
		}
	} else if uri := x.uriArg(args); uri != "" {
		if p, ok := x.fileOfURI(uri); ok {
			paths = []string{p}
		}
	} else if t, ok := x.scmResource(""); ok {
		paths = []string{t.res.path}
	} else if doc, ok := x.api.Documents().Active(); ok {
		if p, ok := x.fileOfURI(doc.URI); ok {
			paths = []string{p}
		}
	}
	var sel *extension.Selection
	if doc, ok := x.api.Documents().Active(); ok && len(doc.Selections) > 0 {
		if p, ok := x.fileOfURI(doc.URI); ok && len(paths) == 1 && p == paths[0] {
			s := doc.Selections[0]
			sel = &s
		}
	}
	for _, p := range paths {
		x.openFile(p, openOptions{preserveFocus: fromResource}, sel)
	}
	return nil
}

// cmdOpenHEADFile is Open File (HEAD): the left side of the change, read only, titled "<name> (HEAD)".
func (x *gitscm) cmdOpenHEADFile(_ context.Context, args any) error {
	var t target
	var ok bool
	preview := true
	if ts := x.resourceTargets(args); len(ts) > 0 {
		t, ok, preview = ts[0], true, false
	} else {
		t, ok = x.scmResource(x.uriArg(args))
	}
	if !ok {
		return nil
	}
	base := filepath.Base(t.res.path)
	s := t.repo.sidesOf(t.group, t.res)
	if s.Left == "" {
		x.api.Notifications().Warn("HEAD version of \"" + base + "\" is not available.")
		return nil
	}
	err := x.api.Documents().OpenDocument(extension.OpenSpec{URI: s.Left, Title: base + " (HEAD)", Preview: preview})
	if err != nil {
		x.log.Warn("cannot open the HEAD version of a file", "path", t.res.path, "err", err)
		x.api.Notifications().Error("Cannot open " + base + " (HEAD): " + err.Error())
	}
	return nil
}

// cmdOpenAllChanges opens the changes of a group in the multi-file diff editor (VS Code opens the files; the SPEC of step 24.17 opens the
// editor in place of many diff editors, so there is nothing to ask about): the group of the argument, and for a repository the
// changes of the work tree and the untracked files.
func (x *gitscm) cmdOpenAllChanges(_ context.Context, args any) error {
	type set struct {
		r     *repository
		group string
	}
	var sets []set
	for _, a := range extension.SCMArgs(args) {
		switch {
		case a.IsGroup():
			if r := x.model.byHandle(a.SourceControl); r != nil {
				sets = append(sets, set{r, a.Group})
			}
		case a.IsSourceControl():
			if r := x.model.byHandle(a.SourceControl); r != nil {
				sets = append(sets, set{r, groupAll})
			}
		}
	}
	if len(sets) == 0 {
		x.target(args, true, func(r *repository) { sets = append(sets, set{r, groupAll}) })
	}
	for _, s := range sets {
		x.openGroupChanges(s.r, s.group)
	}
	return nil
}

// groupLabels are the titles of the groups.
var groupLabels = map[string]string{groupMerge: "Merge Changes", groupIndex: "Staged Changes", groupWorking: "Changes", groupUntracked: "Untracked Changes", groupAll: "Changes"}

// multiTitle is the title of the editor of a set of a repository: "<source control>: <group>", with the name of the repository for the
// source control when there are several, so that two repositories do not have the same title.
func (x *gitscm) multiTitle(r *repository, label string) string {
	who := "Git"
	if len(x.model.openRepos()) > 1 {
		who = r.name
	}
	return who + ": " + label
}

// openGroupChanges opens the multi-file diff editor of a group of a repository; a group with no change opens nothing.
func (x *gitscm) openGroupChanges(r *repository, group string) {
	if list, _ := groupOf(r.res, group); len(list) == 0 {
		return
	}
	spec := extension.MultiDiffSpec{Title: x.multiTitle(r, groupLabels[group]), Kind: MultiDiffKind, Key: groupKey(r.id, group)}
	if err := x.api.Documents().OpenMultiDiff(spec); err != nil {
		x.log.Warn("cannot open the changes of a group", "group", group, "err", err)
		x.api.Notifications().Error("Cannot open the changes: " + err.Error())
	}
}

// cmdRevealInExplorer is Reveal in Explorer View, for the first resource of the menu.
func (x *gitscm) cmdRevealInExplorer(_ context.Context, args any) error { return x.reveal(args, false) }

// cmdRevealFileInOS is Open Containing Folder.
func (x *gitscm) cmdRevealFileInOS(_ context.Context, args any) error { return x.reveal(args, true) }

// reveal shows the file of the first resource of a menu in the Explorer or its folder in the file manager.
func (x *gitscm) reveal(args any, inOS bool) error {
	targets := x.resourceTargets(args)
	if len(targets) == 0 {
		return nil
	}
	return x.api.Workspace().RevealFile(fileURI(targets[0].res.path), inOS)
}

// closeDiffEditors closes the diff editors of the changes in the index (indexPaths) and in the work tree (workPaths); nil means every
// file. It acts only with git.closeDiffOnOperation, unless force is set (Close All Diff Editors). A diff is of the index when its
// modified side is a version in git, and of the work tree when its modified side is a file and its original side a version in git.
//
// Ported from VS Code: extensions/git/src/repository.ts (closeDiffEditors)
func (r *repository) closeDiffEditors(indexPaths, workPaths []string, force bool) {
	x := r.m.x
	if !x.cfg.closeDiffOnOperation && !force {
		return
	}
	var tabs []extension.DocumentTab
	for _, tab := range x.api.Documents().Tabs() {
		if tab.Kind != "diff" || !r.closesWith(tab, indexPaths, workPaths) {
			continue
		}
		tabs = append(tabs, tab)
	}
	if len(tabs) > 0 {
		x.api.Documents().CloseTabs(tabs...)
	}
}

// closesWith tells whether a diff tab is one that closeDiffEditors closes.
func (r *repository) closesWith(tab extension.DocumentTab, indexPaths, workPaths []string) bool {
	if g, ok := parseGitURI(tab.Modified); ok && g.RepoID == r.id {
		return indexPaths == nil || containsPath(indexPaths, g.filePath(r.root))
	}
	if p, ok := uriPath(tab.Modified); ok {
		if g, ok := parseGitURI(tab.Original); ok && g.RepoID == r.id {
			return workPaths == nil || containsPath(workPaths, p)
		}
	}
	return false
}

// containsPath tells whether a list has a path.
func containsPath(list []string, path string) bool {
	for _, p := range list {
		if p == path {
			return true
		}
	}
	return false
}

// cmdCloseAllDiffEditors closes the diff editors of a repository whatever git.closeDiffOnOperation says.
func (x *gitscm) cmdCloseAllDiffEditors(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) { r.closeDiffEditors(nil, nil, true) })
	return nil
}

// cmdCloseAllUnmodifiedEditors closes the text editors that are not dirty and show no file with changes.
func (x *gitscm) cmdCloseAllUnmodifiedEditors(context.Context, any) error {
	changed := map[string]bool{}
	for _, r := range x.model.order {
		for _, g := range []string{groupIndex, groupWorking, groupUntracked, groupMerge} {
			for _, res := range r.groupResources(g) {
				changed[res.path] = true
			}
		}
	}
	var tabs []extension.DocumentTab
	for _, tab := range x.api.Documents().Tabs() {
		if tab.Kind != "text" || tab.Dirty {
			continue
		}
		if p, ok := uriPath(tab.URI); ok && !changed[p] {
			tabs = append(tabs, tab)
		}
	}
	if len(tabs) > 0 {
		x.api.Documents().CloseTabs(tabs...)
	}
	return nil
}

// updateActiveResource sets the context keys that say whether the file of the active editor has staged or unstaged changes.
//
// Ported from VS Code: extensions/git/src/model.ts (onDidChangeActiveTextEditor)
func (x *gitscm) updateActiveResource() {
	if x.stopped.Load() {
		return
	}
	staged, unstaged, conflicts := false, false, false
	if doc, ok := x.api.Documents().Active(); ok {
		if p, ok := uriPath(doc.URI); ok {
			if r := x.model.forPath(p); r != nil {
				_, staged = r.findResource(groupIndex, p)
				_, unstaged = r.findResource(groupWorking, p)
				_, conflicts = r.findResource(groupMerge, p)
			}
		}
	}
	for key, v := range map[string]bool{
		"git.activeResourceHasStagedChanges": staged, "git.activeResourceHasUnstagedChanges": unstaged, "git.activeResourceHasMergeConflicts": conflicts,
	} {
		if err := x.api.Context().Set(key, v); err != nil {
			x.log.Debug("cannot set a context key", "key", key, "err", err)
		}
	}
}

// onActiveEditor hears that another editor became the active one.
func (x *gitscm) onActiveEditor(extension.Event) { x.updateActiveResource() }
