package exthost

import (
	"context"
	"encoding/json"
	"slices"
	"sync/atomic"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/lsp"
)

// The part of the extension API that talks the language server's own protocol (SPEC §9.12 "As built (step 24b.2)"): requests of its
// own methods, its notifications, and the encoding of its positions.

// notifSub is a listener of the notifications of one method of a server.
type notifSub struct {
	server, method string
	fn             func(json.RawMessage)
	cancelled      atomic.Bool
}

// Request implements extension.LanguageServers.
func (s serversAPI) Request(ctx context.Context, serverID, method string, params any) (json.RawMessage, error) {
	in := s.inst
	if in.isDead() {
		return nil, errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	if !in.declared(serverID) {
		return nil, errors.New("ExtensionServerUnknown", "the manifest of the extension declares no language server with this id", "extension", in.id, "server", serverID)
	}
	if _, err := json.Marshal(params); err != nil {
		return nil, errors.NewErr("ExtensionArgumentsInvalid", err).SetMetadata("extension", in.id, "server", serverID, "method", method)
	}
	c := in.serverCtl(serverID)
	var client *lsp.Client
	if c != nil {
		client = c.currentClient()
	}
	if client == nil {
		return nil, errors.New("ExtensionServerNotRunning", "the language server is not running", "extension", in.id, "server", serverID)
	}
	rctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(c.ctx, cancel)
	defer stop()
	defer cancel()
	var result json.RawMessage
	if err := client.Call(rctx, method, params, &result); err != nil {
		return nil, err
	}
	if len(result) == 0 {
		result = json.RawMessage("null")
	}
	return result, nil
}

// OnNotification implements extension.LanguageServers.
func (s serversAPI) OnNotification(serverID, method string, fn func(json.RawMessage)) (func(), error) {
	in := s.inst
	if !in.declared(serverID) {
		return nil, errors.New("ExtensionServerUnknown", "the manifest of the extension declares no language server with this id", "extension", in.id, "server", serverID)
	}
	if in.isDead() || fn == nil {
		return func() {}, nil
	}
	sub := &notifSub{server: serverID, method: method, fn: fn}
	in.mu.Lock()
	in.notifSubs = append(in.notifSubs, sub)
	in.mu.Unlock()
	cancel := func() {
		sub.cancelled.Store(true)
		in.mu.Lock()
		in.notifSubs = slices.DeleteFunc(in.notifSubs, func(x *notifSub) bool { return x == sub })
		in.mu.Unlock()
	}
	in.track(cancel)
	return cancel, nil
}

// PositionEncoding implements extension.LanguageServers.
func (s serversAPI) PositionEncoding(serverID string) (string, error) {
	in := s.inst
	if !in.declared(serverID) {
		return "", errors.New("ExtensionServerUnknown", "the manifest of the extension declares no language server with this id", "extension", in.id, "server", serverID)
	}
	if c := in.serverCtl(serverID); c != nil {
		return c.encoding().String(), nil
	}
	return lsp.UTF16.String(), nil
}

// notification receives the notifications of the server that the client has no handler for. It runs on the goroutine that delivers
// notifications, in order; the listeners are called on the UI thread.
func (c *serverCtl) notification(method string, params json.RawMessage) {
	in := c.inst
	in.mu.Lock()
	subs := slices.DeleteFunc(slices.Clone(in.notifSubs), func(x *notifSub) bool { return x.server != c.spec.ID || x.method != method })
	in.mu.Unlock()
	if len(subs) == 0 {
		return
	}
	data := slices.Clone(params)
	c.s.post(func() {
		for _, sub := range subs {
			if !sub.cancelled.Load() {
				c.s.runNotificationListener(in, sub.fn, data)
			}
		}
	})
}

// runNotificationListener calls a function that an extension gave to hear a notification of a server. A panic fails the extension with
// ExtensionServerCallbackPanicked.
func (s *Scope) runNotificationListener(inst *instance, f func(json.RawMessage), params json.RawMessage) {
	if inst.isDead() {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			failure := errors.NewErrf("ExtensionServerCallbackPanicked", "it panicked in a listener of the notifications of a language server: %v", r).SetMetadata("extension", inst.id, "scope", s.opts.Info.Name)
			s.fail(inst.slot, failure, "language server notification")
		}
	}()
	f(params)
}
