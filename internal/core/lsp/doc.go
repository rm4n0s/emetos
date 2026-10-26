// Package lsp is the Language Server Protocol client of the editor (SPEC §9.12, D5, D20): the
// Content-Length framing, JSON-RPC 2.0 requests, responses and notifications with cancellation, the
// server-to-client requests, the initialize handshake with Emetos's client capabilities, the conversion
// of positions between Emetos's byte columns (D6) and the negotiated position encoding, and the
// synchronization of documents with the server.
//
// The client has no user interface and imports no Glutter: every method may be called from any goroutine
// and the handlers it calls (Handlers) run off the goroutine that reads the server, so the host hands
// their work over to the UI thread itself. The transport is any io.ReadWriteCloser; starting a server
// process is the job of the host (step 12.3).
//
// The wire types of area 3 are a hand-written subset of the protocol, in types*.go.
package lsp
