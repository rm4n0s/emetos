package document

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/fileio"
	"github.com/rm4n0s/errors"
)

// The tests of files changed on disk (step 7.4): a look at the file of a document, in a temporary folder.

// base is the time the files of these tests start at; each version of a file is a few seconds later, so that
// a file written twice within one tick of the clock of the file system still differs.
var base = time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)

// putFile writes a file with the modification time base+n seconds.
func putFile(t *testing.T, path, content string, n int) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, base.Add(time.Duration(n)*time.Second), base.Add(time.Duration(n)*time.Second)); err != nil {
		t.Fatal(err)
	}
}

// openAt opens the file as a document, after giving it the modification time base+n seconds.
func openAt(t *testing.T, dir, name, content string, n int) *Document {
	t.Helper()
	path := filepath.Join(dir, name)
	putFile(t, path, content, n)
	return openFile(t, path)
}

// check looks at the file of a document and applies what it finds.
func check(t *testing.T, d *Document) ExternalResult {
	t.Helper()
	res, err := d.CheckExternal(context.Background(), nil, fileio.ReadOptions{})
	if err != nil {
		t.Fatalf("CheckExternal: %v", err)
	}
	return res
}

// stateLog records the states a document tells.
type stateLog struct{ states []ExternalState }

func watchState(d *Document) *stateLog {
	l := &stateLog{}
	d.OnExternalStateChange(func(s ExternalState) { l.states = append(l.states, s) })
	return l
}

func TestExternalCleanDocumentReloads(t *testing.T) {
	tests := []struct {
		name     string
		original string
		changed  string
		prepare  func(d *Document)
		wantText string
		check    func(t *testing.T, d *Document)
	}{
		{name: "longer text", original: "one\n", changed: "one\ntwo\n", wantText: "one\ntwo\n"},
		{name: "same size, later time", original: "abc\n", changed: "xyz\n", wantText: "xyz\n"},
		{
			name: "windows line endings", original: "a\nb\n", changed: "a\r\nb\r\n", wantText: "a\nb\n",
			check: func(t *testing.T, d *Document) {
				if d.LineEnding() != fileio.CRLF {
					t.Errorf("line ending %q", d.LineEnding())
				}
			},
		},
		{
			name: "the encoding is kept", original: "caf\xe9\n", changed: "caf\xe9 au lait\n", wantText: "café au lait\n",
			prepare: func(d *Document) {
				d.Reload(fileio.ReadResult{Text: "café\n", Encoding: "windows1252", LineEnding: fileio.LF, Stat: d.Stat()})
			},
			check: func(t *testing.T, d *Document) {
				if d.Encoding() != "windows1252" {
					t.Errorf("encoding %q", d.Encoding())
				}
			},
		},
		{
			name: "a byte order mark that appeared", original: "a\n", changed: "\xef\xbb\xbfa\nb\n", wantText: "a\nb\n",
			check: func(t *testing.T, d *Document) {
				if !d.HasBOM() || d.Encoding() != fileio.UTF8BOM {
					t.Errorf("bom %v, encoding %q", d.HasBOM(), d.Encoding())
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			d := openAt(t, dir, "f.txt", tt.original, 0)
			if tt.prepare != nil {
				tt.prepare(d)
			}
			appendText(t, d, "x") // a step of undo history ...
			d.MarkSaved(d.Stat()) // ... on a document that counts as saved
			if !d.CanUndo() || d.Dirty() {
				t.Fatalf("can undo %v, dirty %v", d.CanUndo(), d.Dirty())
			}
			log := watchState(d) // no state change is expected
			putFile(t, d.URI().Path, tt.changed, 10)
			res := check(t, d)
			if res.Outcome != ExternalReloaded {
				t.Fatalf("outcome %v", res.Outcome)
			}
			if d.Text() != tt.wantText || d.Dirty() || d.CanUndo() {
				t.Errorf("text %q, dirty %v, can undo %v", d.Text(), d.Dirty(), d.CanUndo())
			}
			if info, _ := os.Stat(d.URI().Path); !SameStat(d.Stat(), fileio.FileStat{ModTime: info.ModTime(), Size: info.Size()}) {
				t.Errorf("stat %+v is not the file's", d.Stat())
			}
			if len(log.states) != 0 || res.StateChanged {
				t.Errorf("states %v", log.states)
			}
			if tt.check != nil {
				tt.check(t, d)
			}
		})
	}
}

func TestExternalReloadSendsOneChange(t *testing.T) {
	d := openAt(t, t.TempDir(), "f.txt", "one\n", 0)
	var causes []Cause
	d.Subscribe(func(ev ChangeEvent) { causes = append(causes, ev.Cause) })
	putFile(t, d.URI().Path, "two\n", 5)
	check(t, d)
	if !slices.Equal(causes, []Cause{CauseSetText}) {
		t.Errorf("causes %v", causes)
	}
}

func TestExternalSpuriousEventIsIgnored(t *testing.T) {
	d := openAt(t, t.TempDir(), "f.txt", "one\n", 0)
	appendText(t, d, "x")
	if _, err := d.Save(context.Background(), SaveOptions{}); err != nil {
		t.Fatal(err)
	}
	var changes int
	d.Subscribe(func(ChangeEvent) { changes++ })
	log := watchState(d)
	for range 3 {
		if res := check(t, d); res.Outcome != ExternalUnchanged {
			t.Fatalf("outcome %v", res.Outcome)
		}
	}
	if changes != 0 || len(log.states) != 0 || !d.CanUndo() || d.Dirty() {
		t.Errorf("changes %d, states %v, can undo %v, dirty %v", changes, log.states, d.CanUndo(), d.Dirty())
	}
}

func TestExternalTouchedFileKeepsTheHistory(t *testing.T) {
	d := openAt(t, t.TempDir(), "f.txt", "one\n", 0)
	appendText(t, d, "y")
	if _, err := d.Save(context.Background(), SaveOptions{}); err != nil {
		t.Fatal(err)
	}
	putFile(t, d.URI().Path, "one\ny", 30) // the content we saved, written again by someone else
	res := check(t, d)
	if res.Outcome != ExternalUnchanged || !d.CanUndo() || d.Dirty() || d.Text() != "one\ny" {
		t.Errorf("outcome %v, can undo %v, dirty %v, text %q", res.Outcome, d.CanUndo(), d.Dirty(), d.Text())
	}
	if !SameStat(d.Stat(), fileio.FileStat{ModTime: base.Add(30 * time.Second), Size: 5}) {
		t.Errorf("the stat of the document is %+v", d.Stat())
	}
}

func TestExternalLargeFileKeepsItsMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.txt")
	putFile(t, path, "0123456789\n", 0)
	limits := fileio.ReadOptions{Limits: fileio.Limits{LargeFileSize: 5}}
	res, err := fileio.Read(context.Background(), path, limits)
	if err != nil {
		t.Fatal(err)
	}
	d := FromRead(FileURI(path), res, NewOptions())
	if !d.LargeFile() {
		t.Fatal("not a large file")
	}
	putFile(t, path, "0123456789\nmore\n", 9)
	got, err := d.CheckExternal(context.Background(), nil, limits)
	if err != nil || got.Outcome != ExternalReloaded || !d.LargeFile() || d.Text() != "0123456789\nmore\n" {
		t.Errorf("outcome %v, err %v, large %v, text %q", got.Outcome, err, d.LargeFile(), d.Text())
	}
}

func TestExternalDirtyDocumentIsConflicted(t *testing.T) {
	d := openAt(t, t.TempDir(), "f.txt", "one\n", 0)
	appendText(t, d, "mine")
	log := watchState(d)
	putFile(t, d.URI().Path, "theirs\n", 10)
	res := check(t, d)
	if res.Outcome != ExternalConflicted || !res.StateChanged {
		t.Fatalf("result %+v", res)
	}
	if d.Text() != "one\nmine" || !d.Dirty() || !d.Conflicted() || d.Orphaned() || !d.CanUndo() {
		t.Errorf("text %q, dirty %v, conflicted %v, orphaned %v", d.Text(), d.Dirty(), d.Conflicted(), d.Orphaned())
	}
	if d.DiskStat().Size != int64(len("theirs\n")) {
		t.Errorf("disk stat %+v", d.DiskStat())
	}
	// the file changes again, and again: one conflict, one state change, the newest stat is kept
	putFile(t, d.URI().Path, "theirs again\n", 20)
	if res := check(t, d); res.Outcome != ExternalConflicted || res.StateChanged {
		t.Errorf("second look %+v", res)
	}
	if res := check(t, d); res.Outcome != ExternalConflicted || res.StateChanged {
		t.Errorf("third look %+v", res)
	}
	if want := []ExternalState{{Conflicted: true}}; !slices.Equal(log.states, want) {
		t.Errorf("states %v, want %v", log.states, want)
	}
	if d.DiskStat().Size != int64(len("theirs again\n")) {
		t.Errorf("disk stat %+v", d.DiskStat())
	}
	// a save is refused as a conflict by the 7.1 path
	_, err := d.Save(context.Background(), SaveOptions{})
	wantTag(t, err, "Save", "FileModifiedSince")
}

func TestExternalConflictResolution(t *testing.T) {
	tests := []struct {
		name     string
		resolve  func(t *testing.T, d *Document)
		wantText string
		wantDisk string
	}{
		{"overwrite", func(t *testing.T, d *Document) {
			if _, err := d.Save(context.Background(), SaveOptions{IgnoreConflict: true}); err != nil {
				t.Fatal(err)
			}
		}, "one\nmine", "one\nmine"},
		{"revert", func(t *testing.T, d *Document) {
			if err := d.Revert(context.Background(), fileio.ReadOptions{}); err != nil {
				t.Fatal(err)
			}
		}, "theirs\n", "theirs\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := openAt(t, t.TempDir(), "f.txt", "one\n", 0)
			appendText(t, d, "mine")
			log := watchState(d)
			putFile(t, d.URI().Path, "theirs\n", 10)
			check(t, d)
			tt.resolve(t, d)
			if d.Conflicted() || d.Dirty() || d.Text() != tt.wantText || string(readDisk(t, d.URI().Path)) != tt.wantDisk {
				t.Errorf("conflicted %v, dirty %v, text %q, disk %q", d.Conflicted(), d.Dirty(), d.Text(), readDisk(t, d.URI().Path))
			}
			if want := []ExternalState{{Conflicted: true}, {}}; !slices.Equal(log.states, want) {
				t.Errorf("states %v, want %v", log.states, want)
			}
			// the file is as the document has it: a look finds nothing
			if res := check(t, d); res.Outcome != ExternalUnchanged {
				t.Errorf("outcome %v", res.Outcome)
			}
		})
	}
}

func TestExternalConflictClearsWhenTheFileReturnsToTheSameStat(t *testing.T) {
	d := openAt(t, t.TempDir(), "f.txt", "one\n", 0)
	appendText(t, d, "mine")
	log := watchState(d)
	putFile(t, d.URI().Path, "two\n", 10)
	check(t, d)
	putFile(t, d.URI().Path, "one\n", 0) // as it was: the same size and time
	res := check(t, d)
	if res.Outcome != ExternalUnchanged || !res.StateChanged || d.Conflicted() || d.Text() != "one\nmine" || !d.Dirty() {
		t.Errorf("result %+v, conflicted %v, text %q, dirty %v", res, d.Conflicted(), d.Text(), d.Dirty())
	}
	if want := []ExternalState{{Conflicted: true}, {}}; !slices.Equal(log.states, want) {
		t.Errorf("states %v", log.states)
	}
}

func TestExternalConflictedDocumentThatIsCleanAgainReloads(t *testing.T) {
	d := openAt(t, t.TempDir(), "f.txt", "one\n", 0)
	appendText(t, d, "mine")
	putFile(t, d.URI().Path, "theirs\n", 10)
	check(t, d)
	if _, err := d.Undo(); err != nil {
		t.Fatal(err)
	}
	if d.Dirty() || !d.Conflicted() {
		t.Fatalf("dirty %v, conflicted %v", d.Dirty(), d.Conflicted())
	}
	res := check(t, d)
	if res.Outcome != ExternalReloaded || d.Text() != "theirs\n" || d.Conflicted() {
		t.Errorf("result %+v, text %q, conflicted %v", res, d.Text(), d.Conflicted())
	}
}

// hookDisk is the disk of the operating system with hooks between the steps of a look.
type hookDisk struct {
	OSDisk
	// onStat is called after each Stat with the number of Stat calls so far.
	onStat   func(n int)
	beforeRd func()
	afterRd  func()
	readErr  error
	statErr  error
	stats    int
}

func (h *hookDisk) Stat(ctx context.Context, path string) (fileio.FileStat, bool, error) {
	if h.statErr != nil {
		return fileio.FileStat{}, false, h.statErr
	}
	st, ok, err := h.OSDisk.Stat(ctx, path)
	h.stats++
	if h.onStat != nil {
		h.onStat(h.stats)
	}
	return st, ok, err
}

func (h *hookDisk) Read(ctx context.Context, path string, opts fileio.ReadOptions) (fileio.ReadResult, error) {
	if h.beforeRd != nil {
		f := h.beforeRd
		h.beforeRd = nil
		f()
	}
	if h.readErr != nil {
		err := h.readErr
		h.readErr = nil
		return fileio.ReadResult{}, err
	}
	res, err := h.OSDisk.Read(ctx, path, opts)
	if h.afterRd != nil {
		h.afterRd()
	}
	return res, err
}

func TestExternalOwnSaveNeverReloads(t *testing.T) {
	d := openAt(t, t.TempDir(), "f.txt", "one\n", 0)
	appendText(t, d, "mine")
	job, err := d.BeginSave(SaveOptions{IgnoreConflict: true})
	if err != nil {
		t.Fatal(err)
	}
	if !d.SaveInFlight() {
		t.Fatal("no save in flight")
	}
	stat, err := job.Write(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// the event of our own write comes between the write and the end of the save: the file is not the document's
	// yet, but it is ours
	res := check(t, d)
	if res.Outcome != ExternalDeferred || d.Text() != "one\nmine" {
		t.Fatalf("result %+v, text %q", res, d.Text())
	}
	d.FinishSave(job, stat)
	var changes int
	d.Subscribe(func(ChangeEvent) { changes++ })
	res = check(t, d)
	if res.Outcome != ExternalUnchanged || changes != 0 || d.Text() != "one\nmine" || d.Dirty() || !d.CanUndo() {
		t.Errorf("after the save: %+v, changes %d, text %q, dirty %v", res, changes, d.Text(), d.Dirty())
	}
}

func TestExternalLookThatStartedBeforeASaveIsStale(t *testing.T) {
	tests := []struct {
		name  string
		after func(t *testing.T, d *Document)
	}{
		{"saved", func(t *testing.T, d *Document) {
			appendText(t, d, "mine")
			if _, err := d.Save(context.Background(), SaveOptions{IgnoreConflict: true}); err != nil {
				t.Fatal(err)
			}
		}},
		{"renamed", func(t *testing.T, d *Document) { d.SetURI(FileURI(filepath.Join(filepath.Dir(d.URI().Path), "g.txt"))) }},
		{"reloaded", func(t *testing.T, d *Document) {
			d.Reload(fileio.ReadResult{Text: "x\n", Encoding: fileio.UTF8, LineEnding: fileio.LF, Stat: fileio.FileStat{Size: 2}})
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := openAt(t, t.TempDir(), "f.txt", "one\n", 0)
			putFile(t, d.URI().Path, "theirs\n", 10)
			job, err := d.BeginExternal(fileio.ReadOptions{})
			if err != nil {
				t.Fatal(err)
			}
			probe := job.Probe(context.Background(), nil)
			if probe.Read == nil {
				t.Fatalf("probe %+v", probe)
			}
			tt.after(t, d)
			text := d.Text()
			if res := d.ApplyExternal(job, probe); res.Outcome != ExternalStale || d.Text() != text {
				t.Errorf("result %+v, text %q", res, d.Text())
			}
		})
	}
}

func TestExternalDocumentThatBecameCleanDuringTheLookIsLookedAtAgain(t *testing.T) {
	d := openAt(t, t.TempDir(), "f.txt", "one\n", 0)
	appendText(t, d, "mine")
	putFile(t, d.URI().Path, "theirs\n", 10)
	job, _ := d.BeginExternal(fileio.ReadOptions{}) // dirty: the probe reads nothing
	probe := job.Probe(context.Background(), nil)
	if probe.Read != nil {
		t.Fatal("read a file for a dirty document")
	}
	if _, err := d.Undo(); err != nil {
		t.Fatal(err)
	}
	if res := d.ApplyExternal(job, probe); res.Outcome != ExternalStale {
		t.Errorf("outcome %v", res.Outcome)
	}
}

func TestExternalDocumentEditedDuringTheReadIsConflicted(t *testing.T) {
	d := openAt(t, t.TempDir(), "f.txt", "one\n", 0)
	putFile(t, d.URI().Path, "theirs\n", 10)
	job, _ := d.BeginExternal(fileio.ReadOptions{})
	probe := job.Probe(context.Background(), nil)
	appendText(t, d, "mine") // typed while the file was read
	res := d.ApplyExternal(job, probe)
	if res.Outcome != ExternalConflicted || d.Text() != "one\nmine" {
		t.Errorf("result %+v, text %q", res, d.Text())
	}
}

func TestExternalDeleted(t *testing.T) {
	tests := []struct {
		name  string
		dirty bool
	}{{"clean", false}, {"dirty", true}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := openAt(t, t.TempDir(), "f.txt", "one\n", 0)
			if tt.dirty {
				appendText(t, d, "mine")
			}
			text := d.Text()
			log := watchState(d)
			if err := os.Remove(d.URI().Path); err != nil {
				t.Fatal(err)
			}
			res := check(t, d)
			if res.Outcome != ExternalOrphaned || !res.StateChanged {
				t.Fatalf("result %+v", res)
			}
			if !d.Orphaned() || d.Conflicted() || d.Dirty() != tt.dirty || d.Text() != text {
				t.Errorf("orphaned %v, conflicted %v, dirty %v, text %q", d.Orphaned(), d.Conflicted(), d.Dirty(), d.Text())
			}
			// the same news again changes nothing
			if res := check(t, d); res.Outcome != ExternalOrphaned || res.StateChanged {
				t.Errorf("second look %+v", res)
			}
			if want := []ExternalState{{Orphaned: true}}; !slices.Equal(log.states, want) {
				t.Errorf("states %v", log.states)
			}
		})
	}
}

func TestExternalDeletedAfterAConflictIsOrphaned(t *testing.T) {
	d := openAt(t, t.TempDir(), "f.txt", "one\n", 0)
	appendText(t, d, "mine")
	putFile(t, d.URI().Path, "theirs\n", 10)
	check(t, d)
	if err := os.Remove(d.URI().Path); err != nil {
		t.Fatal(err)
	}
	check(t, d)
	if d.ExternalState() != (ExternalState{Orphaned: true}) {
		t.Errorf("state %+v", d.ExternalState())
	}
}

func TestExternalSaveRecreatesADeletedFile(t *testing.T) {
	d := openAt(t, t.TempDir(), "f.txt", "one\n", 0)
	appendText(t, d, "mine")
	if err := os.Remove(d.URI().Path); err != nil {
		t.Fatal(err)
	}
	check(t, d)
	log := watchState(d)
	if _, err := d.Save(context.Background(), SaveOptions{}); err != nil {
		t.Fatal(err)
	}
	if d.Orphaned() || d.Dirty() || string(readDisk(t, d.URI().Path)) != "one\nmine" {
		t.Errorf("orphaned %v, dirty %v, disk %q", d.Orphaned(), d.Dirty(), readDisk(t, d.URI().Path))
	}
	if want := []ExternalState{{}}; !slices.Equal(log.states, want) {
		t.Errorf("states %v", log.states)
	}
	if res := check(t, d); res.Outcome != ExternalUnchanged {
		t.Errorf("outcome %v", res.Outcome)
	}
}

func TestExternalDeletedFolderAndReplacedByFolder(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name   string
		damage func(t *testing.T, path string)
	}{
		{"the folder above is gone", func(t *testing.T, path string) {
			if err := os.RemoveAll(filepath.Dir(path)); err != nil {
				t.Fatal(err)
			}
		}},
		{"a file where the folder was", func(t *testing.T, path string) {
			if err := os.RemoveAll(filepath.Dir(path)); err != nil {
				t.Fatal(err)
			}
			putFile(t, filepath.Dir(path), "x", 0)
		}},
		{"a folder where the file was", func(t *testing.T, path string) {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(path, 0o755); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sub := filepath.Join(dir, string(rune('a'+i)))
			if err := os.Mkdir(sub, 0o755); err != nil {
				t.Fatal(err)
			}
			d := openAt(t, sub, "f.txt", "one\n", 0)
			tt.damage(t, d.URI().Path)
			if res := check(t, d); res.Outcome != ExternalOrphaned {
				t.Errorf("outcome %v", res.Outcome)
			}
		})
	}
}

func TestExternalFileReappears(t *testing.T) {
	tests := []struct {
		name        string
		dirty       bool
		back        string
		backTime    int
		wantOutcome ExternalOutcome
		wantText    string
		wantState   ExternalState
	}{
		{"clean, other content", false, "restored\n", 5, ExternalReloaded, "restored\n", ExternalState{}},
		{"clean, same stat", false, "one\n", 0, ExternalUnchanged, "one\n", ExternalState{}},
		{"dirty, same stat", true, "one\n", 0, ExternalUnchanged, "one\nmine", ExternalState{}},
		{"dirty, other stat", true, "restored\n", 5, ExternalConflicted, "one\nmine", ExternalState{Conflicted: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := openAt(t, t.TempDir(), "f.txt", "one\n", 0)
			if tt.dirty {
				appendText(t, d, "mine")
			}
			if err := os.Remove(d.URI().Path); err != nil {
				t.Fatal(err)
			}
			check(t, d)
			if !d.Orphaned() {
				t.Fatal("not orphaned")
			}
			putFile(t, d.URI().Path, tt.back, tt.backTime)
			res := check(t, d)
			if res.Outcome != tt.wantOutcome || d.Text() != tt.wantText || d.ExternalState() != tt.wantState || d.Dirty() != tt.dirty {
				t.Errorf("result %+v, text %q, state %+v, dirty %v", res, d.Text(), d.ExternalState(), d.Dirty())
			}
		})
	}
}

func TestExternalDocumentsWithoutAFileAreIgnored(t *testing.T) {
	tests := []struct {
		name string
		doc  *Document
	}{
		{"untitled", New(UntitledURI(1), "x", NewOptions())},
		{"virtual", New(VirtualURI("defaults.json"), "x", NewOptions())},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.doc.CheckExternal(context.Background(), nil, fileio.ReadOptions{})
			wantTag(t, err, "CheckExternal->Document.BeginExternal", "ExternalNoFile")
			if tt.doc.Orphaned() || tt.doc.Conflicted() {
				t.Error("state changed")
			}
		})
	}
}

func TestExternalReadFailureIsRetriedAtTheNextLook(t *testing.T) {
	d := openAt(t, t.TempDir(), "f.txt", "one\n", 0)
	putFile(t, d.URI().Path, "two\n", 10)
	disk := &hookDisk{readErr: errors.New("FilePermissionDenied", "no access", "path", d.URI().Path)}
	res, err := d.CheckExternal(context.Background(), disk, fileio.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != ExternalRetry || d.Text() != "one\n" || d.Dirty() {
		t.Fatalf("result %+v, text %q", res, d.Text())
	}
	wantTag(t, res.Err, "FilePermissionDenied", "FilePermissionDenied")
	// the next event finds the file readable
	if res := check(t, d); res.Outcome != ExternalReloaded || d.Text() != "two\n" {
		t.Errorf("second look %+v, text %q", res, d.Text())
	}
}

func TestExternalFailures(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name  string
		setup func(t *testing.T) (*Document, *hookDisk)
		route string
		tag   string
	}{
		{"a file that became binary", func(t *testing.T) (*Document, *hookDisk) {
			d := openAt(t, dir, "bin.txt", "text\n", 0)
			putFile(t, d.URI().Path, "a\x00b\x00\x01\x02", 10)
			return d, nil
		}, "Document.CheckExternal->ExternalJob.Probe->OSDisk.Read->Read.FileBinary", "FileBinary"},
		{"a stat that fails", func(t *testing.T) (*Document, *hookDisk) {
			d := openAt(t, dir, "stat.txt", "text\n", 0)
			return d, &hookDisk{statErr: os.ErrDeadlineExceeded}
		}, "Document.CheckExternal->ExternalJob.Probe.ExternalStatFailed", "ExternalStatFailed"},
		{"a read that fails with a plain error", func(t *testing.T) (*Document, *hookDisk) {
			d := openAt(t, dir, "plain.txt", "text\n", 0)
			putFile(t, d.URI().Path, "other\n", 10)
			return d, &hookDisk{readErr: os.ErrInvalid}
		}, "Document.CheckExternal->ExternalJob.Probe.ExternalReadFailed", "ExternalReadFailed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, disk := tt.setup(t)
			var disker Disk
			if disk != nil {
				disker = disk
			}
			res, err := d.CheckExternal(context.Background(), disker, fileio.ReadOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if res.Outcome != ExternalRetry || res.Err == nil {
				t.Fatalf("result %+v", res)
			}
			wantTag(t, res.Err, tt.route, tt.tag)
			if d.Text() != "text\n" || d.Dirty() || d.Orphaned() || d.Conflicted() {
				t.Errorf("the document changed: %q", d.Text())
			}
		})
	}
}

func TestExternalStatOfAFolderWithoutPermission(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can look into any folder")
	}
	dir := t.TempDir()
	sub := filepath.Join(dir, "closed")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	d := openAt(t, sub, "f.txt", "one\n", 0)
	if err := os.Chmod(sub, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(sub, 0o755) })
	res, err := d.CheckExternal(context.Background(), nil, fileio.ReadOptions{})
	if err != nil || res.Outcome != ExternalRetry {
		t.Fatalf("result %+v, err %v", res, err)
	}
	wantTag(t, res.Err, "ExternalJob.Probe->OSDisk.Stat.ExternalStatFailed", "ExternalStatFailed")
}

func TestExternalFileBeingWrittenIsLeftForTheNextEvent(t *testing.T) {
	tests := []struct {
		name string
		hook func(h *hookDisk, path string)
	}{
		{"changes between the look and the read", func(h *hookDisk, path string) {
			h.beforeRd = func() { putFile(t, path, "half of a write that goes on", 20) }
		}},
		{"changes during the read", func(h *hookDisk, path string) {
			h.afterRd = func() { putFile(t, path, "one\ntwo\nthree\n", 21) }
		}},
		{"is gone at the read", func(h *hookDisk, path string) {
			h.beforeRd = func() {
				if err := os.Remove(path); err != nil {
					t.Error(err)
				}
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := openAt(t, t.TempDir(), "f.txt", "one\n", 0)
			putFile(t, d.URI().Path, "one\ntwo\n", 10)
			disk := &hookDisk{}
			tt.hook(disk, d.URI().Path)
			res, err := d.CheckExternal(context.Background(), disk, fileio.ReadOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if res.Outcome != ExternalRetry || res.Err != nil || d.Text() != "one\n" {
				t.Fatalf("result %+v, text %q", res, d.Text())
			}
			// the watcher reports the end of the write (or the delete): the next look acts on it
			if _, err := os.Stat(d.URI().Path); err != nil {
				putFile(t, d.URI().Path, "done\n", 30)
			}
			if res := check(t, d); res.Outcome != ExternalReloaded && res.Outcome != ExternalUnchanged {
				t.Errorf("next look %+v", res)
			}
		})
	}
}

func TestExternalStateBlocksAutoSave(t *testing.T) {
	f := newAutoFixture(t, after(time.Second))
	d := openAt(t, f.dir, "a.txt", "one\n", 0)
	f.auto.Track(d)
	appendText(t, d, "x")
	if f.clock.pending() != 1 {
		t.Fatalf("%d timers", f.clock.pending())
	}
	putFile(t, d.URI().Path, "theirs\n", 10)
	check(t, d)
	// the conflict keeps the document from being saved by itself
	f.clock.advance(time.Hour)
	if len(f.host.saves) != 0 || f.auto.Check(d, SaveAuto) != AutoSaveBlockedExternal {
		t.Fatalf("saves %v, check %v", f.host.saves, f.auto.Check(d, SaveAuto))
	}
	if f.auto.OnShutdown(d) {
		t.Error("a conflicted document is saved at shutdown")
	}
	// the file is back as it was: the timer starts again
	putFile(t, d.URI().Path, "one\n", 0)
	check(t, d)
	f.clock.advance(time.Second)
	if len(f.host.saves) != 1 {
		t.Errorf("saves %v", f.host.saves)
	}
	// an orphaned document is blocked too
	appendText(t, d, "y")
	if err := os.Remove(d.URI().Path); err != nil {
		t.Fatal(err)
	}
	check(t, d)
	if f.auto.Check(d, SaveAuto) != AutoSaveBlockedExternal {
		t.Errorf("check %v", f.auto.Check(d, SaveAuto))
	}
	f.clock.advance(time.Hour)
	if len(f.host.saves) != 1 {
		t.Errorf("an orphaned document was saved by itself: %v", f.host.saves)
	}
}

func TestAbortedSaveIsNotInFlight(t *testing.T) {
	dir := t.TempDir()
	d := New(FileURI(filepath.Join(dir, "no", "f.txt")), "x", NewOptions())
	job, err := d.BeginSave(SaveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !d.SaveInFlight() {
		t.Fatal("not in flight")
	}
	if _, err := job.Write(context.Background()); err == nil {
		t.Fatal("the folder is missing: expected an error")
	}
	job.Abort()
	if d.SaveInFlight() {
		t.Error("an aborted save is in flight")
	}
	// Save aborts its own job when the write fails
	if _, err := d.Save(context.Background(), SaveOptions{}); err == nil || d.SaveInFlight() {
		t.Errorf("err %v, in flight %v", err, d.SaveInFlight())
	}
}

func TestSameStat(t *testing.T) {
	at := base
	tests := []struct {
		name string
		a, b fileio.FileStat
		want bool
	}{
		{"equal", fileio.FileStat{ModTime: at, Size: 3}, fileio.FileStat{ModTime: at, Size: 3}, true},
		{"mode is not compared", fileio.FileStat{ModTime: at, Size: 3, Mode: 0o644}, fileio.FileStat{ModTime: at, Size: 3, Mode: 0o600}, true},
		{"same instant in another zone", fileio.FileStat{ModTime: at, Size: 3}, fileio.FileStat{ModTime: at.In(time.FixedZone("x", 3600)), Size: 3}, true},
		{"size", fileio.FileStat{ModTime: at, Size: 3}, fileio.FileStat{ModTime: at, Size: 4}, false},
		{"time", fileio.FileStat{ModTime: at, Size: 3}, fileio.FileStat{ModTime: at.Add(time.Nanosecond), Size: 3}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SameStat(tt.a, tt.b); got != tt.want {
				t.Errorf("SameStat = %v", got)
			}
		})
	}
}

func TestExternalOutcomeNames(t *testing.T) {
	want := map[ExternalOutcome]string{
		ExternalUnchanged: "unchanged", ExternalReloaded: "reloaded", ExternalConflicted: "conflicted", ExternalOrphaned: "orphaned",
		ExternalRetry: "retry", ExternalStale: "stale", ExternalDeferred: "deferred", ExternalOutcome(99): "unknown",
	}
	for o, name := range want {
		if o.String() != name {
			t.Errorf("%d is %q, want %q", int(o), o.String(), name)
		}
	}
}
