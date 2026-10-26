// Package lsptest is the test double of a language server (SPEC §9.12 "Tests"): a fake server that answers
// from a script written as JSON, records the traffic, keeps its own copy of every document from the
// notifications it gets, and crashes or stalls on command; and a harness that connects an lsp.Client to it,
// in the same process over pipes or to the fake server program (fakeserver) started as a real process in its
// own process group.
//
// The server reads and writes the base protocol with lsp.ReadFrame and lsp.WriteFrame, but applies the
// changes of documents with code of its own, counting characters as a server of each encoding does, so a
// wrong position conversion of the client shows as a different text.
package lsptest
