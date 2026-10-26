package exthost

import (
	"context"
	"sync"
	"time"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/core/lsp"
	"github.com/rm4n0s/emetos/internal/gui/views/problems"
)

// Diagnostics of the language servers (SPEC §9.12 "Diagnostics", "As built (step 12.4)"): what a server publishes
// (textDocument/publishDiagnostics) or answers to a pull (textDocument/diagnostic) becomes the Problems markers of the
// server, owned by it. The editors show the markers of their files as squiggles (gui/app follows the marker service).
//
// The behavior is that of vscode-languageclient (client.ts handleDiagnostics, diagnostic.ts DiagnosticFeature), which is not in
// VS Code's src/: it is followed from its documentation, not ported.

// pullDelay is how long after the last change of a document its diagnostics are pulled (the delay of vscode-languageclient's
// pull-on-type).
const pullDelay = 250 * time.Millisecond

// diagState is what a server controller keeps for its diagnostics. mu guards queue and busy; the rest belongs to the UI thread.
type diagState struct {
	mu sync.Mutex
	// queue holds the latest publication per URI that was not applied yet, in the order the URIs first came; busy says that a
	// worker is draining it (one worker keeps the publications of a URI in order).
	queue map[string]queuedPublish
	order []string
	busy  bool

	// pulled marks the documents whose markers come from a pull (they go when the document closes: the server does not
	// republish for a closed document), results holds the result id of the last full report per URI, and timers the debounce
	// of the pull of each document.
	pulled  map[document.URI]bool
	results map[document.URI]string
	timers  map[document.URI]*time.Timer
}

// queuedPublish is a publication with the generation of the client it came from.
type queuedPublish struct {
	params lsp.PublishDiagnosticsParams
	gen    int
}

func newDiagState() diagState {
	return diagState{
		queue:   map[string]queuedPublish{},
		pulled:  map[document.URI]bool{},
		results: map[document.URI]string{},
		timers:  map[document.URI]*time.Timer{},
	}
}

// markerOwner is the owner of the markers of the server in the Problems service.
func (c *serverCtl) markerOwner() string { return c.qid }

// publishDiagnostics receives textDocument/publishDiagnostics on a goroutine of the connection and hands it to the worker;
// a publication of a URI that waits is replaced by the newer one.
func (c *serverCtl) publishDiagnostics(p lsp.PublishDiagnosticsParams) {
	if c.s.opts.Markers == nil || c.isStopped() {
		return
	}
	c.mu.Lock()
	gen := c.gen
	c.mu.Unlock()
	c.diag.mu.Lock()
	if _, waiting := c.diag.queue[p.URI]; !waiting {
		c.diag.order = append(c.diag.order, p.URI)
	}
	c.diag.queue[p.URI] = queuedPublish{params: p, gen: gen}
	start := !c.diag.busy
	c.diag.busy = true
	c.diag.mu.Unlock()
	if start {
		c.s.h.servers.Add(1)
		go func() {
			defer c.s.h.servers.Done()
			c.drainPublications()
		}()
	}
}

// drainPublications applies the queued publications one after the other until there are none.
func (c *serverCtl) drainPublications() {
	for {
		c.diag.mu.Lock()
		if len(c.diag.order) == 0 {
			c.diag.busy = false
			c.diag.mu.Unlock()
			return
		}
		uri := c.diag.order[0]
		c.diag.order = c.diag.order[1:]
		q := c.diag.queue[uri]
		delete(c.diag.queue, uri)
		c.diag.mu.Unlock()
		c.applyPublication(q)
	}
}

// openDoc is what the UI thread knows of a document that is open in the window.
type openDoc struct {
	doc     *document.Document
	version int
	text    lsp.Lines
}

// openInfo asks the UI thread for the open document with the URI: its version and a snapshot of its text. The answer is false
// when the document is not open or the server ended meanwhile (ok is then false too).
func (c *serverCtl) openInfo(uri document.URI) (info openDoc, open, ok bool) {
	got := make(chan openDoc, 1)
	c.s.post(func() {
		if d := c.findDoc(uri); d != nil {
			got <- openDoc{doc: d, version: d.Version(), text: d.Snapshot()}
			return
		}
		got <- openDoc{}
	})
	select {
	case info = <-got:
		return info, info.doc != nil, true
	case <-c.ctx.Done():
		return openDoc{}, false, false
	}
}

// findDoc is the open document with the URI (UI thread).
func (c *serverCtl) findDoc(uri document.URI) *document.Document {
	if d, ok := c.wanted[uri]; ok {
		return d
	}
	for _, d := range c.ws.Documents() {
		if d.URI() == uri {
			return d
		}
	}
	return nil
}

// applyPublication converts a publication to markers and sets them. A publication for an older version of a document than the
// one open now is dropped: its positions are not those of the text, and the server republishes for the newer version.
func (c *serverCtl) applyPublication(q queuedPublish) {
	p := q.params
	uri, err := lsp.ParseDocumentURI(p.URI)
	if err != nil || uri.Scheme != document.SchemeFile {
		c.log.Debug("diagnostics for a URI that is not a file are ignored", "uri", p.URI)
		return
	}
	info, open, ok := c.openInfo(uri)
	if !ok {
		return
	}
	var text lsp.Lines
	if open {
		if p.Version != nil && *p.Version < info.version {
			c.log.Debug("stale diagnostics are dropped", "uri", p.URI, "version", *p.Version, "document", info.version)
			return
		}
		text = info.text
	} else {
		text = c.readClosed(uri)
	}
	markers := c.markersFrom(uri, text, p.Diagnostics)
	c.setMarkers(q.gen, uri, markers)
}

// readClosed reads the text of a file that is not open, for the conversion of columns; nil when it cannot be read.
func (c *serverCtl) readClosed(uri document.URI) lsp.Lines {
	ctx, cancel := context.WithTimeout(c.ctx, 10*time.Second)
	defer cancel()
	l, err := c.ws.ReadText(ctx, uri)
	if err != nil {
		c.log.Debug("the text of a file with diagnostics cannot be read", "uri", uri.String(), "err", err)
		return nil
	}
	return l
}

// setMarkers sets the markers of the server for a file unless the client of generation gen is gone.
func (c *serverCtl) setMarkers(gen int, uri document.URI, markers []problems.Marker) {
	c.mu.Lock()
	stale := c.stopped || gen != c.gen || c.client == nil
	c.mu.Unlock()
	if stale {
		return
	}
	c.s.opts.Markers.Set(c.markerOwner(), uri.Path, markers)
}

// clearMarkers takes every marker of the server away (the server stopped, restarted or crashed, as vscode-languageclient
// clears its diagnostics collection) and forgets what was pulled.
func (c *serverCtl) clearMarkers() {
	c.diag.mu.Lock()
	c.diag.queue = map[string]queuedPublish{}
	c.diag.order = nil
	c.diag.mu.Unlock()
	for uri, t := range c.diag.timers {
		t.Stop()
		delete(c.diag.timers, uri)
	}
	c.diag.pulled = map[document.URI]bool{}
	c.diag.results = map[document.URI]string{}
	if c.s.opts.Markers != nil {
		c.s.opts.Markers.Remove(c.markerOwner())
	}
}

// markersFrom converts the diagnostics of a server for a file to Problems markers. Lines count from 1 and columns are UTF-16
// units from 1, whatever position encoding the server uses; text is the file as the positions refer to it (nil: the columns
// are taken as they came).
func (c *serverCtl) markersFrom(uri document.URI, text lsp.Lines, ds []lsp.Diagnostic) []problems.Marker {
	enc := c.encoding()
	// the conversion of langfeatures gives the severity, the tags and the code; the range is worked out here, in UTF-16 units
	converted := langfeatures.DiagnosticsFromLSP(enc, langfeatures.Document{URI: uri, Text: buffer.New("").Snapshot()}, ds)
	out := make([]problems.Marker, 0, len(converted))
	for i, d := range converted {
		r := ds[i].Range
		m := problems.Marker{
			Severity: markerSeverityOf(d.Severity), Code: d.Code, CodeHref: d.CodeHref, Source: d.Source, Message: d.Message,
			Unnecessary: d.Unnecessary, Deprecated: d.Deprecated, Data: d.Raw,
			StartLine: r.Start.Line + 1, StartColumn: utf16Column(enc, text, r.Start) + 1,
			EndLine: r.End.Line + 1, EndColumn: utf16Column(enc, text, r.End) + 1,
		}
		for _, rel := range ds[i].RelatedInformation {
			ru, err := lsp.ParseDocumentURI(rel.Location.URI)
			if err != nil || ru.Scheme != document.SchemeFile {
				continue
			}
			// the text of the other file is not at hand: its columns are taken as they came
			m.Related = append(m.Related, problems.RelatedInformation{
				Resource: ru.Path, Message: rel.Message,
				StartLine: rel.Location.Range.Start.Line + 1, StartColumn: rel.Location.Range.Start.Character + 1,
				EndLine: rel.Location.Range.End.Line + 1, EndColumn: rel.Location.Range.End.Character + 1,
			})
		}
		out = append(out, m)
	}
	return out
}

// markerSeverityOf maps the severity of a language feature to the panel's.
func markerSeverityOf(s langfeatures.Severity) problems.Severity {
	switch s {
	case langfeatures.SeverityWarning:
		return problems.SeverityWarning
	case langfeatures.SeverityInfo:
		return problems.SeverityInfo
	case langfeatures.SeverityHint:
		return problems.SeverityHint
	}
	return problems.SeverityError
}

// ---- pull diagnostics ----

// pullOptions tells whether the server pulls (it declared diagnosticProvider or registered textDocument/diagnostic) and the
// identifier it asked for.
func (c *serverCtl) pullOptions(client *lsp.Client) (identifier string, ok bool) {
	if o := client.Capabilities().DiagnosticProvider; o != nil {
		return o.Identifier, true
	}
	if regs := client.Registered(lsp.MethodDocumentDiagnostic); len(regs) > 0 {
		return "", true
	}
	return "", false
}

// schedulePull asks for the diagnostics of a document after a quiet time (UI thread). Each call restarts the wait.
func (c *serverCtl) schedulePull(doc *document.Document) {
	client := c.currentClient()
	if client == nil {
		return
	}
	if _, ok := c.pullOptions(client); !ok {
		return
	}
	uri := doc.URI()
	if t := c.diag.timers[uri]; t != nil {
		t.Stop()
	}
	c.diag.timers[uri] = time.AfterFunc(pullDelay, func() {
		c.s.post(func() {
			delete(c.diag.timers, uri)
			if d := c.findDoc(uri); d != nil {
				c.pull(d)
			}
		})
	})
}

// pullAll asks for the diagnostics of every document the server has open (UI thread): the server asked to refresh them or
// registered the pull.
func (c *serverCtl) pullAll() {
	for _, sd := range c.synced {
		c.pull(sd.Document())
	}
}

// pull sends a textDocument/diagnostic request for a document and applies the report if the document is still at the version
// it was asked at (UI thread).
func (c *serverCtl) pull(doc *document.Document) {
	client := c.currentClient()
	if client == nil || c.isStopped() {
		return
	}
	identifier, ok := c.pullOptions(client)
	if !ok {
		return
	}
	uri, version, snapshot := doc.URI(), doc.Version(), doc.Snapshot()
	c.mu.Lock()
	gen := c.gen
	c.mu.Unlock()
	params := lsp.DocumentDiagnosticParams{
		TextDocument: lsp.TextDocumentIdentifier{URI: lsp.DocumentURI(uri)}, Identifier: identifier, PreviousResultID: c.diag.results[uri],
	}
	c.s.h.servers.Add(1)
	go func() {
		defer c.s.h.servers.Done()
		ctx, cancel := context.WithTimeout(c.ctx, 60*time.Second)
		defer cancel()
		report, err := lsp.Request[lsp.DocumentDiagnosticReport](ctx, client, lsp.MethodDocumentDiagnostic, params)
		if err != nil {
			if code, isServer := lsp.ErrorCode(err); isServer && (code == lsp.CodeServerCancelled || code == lsp.CodeContentModified) {
				return // the server will be asked again after the next change
			}
			if c.ctx.Err() == nil {
				c.log.Debug("the diagnostics of a document were not pulled", "uri", uri.String(), "err", err)
			}
			return
		}
		c.s.post(func() { c.pulledReport(gen, doc, version, snapshot, report) })
	}()
}

// pulledReport applies the report of a pull (UI thread). A report for an older version of the document than the one open now
// is dropped and the document is pulled again after the quiet time.
func (c *serverCtl) pulledReport(gen int, doc *document.Document, version int, snapshot lsp.Lines, report lsp.DocumentDiagnosticReport) {
	c.mu.Lock()
	stale := c.stopped || gen != c.gen
	c.mu.Unlock()
	if stale {
		return
	}
	uri := doc.URI()
	if c.findDoc(uri) != doc {
		return // closed meanwhile
	}
	if doc.Version() != version {
		c.schedulePull(doc)
		return
	}
	c.applyReport(gen, uri, snapshot, report)
	c.diag.pulled[uri] = true
	// related documents are reported with the document: their markers are set from the file on disk (or the open document)
	if len(report.RelatedDocuments) == 0 {
		return
	}
	related := report.RelatedDocuments
	c.s.h.servers.Add(1)
	go func() {
		defer c.s.h.servers.Done()
		for ru, rep := range related {
			u, err := lsp.ParseDocumentURI(ru)
			if err != nil || u.Scheme != document.SchemeFile || rep.Kind != "full" {
				continue
			}
			info, open, ok := c.openInfo(u)
			if !ok {
				return
			}
			var text lsp.Lines
			if open {
				text = info.text
			} else {
				text = c.readClosed(u)
			}
			c.setMarkers(gen, u, c.markersFrom(u, text, rep.Items))
		}
	}()
}

// applyReport sets the markers of a full report and remembers its result id; an unchanged report keeps what is there.
func (c *serverCtl) applyReport(gen int, uri document.URI, text lsp.Lines, report lsp.DocumentDiagnosticReport) {
	if report.Kind != "full" {
		return
	}
	if report.ResultID != "" {
		c.diag.results[uri] = report.ResultID
	} else {
		delete(c.diag.results, uri)
	}
	c.setMarkers(gen, uri, c.markersFrom(uri, text, report.Items))
}

// refreshDiagnostics answers workspace/diagnostic/refresh: every open document is pulled again.
func (c *serverCtl) refreshDiagnostics() {
	c.s.post(c.pullAll)
}

// forgetDocument is the end of the diagnostics of a document that closed (UI thread): the markers that came from a pull go
// (the server does not report for a closed document), those that were pushed stay until the server publishes again, as in
// VS Code.
func (c *serverCtl) forgetDocument(uri document.URI) {
	if t := c.diag.timers[uri]; t != nil {
		t.Stop()
		delete(c.diag.timers, uri)
	}
	delete(c.diag.results, uri)
	if c.diag.pulled[uri] {
		delete(c.diag.pulled, uri)
		if c.s.opts.Markers != nil {
			c.s.opts.Markers.Remove(c.markerOwner(), uri.Path)
		}
	}
}

// utf16Column is the column, in UTF-16 units, of a position of a server that counts in its encoding enc. Without the text of the
// file, or for UTF-16 itself, the column stays as it came.
func utf16Column(enc lsp.Encoding, text lsp.Lines, p lsp.Position) int {
	if enc == lsp.UTF16 || text == nil {
		return p.Character
	}
	line := min(max(p.Line, 0), max(text.LineCount()-1, 0))
	content, err := text.LineContent(line)
	if err != nil || p.Line > line {
		return p.Character
	}
	return lsp.UTF16.Column(content, enc.ByteColumn(content, p.Character))
}
