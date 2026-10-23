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

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/gofont/gomonobolditalic"
	"golang.org/x/image/font/gofont/gomonoitalic"

	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/core/storage/storagetest"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// MonoFamily is the family name of the test font for the editor (SPEC D14).
const MonoFamily = "Go Mono"

// proofFlag is the -proof flag of proof method A: a test that calls Proof writes its image.
var proofFlag = flag.Bool("proof", false, "write the proof images of tests that make them to docs/implemented-proof-images")

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

// New makes a tester of the given size in logical pixels with the test fonts loaded.
func New(t testing.TB, size ui.Size) *gluttertest.Tester {
	t.Helper()
	return gluttertest.New(t, gluttertest.Opts{Size: size, Fonts: Fonts()})
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
