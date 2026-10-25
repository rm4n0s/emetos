package exthost

import (
	"context"
	"slices"
	"sync"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/storage"
)

// Event sharing between window tabs (SPEC §10.15 "Event sharing" and "As built (step 11.3)"). The links and the private
// scopes are kept by a storage.SharingStore; this file is the rest: the check at delivery, the question the user is
// asked, the changes the page makes and the event that tells a tab its link is gone.

// SharingAnswer is what the user answered to the question of a link.
type SharingAnswer int

// The answers to a SharingAsk.
const (
	// SharingDeny stores nothing; the extension can ask again when it is activated the next time.
	SharingDeny SharingAnswer = iota
	// SharingAllow saves the link in event-sharing.json.
	SharingAllow
	// SharingSession keeps the link in memory until Emetos quits.
	SharingSession
)

// SharingAsk is the question "Let events from <From> reach <To>?" that the window shows in the receiving tab.
type SharingAsk struct {
	// From is the name of the scope the events come from and FromID its ID (for a workspace its folder).
	From, FromID string
	// To is the name of the scope that asks and ToID its ID.
	To, ToID string
	// Extension is the name of the extension that wants the events (or the names, when several do).
	Extension string
	// Topics are the topic patterns, all of them within the cross-tab topics of the extensions.
	Topics []string
}

// SharingEntry is a link as the page lists it.
type SharingEntry struct {
	// From and To are the IDs of the scopes, FromName and ToName their names (the ID where the scope is not open).
	From, To, FromName, ToName string
	// Events are the topic patterns; empty for every topic that an extension asked for.
	Events []string
	// Session tells that the link lives until Emetos quits and is not in the file.
	Session bool
}

// SharingScope is a scope as the page lists it for the private switch.
type SharingScope struct {
	// ID and Name are those of ScopeInfo (the ID where the scope is not open).
	ID, Name string
	// Open tells that a window tab with the ID is open.
	Open bool
	// Private tells that the scope sends nothing to other scopes.
	Private bool
}

// SharingSnapshot is the state of the page "Extensions > Event Sharing".
type SharingSnapshot struct {
	// Links are the saved links, then the session links.
	Links []SharingEntry
	// Scopes are the open scopes that have an ID, then the private scopes that are not open.
	Scopes []SharingScope
}

// ID names the scope in the links between window tabs (ScopeInfo.ID); empty for a scope that cannot be named.
func (s *Scope) ID() string { return s.opts.Info.ID }

// ---- the check at delivery ----

// allows tells whether the events of the topic go from one scope to the other now: both can be named in a link, the
// scope they come from is not private, and a link in this direction carries the topic. It is called for every event at the
// moment of its delivery, so a link that was removed stops delivery at once.
func (h *Host) allows(from, to *Scope, topic string) bool {
	if from == nil || to == nil || from == to || from.ID() == "" || to.ID() == "" || from.ID() == to.ID() {
		return false
	}
	return h.sharing.Allows(from.ID(), to.ID(), topic, extension.MatchTopic)
}

// otherScopes are the open scopes other than src that can be named in a link, in the order they were made; none when src
// cannot be named.
func (h *Host) otherScopes(src *Scope) []*Scope {
	if src.ID() == "" {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []*Scope
	for _, s := range h.scopes {
		if s != src && s.ID() != "" && s.ID() != src.ID() {
			out = append(out, s)
		}
	}
	return out
}

// scopeByID is the open scope with the ID, or nil.
func (h *Host) scopeByID(id string) *Scope {
	if id == "" {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, s := range h.scopes {
		if s.ID() == id {
			return s
		}
	}
	return nil
}

// ---- the denials of the session ----

// denyKey is a question that the user answered with Deny.
type denyKey struct{ ext, from, pattern string }

// isDenied tells whether the user denied the extension this pattern from that scope since it was activated.
func (s *Scope) isDenied(ext, from, pattern string) bool {
	s.denyMu.Lock()
	defer s.denyMu.Unlock()
	return s.denied[denyKey{ext, from, pattern}]
}

// deny remembers a Deny until the extension is deactivated (it can ask again at its next activation).
func (s *Scope) deny(ext, from, pattern string) {
	s.denyMu.Lock()
	defer s.denyMu.Unlock()
	if s.denied == nil {
		s.denied = map[denyKey]bool{}
	}
	s.denied[denyKey{ext, from, pattern}] = true
}

// denyFrom remembers, for every subscription of the scope that listens to the scope with the id, that the user does not
// want these events: removing a link counts as a Deny, so that the extension does not ask at the next event.
func (s *Scope) denyFrom(fromID string) {
	s.bus.mu.Lock()
	subs := slices.Clone(s.bus.subs)
	s.bus.mu.Unlock()
	for _, sub := range subs {
		if sub.from.Matches(fromID) {
			s.deny(sub.inst.id, fromID, sub.pattern)
		}
	}
}

// clearDenied forgets the denials of an extension that was deactivated.
func (s *Scope) clearDenied(ext string) {
	s.denyMu.Lock()
	defer s.denyMu.Unlock()
	for k := range s.denied {
		if k.ext == ext {
			delete(s.denied, k)
		}
	}
}

// ---- the question ----

// askEntry is what one extension wants: a pattern, and the functions to call with the answer (requests wait for it).
type askEntry struct {
	ext     *instance
	pattern string
	dones   []func(allowed bool)
}

// pendingAsk is the question about one pair of scopes that is on screen, with what was asked while it was.
type pendingAsk struct {
	shown, late []*askEntry
}

// askKey is a pair of scopes.
type askKey struct{ from, to string }

// canAsk tells whether the user can be asked to let the events of from reach the extension: both scopes can be named,
// from is not private, the receiving window can ask, and the extension was not denied.
func (h *Host) canAsk(from *Scope, ext *instance, pattern string) bool {
	to := ext.scope
	if from == nil || from == to || from.ID() == "" || to.ID() == "" || to.opts.AskSharing == nil {
		return false
	}
	return !h.sharing.IsPrivate(from.ID()) && !to.isDenied(ext.id, from.ID(), pattern)
}

// ask makes sure that the events of from within the pattern can reach the extension: when a link carries them already
// done(true) is called at once; when the question cannot be asked done(false); otherwise the user is asked in the receiving
// tab, once for a pair of scopes at a time, and done is called with the answer. Questions about the same pair that come
// while one is on screen wait for its answer and are asked again when the link does not cover them. done may be nil (an
// event that waits for no one). It runs on the UI thread.
func (h *Host) ask(from *Scope, ext *instance, pattern string, done func(allowed bool)) {
	call := func(ok bool) {
		if done != nil {
			done(ok)
		}
	}
	to := ext.scope
	if !h.canAsk(from, ext, pattern) {
		call(false)
		return
	}
	if h.sharing.Carries(from.ID(), to.ID(), pattern, extension.PatternWithin) {
		call(true)
		return
	}
	key := askKey{from.ID(), to.ID()}
	h.askMu.Lock()
	if p := h.asks[key]; p != nil {
		for _, list := range [][]*askEntry{p.shown, p.late} {
			for _, e := range list {
				if e.ext == ext && e.pattern == pattern {
					if done != nil {
						e.dones = append(e.dones, done)
					}
					h.askMu.Unlock()
					return
				}
			}
		}
		e := &askEntry{ext: ext, pattern: pattern}
		if done != nil {
			e.dones = append(e.dones, done)
		}
		p.late = append(p.late, e)
		h.askMu.Unlock()
		return
	}
	e := &askEntry{ext: ext, pattern: pattern}
	if done != nil {
		e.dones = append(e.dones, done)
	}
	p := &pendingAsk{shown: []*askEntry{e}}
	h.asks[key] = p
	h.askMu.Unlock()
	var once sync.Once
	to.opts.AskSharing(SharingAsk{
		From: from.opts.Info.Name, FromID: from.ID(), To: to.opts.Info.Name, ToID: to.ID(), Extension: ext.name, Topics: []string{pattern},
	}, func(a SharingAnswer) { once.Do(func() { h.answered(key, from, to, p, a) }) })
}

// answered carries out the answer of the user: the link is saved or kept for the session, or the denial is remembered; the
// extensions that waited for it are told, and what was asked meanwhile is asked again.
func (h *Host) answered(key askKey, from, to *Scope, p *pendingAsk, a SharingAnswer) {
	h.askMu.Lock()
	delete(h.asks, key)
	h.askMu.Unlock()
	var topics []string
	for _, e := range p.shown {
		if !slices.Contains(topics, e.pattern) {
			topics = append(topics, e.pattern)
		}
	}
	link := storage.SharingLink{From: key.from, To: key.to, Events: topics}
	switch a {
	case SharingAllow:
		if err := h.sharing.AddLink(context.Background(), link); err != nil {
			h.log.Warn("the link cannot be saved; it is kept for this session", "err", err)
			h.sharing.AddSessionLink(link)
			to.opts.Notify.Warn("Emetos", "Cannot save the link from "+from.opts.Info.Name+" to "+to.opts.Info.Name+"; it lasts until Emetos quits.")
		}
	case SharingSession:
		h.sharing.AddSessionLink(link)
	default:
		for _, e := range p.shown {
			to.deny(e.ext.id, from.ID(), e.pattern)
		}
	}
	h.changed()
	allowed := a == SharingAllow || a == SharingSession
	for _, e := range p.shown {
		for _, d := range e.dones {
			d(allowed)
		}
	}
	for _, e := range p.late {
		dones := e.dones
		h.ask(from, e.ext, e.pattern, func(ok bool) {
			for _, d := range dones {
				d(ok)
			}
		})
	}
}

// ---- the page ----

// SharingSnapshot is what the page "Extensions > Event Sharing" shows.
func (h *Host) SharingSnapshot() SharingSnapshot {
	snap := h.sharing.Snapshot()
	open := map[string]*Scope{}
	var ids []string
	for _, s := range h.Scopes() {
		if s.ID() != "" && open[s.ID()] == nil {
			open[s.ID()] = s
			ids = append(ids, s.ID())
		}
	}
	name := func(id string) string {
		if s := open[id]; s != nil {
			return s.opts.Info.Name
		}
		return id
	}
	var out SharingSnapshot
	add := func(list []storage.SharingLink, session bool) {
		for _, l := range list {
			out.Links = append(out.Links, SharingEntry{From: l.From, To: l.To, FromName: name(l.From), ToName: name(l.To), Events: l.Events, Session: session})
		}
	}
	add(snap.Links, false)
	add(snap.Session, true)
	for _, id := range ids {
		out.Scopes = append(out.Scopes, SharingScope{ID: id, Name: name(id), Open: true, Private: slices.Contains(snap.Private, id)})
	}
	for _, id := range snap.Private {
		if open[id] == nil {
			out.Scopes = append(out.Scopes, SharingScope{ID: id, Name: id, Private: true})
		}
	}
	return out
}

// RemoveLink removes the link between two scopes (a session link when session is true) and tells the receiving tab with
// emetos.sharing.revoked. The removal takes effect at once, also when the file cannot be written; the error then has the
// tag SharingWriteFailed. The page calls it.
func (h *Host) RemoveLink(ctx context.Context, from, to string, session bool) error {
	removed, ok, err := h.sharing.RemoveLink(ctx, from, to, session)
	if ok {
		// the user said no to these events: the extensions that listen are not asked again until they are activated anew
		if target := h.scopeByID(to); target != nil {
			target.denyFrom(from)
		}
		h.publishRevoked(removed, extension.RevokedLinkRemoved)
		h.changed()
	}
	return err
}

// SetPrivate makes a scope private or not. A scope that becomes private sends nothing at once, and the tabs that had a link
// from it are told with emetos.sharing.revoked. The change takes effect at once, also when the file cannot be written; the
// error then has the tag SharingWriteFailed. The page calls it.
func (h *Host) SetPrivate(ctx context.Context, id string, on bool) error {
	snap := h.sharing.Snapshot()
	changed, err := h.sharing.SetPrivate(ctx, id, on)
	if changed {
		if on {
			for _, list := range [][]storage.SharingLink{snap.Links, snap.Session} {
				for _, l := range list {
					if l.From == id {
						h.publishRevoked(l, extension.RevokedPrivate)
					}
				}
			}
		}
		h.changed()
	}
	return err
}

// publishRevoked tells the open scope a link led to that its events no longer come.
func (h *Host) publishRevoked(l storage.SharingLink, reason string) {
	to := h.scopeByID(l.To)
	if to == nil {
		return
	}
	fromName := l.From
	if from := h.scopeByID(l.From); from != nil {
		fromName = from.opts.Info.Name
	}
	rev := extension.SharingRevoked{From: l.From, FromName: fromName, Topics: l.Events, Reason: reason}
	if err := to.PublishCore(extension.TopicSharingRevoked, rev); err != nil {
		h.log.Error("the revocation cannot be published", "err", err)
	}
}

// ---- reading the documents of another scope ----

// readFrom reads a document of another window tab for an extension: it needs a link that carries the document events of
// that tab, which must not be private (SPEC §10.15 "As built (step 11.3)"). Nobody is asked. The error has the tags
// ExtensionReadNotDeclared, ExtensionReadUnnamed, ExtensionScopeUnknown, ExtensionReadPrivate, ExtensionReadNotShared and
// ExtensionSourceDocumentsUnavailable; the window's own error is returned as it is.
func (s *Scope) readFrom(ctx context.Context, in *instance, scopeID, uri string) (string, error) {
	if !extension.PatternWithin(extension.TopicDocumentChanged, in.slot.info.Manifest.Events.CrossTab) {
		return "", errors.New("ExtensionReadNotDeclared", "reading the documents of another window tab needs emetos.document.changed among the cross-tab topics of the manifest", "extension", in.id, "scope", scopeID, "uri", uri)
	}
	if s.ID() == "" {
		return "", errors.New("ExtensionReadUnnamed", "this window tab has no folder, so no link can name it", "extension", in.id, "scope", scopeID, "uri", uri)
	}
	src := s.h.scopeByID(scopeID)
	if src == nil {
		return "", errors.New("ExtensionScopeUnknown", "no open window tab has this id", "extension", in.id, "scope", scopeID, "uri", uri)
	}
	if s.h.sharing.IsPrivate(scopeID) {
		return "", errors.New("ExtensionReadPrivate", "the window tab is private", "extension", in.id, "scope", scopeID, "uri", uri)
	}
	if !s.h.allows(src, s, extension.TopicDocumentChanged) {
		return "", errors.New("ExtensionReadNotShared", "no link lets the documents of that window tab reach this one", "extension", in.id, "scope", scopeID, "uri", uri)
	}
	if src.opts.Documents == nil {
		return "", errors.New("ExtensionSourceDocumentsUnavailable", "the window tab has no documents", "extension", in.id, "scope", scopeID, "uri", uri)
	}
	return src.opts.Documents.Read(ctx, uri)
}
