// Package todo is the extension that lists the TODO and FIXME comments of the folder of the workspace in a view of the side
// bar (SPEC §10.15 "As built (step 11.5)"). It reads the files the file index lists, scans them in a goroutine of its own,
// follows the changes the watcher reports (emetos.files.changed), and opens the file at the comment when a row is clicked.
package todo

import (
	"context"
	"encoding/json"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
)

// The ids of the extension.
const (
	// ID is the id of the extension.
	ID = "emetos.todo"
	// ContainerID, ViewID, RefreshCommand and CollapseCommand are its container, its view and its commands.
	ContainerID     = "emetos.todo.container"
	ViewID          = "emetos.todo.list"
	RefreshCommand  = "emetos.todo.refresh"
	CollapseCommand = "emetos.todo.collapseAll"
	// TagsSetting lists the words that mark a comment.
	TagsSetting = "emetos.todo.tags"
)

// New is the Factory of the extension.
func New() extension.Extension { return &todo{} }

// Manifest implements extension.Extension. The icon of the container is declared, so it is in the activity bar before the
// extension runs; showing the view activates it.
func (*todo) Manifest() extension.Manifest {
	return extension.Manifest{
		ID: ID, Name: "TODOs", Version: "0.1.0",
		Description: "Lists the TODO and FIXME comments of the folder and opens them.",
		Contributes: extension.Contributes{
			Commands: []extension.CommandSpec{
				{ID: RefreshCommand, Title: "Refresh", Category: "TODOs", Description: "Reads the files of the folder again."},
				{ID: CollapseCommand, Title: "Collapse All", Category: "TODOs"},
			},
			ViewContainers: []extension.ViewContainerSpec{{ID: ContainerID, Title: "TODOs", Icon: "checklist", Order: 50}},
			Views:          []extension.ViewSpec{{ID: ViewID, Name: "TODOs", Container: ContainerID}},
			ViewActions: []extension.ViewActionSpec{
				{View: ViewID, Command: RefreshCommand, Icon: "refresh", Tooltip: "Refresh", Order: 1},
				{View: ViewID, Command: CollapseCommand, Icon: "collapse-all", Tooltip: "Collapse All", Order: 2},
			},
		},
	}
}

// skipExtensions are the files that are not text; they are not read.
var skipExtensions = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".ico": true, ".bmp": true, ".pdf": true,
	".zip": true, ".gz": true, ".tgz": true, ".xz": true, ".bz2": true, ".7z": true, ".jar": true, ".class": true, ".so": true,
	".dll": true, ".exe": true, ".o": true, ".a": true, ".wasm": true, ".ttf": true, ".otf": true, ".woff": true, ".woff2": true,
	".mp3": true, ".mp4": true, ".mov": true, ".ogg": true, ".wav": true, ".sum": true, ".lock": true, ".svg": true,
}

// maxItems is the most comments the view keeps.
const maxItems = 5000

// batch is how many files the worker reads before it hands the results over.
const batch = 64

// todo is the extension of one scope. Everything but the worker's reading is used on the UI thread.
type todo struct {
	api    extension.API
	ctx    context.Context
	folder string

	tagsMu  sync.Mutex // guards tags, which the worker reads
	tags    []string
	byFile  map[string][]Item
	scanned map[string]bool
	// inflight is how many files wait for the worker or for their results to be applied.
	inflight int
	queued   map[string]bool
	complete bool
	total    int

	model   *ui.TreeModel
	ctl     *ui.TreeController
	handle  extension.ViewHandle
	status  extension.StatusHandle
	seen    map[string]bool
	pending []string // the expanded nodes the last session saved, applied when the first comments arrive
	dirty   bool

	// jobs are the batches of paths the worker has not taken yet, and wake tells it to look; the UI thread never waits for
	// the worker.
	jobMu sync.Mutex
	jobs  [][]string
	wake  chan struct{}
	// done is closed when the worker ends.
	done chan struct{}
}

// Activate implements extension.Extension.
func (t *todo) Activate(ctx context.Context, api extension.API) error {
	t.api, t.ctx, t.folder = api, ctx, api.Scope().Folder
	t.byFile, t.scanned, t.queued, t.seen = map[string][]Item{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	t.model = ui.NewTreeModel()
	t.ctl = ui.NewTreeController()
	t.wake = make(chan struct{}, 1)
	t.done = make(chan struct{})
	if err := api.Settings().Register(extension.SettingSpec{
		Key: TagsSetting, Type: "array", Default: []string{"TODO", "FIXME"},
		Description: "The words that mark a comment the TODOs view lists.",
	}); err != nil {
		return err
	}
	t.readTags()
	api.Settings().OnChange(func() {
		before := t.currentTags()
		t.readTags()
		if !slices.Equal(before, t.currentTags()) {
			t.refresh()
		}
	})
	status, err := api.StatusBar().Add(extension.StatusItem{
		ID: "count", Name: "TODOs", Text: "$(checklist) 0", Tooltip: "TODO and FIXME comments", Alignment: extension.StatusLeft, Priority: -10,
		OnClick: func() {
			if t.handle != nil {
				t.handle.Reveal(true)
			}
		},
	})
	if err != nil {
		return err
	}
	t.status = status
	t.handle, err = api.Views().Provide(ViewID, extension.ViewContent{Build: t.build, SaveState: t.saveState, RestoreState: t.restoreState})
	if err != nil {
		return err
	}
	if err := api.Commands().Handle(RefreshCommand, func(context.Context, any) error { t.refresh(); return nil }); err != nil {
		return err
	}
	if err := api.Commands().Handle(CollapseCommand, func(context.Context, any) error {
		t.model.Perform(ui.TreeActionCollapseAll)
		t.handle.Refresh()
		return nil
	}); err != nil {
		return err
	}
	if _, err := api.Events().Subscribe(extension.TopicFilesChanged, t.filesChanged); err != nil {
		return err
	}
	go t.worker()
	t.refresh()
	return nil
}

// readTags reads the setting.
func (t *todo) readTags() {
	tags, err := extension.SettingValue[[]string](t.api.Settings(), TagsSetting)
	if err != nil {
		t.api.Log().Warn("the setting of the tags cannot be read", "err", err)
		tags = []string{"TODO", "FIXME"}
	}
	t.tagsMu.Lock()
	t.tags = slices.DeleteFunc(slices.Clone(tags), func(s string) bool { return strings.TrimSpace(s) == "" })
	t.tagsMu.Unlock()
}

// refresh reads every file of the folder again.
func (t *todo) refresh() {
	t.scanned = map[string]bool{}
	t.reconcile()
}

// reconcile compares the list of files with what was scanned: the files that are gone lose their comments and the new ones
// are queued.
func (t *todo) reconcile() {
	if t.folder == "" {
		t.rebuild()
		return
	}
	list, err := t.api.Workspace().Files()
	if err != nil {
		t.api.Log().Warn("the files of the folder cannot be listed", "err", err)
		return
	}
	t.complete = list.Complete
	present := make(map[string]bool, len(list.Files))
	var fresh []string
	for _, f := range list.Files {
		present[f] = true
		if !t.scanned[f] && !t.queued[f] && !skipExtensions[strings.ToLower(path.Ext(f))] {
			fresh = append(fresh, f)
		}
	}
	for f := range t.byFile {
		if !present[f] {
			delete(t.byFile, f)
		}
	}
	for f := range t.scanned {
		if !present[f] {
			delete(t.scanned, f)
		}
	}
	t.queue(fresh)
	t.rebuild()
}

// queue hands paths to the worker.
func (t *todo) queue(paths []string) {
	for len(paths) > 0 {
		n := min(batch, len(paths))
		chunk := slices.Clone(paths[:n])
		paths = paths[n:]
		for _, p := range chunk {
			t.queued[p] = true
		}
		t.inflight += len(chunk)
		t.jobMu.Lock()
		t.jobs = append(t.jobs, chunk)
		t.jobMu.Unlock()
		select {
		case t.wake <- struct{}{}:
		default:
		}
	}
}

// nextJob takes the oldest batch the worker has not taken yet.
func (t *todo) nextJob() ([]string, bool) {
	t.jobMu.Lock()
	defer t.jobMu.Unlock()
	if len(t.jobs) == 0 {
		return nil, false
	}
	job := t.jobs[0]
	t.jobs = t.jobs[1:]
	return job, true
}

// result is what the worker found in one file.
type result struct {
	path  string
	items []Item
}

// worker reads the files that are queued and hands the comments over to the UI thread, a batch at a time. It ends with the
// activation.
func (t *todo) worker() {
	defer close(t.done)
	for {
		paths, ok := t.nextJob()
		if !ok {
			select {
			case <-t.ctx.Done():
				return
			case <-t.wake:
				continue
			}
		}
		tags := t.currentTags()
		results := make([]result, 0, len(paths))
		for _, p := range paths {
			if t.ctx.Err() != nil {
				return
			}
			text, err := t.api.Workspace().ReadFile(t.ctx, p)
			if err != nil {
				results = append(results, result{path: p}) // a file that is gone, binary or too large has no comments
				continue
			}
			results = append(results, result{path: p, items: scanText(text, tags)})
		}
		t.api.Post(func() { t.apply(results) })
	}
}

// currentTags is the tags as the worker should use them.
func (t *todo) currentTags() []string {
	t.tagsMu.Lock()
	defer t.tagsMu.Unlock()
	return t.tags
}

// apply takes the results of the worker on the UI thread.
func (t *todo) apply(results []result) {
	for _, r := range results {
		delete(t.queued, r.path)
		t.inflight = max(t.inflight-1, 0)
		t.scanned[r.path] = true
		if len(r.items) > 0 {
			t.byFile[r.path] = r.items
		} else {
			delete(t.byFile, r.path)
		}
	}
	t.rebuild()
}

// filesChanged follows the changes of the files of the folder.
func (t *todo) filesChanged(e extension.Event) {
	v, err := extension.EventValue[extension.FilesChanged](e)
	if err != nil {
		t.api.Log().Warn("a change of files cannot be read", "err", err)
		return
	}
	relist := false
	var again []string
	forget := func(p string) {
		for f := range t.byFile {
			if f == p || strings.HasPrefix(f, p+"/") {
				delete(t.byFile, f)
			}
		}
		for f := range t.scanned {
			if f == p || strings.HasPrefix(f, p+"/") {
				delete(t.scanned, f)
			}
		}
	}
	for _, c := range v.Changes {
		switch c.Kind {
		case extension.FileCreated, extension.FileChanged:
			if !skipExtensions[strings.ToLower(path.Ext(c.Path))] {
				delete(t.scanned, c.Path)
				again = append(again, c.Path)
			}
		case extension.FileDeleted:
			forget(c.Path)
		case extension.FileRenamed:
			forget(c.OldPath)
			relist = true
		case extension.FileRescan:
			if c.Path == "." {
				t.scanned = map[string]bool{}
			} else {
				forget(c.Path)
			}
			relist = true
		case extension.FileListed:
			relist = true
		}
	}
	if len(again) > 0 && !relist {
		t.queue(again)
	}
	if relist {
		t.reconcile()
		return
	}
	t.rebuild()
}

// rebuild makes the nodes of the tree again, once for any number of calls before the next turn of the UI thread.
func (t *todo) rebuild() {
	if t.dirty {
		return
	}
	t.dirty = true
	t.api.Post(func() {
		t.dirty = false
		t.setItems()
	})
}

// fileNode and itemNode are the data of the nodes.
type fileNode struct {
	path  string
	count int
}

type itemNode struct {
	path string
	item Item
}

// setItems makes the tree from the comments and the status bar item from their number.
func (t *todo) setItems() {
	files := make([]string, 0, len(t.byFile))
	for f := range t.byFile {
		files = append(files, f)
	}
	sort.Strings(files)
	t.total = 0
	var roots []ui.TreeItem
	for _, f := range files {
		items := t.byFile[f]
		if t.total >= maxItems {
			break
		}
		node := ui.TreeItem{Key: f, Label: f, Data: fileNode{path: f, count: len(items)}}
		for i, it := range items {
			if t.total >= maxItems {
				break
			}
			t.total++
			node.Children = append(node.Children, ui.TreeItem{
				Key: f + ":" + strconv.Itoa(it.Line) + ":" + strconv.Itoa(i), Label: it.Tag + " " + it.Message, Data: itemNode{path: f, item: it},
			})
		}
		roots = append(roots, node)
	}
	expanded := t.model.ExpandedKeys()
	if err := t.model.SetItems(roots); err != nil {
		t.api.Log().Warn("the tree cannot be made", "err", err)
		return
	}
	// a file is open the first time it is seen, unless the last session saved which ones were
	switch {
	case t.pending != nil && len(roots) > 0:
		t.model.SetExpandedKeys(t.pending)
		for _, r := range roots {
			t.seen[r.Key] = true
		}
		t.pending = nil
	default:
		for _, r := range roots {
			if !t.seen[r.Key] {
				t.seen[r.Key] = true
				expanded = append(expanded, r.Key)
			}
		}
		t.model.SetExpandedKeys(expanded)
	}
	text := "$(checklist) " + strconv.Itoa(t.total)
	tip := strconv.Itoa(t.total) + " TODO and FIXME comments"
	if t.inflight > 0 {
		tip += " (scanning)"
	}
	t.status.Update(extension.StatusItem{ID: "count", Name: "TODOs", Text: text, Tooltip: tip, Alignment: extension.StatusLeft, Priority: -10,
		OnClick: func() { t.handle.Reveal(true) }})
	t.handle.Refresh()
}

// state is what the view keeps across a restart.
type state struct {
	Expanded []string `json:"expanded"`
}

func (t *todo) saveState() (any, error) {
	return state{Expanded: t.model.ExpandedKeys()}, nil
}

func (t *todo) restoreState(raw json.RawMessage) error {
	var s state
	if err := json.Unmarshal(raw, &s); err != nil {
		return err
	}
	if s.Expanded != nil {
		t.pending = s.Expanded
		if len(t.model.Rows()) > 0 {
			t.model.SetExpandedKeys(s.Expanded)
			t.pending = nil
		}
	}
	return nil
}

// build makes the widget of the view.
func (t *todo) build(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	message := func(text string) widgets.Widget {
		return widgets.NewPadding(painting.NewEdgeInsetsAll(16), widgets.NewAlign(
			widgets.NewText(text, widgets.TextOpts{Style: ui.TextStyle(bc, 13, colors.DescriptionForeground)}),
			widgets.AlignOpts{Alignment: painting.AlignmentTopLeft}))
	}
	switch {
	case t.folder == "":
		return message("Open a folder to list its TODO and FIXME comments.")
	case t.total == 0 && (t.inflight > 0 || !t.complete):
		return message("Looking for TODO and FIXME comments...")
	case t.total == 0:
		return message("No TODO or FIXME comments in the folder.")
	}
	return ui.NewTree(t.model, ui.TreeOpts{Controller: t.ctl, Render: t.render, OnSelect: t.onSelect})
}

// render builds the contents of a row.
func (t *todo) render(bc widgets.BuildContext, node ui.TreeNode, st ui.RowState) widgets.Widget {
	colors := ui.Theme(bc)
	text := func(s string, color gui.Color) widgets.Widget {
		return widgets.NewText(s, widgets.TextOpts{Style: ui.TextStyle(bc, 13, color), MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})
	}
	switch d := node.Data.(type) {
	case fileNode:
		dir, name := path.Split(d.path)
		row := []widgets.Widget{text(name, st.Foreground)}
		if dir != "" {
			row = append(row, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(6.0)}),
				widgets.NewFlexible(text(strings.TrimSuffix(dir, "/"), colors.DescriptionForeground)))
		}
		row = append(row, widgets.NewSpacer(), ui.NewBadge(d.count), widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(8.0)}))
		return widgets.NewRow(row, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
	case itemNode:
		tagColor := colors.TextLinkForeground
		if d.item.Tag == "FIXME" {
			tagColor = colors.ErrorForeground
		}
		return widgets.NewRow([]widgets.Widget{
			text(d.item.Tag, tagColor), widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(6.0)}),
			widgets.NewFlexible(text(d.item.Message, st.Foreground)),
			widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(6.0)}),
			text(":"+strconv.Itoa(d.item.Line), colors.DescriptionForeground), widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(8.0)}),
		}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
	}
	return text("", st.Foreground)
}

// onSelect opens the file of a comment at the tag.
func (t *todo) onSelect(node ui.TreeNode) {
	d, ok := node.Data.(itemNode)
	if !ok {
		return
	}
	uri := "file://" + filepath.ToSlash(filepath.Join(t.folder, filepath.FromSlash(d.path)))
	if err := t.api.Documents().Show(uri, d.item.Line, d.item.Column, true); err != nil {
		t.api.Notifications().Error("Cannot open " + d.path + ": " + err.Error())
	}
}
