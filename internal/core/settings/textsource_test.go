package settings

import (
	"context"
	"os"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/jsonc"
	"github.com/rm4n0s/errors"
)

// fakeSource is a TextSource over a map of open documents: it applies the edits to the text and saves
// the document (writes the file) when it was not dirty, as the app's source does.
type fakeSource struct {
	f     *fixture
	texts map[string]string
	dirty map[string]bool
	saves int
	edits int
	// vanish makes EditText report that the document is gone.
	vanish bool
}

func (s *fakeSource) OpenText(path string) (string, bool) {
	t, ok := s.texts[path]
	return t, ok
}

func (s *fakeSource) EditText(_ context.Context, path string, edits []jsonc.Edit) (bool, error) {
	text, ok := s.texts[path]
	if !ok || s.vanish {
		return false, nil
	}
	updated, err := jsonc.ApplyEdits(text, edits)
	if err != nil {
		return false, err
	}
	s.edits++
	s.texts[path] = updated
	if !s.dirty[path] {
		s.saves++
		s.f.write(path, updated)
	}
	return true, nil
}

func newSourceFixture(t *testing.T) (*fixture, *fakeSource) {
	t.Helper()
	f := newFixture(t)
	src := &fakeSource{f: f, texts: map[string]string{}, dirty: map[string]bool{}}
	f.store.SetTextSource(src)
	return f, src
}

func TestStoreTextSource(t *testing.T) {
	ctx := context.Background()
	const original = "{\n  // keep me\n  \"editor.tabSize\": 2\n}\n"
	t.Run("a dirty document gets the edit and the file is not written", func(t *testing.T) {
		f, src := newSourceFixture(t)
		f.write(f.userPath, original)
		typed := "{\n  // keep me\n  \"editor.tabSize\": 2,\n  // typed, not saved\n  \"editor.insertSpaces\": false\n}\n"
		src.texts[f.userPath], src.dirty[f.userPath] = typed, true
		f.reload()
		if Get[bool](f.store, "editor.insertSpaces", "") {
			t.Fatal("the store does not read the text of the document")
		}
		if err := f.store.Update(ctx, "files.eol", "\n", TargetUser); err != nil {
			t.Fatal(err)
		}
		if got := f.read(f.userPath); got != original {
			t.Errorf("the file changed: %q", got)
		}
		want := "{\n  // keep me\n  \"editor.tabSize\": 2,\n  // typed, not saved\n  \"editor.insertSpaces\": false,\n  \"files.eol\": \"\\n\"\n}\n"
		if got := src.texts[f.userPath]; got != want {
			t.Errorf("document %q, want %q", got, want)
		}
		if src.saves != 0 || src.edits != 1 {
			t.Errorf("saves %d edits %d", src.saves, src.edits)
		}
		if Get[string](f.store, "files.eol", "") != "\n" || Get[bool](f.store, "editor.insertSpaces", "") {
			t.Error("the settings do not follow the document")
		}
	})
	t.Run("a clean document gets the edit and is saved", func(t *testing.T) {
		f, src := newSourceFixture(t)
		f.write(f.userPath, original)
		src.texts[f.userPath] = original
		if err := f.store.Update(ctx, "editor.tabSize", 8, TargetUser); err != nil {
			t.Fatal(err)
		}
		want := "{\n  // keep me\n  \"editor.tabSize\": 8\n}\n"
		if got := f.read(f.userPath); got != want || src.texts[f.userPath] != want {
			t.Errorf("file %q document %q", got, src.texts[f.userPath])
		}
		if src.saves != 1 {
			t.Errorf("saves %d", src.saves)
		}
		if Get[int](f.store, "editor.tabSize", "") != 8 {
			t.Error("value")
		}
	})
	t.Run("no document writes the file", func(t *testing.T) {
		f, src := newSourceFixture(t)
		f.write(f.userPath, original)
		if err := f.store.Update(ctx, "editor.tabSize", 8, TargetUser); err != nil {
			t.Fatal(err)
		}
		if got := f.read(f.userPath); got != "{\n  // keep me\n  \"editor.tabSize\": 8\n}\n" {
			t.Errorf("file %q", got)
		}
		if src.edits != 0 {
			t.Error("the source was used")
		}
	})
	t.Run("a document that closed meanwhile writes the file", func(t *testing.T) {
		f, src := newSourceFixture(t)
		f.write(f.userPath, original)
		src.texts[f.userPath], src.vanish = original, true
		if err := f.store.Update(ctx, "editor.tabSize", 8, TargetUser); err != nil {
			t.Fatal(err)
		}
		if got := f.read(f.userPath); got != "{\n  // keep me\n  \"editor.tabSize\": 8\n}\n" {
			t.Errorf("file %q", got)
		}
	})
	t.Run("a removal edits the document too", func(t *testing.T) {
		f, src := newSourceFixture(t)
		f.write(f.userPath, original)
		src.texts[f.userPath], src.dirty[f.userPath] = original, true
		f.reload()
		if err := f.store.Remove(ctx, "editor.tabSize", TargetUser); err != nil {
			t.Fatal(err)
		}
		if got := src.texts[f.userPath]; got != "{\n}\n" {
			t.Errorf("document %q", got)
		}
		if got := f.read(f.userPath); got != original {
			t.Errorf("file %q", got)
		}
		if Get[int](f.store, "editor.tabSize", "") != 4 {
			t.Error("the default is not back")
		}
	})
	t.Run("the workspace document", func(t *testing.T) {
		f, src := newSourceFixture(t)
		src.texts[f.wsPath], src.dirty[f.wsPath] = "{}", true
		if err := f.store.Update(ctx, "editor.tabSize", 3, TargetWorkspace); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(f.wsPath); err == nil {
			t.Error("the file was written")
		}
		if Get[int](f.store, "editor.tabSize", "") != 3 {
			t.Error("value")
		}
	})
	t.Run("the change events come after the edit", func(t *testing.T) {
		f, src := newSourceFixture(t)
		src.texts[f.userPath], src.dirty[f.userPath] = "{}", true
		if err := f.store.Update(ctx, "editor.tabSize", 3, TargetUser); err != nil {
			t.Fatal(err)
		}
		if len(f.listeners) != 1 || !f.listeners[0].Affects("editor.tabSize", "") {
			t.Errorf("events %+v", f.listeners)
		}
	})
	t.Run("a setting that is as asked edits nothing", func(t *testing.T) {
		f, src := newSourceFixture(t)
		src.texts[f.userPath], src.dirty[f.userPath] = original, true
		f.reload()
		if err := f.store.Update(ctx, "editor.tabSize", 2, TargetUser); err != nil {
			t.Fatal(err)
		}
		if src.edits != 0 {
			t.Error("the document was edited")
		}
	})
	t.Run("a document with errors fails as a file would", func(t *testing.T) {
		f, src := newSourceFixture(t)
		src.texts[f.userPath] = "{ \"editor.tabSize\": "
		err := f.store.Update(ctx, "editor.tabSize", 3, TargetUser)
		appErr, ok := errors.FromError(err)
		if !ok {
			t.Fatalf("got %T %v", err, err)
		}
		if !appErr.HasRoute("Store.write.Store.editDocument.SettingsFileInvalid") && !appErr.HasRoute("SettingsFileInvalid") {
			t.Errorf("route %s", appErr.Route())
		}
		if src.edits != 0 {
			t.Error("the document was edited")
		}
	})
}
