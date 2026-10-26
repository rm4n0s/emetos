package lsptest

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/rm4n0s/emetos/internal/core/lsp"
	"github.com/rm4n0s/errors"
)

// The methods the fake server understands besides the protocol's.
const (
	// MethodDocument asks for the server's copy of a document: params {"uri"}, result Doc.
	MethodDocument = "$/lsptest/document"
	// MethodTraffic asks for the recorded messages: result []Message.
	MethodTraffic = "$/lsptest/traffic"
	// MethodViolations asks for the protocol violations the server noticed: result []string.
	MethodViolations = "$/lsptest/violations"
	// MethodCrash makes the server die at once, without answering.
	MethodCrash = "$/lsptest/crash"
	// MethodStall makes the server stop answering requests.
	MethodStall = "$/lsptest/stall"
)

// Message is a message the server received or sent.
type Message struct {
	// FromClient is true for a message the server received.
	FromClient bool `json:"fromClient"`
	// Kind is "request", "response" or "notification".
	Kind   string `json:"kind"`
	Method string `json:"method,omitempty"`
	ID     string `json:"id,omitempty"`
	// Body is the whole JSON message.
	Body json.RawMessage `json:"body"`
}

// Doc is the server's copy of a document.
type Doc struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
	// Open is false for a document the server was never told about, or that was closed.
	Open bool `json:"open"`
}

// Server is the fake language server: see the package doc and Script. It is safe for concurrent use.
type Server struct {
	script Script
	enc    string // the encoding the server counts characters in, from the capabilities

	mu         sync.Mutex
	traffic    []Message
	docs       map[string]*Doc
	violations []string
	counts     map[string]int
	stalled    bool
	shutdown   bool
	nextID     int
	waiting    map[string]chan json.RawMessage
	crash      func()
	rw         io.ReadWriter
	writeErr   error

	wmu sync.Mutex
}

// NewServer makes a server that behaves as script says. crash is called when the server must die (a
// request of Script.CrashOn, or MethodCrash): the fake server program exits, and the in-process harness
// closes the pipes; nil does nothing.
func NewServer(script Script, crash func()) *Server {
	s := &Server{
		script: script, enc: lsp.EncodingNameUTF16, crash: crash,
		docs: map[string]*Doc{}, counts: map[string]int{}, waiting: map[string]chan json.RawMessage{},
	}
	var caps struct {
		PositionEncoding string `json:"positionEncoding"`
	}
	if json.Unmarshal(script.Capabilities, &caps) == nil && caps.PositionEncoding != "" {
		s.enc = caps.PositionEncoding
	}
	return s
}

// Shutdown reports whether the client sent shutdown.
func (s *Server) Shutdown() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.shutdown
}

// Traffic is a copy of the recorded messages, in order.
func (s *Server) Traffic() []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Message(nil), s.traffic...)
}

// Document is the server's copy of a document.
func (s *Server) Document(uri string) Doc {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.docs[uri]; ok {
		return *d
	}
	return Doc{URI: uri}
}

// Violations are the protocol violations the server noticed in what the client sent: versions that did not
// go up, changes to a document that is not open, positions that are not in the text, and the like.
func (s *Server) Violations() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.violations...)
}

// Count is how many requests and notifications of a method the server received.
func (s *Server) Count(method string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counts[method]
}

// Stall makes the server stop answering requests from now on.
func (s *Server) Stall() {
	s.mu.Lock()
	s.stalled = true
	s.mu.Unlock()
}

func (s *Server) violate(format string, args ...any) {
	s.mu.Lock()
	s.violations = append(s.violations, fmt.Sprintf(format, args...))
	s.mu.Unlock()
}

// incoming is a message of the client as the server reads it.
type incoming struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

// Serve reads messages from rw and answers them until the client sends exit or closes the stream. It
// returns nil then, and the framing error or the write error otherwise. rw is not closed.
func (s *Server) Serve(rw io.ReadWriter) error {
	s.mu.Lock()
	s.rw = rw
	s.mu.Unlock()
	br := bufio.NewReaderSize(rw, 64<<10)
	for {
		body, err := lsp.ReadFrame(br, 0)
		if errors.Is(err, io.EOF) {
			return s.writeError()
		}
		if err != nil {
			return err
		}
		var m incoming
		if err := json.Unmarshal(body, &m); err != nil {
			s.violate("the client sent a message that is not JSON: %v", err)
			continue
		}
		switch {
		case m.Method != "" && len(m.ID) > 0:
			n := s.record(true, "request", m.Method, string(m.ID), body)
			go s.request(m, n-1)
		case m.Method != "":
			s.record(true, "notification", m.Method, "", body)
			if m.Method == lsp.MethodExit {
				return s.writeError()
			}
			s.notification(m)
		case len(m.ID) > 0:
			s.record(true, "response", "", string(m.ID), body)
			s.mu.Lock()
			ch := s.waiting[string(m.ID)]
			delete(s.waiting, string(m.ID))
			s.mu.Unlock()
			if ch != nil {
				ch <- body
			}
		}
	}
}

func (s *Server) writeError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeErr
}

func (s *Server) record(fromClient bool, kind, method, id string, body []byte) (count int) {
	s.mu.Lock()
	s.traffic = append(s.traffic, Message{FromClient: fromClient, Kind: kind, Method: method, ID: id, Body: append(json.RawMessage(nil), body...)})
	if fromClient && kind != "response" {
		s.counts[method]++
		count = s.counts[method]
	}
	s.mu.Unlock()
	return count
}

// write sends a message; the first failure is kept and returned by Serve.
func (s *Server) write(kind, method, id string, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		s.violate("the server cannot encode %s: %v", method, err)
		return
	}
	s.wmu.Lock()
	defer s.wmu.Unlock()
	s.mu.Lock()
	rw := s.rw
	s.mu.Unlock()
	if err := lsp.WriteFrame(rw, body); err != nil {
		s.mu.Lock()
		if s.writeErr == nil {
			s.writeErr = err
		}
		s.mu.Unlock()
		return
	}
	s.record(false, kind, method, id, body)
}

type responseMessage struct {
	JSONRPC string             `json:"jsonrpc"`
	ID      json.RawMessage    `json:"id"`
	Result  json.RawMessage    `json:"result"`
	Error   *lsp.ResponseError `json:"error,omitempty"`
}

func (s *Server) respond(m incoming, result json.RawMessage, rerr *lsp.ResponseError) {
	if rerr == nil && result == nil {
		result = json.RawMessage("null")
	}
	if rerr != nil {
		result = nil
	}
	s.write("response", m.Method, string(m.ID), responseMessage{JSONRPC: "2.0", ID: m.ID, Result: result, Error: rerr})
}

// Send sends a notification to the client.
func (s *Server) Send(method string, params json.RawMessage) {
	s.write("notification", method, "", struct {
		JSONRPC string          `json:"jsonrpc"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params,omitempty"`
	}{"2.0", method, params})
}

// SendRequest sends a request to the client; the channel gets the whole response message when it arrives.
func (s *Server) SendRequest(method string, params json.RawMessage) <-chan json.RawMessage {
	s.mu.Lock()
	s.nextID++
	id := "\"s" + strconv.Itoa(s.nextID) + "\""
	ch := make(chan json.RawMessage, 1)
	s.waiting[id] = ch
	s.mu.Unlock()
	s.write("request", method, id, struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params,omitempty"`
	}{"2.0", json.RawMessage(id), method, params})
	return ch
}

// triggered sends what the script says to send after a message of the client.
func (s *Server) triggered(method string) {
	for _, o := range s.script.Send {
		if o.After != method {
			continue
		}
		if o.Request {
			s.SendRequest(o.Method, o.Params)
		} else {
			s.Send(o.Method, o.Params)
		}
	}
}

// die ends the server as the crash hook says.
func (s *Server) die() {
	if s.crash != nil {
		s.crash()
	}
}

// request answers a request of the client; n counts the earlier requests of its method.
func (s *Server) request(m incoming, n int) {
	s.mu.Lock()
	s.stalled = s.stalled || (s.script.StallOn != "" && m.Method == s.script.StallOn)
	stalled := s.stalled
	s.mu.Unlock()
	if (m.Method == s.script.CrashOn && m.Method != "") || m.Method == MethodCrash {
		s.die()
		return
	}
	if m.Method == MethodStall {
		s.Stall()
		s.respond(m, nil, nil)
		return
	}
	if stalled && !strings.HasPrefix(m.Method, "$/lsptest/") {
		return
	}
	if _, ok := s.script.Responses[lsp.MethodInitialize]; ok && m.Method == lsp.MethodInitialize {
		s.scripted(m, n) // the script answers initialize itself, with an error for instance
		s.triggered(m.Method)
		return
	}
	switch m.Method {
	case lsp.MethodInitialize:
		caps := s.script.Capabilities
		if caps == nil {
			caps = json.RawMessage("{}")
		}
		res, err := json.Marshal(struct {
			Capabilities json.RawMessage `json:"capabilities"`
			ServerInfo   *lsp.ServerInfo `json:"serverInfo,omitempty"`
		}{caps, s.script.ServerInfo})
		if err != nil {
			s.respond(m, nil, &lsp.ResponseError{Code: lsp.CodeInternalError, Message: err.Error()})
			return
		}
		s.respond(m, res, nil)
	case lsp.MethodShutdown:
		s.mu.Lock()
		s.shutdown = true
		s.mu.Unlock()
		s.respond(m, nil, nil)
	case MethodDocument:
		var p struct {
			URI string `json:"uri"`
		}
		if err := json.Unmarshal(m.Params, &p); err != nil {
			s.respond(m, nil, &lsp.ResponseError{Code: lsp.CodeInvalidParams, Message: err.Error()})
			return
		}
		s.respondValue(m, s.Document(p.URI))
	case MethodTraffic:
		s.respondValue(m, s.Traffic())
	case MethodViolations:
		s.respondValue(m, s.Violations())
	default:
		s.scripted(m, n)
	}
	s.triggered(m.Method)
}

// respondValue answers with a value encoded as JSON.
func (s *Server) respondValue(m incoming, v any) {
	res, err := json.Marshal(v)
	if err != nil {
		s.respond(m, nil, &lsp.ResponseError{Code: lsp.CodeInternalError, Message: err.Error()})
		return
	}
	s.respond(m, res, nil)
}

// scripted answers a request from Script.Responses; n counts the earlier requests of the method.
func (s *Server) scripted(m incoming, n int) {
	r, ok := s.script.Responses[m.Method]
	if !ok {
		s.respond(m, nil, &lsp.ResponseError{Code: lsp.CodeMethodNotFound, Message: "the fake server has no answer for " + m.Method})
		return
	}
	if len(r.Sequence) > 0 {
		r = r.Sequence[min(n, len(r.Sequence)-1)]
	}
	if r.DelayMS > 0 {
		time.Sleep(time.Duration(r.DelayMS) * time.Millisecond)
	}
	s.respond(m, r.Result, r.Error)
}

// notification handles a notification of the client.
func (s *Server) notification(m incoming) {
	switch m.Method {
	case lsp.MethodDidOpen:
		var p lsp.DidOpenTextDocumentParams
		if err := json.Unmarshal(m.Params, &p); err != nil {
			s.violate("didOpen cannot be read: %v", err)
			break
		}
		s.didOpen(p.TextDocument)
	case lsp.MethodDidChange:
		var p lsp.DidChangeTextDocumentParams
		if err := json.Unmarshal(m.Params, &p); err != nil {
			s.violate("didChange cannot be read: %v", err)
			break
		}
		s.didChange(p)
	case lsp.MethodDidClose:
		var p lsp.DidCloseTextDocumentParams
		if err := json.Unmarshal(m.Params, &p); err != nil {
			s.violate("didClose cannot be read: %v", err)
			break
		}
		s.mu.Lock()
		if d, ok := s.docs[p.TextDocument.URI]; !ok || !d.Open {
			s.violations = append(s.violations, "didClose of a document that is not open: "+p.TextDocument.URI)
		} else {
			d.Open = false
		}
		s.mu.Unlock()
	case MethodCrash:
		s.die()
		return
	case MethodStall:
		s.Stall()
	}
	s.triggered(m.Method)
}

func (s *Server) didOpen(item lsp.TextDocumentItem) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.docs[item.URI]; ok && d.Open {
		s.violations = append(s.violations, "didOpen of a document that is open: "+item.URI)
	}
	s.docs[item.URI] = &Doc{URI: item.URI, LanguageID: item.LanguageID, Version: item.Version, Text: item.Text, Open: true}
}

func (s *Server) didChange(p lsp.DidChangeTextDocumentParams) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.docs[p.TextDocument.URI]
	if !ok || !d.Open {
		s.violations = append(s.violations, "didChange of a document that is not open: "+p.TextDocument.URI)
		return
	}
	if p.TextDocument.Version <= d.Version {
		s.violations = append(s.violations, fmt.Sprintf("didChange version %d does not go up from %d: %s", p.TextDocument.Version, d.Version, d.URI))
	}
	d.Version = p.TextDocument.Version
	for i, ch := range p.ContentChanges {
		if ch.Range == nil {
			d.Text = ch.Text
			continue
		}
		start, ok1 := s.offset(d.Text, ch.Range.Start)
		end, ok2 := s.offset(d.Text, ch.Range.End)
		if !ok1 || !ok2 || end < start {
			s.violations = append(s.violations, fmt.Sprintf("didChange change %d of %s has a range that is not in the text: %+v", i, d.URI, *ch.Range))
			return
		}
		d.Text = d.Text[:start] + ch.Text + d.Text[end:]
	}
}

// offset is the byte offset of a position in text, counting characters the way a server of the encoding
// does; ok is false when the position is not in the text (a line past the end, a character past the end of
// its line or in the middle of a character).
func (s *Server) offset(text string, p lsp.Position) (int, bool) {
	if p.Line < 0 || p.Character < 0 {
		return 0, false
	}
	pos := 0
	for line := 0; line < p.Line; line++ {
		i := strings.IndexAny(text[pos:], "\r\n")
		if i < 0 {
			return 0, false
		}
		pos += i
		if text[pos] == '\r' && pos+1 < len(text) && text[pos+1] == '\n' {
			pos++
		}
		pos++
	}
	units := 0
	for units < p.Character {
		if pos >= len(text) || text[pos] == '\r' || text[pos] == '\n' {
			return 0, false
		}
		r, size := utf8.DecodeRuneInString(text[pos:])
		switch s.enc {
		case lsp.EncodingNameUTF8:
			units += size
		case lsp.EncodingNameUTF32:
			units++
		default:
			units++
			if r >= 0x10000 {
				units++
			}
		}
		pos += size
	}
	return pos, units == p.Character
}
