package scm

import (
	"context"
	"reflect"
	"time"
)

// The history of a source control (SPEC §10.30 "The Source Control Graph", step 24.20): the provider that gives the Graph view its
// commits and references, and the current references. The model holds them and tells its subscribers when they change; the view
// asks the provider.
//
// Ported from VS Code: src/vs/workbench/contrib/scm/common/history.ts (ISCMHistoryProvider, ISCMHistoryItem, ISCMHistoryItemRef)

// HistoryRef is a branch or a tag (VS Code's ISCMHistoryItemRef).
type HistoryRef struct {
	// ID identifies the reference ("refs/heads/main"), Name is what the badge shows and Revision the commit it points at.
	ID, Name, Revision string
	// Category groups the references in the picker; Description is dimmed text after the name.
	Category, Description string
	// Icon is the name of a Codicon.
	Icon string
}

// HistoryStats is the size of the change of a commit.
type HistoryStats struct {
	// Files, Insertions and Deletions count the files changed and the lines added and removed.
	Files, Insertions, Deletions int
}

// HistoryItem is a commit (VS Code's ISCMHistoryItem).
type HistoryItem struct {
	// ID is the hash and ParentIDs are the hashes of the parents, the first parent first.
	ID        string
	ParentIDs []string
	// Subject is the first line of the message and Message the whole message; DisplayID is the short hash.
	Subject, Message, DisplayID string
	// Author and AuthorEmail identify the author.
	Author, AuthorEmail string
	// Timestamp is the author date; zero for none.
	Timestamp time.Time
	// Stats is the size of the change; nil when not counted.
	Stats *HistoryStats
	// Refs are the references that point at the commit.
	Refs []HistoryRef
}

// HistoryQuery says which commits are wanted (VS Code's ISCMHistoryOptions).
type HistoryQuery struct {
	// Skip and Limit are the page.
	Skip, Limit int
	// RefIDs are what the walk starts from.
	RefIDs []string
	// Filter is the text of the search box.
	Filter string
}

// HistoryRefsChange is a change of the references (VS Code's ISCMHistoryItemRefsChangeEvent).
type HistoryRefsChange struct {
	// Added, Removed and Modified are the references.
	Added, Removed, Modified []HistoryRef
	// Silent says that it happened in the background.
	Silent bool
}

// HistoryProvider answers the Graph view for one source control. The functions run off the UI thread.
type HistoryProvider interface {
	// Items makes a page of commits.
	Items(ctx context.Context, q HistoryQuery) ([]HistoryItem, error)
	// Refs lists the references, or the ones with the ids.
	Refs(ctx context.Context, ids []string) ([]HistoryRef, error)
	// CommonAncestor finds the best common ancestor of references; empty for none.
	CommonAncestor(ctx context.Context, refs []string) (string, error)
	// OpenCommand is the command that a row is opened with; empty for none.
	OpenCommand() string
}

// HistoryRefs are the three references the Graph is drawn around (VS Code's historyItemRef, historyItemRemoteRef and
// historyItemBaseRef); a nil one is not there.
type HistoryRefs struct {
	// Current is the branch checked out, Remote the branch it tracks and Base the branch its merge base is on.
	Current, Remote, Base *HistoryRef
}

// historyState is what a source control holds of its history; guarded by the model's lock.
type historyState struct {
	provider HistoryProvider
	refs     HistoryRefs
}

// SetHistoryProvider gives the source control a history provider, or takes it away with nil. It reports false for a source control
// that was removed.
func (s *Source) SetHistoryProvider(p HistoryProvider) bool {
	ok := false
	s.mutateKind(HistoryChanged, nil, func() bool {
		if s.removed {
			return false
		}
		ok = true
		if p == nil {
			s.hist = nil
			return true
		}
		s.hist = &historyState{provider: p}
		return true
	})
	return ok
}

// HistoryProvider returns the provider, or nil.
func (s *Source) HistoryProvider() HistoryProvider {
	s.m.mu.Lock()
	defer s.m.mu.Unlock()
	if s.hist == nil {
		return nil
	}
	return s.hist.provider
}

// SetHistoryRefs sets the current references; the subscribers hear of it when they changed.
func (s *Source) SetHistoryRefs(r HistoryRefs) {
	s.mutateKind(HistoryChanged, nil, func() bool {
		if s.removed || s.hist == nil || reflect.DeepEqual(s.hist.refs, cloneRefs(r)) {
			return false
		}
		s.hist.refs = cloneRefs(r)
		return true
	})
}

// HistoryRefs returns the current references.
func (s *Source) HistoryRefs() HistoryRefs {
	s.m.mu.Lock()
	defer s.m.mu.Unlock()
	if s.hist == nil {
		return HistoryRefs{}
	}
	return cloneRefs(s.hist.refs)
}

// NotifyHistoryRefs tells the subscribers that references were added, removed or moved.
func (s *Source) NotifyHistoryRefs(c HistoryRefsChange) {
	s.mutateKind(HistoryRefsChanged, &c, func() bool { return !s.removed && s.hist != nil })
}

func cloneRefs(r HistoryRefs) HistoryRefs {
	cp := func(p *HistoryRef) *HistoryRef {
		if p == nil {
			return nil
		}
		c := *p
		return &c
	}
	return HistoryRefs{Current: cp(r.Current), Remote: cp(r.Remote), Base: cp(r.Base)}
}

// mutateKind is mutate with the kind of the event and a change of references to hand to the subscribers.
func (s *Source) mutateKind(kind EventKind, refs *HistoryRefsChange, fn func() bool) {
	m := s.m
	changed := func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		return fn()
	}()
	if !changed {
		return
	}
	m.notify(Event{Kind: kind, Source: s, Refs: refs})
}

// HistoryProviderCount is the number of source controls that have a history provider (the context key scm.historyProviderCount).
func (m *Model) HistoryProviderCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.historyCountLocked()
}

func (m *Model) historyCountLocked() int {
	n := 0
	for _, s := range m.sources {
		if s.hist != nil {
			n++
		}
	}
	return n
}

// activeBranchLocked is the name of the current reference of the active repository (the context key scmActiveRepositoryBranchName).
func (m *Model) activeBranchLocked() string {
	active, _ := m.activeLocked()
	if active == nil || active.hist == nil || active.hist.refs.Current == nil {
		return ""
	}
	return active.hist.refs.Current.Name
}

// The arguments of the commands that the menus of the Graph run (extension.HistoryArgs reads them): one list of objects, the source
// control, the commit and, for a menu of a reference, the id of the reference.
//
// Ported from VS Code: src/vs/workbench/contrib/scm/browser/scmHistoryViewPane.ts (the actions of SCMHistoryItemContext get the
// provider, then the history item, then the id of the reference)

// HistoryItemArg is the object that stands for a commit in the arguments of a command.
func HistoryItemArg(it HistoryItem) map[string]any {
	refs := make([]any, len(it.Refs))
	for i, r := range it.Refs {
		refs[i] = map[string]any{"id": r.ID, "name": r.Name, "revision": r.Revision, "category": r.Category}
	}
	parents := make([]any, len(it.ParentIDs))
	for i, p := range it.ParentIDs {
		parents[i] = p
	}
	return map[string]any{"historyItem": map[string]any{
		"id": it.ID, "parentIds": parents, "subject": it.Subject, "message": it.Message, "displayId": it.DisplayID,
		"author": it.Author, "authorEmail": it.AuthorEmail, "references": refs,
	}}
}

// HistoryMenuArgs are the arguments of a command run by a menu of a commit (and of one of its references when refID is set).
func HistoryMenuArgs(s *Source, it HistoryItem, refID string) []any {
	args := []any{SourceArg(s), HistoryItemArg(it)}
	if refID != "" {
		args = append(args, map[string]any{"historyItemRef": refID})
	}
	return args
}

// HistoryRangeArgs are the arguments of the command that opens the changes of a range (the incoming and the outgoing changes).
func HistoryRangeArgs(s *Source, base, head, title string) []any {
	return []any{SourceArg(s), map[string]any{"historyRange": map[string]any{"base": base, "head": head, "title": title}}}
}
