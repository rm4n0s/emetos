package editor

import (
	"crypto/rand"
	"strings"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/edit"
	"github.com/rm4n0s/emetos/internal/core/snippet"
)

// Ported from VS Code: src/vs/editor/contrib/snippet/browser/snippetController2.ts and
// src/vs/workbench/contrib/snippets/browser/tabCompletion.ts and commands/insertSnippet.ts

// SnippetPicker is a short list the editor asks its widget to show at the caret: the options of a
// choice (`${1|a,b|}`), the snippets that the Tab key could insert, or the snippets to pick for
// Insert Snippet. Until the quick input exists (step 6.17) it is a kit.ListOverlay.
type SnippetPicker struct {
	// Items are the rows: a label and, for a snippet, its description.
	Items []SnippetPickerItem
	// Filterable lets the user type to filter the rows.
	Filterable bool
	// Accept is called with the index of the row the user chose; Dismiss when the user gave up.
	// Either one closes the picker.
	Accept  func(index int)
	Dismiss func()
}

// SnippetPickerItem is one row of a SnippetPicker.
type SnippetPickerItem struct {
	Label, Description string
}

// snippetLink is what a view model keeps of snippets.
type snippetLink struct {
	ctrl      *snippet.Controller
	service   *snippet.Service
	workspace snippet.WorkspaceResolver
	// picker is the list being shown, or nil.
	picker *SnippetPicker
	// tab are the snippets the Tab key would insert (editor.tabCompletion "onlySnippets").
	tab []*snippet.Snippet
	// busy is set while the view model changes the document for a snippet itself.
	busy bool
}

// SetSnippets gives the editor the snippet files it offers for Insert Snippet and for the Tab key.
func (vm *ViewModel) SetSnippets(svc *snippet.Service) {
	vm.snip.service = svc
	vm.updateTabSnippets()
}

// SetSnippetWorkspace tells the editor which folder is open, for WORKSPACE_NAME, WORKSPACE_FOLDER
// and RELATIVE_FILEPATH.
func (vm *ViewModel) SetSnippetWorkspace(w snippet.WorkspaceResolver) { vm.snip.workspace = w }

// snippets is the controller of the editor's snippet session, made when first needed.
func (vm *ViewModel) snippets() *snippet.Controller {
	if vm.snip.ctrl == nil {
		vm.snip.ctrl = snippet.NewController(snippet.Editor{Doc: vm.doc, Cursors: vm.cursors, Owner: vm.owner(snippetOwner)}, vm.snippetEnv, vm.snippetStateChanged)
		vm.snip.ctrl.OnChoice(vm.showChoices)
	}
	return vm.snip.ctrl
}

// SnippetState is the snippet mode of the editor, which the context keys inSnippetMode,
// hasNextTabstop, hasPrevTabstop and inSnippetChoice show.
func (vm *ViewModel) SnippetState() snippet.State {
	if vm.snip.ctrl == nil {
		return snippet.State{}
	}
	return vm.snip.ctrl.State()
}

// HasSnippetCompletions tells whether the Tab key would insert a snippet (hasSnippetCompletions).
func (vm *ViewModel) HasSnippetCompletions() bool { return len(vm.snip.tab) > 0 }

// SnippetPicker is the list the widget has to show at the caret, or nil.
func (vm *ViewModel) SnippetPicker() *SnippetPicker { return vm.snip.picker }

func (vm *ViewModel) snippetStateChanged(snippet.State) {
	// the context keys follow the selection listeners
	vm.sel.NotifyListeners()
}

// snippetEnv gathers what the variables of a snippet need.
func (vm *ViewModel) snippetEnv() snippet.Env {
	sep := vm.opts.WordSeparators
	if sep == "" {
		sep = edit.DefaultWordSeparators
	}
	env := snippet.Env{
		WordSeparators:  edit.NewWordClassifier(sep),
		Workspace:       vm.snip.workspace,
		ClipboardSpread: vm.opts.MultiCursorPaste != "full",
		Now:             time.Now,
		TimezoneName:    time.Local.String(),
		Random:          rand.Reader,
	}
	uri := vm.doc.URI()
	env.FilePath = uri.Path
	if uri.Scheme == document.SchemeFile {
		if folder := vm.snip.workspace.Folder; folder != "" {
			if rel, ok := strings.CutPrefix(uri.Path, strings.TrimSuffix(folder, "/")+"/"); ok {
				env.RelativePath = rel
			}
		}
	}
	if env.RelativePath == "" {
		env.RelativePath = env.FilePath
	}
	if vm.comments != nil {
		if l := vm.comments.Language(vm.languageCode); l != nil {
			env.Comments = snippet.CommentResolver{LineComment: l.Line, BlockCommentStart: l.BlockStart, BlockCommentEnd: l.BlockEnd}
		}
	}
	return env
}

// InsertSnippet inserts a snippet text at the cursors and starts the session of its tab stops.
// ClipboardText is for CLIPBOARD; the caller reads the clipboard first when the snippet needs it.
// The cursors and the view are updated as after a command.
func (vm *ViewModel) InsertSnippet(template string, opts snippet.InsertOptions) error {
	return vm.Run(func(vm *ViewModel, _ any) error { return vm.insertSnippet(template, opts) }, nil)
}

func (vm *ViewModel) insertSnippet(template string, opts snippet.InsertOptions) error {
	vm.dismissSnippetPicker(false)
	opts.Overtyped = vm.overtypedBy
	err := vm.snippets().Insert(template, opts)
	vm.history.Clear()
	vm.maxLineWidth = 0
	vm.Sync()
	vm.NotifyListeners()
	return err
}

// overtypedBy gives the text a cursor typed over last. The editor does not keep it yet.
func (vm *ViewModel) overtypedBy(int) *snippet.Overtyped { return nil }

// insertSnippetValue inserts a snippet of a file, after reading the clipboard when it needs it.
// The editor must not have changed by then.
func (vm *ViewModel) insertSnippetValue(s *snippet.Snippet, overwriteBefore int) error {
	opts := snippet.DefaultInsertOptions()
	opts.OverwriteBefore = overwriteBefore
	if !s.NeedsClipboard() {
		return vm.insertSnippet(s.CodeSnippet(), opts)
	}
	link, err := vm.clipboard()
	if err != nil {
		return err
	}
	version, sels := vm.doc.Version(), vm.cursors.Selections()
	link.clipboard.Read(func(text string, err error) {
		if err != nil {
			link.report(errors.NewErr("SnippetClipboardReadFailed", err))
			return
		}
		if vm.doc.Version() != version || !buffer.SelectionsEqual(vm.cursors.Selections(), sels) {
			return // the editor changed in the meantime
		}
		opts.ClipboardText = text
		if err := vm.Run(func(vm *ViewModel, _ any) error { return vm.insertSnippet(s.CodeSnippet(), opts) }, nil); err != nil {
			link.report(err)
		}
	})
	return nil
}

// updateTabSnippets finds the snippets that Tab would insert at the cursor.
//
// Ported from VS Code: tabCompletion.ts (_updateSnippets)
func (vm *ViewModel) updateTabSnippets() {
	vm.snip.tab = nil
	if vm.snip.service == nil || vm.opts.TabCompletion != "onlySnippets" || vm.cursors.Len() == 0 {
		return
	}
	snap := vm.doc.Snapshot()
	sel := vm.cursors.Primary().Selection
	candidates := func() []*snippet.Snippet {
		return vm.snip.service.Snippets(snippet.Query{LanguageID: vm.doc.LanguageID(), Path: vm.doc.URI().Path})
	}
	if sel.IsEmpty() {
		pos := sel.Active
		line, err := snap.LineContent(pos.Line)
		if err != nil || pos.Col == 0 || pos.Col > len(line) || snippet.NonWhitespacePrefix(line, pos.Col) == "" {
			return
		}
		vm.snip.tab = snippet.TabCompletions(candidates(), line[:pos.Col], "", false, false)
		return
	}
	r := sel.Range()
	if r.Start.Line != r.End.Line || r.End.Col-r.Start.Col > 400 {
		return
	}
	line, err := snap.LineContent(r.Start.Line)
	if err != nil || r.End.Col > len(line) {
		return
	}
	vm.snip.tab = snippet.TabCompletions(candidates(), "", line[r.Start.Col:r.End.Col], true, false)
}

// performSnippetCompletions is the Tab key with snippets before the cursor: one snippet is
// inserted, several are offered in a list.
//
// Ported from VS Code: tabCompletion.ts (performSnippetCompletions)
func (vm *ViewModel) performSnippetCompletions() error {
	vm.updateTabSnippets()
	list := vm.snip.tab
	switch len(list) {
	case 0:
		return nil
	case 1:
		return vm.insertSnippetValue(list[0], len(list[0].Prefix))
	}
	items := make([]SnippetPickerItem, len(list))
	for i, s := range list {
		items[i] = SnippetPickerItem{Label: s.Prefix, Description: s.Description}
	}
	vm.showSnippetPicker(&SnippetPicker{Items: items, Accept: func(i int) {
		vm.runLater(func() error { return vm.insertSnippetValue(list[i], len(list[i].Prefix)) })
	}})
	return nil
}

// insertSnippetCommand is editor.action.insertSnippet: the argument may give the snippet text
// ("snippet"), the name of a snippet of the language ("name", "langId"); without them the user
// picks one.
//
// Ported from VS Code: commands/insertSnippet.ts
func (vm *ViewModel) insertSnippetCommand(args any) error {
	m, _ := args.(map[string]any)
	str := func(key string) string { s, _ := m[key].(string); return s }
	if text := str("snippet"); text != "" {
		return vm.insertSnippet(text, snippet.DefaultInsertOptions())
	}
	if vm.snip.service == nil {
		return nil
	}
	lang := str("langId")
	if lang == "" {
		lang = vm.doc.LanguageID()
	}
	if name := str("name"); name != "" {
		for _, s := range vm.snip.service.Snippets(snippet.Query{LanguageID: lang, IncludeNoPrefix: true}) {
			if s.Name == name {
				return vm.insertSnippetValue(s, 0)
			}
		}
		return nil
	}
	list := vm.snip.service.Snippets(snippet.Query{LanguageID: lang, Path: vm.doc.URI().Path, IncludeNoPrefix: true})
	if len(list) == 0 {
		return nil
	}
	items := make([]SnippetPickerItem, len(list))
	for i, s := range list {
		label := s.Name
		desc := s.Description
		if s.Prefix != "" {
			desc = strings.TrimSpace(s.Prefix + "  " + desc)
		}
		items[i] = SnippetPickerItem{Label: label, Description: desc}
	}
	vm.showSnippetPicker(&SnippetPicker{Items: items, Filterable: true, Accept: func(i int) {
		vm.runLater(func() error { return vm.insertSnippetValue(list[i], 0) })
	}})
	return nil
}

// runLater runs an editing function as a command, for the callbacks of the pickers.
func (vm *ViewModel) runLater(fn func() error) {
	if err := vm.Run(func(vm *ViewModel, _ any) error { return fn() }, nil); err != nil && vm.clip != nil && vm.clip.report != nil {
		vm.clip.report(err)
	}
}

// showChoices offers the options of a choice (`${1|a,b|}`) when the session reaches its tab stop.
func (vm *ViewModel) showChoices(choice *snippet.Choice, _ buffer.Range) {
	items := make([]SnippetPickerItem, len(choice.Options))
	for i, o := range choice.Options {
		items[i] = SnippetPickerItem{Label: o.Value}
	}
	vm.showSnippetPicker(&SnippetPicker{Items: items, Accept: func(i int) {
		vm.runLater(func() error { return vm.acceptChoice(choice.Options[i].Value) })
	}})
}

// acceptChoice replaces the text of the active choice by an option; the cursor goes after it.
func (vm *ViewModel) acceptChoice(value string) error {
	if vm.snip.ctrl == nil {
		return nil
	}
	_, r, ok := vm.snip.ctrl.ActiveChoice()
	if !ok {
		return nil
	}
	snap := vm.doc.Snapshot()
	end := snap.OffsetAt(r.Start) + len(value)
	vm.snip.busy = true
	defer func() { vm.snip.busy = false }()
	_, err := vm.doc.Edit([]document.Edit{{Range: r, Text: value}}, document.EditOptions{
		Before: vm.cursors.Selections(),
		After: func([]document.Edit) []buffer.Selection {
			pos := vm.doc.Snapshot().PositionAt(end)
			return []buffer.Selection{{Anchor: pos, Active: pos}}
		},
	})
	if err == nil {
		pos := vm.doc.Snapshot().PositionAt(end)
		vm.cursors.SetSelections([]buffer.Selection{{Anchor: pos, Active: pos}})
	}
	vm.snip.busy = false
	vm.snip.ctrl.SelectionsChanged()
	vm.Sync()
	vm.NotifyListeners()
	return err
}

func (vm *ViewModel) showSnippetPicker(p *SnippetPicker) {
	vm.snip.picker = p
	accept, dismiss := p.Accept, p.Dismiss
	p.Accept = func(i int) {
		vm.dismissSnippetPicker(true)
		if accept != nil {
			accept(i)
		}
	}
	p.Dismiss = func() {
		vm.dismissSnippetPicker(true)
		if dismiss != nil {
			dismiss()
		}
	}
	vm.NotifyListeners()
}

// dismissSnippetPicker closes the list; the widget gives the focus back to the editor.
func (vm *ViewModel) dismissSnippetPicker(notify bool) {
	if vm.snip.picker == nil {
		return
	}
	vm.snip.picker = nil
	if notify {
		vm.NotifyListeners()
	}
}

// snippetCursorsChanged follows the cursors for the session and the Tab key.
func (vm *ViewModel) snippetCursorsChanged() {
	if vm.snip.ctrl != nil && !vm.snip.busy {
		vm.snip.ctrl.SelectionsChanged()
	}
	vm.updateTabSnippets()
}

// snippetPlaceholders are the decorations of the tab stops of the session in some rows.
func (vm *ViewModel) snippetPlaceholders(firstRow, lastRow int) []document.Decoration {
	if vm.snip.ctrl == nil || !vm.snip.ctrl.IsInSnippet() {
		return nil
	}
	proj := vm.Projection()
	firstLine, _, _ := proj.RowSpan(firstRow)
	lastLine, _, _ := proj.RowSpan(lastRow)
	return vm.doc.DecorationsIn(buffer.Range{Start: buffer.Position{Line: firstLine}, End: buffer.Position{Line: lastLine + 1}}, vm.owner(snippetOwner))
}

// The snippet commands (SPEC §9.9).
func snippetActions(a map[string]action) {
	session := func(do func(c *snippet.Controller) error) action {
		return func(vm *ViewModel, _ any) error {
			if vm.snip.ctrl == nil {
				return nil
			}
			return do(vm.snip.ctrl)
		}
	}
	a["jumpToNextSnippetPlaceholder"] = session(func(c *snippet.Controller) error { return c.Next() })
	a["jumpToPrevSnippetPlaceholder"] = session(func(c *snippet.Controller) error { return c.Prev() })
	a["acceptSnippet"] = session(func(c *snippet.Controller) error { return c.Finish() })
	a["leaveSnippet"] = session(func(c *snippet.Controller) error { c.Cancel(true); return nil })
	a["insertSnippet"] = func(vm *ViewModel, _ any) error { return vm.performSnippetCompletions() }
	a["editor.action.insertSnippet"] = func(vm *ViewModel, args any) error { return vm.insertSnippetCommand(args) }
}
