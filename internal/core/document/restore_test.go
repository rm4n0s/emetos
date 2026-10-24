package document

import (
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/fileio"
)

func TestRestoreUnsaved(t *testing.T) {
	diskStat := fileio.FileStat{ModTime: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), Size: 4}
	backupStat := fileio.FileStat{ModTime: time.Date(2023, 12, 31, 0, 0, 0, 0, time.UTC), Size: 3}
	conflict := diskStat
	tests := []struct {
		name         string
		uri          URI
		disk         string
		restore      Unsaved
		wantDirty    bool
		wantText     string
		wantUndoText string
		wantOrphaned bool
		wantConflict bool
		wantStat     fileio.FileStat
		wantEOL      fileio.LineEnding
	}{
		{"a file with unsaved changes", FileURI("/a.txt"), "one\n", Unsaved{Text: "one\ntwo\n"}, true, "one\ntwo\n", "one\n", false, false, diskStat, fileio.LF},
		{"the text is the file's", FileURI("/a.txt"), "one\n", Unsaved{Text: "one\n"}, false, "one\n", "one\n", false, false, diskStat, fileio.LF},
		{"the file changed on disk", FileURI("/a.txt"), "disk\n", Unsaved{Text: "mine\n", Stat: backupStat, Conflict: &conflict}, true, "mine\n", "disk\n", false, true, backupStat, fileio.LF},
		{"the file is gone", FileURI("/a.txt"), "", Unsaved{Text: "kept", Stat: backupStat, Orphaned: true}, true, "kept", "", true, false, backupStat, fileio.LF},
		{"the file is gone and the text is empty", FileURI("/a.txt"), "", Unsaved{Orphaned: true}, true, "", "", true, false, diskStat, fileio.LF},
		{"an untitled document", UntitledURI(2), "", Unsaved{Text: "notes", LanguageID: "markdown"}, true, "notes", "", false, false, fileio.FileStat{}, fileio.LF},
		{"properties", FileURI("/a.txt"), "a\n", Unsaved{Text: "a\nb\n", Encoding: fileio.UTF8BOM, HasBOM: true, LineEnding: fileio.CRLF}, true, "a\nb\n", "a\n", false, false, diskStat, fileio.CRLF},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var doc *Document
			if tc.uri.Scheme == SchemeFile {
				doc = FromRead(tc.uri, fileio.ReadResult{Text: tc.disk, Encoding: fileio.UTF8, LineEnding: fileio.LF, Stat: diskStat}, NewOptions())
			} else {
				doc = New(tc.uri, "", NewOptions())
			}
			dirty, err := doc.RestoreUnsaved(tc.restore)
			if err != nil {
				t.Fatal(err)
			}
			if dirty != tc.wantDirty || doc.Dirty() != tc.wantDirty {
				t.Errorf("dirty = %v / %v, want %v", dirty, doc.Dirty(), tc.wantDirty)
			}
			if doc.Text() != tc.wantText {
				t.Errorf("text = %q, want %q", doc.Text(), tc.wantText)
			}
			if doc.Orphaned() != tc.wantOrphaned || doc.Conflicted() != tc.wantConflict {
				t.Errorf("orphaned %v conflicted %v, want %v %v", doc.Orphaned(), doc.Conflicted(), tc.wantOrphaned, tc.wantConflict)
			}
			if tc.wantConflict && doc.DiskStat() != diskStat {
				t.Errorf("disk stat = %+v", doc.DiskStat())
			}
			if tc.uri.Scheme == SchemeFile && doc.Stat() != tc.wantStat {
				t.Errorf("stat = %+v, want %+v", doc.Stat(), tc.wantStat)
			}
			if doc.LineEnding() != tc.wantEOL {
				t.Errorf("line ending = %q", doc.LineEnding())
			}
			if tc.restore.Encoding != "" && (doc.Encoding() != tc.restore.Encoding || !doc.HasBOM()) {
				t.Errorf("encoding %q bom %v", doc.Encoding(), doc.HasBOM())
			}
			if tc.restore.LanguageID != "" && doc.LanguageID() != tc.restore.LanguageID {
				t.Errorf("language = %q", doc.LanguageID())
			}
			if tc.restore.Text != tc.disk {
				// an undo goes back to the text the document was made with
				if _, err := doc.Undo(); err != nil {
					t.Fatal(err)
				}
				if doc.Text() != tc.wantUndoText {
					t.Errorf("after undo: %q, want %q", doc.Text(), tc.wantUndoText)
				}
			}
		})
	}
}

func TestRestoreUnsavedInAReadOnlyDocumentFails(t *testing.T) {
	doc := New(FileURI("/a.txt"), "x", Options{ReadOnly: true})
	_, err := doc.RestoreUnsaved(Unsaved{Text: "y"})
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("Document.RestoreUnsaved->Document.checkWritable.DocumentReadOnly") {
		t.Errorf("unexpected failure: %v", err)
	}
}

func TestRestoredConflictFailsTheSaveUntilItOverwrites(t *testing.T) {
	path := t.TempDir() + "/a.txt"
	writeFile(t, path, "one\n", 0o644)
	res, err := fileio.Read(t.Context(), path, fileio.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	doc := FromRead(FileURI(path), res, NewOptions())
	old := fileio.FileStat{ModTime: res.Stat.ModTime.Add(-time.Hour), Size: 2}
	disk := res.Stat
	if _, err := doc.RestoreUnsaved(Unsaved{Text: "mine\n", Stat: old, Conflict: &disk}); err != nil {
		t.Fatal(err)
	}
	_, err = doc.Save(t.Context(), SaveOptions{})
	if appErr, ok := errors.FromError(err); !ok || appErr.Tag != "FileModifiedSince" {
		t.Fatalf("save: %v, want FileModifiedSince", err)
	}
	if _, err := doc.Save(t.Context(), SaveOptions{IgnoreConflict: true}); err != nil {
		t.Fatal(err)
	}
	if doc.Dirty() || doc.Conflicted() {
		t.Error("the overwrite did not clear the state")
	}
}
