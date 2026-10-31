package app

import (
	"context"
	"math"
	"path"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/gui/diffeditor"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
	"github.com/rm4n0s/emetos/internal/gui/multidiff"
)

// The multi-file diff editor in the window (SPEC §10.30 "Diffs of changes", §9.11, step 24.17): the kind of editor of a MultiDiffInput,
// its pane, and the body of each file. The pane keeps the layout (gui/multidiff) and the files the provider made; a body is the diff
// editor of §9.11 over the two documents of a file, made when its section comes near the viewport and ended when it leaves, so that the
// editors, the tokenizers and the open documents are those of a few files whatever the size of the set. A body reports the height of
// its content, which the layout uses to place the sections.
//
// Ported from VS Code: src/vs/workbench/contrib/multiDiffEditor/browser/multiDiffEditor.ts and
// src/vs/editor/browser/widget/multiDiffEditor/multiDiffEditorWidgetImpl.ts

// multiDiffKind is how the window handles MultiDiffInputs. The saved input holds the kind and the key of its set; its files come back
// when the provider of the kind registers (multidiffhost.go).
func multiDiffKind() editorKind {
	return editorKind{
		typeID: editorgroup.MultiDiffInputTypeID,
		icon:   fonts.CodiconDiffMultiple,
		restore: func(value string) (editorgroup.EditorInput, bool) {
			in, err := editorgroup.ParseMultiDiffInput(value)
			return in, err == nil
		},
		newPane: func(s *shellState, input editorgroup.EditorInput) pane {
			return newMultiDiffPane(s, input.(*editorgroup.MultiDiffInput))
		},
	}
}

const (
	// multiDiffNoticeHeight is the height of the body of a file that cannot be shown as text.
	multiDiffNoticeHeight = 44.0
	// multiDiffWaiting is the message of a restored editor whose provider has not registered.
	multiDiffWaiting = "Waiting for the extension that provides these changes..."
)

// multiDiffPane is the editor of a MultiDiffInput.
type multiDiffPane struct {
	paneFocus
	s     *shellState
	input *editorgroup.MultiDiffInput
	model *multidiff.Model
	node  *widgets.FocusNode
	// group is the group the pane was built in last: the editors of the bodies report their focus to it.
	group *editorgroup.Group
	// index finds the section of a key.
	index map[string]int
	// bodies are the bodies that exist, by the key of their section.
	bodies map[string]*sectionBody
	// loadGen counts the questions asked of the provider; the answer of an older one is dropped. loadCancel ends the one that runs.
	loadGen    int
	loadCancel context.CancelFunc
	// reveal is the document of the file to scroll to when the files arrive.
	reveal string
	// nav is where Go to Next Change is: the section and the change in it; pending says that the section was scrolled to and its diff is
	// not there yet.
	nav      multiDiffNav
	disposed bool
}

// multiDiffNav is the position of Go to Next Change and Go to Previous Change.
type multiDiffNav struct {
	section, change int
	pending, next   bool
}

// newMultiDiffPane makes the pane of an input and asks the provider for the files when it has not been asked.
func newMultiDiffPane(s *shellState, input *editorgroup.MultiDiffInput) *multiDiffPane {
	p := &multiDiffPane{
		s: s, input: input, model: multidiff.NewModel(0), index: map[string]int{}, bodies: map[string]*sectionBody{},
		node: widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "Multi-file diff editor"}),
	}
	p.nav.section = -1
	p.applyItems()
	p.load()
	return p
}

// build implements pane.
func (p *multiDiffPane) build(bc widgets.BuildContext, group *editorgroup.Group, _ bool) widgets.Widget {
	win := PeekWorkspace(bc)
	p.group = group
	return p.wrap(p.s, group, multidiff.NewWidget(p.model, multidiff.WidgetOpts{
		Body: func(_ widgets.BuildContext, i int) widgets.Widget {
			return &sectionBodyWidget{StatefulWidget: widgets.NewStatefulWidgetBase(), p: p, key: p.model.Section(i).Key}
		},
		Actions:   p.actions,
		Icons:     win.FileIcons,
		Language:  func(sec multidiff.Section) string { return win.languageOf(path.Join(sec.Folder, sec.Name)) },
		Message:   p.message,
		FocusNode: p.node,
		OnToggle:  func(int) { p.s.refreshKeys() },
	}))
}

// message is the text of the editor while it has no files.
func (p *multiDiffPane) message() string {
	switch {
	case p.input.Failure() != "" && !p.input.Loaded():
		return "Cannot show the changes: " + p.input.Failure()
	case !p.input.Loaded():
		if p.input.Fixed() || p.s.win.multiDiffProvider(p.input.Kind()) != nil {
			return "Loading the changes..."
		}
		return multiDiffWaiting
	}
	return "There are no changes to show."
}

// focus implements pane.
func (p *multiDiffPane) focus() { p.node.RequestFocus() }

// dispose implements pane: the question to the provider is dropped; the bodies end with their widgets.
func (p *multiDiffPane) dispose() {
	if p.disposed {
		return
	}
	p.disposed = true
	if p.loadCancel != nil {
		p.loadCancel()
	}
	p.model.Dispose()
	p.node.Dispose()
}

// applyItems makes the sections of the items of the input.
func (p *multiDiffPane) applyItems() {
	items := p.input.Items()
	secs := make([]multidiff.Section, len(items))
	p.index = make(map[string]int, len(items))
	for i, it := range items {
		secs[i] = multidiff.Section{
			Key: it.Key(), Name: path.Base(it.Path), Folder: folderOf(it.Path), File: it.File,
			Letter: it.Letter, Color: it.Color, Tooltip: it.Tooltip,
		}
		p.index[secs[i].Key] = i
	}
	p.model.SetSections(secs)
	if !p.input.Loaded() || len(items) == 0 {
		p.model.Refresh()
	}
	p.nav = multiDiffNav{section: -1}
}

// folderOf is the folder of a path with slashes; empty for a file at the root.
func folderOf(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return ""
}

// revealFile scrolls to the file whose Modified or Original document is uri once the files are there; "" for none.
func (p *multiDiffPane) revealFile(uri string) {
	if uri == "" {
		return
	}
	p.reveal = uri
	p.revealNow()
}

func (p *multiDiffPane) revealNow() {
	if p.reveal == "" || !p.input.Loaded() {
		return
	}
	for i, it := range p.input.Items() {
		if it.Modified == p.reveal || it.Original == p.reveal {
			p.model.ScrollToSection(i)
			break
		}
	}
	p.reveal = ""
}

// load asks the provider of the input's kind for the files, off the UI thread, and gives them to the input when they arrive. It does
// nothing for a fixed set, and while the provider is not there (the extension has not started): the provider asks the panes when it
// registers.
func (p *multiDiffPane) load() {
	in := p.input
	if in.Fixed() || p.disposed {
		return
	}
	reg := p.s.win.multiDiffProvider(in.Kind())
	if reg == nil {
		p.model.Refresh()
		return
	}
	if p.loadCancel != nil {
		p.loadCancel()
	}
	ctx, cancel := context.WithCancel(reg.ctx)
	p.loadCancel = cancel
	p.loadGen++
	gen, key := p.loadGen, in.Key()
	binding := widgets.WidgetsBindingOf(p.s.Context())
	p.s.Go(func(life context.Context) {
		stop := context.AfterFunc(life, cancel)
		defer stop()
		files, err := reg.p.Resources(ctx, key)
		binding.Post(func() { p.loaded(gen, files, err) })
	})
	p.model.Refresh()
}

// loaded is the answer of the provider.
func (p *multiDiffPane) loaded(gen int, files []extension.MultiDiffResource, err error) {
	if p.disposed || gen != p.loadGen {
		return
	}
	win := p.s.win
	if err != nil {
		switch {
		case errors.Is(err, context.Canceled):
		case errors.Is(err, &errors.Error{Tag: extension.TagMultiDiffGone}):
			// the set is gone (its repository was closed): the tab goes without a word
			if g := p.s.groupHolding(p.input); g != nil {
				p.s.closeEditors(g, []editorgroup.EditorInput{p.input})
			}
		default:
			win.Process.Logger.Warn("a multi-file diff provider cannot make the files", "kind", p.input.Kind(), "err", describe(err))
			if !p.input.Loaded() {
				p.input.SetFailure(describe(err))
				p.model.Refresh()
			}
		}
		return
	}
	items := make([]editorgroup.MultiDiffItem, len(files))
	for i, f := range files {
		items[i] = multiDiffItem(f)
	}
	p.input.SetItems(items)
	p.applyItems()
	p.revealNow()
	p.s.refreshKeys()
}

// measure is the height a body measured: the layout takes it, and a Go to Next Change that waited for the section goes on.
func (p *multiDiffPane) measure(key string, h float64) {
	i, ok := p.index[key]
	if !ok || p.disposed {
		return
	}
	p.model.SetBodyHeight(i, h)
	if p.nav.pending && p.nav.section == i {
		p.nav.pending = false
		p.step(i, p.nav.next, p.nav.change)
	}
}

// actions are the buttons of the header of section i: Open File, and the actions of the file's menu (the commands the extension
// contributed to MultiDiffEditorFileToolbar, with the context keys and the arguments of the file).
func (p *multiDiffPane) actions(bc widgets.BuildContext, i int) []multidiff.Action {
	items := p.input.Items()
	if i >= len(items) {
		return nil
	}
	item := items[i]
	win := PeekWorkspace(bc)
	var out []multidiff.Action
	if item.File != "" {
		out = append(out, multidiff.Action{Icon: fonts.CodiconGoToFile, Tooltip: "Open File", Run: func() { p.openFile(item) }})
	}
	ctx := menuContext{base: win.Keys, settings: win.Settings, extra: item.Context}
	for _, e := range win.Menus.Items(command.MultiDiffEditorFileToolbar, ctx) {
		code, ok := fonts.CodiconByName(e.Icon)
		if e.Separator || e.Command == "" || e.Disabled || !ok {
			continue
		}
		id, args := e.Command, item.Args
		out = append(out, multidiff.Action{Icon: code, Tooltip: e.Title, Run: func() { p.s.runTitleAction(win, id, args) }})
	}
	return out
}

// openFile opens the file of an item in the editor, at its first change when its diff is there: a kept tab, as Open File of the header
// of VS Code's multi-file editor opens it.
func (p *multiDiffPane) openFile(item editorgroup.MultiDiffItem) {
	file, ok := strings.CutPrefix(item.File, "file://")
	if !ok || file == "" {
		return
	}
	line := 0
	if b := p.bodies[item.Key()]; b != nil && b.dp != nil {
		if ch := b.dp.model.Changes(); len(ch) > 0 {
			line = ch[0].Modified.StartLine
		}
	}
	p.s.openPathAt(widgets.WidgetsBindingOf(p.s.Context()), file, line, 1, openOptions{})
}

// allCollapsed says that the editor has files and every one is collapsed (the context key multiDiffEditorAllCollapsed).
func (p *multiDiffPane) allCollapsed() bool { return p.model.AllCollapsed() }

// goToChange moves to the next or the previous change, through the files: the changes of the section it is in, then those of the next
// one that has any, wrapping around. A section that is not built yet is scrolled to, and the move goes on when its diff is computed.
//
// Ported from VS Code: src/vs/editor/browser/widget/multiDiffEditor/multiDiffEditorWidgetImpl.ts (goToNextChange, goToPreviousChange)
func (p *multiDiffPane) goToChange(next bool) {
	m := p.model
	if m.Len() == 0 {
		return
	}
	at, change := p.nav.section, p.nav.change
	if focused := p.focusedSection(); focused >= 0 {
		at, change = focused, p.changeAtCursor(focused, next)
	} else if at < 0 || at >= m.Len() {
		at, change = m.Current(), edgeBefore(next)
	}
	p.step(at, next, change)
}

// edgeBefore is the change the move starts after in a section it enters: before the first when going forward, after the last when going
// back.
func edgeBefore(next bool) int {
	if next {
		return -1
	}
	return math.MaxInt
}

// focusedSection is the section whose editor has the keyboard, -1 when none has.
func (p *multiDiffPane) focusedSection() int {
	for key, b := range p.bodies {
		if b.dp == nil {
			continue
		}
		for _, side := range b.dp.sides {
			if p.s.group.focused[side] {
				return p.index[key]
			}
		}
	}
	return -1
}

// changeAtCursor is the change of a section the cursor of its modified editor is at or before, so that the move goes on from it.
func (p *multiDiffPane) changeAtCursor(section int, next bool) int {
	b := p.bodies[p.model.Section(section).Key]
	if b == nil || b.dp == nil {
		return edgeBefore(next)
	}
	line := b.dp.sides[1].view.Cursors().Primary().Position().Line + 1
	ch := b.dp.model.Changes()
	if next {
		last := -1
		for i, c := range ch {
			if c.Modified.StartLine <= line {
				last = i
			}
		}
		return last
	}
	first := math.MaxInt
	for i := len(ch) - 1; i >= 0; i-- {
		if c := ch[i]; c.Modified.StartLine >= line {
			first = i
		}
	}
	return first
}

// step finds the change after (or before) change `from` of section i, scrolling to it; a section with none passes the move on.
func (p *multiDiffPane) step(i int, next bool, from int) {
	m := p.model
	for range m.Len() + 1 {
		i = (i + m.Len()) % m.Len()
		if m.Collapsed(i) {
			i, from = i+dir(next), edgeBefore(next)
			continue
		}
		b := p.bodies[m.Section(i).Key]
		if b == nil || !b.ready() {
			// the section is not near the viewport, or its diff is not computed: go there and wait for the diff
			p.nav = multiDiffNav{section: i, change: from, pending: true, next: next}
			m.ScrollToSection(i)
			return
		}
		ch := b.dp.model.Changes()
		idx := min(from, len(ch)) - 1
		if next {
			idx = from + 1
		}
		if idx >= 0 && idx < len(ch) {
			p.nav = multiDiffNav{section: i, change: idx}
			p.scrollToChange(i, b, ch[idx].Modified.StartLine-1)
			return
		}
		i, from = i+dir(next), edgeBefore(next)
	}
}

func dir(next bool) int {
	if next {
		return 1
	}
	return -1
}

// scrollToChange puts the line of a section's modified side in view.
func (p *multiDiffPane) scrollToChange(section int, b *sectionBody, line int) {
	view := b.dp.sides[1].view
	proj := view.Projection()
	line = min(max(line, 0), view.Document().Snapshot().LineCount()-1)
	first, _ := proj.LineRows(line)
	p.model.ScrollToOffset(section, proj.RowTop(first))
}

// multiDiffCommands are the commands of the multi-file diff editor: they act on the editor of the active group.
func (s *shellState) multiDiffCommands() map[string]command.Handler {
	on := func(f func(p *multiDiffPane)) command.Handler {
		return func(command.Context, any) error {
			if p, ok := s.group.panes[s.activeGroup().Model.ActiveEditor()].(*multiDiffPane); ok {
				f(p)
			}
			return nil
		}
	}
	collapse := func(collapsed bool) command.Handler {
		return on(func(p *multiDiffPane) {
			if collapsed {
				p.model.CollapseAll()
			} else {
				p.model.ExpandAll()
			}
			s.refreshKeys()
		})
	}
	return map[string]command.Handler{
		"multiDiffEditor.goToNextChange":     on(func(p *multiDiffPane) { p.goToChange(true) }),
		"multiDiffEditor.goToPreviousChange": on(func(p *multiDiffPane) { p.goToChange(false) }),
		"multiDiffEditor.collapseAll":        collapse(true),
		"multiDiffEditor.expandAll":          collapse(false),
	}
}

// multiDiffAllCollapsed is the value of the context key multiDiffEditorAllCollapsed: the editor of the active group has files and every
// one is collapsed.
func (s *shellState) multiDiffAllCollapsed() bool {
	if s.group.grid == nil {
		return false
	}
	p, ok := s.group.panes[s.activeGroup().Model.ActiveEditor()].(*multiDiffPane)
	return ok && p.allCollapsed()
}

// ---- the body of a file ----

// sectionBodyWidget is the body of the section with a key; its state makes the body when the section comes near the viewport and ends it
// when it leaves.
type sectionBodyWidget struct {
	widgets.StatefulWidget
	p   *multiDiffPane
	key string
}

// CreateState implements widgets.StatefulWidget.
func (w *sectionBodyWidget) CreateState() widgets.State { return &sectionBodyState{} }

type sectionBodyState struct {
	widgets.StateOf[*sectionBodyWidget]
	b *sectionBody
}

// InitState makes the body and starts reading the documents of the file.
func (s *sectionBodyState) InitState() {
	w := s.Widget()
	i, ok := w.p.index[w.key]
	var item editorgroup.MultiDiffItem
	if ok {
		item = w.p.input.Items()[i]
	}
	s.b = &sectionBody{p: w.p, key: w.key, item: item, update: func() { s.SetState(func() {}) }}
	w.p.bodies[w.key] = s.b
	s.b.start()
}

// Dispose ends the body.
func (s *sectionBodyState) Dispose() {
	s.b.dispose()
	if s.b.p.bodies[s.b.key] == s.b {
		delete(s.b.p.bodies, s.b.key)
	}
}

// Build implements widgets.State.
func (s *sectionBodyState) Build(bc widgets.BuildContext) widgets.Widget { return s.b.build(bc) }

// emptySides number the documents that stand for a side that does not exist; their URIs start with emptySidePrefix.
var emptySides atomic.Int64

const emptySidePrefix = "/multidiff/empty/"

// isEmptySide tells whether a side stands for a file that does not exist on that side of the change.
func isEmptySide(sd *side) bool {
	return sd.uri.Scheme == document.SchemeVirtual && strings.HasPrefix(sd.uri.Path, emptySidePrefix)
}

// sectionBody is the diff editor of one file: it reads the two documents off the UI thread, makes the diff editor over them (read-only,
// the whole text) and reports the height of its content to the pane.
type sectionBody struct {
	p    *multiDiffPane
	key  string
	item editorgroup.MultiDiffItem
	// dp is the diff editor, nil until the documents are there and for a file that cannot be shown; notice is the text shown then.
	dp     *diffPane
	notice string
	cancel context.CancelFunc
	// update makes the widget build again; queued says that a report waits for the UI thread.
	update   func()
	queued   bool
	disposed bool
	stops    []func()
}

// sectionOptions are the options of the editors of a section: the diff editor's, read-only, and with no room to scroll past the last
// line (the section is as tall as its text, and the wheel must reach the multi-file editor).
func sectionOptions(o editor.Options, side diffeditor.Side) editor.Options {
	o = diffeditor.AdjustOptions(o, side)
	o.ReadOnly = true
	o.ScrollBeyondLastLine = false
	return o
}

// ready says that the diff of the body is computed, so that its height is the height of its content.
func (b *sectionBody) ready() bool { return b.dp != nil && b.dp.model.Computed() }

// start reads the documents of the two sides in the background.
func (b *sectionBody) start() {
	s, w := b.p.s, b.p.s.win
	docs := extensionDocuments{w}
	var sides [2]*side
	for i, raw := range [2]string{b.item.Original, b.item.Modified} {
		if raw == "" {
			sides[i] = &side{uri: document.URI{Scheme: document.SchemeVirtual, Path: emptySidePrefix + strconv.FormatInt(emptySides.Add(1), 10)}}
			continue
		}
		sd, err := docs.parseSide(raw)
		if err != nil {
			w.releaseSides(sides[:i]...)
			b.fail(err, false)
			return
		}
		sides[i] = sd
	}
	binding := widgets.WidgetsBindingOf(s.Context())
	ctx, cancel := context.WithCancel(w.ctx)
	b.cancel = cancel
	s.Go(func(life context.Context) {
		stop := context.AfterFunc(life, cancel)
		defer stop()
		var failed error
		for _, sd := range sides {
			if isEmptySide(sd) {
				continue
			}
			if w.prepare(ctx, sd); sd.err != nil {
				failed = sd.err
				break
			}
		}
		binding.Post(func() { b.made(sides, failed) })
	})
}

// fail shows why the file has no diff; build is about to run when the body is not started yet (notify false).
func (b *sectionBody) fail(err error, notify bool) {
	name := path.Base(b.item.Path)
	if isBinaryError(err) || errors.Is(err, &errors.Error{Tag: extension.TagContentBinary}) {
		b.notice = BinaryFileNotice
	} else {
		b.notice = "Cannot show " + name + ": " + describe(err)
	}
	b.queueReport()
	if notify && !b.disposed {
		b.update()
	}
}

// made is called on the UI thread when the documents were read: the diff editor is made over them.
func (b *sectionBody) made(sides [2]*side, failed error) {
	s, w := b.p.s, b.p.s.win
	if b.disposed || !s.Mounted() {
		w.releaseSides(sides[:]...)
		return
	}
	if failed != nil {
		w.releaseSides(sides[:]...)
		b.fail(failed, true)
		return
	}
	// the side that does not exist is an empty text in the language of the other
	lang := document.DefaultLanguage
	for _, sd := range sides {
		switch {
		case sd.doc != nil:
			lang = sd.doc.LanguageID()
		case sd.content.Language != "":
			lang = sd.content.Language
		}
	}
	for _, sd := range sides {
		if isEmptySide(sd) {
			sd.content = document.VirtualContent{Label: path.Base(b.item.Path), Language: lang}
		}
		w.finish(sd)
	}
	input := editorgroup.NewDiffInput(sides[0].doc, sides[1].doc, s.inputOptions())
	b.dp = newDiffPaneWith(s, input, sectionOptions)
	w.releaseSides(sides[:]...)
	b.stops = append(b.stops, removeListener(b.dp.model.AddListener(b.queueReport), b.dp.model.RemoveListener))
	for _, ed := range b.dp.sides {
		view := ed.view
		b.stops = append(b.stops, removeListener(view.AddMetricsListener(b.queueReport), view.RemoveMetricsListener))
	}
	b.update()
	b.queueReport()
}

// removeListener makes the function that takes a listener away.
func removeListener(h foundation.ListenerHandle, remove func(foundation.ListenerHandle)) func() {
	return func() { remove(h) }
}

// queueReport reports the height of the content on the UI thread, once for the changes that come together.
func (b *sectionBody) queueReport() {
	if b.queued || b.disposed {
		return
	}
	b.queued = true
	widgets.WidgetsBindingOf(b.p.s.Context()).Post(func() {
		b.queued = false
		if !b.disposed {
			b.report()
		}
	})
}

// report tells the pane the height of the content of the body.
func (b *sectionBody) report() {
	switch {
	case b.dp != nil:
		if b.dp.model.Computed() {
			b.p.measure(b.key, contentHeight(b.dp))
		}
	case b.notice != "":
		b.p.measure(b.key, multiDiffNoticeHeight)
	}
}

// contentHeight is the height the diff editor needs to show its whole text: the modified editor alone in the inline view, and the
// taller of the two, which the filler zones make the same height, side by side.
func contentHeight(dp *diffPane) float64 {
	mod := dp.sides[1].view.Projection().Height()
	if dp.model.Mode() == diffeditor.Inline {
		return mod
	}
	return max(mod, dp.sides[0].view.Projection().Height())
}

// dispose ends the body: the diff editor and its editors, and the documents it counted a use of.
func (b *sectionBody) dispose() {
	if b.disposed {
		return
	}
	b.disposed = true
	if b.cancel != nil {
		b.cancel()
	}
	for _, stop := range b.stops {
		stop()
	}
	b.stops = nil
	if b.dp != nil {
		b.dp.dispose()
		b.dp.input.Dispose()
		b.dp = nil
	}
}

// build is the widget of the body.
func (b *sectionBody) build(bc widgets.BuildContext) widgets.Widget {
	if b.dp != nil {
		return b.dp.editorWidget(bc, b.p.group, false)
	}
	colors := kit.ThemeOf(bc)
	text := "Loading..."
	if b.notice != "" {
		text = b.notice
	}
	style := kit.TextStyleOf(bc, 12, colors.EditorForeground.WithOpacity(0.7))
	return widgets.NewColoredBox(colors.EditorBackground, widgets.NewCenter(widgets.NewPadding(painting.NewEdgeInsetsAll(8),
		widgets.NewText(text, widgets.TextOpts{Style: style, TextAlign: ui.TextAlignCenter, MaxLines: foundation.Some(2), Overflow: painting.TextOverflowEllipsis}))))
}
