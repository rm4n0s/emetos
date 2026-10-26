package lsp

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/fileio"
	"github.com/rm4n0s/errors"
)

// Document synchronization (LSP "Text Document Synchronization", SPEC §9.12).
//
// The server's copy of a document is the document's text with the document's line ending: the buffer holds
// "\n" only (D6), the file on disk may have "\r\n", and the server must see the text the file has. The
// version sent is the document's Version, which goes up with every change, undo and redo included.
//
// A change event of the document becomes one didChange. For a server that wants incremental changes the
// edits of a batch are sent last to first: the ranges of a batch are in the text before it, and an edit at a
// later position does not move an earlier one, so the server applying them in order gets each range right.
// The ranges are converted with the line texts of the text before the batch, which is right because the
// column of a position only depends on the characters in front of it on its line, and the edits that
// come first in the order of the wire are all behind it. A server that wants the full text, a replacement
// of the whole text (CauseSetText) and a change of the line ending (which changes every line end) get the
// full text.

// SyncedDocument is a document the client keeps in sync with the server, from OpenDocument to Close. Its
// methods, like the document's, belong to the goroutine that owns the document.
type SyncedDocument struct {
	c      *Client
	doc    *document.Document
	uri    string
	eol    fileio.LineEnding
	closed bool
	unsub  []func()
}

// syncOptions are the static options of the server with what it registered dynamically.
func (c *Client) syncOptions() TextDocumentSyncOptions {
	var o TextDocumentSyncOptions
	if c.caps.TextDocumentSync != nil {
		o = *c.caps.TextDocumentSync
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, r := range c.registrations {
		switch r.Method {
		case MethodDidOpen, MethodDidClose:
			o.OpenClose = true
		case MethodDidChange:
			var opts struct {
				SyncKind int `json:"syncKind"`
			}
			if json.Unmarshal(r.RegisterOptions, &opts) == nil {
				o.Change = max(o.Change, opts.SyncKind)
			}
		case MethodWillSave:
			o.WillSave = true
		case MethodWillSaveWU:
			o.WillSaveWaitUntil = true
		case MethodDidSave:
			var opts struct {
				IncludeText bool `json:"includeText"`
			}
			o.Save = true
			if json.Unmarshal(r.RegisterOptions, &opts) == nil && opts.IncludeText {
				o.SaveIncludeText = true
			}
		}
	}
	return o
}

// serverText is text with "\n" line ends as the server sees it under the line ending of the document.
func serverText(text string, eol fileio.LineEnding) string {
	if eol == fileio.CRLF {
		return strings.ReplaceAll(text, "\n", "\r\n")
	}
	return text
}

// OpenDocument starts keeping d in sync: it sends didOpen with the text now (when the server wants it)
// and follows the changes, saves and renames of the document until Close. It must be called on the
// goroutine that owns d. It fails with the tag DocumentAlreadyOpen.
func (c *Client) OpenDocument(d *document.Document) (*SyncedDocument, error) {
	uri := DocumentURI(d.URI())
	st := &SyncedDocument{c: c, doc: d, uri: uri, eol: d.LineEnding()}
	c.mu.Lock()
	if _, ok := c.docs[uri]; ok {
		c.mu.Unlock()
		return nil, errors.New("DocumentAlreadyOpen", "the document is open on the server already", "uri", uri)
	}
	c.docs[uri] = st
	c.mu.Unlock()

	if c.syncOptions().OpenClose {
		st.sendOpen()
	}
	st.unsub = []func(){d.Subscribe(st.onChange), d.OnSaved(st.onSaved), d.OnURIChange(st.onURIChange)}
	return st, nil
}

// URI is the URI the server knows the document by.
func (s *SyncedDocument) URI() string { return s.uri }

// Document is the document that is kept in sync.
func (s *SyncedDocument) Document() *document.Document { return s.doc }

// notify sends a notification; the document is the caller's, so a failure (the connection ended) can only
// be logged: the host learns of it from Client.Done and opens its documents again on a new client.
func (s *SyncedDocument) notify(method string, params any) {
	if err := s.c.conn.Notify(method, params); err != nil {
		s.c.log.Debug("a document notification was not sent", "err", err)
	}
}

func (s *SyncedDocument) sendOpen() {
	s.notify(MethodDidOpen, DidOpenTextDocumentParams{TextDocument: TextDocumentItem{
		URI: s.uri, LanguageID: s.doc.LanguageID(), Version: s.doc.Version(), Text: serverText(s.doc.Text(), s.eol),
	}})
}

// Close sends didClose and stops following the document. Closing twice is harmless.
func (s *SyncedDocument) Close() {
	if s.closed {
		return
	}
	s.closed = true
	for _, u := range s.unsub {
		u()
	}
	s.c.mu.Lock()
	delete(s.c.docs, s.uri)
	s.c.mu.Unlock()
	if s.c.syncOptions().OpenClose {
		s.notify(MethodDidClose, DidCloseTextDocumentParams{TextDocument: TextDocumentIdentifier{URI: s.uri}})
	}
}

// onChange sends a change of the document.
func (s *SyncedDocument) onChange(ev document.ChangeEvent) {
	opts := s.c.syncOptions()
	eol := s.doc.LineEnding()
	eolChanged := eol != s.eol
	s.eol = eol
	if s.closed || !opts.OpenClose || opts.Change == SyncNone {
		return
	}
	var changes []TextDocumentContentChangeEvent
	switch {
	case ev.Cause == document.CauseLineEnding && !eolChanged:
		return // nothing the server sees changed
	case opts.Change == SyncFull || ev.Cause == document.CauseSetText || eolChanged || len(ev.Changes) == 0:
		changes = []TextDocumentContentChangeEvent{{Text: serverText(ev.After.String(), eol)}}
	default:
		changes = s.incremental(ev, eol)
	}
	s.notify(MethodDidChange, DidChangeTextDocumentParams{
		TextDocument:   VersionedTextDocumentIdentifier{URI: s.uri, Version: ev.Version},
		ContentChanges: changes,
	})
}

// incremental turns the changes of an event into content changes, last first.
func (s *SyncedDocument) incremental(ev document.ChangeEvent, eol fileio.LineEnding) []TextDocumentContentChangeEvent {
	enc := s.c.encoding
	out := make([]TextDocumentContentChangeEvent, 0, len(ev.Changes))
	for i := len(ev.Changes) - 1; i >= 0; i-- {
		ch := ev.Changes[i]
		r := enc.RangeToLSP(ev.Before, ch.OldRange)
		out = append(out, TextDocumentContentChangeEvent{Range: &r, Text: serverText(ch.NewText, eol)})
	}
	return out
}

// onSaved sends didSave.
func (s *SyncedDocument) onSaved(document.SavedEvent) {
	opts := s.c.syncOptions()
	if s.closed || !opts.OpenClose || !opts.Save {
		return
	}
	p := DidSaveTextDocumentParams{TextDocument: TextDocumentIdentifier{URI: s.uri}}
	if opts.SaveIncludeText {
		// The text of the document now: if it was edited while the file was written, the server has had
		// those changes already, and its copy must stay the document's.
		text := serverText(s.doc.Text(), s.eol)
		p.Text = &text
	}
	s.notify(MethodDidSave, p)
}

// onURIChange follows a document that was renamed or saved under another name: the server closes the old
// URI and opens the new one with the text now.
func (s *SyncedDocument) onURIChange(_, newURI document.URI) {
	if s.closed {
		return
	}
	opts := s.c.syncOptions()
	if opts.OpenClose {
		s.notify(MethodDidClose, DidCloseTextDocumentParams{TextDocument: TextDocumentIdentifier{URI: s.uri}})
	}
	s.c.mu.Lock()
	delete(s.c.docs, s.uri)
	s.uri = DocumentURI(newURI)
	s.c.docs[s.uri] = s
	s.c.mu.Unlock()
	if opts.OpenClose {
		s.sendOpen()
	}
}

// WillSave tells the server that the document is about to be saved (textDocument/willSave), when it asked
// for it. reason is one of SaveReasonManual, SaveReasonAfterDelay and SaveReasonFocusOut.
func (s *SyncedDocument) WillSave(reason int) {
	if s.closed || !s.c.syncOptions().WillSave {
		return
	}
	s.notify(MethodWillSave, WillSaveTextDocumentParams{TextDocument: TextDocumentIdentifier{URI: s.uri}, Reason: reason})
}

// WillSaveWaitUntil asks the server for the edits to make before the document is saved
// (textDocument/willSaveWaitUntil); nil when the server does not offer it. The edits are in the encoding of
// the server and for the version of the document now: apply them with Client.Encoding and the snapshot of
// the document, and only if the version has not changed.
func (s *SyncedDocument) WillSaveWaitUntil(ctx context.Context, reason int) ([]TextEdit, error) {
	if s.closed || !s.c.syncOptions().WillSaveWaitUntil {
		return nil, nil
	}
	return Request[[]TextEdit](ctx, s.c, MethodWillSaveWU, WillSaveTextDocumentParams{
		TextDocument: TextDocumentIdentifier{URI: s.uri}, Reason: reason,
	})
}

// ServerText is the text the server must have for a document: the document's text with its line ending. It
// is what the tests compare the server's copy with.
func ServerText(d *document.Document) string { return serverText(d.Text(), d.LineEnding()) }

// ensure the interface the conversions need is satisfied by the snapshot type.
var _ Lines = buffer.Snapshot{}
