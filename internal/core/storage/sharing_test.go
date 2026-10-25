package storage_test

import (
	"context"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/core/storage/storagetest"
)

// matchExact is the matcher of the tests: a pattern matches the topic it is equal to, or "*" matches anything.
func matchExact(pattern, topic string) bool { return pattern == "*" || pattern == topic }

func newSharing(t *testing.T) (*storage.SharingStore, storage.Paths) {
	t.Helper()
	paths := storagetest.Isolate(t)
	s := storage.NewSharingStore(paths, storage.SharingStoreOptions{Logger: slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))})
	if err := s.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s, paths
}

func TestSharingLinksAreDirectionalAndLimitedByTopic(t *testing.T) {
	s, _ := newSharing(t)
	ctx := context.Background()
	if err := s.AddLink(ctx, storage.SharingLink{From: "/notes", To: "/office", Events: []string{"a.b"}}); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name           string
		from, to, topc string
		want           bool
	}{
		{"along the link", "/notes", "/office", "a.b", true},
		{"against the link", "/office", "/notes", "a.b", false},
		{"another topic", "/notes", "/office", "a.c", false},
		{"another scope", "/notes", "/other", "a.b", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := s.Allows(tt.from, tt.to, tt.topc, matchExact); got != tt.want {
				t.Errorf("Allows(%s, %s, %s) = %v, want %v", tt.from, tt.to, tt.topc, got, tt.want)
			}
		})
	}
}

func TestSharingLinkWithoutPatternsCarriesEveryTopic(t *testing.T) {
	s, _ := newSharing(t)
	ctx := context.Background()
	if err := s.AddLink(ctx, storage.SharingLink{From: "/a", To: "/b", Events: []string{"x"}}); err != nil {
		t.Fatal(err)
	}
	if s.Allows("/a", "/b", "y", matchExact) {
		t.Fatal("y must not pass before the link carries everything")
	}
	if err := s.AddLink(ctx, storage.SharingLink{From: "/a", To: "/b"}); err != nil {
		t.Fatal(err)
	}
	if !s.Allows("/a", "/b", "y", matchExact) {
		t.Error("a link without patterns carries every topic")
	}
	if got := s.Snapshot().Links; len(got) != 1 || len(got[0].Events) != 0 {
		t.Errorf("links = %+v, want one link with no patterns", got)
	}
}

func TestSharingMergesPatternsOfTheSamePair(t *testing.T) {
	s, _ := newSharing(t)
	ctx := context.Background()
	for _, p := range []string{"x", "y", "x"} {
		if err := s.AddLink(ctx, storage.SharingLink{From: "/a", To: "/b", Events: []string{p}}); err != nil {
			t.Fatal(err)
		}
	}
	want := []storage.SharingLink{{From: "/a", To: "/b", Events: []string{"x", "y"}}}
	if got := s.Snapshot().Links; !reflect.DeepEqual(got, want) {
		t.Errorf("links = %+v, want %+v", got, want)
	}
}

func TestSharingPrivateStopsEverythingFromTheScope(t *testing.T) {
	s, _ := newSharing(t)
	ctx := context.Background()
	if err := s.AddLink(ctx, storage.SharingLink{From: "/a", To: "/b"}); err != nil {
		t.Fatal(err)
	}
	if changed, err := s.SetPrivate(ctx, "/a", true); err != nil || !changed {
		t.Fatalf("SetPrivate = %v, %v", changed, err)
	}
	if s.Allows("/a", "/b", "x", matchExact) || !s.IsPrivate("/a") {
		t.Error("a private scope must send nothing, whatever the links say")
	}
	if changed, err := s.SetPrivate(ctx, "/a", true); err != nil || changed {
		t.Errorf("a second SetPrivate = %v, %v; want no change", changed, err)
	}
	if _, err := s.SetPrivate(ctx, "/a", false); err != nil {
		t.Fatal(err)
	}
	if !s.Allows("/a", "/b", "x", matchExact) {
		t.Error("the link works again when the scope is not private")
	}
}

func TestSharingSessionLinksAreNeverWritten(t *testing.T) {
	s, paths := newSharing(t)
	s.AddSessionLink(storage.SharingLink{From: "/a", To: "/b", Events: []string{"x"}})
	if !s.Allows("/a", "/b", "x", matchExact) {
		t.Fatal("a session link must work")
	}
	if _, err := os.Stat(paths.EventSharingPath()); !os.IsNotExist(err) {
		t.Errorf("the file exists after a session link only: %v", err)
	}
	// a saved change writes the file, without the session link
	if _, err := s.SetPrivate(context.Background(), "/c", true); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(paths.EventSharingPath())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "/b") {
		t.Errorf("the file holds the session link:\n%s", data)
	}
	if _, ok, err := s.RemoveLink(context.Background(), "/a", "/b", true); err != nil || !ok {
		t.Fatalf("RemoveLink(session) = %v, %v", ok, err)
	}
	if s.Allows("/a", "/b", "x", matchExact) {
		t.Error("a removed session link must stop at once")
	}
}

func TestSharingFileSurvivesARestart(t *testing.T) {
	s, paths := newSharing(t)
	ctx := context.Background()
	if err := s.AddLink(ctx, storage.SharingLink{From: "/notes", To: "/office", Events: []string{"emetos.document.*"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetPrivate(ctx, "/secret", true); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(paths.EventSharingPath())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != fs.FileMode(0o600) {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}
	again := storage.NewSharingStore(paths, storage.SharingStoreOptions{})
	if err := again.Load(ctx); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(again.Snapshot(), s.Snapshot()) {
		t.Errorf("after the restart %+v, want %+v", again.Snapshot(), s.Snapshot())
	}
	if !again.IsPrivate("/secret") || !again.Allows("/notes", "/office", "emetos.document.changed", func(p, topic string) bool {
		return strings.HasPrefix(topic, "emetos.document.") && p == "emetos.document.*"
	}) {
		t.Error("the restarted store lost the link or the private scope")
	}
	data, err := os.ReadFile(paths.EventSharingPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"version": 1`) {
		t.Errorf("the file has no version:\n%s", data)
	}
}

func TestSharingRemoveLinkWritesTheFile(t *testing.T) {
	s, paths := newSharing(t)
	ctx := context.Background()
	if err := s.AddLink(ctx, storage.SharingLink{From: "/a", To: "/b", Events: []string{"x"}}); err != nil {
		t.Fatal(err)
	}
	removed, ok, err := s.RemoveLink(ctx, "/a", "/b", false)
	if err != nil || !ok || removed.From != "/a" || !reflect.DeepEqual(removed.Events, []string{"x"}) {
		t.Fatalf("RemoveLink = %+v, %v, %v", removed, ok, err)
	}
	if _, ok, err := s.RemoveLink(ctx, "/a", "/b", false); err != nil || ok {
		t.Errorf("a second RemoveLink = %v, %v; want nothing removed", ok, err)
	}
	again := storage.NewSharingStore(paths, storage.SharingStoreOptions{})
	if err := again.Load(ctx); err != nil {
		t.Fatal(err)
	}
	if got := again.Snapshot().Links; len(got) != 0 {
		t.Errorf("the removed link is back after a restart: %+v", got)
	}
}

func TestSharingLoadIgnoresUnusableFiles(t *testing.T) {
	tests := []struct {
		name, content, route string
	}{
		{"not JSON", "{not json", "SharingStore.Load.SharingFileCorrupt"},
		{"another version", `{"version": 7, "links": [{"from":"/a","to":"/b"}], "private": []}`, "SharingStore.Load.SharingVersionUnsupported"},
		{"no version", `{"links": [{"from":"/a","to":"/b"}]}`, "SharingStore.Load.SharingVersionUnsupported"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paths := storagetest.Isolate(t)
			if err := os.MkdirAll(filepath.Dir(paths.EventSharingPath()), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(paths.EventSharingPath(), []byte(tt.content), 0o600); err != nil {
				t.Fatal(err)
			}
			s := storage.NewSharingStore(paths, storage.SharingStoreOptions{})
			err := s.Load(context.Background())
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tt.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
			if snap := s.Snapshot(); len(snap.Links) != 0 || len(snap.Private) != 0 {
				t.Errorf("an unusable file must give an empty store, got %+v", snap)
			}
			// the next change replaces the file
			if _, err := s.SetPrivate(context.Background(), "/x", true); err != nil {
				t.Fatal(err)
			}
			again := storage.NewSharingStore(paths, storage.SharingStoreOptions{})
			if err := again.Load(context.Background()); err != nil || !again.IsPrivate("/x") {
				t.Errorf("the file was not replaced: %v", err)
			}
		})
	}
}

func TestSharingLoadUnreadableFile(t *testing.T) {
	paths := storagetest.Isolate(t)
	// a folder where the file should be cannot be read as a file
	if err := os.MkdirAll(paths.EventSharingPath(), 0o700); err != nil {
		t.Fatal(err)
	}
	s := storage.NewSharingStore(paths, storage.SharingStoreOptions{})
	err := s.Load(context.Background())
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("SharingStore.Load.SharingFileUnreadable") {
		t.Fatalf("unexpected failure: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = storage.NewSharingStore(paths, storage.SharingStoreOptions{}).Load(ctx)
	if appErr, ok := errors.FromError(err); !ok || !appErr.HasRoute("SharingStore.Load.SharingFileUnreadable") {
		t.Fatalf("a cancelled context: unexpected failure %v", err)
	}
}

func TestSharingLoadDropsLinksThatCannotBeUsed(t *testing.T) {
	paths := storagetest.Isolate(t)
	if err := os.MkdirAll(filepath.Dir(paths.EventSharingPath()), 0o700); err != nil {
		t.Fatal(err)
	}
	content := `{"version":1,"links":[{"from":"","to":"/b"},{"from":"/a","to":"/a"},{"from":"/a","to":"/b","events":["x"]}],"private":["","/p","/p"]}`
	if err := os.WriteFile(paths.EventSharingPath(), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	s := storage.NewSharingStore(paths, storage.SharingStoreOptions{})
	if err := s.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	snap := s.Snapshot()
	if want := []storage.SharingLink{{From: "/a", To: "/b", Events: []string{"x"}}}; !reflect.DeepEqual(snap.Links, want) {
		t.Errorf("links = %+v, want %+v", snap.Links, want)
	}
	if want := []string{"/p"}; !reflect.DeepEqual(snap.Private, want) {
		t.Errorf("private = %v, want %v", snap.Private, want)
	}
}

func TestSharingWriteFailure(t *testing.T) {
	paths := storagetest.Isolate(t)
	failing := func(string, []byte) error { return os.ErrPermission }
	s := storage.NewSharingStore(paths, storage.SharingStoreOptions{WriteFile: failing})
	ctx := context.Background()
	err := s.AddLink(ctx, storage.SharingLink{From: "/a", To: "/b"})
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("SharingStore.save.SharingWriteFailed") {
		t.Fatalf("unexpected failure: %v", err)
	}
	if len(s.Snapshot().Links) != 0 {
		t.Error("a link that could not be saved must not be kept (the caller keeps a session link)")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := s.AddLink(cancelled, storage.SharingLink{From: "/a", To: "/b"}); err == nil {
		t.Error("a cancelled context must fail the write")
	}
	// a removal that cannot be written still takes effect
	ok2 := storage.NewSharingStore(paths, storage.SharingStoreOptions{})
	if err := ok2.AddLink(ctx, storage.SharingLink{From: "/a", To: "/b"}); err != nil {
		t.Fatal(err)
	}
	removing := storage.NewSharingStore(paths, storage.SharingStoreOptions{WriteFile: failing})
	if err := removing.Load(ctx); err != nil {
		t.Fatal(err)
	}
	_, found, err := removing.RemoveLink(ctx, "/a", "/b", false)
	if !found || err == nil {
		t.Fatalf("RemoveLink = %v, %v; want the link removed and the write error", found, err)
	}
	if removing.Allows("/a", "/b", "x", matchExact) {
		t.Error("the removed link must stop at once, even when the file cannot be written")
	}
}

func TestSharingInMemoryStoreWritesNothing(t *testing.T) {
	s := storage.NewSharingStore(storage.Paths{}, storage.SharingStoreOptions{})
	if s.Path() != "" {
		t.Fatalf("Path = %q", s.Path())
	}
	if err := s.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.AddLink(context.Background(), storage.SharingLink{From: "/a", To: "/b"}); err != nil {
		t.Fatal(err)
	}
	if !s.Allows("/a", "/b", "x", matchExact) {
		t.Error("an in-memory store must keep its links")
	}
}
