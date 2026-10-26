package lsp_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/fileio"
	"github.com/rm4n0s/emetos/internal/core/lsp"
	"github.com/rm4n0s/emetos/internal/core/lsp/lsptest"
)

// The capabilities of the servers the sync tests use.
const (
	capsUTF16     = `{"textDocumentSync":2}`
	capsUTF8      = `{"positionEncoding":"utf-8","textDocumentSync":2}`
	capsUTF32     = `{"positionEncoding":"utf-32","textDocumentSync":2}`
	capsFull      = `{"textDocumentSync":1}`
	capsFullUTF8  = `{"positionEncoding":"utf-8","textDocumentSync":1}`
	capsSaveAware = `{"textDocumentSync":{"openClose":true,"change":2,"willSave":true,"willSaveWaitUntil":true,"save":{"includeText":true}}}`
)

func newDoc(path, text string, eol fileio.LineEnding) *document.Document {
	opts := document.NewOptions()
	opts.LanguageID = "go"
	opts.LineEnding = eol
	return document.New(document.FileURI(path), text, opts)
}

func pos(line, col int) buffer.Position { return buffer.Position{Line: line, Col: col} }

func rng(l1, c1, l2, c2 int) buffer.Range { return buffer.Range{Start: pos(l1, c1), End: pos(l2, c2)} }

func edit(t *testing.T, d *document.Document, edits ...document.Edit) {
	t.Helper()
	if _, err := d.Edit(edits, document.EditOptions{}); err != nil {
		t.Fatalf("Edit: %v", err)
	}
}

// synced checks that the server's copy is the document, as the server must see it, with no violation.
func synced(t *testing.T, h *lsptest.Harness, d *document.Document) {
	t.Helper()
	got := h.Document(lsp.DocumentURI(d.URI()))
	if !got.Open {
		t.Fatalf("the server does not have %s open", d.URI())
	}
	if want := lsp.ServerText(d); got.Text != want {
		t.Errorf("server text = %q, want %q", got.Text, want)
	}
	if got.Version != d.Version() {
		t.Errorf("server version = %d, want %d", got.Version, d.Version())
	}
	if v := h.Violations(); len(v) > 0 {
		t.Errorf("protocol violations: %v", v)
	}
}

// changesOf returns the contentChanges of the didChange notifications the server got, in order.
func changesOf(t *testing.T, h *lsptest.Harness) [][]lsp.TextDocumentContentChangeEvent {
	t.Helper()
	var out [][]lsp.TextDocumentContentChangeEvent
	for _, m := range h.Traffic() {
		if m.Method != lsp.MethodDidChange || !m.FromClient {
			continue
		}
		var msg struct {
			Params lsp.DidChangeTextDocumentParams `json:"params"`
		}
		if err := json.Unmarshal(m.Body, &msg); err != nil {
			t.Fatal(err)
		}
		out = append(out, msg.Params.ContentChanges)
	}
	return out
}

func TestOpenSendsTheDocument(t *testing.T) {
	tests := []struct {
		name string
		eol  fileio.LineEnding
		text string
		want string
	}{
		{"lf", fileio.LF, "package a\nfunc é() {}\n", "package a\nfunc é() {}\n"},
		{"crlf", fileio.CRLF, "package a\nfunc é() {}\n", "package a\r\nfunc é() {}\r\n"},
		{"empty", fileio.LF, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := lsptest.Start(t, lsptest.Options{Script: script(t, capsUTF16)})
			d := newDoc("/work/my file.go", tt.text, tt.eol)
			sd, err := h.Client.OpenDocument(d)
			if err != nil {
				t.Fatal(err)
			}
			if sd.URI() != "file:///work/my%20file.go" || sd.Document() != d {
				t.Errorf("URI = %s", sd.URI())
			}
			got := h.Document("file:///work/my%20file.go")
			if got.Text != tt.want || got.LanguageID != "go" || got.Version != 1 || !got.Open {
				t.Errorf("server copy = %+v", got)
			}
		})
	}
}

func TestOpenTwiceFails(t *testing.T) {
	h := lsptest.Start(t, lsptest.Options{Script: script(t, capsUTF16)})
	d := newDoc("/work/a.go", "x", fileio.LF)
	if _, err := h.Client.OpenDocument(d); err != nil {
		t.Fatal(err)
	}
	_, err := h.Client.OpenDocument(d)
	appErr := wantRouteError(t, err, "Client.OpenDocument.DocumentAlreadyOpen")
	if appErr.Metadata["uri"] != "file:///work/a.go" {
		t.Errorf("metadata = %v", appErr.Metadata)
	}
}

func TestIncrementalChangesAreConvertedToTheEncoding(t *testing.T) {
	// "a😀b": the emoji is 4 bytes and 2 UTF-16 units.
	tests := []struct {
		caps string
		want lsp.Range
	}{
		{capsUTF16, lsp.Range{Start: lsp.Position{Line: 0, Character: 3}, End: lsp.Position{Line: 0, Character: 3}}},
		{capsUTF8, lsp.Range{Start: lsp.Position{Line: 0, Character: 5}, End: lsp.Position{Line: 0, Character: 5}}},
		{capsUTF32, lsp.Range{Start: lsp.Position{Line: 0, Character: 2}, End: lsp.Position{Line: 0, Character: 2}}},
	}
	for _, tt := range tests {
		t.Run(tt.caps, func(t *testing.T) {
			h := lsptest.Start(t, lsptest.Options{Script: script(t, tt.caps)})
			d := newDoc("/work/a.go", "a😀b", fileio.LF)
			if _, err := h.Client.OpenDocument(d); err != nil {
				t.Fatal(err)
			}
			edit(t, d, document.Edit{Range: rng(0, 5, 0, 5), Text: "X"})
			synced(t, h, d)
			ch := changesOf(t, h)
			if len(ch) != 1 || len(ch[0]) != 1 || ch[0][0].Range == nil || *ch[0][0].Range != tt.want || ch[0][0].Text != "X" {
				t.Errorf("didChange = %+v, want an insert of X at %+v", ch, tt.want)
			}
		})
	}
}

func TestMultipleCursorsAreSentLastFirst(t *testing.T) {
	h := lsptest.Start(t, lsptest.Options{Script: script(t, capsUTF16)})
	d := newDoc("/work/a.go", "😀 one\ntwo €\nthree", fileio.LF)
	if _, err := h.Client.OpenDocument(d); err != nil {
		t.Fatal(err)
	}
	// Three edits of one event, two of them on the same line as non-ASCII text.
	edit(t, d,
		document.Edit{Range: rng(0, 5, 0, 8), Text: "1"}, // "one" after the emoji and a space
		document.Edit{Range: rng(1, 0, 1, 3), Text: "TWO"},
		document.Edit{Range: rng(1, 7, 1, 7), Text: "!\n"}, // after the euro sign
	)
	synced(t, h, d)
	ch := changesOf(t, h)
	if len(ch) != 1 || len(ch[0]) != 3 {
		t.Fatalf("didChange = %+v", ch)
	}
	var starts []lsp.Position
	for _, c := range ch[0] {
		starts = append(starts, c.Range.Start)
	}
	want := []lsp.Position{{Line: 1, Character: 5}, {Line: 1, Character: 0}, {Line: 0, Character: 3}}
	for i := range want {
		if starts[i] != want[i] {
			t.Errorf("change %d starts at %+v, want %+v (last position first)", i, starts[i], want[i])
		}
	}
}

func TestEditsUndoRedoAndReplacements(t *testing.T) {
	for _, caps := range []string{capsUTF16, capsUTF8, capsFull, capsFullUTF8} {
		for _, eol := range []fileio.LineEnding{fileio.LF, fileio.CRLF} {
			t.Run(caps+string(eol), func(t *testing.T) {
				h := lsptest.Start(t, lsptest.Options{Script: script(t, caps)})
				d := newDoc("/work/a.go", "one\ntwo é\n😀 three", eol)
				if _, err := h.Client.OpenDocument(d); err != nil {
					t.Fatal(err)
				}
				steps := []struct {
					name string
					do   func()
				}{
					{"insert", func() { edit(t, d, document.Edit{Range: rng(1, 0, 1, 0), Text: "new\nlines\n"}) }},
					{"delete across lines", func() { edit(t, d, document.Edit{Range: rng(0, 1, 2, 2), Text: ""}) }},
					{"replace with a line break", func() { edit(t, d, document.Edit{Range: rng(0, 0, 0, 1), Text: "a\r\nb"}) }},
					{"undo", func() { mustHistory(t, d.Undo) }},
					{"undo again", func() { mustHistory(t, d.Undo) }},
					{"redo", func() { mustHistory(t, d.Redo) }},
					{"set text", func() { d.SetText("replaced é €\r\n😀\r\n") }},
					{"edit after set text", func() { edit(t, d, document.Edit{Range: rng(1, 4, 1, 4), Text: "X"}) }},
					{"empty the document", func() { edit(t, d, document.Edit{Range: rng(0, 0, 9, 0), Text: ""}) }},
					{"type into the empty document", func() { edit(t, d, document.Edit{Range: rng(0, 0, 0, 0), Text: "é\n"}) }},
				}
				for _, step := range steps {
					step.do()
					synced(t, h, d)
					if t.Failed() {
						t.Fatalf("diverged after %q", step.name)
					}
				}
			})
		}
	}
}

func mustHistory(t *testing.T, f func() (document.HistoryResult, error)) {
	t.Helper()
	if _, err := f(); err != nil {
		t.Fatal(err)
	}
}

func TestLineEndingChange(t *testing.T) {
	h := lsptest.Start(t, lsptest.Options{Script: script(t, capsUTF16)})
	d := newDoc("/work/a.go", "one\ntwo\n", fileio.LF)
	if _, err := h.Client.OpenDocument(d); err != nil {
		t.Fatal(err)
	}
	if err := d.SetLineEnding(fileio.CRLF); err != nil {
		t.Fatal(err)
	}
	synced(t, h, d)
	if got := h.Document("file:///work/a.go").Text; got != "one\r\ntwo\r\n" {
		t.Errorf("text = %q", got)
	}
	edit(t, d, document.Edit{Range: rng(1, 3, 1, 3), Text: "\nthree"})
	synced(t, h, d)
	mustHistory(t, d.Undo)
	synced(t, h, d)
	mustHistory(t, d.Undo) // the line ending goes back
	synced(t, h, d)
	if got := h.Document("file:///work/a.go").Text; got != "one\ntwo\n" {
		t.Errorf("text after undoing the line ending = %q", got)
	}
	// Setting the same line ending changes nothing and sends nothing.
	before := h.Server.Count(lsp.MethodDidChange)
	if err := d.SetLineEnding(fileio.LF); err != nil {
		t.Fatal(err)
	}
	if h.Server.Count(lsp.MethodDidChange) != before {
		t.Errorf("a no-op line ending change was sent")
	}
}

func TestVersionsGoUp(t *testing.T) {
	h := lsptest.Start(t, lsptest.Options{Script: script(t, capsUTF16)})
	d := newDoc("/work/a.go", "x", fileio.LF)
	if _, err := h.Client.OpenDocument(d); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		edit(t, d, document.Edit{Range: rng(0, 0, 0, 0), Text: "y"})
	}
	mustHistory(t, d.Undo)
	mustHistory(t, d.Redo)
	synced(t, h, d) // the server's Violations include a version that does not go up
	if got := h.Document("file:///work/a.go").Version; got != d.Version() || got < 8 {
		t.Errorf("version = %d", got)
	}
}

func TestCloseSendsDidCloseAndStopsFollowing(t *testing.T) {
	h := lsptest.Start(t, lsptest.Options{Script: script(t, capsUTF16)})
	d := newDoc("/work/a.go", "x", fileio.LF)
	sd, err := h.Client.OpenDocument(d)
	if err != nil {
		t.Fatal(err)
	}
	sd.Close()
	sd.Close() // harmless
	edit(t, d, document.Edit{Range: rng(0, 0, 0, 0), Text: "y"})
	if got := h.Document("file:///work/a.go"); got.Open || got.Text != "x" {
		t.Errorf("server copy after close = %+v", got)
	}
	if n := h.Server.Count(lsp.MethodDidClose); n != 1 {
		t.Errorf("didClose sent %d times", n)
	}
	if n := h.Server.Count(lsp.MethodDidChange); n != 0 {
		t.Errorf("didChange sent %d times after close", n)
	}
	// It can be opened again.
	sd2, err := h.Client.OpenDocument(d)
	if err != nil {
		t.Fatal(err)
	}
	defer sd2.Close()
	synced(t, h, d)
}

func TestRenameClosesAndReopens(t *testing.T) {
	h := lsptest.Start(t, lsptest.Options{Script: script(t, capsUTF16)})
	d := newDoc("/work/a.go", "x\n", fileio.LF)
	sd, err := h.Client.OpenDocument(d)
	if err != nil {
		t.Fatal(err)
	}
	d.SetURI(document.FileURI("/work/b.go"))
	if sd.URI() != "file:///work/b.go" {
		t.Errorf("URI = %s", sd.URI())
	}
	if old := h.Document("file:///work/a.go"); old.Open {
		t.Errorf("the old URI is still open")
	}
	synced(t, h, d)
	edit(t, d, document.Edit{Range: rng(0, 1, 0, 1), Text: "y"})
	synced(t, h, d)
	// Opening a.go again is allowed: the old name is free.
	other := newDoc("/work/a.go", "other", fileio.LF)
	if _, err := h.Client.OpenDocument(other); err != nil {
		t.Errorf("the old name is not free: %v", err)
	}
}

func TestUntitledDocument(t *testing.T) {
	h := lsptest.Start(t, lsptest.Options{Script: script(t, capsUTF16)})
	opts := document.NewOptions()
	d := document.New(document.UntitledURI(2), "hello", opts)
	if _, err := h.Client.OpenDocument(d); err != nil {
		t.Fatal(err)
	}
	edit(t, d, document.Edit{Range: rng(0, 5, 0, 5), Text: "!"})
	if got := h.Document("untitled:Untitled-2"); got.Text != "hello!" || !got.Open {
		t.Errorf("untitled copy = %+v", got)
	}
}

func TestSavingSendsWillSaveAndDidSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.go")
	if err := os.WriteFile(path, []byte("one\r\ntwo\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := fileio.Read(context.Background(), path, fileio.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	d := document.FromRead(document.FileURI(path), res, document.NewOptions())

	h := lsptest.Start(t, lsptest.Options{
		Script: lsptest.Script{
			Capabilities: raw(capsSaveAware),
			Responses: map[string]lsptest.Response{"textDocument/willSaveWaitUntil": {
				Result: raw(`[{"range":{"start":{"line":1,"character":3},"end":{"line":1,"character":3}},"newText":"\n"}]`),
			}},
		},
	})
	sd, err := h.Client.OpenDocument(d)
	if err != nil {
		t.Fatal(err)
	}
	edit(t, d, document.Edit{Range: rng(0, 3, 0, 3), Text: " é"})

	sd.WillSave(lsp.SaveReasonManual)
	edits, err := sd.WillSaveWaitUntil(ctxT(t), lsp.SaveReasonManual)
	if err != nil || len(edits) != 1 || edits[0].NewText != "\n" {
		t.Errorf("WillSaveWaitUntil = %+v, %v", edits, err)
	}
	if _, err := d.Save(context.Background(), document.SaveOptions{}); err != nil {
		t.Fatal(err)
	}
	synced(t, h, d)

	var willSave lsp.WillSaveTextDocumentParams
	var didSave lsp.DidSaveTextDocumentParams
	for _, m := range h.Traffic() {
		var msg struct {
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(m.Body, &msg); err != nil {
			t.Fatal(err)
		}
		switch m.Method {
		case lsp.MethodWillSave:
			if err := json.Unmarshal(msg.Params, &willSave); err != nil {
				t.Fatal(err)
			}
		case lsp.MethodDidSave:
			if err := json.Unmarshal(msg.Params, &didSave); err != nil {
				t.Fatal(err)
			}
		}
	}
	if willSave.Reason != lsp.SaveReasonManual || willSave.TextDocument.URI != sd.URI() {
		t.Errorf("willSave = %+v", willSave)
	}
	if didSave.Text == nil || *didSave.Text != "one é\r\ntwo\r\n" {
		t.Errorf("didSave text = %v", didSave.Text)
	}
	if h.Server.Count(lsp.MethodDidSave) != 1 {
		t.Errorf("didSave sent %d times", h.Server.Count(lsp.MethodDidSave))
	}
}

func TestSaveAsMovesTheDocument(t *testing.T) {
	dir := t.TempDir()
	h := lsptest.Start(t, lsptest.Options{Script: lsptest.Script{Capabilities: raw(capsSaveAware)}})
	d := document.New(document.UntitledURI(1), "text é", document.NewOptions())
	if _, err := h.Client.OpenDocument(d); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "new.go")
	if _, err := d.SaveAs(context.Background(), path, document.SaveOptions{}); err != nil {
		t.Fatal(err)
	}
	uri := lsp.DocumentURI(document.FileURI(path))
	if got := h.Document("untitled:Untitled-1"); got.Open {
		t.Errorf("the untitled document is still open")
	}
	if got := h.Document(uri); !got.Open || got.Text != "text é" {
		t.Errorf("saved copy = %+v", got)
	}
	if h.Server.Count(lsp.MethodDidSave) != 1 {
		t.Errorf("didSave sent %d times", h.Server.Count(lsp.MethodDidSave))
	}
	synced(t, h, d)
}

func TestWhatTheServerDoesNotWantIsNotSent(t *testing.T) {
	tests := []struct {
		name string
		caps string
	}{
		{"no synchronization", `{}`},
		{"change kind none", `{"textDocumentSync":{"openClose":true,"change":0}}`},
		{"open and close off", `{"textDocumentSync":{"openClose":false,"change":2}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := lsptest.Start(t, lsptest.Options{Script: script(t, tt.caps)})
			d := newDoc("/work/a.go", "x", fileio.LF)
			sd, err := h.Client.OpenDocument(d)
			if err != nil {
				t.Fatal(err)
			}
			edit(t, d, document.Edit{Range: rng(0, 1, 0, 1), Text: "y"})
			sd.WillSave(lsp.SaveReasonManual)
			if edits, err := sd.WillSaveWaitUntil(ctxT(t), lsp.SaveReasonManual); edits != nil || err != nil {
				t.Errorf("WillSaveWaitUntil = %v, %v", edits, err)
			}
			sd.Close()
			h.Violations() // a question after everything: the server has handled it all when it answers
			counts := []string{lsp.MethodDidOpen, lsp.MethodDidChange, lsp.MethodDidClose, lsp.MethodWillSave, lsp.MethodWillSaveWU}
			if tt.name == "change kind none" {
				// open and close are wanted; changes are not.
				if h.Server.Count(lsp.MethodDidOpen) != 1 || h.Server.Count(lsp.MethodDidClose) != 1 || h.Server.Count(lsp.MethodDidChange) != 0 {
					t.Errorf("open %d change %d close %d", h.Server.Count(lsp.MethodDidOpen), h.Server.Count(lsp.MethodDidChange), h.Server.Count(lsp.MethodDidClose))
				}
				return
			}
			for _, m := range counts {
				if n := h.Server.Count(m); n != 0 {
					t.Errorf("%s was sent %d times", m, n)
				}
			}
		})
	}
}

func TestDynamicRegistrationOfSynchronization(t *testing.T) {
	h := lsptest.Start(t, lsptest.Options{Script: script(t, `{}`)})
	reg := h.Server.SendRequest("client/registerCapability", raw(`{"registrations":[
		{"id":"1","method":"textDocument/didOpen","registerOptions":{"documentSelector":[{"language":"go"}]}},
		{"id":"2","method":"textDocument/didChange","registerOptions":{"documentSelector":[{"language":"go"}],"syncKind":2}},
		{"id":"3","method":"textDocument/didClose","registerOptions":{"documentSelector":[{"language":"go"}]}}]}`))
	<-reg
	d := newDoc("/work/a.go", "x", fileio.LF)
	if _, err := h.Client.OpenDocument(d); err != nil {
		t.Fatal(err)
	}
	edit(t, d, document.Edit{Range: rng(0, 1, 0, 1), Text: "y"})
	synced(t, h, d)
	if ch := changesOf(t, h); len(ch) != 1 || ch[0][0].Range == nil {
		t.Errorf("changes = %+v, want one incremental change", ch)
	}
}

func TestSyncAfterTheServerDiedIsHarmless(t *testing.T) {
	h := lsptest.Start(t, lsptest.Options{Script: script(t, capsSaveAware)})
	d := newDoc("/work/a.go", "x", fileio.LF)
	sd, err := h.Client.OpenDocument(d)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Client.Close(); err != nil {
		t.Fatal(err)
	}
	// None of these may panic, block or report: the host learns of the end from Done.
	edit(t, d, document.Edit{Range: rng(0, 1, 0, 1), Text: "y"})
	d.SetURI(document.FileURI("/work/b.go"))
	sd.WillSave(lsp.SaveReasonManual)
	_, err = sd.WillSaveWaitUntil(ctxT(t), lsp.SaveReasonManual)
	wantRoute(t, err, "Conn.Call.ServerClosed")
	sd.Close()
}

func TestServerTextOfAPlainDocument(t *testing.T) {
	d := newDoc("/work/a.go", "a\nb\n", fileio.CRLF)
	if got := lsp.ServerText(d); got != "a\r\nb\r\n" || strings.Contains(d.Text(), "\r") {
		t.Errorf("ServerText = %q", got)
	}
}
