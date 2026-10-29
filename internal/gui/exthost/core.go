package exthost

import (
	"context"
	"encoding/json"
	"slices"
	"sync"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
)

// The core request topics (SPEC §10.19 "Core request topics", "As built (step 16.1)"): Emetos answers them itself, in the
// scope of every workspace window tab, for every extension. A request whose topic is one of them never reaches the handlers
// of the extensions (an extension cannot own a topic under emetos., except under its own id). The answer is made by the
// Scope the request is made in, so it acts in that window tab and in no other. Their errors reach the requester as they are:
// the tag is the failure's (CoreCommandRefused, ConfineOutside, CoreDocumentVersionStale), not EventHandlerFailed.

// FileOpSlots is how many file requests (list, stat, read) a scope works on at once; the others wait for a slot, within
// their deadline. It bounds the memory the reads of a client can hold.
const FileOpSlots = 4

// coreCall is a core request being answered.
type coreCall struct {
	// inst is the extension that made the request.
	inst *instance
	// ctx ends with the request: its answer, its deadline or cancellation, the end of the extension.
	ctx context.Context
	// data is the payload, JSON.
	data json.RawMessage
	// reply answers the request, once; any goroutine may call it. A second call does nothing.
	reply func(value any, err error)
}

// coreFunc answers one core request. It is called on the UI thread; long work belongs to a goroutine that calls reply
// when it is done.
type coreFunc func(s *Scope, c *coreCall)

// coreHandlerFor is the function that answers a core topic, or nil for any other topic.
func coreHandlerFor(topic string) coreFunc {
	switch topic {
	case extension.TopicCommandsList:
		return (*Scope).coreCommandsList
	case extension.TopicCommandsExecute:
		return (*Scope).coreCommandsExecute
	case extension.TopicFilesList:
		return (*Scope).coreFilesList
	case extension.TopicFilesStat:
		return (*Scope).coreFilesStat
	case extension.TopicFilesRead:
		return (*Scope).coreFilesRead
	case extension.TopicDocumentOpen:
		return (*Scope).coreDocumentOpen
	case extension.TopicDocumentRead:
		return (*Scope).coreDocumentRead
	case extension.TopicDocumentEdit:
		return (*Scope).coreDocumentEdit
	case extension.TopicDocumentSave:
		return (*Scope).coreDocumentSave
	case extension.TopicDocumentClose:
		return (*Scope).coreDocumentClose
	case extension.TopicEditorReveal:
		return (*Scope).coreEditorReveal
	}
	return nil
}

// startCore answers a request for a core topic (UI thread). The wait is bounded like any request: the deadline of the
// context, else DefaultRequestTimeout.
func (b *bus) startCore(r *request, ctx context.Context, topic string, fn coreFunc) {
	r.stops = append(r.stops, b.watch(r, ctx))
	hctx, cancel := context.WithCancel(ctx)
	r.cancel = cancel
	s := b.s
	var once sync.Once
	answer := func(ev extension.Event, err error) {
		once.Do(func() {
			s.post(func() {
				if !b.settle(r) {
					return
				}
				s.callReply(r, ev, err)
			})
		})
	}
	reply := func(value any, err error) {
		if err != nil {
			answer(extension.Event{}, err)
			return
		}
		data, merr := json.Marshal(value)
		if merr != nil {
			answer(extension.Event{}, errors.NewErr("EventReplyInvalid", merr).SetMetadata("topic", topic))
			return
		}
		answer(extension.Event{Topic: topic, Publisher: extension.CorePublisher, Scope: s.opts.Info, Seq: b.nextSeq(), Data: data}, nil)
	}
	defer func() {
		if rec := recover(); rec != nil {
			s.log.Error("a core request panicked", "topic", topic, "panic", rec)
			answer(extension.Event{}, errors.NewErrf("CoreHandlerPanicked", "the core request panicked: %v", rec).SetMetadata("topic", topic))
		}
	}()
	fn(s, &coreCall{inst: r.requester, ctx: hctx, data: slices.Clone(r.ev.Data), reply: reply})
}

// decodeCore decodes the payload of a core request into v (a pointer). A payload of null or no bytes leaves v as it is. It
// fails with CorePayloadInvalid.
func decodeCore(c *coreCall, v any) error {
	if len(c.data) == 0 || string(c.data) == "null" {
		return nil
	}
	return decodeJSON(c.data, v)
}

// decodeJSON decodes JSON into v (a pointer); it fails with CorePayloadInvalid. The arguments of a command are decoded with
// it too: they were checked as part of the payload, so they decode into a value of any type.
func decodeJSON(data json.RawMessage, v any) error {
	if err := json.Unmarshal(data, v); err != nil {
		return errors.NewErr("CorePayloadInvalid", err)
	}
	return nil
}

// ---- the commands ----

// commandKeys are the context keys a command of a client is run and checked with: those of the window tab.
func (s *Scope) commandKeys() contextkey.Context {
	if s.opts.Keys != nil {
		return s.opts.Keys
	}
	return contextkey.NewRoot()
}

// coreCommandsList answers emetos.commands.list: every command of the registry of the window tab, with whether it is
// enabled now and whether clients may run it.
func (s *Scope) coreCommandsList(c *coreCall) {
	reg := s.opts.Commands
	if reg == nil {
		c.reply(nil, errors.New("CoreCommandsUnavailable", "the scope has no commands"))
		return
	}
	keys := s.commandKeys()
	metas := reg.All()
	out := extension.CommandsList{Commands: make([]extension.CommandInfo, 0, len(metas))}
	for _, m := range metas {
		info := extension.CommandInfo{ID: m.ID, Title: m.Title, Category: m.Category, Description: m.Description}
		info.Enabled = reg.HasHandler(m.ID) && reg.Enabled(m.ID, keys)
		if ref, refused := command.ClientRefusal(m.ID); refused {
			info.Refused, info.RefusedClass, info.Reason = true, ref.Class, ref.Reason
		}
		out.Commands = append(out.Commands, info)
	}
	c.reply(out, nil)
}

// coreCommandsExecute answers emetos.commands.execute: it runs a command of the window tab's registry (its commands and
// those of its extensions), never another tab's. It fails with ExtensionPrivilegeMissing for an extension without the
// privilege commands.execute, CoreCommandRefused for a command a client may not run, CoreCommandNotFound,
// CoreCommandDisabled (no handler, or its precondition does not hold), CorePayloadInvalid for arguments that are not JSON,
// and CoreCommandFailed for the error of the command when it is not an *errors.Error already (those are returned as they
// are).
func (s *Scope) coreCommandsExecute(c *coreCall) {
	if !c.inst.hasPrivilege(extension.PrivilegeCommandsExecute) {
		c.reply(nil, errors.New("ExtensionPrivilegeMissing", "running a command needs the privilege commands.execute", "extension", c.inst.id, "privilege", extension.PrivilegeCommandsExecute))
		return
	}
	var req extension.CommandsExecuteRequest
	err := decodeCore(c, &req)
	if err != nil {
		c.reply(nil, err)
		return
	}
	if ref, refused := command.ClientRefusal(req.ID); refused {
		c.reply(nil, errors.New("CoreCommandRefused", ref.Reason, "command", req.ID, "class", ref.Class, "extension", c.inst.id))
		return
	}
	reg := s.opts.Commands
	if reg == nil {
		c.reply(nil, errors.New("CoreCommandsUnavailable", "the scope has no commands"))
		return
	}
	if _, ok := reg.Lookup(req.ID); !ok {
		c.reply(nil, errors.New("CoreCommandNotFound", "no command has this id", "command", req.ID))
		return
	}
	keys := s.commandKeys()
	if !reg.HasHandler(req.ID) || !reg.Enabled(req.ID, keys) {
		c.reply(nil, errors.New("CoreCommandDisabled", "the command has no handler or its precondition does not hold here and now", "command", req.ID))
		return
	}
	var args any
	if len(req.Args) > 0 && string(req.Args) != "null" {
		if err := decodeJSON(req.Args, &args); err != nil {
			c.reply(nil, err)
			return
		}
	}
	s.log.Info("a client runs a command", "extension", c.inst.id, "command", req.ID)
	if err := reg.Run(command.Context{Ctx: c.ctx, Keys: keys, Source: command.SourceAPI}, req.ID, args); err != nil {
		if _, ok := errors.FromError(err); ok {
			c.reply(nil, err)
			return
		}
		c.reply(nil, errors.NewErr("CoreCommandFailed", err).SetMetadata("command", req.ID))
		return
	}
	c.reply(extension.CommandsExecuteReply{Result: json.RawMessage("null")}, nil)
}
