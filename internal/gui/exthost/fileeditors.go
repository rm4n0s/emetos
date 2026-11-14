package exthost

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/glob"
)

// File editors (SPEC §10.37 "File editors", D52): the editors of files that the extensions declare, which of them a file opens in
// (the priorities and workbench.editorAssociations), and their tabs: one instance of the provider's content per editor tab of the
// window, shown in a view box, with its state, its status bar items and the changes of its file on disk.

// TextEditorID is the editor id that stands for the text editor in workbench.editorAssociations and in Reopen Editor With.
const TextEditorID = "default"

// fileEditorDecl is a file editor the manifest declares.
type fileEditorDecl struct {
	spec     extension.FileEditorSpec
	slot     *slot
	provider *extension.FileEditorProvider
	inst     *instance
	globs    []interface{ Match(string) bool }
}

// FileEditorInfo is a file editor that matches a file, as the window offers it.
type FileEditorInfo struct {
	// ID is the id of the editor and Name its name; Extension is the name of its extension.
	ID, Name, Extension string
	// Icon is the code point of its Codicon (0 for none).
	Icon int
	// Default tells that a matching file opens in it unless an association says otherwise.
	Default bool
}

// installFileEditors adds the file editors of a manifest to the editor declarations of its slot.
func installFileEditors(d *editorDecls, sl *slot) {
	for _, spec := range sl.info.Manifest.Contributes.FileEditors {
		fe := &fileEditorDecl{spec: spec, slot: sl}
		for _, g := range spec.Globs {
			fe.globs = append(fe.globs, glob.Parse(g, glob.Options{IgnoreCase: true}))
		}
		d.fileEditors = append(d.fileEditors, fe)
	}
}

// matches tells whether the editor is for a file with the base name.
func (fe *fileEditorDecl) matches(base string) bool {
	for _, g := range fe.globs {
		if g.Match(base) {
			return true
		}
	}
	return false
}

// FileEditorsFor are the file editors of the extensions that are on whose globs match the base name of path, in the order of the
// extensions and their manifests.
func (s *Scope) FileEditorsFor(path string) []FileEditorInfo {
	base := filepath.Base(path)
	var out []FileEditorInfo
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sl := range s.slots {
		if !liveSlot(sl) || sl.editors == nil {
			continue
		}
		for _, fe := range sl.editors.fileEditors {
			if fe.matches(base) {
				out = append(out, FileEditorInfo{ID: fe.spec.ID, Name: fe.spec.Name, Extension: sl.info.Manifest.Name, Icon: iconCode(fe.spec.Icon),
					Default: fe.spec.Priority == extension.FileEditorDefault})
			}
		}
	}
	return out
}

// ResolveFileEditor is the editor a file opens in: the editor id of the most specific key of associations (workbench.editorAssociations)
// that matches the file, when it is TextEditorID or an editor that matches the file; else the first default file editor that matches;
// else "" (the text editor). A key with a slash is matched against the whole path, any other against the base name, without regard to
// case; the longest matching key is the most specific (VS Code's editorResolverService).
func (s *Scope) ResolveFileEditor(path string, associations map[string]string) string {
	editors := s.FileEditorsFor(path)
	keys := make([]string, 0, len(associations))
	for k := range associations {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if len(keys[i]) != len(keys[j]) {
			return len(keys[i]) > len(keys[j])
		}
		return keys[i] < keys[j]
	})
	base := filepath.Base(path)
	for _, k := range keys {
		target := base
		if strings.Contains(k, "/") {
			target = filepath.ToSlash(path)
		}
		if !glob.Parse(k, glob.Options{IgnoreCase: true}).Match(target) {
			continue
		}
		id := associations[k]
		if id == TextEditorID {
			return ""
		}
		if slices.ContainsFunc(editors, func(e FileEditorInfo) bool { return e.ID == id }) {
			return id
		}
	}
	for _, e := range editors {
		if e.Default {
			return e.ID
		}
	}
	return ""
}

// fileEditorDeclOf finds the declaration of a file editor among the extensions that are on. s.mu is held.
func (s *Scope) fileEditorDeclOf(editorID string) *fileEditorDecl {
	for _, sl := range s.slots {
		if !liveSlot(sl) || sl.editors == nil {
			continue
		}
		for _, fe := range sl.editors.fileEditors {
			if fe.spec.ID == editorID {
				return fe
			}
		}
	}
	return nil
}

// fileEditorInstance is a file editor in one editor tab.
type fileEditorInstance struct {
	key      any
	editorID string
	path     string
	folder   string
	state    json.RawMessage
	decl     *viewDecl
	fe       *fileEditorDecl
	// failure is the error of Open; the tab shows it.
	failure error
	opened  bool
	content *extension.FileEditorContent
	// changes are the listeners of OnChange.
	changes []*func(extension.FileEditorChange)
	// items are the status bar items the tab set, handles their handles in the bar, and active tells that the tab is the active
	// editor of the window.
	items   []extension.StatusItem
	handles []extension.StatusHandle
	active  bool
	busy    bool
}

// FileEditorRequest names the tab a file editor body is for: Key identifies the editor tab (the window uses its input), Path the
// file, Folder the folder of the workspace, and State what the tab saved, for a tab that comes back.
type FileEditorRequest struct {
	Key            any
	EditorID, Path string
	Folder         string
	State          json.RawMessage
}

// FileEditorBody is the widget of a file editor in the editor tab of req.Key. The extension is activated when the widget is on
// screen. It reports false when the editor is not available (its extension is off or failed, or it is not declared); the error is
// the failure of the provider's Open, which the window shows with a way to the text editor.
func (s *Scope) FileEditorBody(req FileEditorRequest) (widgets.Widget, bool, error) {
	s.mu.Lock()
	fe := s.fileEditorDeclOf(req.EditorID)
	if fe == nil {
		s.mu.Unlock()
		return nil, false, nil
	}
	var inst *fileEditorInstance
	for _, x := range s.fileEditorInstances {
		if x.key == req.Key {
			inst = x
		}
	}
	if inst != nil && inst.editorID != req.EditorID {
		s.mu.Unlock()
		s.ReleaseFileEditor(req.Key)
		s.mu.Lock()
		inst = nil
	}
	if inst == nil {
		d := &viewDecl{id: "fileeditor:" + req.EditorID + "@" + newTabID(), slot: fe.slot, event: extension.OnFileEditor(req.EditorID)}
		inst = &fileEditorInstance{key: req.Key, editorID: req.EditorID, path: req.Path, folder: req.Folder, state: req.State, decl: d, fe: fe}
		s.fileEditorInstances = append(s.fileEditorInstances, inst)
		if s.viewDecls == nil {
			s.viewDecls = map[string]*viewDecl{}
		}
		s.viewDecls[d.id] = d
	}
	s.mu.Unlock()
	s.openFileEditor(inst)
	s.mu.Lock()
	failure := inst.failure
	s.mu.Unlock()
	if failure != nil {
		return nil, true, failure
	}
	return newViewBox(s, inst.decl), true, nil
}

// openFileEditor calls the provider's Open for an instance, once, when the extension has provided the editor.
func (s *Scope) openFileEditor(inst *fileEditorInstance) {
	s.mu.Lock()
	fe := inst.fe
	if inst.opened || fe.provider == nil || fe.inst == nil || fe.inst.isDead() {
		s.mu.Unlock()
		return
	}
	inst.opened = true
	provider, ext := fe.provider, fe.inst
	s.mu.Unlock()
	var content extension.FileEditorContent
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				failure := errors.NewErrf("ExtensionFileEditorPanicked", "it panicked in Open of the file editor %s: %v", inst.editorID, r).SetMetadata("extension", ext.id, "editor", inst.editorID, "scope", s.opts.Info.Name)
				s.post(func() { s.fail(fe.slot, failure, "file editor "+inst.editorID) })
				err = failure
			}
		}()
		content, err = provider.Open(fileEditorFile{s: s, inst: inst}, fileEditorTab{s: s, inst: inst, ext: ext})
	}()
	if err == nil && content.Build == nil {
		err = errors.New("ExtensionFileEditorContentInvalid", "the content of a file editor needs a Build function", "extension", ext.id, "editor", inst.editorID)
	}
	s.mu.Lock()
	if err != nil {
		inst.failure = err
		s.mu.Unlock()
		ext.log.Warn("a file editor could not open its file", "editor", inst.editorID, "path", inst.path, "err", describe(err))
		return
	}
	inst.content = &content
	inst.decl.content = &extension.ViewContent{Build: content.Build}
	inst.decl.inst = ext
	s.mu.Unlock()
}

// FileEditorState is the state the file editor of a tab saves (its SaveState), or nil.
func (s *Scope) FileEditorState(key any) json.RawMessage {
	s.mu.Lock()
	var inst *fileEditorInstance
	for _, x := range s.fileEditorInstances {
		if x.key == key {
			inst = x
		}
	}
	if inst == nil {
		s.mu.Unlock()
		return nil
	}
	state := inst.state
	if inst.content == nil || inst.content.SaveState == nil || inst.decl.inst == nil || inst.decl.inst.isDead() {
		s.mu.Unlock()
		return state
	}
	save, ext, fe := inst.content.SaveState, inst.decl.inst, inst.fe
	s.mu.Unlock()
	var v any
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				failure := errors.NewErrf("ExtensionFileEditorPanicked", "it panicked in SaveState of the file editor %s: %v", inst.editorID, r).SetMetadata("extension", ext.id, "editor", inst.editorID, "scope", s.opts.Info.Name)
				s.post(func() { s.fail(fe.slot, failure, "file editor "+inst.editorID) })
				err = failure
			}
		}()
		v, err = save()
	}()
	if err != nil {
		ext.log.Warn("the state of a file editor cannot be saved", "editor", inst.editorID, "err", describe(err))
		return state
	}
	if v == nil {
		return nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		ext.log.Warn("the state of a file editor does not encode", "editor", inst.editorID, "err", err)
		return state
	}
	s.mu.Lock()
	inst.state = data
	s.mu.Unlock()
	return data
}

// ReleaseFileEditor ends the file editor of a tab that closed, or that is reopened in another editor: OnClose is called and its status
// bar items go.
func (s *Scope) ReleaseFileEditor(key any) {
	s.mu.Lock()
	var gone []*fileEditorInstance
	s.fileEditorInstances = slices.DeleteFunc(s.fileEditorInstances, func(x *fileEditorInstance) bool {
		if x.key == key {
			gone = append(gone, x)
			delete(s.viewDecls, x.decl.id)
			return true
		}
		return false
	})
	s.mu.Unlock()
	for _, x := range gone {
		s.closeFileEditor(x)
	}
}

// closeFileEditor calls OnClose of an instance and takes its status bar items away.
func (s *Scope) closeFileEditor(x *fileEditorInstance) {
	s.mu.Lock()
	var onClose func()
	if x.content != nil {
		onClose = x.content.OnClose
	}
	ext, handles := x.decl.inst, x.handles
	x.content, x.handles, x.items, x.changes = nil, nil, nil, nil
	s.mu.Unlock()
	for _, h := range handles {
		h.Dispose()
	}
	if onClose == nil || ext == nil || ext.isDead() {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			failure := errors.NewErrf("ExtensionFileEditorPanicked", "it panicked in OnClose of the file editor %s: %v", x.editorID, r).SetMetadata("extension", ext.id, "editor", x.editorID, "scope", s.opts.Info.Name)
			s.post(func() { s.fail(x.fe.slot, failure, "file editor "+x.editorID) })
		}
	}()
	onClose()
}

// ShowsFile tells whether a file editor tab shows the file at path. It may be called on any goroutine: the window asks it before it
// hands a change of the file to the UI thread, so the changes of other files cost no frame.
func (s *Scope) ShowsFile(path string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, x := range s.fileEditorInstances {
		if x.path == path {
			return true
		}
	}
	return false
}

// FileEditorChanged tells the file editors of a file that it changed on disk, or was deleted. It runs on the UI thread.
func (s *Scope) FileEditorChanged(path string, deleted bool) {
	s.mu.Lock()
	var calls []func(extension.FileEditorChange)
	var exts []*instance
	for _, x := range s.fileEditorInstances {
		if x.path != path || x.decl.inst == nil {
			continue
		}
		for _, f := range x.changes {
			calls = append(calls, *f)
			exts = append(exts, x.decl.inst)
		}
	}
	s.mu.Unlock()
	for i, f := range calls {
		ext := exts[i]
		if ext.isDead() {
			continue
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					failure := errors.NewErrf("ExtensionFileEditorPanicked", "it panicked in OnChange of a file editor: %v", r).SetMetadata("extension", ext.id, "path", path, "scope", s.opts.Info.Name)
					s.post(func() { s.fail(ext.slot, failure, "file editor") })
				}
			}()
			f(extension.FileEditorChange{Deleted: deleted})
		}()
	}
}

// SetActiveFileEditor says which tab is the active editor of the window (nil for none that is a file editor's): its status bar
// items show, the others' hide.
func (s *Scope) SetActiveFileEditor(key any) {
	s.mu.Lock()
	var update []*fileEditorInstance
	for _, x := range s.fileEditorInstances {
		active := key != nil && x.key == key
		if x.active != active {
			x.active = active
			update = append(update, x)
		}
	}
	s.mu.Unlock()
	for _, x := range update {
		s.syncFileEditorStatus(x)
	}
}

// FileEditorBusy tells whether the file editor of a tab marked it busy.
func (s *Scope) FileEditorBusy(key any) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, x := range s.fileEditorInstances {
		if x.key == key {
			return x.busy
		}
	}
	return false
}

// syncFileEditorStatus makes the status bar items of an instance what it set, shown while it is active.
func (s *Scope) syncFileEditorStatus(x *fileEditorInstance) {
	s.mu.Lock()
	ext, items, handles, active := x.decl.inst, slices.Clone(x.items), x.handles, x.active
	s.mu.Unlock()
	if ext == nil || ext.isDead() {
		return
	}
	prefix := strings.TrimPrefix(x.decl.id, "fileeditor:") + ":"
	// the handles follow the items by place: an item more is added, one less is disposed
	for i := len(items); i < len(handles); i++ {
		handles[i].Dispose()
	}
	if len(handles) > len(items) {
		handles = handles[:len(items)]
	}
	for i, item := range items {
		item.ID = prefix + item.ID
		if i < len(handles) {
			handles[i].Update(item)
			handles[i].SetVisible(active)
			continue
		}
		h, err := statusBarAPI{ext}.Add(item)
		if err != nil {
			ext.log.Debug("a status bar item of a file editor cannot be added", "item", item.ID, "err", err)
			continue
		}
		h.SetVisible(active)
		handles = append(handles, h)
	}
	s.mu.Lock()
	x.handles = handles
	s.mu.Unlock()
}

// fileEditorFile is the extension.FileEditorFile of an instance.
type fileEditorFile struct {
	s    *Scope
	inst *fileEditorInstance
}

// Path implements extension.FileEditorFile.
func (f fileEditorFile) Path() string { return f.inst.path }

// URI implements extension.FileEditorFile.
func (f fileEditorFile) URI() string { return document.FileURI(f.inst.path).String() }

// Folder implements extension.FileEditorFile.
func (f fileEditorFile) Folder() string { return f.inst.folder }

// State implements extension.FileEditorFile.
func (f fileEditorFile) State() json.RawMessage {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	return f.inst.state
}

// OnChange implements extension.FileEditorFile.
func (f fileEditorFile) OnChange(fn func(extension.FileEditorChange)) (cancel func()) {
	p := &fn
	f.s.mu.Lock()
	f.inst.changes = append(f.inst.changes, p)
	f.s.mu.Unlock()
	return func() {
		f.s.mu.Lock()
		defer f.s.mu.Unlock()
		f.inst.changes = slices.DeleteFunc(f.inst.changes, func(x *func(extension.FileEditorChange)) bool { return x == p })
	}
}

// fileEditorTab is the extension.FileEditorTab of an instance.
type fileEditorTab struct {
	s    *Scope
	inst *fileEditorInstance
	ext  *instance
}

// SetBusy implements extension.FileEditorTab.
func (t fileEditorTab) SetBusy(busy bool) {
	t.s.mu.Lock()
	changed := t.inst.busy != busy
	t.inst.busy = busy
	t.s.mu.Unlock()
	if changed {
		t.s.post(t.s.editorsChanged)
	}
}

// Refresh implements extension.FileEditorTab.
func (t fileEditorTab) Refresh() { t.s.post(func() { t.s.refreshDecl(t.inst.decl) }) }

// SetStatus implements extension.FileEditorTab.
func (t fileEditorTab) SetStatus(items []extension.StatusItem) {
	t.s.mu.Lock()
	t.inst.items = slices.Clone(items)
	t.s.mu.Unlock()
	t.s.syncFileEditorStatus(t.inst)
}

// Group implements extension.FileEditorTab.
func (t fileEditorTab) Group() int {
	host := t.s.opts.Editors
	if host == nil {
		return 0
	}
	return host.FileEditorGroup(t.inst.key)
}

// Active implements extension.FileEditorTab.
func (t fileEditorTab) Active() bool {
	t.s.mu.Lock()
	defer t.s.mu.Unlock()
	return t.inst.active
}

// ReopenAsText implements extension.FileEditorTab.
func (t fileEditorTab) ReopenAsText() {
	host := t.s.opts.Editors
	if host == nil {
		return
	}
	key := t.inst.key
	t.s.post(func() { host.ReopenFileEditorAsText(key) })
}

// fileEditorsAPI is the extension.FileEditors of an activation.
type fileEditorsAPI struct{ inst *instance }

// FileEditors implements extension.API.
func (a *api) FileEditors() extension.FileEditors { return fileEditorsAPI{a.inst} }

// Provide implements extension.FileEditors.
func (f fileEditorsAPI) Provide(editorID string, p extension.FileEditorProvider) error {
	in := f.inst
	if in.isDead() {
		return errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	s := in.scope
	s.mu.Lock()
	var fe *fileEditorDecl
	if in.slot.editors != nil {
		for _, x := range in.slot.editors.fileEditors {
			if x.spec.ID == editorID {
				fe = x
			}
		}
	}
	switch {
	case fe == nil:
		s.mu.Unlock()
		return errors.New("ExtensionFileEditorNotDeclared", "the manifest of the extension does not declare this file editor", "extension", in.id, "editor", editorID)
	case p.Open == nil:
		s.mu.Unlock()
		return errors.New("ExtensionFileEditorInvalid", "a file editor needs an Open function", "extension", in.id, "editor", editorID)
	case fe.provider != nil && fe.inst == in:
		s.mu.Unlock()
		return errors.New("ExtensionFileEditorProvided", "the activation provided this file editor already", "extension", in.id, "editor", editorID)
	}
	fe.provider, fe.inst = &p, in
	var waiting []*fileEditorInstance
	for _, x := range s.fileEditorInstances {
		if x.fe == fe {
			waiting = append(waiting, x)
		}
	}
	s.mu.Unlock()
	in.track(func() { s.releaseFileEditorDecl(fe, in) })
	for _, x := range waiting {
		s.openFileEditor(x)
		s.post(func() { s.refreshDecl(x.decl) })
	}
	s.post(s.editorsChanged)
	return nil
}

// releaseFileEditorDecl takes the content of a file editor away when the activation that provided it ends: its tabs wait for the
// extension again (a new activation opens them again with the state they saved).
func (s *Scope) releaseFileEditorDecl(fe *fileEditorDecl, in *instance) {
	s.mu.Lock()
	var mine []*fileEditorInstance
	for _, x := range s.fileEditorInstances {
		if x.fe == fe {
			mine = append(mine, x)
		}
	}
	s.mu.Unlock()
	for _, x := range mine {
		s.FileEditorState(x.key)
		s.closeFileEditor(x)
		s.mu.Lock()
		x.decl.content, x.decl.inst, x.opened, x.failure = nil, nil, false, nil
		s.mu.Unlock()
	}
	s.mu.Lock()
	if fe.inst == in {
		fe.provider, fe.inst = nil, nil
	}
	s.mu.Unlock()
	s.post(s.editorsChanged)
}
