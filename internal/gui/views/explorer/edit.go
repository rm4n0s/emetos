package explorer

import (
	"path/filepath"
	"strings"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/theme"
	"github.com/rm4n0s/emetos/internal/core/workspace"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// The inline input of the Explorer: the row in which the name of a new file or folder is typed, and the one
// that renames an item (SPEC §10.5).
//
// Ported from VS Code: src/vs/workbench/contrib/files/browser/views/explorerViewer.ts (renderInputBox),
// src/vs/workbench/browser/parts/views/inlineEditInputBox.ts and
// src/vs/workbench/contrib/files/browser/fileActions.ts (openExplorerAndCreate, renameHandler)

// EditKind says what the inline input does.
type EditKind int

const (
	// EditNewFile names a file to create.
	EditNewFile EditKind = iota + 1
	// EditNewFolder names a folder to create.
	EditNewFolder
	// EditRename gives an item another name.
	EditRename
)

// EditResult is a name the user committed in the inline input.
type EditResult struct {
	Kind EditKind
	// Parent is the folder a new item goes in; for a rename, the folder the item is in.
	Parent string
	// Path is the item that is renamed; empty for a new item.
	Path string
	// Name is what was typed (it may hold slashes).
	Name string
}

// Keys of the widgets of the inline input.
var (
	// EditInputKey is the key of the inline input.
	EditInputKey = foundation.NewValueKey("explorer:edit-input")
)

// editState is the inline input that is open.
type editState struct {
	kind   EditKind
	parent string
	path   string
	// key is the key of the row of the input in the tree.
	key      string
	original string
	ctl      *kit.InputBoxController
	// selection is the state of the F2 key in a rename: the name without its extension, all of it, the extension.
	selection int
	// ahead takes the keys typed before the input is built (focusEdit).
	ahead *kit.TypeAhead
}

// newEditKey is the key of the row of a new item below a folder; no path has the NUL byte.
func newEditKey(parent string) string { return filepath.Join(parent, "\x00new") }

// Editing tells whether the inline input is open.
func (v *View) Editing() bool { return v.edit != nil }

// StartNew opens the inline input for a new file or folder in a folder of the tree: the folder is read and
// opened, and the input row shows at the top of its files (or of its folders, for a folder), with the
// keyboard focus. Nothing happens while an input is open already or when the folder is not in the tree.
func (v *View) StartNew(folder string, isFolder bool) {
	if v.edit != nil || v.disposed {
		return
	}
	item := v.ws.Find(folder)
	if item == nil || !item.IsDir() {
		return
	}
	go func() {
		_, err := item.Children(v.ctx)
		v.opts.Post(func() {
			if v.disposed || v.edit != nil {
				return
			}
			if err != nil {
				if v.opts.OnLoadFailed != nil {
					v.opts.OnLoadFailed(folder, err)
				}
				return
			}
			kind := EditNewFile
			if isFolder {
				kind = EditNewFolder
			}
			e := &editState{kind: kind, parent: folder, key: newEditKey(folder), ctl: kit.NewInputBoxController("")}
			v.beginEdit(e)
			v.rebuild()
			if folder != v.ws.Path() {
				if key := v.KeyOf(folder); key != "" {
					v.tree.Reveal(key)
					v.tree.Expand(key)
				}
			}
			v.tree.SetFocus(e.key)
			v.scrollTo = e.key
			v.notify()
			v.focusEdit(e)
		})
	}()
}

// StartRename opens the inline input on the row of an item, with its name selected without the extension. It
// does nothing while an input is open, or when the item has no row of its own (the middle of a compact
// chain).
func (v *View) StartRename(path string) {
	if v.edit != nil || v.disposed {
		return
	}
	item := v.ws.Find(path)
	if item == nil || item.IsRoot() {
		return
	}
	key := v.KeyOf(path)
	if it := v.itemOf(key); key == "" || it == nil || it.Path() != path {
		return
	}
	name := item.Name()
	e := &editState{kind: EditRename, parent: filepath.Dir(path), path: path, key: key, original: name, ctl: kit.NewInputBoxController(name)}
	end := len(name)
	if dot := strings.LastIndex(name, "."); dot > 0 && !item.IsDir() {
		end = dot
	}
	e.ctl.Select(0, end)
	v.beginEdit(e)
	v.tree.SetFocus(key)
	v.scrollTo = key
	v.notify()
	v.focusEdit(e)
}

// focusEdit gives the input the keyboard focus once its row is on screen: the autofocus of a text field does
// nothing while the tree has the focus.
func (v *View) focusEdit(e *editState) {
	if b := v.opts.Binding; b != nil {
		// the keys typed until the row is built and the input has the focus go into it, over the selected part of the name
		e.ahead = kit.StartTypeAhead(b, e.ctl.TextController(), e.ctl.FocusNode())
	}
	after := v.opts.AfterFrame
	if after == nil {
		after = v.opts.Post
	}
	after(func() {
		if !v.disposed && v.edit == e {
			e.ctl.Focus()
		}
	})
}

// beginEdit makes an input the open one.
func (v *View) beginEdit(e *editState) {
	v.edit = e
	// the keys of the input: Escape cancels, F2 cycles the selection of a name; the input box chains to this handler
	// for the keys it does not take
	e.ctl.FocusNode().SetOnKeyEvent(func(_ *widgets.FocusNode, ev services.KeyEvent) widgets.KeyEventResult {
		return v.editKey(e, ev)
	})
	if v.opts.Keys != nil {
		v.opts.Keys.Set("inputFocus", true)
	}
}

// editKey handles the keys the input box leaves: Escape cancels the input, F2 selects the name without its
// extension, all of it, then the extension (VS Code's rename).
func (v *View) editKey(e *editState, ev services.KeyEvent) widgets.KeyEventResult {
	if _, ok := ev.(*services.KeyDownEvent); !ok {
		return widgets.KeyEventResultIgnored
	}
	switch ev.LogicalKey() {
	case services.LogicalKeyboardKeyEscape:
		if v.edit == e {
			v.finishEdit(true)
		}
		return widgets.KeyEventResultHandled
	case services.LogicalKeyboardKeyF2:
		text := e.ctl.Value()
		dot := strings.LastIndex(text, ".")
		if e.kind != EditRename || dot == -1 || e.isFolder(v) {
			return widgets.KeyEventResultHandled
		}
		e.selection = (e.selection + 1) % 3
		switch e.selection {
		case 1:
			e.ctl.Select(0, len(text))
		case 2:
			e.ctl.Select(dot+1, len(text))
		default:
			e.ctl.Select(0, dot)
		}
		return widgets.KeyEventResultHandled
	}
	return widgets.KeyEventResultIgnored
}

// isFolder tells whether the item of a rename is a folder.
func (e *editState) isFolder(v *View) bool {
	if it := v.ws.Find(e.path); it != nil {
		return it.IsDir()
	}
	return false
}

// problem checks the text of an input against the names in the folder.
func (v *View) problem(e *editState, text string) *workspace.NameProblem {
	folder := v.ws.Find(e.parent)
	exists := func(name string) bool {
		return folder != nil && !strings.ContainsAny(name, "/\\") && folder.Child(name) != nil
	}
	return workspace.ValidateFileName(text, e.original, exists)
}

// message is the validation message of an input: the problem of the text without the marks of bold.
func (v *View) message(e *editState, text string) *kit.InputMessage {
	p := v.problem(e, text)
	if p == nil {
		return nil
	}
	content := strings.ReplaceAll(strings.ReplaceAll(p.Content, `\*`, "*"), "**", "")
	typ := kit.ValidationWarning
	if p.Severity == workspace.SeverityError {
		typ = kit.ValidationError
	}
	return &kit.InputMessage{Content: content, Type: typ}
}

// submit is Enter in the input: a name with an error stays open and shows its message.
func (v *View) submit(e *editState) {
	if v.edit != e {
		return
	}
	e.ctl.Validate()
	if p := v.problem(e, e.ctl.Value()); p != nil && p.Severity == workspace.SeverityError {
		return
	}
	v.finishEdit(false)
}

// editFocus follows the keyboard focus of the input: leaving it commits a valid name and cancels an invalid one
// (VS Code's blur), once the focus has settled.
func (v *View) editFocus(e *editState, focused bool) {
	if focused {
		if v.opts.Keys != nil && v.edit == e {
			v.opts.Keys.Set("inputFocus", true)
		}
		return
	}
	after := v.opts.AfterFrame
	if after == nil {
		after = v.opts.Post
	}
	after(func() {
		if v.disposed || v.edit != e || e.ctl.HasFocus() {
			return
		}
		p := v.problem(e, e.ctl.Value())
		v.leaveEdit(e, p != nil && p.Severity == workspace.SeverityError)
	})
}

// CancelEdit closes the inline input without committing it.
func (v *View) CancelEdit() {
	if v.edit != nil {
		v.finishEdit(true)
	}
}

// finishEdit closes the input; unless cancel is set, the name goes to OnCommitEdit. The tree gets the keyboard
// focus back.
func (v *View) finishEdit(cancel bool) {
	e := v.edit
	if e == nil {
		return
	}
	v.leaveEdit(e, cancel)
	v.ctl.Focus()
}

// leaveEdit closes the input without giving the tree the focus.
func (v *View) leaveEdit(e *editState, cancel bool) {
	if v.edit != e {
		return
	}
	if e.ahead != nil {
		e.ahead.Stop()
	}
	text := e.ctl.Value()
	v.edit = nil
	v.retired = append(v.retired, e.ctl)
	if v.opts.Keys != nil {
		v.opts.Keys.Set("inputFocus", false)
	}
	if e.kind != EditNewFile && e.kind != EditNewFolder {
		v.scrollTo = ""
	}
	v.rebuild()
	if cancel || v.opts.OnCommitEdit == nil {
		return
	}
	if e.kind == EditRename && strings.TrimRight(strings.Trim(text, "\t"), "/\\") == e.original {
		return // the same name: nothing to do
	}
	v.opts.OnCommitEdit(EditResult{Kind: e.kind, Parent: e.parent, Path: e.path, Name: text})
}

// releaseEdit lets go of the open input when the view goes.
func (v *View) releaseEdit() {
	if v.edit != nil {
		v.retired = append(v.retired, v.edit.ctl)
		v.edit = nil
	}
}

// withEdit puts the row of a new item into the children of a folder: at the top of the folders for a folder, and of
// the files (after the folders) for a file, where a name that sorts first goes.
func (v *View) withEdit(children *[]kit.TreeItem, folder string) {
	e := v.edit
	if e == nil || e.kind == EditRename || e.parent != folder {
		return
	}
	row := kit.TreeItem{Key: e.key, Data: &nodeData{Edit: true, IsDir: e.kind == EditNewFolder}}
	at := 0
	if e.kind == EditNewFile {
		at = len(*children)
		for i, c := range *children {
			if nd, ok := c.Data.(*nodeData); ok && !nd.IsDir {
				at = i
				break
			}
		}
	}
	list := *children
	list = append(list[:at:at], append([]kit.TreeItem{row}, list[at:]...)...)
	*children = list
}

// editRow is the row of the inline input: the icon of what is being named and the input box.
func (v *View) editRow(bc widgets.BuildContext, nd *nodeData, st kit.TreeRowState) widgets.Widget {
	e := v.edit
	colors := kit.ThemeOf(bc)
	var icon widgets.Widget
	name := e.original
	if e.kind != EditRename {
		name = e.ctl.Value()
	}
	if v.opts.Icons != nil {
		if th := v.opts.Icons(); th != nil {
			q := theme.FileIconQuery{Kind: theme.FileIconFile, Name: name, Type: colors.Type}
			if nd.IsDir {
				q.Kind = theme.FileIconFolder
			} else if v.opts.Language != nil && name != "" {
				q.LanguageID = v.opts.Language(filepath.Join(e.parent, name))
			}
			if ic, ok := th.IconFor(q); ok {
				icon = FileIconWidget(ic, st.Foreground)
			}
		}
	}
	input := kit.NewInputBox(nil, nil, kit.InputBoxOpts{
		Key: EditInputKey, Controller: e.ctl, Autofocus: true,
		Validate:      func(text string) *kit.InputMessage { return v.message(e, text) },
		OnChanged:     func(string) { v.notify() },
		OnSubmit:      func(string) { v.submit(e) },
		OnFocusChange: func(has bool) { v.editFocus(e, has) },
	})
	return iconRow(icon, widgets.NewExpanded(widgets.NewSizedBox(input, widgets.SizedBoxOpts{Height: foundation.Some(22.0)})))
}

// EditKind returns what the open input does, and false when none is open.
func (v *View) EditKind() (EditKind, bool) {
	if v.edit == nil {
		return 0, false
	}
	return v.edit.kind, true
}

// EditText is the text in the inline input, empty when none is open.
func (v *View) EditText() string {
	if v.edit == nil {
		return ""
	}
	return v.edit.ctl.Value()
}

// EditSelection is the selected range of the text in the inline input, in bytes (start equals end for a caret).
func (v *View) EditSelection() (start, end int) {
	if v.edit == nil {
		return 0, 0
	}
	start, end, _ = v.edit.ctl.Selection()
	return start, end
}
