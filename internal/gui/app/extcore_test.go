package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/gui/extensionsui"
	"github.com/rm4n0s/emetos/internal/gui/exthost"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// The core request topics on a real workspace (step 16.1, SPEC §10.19 "As built (step 16.1)"): the documents, the editor
// and the commands of the window answer an extension that has the privileges.

type coreReply struct {
	mu   sync.Mutex
	done bool
	ev   extension.Event
	err  error
}

func TestTheCoreTopicsDriveARealWorkspace(t *testing.T) {
	var api extension.API
	relay := func() extension.Extension {
		return &testExtension{
			manifest: extension.Manifest{
				ID: extension.RelayID, Name: "Relay", ActivationEvents: []string{extension.OnStartup},
				Privileges: []string{extension.PrivilegeEventsPublishAny, extension.PrivilegeCommandsExecute},
			},
			activate: func(_ context.Context, a extension.API) error { api = a; return nil },
		}
	}
	p := extensionProcess(t, relay)
	folder := t.TempDir()
	for name, text := range map[string]string{"a.go": "package a\n// a\n", "b.go": "package b\n"} {
		if err := os.WriteFile(filepath.Join(folder, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	win := newExtensionWorkspace(t, p, folder)
	a := &groupApp{t: t, win: win, files: map[string]string{}, docs: map[string]*document.Document{}}
	a.ts = testutil.New(t, testWindowSize)
	a.ts.PumpWidget(newAppDocs(context.Background(), win, nil, nil, ""))
	a.pump()
	if api == nil {
		t.Fatal("the extension was not activated")
	}
	ask := func(topic string, payload any) (extension.Event, error) {
		t.Helper()
		r := &coreReply{}
		// a deadline of the wall clock: without one the request ends after DefaultRequestTimeout of the fake clock, which the
		// pumps below run several times faster than the wall clock, and a save under the race detector takes a while
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if err := api.Events().Request(ctx, topic, payload, func(ev extension.Event, err error) {
			r.mu.Lock()
			r.done, r.ev, r.err = true, ev, err
			r.mu.Unlock()
		}); err != nil {
			t.Fatal(err)
		}
		a.waitFor("the reply to "+topic, func() bool { r.mu.Lock(); defer r.mu.Unlock(); return r.done })
		// the reply comes from the UI thread as soon as the model has changed (the editor is shown, the cursor is moved), and the
		// tabs and the editor are widgets that the next frames build: the test reads them, so it lets them be built first
		a.pump()
		return r.ev, r.err
	}
	aURI := document.FileURI(filepath.Join(folder, "a.go")).String()

	// open shows the file in the editor with the cursor where the request says
	ev, err := ask(extension.TopicDocumentOpen, extension.DocumentOpenRequest{Path: "a.go", Line: 1, Col: 3})
	if err != nil {
		t.Fatal(err)
	}
	info, _ := extension.EventValue[extension.DocumentInfo](ev)
	if info.URI != aURI || info.LanguageID != "go" || a.active() != "a.go" {
		t.Fatalf("open: %+v, active tab %q", info, a.active())
	}
	if got := a.vm().Cursors().Selections()[0].Active; got != (buffer.Position{Line: 1, Col: 3}) {
		t.Errorf("the cursor is at %+v", got)
	}

	// read, then edit against its version; the editor changed and one undo takes the edit back
	ev, err = ask(extension.TopicDocumentRead, extension.DocumentURIRequest{URI: aURI})
	content, _ := extension.EventValue[extension.DocumentContent](ev)
	if err != nil || content.Text != "package a\n// a\n" || content.Dirty {
		t.Fatalf("read: %+v %v", content, err)
	}
	ev, err = ask(extension.TopicDocumentEdit, extension.DocumentEditRequest{URI: aURI, Version: content.Version, Edits: []extension.TextChange{
		{Range: extension.Range{Start: extension.Position{Line: 0, Col: 8}, End: extension.Position{Line: 0, Col: 9}}, Text: "z"},
		{Range: extension.Range{Start: extension.Position{Line: 2, Col: 0}, End: extension.Position{Line: 2, Col: 0}}, Text: "// done\n"},
	}})
	edited, _ := extension.EventValue[extension.DocumentEditReply](ev)
	doc, _ := win.Documents.Get(document.FileURI(filepath.Join(folder, "a.go")))
	if err != nil || !edited.Changed || edited.Version != content.Version+1 || doc.Text() != "package z\n// a\n// done\n" {
		t.Fatalf("edit: %+v %v, text %q", edited, err, doc.Text())
	}
	if !doc.Dirty() {
		t.Error("the edited document is not dirty")
	}
	// the version it was made against is stale now
	_, err = ask(extension.TopicDocumentEdit, extension.DocumentEditRequest{URI: aURI, Version: content.Version, Edits: nil})
	if appErr, ok := errors.FromError(err); !ok || !appErr.HasRoute("ApplyTextEdits.RelayStaleVersion") {
		t.Errorf("a stale edit: %v", err)
	}
	// closing a dirty document needs discard
	_, err = ask(extension.TopicDocumentClose, extension.DocumentCloseRequest{URI: aURI})
	if appErr, ok := errors.FromError(err); !ok || appErr.Tag != "CoreDocumentDirty" {
		t.Errorf("close of a dirty document: %v", err)
	}

	// save goes through the Save command: the file on disk has the edit
	ev, err = ask(extension.TopicDocumentSave, extension.DocumentURIRequest{URI: aURI})
	saved, _ := extension.EventValue[extension.DocumentSaved](ev)
	data, rerr := os.ReadFile(filepath.Join(folder, "a.go"))
	if err != nil || saved.Version != doc.Version() || string(data) != "package z\n// a\n// done\n" || doc.Dirty() {
		t.Fatalf("save: %+v %v, disk %q", saved, err, data)
	} else if rerr != nil {
		t.Fatal(rerr)
	}

	// a save that does not happen says so (a read-only document is not saved; the window tab shows why)
	doc.SetReadOnly(true)
	_, err = ask(extension.TopicDocumentSave, extension.DocumentURIRequest{URI: aURI})
	if appErr, ok := errors.FromError(err); !ok || !appErr.HasRoute("extensionDocuments.Save.func1.ExtDocumentNotSaved") {
		t.Errorf("save of a read-only document: %v", err)
	}
	doc.SetReadOnly(false)

	// reveal brings the editor of a document to the front
	if _, err := ask(extension.TopicDocumentOpen, extension.DocumentOpenRequest{Path: "b.go"}); err != nil {
		t.Fatal(err)
	}
	if a.active() != "b.go" {
		t.Fatalf("the active tab is %q", a.active())
	}
	if _, err := ask(extension.TopicEditorReveal, extension.EditorRevealRequest{URI: aURI, Line: 2, Col: 1}); err != nil {
		t.Fatal(err)
	}
	if a.active() != "a.go" {
		t.Errorf("after reveal the active tab is %q", a.active())
	}
	if got := a.vm().Cursors().Selections()[0].Active; got != (buffer.Position{Line: 2, Col: 1}) {
		t.Errorf("the cursor is at %+v", got)
	}

	// a command of the window runs in this workspace
	before := len(win.Documents.All())
	if _, err := ask(extension.TopicCommandsExecute, extension.CommandsExecuteRequest{ID: "workbench.action.files.newUntitledFile"}); err != nil {
		t.Fatal(err)
	}
	a.pump()
	if got := len(win.Documents.All()); got != before+1 {
		t.Errorf("%d documents after New Untitled File, before %d", got, before)
	}
	// and one that reaches outside it does not
	_, err = ask(extension.TopicCommandsExecute, extension.CommandsExecuteRequest{ID: "workbench.action.closeWindow"})
	if appErr, ok := errors.FromError(err); !ok || appErr.Tag != "RelayCommandRefused" {
		t.Errorf("close window: %v", err)
	}
	ev, err = ask(extension.TopicCommandsList, nil)
	list, _ := extension.EventValue[extension.CommandsList](ev)
	refused := 0
	for _, c := range list.Commands {
		if c.Refused {
			refused++
		}
	}
	if err != nil || len(list.Commands) < 100 || refused < 20 {
		t.Errorf("the command list has %d commands, %d refused (%v)", len(list.Commands), refused, err)
	}

	// close with discard closes the editor and the document
	if _, err := ask(extension.TopicDocumentClose, extension.DocumentCloseRequest{URI: aURI}); err != nil {
		t.Fatal(err) // saved, so it is not dirty
	}
	for _, n := range a.tabNames() {
		if n == "a.go" {
			t.Errorf("the tab of a.go is still open: %v", a.tabNames())
		}
	}
	if _, ok := win.Documents.Get(document.FileURI(filepath.Join(folder, "a.go"))); ok {
		t.Error("the document is still open")
	}

	// files: the folder of this workspace only
	ev, err = ask(extension.TopicFilesRead, extension.FilesPathRequest{Path: "b.go"})
	read, _ := extension.EventValue[extension.FilesReadReply](ev)
	if err != nil || read.Text != "package b\n" {
		t.Errorf("files.read: %+v %v", read, err)
	}
	_, err = ask(extension.TopicFilesRead, extension.FilesPathRequest{Path: "../" + filepath.Base(folder) + "/b.go"})
	if appErr, ok := errors.FromError(err); !ok || !strings.Contains(appErr.Route(), "ConfinePathInvalid") {
		t.Errorf("files.read of ..: %v", err)
	}
}

// A request that ends while its file is being read leaves nothing open that it held.
func TestAnOpenThatIsCancelledWhileTheFileIsReadReleasesTheDocument(t *testing.T) {
	p := extensionProcess(t)
	folder := t.TempDir()
	path := filepath.Join(folder, "a.go")
	if err := os.WriteFile(path, []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	win := newExtensionWorkspace(t, p, folder)
	a := &groupApp{t: t, win: win, files: map[string]string{}, docs: map[string]*document.Document{}}
	a.ts = testutil.New(t, testWindowSize)
	a.ts.PumpWidget(newAppDocs(context.Background(), win, nil, nil, ""))
	a.pump()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// the request ends the moment the document is added to the window's set, which is when the read is done
	stop := win.Documents.WatchOpen(func(*document.Document) { cancel() })
	defer stop()
	result := make(chan error, 1)
	extensionDocuments{w: win}.OpenFile(ctx, path, extension.Position{}, func(_ *document.Document, err error) { result <- err })
	var err error
	a.waitFor("the open to end", func() bool {
		select {
		case err = <-result:
			return true
		default:
			return false
		}
	})
	if appErr, ok := errors.FromError(err); !ok || !appErr.HasRoute("OpenFile.func1.1.ExtDocumentOpenCanceled") {
		t.Fatalf("open: %v", appErr.Route())
	}
	if doc, ok := win.Documents.Get(document.FileURI(path)); ok && win.Documents.Refs(doc) != 0 {
		t.Errorf("the document is held %d times", win.Documents.Refs(doc))
	}
	if got := a.tabNames(); len(got) != 0 {
		t.Errorf("an editor opened: %v", got)
	}
}

// A window tab that is not on screen cannot save or reveal, and opens a document without an editor.
func TestTheCoreDocumentTopicsOfAWorkspaceThatIsNotOnScreen(t *testing.T) {
	p := extensionProcess(t)
	folder := t.TempDir()
	if err := os.WriteFile(filepath.Join(folder, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	win := newExtensionWorkspace(t, p, folder)
	docs := extensionDocuments{w: win}
	opened := make(chan error, 1)
	var doc *document.Document
	docs.OpenFile(context.Background(), filepath.Join(folder, "a.go"), extension.Position{}, func(d *document.Document, err error) { doc = d; opened <- err })
	if err := <-opened; err != nil || doc == nil {
		t.Fatalf("open: %v", err)
	}
	if _, ok := win.Documents.Get(doc.URI()); !ok {
		t.Fatal("the document is not open")
	}
	saveErr := make(chan error, 1)
	docs.Save(context.Background(), doc, func(err error) { saveErr <- err })
	if appErr, ok := errors.FromError(<-saveErr); !ok || appErr.Tag != "ExtDocumentSaveUnavailable" {
		t.Errorf("save: %v", appErr)
	}
	if appErr, ok := errors.FromError(docs.Reveal(doc, extension.Position{})); !ok || appErr.Tag != "ExtDocumentRevealUnavailable" {
		t.Errorf("reveal: %v", appErr)
	}
	if err := docs.Close(doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := win.Documents.Get(doc.URI()); ok {
		t.Error("the document is still open")
	}
}

// An extension that declares privileges and is not emetos.relay is refused: it is left out and the Extensions page says so.
func TestTheExtensionsPageShowsTheExtensionsThatDeclaredPrivileges(t *testing.T) {
	sneaky := func() extension.Extension {
		return &testExtension{manifest: extension.Manifest{
			ID: "ext.sneaky", Name: "Sneaky", ActivationEvents: []string{extension.OnStartup},
			Privileges: []string{extension.PrivilegeCommandsExecute},
		}}
	}
	fine, _ := greeterFactory([]string{extension.OnStartup}, nil)
	p := extensionProcess(t, sneaky, fine)
	if problems := p.Extensions.Problems(); len(problems) != 1 {
		t.Fatalf("problems: %v", problems)
	}
	win := newExtensionWorkspace(t, p, t.TempDir())
	a := &groupApp{t: t, win: win, files: map[string]string{}, docs: map[string]*document.Document{}}
	a.ts = testutil.New(t, testWindowSize)
	a.ts.PumpWidget(newAppDocs(context.Background(), win, nil, nil, ""))
	a.pump()
	a.run("workbench.extensions.action.showInstalledExtensions")
	if !a.ts.Exists(gluttertest.ByKey(extensionsui.ProblemKey("ExtensionPrivilegeRefused", "ext.sneaky"))) {
		t.Error("the page does not show the problem of ext.sneaky")
	}
	if !a.ts.Exists(gluttertest.ByTextContaining("ext.sneaky: only emetos.relay may declare privileges (ExtensionPrivilegeRefused)")) {
		t.Error("the page does not say why")
	}
	if a.ts.Exists(gluttertest.ByKey(extensionsui.RowKey("ext.sneaky"))) {
		t.Error("the refused extension has a row")
	}
	if !a.ts.Exists(gluttertest.ByKey(extensionsui.RowKey("emetos.greeter"))) {
		t.Error("the other extension has no row")
	}
	if win.Extensions().State("ext.sneaky") != exthost.StateInactive {
		t.Error("the refused extension is in the scope")
	}
}
