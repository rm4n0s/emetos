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
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/core/settings"
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

// TestGoplsNavigatesAGoProject runs the real gopls on the fixture of step 12.7 and checks the definition, the references, the
// document symbols, the highlights and the workspace symbols of a function used in two files (SPEC §9.12 "Navigation").
func TestGoplsNavigatesAGoProject(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls is not installed")
	}
	folder := t.TempDir()
	fixture := filepath.Join(testRoot(t), "testdata", "proof", "12.7", "project")
	for _, name := range []string{"go.mod", "greet.go", "main.go", "greet_test.go"} {
		text, err := os.ReadFile(filepath.Join(fixture, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(folder, name), text, 0o600); err != nil {
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
	mainPath := filepath.Join(folder, "main.go")
	src, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	doc := document.New(document.FileURI(mainPath), string(src), opts)
	tb.lsp.open(doc)
	tb.pump()
	waitFor(t, tb, "gopls to run", func() bool {
		s, _ := tb.scope.ServerStatus("gopls.pack", "gopls")
		return s.State == ServerRunning
	})
	status, _ := tb.scope.ServerStatus("gopls.pack", "gopls")

	// the servers read the text of other files through the UI thread of the tab, which the test drives while it waits
	pumped := func(f func()) {
		done := make(chan struct{})
		go func() { defer close(done); f() }()
		for {
			select {
			case <-done:
				return
			default:
				tb.pump()
				time.Sleep(5 * time.Millisecond)
			}
		}
	}
	d := langfeatures.Document{URI: doc.URI(), LanguageID: "go", Version: doc.Version(), Text: doc.Snapshot()}
	reg := tb.scope.Features()
	lines := strings.Split(string(src), "\n")
	use := -1
	for i, l := range lines {
		if strings.Contains(l, "fmt.Println(Greet(n))") {
			use = i
		}
	}
	if use < 0 {
		t.Fatal("the fixture has no use of Greet")
	}
	pos := langfeatures.Position{Line: use, Col: strings.Index(lines[use], "Greet") + 1}

	// the definition: gopls needs its workspace loaded first
	var links []langfeatures.LocationLink
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) && len(links) == 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		pumped(func() { links, _ = reg.Locations(ctx, d, pos, langfeatures.NavDefinition) })
		cancel()
		if len(links) == 0 {
			tb.pump()
			time.Sleep(500 * time.Millisecond)
		}
	}
	if len(links) != 1 || !strings.HasSuffix(links[0].Target.URI.Path, "greet.go") || links[0].Target.Range.Start.Line != 5 {
		t.Fatalf("definition = %+v\nOutput:\n%s", links, strings.Join(tb.channels.Lines("gopls"), "\n"))
	}

	// the references: the declaration, the uses in main.go, greet.go and the test
	var refs []langfeatures.Location
	var errs []langfeatures.ProviderError
	pumped(func() { refs, errs = reg.References(context.Background(), d, pos, true) })
	if len(errs) != 0 {
		t.Errorf("errors = %v", errs)
	}
	files := map[string]int{}
	for _, l := range refs {
		files[filepath.Base(l.URI.Path)]++
	}
	if files["main.go"] != 2 || files["greet.go"] != 2 || files["greet_test.go"] != 1 {
		t.Errorf("references by file = %v", files)
	}

	// the symbols of a file and the highlights of an identifier
	var syms []langfeatures.DocumentSymbol
	pumped(func() { syms, _ = reg.DocumentSymbols(context.Background(), d) })
	if len(syms) != 1 || syms[0].Name != "main" || syms[0].Kind != 12 {
		t.Errorf("symbols = %+v", syms)
	}
	var hls []langfeatures.DocumentHighlight
	pumped(func() { hls, _ = reg.DocumentHighlights(context.Background(), d, pos) })
	if len(hls) < 2 {
		t.Errorf("highlights = %+v", hls)
	}

	// the workspace symbols
	var ws []langfeatures.WorkspaceSymbol
	for time.Now().Before(deadline) && len(ws) == 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		pumped(func() { ws, _ = reg.WorkspaceSymbols(ctx, "Shout") })
		cancel()
		if len(ws) == 0 {
			tb.pump()
			time.Sleep(500 * time.Millisecond)
		}
	}
	if len(ws) == 0 || ws[0].Name != "Shout" || !strings.HasSuffix(ws[0].Location.URI.Path, "greet.go") {
		t.Errorf("workspace symbols = %+v", ws)
	}

	tb.scope.Close()
	r.host.WaitServers()
	requireGone(t, "gopls", status.PID)
}

// TestGoplsFixesFormatsAndRenamesAGoProject runs the real gopls on a project with an undeclared name, an unused import and an
// unformatted function (SPEC §9.12 "Code actions", "Formatting", "Rename"): the quick fix and organize imports come back as
// edits that are made (the quick fix is resolved first, as gopls leaves its edit to codeAction/resolve), the
// formatter's edits make the file the one gofmt writes, and a rename of a function changes three files.
func TestGoplsFixesFormatsAndRenamesAGoProject(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls is not installed")
	}
	folder := t.TempDir()
	fixture := filepath.Join(testRoot(t), "testdata", "proof", "12.8", "project")
	texts := map[string]string{}
	for _, name := range []string{"go.mod", "greet.go", "main.go", "greet_test.go"} {
		text, err := os.ReadFile(filepath.Join(fixture, name))
		if err != nil {
			t.Fatal(err)
		}
		texts[name] = string(text)
		if err := os.WriteFile(filepath.Join(folder, name), text, 0o600); err != nil {
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
	docs := map[string]*document.Document{}
	for _, name := range []string{"main.go", "greet.go", "greet_test.go"} {
		docs[name] = document.New(document.FileURI(filepath.Join(folder, name)), texts[name], opts)
		tb.lsp.open(docs[name])
	}
	tb.pump()
	waitFor(t, tb, "gopls to run", func() bool {
		s, _ := tb.scope.ServerStatus("gopls.pack", "gopls")
		return s.State == ServerRunning
	})
	status, _ := tb.scope.ServerStatus("gopls.pack", "gopls")
	pumped := func(f func()) {
		done := make(chan struct{})
		go func() { defer close(done); f() }()
		for {
			select {
			case <-done:
				return
			default:
				tb.pump()
				time.Sleep(5 * time.Millisecond)
			}
		}
	}
	reg := tb.scope.Features()
	langDoc := func(name string) langfeatures.Document {
		d := docs[name]
		return langfeatures.Document{URI: d.URI(), LanguageID: "go", Version: d.Version(), Text: d.Snapshot()}
	}
	apply := func(we *langfeatures.WorkspaceEdit) {
		t.Helper()
		var err error
		pumped(func() {})
		if _, err = langfeatures.Apply(context.Background(), tb.lsp.edits, we); err != nil {
			t.Fatalf("apply: %v", err)
		}
		tb.pump()
	}

	// the quick fix of the undeclared name: the diagnostic goes back to the server as it sent it
	mainPath := filepath.Join(folder, "main.go")
	var unused problems.Marker
	waitFor(t, tb, "the diagnostic of the undeclared name", func() bool {
		for _, mk := range tb.markers.Read(problems.ReadFilter{Owner: "gopls.pack.gopls", Resource: mainPath}) {
			if strings.Contains(mk.Message, "undefined: total") || strings.Contains(mk.Message, "undeclared name: total") {
				unused = mk
				return true
			}
		}
		return false
	})
	rng := langfeatures.Range{
		Start: langfeatures.Position{Line: unused.StartLine - 1, Col: unused.StartColumn - 1},
		End:   langfeatures.Position{Line: unused.EndLine - 1, Col: unused.EndColumn - 1},
	}
	diag := langfeatures.Diagnostic{Range: rng, Message: unused.Message, Source: unused.Source, Code: unused.Code, Raw: unused.Data}
	var set langfeatures.CodeActionSet
	var errs []langfeatures.ProviderError
	pumped(func() {
		set, errs = reg.GetCodeActions(context.Background(), langDoc("main.go"),
			langfeatures.CodeActionRequest{Range: rng, Diagnostics: []langfeatures.Diagnostic{diag}},
			langfeatures.CodeActionTrigger{Type: langfeatures.TriggerInvokedAction, Source: langfeatures.SourceQuickFix, Filter: langfeatures.CodeActionFilter{Include: langfeatures.Kind(langfeatures.KindQuickFix)}})
	})
	if len(errs) != 0 {
		t.Errorf("errors = %v", errs)
	}
	var fix *langfeatures.CodeAction
	for i, a := range set.Valid {
		t.Logf("quick fix %q kind=%q edit=%v", a.Title, a.Kind, a.Edit != nil)
		if fix == nil && strings.Contains(a.Title, "total") {
			fix = &set.Valid[i]
		}
	}
	if fix == nil {
		t.Fatalf("no quick fix for the undeclared name in %+v\nOutput:\n%s", set.Valid, strings.Join(tb.channels.Lines("gopls"), "\n"))
	}
	if fix.Edit == nil {
		pumped(func() {
			resolved, err := reg.ResolveCodeAction(context.Background(), *fix)
			if err != nil {
				t.Errorf("resolve: %v", err)
			}
			*fix = resolved
		})
	}
	if fix.Edit == nil {
		t.Fatalf("the quick fix has no edit: %+v", *fix)
	}
	apply(fix.Edit)
	if got := docs["main.go"].Text(); !strings.Contains(got, "total") || strings.Count(got, "total") < 2 {
		t.Errorf("main.go after the quick fix:\n%s", got)
	}

	// organize imports: the imports are put in the wrong order, and the action sorts them again and drops the unused one
	mainText := docs["main.go"].Text()
	if _, err := docs["main.go"].Edit([]document.Edit{{
		Range: buffer.Range{End: buffer.Position{Line: strings.Count(mainText, "\n"), Col: 0}},
		Text:  strings.Replace(mainText, "\t\"fmt\"\n\t\"strings\"\n", "\t\"strings\"\n\t\"fmt\"\n", 1),
	}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	tb.pump()
	var organize langfeatures.CodeActionSet
	pumped(func() {
		d := langDoc("main.go")
		organize, _ = reg.GetCodeActions(context.Background(), d,
			langfeatures.CodeActionRequest{Range: langfeatures.FullRange(d.Text)},
			langfeatures.CodeActionTrigger{Type: langfeatures.TriggerAutomatic, Source: langfeatures.SourceOnSave,
				Filter: langfeatures.CodeActionFilter{Include: langfeatures.Kind(langfeatures.KindSourceOrganize), IncludeSourceActions: true}})
	})
	if len(organize.Valid) == 0 {
		t.Fatalf("no organize imports action\nOutput:\n%s", strings.Join(tb.channels.Lines("gopls"), "\n"))
	}
	org := organize.Valid[0]
	if org.Edit == nil {
		pumped(func() { org, _ = reg.ResolveCodeAction(context.Background(), org) })
	}
	if org.Edit != nil {
		apply(org.Edit)
	}
	if got := docs["main.go"].Text(); strings.Contains(got, "\"os\"") || strings.Index(got, "\"fmt\"") < 0 || strings.Index(got, "\"fmt\"") > strings.Index(got, "\"strings\"") {
		t.Errorf("main.go after organize imports:\n%s", got)
	}

	// the formatter: the edits make the text the one gofmt writes, and as few as the difference needs
	var edits []langfeatures.TextEdit
	pumped(func() {
		edits, _ = reg.FormatDocument(context.Background(), langDoc("greet.go"), langfeatures.FormattingOptions{TabSize: 4}, "")
	})
	if len(edits) == 0 {
		t.Fatal("the formatter changed nothing in an unformatted file")
	}
	min := langfeatures.MinimalEdits(docs["greet.go"].Snapshot(), edits)
	v := docs["greet.go"].Version()
	apply(&langfeatures.WorkspaceEdit{Changes: []langfeatures.Change{{URI: docs["greet.go"].URI(), Version: &v, Edits: min}}})
	want := "package main\n\n// Greet says hello to a name.\nfunc Greet(name string) string {\n\treturn \"Hello, \" + name\n}\n"
	if got := docs["greet.go"].Text(); got != want {
		t.Errorf("greet.go after formatting:\n%s\nwant:\n%s", got, want)
	}

	// rename: prepareRename says where the name is, and the edit changes the three files
	lines := strings.Split(docs["main.go"].Text(), "\n")
	use := -1
	for i, l := range lines {
		if strings.Contains(l, "Greet(\"world\")") {
			use = i
		}
	}
	if use < 0 {
		t.Fatalf("main.go has no use of Greet:\n%s", docs["main.go"].Text())
	}
	pos := langfeatures.Position{Line: use, Col: strings.Index(lines[use], "Greet") + 2}
	var prep *langfeatures.PrepareRename
	var err error
	pumped(func() { prep, err = reg.PrepareRename(context.Background(), langDoc("main.go"), pos) })
	if err != nil || prep == nil || prep.Placeholder != "Greet" && !prep.DefaultBehavior {
		t.Errorf("prepareRename = %+v, %v", prep, err)
	}
	var we *langfeatures.WorkspaceEdit
	pumped(func() { we, err = reg.Rename(context.Background(), langDoc("main.go"), pos, "Hello") })
	if err != nil || we == nil {
		t.Fatalf("rename = %+v, %v", we, err)
	}
	apply(we)
	for name, doc := range docs {
		if name == "go.mod" {
			continue
		}
		if got := doc.Text(); strings.Contains(got, "Greet(") && !strings.Contains(got, "TestGreet(") || !strings.Contains(got, "Hello(") {
			t.Errorf("%s after the rename:\n%s", name, got)
		}
	}
	// a name that is not an identifier is refused by the server
	pumped(func() {
		_, err = reg.PrepareRename(context.Background(), langDoc("main.go"), langfeatures.Position{Line: 0, Col: 2})
	})
	if err == nil {
		t.Log("prepareRename on the package clause did not refuse")
	}

	tb.scope.Close()
	r.host.WaitServers()
	requireGone(t, "gopls", status.PID)
}

// TestGoplsAnswersHintsTokensFoldingAndSelectionRanges runs the real gopls on the fixture of step 12.9 with the hints and the
// semantic tokens on (the section go.gopls of the settings): inlay hints for the parameters, the types and the fields, semantic
// tokens that decode to the text of the identifiers they cover, folding ranges, selection ranges and the code lens of
// //go:generate (SPEC §9.12 "As built (step 12.9)").
func TestGoplsAnswersHintsTokensFoldingAndSelectionRanges(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls is not installed")
	}
	folder := t.TempDir()
	src, err := os.ReadFile(filepath.Join(testRoot(t), "testdata", "proof", "12.9", "project", "main.go"))
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
	if err := r.registry.Register(settings.Setting{Key: "go.gopls", Types: []settings.Type{settings.Object}, Default: map[string]any{}, Scope: settings.ScopeWindow}); err != nil {
		t.Fatal(err)
	}
	tb.changeSetting("go.gopls", map[string]any{
		"semanticTokens": true,
		"hints": map[string]any{
			"parameterNames": true, "assignVariableTypes": true, "compositeLiteralFields": true,
			"constantValues": true, "rangeVariableTypes": true,
		},
	})
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

	f2 := tb.scope.Features()
	d := langfeatures.Document{URI: doc.URI(), LanguageID: "go", Version: doc.Version(), Text: doc.Snapshot()}
	lines := strings.Split(string(src), "\n")
	whole := buffer.Range{Start: buffer.Position{}, End: buffer.Position{Line: len(lines) - 1}}

	// the inlay hints: gopls needs its workspace loaded, so ask until it answers
	var hints []langfeatures.InlayHint
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) && len(hints) == 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		hints, _ = f2.InlayHints(ctx, d, whole)
		cancel()
		if len(hints) == 0 {
			tb.pump()
			time.Sleep(500 * time.Millisecond)
		}
	}
	if len(hints) == 0 {
		t.Fatalf("gopls gave no inlay hints; Output:\n%s", strings.Join(tb.channels.Lines("gopls"), "\n"))
	}
	var labels []string
	for _, h := range hints {
		labels = append(labels, h.Label())
	}
	t.Logf("%d hints: %q", len(hints), labels)
	for _, want := range []string{"p:", "factor:", "label:", "X:", "Y:"} {
		if !slices.Contains(labels, want) {
			t.Errorf("hints %q lack %q", labels, want)
		}
	}
	if !slices.ContainsFunc(labels, func(l string) bool { return strings.Contains(l, "Point") }) {
		t.Errorf("hints %q have no type of the variables", labels)
	}
	for _, h := range hints {
		if h.Label() == "p:" && (h.Kind != langfeatures.InlayHintParameter || h.Position.Line != 23) {
			t.Errorf("the hint p: = %+v", h)
		}
	}

	// the semantic tokens: each token covers the text of an identifier, and `scale` is a function
	cache := langfeatures.NewSemanticTokensCache(f2)
	var toks *langfeatures.SemanticTokens
	for time.Now().Before(deadline) && (toks == nil || len(toks.Tokens) == 0) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		toks, _ = cache.Get(ctx, d)
		cancel()
		if toks == nil || len(toks.Tokens) == 0 {
			tb.pump()
			time.Sleep(500 * time.Millisecond)
		}
	}
	if toks == nil || len(toks.Tokens) == 0 {
		t.Fatalf("gopls gave no semantic tokens; Output:\n%s", strings.Join(tb.channels.Lines("gopls"), "\n"))
	}
	t.Logf("%d semantic tokens", len(toks.Tokens))
	found := map[string]string{}
	for _, tk := range toks.Tokens {
		text := lines[tk.Line][tk.Col : tk.Col+tk.Len]
		if text == "scale" || text == "Point" || text == "fmt" || text == "limit" {
			found[text] = toks.TypeName(tk)
		}
	}
	for text, typ := range map[string]string{"scale": "function", "Point": "type", "fmt": "namespace", "limit": "variable"} {
		if found[text] != typ {
			t.Errorf("token of %q has the type %q, want %q (all: %v)", text, found[text], typ, found)
		}
	}
	// a second request for the same version is the cached one; after an edit gopls may answer with edits (a delta)
	again, err := cache.Get(context.Background(), d)
	if err != nil || again == nil || len(again.Tokens) != len(toks.Tokens) {
		t.Errorf("the second request gave %v, %v", again, err)
	}

	// the folding ranges: the function main, the import group and the struct
	ranges, _, ok, _ := f2.FoldingRanges(context.Background(), d)
	if !ok {
		t.Fatal("gopls gave no folding ranges")
	}
	var starts []int
	for _, fr := range ranges {
		starts = append(starts, fr.Start)
	}
	for _, sub := range []string{"import (", "type Point struct", "func main()"} {
		line := slices.IndexFunc(lines, func(l string) bool { return strings.HasPrefix(l, sub) })
		if !slices.Contains(starts, line) {
			t.Errorf("no folding range starts at %q (line %d): %v", sub, line, starts)
		}
	}

	// the selection ranges around `limit` in the call of scale: the chain grows to the whole file
	callLine := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "moved := scale") })
	at := langfeatures.Position{Line: callLine, Col: strings.Index(lines[callLine], "limit") + 1}
	set, ok, _ := f2.SelectionRanges(context.Background(), d, []langfeatures.Position{at})
	if !ok || len(set.Positions()) != 1 {
		t.Fatalf("selection ranges = %+v, %v", set, ok)
	}

	// the code lens of the //go:generate directive
	lenses, _ := f2.CodeLenses(context.Background(), d)
	genLine := slices.IndexFunc(lines, func(l string) bool { return strings.HasPrefix(l, "//go:generate") })
	var gen bool
	for _, l := range lenses {
		gen = gen || (l.Range.Start.Line == genLine && l.Command != nil && strings.Contains(l.Command.Title, "go generate"))
	}
	if !gen {
		t.Errorf("no code lens for //go:generate at line %d: %+v", genLine, lenses)
	}

	tb.scope.Close()
	r.host.WaitServers()
	requireGone(t, "gopls", status.PID)
}
