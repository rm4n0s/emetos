package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/notify"
	"github.com/rm4n0s/emetos/internal/core/storage"
)

// What the user wrote in the removed Notes and Scratchpad extensions is written to Markdown files, once (SPEC §10.26, D28).
//
// The two extensions are deleted (step 17e.1), and the text of a note and of a Scratchpad tab lived in the UI state that Emetos
// saved, not in files of their own. The first start after the removal reads that state, writes each text to
// $XDG_DATA_HOME/emetos/saved-notes/, leaves a marker file so that the next start does nothing, and tells the user where the files
// are. The saved state itself is never changed or deleted. The formats read here are those the removed extensions wrote (see
// extensions/notes and extensions/scratchpad in git, before step 17e.1); they are copied here because nothing else reads them.

const (
	// savedNotesFolder is the folder of the files, below the Emetos folder of $XDG_DATA_HOME, and savedNotesMarker the file in it
	// that says the export was made.
	savedNotesFolder = "saved-notes"
	savedNotesMarker = ".exported"
	// notesListView is the id of the view of the Notes extension: its state is the list of the notes of the window.
	notesListView = "emetos.notes.list"
	// noteTabKind is the editor tab kind of the Notes extension and noteTabExtension its extension; the state of such a tab is in the
	// views of the window, under "editor:<tab id>".
	noteTabKind      = "emetos.notes.note"
	noteTabExtension = "emetos.notes"
	// scratchpadTabKind is the window tab kind of the Scratchpad extension.
	scratchpadTabKind = "emetos.scratchpad.tab"
	// untitledNote is the name of a note that has no title (the Notes extension called it so), and scratchpadName the name of a
	// Scratchpad tab that has no title of its own.
	untitledNote   = "Untitled Note"
	scratchpadName = "Scratchpad"
	// maxSavedNameBytes is the longest name of a file made from a title, without the extension and the number of a clash.
	maxSavedNameBytes = 150
)

// savedText is a text to write: the name of its file (without ".md") and what to write.
type savedText struct {
	name, text string
}

// savedNote is a note as the Notes extension saved it, in the state of its view and of its tabs.
type savedNote struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Text  string `json:"text"`
}

// savedNoteList is the state of the view of the Notes extension.
type savedNoteList struct {
	Notes []*savedNote `json:"notes"`
}

// savedNoteTab is the state of an editor tab of the Notes extension: the note it shows.
type savedNoteTab struct {
	Note *savedNote `json:"note"`
}

// savedScratchpad is the state of the window tab of the Scratchpad extension.
type savedScratchpad struct {
	Notes string `json:"notes"`
}

// savedNotes is the export that is read and not written yet.
type savedNotes struct {
	// folder is where the files go.
	folder string
	texts  []savedText
	log    *slog.Logger
}

// savedNotesFolderOf is the folder of the files below the data home.
func savedNotesFolderOf(paths storage.Paths) string {
	return filepath.Join(paths.DataHome, "emetos", savedNotesFolder)
}

// loadSavedNotes reads the saved state for the texts of the removed extensions. It is nil when the export was made before, when
// the state holds no text and when it cannot be read at all; a file of the state that cannot be read is one warning in the log and
// the others are still read. It reads the files on the calling goroutine, because it has to run before the windows are made (they
// write the state again at once, and the tab of a removed kind leaves the list of the tabs); only reading is done here, the files
// are written by savedNotes.export.
func (p *Process) loadSavedNotes(ctx context.Context) *savedNotes {
	folder := savedNotesFolderOf(p.Paths)
	if _, err := os.Lstat(filepath.Join(folder, savedNotesMarker)); err == nil {
		return nil
	}
	var texts []savedText
	for _, path := range savedStateFiles(p.Paths) {
		found, err := readSavedNotesFile(ctx, path)
		if err != nil {
			p.Logger.Warn("a saved state cannot be read for the saved notes; its notes are not saved", "err", err)
			continue
		}
		texts = append(texts, found...)
	}
	if len(texts) == 0 {
		return nil
	}
	return &savedNotes{folder: folder, texts: texts, log: p.Logger}
}

// savedStateFiles are the files of the saved state that may hold the text, in a fixed order: for the default profile and for
// each profile, the global state and the state of each window.
func savedStateFiles(paths storage.Paths) []string {
	root := filepath.Join(paths.StateHome, "emetos")
	dirs := []string{root}
	if entries, err := os.ReadDir(filepath.Join(root, "profiles")); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				dirs = append(dirs, filepath.Join(root, "profiles", e.Name()))
			}
		}
	}
	var files []string
	for _, dir := range dirs {
		files = append(files, filepath.Join(dir, "state.json"))
		windows, err := filepath.Glob(filepath.Join(dir, "windows", "*.json"))
		if err != nil {
			continue // the pattern is fixed and valid; a folder name with a metacharacter only makes this profile have no windows
		}
		slices.Sort(windows)
		files = append(files, windows...)
	}
	return files
}

// readSavedNotesFile reads one file of the saved state and gives the texts of the removed extensions in it. A file that is not
// there, and one that mentions neither extension, give none. The error has the tag SavedNotesStateUnreadable.
func readSavedNotesFile(ctx context.Context, path string) ([]savedText, error) {
	if err := ctx.Err(); err != nil {
		return nil, errors.NewErr("SavedNotesStateUnreadable", err).SetMetadata("path", path)
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.NewErr("SavedNotesStateUnreadable", err).SetMetadata("path", path)
	}
	if !bytes.Contains(data, []byte(noteTabExtension)) && !bytes.Contains(data, []byte(scratchpadTabKind)) {
		return nil, nil
	}
	if filepath.Base(path) == "state.json" {
		var g storage.GlobalState
		if err := json.Unmarshal(data, &g); err != nil {
			return nil, errors.NewErr("SavedNotesStateUnreadable", err).SetMetadata("path", path)
		}
		return scratchpadTexts(g.WorkspaceTabs), nil
	}
	var w storage.WindowState
	if err := json.Unmarshal(data, &w); err != nil {
		return nil, errors.NewErr("SavedNotesStateUnreadable", err).SetMetadata("path", path)
	}
	return noteTexts(w), nil
}

// scratchpadTexts are the texts of the Scratchpad tabs in the list of the open tabs. A tab with no text gives none.
func scratchpadTexts(tabs []storage.WorkspaceTab) []savedText {
	var out []savedText
	for _, t := range tabs {
		if t.Kind != scratchpadTabKind {
			continue
		}
		var st savedScratchpad
		if len(t.State) == 0 || json.Unmarshal(t.State, &st) != nil || strings.TrimSpace(st.Notes) == "" {
			continue
		}
		name := scratchpadName
		if title := strings.TrimSpace(t.Title); title != "" && !strings.EqualFold(title, scratchpadName) {
			name += " " + title
		}
		out = append(out, savedText{name: name, text: st.Notes})
	}
	return out
}

// noteTexts are the notes of a window: those of the list of the view, then those that only a tab of the window remembers (the
// list of the view did not come back, and the tab kept its note). A note with neither title nor text gives none.
func noteTexts(w storage.WindowState) []savedText {
	var notes []*savedNote
	seen := map[string]bool{}
	add := func(n *savedNote) {
		if n == nil || n.ID == "" || seen[n.ID] {
			return
		}
		seen[n.ID] = true
		notes = append(notes, n)
	}
	var list savedNoteList
	if raw := w.Views[notesListView]; len(raw) > 0 && json.Unmarshal(raw, &list) == nil {
		for _, n := range list.Notes {
			add(n)
		}
	}
	for _, id := range noteTabIDs(w.Grid) {
		var tab savedNoteTab
		if raw := w.Views["editor:"+id]; len(raw) > 0 && json.Unmarshal(raw, &tab) == nil {
			add(tab.Note)
		}
	}
	var out []savedText
	for _, n := range notes {
		title := strings.TrimSpace(n.Title)
		if title == "" && strings.TrimSpace(n.Text) == "" {
			continue
		}
		if title == "" {
			title = untitledNote
		}
		out = append(out, savedText{name: title, text: n.Text})
	}
	return out
}

// noteTabIDs are the ids of the tabs of the Notes extension in the editors of a window, in the order of the grid.
func noteTabIDs(grid *editorgroup.SerializedGrid) []string {
	if grid == nil {
		return nil
	}
	var ids []string
	var walk func(nodes []editorgroup.SerializedNode)
	walk = func(nodes []editorgroup.SerializedNode) {
		for _, n := range nodes {
			walk(n.Groups)
			if n.Model == nil {
				continue
			}
			for _, in := range n.Model.Editors {
				if in.TypeID != editorgroup.ExtensionTabInputTypeID {
					continue
				}
				if tab, err := editorgroup.ParseExtensionTabInput(in.Value); err == nil && tab.Kind() == noteTabKind {
					ids = append(ids, tab.ID())
				}
			}
		}
	}
	walk(grid.Root)
	return ids
}

// fileNameOf makes a name for a file from a title: the characters a file name cannot hold become "-", the dots at the start are
// dropped (the file would be hidden) and a long title is cut at a character. An empty result is "Untitled".
func fileNameOf(title string) string {
	var b strings.Builder
	for _, r := range title {
		switch {
		case r == '/' || r == '\\' || r == utf8.RuneError || unicode.IsControl(r):
			b.WriteRune('-')
		default:
			b.WriteRune(r)
		}
	}
	name := strings.TrimLeft(strings.TrimSpace(b.String()), ".")
	for len(name) > maxSavedNameBytes {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	if name = strings.TrimSpace(name); name == "" {
		return "Untitled"
	}
	return name
}

// write makes the folder, writes the files and then the marker. A file that has the name of another already is given " 2", " 3"
// and so on. A file that is there already with the very text is taken as written by an earlier run (it stopped before the marker), unless this run wrote it. The errors have
// the tags SavedNotesFolderFailed, SavedNotesFileFailed and SavedNotesMarkerFailed; nothing is retried here, and with no marker the
// next start tries again.
func (s *savedNotes) write(ctx context.Context) error {
	if err := os.MkdirAll(s.folder, 0o700); err != nil {
		return errors.NewErr("SavedNotesFolderFailed", err).SetMetadata("folder", s.folder)
	}
	written := map[string]bool{} // the files of this run: another text never takes the place of one of them
	for _, t := range s.texts {
		if err := ctx.Err(); err != nil {
			return errors.NewErr("SavedNotesFileFailed", err).SetMetadata("folder", s.folder, "name", t.name)
		}
		base := fileNameOf(t.name)
		for n := 1; ; n++ {
			name := base + ".md"
			if n > 1 {
				name = base + " " + strconv.Itoa(n) + ".md"
			}
			path := filepath.Join(s.folder, name)
			f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if errors.Is(err, fs.ErrExist) {
				if existing, rerr := os.ReadFile(path); rerr == nil && string(existing) == t.text && !written[path] {
					break
				}
				continue
			}
			if err != nil {
				return errors.NewErr("SavedNotesFileFailed", err).SetMetadata("path", path)
			}
			_, err = f.WriteString(t.text)
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return errors.NewErr("SavedNotesFileFailed", err).SetMetadata("path", path)
			}
			written[path] = true
			break
		}
	}
	marker := filepath.Join(s.folder, savedNotesMarker)
	note := "The notes and the Scratchpad text of an earlier Emetos were saved in this folder, once. Delete this file and the next start saves them again.\n"
	if err := os.WriteFile(marker, []byte(note), 0o600); err != nil {
		return errors.NewErr("SavedNotesMarkerFailed", err).SetMetadata("path", marker)
	}
	return nil
}

// savedNotesMessage is what the notification says.
func savedNotesMessage(folder string) string {
	return "Your notes and Scratchpad text were saved as files in " + folder + "."
}

// export writes the files and tells the user. It does its I/O on the calling goroutine (call it off the UI thread); post hands
// work to the UI thread, which makes the notification in n, and open opens the folder in a new workspace tab (it runs on the UI
// thread, when the user presses Open Folder). A failure is logged and shown as an error notification; with no marker the next start
// tries again.
func (s *savedNotes) export(ctx context.Context, post func(func()), n *notify.Service, open func(folder string)) {
	err := s.write(ctx)
	if err != nil {
		s.log.Error("the notes and the Scratchpad text cannot be saved as files", "err", err)
		post(func() {
			n.Error("Emetos", "Your notes and Scratchpad text could not be saved as files in "+s.folder+": "+describe(err))
		})
		return
	}
	s.log.Info("the notes and the Scratchpad text were saved as files", "folder", s.folder, "files", len(s.texts))
	post(func() {
		n.Info("Emetos", savedNotesMessage(s.folder), notify.Action{Label: "Open Folder", Run: func() { open(s.folder) }})
	})
}

// savedNotesRun is an export that waits for the application to be bound, to be written off the UI thread (Workspaces.bind).
type savedNotesRun struct {
	texts *savedNotes
	// notify is the service of the window that is shown first: the notification comes there.
	notify *notify.Service
}

// startSavedNotes writes the saved notes in a goroutine of its own, once. The notification is made on the UI thread; the button
// Open Folder opens the folder in a new workspace tab. It must be called on the UI thread, once the binding is there.
func (m *Workspaces) startSavedNotes(post func(func())) {
	run := m.savedNotes
	if run == nil {
		return
	}
	m.savedNotes = nil
	ctx := context.WithoutCancel(m.ctxOr()) // a quit while the files are written does not leave half of them
	open := func(folder string) {
		if _, err := m.Open(WorkspaceSpec{Folder: folder}); err != nil {
			run.notify.Error("Emetos", "Cannot open "+folder+": "+describe(err))
		}
	}
	go run.texts.export(ctx, post, run.notify, open)
}
