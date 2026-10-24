package app

import (
	"context"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/fileio"
	"github.com/rm4n0s/emetos/internal/core/language"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/quickinput"
	"github.com/rm4n0s/emetos/internal/gui/workbench"
)

// The pickers of the items of the status bar: the indentation, the encoding, the end of line sequence and the
// language mode. Each is a command (the click on the item runs it), so the palette and the keys reach it too.
//
// Ported from VS Code: src/vs/workbench/browser/parts/editor/editorStatus.ts

// Texts of the pickers that tell there is nothing to pick for.
const (
	noEditorText   = "No text editor active at this time"
	readOnlyText   = "The active code editor is read-only."
	noFileText     = "No file active at this time"
	encodingGuess  = "Guessed from content"
	languagesTitle = "languages (identifier)"
)

// guessLimit is the largest file read to guess its encoding for the encoding picker; a larger file shows no
// guess.
const guessLimit = 1 << 20

// bindStatusCommands binds the commands of the pickers while the shell is on screen.
func (s *shellState) bindStatusCommands(binding *widgets.WidgetsBinding) {
	win := PeekWindow(s.Context())
	handlers := map[string]command.Handler{
		"changeEditorIndentation":                func(command.Context, any) error { s.showIndentationPicker(); return nil },
		"workbench.action.editor.changeEOL":      func(command.Context, any) error { s.showEOLPicker(binding); return nil },
		"workbench.action.editor.changeEncoding": func(command.Context, any) error { s.showEncodingPicker(binding); return nil },
		"workbench.action.editor.changeLanguageMode": func(_ command.Context, args any) error {
			s.showLanguagePicker(binding, args)
			return nil
		},
	}
	for id, h := range handlers {
		remove, err := win.Commands.SetHandler(id, h)
		if err != nil {
			// the commands are registered by the program itself: a missing one is a broken build
			panic(err)
		}
		s.status.removeCommands = append(s.status.removeCommands, remove)
	}
}

// pickInfo shows a picker with a single line that tells why there is nothing to do; choosing it closes it.
func (s *shellState) pickInfo(text string) {
	PeekWindow(s.Context()).QuickInput.Pick(quickinput.PickOptions{
		Items: []quickinput.Item{{Label: text, Accept: func(quickinput.KeyMods) {}}},
	})
}

// ---- indentation ----

// showIndentationPicker is the action list of the indentation item: the commands that change how the file is
// indented and how it is shown, and those that convert it. Choosing one gives the editor the focus and runs it.
func (s *shellState) showIndentationPicker() {
	e := s.activeEditor()
	if e == nil {
		s.pickInfo(noEditorText)
		return
	}
	if e.doc.ReadOnly() {
		s.pickInfo(readOnlyText)
		return
	}
	win := PeekWindow(s.Context())
	action := func(id string) quickinput.Item {
		meta, _ := win.Commands.Lookup(id)
		return quickinput.Item{
			ID: id, Label: meta.Title,
			Accept: func(quickinput.KeyMods) {
				e.focus.Request()
				if err := e.view.RunCommand(id, nil); err != nil {
					win.Notify.Error("Emetos", describe(err))
				}
				s.refreshStatus()
			},
		}
	}
	items := []quickinput.Item{
		{Separator: true, Label: "change view"},
		action("editor.action.indentUsingSpaces"),
		action("editor.action.indentUsingTabs"),
		action("editor.action.changeTabDisplaySize"),
		action("editor.action.detectIndentation"),
		{Separator: true, Label: "convert file"},
		action("editor.action.indentationToSpaces"),
		action("editor.action.indentationToTabs"),
		action("editor.action.trimTrailingWhitespace"),
	}
	win.QuickInput.Pick(quickinput.PickOptions{Placeholder: "Select Action", Items: items, MatchOnDetail: true})
}

// ---- end of line sequence ----

// showEOLPicker lets the user choose LF or CRLF for the active editor; the current one is active. The change is
// an edit of its own that can be undone.
func (s *shellState) showEOLPicker(binding *widgets.WidgetsBinding) {
	e := s.activeEditor()
	if e == nil {
		s.pickInfo(noEditorText)
		return
	}
	if e.doc.ReadOnly() {
		s.pickInfo(readOnlyText)
		return
	}
	win := PeekWindow(s.Context())
	option := func(le fileio.LineEnding) quickinput.Item {
		return quickinput.Item{
			ID: workbench.EOLLabel(le), Label: workbench.EOLLabel(le),
			Accept: func(quickinput.KeyMods) {
				if !e.disposed && !e.doc.ReadOnly() {
					e.doc.PushStackElement()
					if err := e.doc.SetLineEnding(le); err != nil {
						win.Notify.Error("Emetos", describe(err))
					}
					e.doc.PushStackElement()
					e.view.DocumentChanged()
				}
				s.refreshStatus()
				s.askFocus(binding)
			},
		}
	}
	win.QuickInput.Pick(quickinput.PickOptions{
		Placeholder: "Select End of Line Sequence",
		Items:       []quickinput.Item{option(fileio.LF), option(fileio.CRLF)},
		ActiveID:    workbench.EOLLabel(e.doc.LineEnding()),
	})
}

// ---- encoding ----

// showEncodingPicker is the action list of the encoding item: "Reopen with Encoding" for a file, and "Save with
// Encoding" for a document that can be saved (an untitled one has no file to reopen). A read-only file goes
// straight to the encodings to reopen it with.
func (s *shellState) showEncodingPicker(binding *widgets.WidgetsBinding) {
	e := s.activeEditor()
	if e == nil {
		s.pickInfo(noEditorText)
		return
	}
	scheme := e.doc.URI().Scheme
	isFile := scheme == document.SchemeFile
	if !isFile && scheme != document.SchemeUntitled {
		s.pickInfo(noFileText)
		return
	}
	reopen := func() { s.guessEncoding(binding, e) }
	if e.doc.ReadOnly() {
		if isFile {
			reopen()
		} else {
			s.pickInfo(readOnlyText)
		}
		return
	}
	var items []quickinput.Item
	if isFile {
		items = append(items, quickinput.Item{ID: "reopen", Label: "Reopen with Encoding", Accept: func(quickinput.KeyMods) { reopen() }})
	}
	items = append(items, quickinput.Item{ID: "save", Label: "Save with Encoding",
		Accept: func(quickinput.KeyMods) { s.showSaveEncodingList(binding, e, "") }})
	PeekWindow(s.Context()).QuickInput.Pick(quickinput.PickOptions{Placeholder: "Select Action", MatchOnDetail: true, Items: items})
}

// guessEncoding finds the encoding the file looks to be in (a byte order mark, or valid UTF-8) on a goroutine
// and shows the list of encodings to reopen it with.
func (s *shellState) guessEncoding(binding *widgets.WidgetsBinding, e *textEditor) {
	win := PeekWindow(s.Context())
	file := e.doc.URI().Path
	configured := settings.Get[string](win.Settings, "files.encoding", e.doc.LanguageID())
	s.Go(func(life context.Context) {
		guessed := ""
		res, err := fileio.Read(life, file, fileio.ReadOptions{
			Encoding: configured, AutoGuess: true, AllowBinary: true, Limits: fileio.Limits{MaxFileSize: guessLimit},
		})
		if err == nil {
			guessed = res.Encoding
		}
		binding.Post(func() {
			if s.Mounted() && !e.disposed {
				s.showEncodingList(binding, e, configured, guessed)
			}
		})
	})
}

// encodingPicks are the encodings to choose from: the configured one first and then the list of VS Code in its
// order, without the guessed encoding (it is offered at the top) and, to reopen a file, without the ones that can
// only be saved in.
func encodingPicks(configured, guessed string, reopen bool, accept func(fileio.Encoding)) []quickinput.Item {
	all := fileio.Encodings()
	sort.SliceStable(all, func(i, j int) bool {
		switch {
		case all[i].ID == configured:
			return all[j].ID != configured
		case all[j].ID == configured:
			return false
		}
		return all[i].Order < all[j].Order
	})
	var items []quickinput.Item
	for _, enc := range all {
		if enc.ID == guessed && guessed != configured {
			continue
		}
		if reopen && enc.EncodeOnly {
			continue
		}
		items = append(items, quickinput.Item{
			ID: enc.ID, Label: enc.LabelLong, Description: enc.ID,
			Accept: func(quickinput.KeyMods) { accept(enc) },
		})
	}
	if g, ok := fileio.LookupEncoding(guessed); ok && guessed != configured && (!reopen || !g.EncodeOnly) {
		items = append([]quickinput.Item{
			{ID: g.ID, Label: g.LabelLong, Description: encodingGuess, Accept: func(quickinput.KeyMods) { accept(g) }},
			{Separator: true},
		}, items...)
	}
	return items
}

// showEncodingList shows the encodings to reopen the file with; the one the file has is active.
func (s *shellState) showEncodingList(binding *widgets.WidgetsBinding, e *textEditor, configured, guessed string) {
	win := PeekWindow(s.Context())
	win.QuickInput.Pick(quickinput.PickOptions{
		Placeholder: "Select File Encoding to Reopen File",
		Items:       encodingPicks(configured, guessed, true, func(enc fileio.Encoding) { s.reopenWithEncoding(binding, e, enc) }),
		ActiveID:    e.doc.Encoding(),
	})
}

// reopenWithEncoding reads the file of the editor again, decoded as the encoding says, in place of its text. A
// document with unsaved changes asks first whether to discard them, as VS Code does.
func (s *shellState) reopenWithEncoding(binding *widgets.WidgetsBinding, e *textEditor, enc fileio.Encoding) {
	if e.doc.Dirty() {
		s.confirm(confirmSpec{
			Type:    kit.DialogQuestion,
			Message: "Do you want to revert the active text editor and reopen with a different encoding?",
			Detail:  "This will discard any unsaved changes.",
			Primary: "Discard Changes and Reopen",
		}, func(confirmed, _ bool) {
			if confirmed && s.Mounted() && !e.disposed {
				s.readWithEncoding(binding, e, enc)
			}
		})
		return
	}
	s.readWithEncoding(binding, e, enc)
}

// readWithEncoding does the read of reopenWithEncoding on a goroutine and applies it on the UI thread.
func (s *shellState) readWithEncoding(binding *widgets.WidgetsBinding, e *textEditor, enc fileio.Encoding) {
	win := PeekWindow(s.Context())
	file := e.doc.URI().Path
	s.Go(func(life context.Context) {
		res, err := fileio.Read(life, file, fileio.ReadOptions{Encoding: enc.ID, ForceEncoding: true, AllowBinary: true})
		binding.Post(func() {
			if !s.Mounted() || e.disposed {
				return
			}
			if err != nil {
				win.Notify.Error("Emetos", "Cannot reopen "+filepath.Base(file)+" with the encoding "+enc.LabelLong+": "+describe(err))
				return
			}
			views := s.captureViews(e.doc)
			e.doc.Reload(res)
			s.documentReloaded(binding, e.doc, views, true)
		})
	})
}

// ---- language mode ----

// languagePick is a language of the registry as the pickers list it.
type languagePick struct{ id, name string }

// sortedLanguages are the languages in the order of their names, without regard to case.
func (w *Window) sortedLanguages() []languagePick {
	var out []languagePick
	for _, l := range w.Process.Languages.Languages() {
		name := l.Name
		if name == "" {
			name = l.ID
		}
		out = append(out, languagePick{id: l.ID, name: name})
	}
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].name) < strings.ToLower(out[j].name) })
	return out
}

// firstLineOf is the first line of a document.
func firstLineOf(doc *document.Document) string {
	line, _ := doc.Snapshot().LineContent(0)
	return line
}

// showLanguagePicker lets the user choose the language of the active editor (Change Language Mode): Auto Detect
// when the document has text, the association of its file extension, and the languages by name with their ids,
// the current one marked. The command takes the name or the id of a language as its argument and then changes the
// language at once.
//
// Not built: "Configure 'x' language based settings...", which opens the settings file of the language, and the
// marketplace entry.
func (s *shellState) showLanguagePicker(binding *widgets.WidgetsBinding, args any) {
	e := s.activeEditor()
	if e == nil {
		s.pickInfo(noEditorText)
		return
	}
	win := PeekWindow(s.Context())
	doc := e.doc
	if name, ok := args.(string); ok {
		if id, found := win.Process.Languages.ByName(name); found {
			s.setLanguage(binding, e, id)
		} else {
			win.Notify.Error("Emetos", "There is no language named "+name+".")
		}
		return
	}
	uri := doc.URI()
	hasSupport := uri.Scheme == document.SchemeFile
	currentID := doc.LanguageID()

	var items []quickinput.Item
	if doc.Snapshot().Len() > 0 {
		items = append(items, quickinput.Item{ID: "autoDetect", Label: "Auto Detect", Accept: func(quickinput.KeyMods) {
			s.setLanguage(binding, e, win.Process.Languages.Detect(uri.Path, firstLineOf(doc)))
		}})
	}
	if hasSupport {
		ext := filepath.Ext(uri.Path)
		if ext == "" {
			ext = filepath.Base(uri.Path)
		}
		items = append(items, quickinput.Item{ID: "configureAssociation", Label: "Configure File Association for '" + ext + "'...",
			Accept: func(quickinput.KeyMods) { s.pickAssociation(binding, e) }})
	}
	items = append(items, quickinput.Item{Separator: true, Label: languagesTitle})
	for _, l := range win.sortedLanguages() {
		description := "(" + l.id + ")"
		if l.id == currentID {
			description = "(" + l.id + ") - Configured Language"
		}
		items = append(items, quickinput.Item{
			ID: l.id, Label: l.name, Description: description,
			Accept: func(quickinput.KeyMods) { s.setLanguage(binding, e, l.id) },
		})
	}
	win.QuickInput.Pick(quickinput.PickOptions{Placeholder: "Select Language Mode", Items: items, MatchOnDescription: true})
}

// setLanguage gives the document of an editor a language and makes every editor of it follow, then gives the
// focus back to the editor.
func (s *shellState) setLanguage(binding *widgets.WidgetsBinding, e *textEditor, id string) {
	if e.disposed {
		return
	}
	if e.doc.LanguageID() != id {
		e.doc.SetLanguageID(id)
		for _, other := range s.group.editors {
			if other.doc == e.doc {
				s.languageChanged(binding, other)
			}
		}
	}
	s.refreshStatus()
	s.askFocus(binding)
}

// currentAssociation is the language the registry gives to a file name alone, for "Current Association".
func currentAssociation(langs *language.Registry, base string) string {
	return langs.Detect(base, "")
}

// pickAssociation shows the languages to associate the extension of the file with and writes the choice to
// files.associations: as "*.ext" for a name of the form name.ext, and as the whole name for a name such as
// .gitignore or Dockerfile. The setting is written to the workspace settings when the association is made there
// already, else to the user settings; the documents open take the language their name gives now.
func (s *shellState) pickAssociation(binding *widgets.WidgetsBinding, e *textEditor) {
	win := PeekWindow(s.Context())
	file := e.doc.URI().Path
	base, ext := path.Base(file), path.Ext(file)
	current := currentAssociation(win.Process.Languages, base)
	var items []quickinput.Item
	for _, l := range win.sortedLanguages() {
		description := ""
		if l.id == current {
			description = "Current Association"
		}
		items = append(items, quickinput.Item{
			ID: l.id, Label: l.name, Description: description,
			Accept: func(quickinput.KeyMods) { s.writeAssociation(binding, e, base, ext, l.id) },
		})
	}
	label := ext
	if label == "" {
		label = base
	}
	win.QuickInput.Pick(quickinput.PickOptions{Placeholder: "Select Language Mode to Associate with '" + label + "'", Items: items})
}

// writeAssociation stores the association of a file name with a language.
func (s *shellState) writeAssociation(binding *widgets.WidgetsBinding, e *textEditor, base, ext, languageID string) {
	win := PeekWindow(s.Context())
	key := base
	if ext != "" && base[0] != '.' {
		key = "*" + ext
	}
	target := settings.TargetUser
	in := win.Settings.Inspect("files.associations", "")
	if m, ok := in.Workspace.Value.(map[string]any); ok && in.Workspace.Ok {
		if _, has := m[key]; has {
			target = settings.TargetWorkspace
		}
	}
	// the documents whose language is the one their name gives follow the association
	var follow []*document.Document
	for _, d := range win.Documents.All() {
		if u := d.URI(); u.Scheme == document.SchemeFile && d.LanguageID() == win.Process.Languages.Detect(u.Path, firstLineOf(d)) {
			follow = append(follow, d)
		}
	}
	// the value to write is the one of the target alone, not the merged one
	merged := map[string]any{}
	layer := in.User
	if target == settings.TargetWorkspace {
		layer = in.Workspace
	}
	if m, ok := layer.Value.(map[string]any); ok && layer.Ok {
		for k, v := range m {
			merged[k] = v
		}
	}
	merged[key] = languageID
	if err := win.Settings.Update(win.ctx, "files.associations", merged, target); err != nil {
		win.Process.Logger.Warn("cannot write the file association", "key", key, "err", err)
		win.Notify.Error("Emetos", "Cannot save the file association "+key+": "+describe(err))
		return
	}
	for _, d := range follow {
		u := d.URI()
		if lang := win.Process.Languages.Detect(u.Path, firstLineOf(d)); lang != d.LanguageID() {
			d.SetLanguageID(lang)
			for _, other := range s.group.editors {
				if other.doc == d {
					s.languageChanged(binding, other)
				}
			}
		}
	}
	s.refreshStatus()
	s.askFocus(binding)
}
