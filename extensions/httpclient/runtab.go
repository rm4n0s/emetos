package httpclient

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/httpclient/rundata"
	"github.com/rm4n0s/emetos/extensions/httpclient/store"
	"github.com/rm4n0s/emetos/extensions/internal/pageui"
)

// RunnerKindID is the kind of editor tab of the collection runner (SPEC §10.33 "The collection runner").
const RunnerKindID = ID + ".runner"

// The limits of the boxes of the runner.
const (
	maxIterations = 10000
	maxDelayMs    = 3600000
)

// runTabState is what a runner tab keeps in the state of the window: the collection or folder and the choices made in the tab. The
// results are not kept (they are gone when the tab closes).
type runTabState struct {
	Path string `json:"path"`
	// Env is the key of the environment of the run; nil: the active environment when the tab opened, "" none.
	Env        *string `json:"env,omitempty"`
	Iterations int     `json:"iterations,omitempty"`
	DelayMs    int     `json:"delayMs,omitempty"`
	Stop       bool    `json:"stop,omitempty"`
	Data       string  `json:"data,omitempty"`
	// Skip are the requests that are not run, by their path inside the group, with slashes.
	Skip []string `json:"skip,omitempty"`
}

// runRequest is a request of the group as the list of the tab shows it.
type runRequest struct {
	node *store.Node
	// rel is the path of the request inside the group, "users/Create user".
	rel string
}

// runTab is the collection runner: an editor tab with the choices of a run on the left and its results on the right. It is used on the
// UI thread.
type runTab struct {
	e   *Extension
	tab extension.EditorTab

	// path is the collection or folder.
	path   string
	envKey string
	// iterations and delay are the boxes of the number of passes and the milliseconds between two requests, data the one of the data file.
	iterations, delay, data *ui.InputBoxController
	// iterShown shows the number of rows of the data file in place of the iterations.
	iterShown *ui.InputBoxController
	stop      bool
	// skip holds the paths of the requests that are unchecked.
	skip map[string]bool

	// table is the data file as it was read, for the file named dataFor; dataErr is why it could not be.
	table   *rundata.Table
	dataFor string
	dataErr string

	// running is true while a run goes on and cancel ends it. results are those of the last run, end how it ended (0: it has not).
	running bool
	cancel  context.CancelFunc
	results []runResult
	end     runEnd
	// total is the number of iterations of the last run, filter the iteration the table shows (0: all), open the results that are expanded.
	total, filter int
	open          map[int]bool
	// name and envName are those of the last run, for the report.
	name, envName string

	gone, closed bool
	note         pageui.Note
	listCtl      widgets.ScrollController
	leftCtl      widgets.ScrollController
}

// createRunnerTab makes the content of a runner tab, for a new one and for one that comes back after a restart.
func (e *Extension) createRunnerTab(tab extension.EditorTab, raw json.RawMessage) (extension.EditorTabContent, error) {
	var st runTabState
	if len(raw) == 0 {
		return extension.EditorTabContent{}, errors.New("HttpRunnerTabStateMissing", "a runner tab is opened with a collection or a folder")
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		return extension.EditorTabContent{}, errors.NewErr("HttpRunnerTabStateInvalid", err)
	}
	if st.Path == "" {
		return extension.EditorTabContent{}, errors.New("HttpRunnerTabStateMissing", "a runner tab is opened with a collection or a folder")
	}
	iter := st.Iterations
	if iter < 1 {
		iter = 1
	}
	t := &runTab{
		e: e, tab: tab, path: st.Path, stop: st.Stop, skip: map[string]bool{}, open: map[int]bool{}, listCtl: widgets.NewScrollController(), leftCtl: widgets.NewScrollController(),
		iterations: ui.NewInputBoxController(strconv.Itoa(iter)), delay: ui.NewInputBoxController(strconv.Itoa(st.DelayMs)),
		data: ui.NewInputBoxController(st.Data), iterShown: ui.NewInputBoxController(""),
	}
	if st.Env != nil {
		t.envKey = *st.Env
	} else {
		t.envKey = e.activeKey()
	}
	for _, s := range st.Skip {
		t.skip[filepath.Join(st.Path, filepath.FromSlash(s))] = true
	}
	e.runTabs = append(e.runTabs, t)
	tab.SetTitle("Run " + filepath.Base(st.Path))
	tab.SetHint(st.Path)
	t.storeChanged()
	if st.Data != "" {
		t.loadData()
	}
	return extension.EditorTabContent{Build: t.build, SaveState: t.saveState, OnClose: t.onClose}, nil
}

// runGroup shows the runner of a collection or folder, the one that is open already when there is one. The arguments of the command
// (nil for none) fill in the tab: see runTab.apply.
func (e *Extension) runGroup(n *store.Node, args any) {
	for _, t := range e.runTabs {
		if t.path == n.Path {
			t.tab.Reveal(true)
			t.apply(args)
			return
		}
	}
	tab, err := e.api.Editors().Open(RunnerKindID, runTabState{Path: n.Path}, extension.EditorOpenOptions{})
	if err != nil {
		e.fail("open the runner", err)
		return
	}
	tab.Reveal(true)
	if len(e.runTabs) > 0 && e.runTabs[len(e.runTabs)-1].path == n.Path {
		e.runTabs[len(e.runTabs)-1].apply(args)
	}
}

// apply fills in the tab from the arguments of the command `runCollection`, an object with any of "environment" (the key of an
// environment, "" for none), "iterations", "delayMs", "stop", "data" (a data file) and "start" (true runs at once). This is what a
// keybinding or a task gives to run a collection without the tab being touched.
func (t *runTab) apply(args any) {
	m := argMap(args)
	if v, ok := m["environment"].(string); ok {
		t.envKey = v
	}
	for key, box := range map[string]*ui.InputBoxController{"iterations": t.iterations, "delayMs": t.delay} {
		if v, ok := m[key].(float64); ok {
			box.SetValue(strconv.Itoa(int(v)))
		}
	}
	if v, ok := m["stop"].(bool); ok {
		t.stop = v
	}
	if v, ok := m["data"].(string); ok {
		t.data.SetValue(v)
		t.dataEdited()
		t.loadData()
	}
	t.tab.Refresh()
	if v, _ := m["start"].(bool); v {
		t.start()
	}
}

// argMap reads the arguments of a command that are an object.
func argMap(args any) map[string]any {
	switch a := args.(type) {
	case map[string]any:
		return a
	case json.RawMessage:
		var m map[string]any
		if json.Unmarshal(a, &m) == nil {
			return m
		}
	}
	return nil
}

// saveState is the state the window keeps for the tab.
func (t *runTab) saveState() (any, error) {
	st := runTabState{Path: t.path, Stop: t.stop, Data: strings.TrimSpace(t.data.Value())}
	env := t.envKey
	st.Env = &env
	st.Iterations, _ = strconv.Atoi(strings.TrimSpace(t.iterations.Value()))
	st.DelayMs, _ = strconv.Atoi(strings.TrimSpace(t.delay.Value()))
	for p := range t.skip {
		if rel, err := filepath.Rel(t.path, p); err == nil {
			st.Skip = append(st.Skip, filepath.ToSlash(rel))
		}
	}
	return st, nil
}

// onClose ends a run that is going and lets go of everything the tab holds.
func (t *runTab) onClose() {
	t.closed = true
	if t.cancel != nil {
		t.cancel()
	}
	e := t.e
	for i, x := range e.runTabs {
		if x == t {
			e.runTabs = append(e.runTabs[:i], e.runTabs[i+1:]...)
			break
		}
	}
	t.iterations.Dispose()
	t.delay.Dispose()
	t.data.Dispose()
	t.iterShown.Dispose()
}

// node is the collection or folder of the tab in the tree, or nil.
func (t *runTab) node() *store.Node {
	if t.e.tree == nil {
		return nil
	}
	return t.e.tree.Find(t.path)
}

// storeChanged follows the files: the list of requests is made again from the tree when the tab is drawn.
func (t *runTab) storeChanged() {
	t.gone = t.e.tree != nil && t.node() == nil
	if n := t.node(); n != nil && !t.running {
		t.tab.SetTitle("Run " + n.Name)
	}
	t.tab.Refresh()
}

// requests lists the requests of the group in the order they run: the entries of a collection or folder in the order of the file,
// the requests of a folder where the folder is.
func (t *runTab) requests() []runRequest {
	n := t.node()
	if n == nil {
		return nil
	}
	var out []runRequest
	var walk func(g *store.Node, prefix string)
	walk = func(g *store.Node, prefix string) {
		for _, c := range g.Children {
			switch c.Kind {
			case store.KindRequest:
				out = append(out, runRequest{node: c, rel: prefix + c.Name})
			case store.KindCollection, store.KindFolder:
				walk(c, prefix+c.Name+" / ")
			}
		}
	}
	walk(n, "")
	return out
}

// runnable says that a request can be sent: its file was read and is not from a newer version.
func runnable(n *store.Node) bool { return n.Err == nil && n.Request != nil && !n.ReadOnly }

// selected are the requests that will run.
func (t *runTab) selected() []runRequest {
	var out []runRequest
	for _, r := range t.requests() {
		if runnable(r.node) && !t.skip[r.node.Path] {
			out = append(out, r)
		}
	}
	return out
}

// setAll checks or unchecks every request.
func (t *runTab) setAll(on bool) {
	for _, r := range t.requests() {
		if on {
			delete(t.skip, r.node.Path)
		} else {
			t.skip[r.node.Path] = true
		}
	}
	t.tab.Refresh()
}

// toggle checks or unchecks a request.
func (t *runTab) toggle(path string, on bool) {
	if on {
		delete(t.skip, path)
	} else {
		t.skip[path] = true
	}
	t.tab.Refresh()
}

// environment is the environment of the run, nil for none (or one that was deleted).
func (t *runTab) environment() *store.Node {
	if t.envKey == "" || t.e.tree == nil {
		return nil
	}
	for _, n := range t.e.tree.Environments {
		if n.Key == t.envKey {
			return n
		}
	}
	return nil
}

// dataPath is the path of the data file, "" for none; a relative one is taken from the folder of the workspace.
func (t *runTab) dataPath() string {
	p := strings.TrimSpace(t.data.Value())
	if p != "" && !filepath.IsAbs(p) {
		p = filepath.Join(t.e.folder, filepath.FromSlash(p))
	}
	return p
}

// dataEdited forgets the table of a file when the box names another one.
func (t *runTab) dataEdited() {
	if t.table != nil && t.dataFor != t.dataPath() {
		t.table = nil
	}
	if t.dataFor != t.dataPath() {
		t.dataErr = ""
	}
	t.note = pageui.Note{}
	t.tab.Refresh()
}

// loadData reads the data file named in the box, off the UI thread, and shows how many rows it has.
func (t *runTab) loadData() {
	path := t.dataPath()
	if path == "" {
		t.table, t.dataErr, t.dataFor = nil, "", ""
		t.tab.Refresh()
		return
	}
	var tb rundata.Table
	t.e.async(func(ctx context.Context) error {
		var err error
		tb, err = rundata.Load(ctx, path)
		return err //nolint:wrapcheck // the error of rundata is the one the tab shows
	}, func(err error) {
		if t.closed || t.dataPath() != path {
			return
		}
		t.dataFor = path
		if err != nil {
			t.table, t.dataErr = nil, err.Error()
			t.e.api.Log().Warn("the data file of the runner cannot be read", "err", err)
		} else {
			t.table, t.dataErr = &tb, ""
		}
		t.tab.Refresh()
	})
}

// clearData forgets the data file.
func (t *runTab) clearData() {
	t.data.SetValue("")
	t.dataEdited()
}

// chooseData asks for the data file with the dialog of the desktop.
func (t *runTab) chooseData() {
	e := t.e
	go func() {
		path, err := e.api.FileChooser().Open(e.ctx, extension.FileChooserOptions{
			Title: "Choose the data file", AcceptLabel: "Choose", Folder: e.folder,
			Filters: []extension.FileFilter{{Name: "Data files", Patterns: []string{"*.csv", "*.json"}}},
		})
		e.api.Post(func() {
			if t.closed {
				return
			}
			if err != nil {
				e.fail("choose the data file", err)
				return
			}
			if path == "" {
				return
			}
			if rel, rerr := filepath.Rel(e.folder, path); rerr == nil && !strings.HasPrefix(rel, "..") {
				path = filepath.ToSlash(rel)
			}
			t.data.SetValue(path)
			t.dataEdited()
			t.loadData()
		})
	}()
}

// parseBox reads a whole number from a box, within min and max; ok is false and the note says why when it is not one.
func (t *runTab) parseBox(c *ui.InputBoxController, what string, min, max int) (n int, ok bool) {
	text := strings.TrimSpace(c.Value())
	n, err := strconv.Atoi(text)
	if err != nil || n < min || n > max {
		t.note = pageui.Bad(what + " must be a whole number from " + strconv.Itoa(min) + " to " + strconv.Itoa(max) + ".")
		t.tab.Refresh()
		return 0, false
	}
	return n, true
}

// start runs the selected requests, off the UI thread: the results arrive one by one.
func (t *runTab) start() {
	e := t.e
	if t.running || t.gone || !e.ready() {
		return
	}
	n := t.node()
	if n == nil {
		return
	}
	picked := t.selected()
	if len(picked) == 0 {
		t.note = pageui.Bad("Check at least one request to run.")
		t.tab.Refresh()
		return
	}
	iterations, ok := t.parseBox(t.iterations, "Iterations", 1, maxIterations)
	if !ok {
		return
	}
	delayMs, ok := t.parseBox(t.delay, "The delay", 0, maxDelayMs)
	if !ok {
		return
	}
	env := t.environment()
	plan := runPlan{
		store: e.store, iterations: iterations, delay: durationMs(delayMs), stopOnFail: t.stop, dataPath: t.dataPath(),
	}
	for _, r := range picked {
		plan.items = append(plan.items, runItem{path: r.node.Path, name: r.rel, method: r.node.Request.Method, place: e.placeIn(r.node.Path, env)})
	}
	t.name, t.envName = n.Name, ""
	if env != nil {
		t.envName = env.Name
	}
	t.results, t.end, t.total, t.filter, t.open, t.note = nil, 0, iterations, 0, map[int]bool{}, pageui.Note{}
	if plan.dataPath != "" && t.table != nil && t.dataFor == plan.dataPath {
		t.total = len(t.table.Rows)
	}
	ctx, cancel := context.WithCancel(e.ctx)
	t.running, t.cancel = true, cancel
	t.tab.SetBusy(true)
	t.tab.Refresh()
	var end runEnd
	var runErr error
	e.async(func(context.Context) error {
		end, runErr = e.execute(ctx, plan, runner{
			result: func(r runResult) { e.api.Post(func() { t.addResult(r) }) },
			rows:   func(tb rundata.Table) { e.api.Post(func() { t.gotRows(plan.dataPath, tb) }) },
		})
		return nil
	}, func(error) {
		cancel()
		t.finish(end, runErr)
	})
}

// gotRows takes the data file the run read as the table of the tab.
func (t *runTab) gotRows(path string, tb rundata.Table) {
	if t.closed {
		return
	}
	t.total = len(tb.Rows)
	if t.dataPath() == path {
		t.table, t.dataFor, t.dataErr = &tb, path, ""
	}
	t.tab.Refresh()
}

// addResult shows the result of one request.
func (t *runTab) addResult(r runResult) {
	if t.closed {
		return
	}
	t.results = append(t.results, r)
	if len(r.Extracted) > 0 {
		t.e.valuesChanged()
	}
	t.tab.Refresh()
}

// finish shows how the run ended.
func (t *runTab) finish(end runEnd, err error) {
	t.running, t.cancel = false, nil
	if t.closed {
		return
	}
	t.end = end
	t.tab.SetBusy(false)
	if err != nil {
		t.note = pageui.Bad("The run did not start: " + err.Error())
		t.dataErr = err.Error()
	}
	t.tab.Refresh()
}

// cancelRun stops the run: the request that is being sent is canceled and no other is sent.
func (t *runTab) cancelRun() {
	if t.running && t.cancel != nil {
		t.cancel()
	}
}

// durationMs is a number of milliseconds.
func durationMs(ms int) time.Duration { return time.Duration(ms) * time.Millisecond }
