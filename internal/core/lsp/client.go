package lsp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/errors"
)

// Handlers answer what a server asks of the client and receive what it tells (SPEC §9.12). Every member is
// optional. Requests are answered on a goroutine each and notifications are delivered one at a time in the
// order they arrived, never on the goroutine that reads the stream; none of them runs on the UI thread, so
// a handler hands its work to the UI thread itself and, if it must wait for the UI, waits with ctx.
type Handlers struct {
	// Configuration answers workspace/configuration: one value per item, as JSON. Without it every item is
	// null.
	Configuration func(ctx context.Context, params ConfigurationParams) ([]json.RawMessage, error)
	// ApplyEdit answers workspace/applyEdit. Without it the edit is refused with Applied false.
	ApplyEdit func(ctx context.Context, params ApplyWorkspaceEditParams) (ApplyWorkspaceEditResult, error)
	// ShowMessageRequest answers window/showMessageRequest with the action the user chose, or nil for none.
	// Without it the answer is null.
	ShowMessageRequest func(ctx context.Context, params ShowMessageRequestParams) (*MessageActionItem, error)
	// ShowMessage receives window/showMessage.
	ShowMessage func(params ShowMessageParams)
	// LogMessage receives window/logMessage.
	LogMessage func(params LogMessageParams)
	// CreateProgress answers window/workDoneProgress/create; the progress itself arrives in Progress.
	// Without it the creation is accepted.
	CreateProgress func(ctx context.Context, token ProgressToken) error
	// Progress receives $/progress.
	Progress func(params ProgressParams)
	// PublishDiagnostics receives textDocument/publishDiagnostics.
	PublishDiagnostics func(params PublishDiagnosticsParams)
	// Refresh answers the workspace refresh requests (workspace/semanticTokens/refresh, codeLens/refresh,
	// inlayHint/refresh, diagnostic/refresh): what was cached from the server is stale. Without it they
	// are accepted.
	Refresh func(ctx context.Context, method string) error
	// RegistrationsChanged is called after client/registerCapability or client/unregisterCapability was
	// recorded; Client.Registrations gives the new state.
	RegistrationsChanged func()
	// Notification receives every other notification of the server ($/logTrace, telemetry/event, ...).
	Notification func(method string, params json.RawMessage)
}

// ClientOptions are the settings of a Client.
type ClientOptions struct {
	// Root is the absolute path of the workspace folder; empty means no folder (rootUri and
	// workspaceFolders are null).
	Root string
	// RootName is the name of the folder; empty means the last element of Root.
	RootName string
	// Version is the version of Emetos, for clientInfo; empty means "dev".
	Version string
	// Locale is the language of the user interface, such as "en".
	Locale string
	// InitializationOptions is sent as initializationOptions, as JSON.
	InitializationOptions json.RawMessage
	// Capabilities replace DefaultClientCapabilities when set (tests).
	Capabilities ClientCapabilities
	// Handlers answer the server.
	Handlers Handlers
	// MaxMessageSize is the largest message body accepted; zero means DefaultMaxMessageSize.
	MaxMessageSize int
	// Logger receives the problems that do not end the connection; nil discards them.
	Logger *slog.Logger
	// Trace receives every message that crosses the connection (ConnOptions.Trace).
	Trace func(TraceMessage)
}

// Client is a language server as the editor uses it: an initialized connection, the capabilities of the
// server, the dynamic registrations and the documents it keeps in sync. It is safe for concurrent use,
// except that the methods that take a document (OpenDocument and the SyncedDocument's) belong to the
// goroutine that owns the document.
type Client struct {
	conn *Conn
	opts ClientOptions
	log  *slog.Logger

	caps     ServerCapabilities
	info     *ServerInfo
	encoding Encoding

	mu            sync.Mutex
	registrations map[string]Registration
	docs          map[string]*SyncedDocument
}

// NewClient starts a connection over rwc and performs the handshake: the initialize request with the
// client capabilities, the position encoding negotiation, and the initialized notification. The client
// owns rwc. On failure rwc is closed. It fails with the tags of Conn.Call (a server that answers with an
// error or dies gives ServerError or ServerClosed) and PositionEncodingUnsupported.
func NewClient(ctx context.Context, rwc io.ReadWriteCloser, opts ClientOptions) (*Client, error) {
	c := &Client{
		opts: opts, log: opts.Logger,
		registrations: map[string]Registration{},
		docs:          map[string]*SyncedDocument{},
	}
	if c.log == nil {
		c.log = slog.New(slog.DiscardHandler)
	}
	c.conn = NewConn(rwc, ConnOptions{
		MaxMessageSize: opts.MaxMessageSize, Logger: c.log, Trace: opts.Trace,
		HandleRequest: c.handleRequest, HandleNotification: c.handleNotification,
	})

	version := opts.Version
	if version == "" {
		version = "dev"
	}
	caps := opts.Capabilities
	if caps == nil {
		caps = DefaultClientCapabilities()
	}
	params := InitializeParams{
		ProcessID: os.Getpid(), ClientInfo: &ClientInfo{Name: "Emetos", Version: version}, Locale: opts.Locale,
		InitializationOptions: opts.InitializationOptions, Capabilities: caps, Trace: "off",
	}
	if opts.Root != "" {
		uri := DocumentURI(document.FileURI(opts.Root))
		params.RootURI = &uri
		params.WorkspaceFolders = c.workspaceFolders()
	}
	var res InitializeResult
	if err := c.conn.Call(ctx, MethodInitialize, params, &res); err != nil {
		c.closeConn()
		return nil, err
	}
	enc, ok := res.Capabilities.Encoding()
	if !ok {
		c.closeConn()
		return nil, errors.New("PositionEncodingUnsupported", "the server chose a position encoding the client does not know", "encoding", res.Capabilities.PositionEncoding)
	}
	c.caps, c.info, c.encoding = res.Capabilities, res.ServerInfo, enc
	if err := c.conn.Notify(MethodInitialized, struct{}{}); err != nil {
		c.closeConn()
		return nil, err
	}
	return c, nil
}

// closeConn closes the connection of a client that is being given up; a failure to close is logged.
func (c *Client) closeConn() {
	if err := c.conn.Close(); err != nil {
		c.log.Debug("closing the connection failed", "err", err)
	}
}

func (c *Client) workspaceFolders() []WorkspaceFolder {
	if c.opts.Root == "" {
		return nil
	}
	name := c.opts.RootName
	if name == "" {
		name = filepath.Base(c.opts.Root)
	}
	return []WorkspaceFolder{{URI: DocumentURI(document.FileURI(c.opts.Root)), Name: name}}
}

// Capabilities are the capabilities the server returned from initialize. Dynamic registrations are not in
// them: see Registrations.
func (c *Client) Capabilities() ServerCapabilities { return c.caps }

// ServerInfo is the name and version the server gave, or nil.
func (c *Client) ServerInfo() *ServerInfo { return c.info }

// Encoding is the position encoding negotiated with the server.
func (c *Client) Encoding() Encoding { return c.encoding }

// Done is closed when the connection has ended (the server died, the stream broke, or Close or Shutdown
// ended it).
func (c *Client) Done() <-chan struct{} { return c.conn.Done() }

// Err is why the connection ended unexpectedly; nil while it runs and after Close or Shutdown. At the end of
// the server's output it is io.EOF.
func (c *Client) Err() error { return c.conn.Err() }

// Wait returns when the goroutines of the connection have ended.
func (c *Client) Wait() { c.conn.Wait() }

// Call sends a request and decodes the result; see Conn.Call.
func (c *Client) Call(ctx context.Context, method string, params, result any) error {
	return c.conn.Call(ctx, method, params, result)
}

// Notify queues a notification; see Conn.Notify.
func (c *Client) Notify(method string, params any) error { return c.conn.Notify(method, params) }

// Request sends a request and returns its result as an R (the zero value when the server answers null).
func Request[R any](ctx context.Context, c *Client, method string, params any) (R, error) {
	var res R
	if err := c.conn.Call(ctx, method, params, &res); err != nil {
		var zero R
		return zero, err
	}
	return res, nil
}

// ErrorCode is the code of the error response of a server that failed a request (the error has the tag
// ServerError); ok is false for any other error.
func ErrorCode(err error) (code int, ok bool) {
	appErr, isApp := errors.FromError(err)
	if !isApp || appErr.Tag != "ServerError" {
		return 0, false
	}
	code, ok = appErr.Metadata["code"].(int)
	return code, ok
}

// Registrations are the capabilities the server registered with client/registerCapability and did not
// unregister, in no particular order.
func (c *Client) Registrations() []Registration {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Registration, 0, len(c.registrations))
	for _, r := range c.registrations {
		out = append(out, r)
	}
	return out
}

// Registered returns the registrations of a method.
func (c *Client) Registered(method string) []Registration {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []Registration
	for _, r := range c.registrations {
		if r.Method == method {
			out = append(out, r)
		}
	}
	return out
}

// Shutdown ends the session as the protocol says: the shutdown request, the exit notification, and then
// the connection is closed. ctx limits the wait for the answer to shutdown; the exit notification is
// written even when it ended (the server is then closed by the connection). A failure of shutdown is
// returned after the connection was closed, so it is closed in every case.
func (c *Client) Shutdown(ctx context.Context) error {
	err := c.conn.Call(ctx, MethodShutdown, nil, nil)
	if nerr := c.conn.Notify(MethodExit, nil); nerr != nil {
		c.log.Debug("the exit notification was not sent", "err", nerr)
	} else {
		flushCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		if ferr := c.conn.Flush(flushCtx); ferr != nil {
			c.log.Debug("the exit notification was not written", "err", ferr)
		}
		cancel()
	}
	if cerr := c.conn.Close(); cerr != nil && err == nil {
		return cerr
	}
	return err
}

// Close closes the connection at once, without the shutdown handshake: for a server that died or does not
// answer. The open documents stay as they are; the host closes them.
func (c *Client) Close() error { return c.conn.Close() }

// handleRequest answers a request of the server.
func (c *Client) handleRequest(ctx context.Context, method string, params json.RawMessage) (any, error) {
	h := c.opts.Handlers
	switch method {
	case MethodConfiguration:
		var p ConfigurationParams
		if err := decodeParams(method, params, &p); err != nil {
			return nil, err
		}
		if h.Configuration == nil {
			return make([]json.RawMessage, len(p.Items)), nil
		}
		out, err := h.Configuration(ctx, p)
		if err != nil {
			return nil, err
		}
		for i := range out {
			if out[i] == nil {
				out[i] = json.RawMessage("null")
			}
		}
		return out, nil
	case MethodApplyEdit:
		var p ApplyWorkspaceEditParams
		if err := decodeParams(method, params, &p); err != nil {
			return nil, err
		}
		if h.ApplyEdit == nil {
			return ApplyWorkspaceEditResult{Applied: false, FailureReason: "the client cannot apply edits"}, nil
		}
		return h.ApplyEdit(ctx, p)
	case MethodShowMessageRequest:
		var p ShowMessageRequestParams
		if err := decodeParams(method, params, &p); err != nil {
			return nil, err
		}
		if h.ShowMessageRequest == nil {
			return nil, nil
		}
		item, err := h.ShowMessageRequest(ctx, p)
		if err != nil || item == nil {
			return nil, err
		}
		return item, nil
	case MethodProgressCreate:
		var p WorkDoneProgressCreateParams
		if err := decodeParams(method, params, &p); err != nil {
			return nil, err
		}
		if h.CreateProgress == nil {
			return nil, nil
		}
		return nil, h.CreateProgress(ctx, p.Token)
	case MethodRegisterCapability:
		var p RegistrationParams
		if err := decodeParams(method, params, &p); err != nil {
			return nil, err
		}
		c.mu.Lock()
		for _, r := range p.Registrations {
			c.registrations[r.ID] = r
		}
		c.mu.Unlock()
		if h.RegistrationsChanged != nil {
			h.RegistrationsChanged()
		}
		return nil, nil
	case MethodUnregisterCapability:
		var p UnregistrationParams
		if err := decodeParams(method, params, &p); err != nil {
			return nil, err
		}
		c.mu.Lock()
		for _, u := range p.Unregisterations {
			delete(c.registrations, u.ID)
		}
		c.mu.Unlock()
		if h.RegistrationsChanged != nil {
			h.RegistrationsChanged()
		}
		return nil, nil
	case MethodWorkspaceFolders:
		return c.workspaceFolders(), nil
	case MethodShowDocument:
		return struct {
			Success bool `json:"success"`
		}{false}, nil
	case MethodSemanticTokensRefresh, MethodCodeLensRefresh, MethodInlayHintRefresh, MethodDiagnosticRefresh:
		if h.Refresh == nil {
			return nil, nil
		}
		return nil, h.Refresh(ctx, method)
	}
	return nil, errors.New("MethodNotFound", "the client does not handle this request", "method", method)
}

// handleNotification delivers a notification of the server to its handler.
func (c *Client) handleNotification(method string, params json.RawMessage) {
	h := c.opts.Handlers
	switch method {
	case MethodLogMessage:
		deliverTo(c, method, params, h.LogMessage)
	case MethodShowMessage:
		deliverTo(c, method, params, h.ShowMessage)
	case MethodProgress:
		deliverTo(c, method, params, h.Progress)
	case MethodPublishDiag:
		deliverTo(c, method, params, h.PublishDiagnostics)
	default:
		if h.Notification != nil {
			h.Notification(method, params)
		}
	}
}

// deliverTo decodes the params of a notification and calls its handler; a notification that cannot be
// read is logged and dropped.
func deliverTo[T any](c *Client, method string, params json.RawMessage, fn func(T)) {
	if fn == nil {
		return
	}
	var p T
	if err := decodeParams(method, params, &p); err != nil {
		c.log.Warn("a notification of the server cannot be read", "err", err)
		return
	}
	fn(p)
}

// decodeParams reads the params of a message of the server into v. It fails with the tag InvalidParams,
// which the connection answers with the code -32602.
func decodeParams(method string, raw json.RawMessage, v any) error {
	if err := json.Unmarshal(raw, v); err != nil {
		return errors.NewErr("InvalidParams", err).SetMetadata("method", method)
	}
	return nil
}
