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
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/notify"
	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/core/storage/storagetest"
)

// The export of the text of the removed Notes and Scratchpad extensions (step 17e.1, SPEC §10.26, D28). The fixture in
// testdata/savednotes/state is a saved state made by the code of those extensions and of the state store before they were removed:
// two windows (one with the list of the notes, a tab of one of them and a TODOs view; one whose list did not come back and whose
// tab keeps the note) and the list of the tabs with three Scratchpad tabs.

// savedNotesLog is a logger that keeps what it writes.
func savedNotesLog() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})), &buf
}

// savedNotesEnv is an isolated set of folders and a process over them.
type savedNotesEnv struct {
	t     *testing.T
	paths storage.Paths
	p     *Process
	log   *bytes.Buffer
}

func newSavedNotesEnv(t *testing.T) *savedNotesEnv {
	t.Helper()
	paths := storagetest.Isolate(t)
	logger, buf := savedNotesLog()
	return &savedNotesEnv{t: t, paths: paths, p: &Process{Paths: paths, Logger: logger}, log: buf}
}

// state writes a file of the saved state, below the state folder of Emetos.
func (e *savedNotesEnv) state(rel, content string) string {
	e.t.Helper()
	path := filepath.Join(e.paths.StateHome, "emetos", filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		e.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		e.t.Fatal(err)
	}
	return path
}

// fixture copies the saved state of testdata/savednotes/state into the state folder.
func (e *savedNotesEnv) fixture() {
	e.t.Helper()
	root := filepath.Join("testdata", "savednotes", "state", "emetos")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		e.state(filepath.ToSlash(rel), string(data))
		return nil
	})
	if err != nil {
		e.t.Fatal(err)
	}
}

// files are the files of the folder of the export, by name, with their text.
func (e *savedNotesEnv) files() map[string]string {
	e.t.Helper()
	out := map[string]string{}
	entries, err := os.ReadDir(savedNotesFolderOf(e.paths))
	if errors.Is(err, fs.ErrNotExist) {
		return out
	}
	if err != nil {
		e.t.Fatal(err)
	}
	for _, en := range entries {
		data, err := os.ReadFile(filepath.Join(savedNotesFolderOf(e.paths), en.Name()))
		if err != nil {
			e.t.Fatal(err)
		}
		out[en.Name()] = string(data)
	}
	return out
}

// export loads the saved notes and writes them, with a notification service of a window; post runs the work at once.
func (e *savedNotesEnv) export() (loaded *savedNotes, n *notify.Service) {
	e.t.Helper()
	n = notify.NewService(e.p.Logger, nil)
	loaded = e.p.loadSavedNotes(context.Background())
	if loaded == nil {
		return nil, n
	}
	loaded.export(context.Background(), func(f func()) { f() }, n, func(string) {})
	return loaded, n
}

func routeOf(t *testing.T, err error, want string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute(want) {
		t.Errorf("unexpected failure path: %s (want %s)", appErr.Route(), want)
	}
}

func TestSavedNotesAreReadFromTheStateTheRemovedExtensionsLeft(t *testing.T) {
	e := newSavedNotesEnv(t)
	e.fixture()
	got := e.p.loadSavedNotes(context.Background())
	if got == nil {
		t.Fatal("nothing was found in the saved state")
	}
	// the list of the tabs first (the Scratchpad tabs; the one without text gives none), then the windows by the name of their
	// file: the note that only a tab keeps, then the list of the notes (the note of the open tab is the one in the list)
	var names []string
	for _, x := range got.texts {
		names = append(names, x.name)
	}
	want := []string{
		"Scratchpad", "Scratchpad Ideas",
		`Plans/2026: "big" ones`,
		"Groceries", "Groceries", "Untitled Note", "Empty idea", `Plans/2026: "big" ones`,
	}
	if !slices.Equal(names, want) {
		t.Errorf("the texts are\n%q\nwant\n%q", names, want)
	}
	if got.texts[0].text != "Ideas for the release\n- the window tabs\n- the remote control" || got.texts[3].text != "milk\neggs\nbread" {
		t.Errorf("the texts hold %q and %q", got.texts[0].text, got.texts[3].text)
	}
	if got.folder != filepath.Join(e.paths.DataHome, "emetos", "saved-notes") {
		t.Errorf("the folder is %q", got.folder)
	}
}

func TestTheExportWritesTheFilesOnceAndTellsTheUser(t *testing.T) {
	e := newSavedNotesEnv(t)
	e.fixture()
	_, n := e.export()
	want := map[string]string{
		"Scratchpad.md":               "Ideas for the release\n- the window tabs\n- the remote control",
		"Scratchpad Ideas.md":         "Ideas for the release\n- the window tabs\n- the remote control",
		`Plans-2026: "big" ones.md`:   "title with characters a file name cannot hold",
		`Plans-2026: "big" ones 2.md`: "title with characters a file name cannot hold",
		"Groceries.md":                "milk\neggs\nbread",
		"Groceries 2.md":              "a second list with the same title",
		"Untitled Note.md":            "a note with a text and no title",
		"Empty idea.md":               "",
		savedNotesMarker:              "",
	}
	got := e.files()
	if !strings.HasPrefix(got[savedNotesMarker], "The notes and the Scratchpad text") {
		t.Errorf("the marker holds %q", got[savedNotesMarker])
	}
	got[savedNotesMarker] = ""
	if len(got) != len(want) {
		t.Errorf("the files are %v", got)
	}
	for name, text := range want {
		if g, ok := got[name]; !ok || g != text {
			t.Errorf("%s holds %q (present %v), want %q", name, g, ok, text)
		}
	}
	folder := savedNotesFolderOf(e.paths)
	if info, err := os.Stat(folder); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("the folder is %v, %v", info, err)
	}
	if info, err := os.Stat(filepath.Join(folder, "Groceries.md")); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("the file is %v, %v", info, err)
	}
	// one notification, with the button that opens the folder
	items := n.Items()
	if len(items) != 1 || items[0].Severity != notify.Info || items[0].Message != "Your notes and Scratchpad text were saved as files in "+folder+"." ||
		len(items[0].Actions) != 1 || items[0].Actions[0].Label != "Open Folder" {
		t.Fatalf("the notifications are %+v", items)
	}
	// the saved state is not changed
	if data, err := os.ReadFile(filepath.Join(e.paths.StateHome, "emetos", "state.json")); err != nil || !bytes.Contains(data, []byte("emetos.scratchpad.tab")) {
		t.Errorf("the saved state changed: %v", err)
	}
}

func TestOpenFolderOpensTheFolderOfTheFiles(t *testing.T) {
	e := newSavedNotesEnv(t)
	e.fixture()
	n := notify.NewService(e.p.Logger, nil)
	loaded := e.p.loadSavedNotes(context.Background())
	var opened []string
	loaded.export(context.Background(), func(f func()) { f() }, n, func(folder string) { opened = append(opened, folder) })
	n.Items()[0].Actions[0].Run()
	if !slices.Equal(opened, []string{loaded.folder}) {
		t.Errorf("opened %v, want %v", opened, loaded.folder)
	}
}

func TestSavedNotesCases(t *testing.T) {
	windowOf := func(views string) string { return `{"version":1,"views":` + views + `}` }
	tests := []struct {
		name string
		// files are the files of the saved state, by their place below the state folder of Emetos
		files map[string]string
		// want are the files that are written, by name, with their text; nil means that nothing is written
		want map[string]string
	}{
		{"notes with and without titles", map[string]string{"windows/00000000000000a1.json": windowOf(
			`{"emetos.notes.list":{"notes":[{"id":"n1","title":"Shopping","text":"milk"},{"id":"n2","title":"","text":"loose"},{"id":"n3","title":"  Padded  ","text":"x"}]}}`)},
			map[string]string{"Shopping.md": "milk", "Untitled Note.md": "loose", "Padded.md": "x"}},
		{"two notes with one title", map[string]string{"windows/00000000000000a1.json": windowOf(
			`{"emetos.notes.list":{"notes":[{"id":"n1","title":"Same","text":"one"},{"id":"n2","title":"Same","text":"two"},{"id":"n3","title":"Same","text":"three"}]}}`)},
			map[string]string{"Same.md": "one", "Same 2.md": "two", "Same 3.md": "three"}},
		{"a note with a title and no text, and one with neither", map[string]string{"windows/00000000000000a1.json": windowOf(
			`{"emetos.notes.list":{"notes":[{"id":"n1","title":"Idea","text":""},{"id":"n2","title":"","text":""},null,{"title":"no id","text":"t"}]}}`)},
			map[string]string{"Idea.md": ""}},
		{"a Scratchpad with text", map[string]string{"state.json": `{"version":1,"workspaceTabs":[{"id":"1111111111111111","kind":"emetos.scratchpad.tab","state":{"notes":"remember"}}]}`},
			map[string]string{"Scratchpad.md": "remember"}},
		{"a Scratchpad with its own title", map[string]string{"state.json": `{"version":1,"workspaceTabs":[{"id":"1111111111111111","kind":"emetos.scratchpad.tab","title":"Release","state":{"notes":"a"}},{"id":"2222222222222222","kind":"emetos.scratchpad.tab","title":"Release","state":{"notes":"b"}}]}`},
			map[string]string{"Scratchpad Release.md": "a", "Scratchpad Release 2.md": "b"}},
		{"a Scratchpad without text", map[string]string{"state.json": `{"version":1,"workspaceTabs":[{"id":"1111111111111111","kind":"emetos.scratchpad.tab","state":{"notes":" \n"}},{"id":"2222222222222222","kind":"emetos.scratchpad.tab"}]}`}, nil},
		{"a note that only a tab keeps", map[string]string{"windows/00000000000000a1.json": `{"version":1,"grid":{"orientation":0,"root":[{"size":1,"model":{"id":1,"editors":[{"id":"emetos.extensionTab","value":"{\"ext\":\"emetos.notes\",\"kind\":\"emetos.notes.note\",\"id\":\"t1\"}"}],"mru":[0]}}],"active":1,"mru":[1],"nextId":2},"views":{"editor:t1":{"note":{"id":"n1","title":"Alone","text":"in a tab"}},"editor:t2":{"note":{"id":"n2","title":"Not in the grid","text":"y"}}}}`},
			map[string]string{"Alone.md": "in a tab"}},
		{"a window of a profile", map[string]string{"profiles/Work/windows/00000000000000a1.json": windowOf(`{"emetos.notes.list":{"notes":[{"id":"n1","title":"Work note","text":"w"}]}}`)},
			map[string]string{"Work note.md": "w"}},
		{"nothing to save: no state", nil, nil},
		{"nothing to save: a state without the extensions", map[string]string{
			"state.json":                    `{"version":1,"workspaceTabs":[{"id":"1111111111111111","stateId":"aaaaaaaaaaaaaaaa"}]}`,
			"windows/00000000000000a1.json": windowOf(`{"emetos.tasks.panel":{"command":"go test"}}`)}, nil},
		{"nothing to save: empty notes", map[string]string{"windows/00000000000000a1.json": windowOf(
			`{"emetos.notes.list":{"notes":[{"id":"n1","title":"","text":" "}]}}`)}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newSavedNotesEnv(t)
			for rel, content := range tc.files {
				e.state(rel, content)
			}
			loaded, n := e.export()
			got := e.files()
			if tc.want == nil {
				if loaded != nil || len(got) != 0 || len(n.Items()) != 0 {
					t.Fatalf("something was done: loaded %v, files %v, notifications %v", loaded, got, n.Items())
				}
				if _, err := os.Stat(savedNotesFolderOf(e.paths)); !errors.Is(err, fs.ErrNotExist) {
					t.Errorf("the folder was made: %v", err)
				}
				return
			}
			delete(got, savedNotesMarker)
			if len(got) != len(tc.want) {
				t.Errorf("the files are %v, want %v", got, tc.want)
			}
			for name, text := range tc.want {
				if g, ok := got[name]; !ok || g != text {
					t.Errorf("%s holds %q (present %v), want %q", name, g, ok, text)
				}
			}
			if len(n.Items()) != 1 {
				t.Errorf("%d notifications", len(n.Items()))
			}
		})
	}
}

func TestASecondStartWritesNothingAgain(t *testing.T) {
	e := newSavedNotesEnv(t)
	e.fixture()
	if loaded, _ := e.export(); loaded == nil {
		t.Fatal("the first start saved nothing")
	}
	before := e.files()
	// the user edits a file and adds a note to the state: the next start leaves both alone
	edited := filepath.Join(savedNotesFolderOf(e.paths), "Groceries.md")
	if err := os.WriteFile(edited, []byte("my own text"), 0o600); err != nil {
		t.Fatal(err)
	}
	e.state("windows/00000000000000c3.json", `{"version":1,"views":{"emetos.notes.list":{"notes":[{"id":"n1","title":"New","text":"z"}]}}}`)
	loaded, n := e.export()
	if loaded != nil || len(n.Items()) != 0 {
		t.Errorf("a second start did something: %v, %d notifications", loaded, len(n.Items()))
	}
	after := e.files()
	if len(after) != len(before) || after["Groceries.md"] != "my own text" {
		t.Errorf("the files changed: %v", after)
	}
}

func TestAnUnreadableStateIsOneWarningAndTheOthersAreStillSaved(t *testing.T) {
	e := newSavedNotesEnv(t)
	e.state("windows/00000000000000a1.json", `{"version":1,"views":{"emetos.notes.list":{"notes":[{"id":"n1","title":"Kept","text":"k"}]}}}`)
	e.state("windows/00000000000000a2.json", `{"version":1, "views": {"emetos.notes.list": `) // cut short, mentions the extension
	e.state("windows/00000000000000a3.json", `not json at all`)                               // mentions nothing: not read as a state
	e.state("state.json", `{"version":1,"workspaceTabs":[{"id":"1111111111111111","kind":"emetos.scratchpad.tab","state":{"notes":"s"}}]}`)
	loaded, n := e.export()
	if loaded == nil {
		t.Fatal("nothing was saved")
	}
	got := e.files()
	delete(got, savedNotesMarker)
	if len(got) != 2 || got["Kept.md"] != "k" || got["Scratchpad.md"] != "s" {
		t.Errorf("the files are %v", got)
	}
	if warns := strings.Count(e.log.String(), "level=WARN"); warns != 1 || !strings.Contains(e.log.String(), "a2.json") {
		t.Errorf("%d warnings, log:\n%s", warns, e.log.String())
	}
	if len(n.Items()) != 1 || n.Items()[0].Severity != notify.Info {
		t.Errorf("the notifications are %+v", n.Items())
	}
}

func TestEachFailureOfTheSavedNotesHasItsOwnRoute(t *testing.T) {
	t.Run("the state cannot be read: not JSON", func(t *testing.T) {
		e := newSavedNotesEnv(t)
		path := e.state("windows/00000000000000a1.json", `{"views": emetos.notes`)
		_, err := readSavedNotesFile(context.Background(), path)
		routeOf(t, err, "readSavedNotesFile.SavedNotesStateUnreadable")
	})
	t.Run("the state cannot be read: global state", func(t *testing.T) {
		e := newSavedNotesEnv(t)
		path := e.state("state.json", `[emetos.scratchpad.tab`)
		_, err := readSavedNotesFile(context.Background(), path)
		routeOf(t, err, "readSavedNotesFile.SavedNotesStateUnreadable")
	})
	t.Run("the state cannot be read: not a file", func(t *testing.T) {
		e := newSavedNotesEnv(t)
		path := filepath.Join(e.paths.StateHome, "emetos", "windows", "00000000000000a1.json")
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
		_, err := readSavedNotesFile(context.Background(), path)
		routeOf(t, err, "readSavedNotesFile.SavedNotesStateUnreadable")
	})
	t.Run("the state cannot be read: the context ended", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := readSavedNotesFile(ctx, filepath.Join(t.TempDir(), "state.json"))
		routeOf(t, err, "readSavedNotesFile.SavedNotesStateUnreadable")
	})
	t.Run("the folder cannot be made", func(t *testing.T) {
		e := newSavedNotesEnv(t)
		// the Emetos folder of the data home is a file
		if err := os.WriteFile(filepath.Join(e.paths.DataHome, "emetos"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		s := &savedNotes{folder: savedNotesFolderOf(e.paths), texts: []savedText{{name: "a", text: "b"}}, log: e.p.Logger}
		routeOf(t, s.write(context.Background()), "savedNotes.write.SavedNotesFolderFailed")
	})
	t.Run("a file cannot be written", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root writes into a folder that is read-only")
		}
		e := newSavedNotesEnv(t)
		folder := savedNotesFolderOf(e.paths)
		if err := os.MkdirAll(folder, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(folder, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(folder, 0o700) })
		s := &savedNotes{folder: folder, texts: []savedText{{name: "a", text: "b"}}, log: e.p.Logger}
		routeOf(t, s.write(context.Background()), "savedNotes.write.SavedNotesFileFailed")
		if _, err := os.Stat(filepath.Join(folder, savedNotesMarker)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("a marker was written after the failure: %v", err)
		}
	})
	t.Run("a file cannot be written: the context ended", func(t *testing.T) {
		e := newSavedNotesEnv(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		s := &savedNotes{folder: savedNotesFolderOf(e.paths), texts: []savedText{{name: "a", text: "b"}}, log: e.p.Logger}
		routeOf(t, s.write(ctx), "savedNotes.write.SavedNotesFileFailed")
	})
	t.Run("the marker cannot be written", func(t *testing.T) {
		e := newSavedNotesEnv(t)
		folder := savedNotesFolderOf(e.paths)
		if err := os.MkdirAll(folder, 0o700); err != nil {
			t.Fatal(err)
		}
		// a link that points nowhere, where the marker goes
		if err := os.Symlink(filepath.Join(folder, "no", "such", "folder", "x"), filepath.Join(folder, savedNotesMarker)); err != nil {
			t.Fatal(err)
		}
		s := &savedNotes{folder: folder, texts: []savedText{{name: "a", text: "b"}}, log: e.p.Logger}
		routeOf(t, s.write(context.Background()), "savedNotes.write.SavedNotesMarkerFailed")
	})
}

func TestAFailureOfTheExportIsAnErrorNotificationAndTheNextStartTriesAgain(t *testing.T) {
	e := newSavedNotesEnv(t)
	e.fixture()
	if err := os.WriteFile(filepath.Join(e.paths.DataHome, "emetos"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, n := e.export()
	if loaded == nil {
		t.Fatal("nothing was read")
	}
	items := n.Items()
	if len(items) != 1 || items[0].Severity != notify.Error || !strings.Contains(items[0].Message, "could not be saved as files") || len(items[0].Actions) != 0 {
		t.Fatalf("the notifications are %+v", items)
	}
	if !strings.Contains(e.log.String(), "level=ERROR") {
		t.Errorf("the failure is not in the log:\n%s", e.log.String())
	}
	// the cause goes away: the next start writes the files
	if err := os.Remove(filepath.Join(e.paths.DataHome, "emetos")); err != nil {
		t.Fatal(err)
	}
	if again, _ := e.export(); again == nil || len(e.files()) != 9 {
		t.Errorf("the next start did not save the files: %v", e.files())
	}
}

func TestAnInterruptedExportDoesNotMakeTwinsOfTheFilesItWrote(t *testing.T) {
	e := newSavedNotesEnv(t)
	e.fixture()
	loaded := e.p.loadSavedNotes(context.Background())
	// an earlier run wrote two files and stopped before the marker; the user changed one of them
	if err := os.MkdirAll(loaded.folder, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"Scratchpad.md": loaded.texts[0].text, "Groceries.md": "changed by the user"} {
		if err := os.WriteFile(filepath.Join(loaded.folder, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := loaded.write(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := e.files()
	if _, twin := got["Scratchpad 2.md"]; twin {
		t.Errorf("a file that was written already was written again: %v", got)
	}
	if got["Groceries.md"] != "changed by the user" || got["Groceries 2.md"] != "milk\neggs\nbread" || got["Groceries 3.md"] != "a second list with the same title" {
		t.Errorf("the file of the user was not kept: %v", got)
	}
}

func TestFileNameOf(t *testing.T) {
	tests := []struct{ title, want string }{
		{"Groceries", "Groceries"},
		{"  spaced  ", "spaced"},
		{"a/b\\c", "a-b-c"},
		{"line\none\ttab", "line-one-tab"},
		{".hidden", "hidden"},
		{"...", "Untitled"},
		{"", "Untitled"},
		{"café ☕", "café ☕"},
		{strings.Repeat("é", 100), strings.Repeat("é", 75)},
		{"bad \xff byte", "bad - byte"},
	}
	for _, tt := range tests {
		if got := fileNameOf(tt.title); got != tt.want {
			t.Errorf("fileNameOf(%q) = %q, want %q", tt.title, got, tt.want)
		}
	}
}

func TestTheFixtureIsAStateThatTheStoreReads(t *testing.T) {
	e := newSavedNotesEnv(t)
	e.fixture()
	store, err := storage.NewStateStore(e.paths, storage.DefaultProfile, storage.StateStoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	g, err := store.LoadGlobal(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	kinds := 0
	for _, tab := range g.WorkspaceTabs {
		if tab.Kind == scratchpadTabKind {
			kinds++
			var st savedScratchpad
			if err := json.Unmarshal(tab.State, &st); err != nil {
				t.Errorf("the state of a Scratchpad tab: %v", err)
			}
		}
	}
	if kinds != 3 {
		t.Errorf("%d Scratchpad tabs in the fixture, want 3", kinds)
	}
}

// TestTheStartWritesTheFilesOffTheUIThreadAndOpenFolderOpensThemInANewTab is the whole path in a window: the export waits for the
// binding, the files are written in a goroutine, the notification is made on the UI thread, and Open Folder opens the folder as a
// workspace tab, with no dialog.
func TestTheStartWritesTheFilesOffTheUIThreadAndOpenFolderOpensThemInANewTab(t *testing.T) {
	env := newMultiEnv(t, nil)
	a := env.run(Options{Folder: env.folder("one", "a.go")})
	se := &savedNotesEnv{t: t, paths: env.paths, p: a.p, log: &bytes.Buffer{}}
	se.fixture()
	loaded := a.p.loadSavedNotes(context.Background())
	if loaded == nil {
		t.Fatal("nothing was read")
	}
	win := a.m.Active()
	a.m.savedNotes = &savedNotesRun{texts: loaded, notify: win.Notify}
	a.m.startSavedNotes(a.m.binding.Post)
	if a.m.savedNotes != nil {
		t.Error("the export waits for a second start")
	}
	a.waitFor("the notification", func() bool { return len(win.Notify.Items()) == 1 })
	items := win.Notify.Items()
	if items[0].Message != savedNotesMessage(loaded.folder) || len(items[0].Actions) != 1 {
		t.Fatalf("the notification is %+v", items[0])
	}
	if got := se.files(); len(got) != 9 {
		t.Errorf("%d files written: %v", len(got), got)
	}
	before := a.m.Count()
	items[0].Actions[0].Run()
	a.waitFor("the folder to open", func() bool { return a.m.Count() == before+1 && a.m.Active().Folder == loaded.folder })
	if d := a.dialog(a.m.Active()); d != "" {
		t.Errorf("a dialog shows: %q", d)
	}
	if errs := a.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}
