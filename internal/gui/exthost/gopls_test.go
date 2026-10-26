//go:build emetos_gopls

package exthost

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
	"github.com/rm4n0s/emetos/internal/gui/views/problems"
)

// TestGoplsStartsAndAnswersForAGoFile runs the real gopls (SPEC §9.12 "The first server"): it is skipped when gopls is not
// installed. The declaration is that of extensions/golang.
func TestGoplsStartsAndAnswersForAGoFile(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls is not installed")
	}
	folder := t.TempDir()
	src := "package main\n\n// Greet says hello.\nfunc Greet() string { return \"hello\" }\n\nfunc main() { _ = Greet() }\n"
	for name, text := range map[string]string{"go.mod": "module example.com/hello\n\ngo 1.21\n", "main.go": src} {
		if err := os.WriteFile(filepath.Join(folder, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	m := manifest("gopls.pack")
	m.Contributes.LanguageServers = []extension.LanguageServerSpec{{
		ID: "gopls", Name: "gopls", Languages: []string{"go", "go.mod", "go.work"}, Program: "gopls",
		PathSetting: "go.goplsPath", ConfigurationSection: "go.gopls",
	}}
	f, _ := factory(m, nil)
	r := newRig(t, f)
	r.folders["one"] = folder
	t.Cleanup(func() { r.host.Shutdown() })
	tb := r.newTab("one")
	tb.deferred = true
	tb.scope.Start()

	opts := document.NewOptions()
	opts.LanguageID = "go"
	doc := document.New(document.FileURI(filepath.Join(folder, "main.go")), src, opts)
	tb.lsp.open(doc)
	tb.pump()
	waitFor(t, tb, "gopls to run", func() bool {
		s, _ := tb.scope.ServerStatus("gopls.pack", "gopls")
		return s.State == ServerRunning
	})
	status, _ := tb.scope.ServerStatus("gopls.pack", "gopls")

	// hover on Greet in main(): gopls answers once its workspace is loaded
	d := langfeatures.Document{URI: doc.URI(), LanguageID: "go", Version: doc.Version(), Text: doc.Snapshot()}
	pos := langfeatures.Position{Line: 5, Col: strings.Index("func main() { _ = Greet() }", "Greet")}
	deadline := time.Now().Add(90 * time.Second)
	var found bool
	for time.Now().Before(deadline) && !found {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		hs, _ := tb.scope.Features().Hover(ctx, d, pos)
		cancel()
		for _, h := range hs {
			for _, c := range h.Contents {
				found = found || strings.Contains(c.Value, "func Greet() string")
			}
		}
		if !found {
			tb.pump()
			time.Sleep(500 * time.Millisecond)
		}
	}
	if !found {
		t.Fatalf("gopls did not answer the hover; Output:\n%s", strings.Join(tb.channels.Lines("gopls"), "\n"))
	}
	syms, _ := tb.scope.Features().DocumentSymbols(context.Background(), d)
	if len(syms) < 2 {
		t.Errorf("symbols = %+v", syms)
	}

	tb.scope.Close()
	r.host.WaitServers()
	requireGone(t, "gopls", status.PID)
}

// TestGoplsPublishesDiagnosticsForAGoFile runs the real gopls on a file with a type error, an unused variable and a deprecated
// call, and checks that they become the markers of the server (SPEC §9.12 "Diagnostics").
func TestGoplsPublishesDiagnosticsForAGoFile(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls is not installed")
	}
	folder := t.TempDir()
	src, err := os.ReadFile(filepath.Join(testRoot(t), "testdata", "proof", "12.4", "project", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"go.mod": "module example.com/proof\n\ngo 1.21\n", "main.go": string(src)} {
		if err := os.WriteFile(filepath.Join(folder, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	m := manifest("gopls.pack")
	m.Contributes.LanguageServers = []extension.LanguageServerSpec{{
		ID: "gopls", Name: "gopls", Languages: []string{"go", "go.mod", "go.work"}, Program: "gopls",
		PathSetting: "go.goplsPath", ConfigurationSection: "go.gopls",
	}}
	f, _ := factory(m, nil)
	r := newRig(t, f)
	r.folders["one"] = folder
	t.Cleanup(func() { r.host.Shutdown() })
	tb := r.newTab("one")
	tb.deferred = true
	tb.scope.Start()

	opts := document.NewOptions()
	opts.LanguageID = "go"
	path := filepath.Join(folder, "main.go")
	doc := document.New(document.FileURI(path), string(src), opts)
	tb.lsp.open(doc)
	tb.pump()
	var got []problems.Marker
	waitFor(t, tb, "gopls to publish diagnostics", func() bool {
		got = tb.markers.Read(problems.ReadFilter{Owner: "gopls.pack.gopls", Resource: path})
		return len(got) >= 2
	})
	var unused, mismatch bool
	for _, m := range got {
		t.Logf("%v %d:%d-%d:%d %q source=%q code=%q href=%q tags=%v/%v", m.Severity, m.StartLine, m.StartColumn, m.EndLine, m.EndColumn, m.Message, m.Source, m.Code, m.CodeHref, m.Unnecessary, m.Deprecated)
		unused = unused || (m.Severity == problems.SeverityError && strings.Contains(m.Message, "count") && m.StartLine == 9)
		mismatch = mismatch || (m.Severity == problems.SeverityError && strings.Contains(m.Message, "42") && m.StartLine == 10)
	}
	if !unused || !mismatch {
		t.Fatalf("markers = %+v\nOutput:\n%s", got, strings.Join(tb.channels.Lines("gopls"), "\n"))
	}
	status, _ := tb.scope.ServerStatus("gopls.pack", "gopls")
	tb.scope.Close()
	r.host.WaitServers()
	requireGone(t, "gopls", status.PID)
	if left := tb.markers.Read(problems.ReadFilter{Owner: "gopls.pack.gopls"}); len(left) != 0 {
		t.Errorf("%d markers are left after the server stopped", len(left))
	}
}

// testRoot is the root of the repository.
func testRoot(t *testing.T) string {
	t.Helper()
	return testutil.RepoRoot(t)
}
