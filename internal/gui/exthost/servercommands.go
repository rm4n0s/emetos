package exthost

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/core/lsp"
)

// The part of the extension API that runs the commands and the code actions of a language server (SPEC §10.17 "As built (step
// 14.2)"): the commands the server announced, workspace/executeCommand, and "apply the first code action of a kind".

// commandsOf lists the commands of the running client: those of executeCommandProvider and those it registered.
func commandsOf(client *lsp.Client) []string {
	var out []string
	if p := client.Capabilities().ExecuteCommandProvider; p != nil {
		out = append(out, p.Commands...)
	}
	for _, r := range client.Registered(lsp.MethodExecuteCommand) {
		var opts lsp.ExecuteCommandOptions
		if json.Unmarshal(r.RegisterOptions, &opts) == nil {
			out = append(out, opts.Commands...)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// Commands implements extension.LanguageServers.
func (s serversAPI) Commands(serverID string) ([]string, error) {
	in := s.inst
	if !in.declared(serverID) {
		return nil, errors.New("ExtensionServerUnknown", "the manifest of the extension declares no language server with this id", "extension", in.id, "server", serverID)
	}
	c := in.serverCtl(serverID)
	if c == nil {
		return nil, nil
	}
	client := c.currentClient()
	if client == nil {
		return nil, nil
	}
	return commandsOf(client), nil
}

// ExecuteCommand implements extension.LanguageServers.
func (s serversAPI) ExecuteCommand(ctx context.Context, serverID, command string, args []any, done func(error)) error {
	in := s.inst
	if in.isDead() {
		return errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	if !in.declared(serverID) {
		return errors.New("ExtensionServerUnknown", "the manifest of the extension declares no language server with this id", "extension", in.id, "server", serverID)
	}
	cmd := langfeatures.Command{ID: command}
	for _, a := range args {
		data, err := json.Marshal(a)
		if err != nil {
			return errors.NewErr("ExtensionArgumentsInvalid", err).SetMetadata("extension", in.id, "command", command)
		}
		cmd.Arguments = append(cmd.Arguments, data)
	}
	c := in.serverCtl(serverID)
	if c == nil || c.currentClient() == nil {
		s.finishLater(in, done, errors.New("ExtensionServerNotRunning", "the language server is not running", "extension", in.id, "server", serverID))
		return nil
	}
	rctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(c.ctx, cancel)
	go func() {
		defer stop()
		defer cancel()
		err := c.provider.ExecuteCommand(rctx, cmd)
		s.finishLater(in, done, err)
	}()
	return nil
}

// finishLater calls done with the error, on the UI thread, after the call that failed returned.
func (s serversAPI) finishLater(in *instance, done func(error), err error) {
	if done == nil {
		return
	}
	in.scope.post(func() { in.scope.runServerDone(in, func() { done(err) }) })
}

// runServerDone calls a function an extension gave to hear the end of a request to its server. A panic fails the extension with
// ExtensionServerCallbackPanicked.
func (s *Scope) runServerDone(inst *instance, f func()) {
	if inst.isDead() {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			failure := errors.NewErrf("ExtensionServerCallbackPanicked", "it panicked in the function that hears the end of a request to a language server: %v", r).SetMetadata("extension", inst.id, "scope", s.opts.Info.Name)
			s.fail(inst.slot, failure, "language server request")
		}
	}()
	f()
}

// ApplyCodeAction implements extension.LanguageServers.
func (s serversAPI) ApplyCodeAction(ctx context.Context, serverID, uri string, req extension.CodeActionRequest, done func(string, error)) error {
	in := s.inst
	if in.isDead() {
		return errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	if !in.declared(serverID) {
		return errors.New("ExtensionServerUnknown", "the manifest of the extension declares no language server with this id", "extension", in.id, "server", serverID)
	}
	finish := func(title string, err error) {
		if done != nil {
			in.scope.post(func() { in.scope.runServerDone(in, func() { done(title, err) }) })
		}
	}
	c := in.serverCtl(serverID)
	if c == nil || c.currentClient() == nil {
		finish("", errors.New("ExtensionServerNotRunning", "the language server is not running", "extension", in.id, "server", serverID))
		return nil
	}
	u, err := document.ParseURI(uri)
	if err != nil {
		finish("", err)
		return nil
	}
	var doc *document.Document
	for _, d := range c.ws.Documents() {
		if d.URI() == u {
			doc = d
		}
	}
	if doc == nil {
		finish("", errors.New("ExtensionDocumentUnavailable", "the document is not open in the scope", "extension", in.id, "uri", uri))
		return nil
	}
	ld := langfeatures.Document{URI: u, LanguageID: doc.LanguageID(), Version: doc.Version(), Text: doc.Snapshot()}
	rctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(c.ctx, cancel)
	go func() {
		defer stop()
		defer cancel()
		title, err := c.applyFirstAction(rctx, ld, langfeatures.CodeActionRequest{Range: fromRange(req.Range), Only: req.Only, Diagnostics: fromDiagnostics(req.Diagnostics)})
		finish(title, err)
	}()
	return nil
}

// applyFirstAction asks the server for the code actions of the request and applies the one to take: not disabled, of a kind asked
// for, the preferred one first. It runs off the UI thread; the edit is applied on it.
func (c *serverCtl) applyFirstAction(ctx context.Context, doc langfeatures.Document, req langfeatures.CodeActionRequest) (string, error) {
	actions, err := c.provider.ProvideCodeActions(ctx, doc, req)
	if err != nil {
		return "", err
	}
	var chosen *langfeatures.CodeAction
	for i := range actions {
		a := &actions[i]
		if a.Disabled != "" || !kindWanted(req.Only, a.Kind) {
			continue
		}
		if chosen == nil || (a.Preferred && !chosen.Preferred) {
			chosen = a
		}
	}
	if chosen == nil {
		return "", nil
	}
	act := *chosen
	if act.Edit == nil && act.Command == nil && c.provider.CanResolveAction(act) {
		if act, err = c.provider.ResolveCodeAction(ctx, act); err != nil {
			return act.Title, err
		}
	}
	if act.Edit != nil {
		applied := make(chan error, 1)
		c.s.post(func() {
			_, err := langfeatures.Apply(ctx, c.ws.Edits(), act.Edit)
			applied <- err
		})
		select {
		case err := <-applied:
			if err != nil {
				return act.Title, err
			}
		case <-ctx.Done():
			return act.Title, ctx.Err()
		}
	}
	if act.Command != nil {
		if err := c.provider.ExecuteCommand(ctx, *act.Command); err != nil {
			return act.Title, err
		}
	}
	return act.Title, nil
}

// kindWanted tells whether a code action of the kind is one of the kinds asked for (any when none is).
func kindWanted(only []string, kind string) bool {
	if len(only) == 0 {
		return true
	}
	return slices.ContainsFunc(only, func(o string) bool { return langfeatures.KindContains(o, kind) })
}
