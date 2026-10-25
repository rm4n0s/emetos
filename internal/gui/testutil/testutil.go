package testutil

import (
	"bytes"
	"context"
	"flag"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/gofont/gomonobolditalic"
	"golang.org/x/image/font/gofont/gomonoitalic"

	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/core/storage/storagetest"
	"github.com/rm4n0s/emetos/internal/core/tokens"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// MonoFamily is the family name of the test font for the editor (SPEC D14).
const MonoFamily = "Go Mono"

// proofFlag is the -proof flag of proof method A: a test that calls Proof writes its image.
var proofFlag = flag.Bool("proof", false, "write the proof images of tests that make them to docs/implemented-proof-images")

// ScanTimeout is the time limit of one pattern search of a grammar in the tests that tokenize. The
// default (100 ms in production, SPEC §8.2) is exceeded by the first search of a grammar on a loaded
// machine under the race detector, and a search that times out leaves its line without colors, which a
// golden image shows.
const ScanTimeout = 30 * time.Second

// Fonts are the fonts every UI test loads: Go Mono in its four styles under the family
// MonoFamily, then Codicons and Seti. The look of a test never depends on the fonts of the
// machine that runs it.
func Fonts() []gluttertest.Font {
	out := []gluttertest.Font{
		{Family: MonoFamily, Data: gomono.TTF},
		{Family: MonoFamily, Data: gomonobold.TTF},
		{Family: MonoFamily, Data: gomonoitalic.TTF},
		{Family: MonoFamily, Data: gomonobolditalic.TTF},
	}
	for _, src := range fonts.Sources() {
		out = append(out, gluttertest.Font{Family: src.Family, Data: src.Data})
	}
	return out
}

// WindowInfo is the window the tester shows: the one Emetos asks for (SPEC §10.14), whose title bar the application
// draws (DecorationApp), with every capability, focused and maximized (so that the window has no border and no resize
// edges, which would show in every image).
var WindowInfo = ui.WindowInfo{
	Decoration: ui.DecorationApp, Capabilities: ui.AllWindowCapabilities, Activated: true, Maximized: true,
}

// New makes a tester of the given size in logical pixels with the test fonts loaded. The tester
// unmounts the widget tree when the test ends, so the states are disposed and the goroutines they
// started (State.Go, the tokenizers of the app) end with the test. Its window is WindowInfo, so
// that the title bar row shows as it does in the application.
func New(t testing.TB, size ui.Size) *gluttertest.Tester {
	t.Helper()
	ts := gluttertest.New(t, gluttertest.Opts{Size: size, Fonts: Fonts()})
	ts.SetViewConfiguration(func(c *ui.ViewConfiguration) { c.Window = WindowInfo })
	return ts
}

// Isolate points every user folder at temporary ones (see storagetest.Isolate) and returns
// their paths.
func Isolate(t testing.TB) storage.Paths {
	t.Helper()
	return storagetest.Isolate(t)
}

// Settings makes a settings store over the registry of Emetos whose user settings.json sits
// in the isolated configuration folder and says editor.fontFamily is MonoFamily. extra adds
// or replaces other settings. The files are read before it returns.
func Settings(t testing.TB, paths storage.Paths, extra map[string]any) *settings.Store {
	t.Helper()
	userPath, err := settings.UserSettingsPath(paths, storage.DefaultProfile)
	if err != nil {
		t.Fatalf("testutil.Settings: %v", err)
	}
	store := settings.NewStore(settings.DefaultRegistry(), settings.StoreOptions{UserPath: userPath})
	ctx := context.Background()
	if err := store.Update(ctx, "editor.fontFamily", MonoFamily, settings.TargetUser); err != nil {
		t.Fatalf("testutil.Settings: %v", err)
	}
	for k, v := range extra {
		if err := store.Update(ctx, k, v, settings.TargetUser); err != nil {
			t.Fatalf("testutil.Settings: %v", err)
		}
	}
	return store
}

// RepoRoot finds the folder of the repository: the nearest parent of the working directory
// that holds the go.mod of Emetos.
func RepoRoot(t testing.TB) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("testutil.RepoRoot: %v", err)
	}
	for {
		data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err == nil && strings.HasPrefix(string(data), "module github.com/rm4n0s/emetos\n") {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("testutil.RepoRoot: no go.mod of Emetos above %s", dir)
		}
		dir = parent
	}
}

// ProofEnabled tells whether the test run has the -proof flag.
func ProofEnabled() bool { return *proofFlag }

// Proof writes the screenshot of the tester as a PNG to docs/implemented-proof-images/<name>
// (proof method A) when the run has the -proof flag, and does nothing otherwise. The name
// ends in .png.
func Proof(t testing.TB, ts *gluttertest.Tester, name string) {
	t.Helper()
	if !*proofFlag {
		return
	}
	img := ts.Screenshot()
	if img == nil {
		return // the tester has reported why
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("testutil.Proof: %v", err)
	}
	dir := filepath.Join(RepoRoot(t), "docs", "implemented-proof-images")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("testutil.Proof: %v", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("testutil.Proof: %v", err)
	}
	t.Logf("proof image written to %s", path)
}

// tokenized is what a widget that shows highlighted text says of its tokenizer (the editor
// widget does).
type tokenized interface {
	Tokenizer() *tokens.Tokenizer
}

// SettleTokens tokenizes, on the caller, every line of every editor the tester shows (also the
// ones that are offstage), then pumps a frame so that the text is painted with the colors. The
// editor tokenizes the lines it draws only as far as a wall-clock budget allows and leaves the rest
// to a background goroutine, so a picture taken without this depends on the speed of the machine.
func SettleTokens(ts *gluttertest.Tester) {
	f := gluttertest.ByPredicate("a widget with a tokenizer", func(w widgets.Widget) bool {
		t, ok := w.(tokenized)
		return ok && t.Tokenizer() != nil
	}, gluttertest.FindOpts{IncludeOffstage: true})
	for _, w := range ts.Widgets(f) {
		w.(tokenized).Tokenizer().Settle()
	}
	ts.Pump()
}

// MatchesGolden is the MatchesGolden of the tester after SettleTokens: a golden image never
// depends on how far the background tokenizer got. Every golden test calls it.
func MatchesGolden(ts *gluttertest.Tester, name string) {
	SettleTokens(ts)
	ts.MatchesGolden(name)
}
