package lsptest

import (
	"encoding/json"
	"os"

	"github.com/rm4n0s/emetos/internal/core/lsp"
	"github.com/rm4n0s/errors"
)

// Script is what the fake server does, as JSON (the fake server program reads it from a file).
type Script struct {
	// Capabilities is the "capabilities" member of the initialize result, as JSON: it decides the
	// synchronization kind and the position encoding ("positionEncoding"). Absent means {}.
	Capabilities json.RawMessage `json:"capabilities,omitempty"`
	// ServerInfo is the "serverInfo" member of the initialize result.
	ServerInfo *lsp.ServerInfo `json:"serverInfo,omitempty"`
	// Responses answers requests by method. A request without an entry gets MethodNotFound.
	Responses map[string]Response `json:"responses,omitempty"`
	// Send lists messages the server sends after it received a message.
	Send []Outgoing `json:"send,omitempty"`
	// CrashOn is a method: on a request of it the server dies without answering.
	CrashOn string `json:"crashOn,omitempty"`
	// StallOn is a method: a request of it, and every request after it, is never answered.
	StallOn string `json:"stallOn,omitempty"`
}

// Response is the answer to a request.
type Response struct {
	// Result is the "result", as JSON; absent or null answers null.
	Result json.RawMessage `json:"result,omitempty"`
	// Error, when set, is the answer instead of the result.
	Error *lsp.ResponseError `json:"error,omitempty"`
	// DelayMS waits before answering.
	DelayMS int `json:"delayMs,omitempty"`
	// Sequence, when set, replaces the members above: the nth request of the method gets the nth
	// response, and the last one is repeated.
	Sequence []Response `json:"sequence,omitempty"`
}

// Outgoing is a message the server sends of its own.
type Outgoing struct {
	// After is the method of the message from the client that triggers it ("initialize" triggers it after
	// the answer was written, "initialized", "textDocument/didOpen", ...).
	After string `json:"after"`
	// Method and Params are the message.
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
	// Request makes it a request of the server to the client; its response is recorded in the traffic.
	Request bool `json:"request,omitempty"`
}

// LoadScript reads a script from a JSON file. It fails with the tags ScriptReadFailed and ScriptInvalid.
func LoadScript(path string) (Script, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Script{}, errors.NewErr("ScriptReadFailed", err).SetMetadata("path", path)
	}
	var s Script
	if err := json.Unmarshal(data, &s); err != nil {
		return Script{}, errors.NewErr("ScriptInvalid", err).SetMetadata("path", path)
	}
	return s, nil
}
