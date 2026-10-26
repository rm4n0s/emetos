package exthost

import (
	"encoding/json"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/lsp"
	"github.com/rm4n0s/emetos/internal/core/lsp/lsptest"
	"github.com/rm4n0s/emetos/internal/gui/views/problems"
)

// The diagnostics of the language servers (step 12.4), run with the fake server program of core/lsp/lsptest. A publication is
// handed to the controller as the goroutine of the connection does it.

const (
	pullCaps    = `{"textDocumentSync":2,"diagnosticProvider":{"interFileDependencies":false,"workspaceDiagnostics":false}}`
	serverOwner = testExt + "." + testServer
)

// diagServer is a running server with the document a.go open.
type diagServer struct {
	st   *serverTest
	tb   *tab
	c    *serverCtl
	doc  *document.Document
	path string
}

// startDiagServer starts the fake server with a script and opens a.go with a text.
func startDiagServer(t *testing.T, script lsptest.Script, text string) *diagServer {
	t.Helper()
	st := newServerTest(t, script, nil)
	tb := st.startTab("one")
	doc := st.goDoc(tb, "a.go", text)
	st.waitRunning(tb)
	return &diagServer{st: st, tb: tb, c: st.controller(tb), doc: doc, path: doc.URI().Path}
}

// publish sends a publication of diagnostics for a file, as the connection would.
func (d *diagServer) publish(path string, version *int, diags string) {
	d.st.t.Helper()
	var ds []lsp.Diagnostic
	if err := json.Unmarshal([]byte(diags), &ds); err != nil {
		d.st.t.Fatal(err)
	}
	d.c.publishDiagnostics(lsp.PublishDiagnosticsParams{URI: uriOf(path), Version: version, Diagnostics: ds})
}

// markers are the markers the server has set for a file now.
func (d *diagServer) markers(path string) []problems.Marker {
	return d.tb.markers.Read(problems.ReadFilter{Owner: serverOwner, Resource: path})
}

// waitMarkers waits until the file has n markers.
func (d *diagServer) waitMarkers(path string, n int) []problems.Marker {
	d.st.t.Helper()
	var got []problems.Marker
	waitFor(d.st.t, d.tb, "markers of "+filepath.Base(path), func() bool {
		got = d.markers(path)
		return len(got) == n
	})
	return got
}

func ptr(v int) *int { return &v }

func TestPublishedDiagnosticsAreMarkersOwnedByTheServer(t *testing.T) {
	d := startDiagServer(t, simple(), "package a\n\nvar x = y\n")
	d.tb.markers.Set("someone else", d.path, []problems.Marker{{Severity: problems.SeverityInfo, Message: "not the server's"}})
	related := uriOf(filepath.Join(d.st.folder, "b.go"))
	d.publish(d.path, nil, `[
		{"range":{"start":{"line":2,"character":8},"end":{"line":2,"character":9}},"severity":1,"code":"UndeclaredName","source":"compiler",
		 "message":"undefined: y","codeDescription":{"href":"https://pkg.go.dev/y"},
		 "relatedInformation":[{"location":{"uri":"`+related+`","range":{"start":{"line":6,"character":1},"end":{"line":6,"character":4}}},"message":"declared here"}]},
		{"range":{"start":{"line":2,"character":4},"end":{"line":2,"character":5}},"severity":2,"code":7,"message":"unused","tags":[1]},
		{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":7}},"severity":3,"message":"info"},
		{"range":{"start":{"line":1,"character":0},"end":{"line":1,"character":0}},"severity":4,"message":"hint","tags":[2]},
		{"range":{"start":{"line":1,"character":0},"end":{"line":1,"character":0}},"message":"no severity is an error"}
	]`)
	got := d.waitMarkers(d.path, 5)
	byMessage := map[string]problems.Marker{}
	for _, m := range got {
		byMessage[m.Message] = m
		if m.Owner != serverOwner || m.Resource != d.path {
			t.Errorf("marker %q: owner %q resource %q", m.Message, m.Owner, m.Resource)
		}
	}
	e := byMessage["undefined: y"]
	if e.Severity != problems.SeverityError || e.Code != "UndeclaredName" || e.Source != "compiler" || e.CodeHref != "https://pkg.go.dev/y" ||
		e.StartLine != 3 || e.StartColumn != 9 || e.EndLine != 3 || e.EndColumn != 10 {
		t.Errorf("the error = %+v", e)
	}
	if len(e.Related) != 1 || e.Related[0].Resource != filepath.Join(d.st.folder, "b.go") || e.Related[0].Message != "declared here" ||
		e.Related[0].StartLine != 7 || e.Related[0].StartColumn != 2 {
		t.Errorf("the related place = %+v", e.Related)
	}
	if u := byMessage["unused"]; u.Severity != problems.SeverityWarning || !u.Unnecessary || u.Deprecated || u.Code != "7" {
		t.Errorf("the unused warning = %+v", u)
	}
	if i := byMessage["info"]; i.Severity != problems.SeverityInfo || i.StartLine != 1 || i.StartColumn != 1 || i.EndColumn != 8 {
		t.Errorf("the info = %+v", i)
	}
	if h := byMessage["hint"]; h.Severity != problems.SeverityHint || !h.Deprecated || h.Unnecessary {
		t.Errorf("the deprecated hint = %+v", h)
	}
	if n := byMessage["no severity is an error"]; n.Severity != problems.SeverityError {
		t.Errorf("no severity gave %v", n.Severity)
	}
	// the markers of another owner are not touched, and an empty publication takes the server's away
	if all := d.tb.markers.Read(problems.ReadFilter{Resource: d.path}); len(all) != 6 {
		t.Errorf("%d markers in all", len(all))
	}
	d.publish(d.path, nil, `[]`)
	d.waitMarkers(d.path, 0)
	if all := d.tb.markers.Read(problems.ReadFilter{Resource: d.path}); len(all) != 1 || all[0].Owner != "someone else" {
		t.Errorf("after the empty publication: %+v", all)
	}
}

func TestOnlyTheLatestOfTheQueuedPublicationsOfAFileCounts(t *testing.T) {
	d := startDiagServer(t, simple(), "package a\n")
	for i := range 30 {
		d.publish(d.path, nil, `[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":1}},"message":"round `+strconv.Itoa(i)+`"}]`)
	}
	d.publish(d.path, nil, `[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":1}},"message":"the last"}]`)
	waitFor(t, d.tb, "the last publication", func() bool {
		ms := d.markers(d.path)
		return len(ms) == 1 && ms[0].Message == "the last"
	})
}

func TestDiagnosticsAreKeptPerVersionOfTheDocument(t *testing.T) {
	d := startDiagServer(t, simple(), "package a\n")
	one := `[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":7}},"message":"for the version"}]`
	v := d.doc.Version()
	d.publish(d.path, ptr(v), one)
	d.waitMarkers(d.path, 1)

	if _, err := d.doc.Edit([]document.Edit{{Text: "// x\n"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	if d.doc.Version() == v {
		t.Fatal("the edit did not change the version")
	}
	// a publication for the old version is dropped: its positions are not those of the text
	d.publish(d.path, ptr(v), `[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":1}},"message":"stale"},{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":1}},"message":"stale too"}]`)
	// ... which the next one, for the version of the text, shows
	d.publish(d.path, ptr(d.doc.Version()), `[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":5}},"message":"current"}]`)
	waitFor(t, d.tb, "the current publication", func() bool {
		ms := d.markers(d.path)
		return len(ms) == 1 && ms[0].Message == "current"
	})
	// no version: the text is taken to be the one of the server
	d.publish(d.path, nil, `[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":5}},"message":"no version"}]`)
	waitFor(t, d.tb, "the publication without a version", func() bool {
		ms := d.markers(d.path)
		return len(ms) == 1 && ms[0].Message == "no version"
	})
}

func TestColumnsOfAnyEncodingBecomeUTF16Columns(t *testing.T) {
	const text = "a😀b\n" // a, a letter of 4 bytes and 2 UTF-16 units, b
	for _, tc := range []struct {
		name         string
		capabilities string
		from, to     int // the columns of b in the encoding of the server
		wantFrom     int // counted from 1, in UTF-16 units
	}{
		{"utf-16", serverCaps, 3, 4, 4},
		{"utf-8", `{"textDocumentSync":2,"positionEncoding":"utf-8"}`, 5, 6, 4},
		{"utf-32", `{"textDocumentSync":2,"positionEncoding":"utf-32"}`, 2, 3, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			script := lsptest.Script{Capabilities: raw(tc.capabilities)}
			d := startDiagServer(t, script, text)
			d.publish(d.path, nil, `[{"range":{"start":{"line":0,"character":`+strconv.Itoa(tc.from)+`},"end":{"line":0,"character":`+strconv.Itoa(tc.to)+`}},"message":"b"}]`)
			ms := d.waitMarkers(d.path, 1)
			if ms[0].StartColumn != tc.wantFrom || ms[0].EndColumn != tc.wantFrom+1 {
				t.Errorf("the marker is at %d-%d, want %d-%d", ms[0].StartColumn, ms[0].EndColumn, tc.wantFrom, tc.wantFrom+1)
			}
		})
	}
}

func TestDiagnosticsOfAFileThatIsNotOpenAreReadFromDisk(t *testing.T) {
	script := lsptest.Script{Capabilities: raw(`{"textDocumentSync":2,"positionEncoding":"utf-8"}`)}
	d := startDiagServer(t, script, "package a\n")
	closed := filepath.Join(d.st.folder, "closed.go")
	d.tb.lsp.mu.Lock()
	d.tb.lsp.disk[closed] = "é😀b\n"
	d.tb.lsp.mu.Unlock()
	d.publish(closed, nil, `[{"range":{"start":{"line":0,"character":6},"end":{"line":0,"character":7}},"severity":2,"message":"in a file that is not open"}]`)
	ms := d.waitMarkers(closed, 1)
	if ms[0].StartLine != 1 || ms[0].StartColumn != 4 || ms[0].Severity != problems.SeverityWarning {
		t.Errorf("marker = %+v", ms[0])
	}
}

func TestPushedMarkersStayWhenTheDocumentClosesAndGoWhenTheServerStops(t *testing.T) {
	d := startDiagServer(t, simple(), "package a\n")
	d.publish(d.path, nil, `[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":1}},"message":"stays"}]`)
	d.waitMarkers(d.path, 1)
	d.tb.lsp.closeDoc(d.doc)
	d.tb.pump()
	time.Sleep(50 * time.Millisecond)
	d.tb.pump()
	if got := d.markers(d.path); len(got) != 1 {
		t.Fatalf("closing the document took the pushed markers away: %+v", got)
	}
	// restarting the server (as after a crash) clears what it had said, and so does stopping it
	d.c.restart()
	if got := d.markers(d.path); len(got) != 0 {
		t.Errorf("a restarted server keeps %d markers", len(got))
	}
	d.st.waitRunning(d.tb)
	d.publish(d.path, nil, `[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":1}},"message":"again"}]`)
	d.waitMarkers(d.path, 1)
	d.tb.scope.Close()
	if got := d.tb.markers.Read(problems.ReadFilter{Owner: serverOwner}); len(got) != 0 {
		t.Errorf("the closed tab leaves %d markers", len(got))
	}
}

func TestACrashedServerLeavesNoMarkers(t *testing.T) {
	d := startDiagServer(t, simple(), "package a\n")
	d.publish(d.path, nil, `[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":1}},"message":"before the crash"}]`)
	d.waitMarkers(d.path, 1)
	client := d.c.currentClient()
	_ = client.Call(t.Context(), lsptest.MethodCrash, nil, nil)
	waitFor(t, d.tb, "the markers to go", func() bool { return len(d.markers(d.path)) == 0 })
}

func TestAPublicationFromAnOldServerIsNotShownAfterARestart(t *testing.T) {
	d := startDiagServer(t, simple(), "package a\n")
	d.c.mu.Lock()
	old := d.c.gen
	d.c.mu.Unlock()
	d.c.restart()
	d.st.waitRunning(d.tb)
	// what the old connection had queued is applied by the worker after the restart: it is dropped
	done := make(chan struct{})
	go func() {
		defer close(done)
		d.c.applyPublication(queuedPublish{gen: old, params: lsp.PublishDiagnosticsParams{
			URI: uriOf(d.path), Diagnostics: []lsp.Diagnostic{{Message: "late"}},
		}})
	}()
	waitFor(t, d.tb, "the old publication to be dealt with", func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	})
	if got := d.markers(d.path); len(got) != 0 {
		t.Errorf("a late publication was shown: %+v", got)
	}
}

// ---- pull diagnostics ----

func report(kind, id string, items string) lsptest.Response {
	body := `{"kind":"` + kind + `"`
	if id != "" {
		body += `,"resultId":"` + id + `"`
	}
	if items != "" {
		body += `,"items":` + items
	}
	return lsptest.Response{Result: raw(body + `}`)}
}

func item(msg string) string {
	return `{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":3}},"message":"` + msg + `"}`
}

// pullRequests are the textDocument/diagnostic requests the server got.
func pullRequests(t *testing.T, c *serverCtl) []lsptest.Message {
	t.Helper()
	var out []lsptest.Message
	for _, m := range traffic(t, c) {
		if m.Method == lsp.MethodDocumentDiagnostic && m.FromClient && m.Kind == "request" {
			out = append(out, m)
		}
	}
	return out
}

func TestPulledDiagnosticsOnOpenAndOnChange(t *testing.T) {
	script := lsptest.Script{Capabilities: raw(pullCaps), Responses: map[string]lsptest.Response{
		lsp.MethodDocumentDiagnostic: {Sequence: []lsptest.Response{
			report("full", "r1", `[`+item("first")+`]`),
			report("full", "r2", `[`+item("second")+`,`+item("third")+`]`),
			report("unchanged", "r2", ""),
		}},
	}}
	d := startDiagServer(t, script, "package a\n")
	ms := d.waitMarkers(d.path, 1)
	if ms[0].Message != "first" || ms[0].Owner != serverOwner {
		t.Fatalf("pulled on open: %+v", ms)
	}
	// a change pulls again after a quiet time, with the id of the last report
	if _, err := d.doc.Edit([]document.Edit{{Text: "// x\n"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	d.waitMarkers(d.path, 2)
	// an unchanged report keeps what is there
	if _, err := d.doc.Edit([]document.Edit{{Text: "// y\n"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, d.tb, "the third request", func() bool { return len(pullRequests(t, d.c)) >= 3 })
	time.Sleep(50 * time.Millisecond)
	d.tb.pump()
	if got := d.markers(d.path); len(got) != 2 {
		t.Errorf("an unchanged report changed the markers: %+v", got)
	}
	reqs := pullRequests(t, d.c)
	var params [3]lsp.DocumentDiagnosticParams
	for i, r := range reqs {
		if err := json.Unmarshal(r.Body, &struct {
			Params *lsp.DocumentDiagnosticParams `json:"params"`
		}{&params[i]}); err != nil {
			t.Fatal(err)
		}
	}
	if params[0].PreviousResultID != "" || params[1].PreviousResultID != "r1" || params[2].PreviousResultID != "r2" {
		t.Errorf("previous result ids: %q %q %q", params[0].PreviousResultID, params[1].PreviousResultID, params[2].PreviousResultID)
	}
}

func TestPulledMarkersGoWhenTheDocumentCloses(t *testing.T) {
	script := lsptest.Script{Capabilities: raw(pullCaps), Responses: map[string]lsptest.Response{
		lsp.MethodDocumentDiagnostic: report("full", "r1", `[`+item("pulled")+`]`),
	}}
	d := startDiagServer(t, script, "package a\n")
	d.waitMarkers(d.path, 1)
	// markers that were pushed for another file stay: only the pulled ones of a closed document go
	other := filepath.Join(d.st.folder, "other.go")
	d.tb.lsp.mu.Lock()
	d.tb.lsp.disk[other] = "package o\n"
	d.tb.lsp.mu.Unlock()
	d.publish(other, nil, `[`+item("pushed")+`]`)
	d.waitMarkers(other, 1)
	d.tb.lsp.closeDoc(d.doc)
	d.tb.pump()
	if got := d.markers(d.path); len(got) != 0 {
		t.Errorf("pulled markers of a closed document stay: %+v", got)
	}
	if got := d.markers(other); len(got) != 1 {
		t.Errorf("the pushed markers of another file went: %+v", got)
	}
}

func TestAReportForAnOlderVersionIsDroppedAndPulledAgain(t *testing.T) {
	script := lsptest.Script{Capabilities: raw(pullCaps), Responses: map[string]lsptest.Response{
		lsp.MethodDocumentDiagnostic: {Sequence: []lsptest.Response{
			func() lsptest.Response { r := report("full", "r1", `[`+item("stale")+`]`); r.DelayMS = 400; return r }(),
			report("full", "r2", `[`+item("fresh")+`]`),
		}},
	}}
	d := startDiagServer(t, script, "package a\n")
	waitFor(t, d.tb, "the first request", func() bool { return len(pullRequests(t, d.c)) >= 1 })
	// the document changes while the request is on its way
	if _, err := d.doc.Edit([]document.Edit{{Text: "// x\n"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, d.tb, "the fresh report", func() bool {
		ms := d.markers(d.path)
		return len(ms) == 1 && ms[0].Message == "fresh"
	})
}

func TestTheServerCanAskForTheDiagnosticsToBePulledAgain(t *testing.T) {
	script := lsptest.Script{Capabilities: raw(pullCaps), Responses: map[string]lsptest.Response{
		lsp.MethodDocumentDiagnostic: {Sequence: []lsptest.Response{
			report("full", "r1", `[`+item("one")+`]`),
			report("full", "r2", `[`+item("one")+`,`+item("two")+`]`),
		}},
	}, Send: []lsptest.Outgoing{{After: lsp.MethodDidOpen, Method: lsp.MethodDiagnosticRefresh, Request: true}}}
	d := startDiagServer(t, script, "package a\n")
	// the refresh request that follows didOpen pulls the open documents again
	d.waitMarkers(d.path, 2)
	if n := len(pullRequests(t, d.c)); n < 2 {
		t.Errorf("%d pull requests", n)
	}
}

func TestPullReportsMayCarryRelatedDocuments(t *testing.T) {
	script := lsptest.Script{Capabilities: raw(pullCaps)}
	st := newServerTest(t, script, nil)
	rel := filepath.Join(st.folder, "rel.go")
	script.Responses = map[string]lsptest.Response{lsp.MethodDocumentDiagnostic: report("full", "r1", `[`+item("here")+`]`)}
	script.Responses[lsp.MethodDocumentDiagnostic] = lsptest.Response{Result: raw(`{"kind":"full","items":[` + item("here") + `],"relatedDocuments":{"` + uriOf(rel) + `":{"kind":"full","items":[` + item("there") + `]}}}`)}
	st.writeScript(script)
	tb := st.startTab("one")
	doc := st.goDoc(tb, "a.go", "package a\n")
	tb.lsp.mu.Lock()
	tb.lsp.disk[rel] = "package r\n"
	tb.lsp.mu.Unlock()
	st.waitRunning(tb)
	d := &diagServer{st: st, tb: tb, c: st.controller(tb), doc: doc, path: doc.URI().Path}
	d.waitMarkers(d.path, 1)
	if ms := d.waitMarkers(rel, 1); ms[0].Message != "there" {
		t.Errorf("related document markers = %+v", ms)
	}
}
