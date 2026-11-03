package httpclient

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/httpclient/engine"
	"github.com/rm4n0s/emetos/extensions/httpclient/format"
	"github.com/rm4n0s/emetos/extensions/httpclient/store"
	"github.com/rm4n0s/emetos/extensions/internal/pageui"
)

// RequestKindID is the kind of editor tab of the request tab (SPEC §10.33 "The request tab").
const RequestKindID = ID + ".request"

// The sections of the request tab.
const (
	secParams   = "params"
	secHeaders  = "headers"
	secBody     = "body"
	secAuth     = "auth"
	secSettings = "settings"
	secExtract  = "extract"
	secChecks   = "checks"
)

// sections are the tabs inside the request tab, in order.
var sections = []struct{ id, label string }{
	{secParams, "Params"}, {secHeaders, "Headers"}, {secBody, "Body"}, {secAuth, "Auth"}, {secSettings, "Settings"}, {secExtract, "Extract"}, {secChecks, "Checks"},
}

// reqTabState is what a request tab keeps in the state of the window: the file it edits and, for the work that is not in a file, the
// draft (an unsaved request opened from the history, or changes that are not saved: they come back after a restart, as the unsaved
// documents do with hot exit).
type reqTabState struct {
	Path    string              `json:"path,omitempty"`
	Draft   *store.RequestFiles `json:"draft,omitempty"`
	Base    string              `json:"base,omitempty"`
	Section string              `json:"section,omitempty"`
}

// reqTab is the request tab: a form over a draft of one request. It is used on the UI thread.
type reqTab struct {
	e   *Extension
	tab extension.EditorTab

	// path is the request file; empty for a request that is not saved anywhere yet.
	path string
	d    *draft
	// baseline is the snapshot of the draft as it is saved; the tab is dirty while the draft differs.
	baseline string
	// pending is a draft from the state that waits for the file to be read to find out whether it differs from it.
	pending *store.RequestFiles
	section string

	loading, gone bool
	readOnly      bool
	problem       string
	// diskChanged says the file changed on disk while the tab has edits.
	diskChanged bool

	syncing bool
	// bulk are the bulk editors ("name: value" lines) of the tables; open says which are shown.
	bulk map[string]*ui.InputBoxController
	open map[string]bool
	// showSecrets shows the passwords and tokens of the Auth tab.
	showSecrets bool
	scroll      ui.ScrollHandle
	tokens      map[string]*tokenCache

	note   pageui.Note
	saving bool

	send sendState
}

// sendState is the request being sent and what the last one gave.
type sendState struct {
	sending bool
	cancel  context.CancelFunc
	// status is the line under the form; bad says it is an error.
	status string
	code   int
	bad    bool
	// resp is the last response; the body of an earlier one is removed when the next arrives.
	resp *engine.Response
}

// createRequestTab makes the content of a request tab, for a new one and for one that comes back after a restart.
func (e *Extension) createRequestTab(tab extension.EditorTab, raw json.RawMessage) (extension.EditorTabContent, error) {
	var st reqTabState
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &st); err != nil {
			return extension.EditorTabContent{}, errors.NewErr("HttpRequestTabStateInvalid", err)
		}
	}
	if st.Path == "" && st.Draft == nil {
		return extension.EditorTabContent{}, errors.New("HttpRequestTabStateMissing", "a request tab is opened with a request file or a draft")
	}
	t := &reqTab{e: e, tab: tab, path: st.Path, section: st.Section, bulk: map[string]*ui.InputBoxController{}, open: map[string]bool{}, tokens: map[string]*tokenCache{}}
	if t.section == "" {
		t.section = secParams
	}
	e.reqTabs = append(e.reqTabs, t)
	e.lastReq = t
	tab.SetTitle(titleOfPath(st.Path))
	tab.SetHint(st.Path)
	switch {
	case st.Draft != nil && st.Path == "":
		t.adopt(*st.Draft, st.Base)
	case st.Draft != nil:
		t.pending = st.Draft
		t.adopt(*st.Draft, "")
		t.baseline = st.Base
		t.tab.SetDirty(t.d.snapshot() != t.baseline)
	}
	t.storeChanged()
	return extension.EditorTabContent{
		Build:     t.build,
		SaveState: t.saveState,
		Save:      t.saveForClose,
		OnClose:   t.onClose,
	}, nil
}

// titleOfPath is the title of a tab before the request's name is known.
func titleOfPath(path string) string {
	if path == "" {
		return "New Request"
	}
	return strings.TrimSuffix(filepath.Base(path), format.RequestSuffix)
}

// onClose lets go of everything the tab holds.
func (t *reqTab) onClose() {
	e := t.e
	for i, x := range e.reqTabs {
		if x == t {
			e.reqTabs = append(e.reqTabs[:i], e.reqTabs[i+1:]...)
			break
		}
	}
	if e.lastReq == t {
		e.lastReq = nil
		if n := len(e.reqTabs); n > 0 {
			e.lastReq = e.reqTabs[n-1]
		}
	}
	if t.send.cancel != nil {
		t.send.cancel()
	}
	t.dropResponse()
	if t.d != nil {
		t.d.dispose()
	}
	for _, c := range t.bulk {
		c.Dispose()
	}
}

// dropResponse removes the file of the last response's body, if it has one.
func (t *reqTab) dropResponse() {
	if r := t.send.resp; r != nil && r.Body != nil {
		if err := r.Body.Remove(); err != nil {
			t.e.api.Log().Warn("the body of a response was not removed", "err", err)
		}
	}
	t.send.resp = nil
}

// saveState is the state the window keeps for the tab.
func (t *reqTab) saveState() (any, error) {
	st := reqTabState{Path: t.path, Section: t.section}
	if t.d != nil && (t.path == "" || t.dirty()) {
		f := t.d.files()
		st.Draft, st.Base = &f, t.baseline
	}
	return st, nil
}

// dirty says that the draft differs from what is saved.
func (t *reqTab) dirty() bool { return t.d != nil && t.d.snapshot() != t.baseline }

// adopt makes the draft from a request and its body texts and watches its boxes. The baseline is base, or the draft itself when base
// is empty.
func (t *reqTab) adopt(files store.RequestFiles, base string) {
	if t.d != nil {
		t.d.dispose()
	}
	t.d = newDraft(files)
	t.watchDraft()
	t.baseline = base
	if base == "" {
		t.baseline = t.d.snapshot()
	}
	t.tab.SetTitle(t.title())
	t.tab.SetDirty(t.d.snapshot() != t.baseline)
	t.tab.Refresh()
}

// title is the name of the request, else the name of its file.
func (t *reqTab) title() string {
	if t.d != nil && strings.TrimSpace(t.d.req.Name) != "" {
		return t.d.req.Name
	}
	return titleOfPath(t.path)
}

// watchDraft makes every box tell the tab when its text changes, so that the dirty mark, the Params table and the URL follow what is
// typed (and what is set from code).
func (t *reqTab) watchDraft() {
	d := t.d
	watch := func(c *ui.InputBoxController, fn func()) {
		last := c.Value()
		c.TextController().AddListener(func() {
			if v := c.Value(); v != last {
				last = v
				fn()
			}
		})
	}
	watch(d.url, func() {
		if t.syncing {
			return
		}
		t.syncing = true
		d.paramsFromURL()
		t.watchNewRows(d.params, secParams)
		t.syncing = false
		t.refreshBulk(secParams)
		t.edited()
	})
	for _, c := range []*ui.InputBoxController{d.gqlVars, d.binary, d.timeout, d.maxRedirects} {
		watch(c, t.edited)
	}
	for _, c := range d.texts {
		watch(c, t.edited)
	}
	for _, c := range d.auth.box {
		watch(c, t.edited)
	}
	t.watchRows(d.params, secParams)
	t.watchRows(d.headers, secHeaders)
	t.watchRows(d.form, "")
	t.watchRows(d.multi, "")
}

// watchRows makes the boxes of the rows tell the tab; section is the table they are in when it is kept in step with something.
func (t *reqTab) watchRows(rows []*pairBox, section string) {
	for _, r := range rows {
		t.watchRow(r, section)
	}
}

func (t *reqTab) watchRow(r *pairBox, section string) {
	r.watched = true
	for _, c := range []*ui.InputBoxController{r.name, r.value, r.desc} {
		c := c
		last := c.Value()
		c.TextController().AddListener(func() {
			if v := c.Value(); v != last {
				last = v
				t.rowEdited(section)
			}
		})
	}
}

// rowEdited is called when a box of a table changed.
func (t *reqTab) rowEdited(section string) {
	if t.syncing {
		return
	}
	if section == secParams {
		t.syncing = true
		t.d.urlFromParams()
		t.syncing = false
	}
	t.refreshBulk(section)
	t.edited()
}

// edited updates the dirty mark after a change of the draft.
func (t *reqTab) edited() {
	if t.d == nil {
		return
	}
	t.note = pageui.Note{}
	t.tab.SetDirty(t.dirty())
	t.tab.Refresh()
}

// ---- following the files ----

// node is the request in the tree, or nil.
func (t *reqTab) node() *store.Node {
	if t.path == "" || t.e.tree == nil {
		return nil
	}
	return t.e.tree.Find(t.path)
}

// storeChanged follows the files: a tab with no edits shows what is on disk now; one with edits keeps them and says when the file
// changed.
func (t *reqTab) storeChanged() {
	if t.path == "" {
		return
	}
	if t.e.tree == nil || t.e.store == nil {
		return
	}
	n := t.node()
	switch {
	case n == nil:
		t.gone = true
		t.tab.Refresh()
		return
	case n.Err != nil && !n.ReadOnly:
		t.gone, t.problem = false, n.Err.Error()
		t.tab.Refresh()
		return
	}
	t.gone, t.problem, t.readOnly = false, "", n.ReadOnly
	if t.d == nil {
		t.loading = true
		t.tab.Refresh()
	}
	t.reload(n)
}

// reload reads the request file and its body files off the UI thread and shows them when the tab has no edits.
func (t *reqTab) reload(n *store.Node) {
	var files store.RequestFiles
	path := n.Path
	t.e.async(func(ctx context.Context) error {
		f, err := t.e.store.ReadRequest(ctx, path)
		if err != nil && !format.IsNewer(err) {
			return err
		}
		files = f
		return nil
	}, func(err error) {
		t.loading = false
		if t.path != path {
			return
		}
		if err != nil {
			t.problem = err.Error()
			t.tab.Refresh()
			return
		}
		t.problem = ""
		disk := newDraft(files)
		diskSnap := disk.snapshot()
		disk.dispose()
		switch {
		case t.d == nil:
			t.adopt(files, "")
		case t.pending != nil:
			// a draft came back from the last session: it is dirty when it differs from the file
			t.baseline = diskSnap
			t.pending = nil
			t.tab.SetDirty(t.dirty())
		case !t.dirty() && diskSnap != t.baseline:
			t.adopt(files, "")
		case diskSnap != t.baseline:
			t.diskChanged = true
		}
		t.tab.Refresh()
	})
}

// ---- save ----

// saveForClose is the Save of the tab's content: the close question's Save.
func (t *reqTab) saveForClose(done func(saved bool)) { t.save(done) }

// save writes the request and its body files. A request that is not saved anywhere asks for a collection first. done, when set, is told
// whether it is saved.
func (t *reqTab) save(done func(saved bool)) {
	finish := func(ok bool) {
		if done != nil {
			done(ok)
		}
	}
	if t.d == nil || t.saving {
		finish(false)
		return
	}
	if t.readOnly || t.problem != "" || t.gone {
		t.note = pageui.Bad("This request cannot be saved: " + firstNonEmpty(t.problem, "its file is read-only, deleted or moved."))
		t.tab.Refresh()
		finish(false)
		return
	}
	if !t.e.ready() {
		finish(false)
		return
	}
	if t.path == "" {
		t.saveNew(finish)
		return
	}
	files, snap := t.d.files(), t.d.snapshot()
	path := t.path
	t.saving = true
	t.tab.SetBusy(true)
	t.e.async(func(ctx context.Context) error { return t.e.store.SaveRequest(ctx, path, files) }, func(err error) {
		t.saving = false
		t.tab.SetBusy(false)
		if err != nil {
			t.note = pageui.Bad("Cannot save: " + err.Error())
			t.e.api.Log().Warn("the request cannot be saved", "err", err)
			t.tab.Refresh()
			finish(false)
			return
		}
		t.baseline, t.diskChanged = snap, false
		t.note = pageui.Fine("Saved.")
		t.tab.SetDirty(t.dirty())
		t.tab.Refresh()
		finish(!t.dirty())
	})
}

// saveNew asks for the collection or folder a request that is not saved anywhere goes to, and makes it there.
func (t *reqTab) saveNew(finish func(bool)) {
	e := t.e
	var groups []*store.Node
	e.tree.Walk(func(n *store.Node) bool {
		if (n.Kind == store.KindCollection || n.Kind == store.KindFolder) && !n.ReadOnly && n.Err == nil {
			groups = append(groups, n)
		}
		return true
	})
	if len(groups) == 0 {
		e.api.Notifications().Info("There is no collection to save the request in. Make one with New Collection first.")
		finish(false)
		return
	}
	items := make([]extension.QuickPickItem, len(groups))
	for i, g := range groups {
		items[i] = extension.QuickPickItem{Label: "$(" + nodeIcon(g) + ") " + g.Name, Description: e.where(g)}
	}
	err := e.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: "Save the request in which collection or folder?", Items: items, MatchOnDescription: true}, func(i int) {
		if i < 0 || i >= len(groups) {
			finish(false)
			return
		}
		t.askNameAndCreate(groups[i], finish)
	})
	if err != nil {
		e.fail("show the collections", err)
		finish(false)
		return
	}
}

// askNameAndCreate asks for the name of the request and makes it in the group.
func (t *reqTab) askNameAndCreate(parent *store.Node, finish func(bool)) {
	suggest := strings.TrimSpace(t.d.req.Name)
	if suggest == "" {
		suggest = strings.TrimSpace(t.d.method + " " + pathOf(t.d.url.Value()))
	}
	t.askRequestName(suggest, finish, func(name string) {
		files := t.d.files()
		files.Request.Name = name
		if b := files.Request.Body; b != nil { // the store names the files of the body after the new request
			c := *b
			c.File, c.Variables = "", ""
			files.Request.Body = &c
		}
		var path string
		t.saving = true
		t.tab.SetBusy(true)
		t.e.async(func(ctx context.Context) (err error) {
			path, err = t.e.store.CreateRequest(ctx, parent.Path, files)
			return err
		}, func(err error) {
			t.saving = false
			t.tab.SetBusy(false)
			if err != nil {
				t.note = pageui.Bad("Cannot save: " + err.Error())
				t.e.api.Log().Warn("the request cannot be saved", "err", err)
				t.tab.Refresh()
				finish(false)
				return
			}
			t.path = path
			t.d.req.Name = name
			t.baseline = t.d.snapshot()
			t.tab.SetTitle(name)
			t.tab.SetHint(path)
			t.tab.SetDirty(false)
			t.note = pageui.Fine("Saved in \"" + parent.Name + "\".")
			t.tab.Refresh()
			t.e.collections.reveal(path)
			finish(true)
		})
	})
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// ---- sending ----

// sendRequest sends the request as the form has it. Only one send runs at a time.
func (t *reqTab) sendRequest() {
	if t.d == nil || t.send.sending {
		return
	}
	e := t.e
	job := sendJob{files: t.d.files(), place: e.placeOf(t.path)}
	ctx, cancel := context.WithCancel(e.ctx)
	t.send.sending, t.send.cancel = true, cancel
	t.send.status, t.send.bad, t.send.code = "Sending...", false, 0
	t.tab.SetBusy(true)
	t.tab.Refresh()
	var out sendOutcome
	e.async(func(context.Context) error {
		out = e.runSend(ctx, job)
		return nil
	}, func(error) {
		cancel()
		t.send.sending, t.send.cancel = false, nil
		t.tab.SetBusy(false)
		t.finishSend(job, out)
	})
}

// cancelSend stops the request that is being sent.
func (t *reqTab) cancelSend() {
	if t.send.sending && t.send.cancel != nil {
		t.send.cancel()
	}
}

// finishSend shows how a send ended and records it in the history.
func (t *reqTab) finishSend(job sendJob, out sendOutcome) {
	t.dropResponse()
	t.send.resp = out.resp
	switch {
	case out.err != nil && out.resp == nil:
		t.send.status, t.send.bad, t.send.code = out.err.Error(), true, 0
		if appErr, ok := errors.FromError(out.err); ok && appErr.Tag == "EngineCanceled" {
			t.send.status, t.send.bad = "Canceled.", false
		}
	default:
		t.send.status, t.send.bad, t.send.code = statusLine(out), out.err != nil, out.resp.StatusCode
	}
	if err := t.e.RecordSent(sentOf(job, out, t.e.opts.Now())); err != nil {
		t.e.api.Log().Warn("the request cannot be added to the history", "err", err)
	}
	t.tab.Refresh()
}

// statusLine is the minimal status line under the form (the response pane of step 27.5 shows the rest): the status, the time and the
// size, then what the request used that had no value.
func statusLine(out sendOutcome) string {
	r := out.resp
	size := int64(0)
	if r.Body != nil {
		size = r.Body.Size()
	}
	parts := []string{r.Status, durationText(out.duration), byteSize(size)}
	line := strings.Join(parts, "  ·  ")
	if len(r.Unresolved) > 0 {
		line += "  ·  no value for " + strings.Join(r.Unresolved, ", ")
	}
	if len(r.MissingSecrets) > 0 {
		line += "  ·  the secret " + strings.Join(r.MissingSecrets, ", ") + " has no value (set it in the environment editor)"
	}
	if out.err != nil {
		line += "  ·  " + out.err.Error()
	}
	return line
}

// ---- helpers for the form ----

// addRow adds an empty row to a table.
func (t *reqTab) addRow(rows *[]*pairBox, section string) {
	r := newPairBox(format.Pair{Enabled: true})
	*rows = append(*rows, r)
	t.watchRow(r, section)
	t.refreshBulk(section)
	t.edited()
}

// removeRow takes a row out of a table.
func (t *reqTab) removeRow(rows *[]*pairBox, r *pairBox, section string) {
	for i, x := range *rows {
		if x == r {
			*rows = append((*rows)[:i], (*rows)[i+1:]...)
			r.dispose()
			break
		}
	}
	t.rowEdited(section)
}

// toggleRow switches a row on or off.
func (t *reqTab) toggleRow(r *pairBox, on bool, section string) {
	r.enabled = on
	t.rowEdited(section)
}

// rowsOf are the rows of the table of a section.
func (t *reqTab) rowsOf(section string) *[]*pairBox {
	switch section {
	case secParams:
		return &t.d.params
	case secHeaders:
		return &t.d.headers
	}
	return nil
}

// bulkBox is the bulk editor of a table, made when first needed.
func (t *reqTab) bulkBox(section string) *ui.InputBoxController {
	c := t.bulk[section]
	if c == nil {
		c = ui.NewInputBoxController("")
		t.bulk[section] = c
		last := ""
		c.TextController().AddListener(func() {
			if v := c.Value(); v != last {
				last = v
				t.bulkEdited(section)
			}
		})
	}
	return c
}

// toggleBulk shows or hides the bulk editor of a table; showing it fills it from the rows.
func (t *reqTab) toggleBulk(section string) {
	t.open[section] = !t.open[section]
	if t.open[section] {
		t.fillBulk(section)
	}
	t.tab.Refresh()
}

// fillBulk writes the rows into the bulk editor.
func (t *reqTab) fillBulk(section string) {
	rows := t.rowsOf(section)
	if rows == nil {
		return
	}
	c := t.bulkBox(section)
	t.syncing = true
	c.SetValue(bulkText(*rows))
	t.syncing = false
}

// refreshBulk keeps an open bulk editor in step with a change of the rows.
func (t *reqTab) refreshBulk(section string) {
	if !t.open[section] || t.syncing {
		return
	}
	if rows := t.rowsOf(section); rows != nil && t.bulk[section] != nil {
		text := bulkText(*rows)
		if t.bulk[section].Value() != text {
			t.fillBulk(section)
		}
	}
}

// bulkEdited reads the lines of a bulk editor into the rows of its table.
func (t *reqTab) bulkEdited(section string) {
	if t.syncing || t.d == nil {
		return
	}
	rows := t.rowsOf(section)
	if rows == nil {
		return
	}
	t.syncing = true
	*rows = setRows(*rows, parseBulk(t.bulk[section].Value()))
	t.watchNewRows(*rows, section)
	if section == secParams {
		t.d.urlFromParams()
	}
	t.syncing = false
	t.edited()
}

// watched are the rows that have listeners already.
func (t *reqTab) watchNewRows(rows []*pairBox, section string) {
	for _, r := range rows {
		if r.watched {
			continue
		}
		r.watched = true
		t.watchRow(r, section)
	}
}

// setMethod changes the method.
func (t *reqTab) setMethod(m string) {
	t.d.method = strings.ToUpper(strings.TrimSpace(m))
	if t.d.method == "" {
		t.d.method = "GET"
	}
	t.edited()
}

// askMethod asks for a method of one's own.
func (t *reqTab) askMethod() {
	err := t.e.api.InputBox().Show(extension.InputBoxOptions{
		Prompt: "The method (a word such as PROPFIND)", Value: t.d.method,
		Validate: func(v string) string {
			v = strings.TrimSpace(v)
			if v == "" || strings.ContainsAny(v, " \t:/()<>@,;\\\"[]?={}") {
				return "A method is one word of letters, digits and dashes."
			}
			return ""
		},
	}, func(v string, ok bool) {
		if ok {
			t.setMethod(v)
		}
	})
	if err != nil {
		t.e.fail("ask for the method", err)
	}
}

// chooseFile asks for a file with the dialog of the desktop and puts its path (relative to the folder when it is inside it) in the box.
func (t *reqTab) chooseFile(box *ui.InputBoxController) {
	e := t.e
	go func() {
		path, err := e.api.FileChooser().Open(e.ctx, extension.FileChooserOptions{Title: "Choose the file to send", AcceptLabel: "Choose", Folder: e.folder})
		e.api.Post(func() {
			if err != nil {
				e.fail("choose the file", err)
				return
			}
			if path == "" {
				return
			}
			if rel, rerr := filepath.Rel(e.folder, path); rerr == nil && !strings.HasPrefix(rel, "..") {
				path = filepath.ToSlash(rel)
			}
			box.SetValue(path)
		})
	}()
}

// askRequestName asks for the name of a request that is saved for the first time; cancelled is told when the question is dismissed.
func (t *reqTab) askRequestName(suggest string, cancelled func(bool), fn func(name string)) {
	err := t.e.api.InputBox().Show(extension.InputBoxOptions{
		Prompt: "Name of the request", Value: suggest,
		Validate: func(v string) string {
			if strings.TrimSpace(v) == "" {
				return "The name cannot be empty."
			}
			return ""
		},
	}, func(v string, ok bool) {
		if !ok || strings.TrimSpace(v) == "" {
			cancelled(false)
			return
		}
		fn(strings.TrimSpace(v))
	})
	if err != nil {
		t.e.fail("ask for the name", err)
		cancelled(false)
	}
}

// showSection goes to a section of the tab by its id; an unknown id changes nothing.
func (t *reqTab) showSection(id string) {
	for _, s := range sections {
		if s.id == id && t.section != id {
			t.section = id
			t.tab.Refresh()
		}
	}
}
