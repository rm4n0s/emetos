// Package dap is the Debug Adapter Protocol client of the editor (SPEC §10.31, D5, D39, D43): the
// Content-Length framing of the wire, the requests, responses and events of DAP 1.65 and later that Run and
// Debug uses as Go types, a Client that numbers and matches requests, cancels them when their context ends,
// answers the requests of the adapter (runInTerminal, startDebugging) through a handler and hands the events
// to one consumer through a bounded channel, and the transports that reach an adapter: a program on its
// standard input and output, a TCP port, a Unix socket, and a program that prints the port it listens on.
//
// The client has no user interface and imports no Glutter: every method may be called from any goroutine, and
// the handler for the adapter's requests runs off the goroutine that reads the adapter, so the host hands its
// work over to the UI thread itself. The debug model that drives the sequence of a session is internal/core/debug
// (step 25.2); the scripted fake adapter that every test uses is internal/core/dap/daptest.
//
// The protocol types in types*.go are ported from VS Code's debugProtocol.d.ts, which is generated from the
// protocol's JSON schema.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/common/debugProtocol.d.ts (the types),
// src/vs/workbench/contrib/debug/common/abstractDebugAdapter.ts (the sequence numbers and the pending requests),
// src/vs/workbench/contrib/debug/node/debugAdapter.ts (the stream, socket, pipe and executable adapters) and
// src/vs/workbench/contrib/debug/common/debugUtils.ts (FormatMessage).
package dap
