package document

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/fileio"
	"github.com/rm4n0s/errors"
)

// openFile makes the document of a file on disk, as the window does.
func openFile(t *testing.T, path string) *Document {
	t.Helper()
	res, err := fileio.Read(context.Background(), path, fileio.ReadOptions{})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	return FromRead(FileURI(path), res, NewOptions())
}

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func appendText(t *testing.T, d *Document, text string) {
	t.Helper()
	end := d.Snapshot().PositionAt(d.Snapshot().Len())
	edit(t, d, []Edit{ins(end.Line, end.Col, text)}, EditOptions{})
}

func readDisk(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSaveRoundTripsTheFileProperties(t *testing.T) {
	tests := []struct {
		name    string
		bytes   string
		wantEnc string
		add     string
		want    string
	}{
		{"utf8 lf", "one\ntwo\n", "utf8", "three\n", "one\ntwo\nthree\n"},
		{"utf8 crlf", "one\r\ntwo\r\n", "utf8", "three\n", "one\r\ntwo\r\nthree\r\n"},
		{"utf8 with a mark", "\xEF\xBB\xBFé\n", "utf8bom", "x\n", "\xEF\xBB\xBF\xc3\xa9\nx\n"},
		{"utf16 le with a mark", "\xFF\xFEa\x00\n\x00", "utf16le", "b", "\xFF\xFEa\x00\n\x00b\x00"},
		{"utf16 be with a mark", "\xFE\xFF\x00a\x00\n", "utf16be", "b", "\xFE\xFF\x00a\x00\n\x00b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "f.txt")
			writeFile(t, path, tt.bytes, 0o644)
			d := openFile(t, path)
			if d.Encoding() != tt.wantEnc {
				t.Fatalf("encoding = %s, want %s", d.Encoding(), tt.wantEnc)
			}
			appendText(t, d, tt.add)
			if !d.Dirty() {
				t.Fatal("not dirty after an edit")
			}
			res, err := d.Save(context.Background(), SaveOptions{})
			if err != nil {
				t.Fatalf("Save: %v", err)
			}
			if got := string(readDisk(t, path)); got != tt.want {
				t.Errorf("file = %q, want %q", got, tt.want)
			}
			if d.Dirty() || !res.Clean {
				t.Error("still dirty after the save")
			}
			if d.Stat() != res.Stat || d.Stat().Size != int64(len(tt.want)) {
				t.Errorf("stat = %+v, size should be %d", d.Stat(), len(tt.want))
			}
		})
	}
}

func TestSaveLatin1RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "l.txt")
	writeFile(t, path, "caf\xe9\n", 0o644)
	res, err := fileio.Read(context.Background(), path, fileio.ReadOptions{Encoding: "iso88591"})
	if err != nil {
		t.Fatal(err)
	}
	d := FromRead(FileURI(path), res, NewOptions())
	appendText(t, d, "naïve\n")
	if _, err := d.Save(context.Background(), SaveOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := string(readDisk(t, path)); got != "caf\xe9\nna\xefve\n" {
		t.Errorf("file = %q", got)
	}
}

func TestSaveKeepsTheModeAndFollowsLinks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "link")
	writeFile(t, target, "old\n", 0o640)
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	d := openFile(t, link)
	appendText(t, d, "new\n")
	if _, err := d.Save(context.Background(), SaveOptions{}); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the link was replaced: %v %v", info, err)
	}
	info, err := os.Stat(target)
	if err != nil || info.Mode().Perm() != 0o640 || string(readDisk(t, target)) != "old\nnew\n" {
		t.Errorf("target: %v %v %q", info, err, readDisk(t, target))
	}
	// the stat of the document is the one of the target, so the next save finds no conflict
	appendText(t, d, "again\n")
	if _, err := d.Save(context.Background(), SaveOptions{}); err != nil {
		t.Errorf("second save: %v", err)
	}
}

func TestSaveRecreatesADeletedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.txt")
	writeFile(t, path, "a\n", 0o644)
	d := openFile(t, path)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	appendText(t, d, "b\n")
	if _, err := d.Save(context.Background(), SaveOptions{}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got := string(readDisk(t, path)); got != "a\nb\n" {
		t.Errorf("file = %q", got)
	}
}

func TestSaveConflict(t *testing.T) {
	tests := []struct {
		name   string
		change func(t *testing.T, path string)
	}{
		{"another size", func(t *testing.T, path string) { writeFile(t, path, "changed elsewhere\n", 0o644) }},
		{"another time", func(t *testing.T, path string) {
			if err := os.Chtimes(path, time.Now(), time.Now().Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "f.txt")
			writeFile(t, path, "a\n", 0o644)
			d := openFile(t, path)
			appendText(t, d, "mine\n")
			tt.change(t, path)
			before := readDisk(t, path)
			_, err := d.Save(context.Background(), SaveOptions{})
			wantTag(t, err, "Save", "FileModifiedSince")
			if string(readDisk(t, path)) != string(before) {
				t.Error("a refused save changed the file")
			}
			if !d.Dirty() {
				t.Error("a refused save cleared the dirty mark")
			}
			// IgnoreConflict saves anyway
			if _, err := d.Save(context.Background(), SaveOptions{IgnoreConflict: true}); err != nil {
				t.Fatalf("Save with IgnoreConflict: %v", err)
			}
			if got := string(readDisk(t, path)); got != "a\nmine\n" {
				t.Errorf("file = %q", got)
			}
			if d.Dirty() {
				t.Error("dirty after the overwrite")
			}
		})
	}
}

// An edit made while the write goes on keeps the document dirty, and the stat of the write is kept.
func TestEditDuringTheWriteKeepsTheDocumentDirty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.txt")
	writeFile(t, path, "a\n", 0o644)
	d := openFile(t, path)
	appendText(t, d, "b\n")
	job, err := d.BeginSave(SaveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	appendText(t, d, "typed meanwhile\n") // the user types while the write waits for the disk
	stat, err := job.Write(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var events []SavedEvent
	d.OnSaved(func(e SavedEvent) { events = append(events, e) })
	res := d.FinishSave(job, stat)
	if got := string(readDisk(t, path)); got != "a\nb\n" {
		t.Errorf("file = %q, want the text of the moment the save began", got)
	}
	if res.Clean || !d.Dirty() {
		t.Error("the document counts as saved although it was edited during the write")
	}
	if d.Stat() != stat {
		t.Errorf("stat = %+v, want the stat of the write %+v", d.Stat(), stat)
	}
	if len(events) != 1 || events[0].Clean || events[0].Path != path {
		t.Errorf("events = %+v", events)
	}
	// the next save is not a conflict with the document's own write
	if _, err := d.Save(context.Background(), SaveOptions{}); err != nil {
		t.Fatalf("second save: %v", err)
	}
	if got := string(readDisk(t, path)); got != "a\nb\ntyped meanwhile\n" || d.Dirty() {
		t.Errorf("file = %q, dirty %v", got, d.Dirty())
	}
}

// Typing and undoing during the write brings the document back to the text that was written.
func TestUndoDuringTheWriteLeavesTheDocumentClean(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.txt")
	writeFile(t, path, "a\n", 0o644)
	d := openFile(t, path)
	appendText(t, d, "b\n")
	job, err := d.BeginSave(SaveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	appendText(t, d, "x")
	undo(t, d)
	stat, err := job.Write(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res := d.FinishSave(job, stat); !res.Clean {
		t.Error("the text is the one that was written, and the document is dirty")
	}
}

func TestSaveFailures(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name  string
		setup func(t *testing.T) *Document
		route string
		tag   string
		skip  func() bool
	}{
		{"read-only file", func(t *testing.T) *Document {
			path := filepath.Join(dir, "ro.txt")
			writeFile(t, path, "a\n", 0o444)
			return openFile(t, path)
		}, "Save", "FileReadOnly", func() bool { return os.Geteuid() == 0 }},
		{"missing folder", func(*testing.T) *Document {
			return New(FileURI(filepath.Join(dir, "no", "such", "f.txt")), "x", NewOptions())
		}, "Save", "FileNotFound", nil},
		{"a folder", func(*testing.T) *Document { return New(FileURI(dir), "x", NewOptions()) }, "Save", "FileIsDirectory", nil},
		{"read-only document", func(*testing.T) *Document {
			o := NewOptions()
			o.ReadOnly = true
			return New(FileURI(filepath.Join(dir, "x.txt")), "x", o)
		}, "Save", "SaveReadOnlyDocument", nil},
		{"untitled", func(*testing.T) *Document { return New(UntitledURI(1), "x", NewOptions()) }, "Save", "SaveNoFile", nil},
		{"virtual", func(*testing.T) *Document { return New(VirtualURI("defaults.json"), "x", NewOptions()) }, "Save", "SaveNoFile", nil},
		{"character the encoding lacks", func(*testing.T) *Document {
			d := New(FileURI(filepath.Join(dir, "enc.txt")), "日本", NewOptions())
			d.SetEncoding("windows1252")
			return d
		}, "Save", "EncodingCannotRepresent", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.skip != nil && tt.skip() {
				t.Skip("not applicable here")
			}
			d := tt.setup(t)
			before := d.Dirty()
			_, err := d.Save(context.Background(), SaveOptions{})
			wantTag(t, err, tt.route, tt.tag)
			if d.Dirty() != before {
				t.Error("a failed save changed the dirty mark")
			}
		})
	}
}

func TestReadOnlyFileSavesWithOverwrite(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permissions don't apply")
	}
	path := filepath.Join(t.TempDir(), "ro.txt")
	writeFile(t, path, "a\n", 0o444)
	d := openFile(t, path)
	appendText(t, d, "b\n")
	if _, err := d.Save(context.Background(), SaveOptions{Overwrite: true}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, _ := os.Stat(path)
	if string(readDisk(t, path)) != "a\nb\n" || info.Mode().Perm() != 0o644 {
		t.Errorf("file %q mode %v", readDisk(t, path), info.Mode().Perm())
	}
}

func TestSaveRunsTheParticipantsBeforeTheTextIsTaken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.txt")
	writeFile(t, path, "a\n", 0o644)
	d := openFile(t, path)
	appendText(t, d, "b  ")
	var reason SaveReason = -1
	_, err := d.Save(context.Background(), SaveOptions{Reason: SaveAuto, Participate: func(d *Document, r SaveReason) error {
		reason = r
		edit(t, d, []Edit{{Range: rg(1, 1, 1, 3), Text: ""}}, EditOptions{})
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(readDisk(t, path)); got != "a\nb" || d.Dirty() || reason != SaveAuto {
		t.Errorf("file %q dirty %v reason %v", got, d.Dirty(), reason)
	}
}

func TestParticipantFailureStopsTheSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.txt")
	writeFile(t, path, "a\n", 0o644)
	d := openFile(t, path)
	appendText(t, d, "b")
	_, err := d.Save(context.Background(), SaveOptions{Participate: func(*Document, SaveReason) error { return os.ErrInvalid }})
	wantTag(t, err, "begin", "SaveParticipantFailed")
	if got := string(readDisk(t, path)); got != "a\n" {
		t.Errorf("file = %q", got)
	}
	// an error that is tagged already passes untouched
	tagged := errors.New("Custom", "x")
	_, err = d.Save(context.Background(), SaveOptions{Participate: func(*Document, SaveReason) error { return tagged }})
	if err != error(tagged) {
		t.Errorf("err = %v, want the participant's own error", err)
	}
}

func TestSaveAsFromUntitled(t *testing.T) {
	dir := t.TempDir()
	d := New(UntitledURI(1), "package main\n", NewOptions())
	edit(t, d, []Edit{ins(1, 0, "// x\n")}, EditOptions{})
	var olds, news []URI
	d.OnURIChange(func(o, n URI) { olds, news = append(olds, o), append(news, n) })
	var saved []SavedEvent
	d.OnSaved(func(e SavedEvent) { saved = append(saved, e) })
	path := filepath.Join(dir, "x.go")
	detect := func(p, first string) string {
		if strings.HasSuffix(p, ".go") {
			return "go"
		}
		return "plaintext"
	}
	res, err := d.SaveAs(context.Background(), path, SaveOptions{DetectLanguage: detect})
	if err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
	if d.URI() != FileURI(path) || len(olds) != 1 || olds[0] != UntitledURI(1) || news[0] != FileURI(path) {
		t.Errorf("uri %v, changes %v -> %v", d.URI(), olds, news)
	}
	if d.LanguageID() != "go" || !res.LanguageChanged {
		t.Errorf("language %q, changed %v", d.LanguageID(), res.LanguageChanged)
	}
	if got := string(readDisk(t, path)); got != "package main\n// x\n" {
		t.Errorf("file = %q", got)
	}
	if d.Dirty() || d.Stat() != res.Stat || len(saved) != 1 || !saved[0].As {
		t.Errorf("dirty %v stat %+v events %+v", d.Dirty(), d.Stat(), saved)
	}
	// the history survives the save
	if !d.CanUndo() {
		t.Fatal("the undo history is gone")
	}
	undo(t, d)
	if d.Text() != "package main\n" || !d.Dirty() {
		t.Errorf("after undo %q dirty %v", d.Text(), d.Dirty())
	}
	// and from now on it is saved in place
	if _, err := d.Save(context.Background(), SaveOptions{}); err != nil {
		t.Fatalf("Save after SaveAs: %v", err)
	}
	if got := string(readDisk(t, path)); got != "package main\n" {
		t.Errorf("file = %q", got)
	}
}

func TestSaveAsKeepsTheLanguageOfAnUnknownName(t *testing.T) {
	d := New(UntitledURI(1), "x", NewOptions())
	d.SetLanguageID("go")
	path := filepath.Join(t.TempDir(), "noext")
	res, err := d.SaveAs(context.Background(), path, SaveOptions{DetectLanguage: func(string, string) string { return "plaintext" }})
	if err != nil || d.LanguageID() != "go" || res.LanguageChanged {
		t.Errorf("language %q changed %v err %v", d.LanguageID(), res.LanguageChanged, err)
	}
}

func TestSaveAsLeavesTheOldFileAlone(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old.txt")
	writeFile(t, old, "old\n", 0o644)
	d := openFile(t, old)
	appendText(t, d, "more\n")
	newPath := filepath.Join(dir, "new.txt")
	if _, err := d.SaveAs(context.Background(), newPath, SaveOptions{}); err != nil {
		t.Fatal(err)
	}
	if string(readDisk(t, old)) != "old\n" || string(readDisk(t, newPath)) != "old\nmore\n" || d.URI() != FileURI(newPath) {
		t.Errorf("old %q new %q uri %v", readDisk(t, old), readDisk(t, newPath), d.URI())
	}
}

func TestSaveAsOverAFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "t.txt")
	writeFile(t, target, "target\n", 0o644)
	d := New(UntitledURI(1), "mine\n", NewOptions())
	_, err := d.SaveAs(context.Background(), target, SaveOptions{})
	wantTag(t, err, "SaveAs", "FileExists")
	if string(readDisk(t, target)) != "target\n" || d.URI() != UntitledURI(1) {
		t.Errorf("a refused Save As changed %q or the uri %v", readDisk(t, target), d.URI())
	}
	if _, err := d.SaveAs(context.Background(), target, SaveOptions{Overwrite: true}); err != nil {
		t.Fatal(err)
	}
	if string(readDisk(t, target)) != "mine\n" || d.URI() != FileURI(target) {
		t.Errorf("file %q uri %v", readDisk(t, target), d.URI())
	}
}

func TestSaveAsToItsOwnPathIsASave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.txt")
	writeFile(t, path, "a\n", 0o644)
	d := openFile(t, path)
	appendText(t, d, "b\n")
	var uriChanges int
	d.OnURIChange(func(_, _ URI) { uriChanges++ })
	if _, err := d.SaveAs(context.Background(), path, SaveOptions{}); err != nil {
		t.Fatal(err)
	}
	if uriChanges != 0 || string(readDisk(t, path)) != "a\nb\n" {
		t.Errorf("uri changes %d file %q", uriChanges, readDisk(t, path))
	}
}

func TestSaveAsErrors(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name string
		doc  *Document
		path string
		tag  string
	}{
		{"relative path", New(UntitledURI(1), "x", NewOptions()), "rel/f.txt", "SavePathInvalid"},
		{"empty path", New(UntitledURI(1), "x", NewOptions()), "", "SavePathInvalid"},
		{"missing folder", New(UntitledURI(1), "x", NewOptions()), filepath.Join(dir, "no", "f.txt"), "FileNotFound"},
		{"a folder", New(UntitledURI(1), "x", NewOptions()), dir, "FileIsDirectory"},
		{"read-only document", New(VirtualURI("v"), "x", Options{ReadOnly: true}), filepath.Join(dir, "v.txt"), "SaveReadOnlyDocument"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.doc.SaveAs(context.Background(), tt.path, SaveOptions{})
			wantTag(t, err, "SaveAs", tt.tag)
			if tt.doc.URI().Scheme == SchemeFile {
				t.Error("a failed Save As changed the URI")
			}
		})
	}
}

func TestSaveAsOfAVirtualDocument(t *testing.T) {
	path := filepath.Join(t.TempDir(), "copy.json")
	d := New(VirtualURI("defaults.json"), "{}\n", NewOptions())
	if _, err := d.SaveAs(context.Background(), path, SaveOptions{}); err != nil {
		t.Fatal(err)
	}
	if d.URI() != FileURI(path) || string(readDisk(t, path)) != "{}\n" {
		t.Errorf("uri %v file %q", d.URI(), readDisk(t, path))
	}
}

func TestRevert(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	writeFile(t, path, "caf\xe9\r\n", 0o644)
	res, err := fileio.Read(context.Background(), path, fileio.ReadOptions{Encoding: "iso88591", ForceEncoding: true})
	if err != nil {
		t.Fatal(err)
	}
	d := FromRead(FileURI(path), res, NewOptions())
	appendText(t, d, "typed")
	writeFile(t, path, "caf\xe9\r\nfrom elsewhere\r\n", 0o644)
	if err := d.Revert(context.Background(), fileio.ReadOptions{}); err != nil {
		t.Fatalf("Revert: %v", err)
	}
	if d.Text() != "café\nfrom elsewhere\n" || d.Dirty() || d.CanUndo() {
		t.Errorf("text %q dirty %v canUndo %v", d.Text(), d.Dirty(), d.CanUndo())
	}
	if d.Encoding() != "iso88591" || d.LineEnding() != fileio.CRLF {
		t.Errorf("encoding %s eol %q: the encoding of the document is kept", d.Encoding(), d.LineEnding())
	}
	if d.Stat().Size != int64(len("caf\xe9\r\nfrom elsewhere\r\n")) {
		t.Errorf("stat = %+v", d.Stat())
	}
}

func TestRevertFailures(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name string
		doc  *Document
		tag  string
	}{
		{"deleted file", New(FileURI(filepath.Join(dir, "gone.txt")), "x", NewOptions()), "FileNotFound"},
		{"virtual", New(VirtualURI("v"), "x", NewOptions()), "RevertNoFile"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantTag(t, tt.doc.Revert(context.Background(), fileio.ReadOptions{}), "Revert", tt.tag)
		})
	}
}

func TestRevertOfUntitledEmptiesIt(t *testing.T) {
	d := New(UntitledURI(1), "text", NewOptions())
	if !d.Dirty() {
		t.Fatal("an untitled document with text is not dirty")
	}
	if err := d.Revert(context.Background(), fileio.ReadOptions{}); err != nil {
		t.Fatal(err)
	}
	if d.Text() != "" || d.Dirty() {
		t.Errorf("text %q dirty %v", d.Text(), d.Dirty())
	}
}

func TestUntitledDirtyFollowsTheContent(t *testing.T) {
	d := New(UntitledURI(1), "", NewOptions())
	steps := []struct {
		name string
		do   func()
		want bool
	}{
		{"empty", func() {}, false},
		{"typed", func() { edit(t, d, []Edit{ins(0, 0, "a")}, EditOptions{}) }, true},
		{"emptied again", func() { edit(t, d, []Edit{{Range: rg(0, 0, 0, 1), Text: ""}}, EditOptions{}) }, false},
		{"undo", func() { undo(t, d) }, true},
	}
	for _, s := range steps {
		s.do()
		if d.Dirty() != s.want {
			t.Errorf("%s: dirty = %v, want %v", s.name, d.Dirty(), s.want)
		}
	}
}

func TestSetSaveEncoding(t *testing.T) {
	tests := []struct {
		id  string
		bom bool
		tag string
	}{
		{"utf8", false, ""}, {"utf8bom", true, ""}, {"utf16le", true, ""}, {"utf16be", true, ""}, {"windows1252", false, ""},
		{"klingon", false, "SaveEncodingUnknown"},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			d := New(FileURI("/x"), "x", NewOptions())
			err := d.SetSaveEncoding(tt.id)
			if tt.tag != "" {
				wantTag(t, err, "SetSaveEncoding", tt.tag)
				return
			}
			if err != nil || d.Encoding() != tt.id || d.HasBOM() != tt.bom {
				t.Errorf("encoding %s bom %v err %v", d.Encoding(), d.HasBOM(), err)
			}
		})
	}
}

func TestSaveInANewEncodingWritesTheMark(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.txt")
	writeFile(t, path, "a\n", 0o644)
	d := openFile(t, path)
	if err := d.SetSaveEncoding("utf16le"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Save(context.Background(), SaveOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := string(readDisk(t, path)); got != "\xFF\xFEa\x00\n\x00" {
		t.Errorf("file = %q", got)
	}
}

// TestSaveClosesTheUndoGroup: typing before a save and typing after it are two undo steps, so that one Undo brings back the
// saved text (VS Code's textFileEditorModel.doSave pushes a stack element "so that the user has a chance to Ctrl+Z back to the
// saved version"). Typing after a save joined the typing before it, and one Undo went past the saved text.
func TestSaveClosesTheUndoGroup(t *testing.T) {
	for _, tt := range []struct {
		name string
		save func(d *Document) error
	}{
		{"save", func(d *Document) error { _, err := d.Save(context.Background(), SaveOptions{}); return err }},
		{"save as to the same file", func(d *Document) error {
			_, err := d.SaveAs(context.Background(), d.URI().Path, SaveOptions{})
			return err
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "a.txt")
			writeFile(t, path, "one\n", 0o644)
			d := openFile(t, path)
			edit(t, d, []Edit{ins(0, 3, "a")}, EditOptions{Kind: KindTyping})
			if err := tt.save(d); err != nil {
				t.Fatal(err)
			}
			saved := d.Text()
			edit(t, d, []Edit{ins(0, 4, "b")}, EditOptions{Kind: KindTyping})
			if _, err := d.Undo(); err != nil {
				t.Fatal(err)
			}
			if got := d.Text(); got != saved {
				t.Errorf("after one undo the text is %q, want the saved %q", got, saved)
			}
			if d.Dirty() {
				t.Error("the document is dirty at the saved text")
			}
		})
	}
}
