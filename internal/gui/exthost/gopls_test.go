//go:build emetos_gopls

package exthost

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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

// TestGoplsCompletesAMemberOfAPackage runs the real gopls on `fmt.` and checks the list, the resolve of one member (its
// documentation) and the trigger character (SPEC §9.12 "Completion").
func TestGoplsCompletesAMemberOfAPackage(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls is not installed")
	}
	folder := t.TempDir()
	src, err := os.ReadFile(filepath.Join(testRoot(t), "testdata", "proof", "12.5", "project", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(string(src), "\tfmt\n", "\tfmt.\n", 1)
	for name, content := range map[string]string{"go.mod": "module example.com/proof\n\ngo 1.21\n", "main.go": text} {
		if err := os.WriteFile(filepath.Join(folder, name), []byte(content), 0o600); err != nil {
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
	doc := document.New(document.FileURI(filepath.Join(folder, "main.go")), text, opts)
	tb.lsp.open(doc)
	tb.pump()
	waitFor(t, tb, "gopls to run", func() bool {
		s, _ := tb.scope.ServerStatus("gopls.pack", "gopls")
		return s.State == ServerRunning
	})
	status, _ := tb.scope.ServerStatus("gopls.pack", "gopls")

	d := langfeatures.Document{URI: doc.URI(), LanguageID: "go", Version: doc.Version(), Text: doc.Snapshot()}
	pos := langfeatures.Position{Line: 5, Col: len("\tfmt.")}
	if got := tb.scope.Features().CompletionTriggers(d); len(got) == 0 {
		t.Errorf("gopls has no trigger characters: %v", got)
	}
	var println langfeatures.CompletionItem
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) && println.Label == "" {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		list, _ := tb.scope.Features().Completion(ctx, d, pos, langfeatures.CompletionTrigger{Kind: langfeatures.TriggerCharacter, Character: "."})
		cancel()
		for _, it := range list.Items {
			if it.Label == "Println" {
				println = it
			}
		}
		if println.Label == "" {
			tb.pump()
			time.Sleep(500 * time.Millisecond)
		}
	}
	if println.Label == "" {
		t.Fatalf("gopls did not complete fmt.; Output:\n%s", strings.Join(tb.channels.Lines("gopls"), "\n"))
	}
	if println.Kind != 3 && println.Kind != 2 {
		t.Errorf("Println has the kind %d, want a function", println.Kind)
	}
	if !tb.scope.Features().CanResolveCompletion(println) {
		// gopls does not resolve: the details come with the list
		if println.Detail == "" && println.Documentation == nil {
			t.Errorf("Println has neither a detail nor a documentation: %+v", println)
		}
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		resolved, err := tb.scope.Features().ResolveCompletion(ctx, d, println)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		if resolved.Documentation == nil || !strings.Contains(resolved.Documentation.Value, "Println") {
			t.Errorf("the resolved item has no documentation: %+v", resolved.Documentation)
		}
	}

	tb.scope.Close()
	r.host.WaitServers()
	requireGone(t, "gopls", status.PID)
}

// TestGoplsAnswersHoverAndSignatureHelp runs the real gopls on the fixture of step 12.6: the hover of a function of the
// standard library has a code block and its documentation, and the signature help inside a call has the signature with the
// active parameter, found by the offsets of its label (SPEC §9.12 "Hover", "Signature help").
func TestGoplsAnswersHoverAndSignatureHelp(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls is not installed")
	}
	folder := t.TempDir()
	src, err := os.ReadFile(filepath.Join(testRoot(t), "testdata", "proof", "12.6", "project", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"go.mod": "module example.com/proof\n\ngo 1.21\n", "main.go": string(src)} {
		if err := os.WriteFile(filepath.Join(folder, name), []byte(content), 0o600); err != nil {
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
	doc := document.New(document.FileURI(filepath.Join(folder, "main.go")), string(src), opts)
	tb.lsp.open(doc)
	tb.pump()
	waitFor(t, tb, "gopls to run", func() bool {
		s, _ := tb.scope.ServerStatus("gopls.pack", "gopls")
		return s.State == ServerRunning
	})
	status, _ := tb.scope.ServerStatus("gopls.pack", "gopls")

	d := langfeatures.Document{URI: doc.URI(), LanguageID: "go", Version: doc.Version(), Text: doc.Snapshot()}
	lines := strings.Split(string(src), "\n")
	line := func(sub string) int {
		for i, l := range lines {
			if strings.Contains(l, sub) {
				return i
			}
		}
		t.Fatalf("the fixture has no line with %q", sub)
		return 0
	}

	// the hover of strings.Join: a code block and the documentation
	join := langfeatures.Position{Line: line("strings.Join"), Col: strings.Index(lines[line("strings.Join")], "Join") + 1}
	var hover langfeatures.Hover
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) && len(hover.Contents) == 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		hs, _ := tb.scope.Features().Hover(ctx, d, join)
		cancel()
		if len(hs) > 0 {
			hover = hs[0]
		} else {
			tb.pump()
			time.Sleep(500 * time.Millisecond)
		}
	}
	if len(hover.Contents) == 0 {
		t.Fatalf("gopls did not answer the hover; Output:\n%s", strings.Join(tb.channels.Lines("gopls"), "\n"))
	}
	text := hover.Contents[0]
	if !text.Markdown || !strings.Contains(text.Value, "```go\nfunc strings.Join(elems []string, sep string) string\n```") || !strings.Contains(text.Value, "Join concatenates") {
		t.Errorf("hover = %q", text.Value)
	}
	if hover.Range == nil || hover.Range.Start.Line != join.Line {
		t.Errorf("hover range = %v", hover.Range)
	}

	// the trigger and retrigger characters
	triggers, retriggers := tb.scope.Features().SignatureCharacters(d)
	if !slices.Contains(triggers, "(") || !slices.Contains(triggers, ",") && !slices.Contains(retriggers, ",") {
		t.Errorf("trigger characters %q, retrigger characters %q", triggers, retriggers)
	}

	// the signature help right after the comma typed in Greeting(", ", "hello",
	call := lines[line("Greeting(\", \"")]
	pos := langfeatures.Position{Line: line("Greeting(\", \""), Col: strings.Index(call, `"hello",`) + len(`"hello",`)}
	var help *langfeatures.SignatureHelp
	for time.Now().Before(deadline) && help == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		var errs []langfeatures.ProviderError
		help, errs = tb.scope.Features().SignatureHelp(ctx, d, pos, langfeatures.SignatureContext{Kind: langfeatures.SignatureTriggerCharacter, Character: ","})
		cancel()
		if len(errs) > 0 {
			t.Logf("signature help errors: %v", errs)
		}
		if help == nil {
			tb.pump()
			time.Sleep(500 * time.Millisecond)
		}
	}
	if help == nil {
		t.Fatalf("gopls did not answer the signature help; Output:\n%s", strings.Join(tb.channels.Lines("gopls"), "\n"))
	}
	sig := help.Signatures[help.ActiveSignature]
	if sig.Label != "Greeting(sep string, words ...string) string" || sig.Documentation == nil || !strings.Contains(sig.Documentation.Value, "joins the words") {
		t.Errorf("signature = %+v", sig)
	}
	active := help.ActiveParameter
	if sig.ActiveParameter != nil {
		active = *sig.ActiveParameter
	}
	if start, end := sig.LabelRange(active); sig.Label[start:end] != "words ...string" {
		t.Errorf("the active parameter %d is %q, want the variadic parameter", active, sig.Label[start:end])
	}
	// asked again with the help that showed, as a retrigger
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	again, _ := tb.scope.Features().SignatureHelp(ctx, d, pos, langfeatures.SignatureContext{Kind: langfeatures.SignatureContentChange, IsRetrigger: true, Active: help})
	cancel()
	if again == nil || len(again.Signatures) == 0 {
		t.Error("gopls does not answer a retrigger")
	}
	// outside a call there is none
	ctx, cancel = context.WithTimeout(context.Background(), 20*time.Second)
	none, _ := tb.scope.Features().SignatureHelp(ctx, d, langfeatures.Position{Line: 0, Col: 3}, langfeatures.SignatureContext{Kind: langfeatures.SignatureInvoke})
	cancel()
	if none != nil {
		t.Errorf("signature help at the package clause: %+v", none)
	}

	tb.scope.Close()
	r.host.WaitServers()
	requireGone(t, "gopls", status.PID)
}
