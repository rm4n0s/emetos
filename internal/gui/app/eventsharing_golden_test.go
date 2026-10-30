package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/ui"
	"golang.org/x/sys/unix"

	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// The goldens of event sharing: the page "Extensions > Event Sharing" and the question of a link.

// goldenSharingRoot is the folder of the tabs of the goldens.
const goldenSharingRoot = "/tmp/emetos-golden-sharing"

// goldenSharing is the receiving tab ("office") shown in a window of the theme, with a second tab ("notes") that sends.
// prepare writes event-sharing.json before the process starts, given the folders of the tabs.
func goldenSharing(t *testing.T, theme string, height float64, prepare func(store *storage.SharingStore, root string)) (*sharingWorld, *groupApp) {
	t.Helper()
	paths := testutil.Isolate(t)
	// the page and the question show the folders, so the folders have a fixed name: a golden cannot hold a random one
	root := goldenSharingRoot
	lockGoldenSharingRoot(t)
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Errorf("removing %s: %v", root, err)
		}
	})
	if prepare != nil {
		store := storage.NewSharingStore(paths, storage.SharingStoreOptions{})
		prepare(store, root)
	}
	w := openSharingWorld(t, paths, &sharer{}, root, map[string]any{
		"workbench.colorTheme": theme, "editor.minimap.enabled": false, "breadcrumbs.enabled": false, "window.menuBarVisibility": "classic",
	})
	a := &groupApp{t: t, win: w.office, files: map[string]string{}, docs: map[string]*document.Document{}}
	a.ts = testutil.New(t, ui.Size{Width: 960, Height: height})
	a.ts.SetPlatformConfiguration(func(c *ui.PlatformConfiguration) { c.UIFontFamily, c.UIFontSize = testutil.MonoFamily, 12 })
	a.ts.PumpWidget(newAppDocs(context.Background(), w.office, nil, nil, ""))
	a.pump()
	return w, a
}

// lockGoldenSharingRoot holds a lock beside the fixed folder until the test ends, so that two test processes (two worktrees
// checked at once) do not remove each other's folders.
func lockGoldenSharingRoot(t *testing.T) {
	t.Helper()
	f, err := os.OpenFile(goldenSharingRoot+".lock", os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := f.Close(); err != nil { // closing releases the lock
			t.Errorf("closing the lock: %v", err)
		}
	})
}

func TestGoldenEventSharingPage(t *testing.T) {
	for _, th := range goldenThemesOfThePanels {
		t.Run(th.name, func(t *testing.T) {
			// a saved link limited to two topics, and two private workspaces, one of them not open
			w, a := goldenSharing(t, th.theme, 700, func(store *storage.SharingStore, root string) {
				ctx := context.Background()
				link := storage.SharingLink{From: filepath.Join(root, "notes"), To: filepath.Join(root, "office"), Events: []string{"emetos.document.changed", "emetos.document.opened"}}
				if err := store.AddLink(ctx, link); err != nil {
					t.Fatal(err)
				}
				for _, id := range []string{filepath.Join(root, "office"), "/home/me/archive"} {
					if _, err := store.SetPrivate(ctx, id, true); err != nil {
						t.Fatal(err)
					}
				}
			})
			// a link for this session, from a third tab: the question is answered with This Session Only
			docs := filepath.Join(w.root, "docs")
			if err := os.MkdirAll(docs, 0o755); err != nil {
				t.Fatal(err)
			}
			third := newExtensionWorkspace(t, w.p, docs)
			w.openIn(third, "a.txt")
			answer(t, w.office, 1)
			a.run("workbench.action.toggleSidebarVisibility")
			a.run("emetos.extensions.manageEventSharing")
			w.office.Notify.ClearAll()
			a.shell().SetState(func() {})
			a.pump()
			testutil.MatchesGolden(a.ts, "event_sharing_page_"+th.name)
		})
	}
}

func TestGoldenEventSharingQuestion(t *testing.T) {
	for _, th := range goldenThemesOfThePanels {
		t.Run(th.name, func(t *testing.T) {
			w, a := goldenSharing(t, th.theme, 560, nil)
			w.openIn(w.notes, "a.txt")
			a.ts.Pump(300 * time.Millisecond)
			a.pump()
			if !w.office.Dialogs.IsShowing() {
				t.Fatal("no question shows")
			}
			testutil.MatchesGolden(a.ts, "event_sharing_question_"+th.name)
		})
	}
}
