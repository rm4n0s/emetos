package extension

import (
	"context"
	"time"
)

// The history API of a source control (SPEC §10.30 "The Source Control Graph", step 24.20; after SourceControlHistoryProvider,
// SourceControlHistoryItem, SourceControlHistoryItemRef and SourceControlHistoryOptions of vscode.proposed.scmHistoryProvider.d.ts and
// ISCMHistoryProvider of the workbench). An extension tells the workbench the commits of a source control and the references that name
// them, and the Graph view of Source Control draws them as lanes, in pages.
//
// Everything here may be called from any goroutine, except where a comment says that Emetos calls the extension.

// HistoryItemRef is a reference to a history item: a branch, a remote branch or a tag (VS Code's SourceControlHistoryItemRef).
type HistoryItemRef struct {
	// ID identifies the reference: "refs/heads/main", "refs/remotes/origin/main", "refs/tags/v1". The menus read it as the context key
	// scmHistoryItemRef.
	ID string
	// Name is what the badge and the pickers show ("main", "origin/main").
	Name string
	// Revision is the history item the reference points at: a hash.
	Revision string
	// Category groups the references in the picker of the filter ("branches", "remote branches", "tags").
	Category string
	// Description is dimmed text after the name in the picker ("a1b2c3d", "Tag at a1b2c3d").
	Description string
	// Icon is the name of a Codicon drawn in the badge ("git-branch", "cloud", "tag", "target").
	Icon string
}

// HistoryStatistics are the size of the change a history item made (VS Code's SourceControlHistoryItemStatistics).
type HistoryStatistics struct {
	// Files, Insertions and Deletions are the number of files changed and of lines added and removed.
	Files, Insertions, Deletions int
}

// HistoryItem is one commit of the history (VS Code's SourceControlHistoryItem).
type HistoryItem struct {
	// ID identifies the item: a full hash. It is not empty.
	ID string
	// ParentIDs are the IDs of the parents, the first parent first; none for a root.
	ParentIDs []string
	// Subject is the first line of the message and Message the whole message.
	Subject, Message string
	// DisplayID is the short form of the ID ("a1b2c3d").
	DisplayID string
	// Author and AuthorEmail are the name and the address of the author.
	Author, AuthorEmail string
	// Timestamp is the time of the item (the author date); the zero time is none.
	Timestamp time.Time
	// Statistics are the size of the change; nil when the provider did not count it.
	Statistics *HistoryStatistics
	// References are the references that point at this item.
	References []HistoryItemRef
}

// HistoryOptions say which history items a provider is asked for (VS Code's SourceControlHistoryOptions).
type HistoryOptions struct {
	// Skip is how many items to leave out first, and Limit the most items to return (the page).
	Skip, Limit int
	// RefIDs are what the walk starts from: the Revision of the references of the filter, or their IDs for one with no revision. The
	// items are the ones that can be reached from them, newest first with the parents after the children (git's --topo-order).
	RefIDs []string
	// FilterText is the search of the filter box: the items whose message or author has the text, and the item whose hash starts with
	// it. Empty is no filter.
	FilterText string
}

// HistoryRefsChange says which references changed (VS Code's SourceControlHistoryItemRefsChangeEvent).
type HistoryRefsChange struct {
	// Added, Removed and Modified are the references.
	Added, Removed, Modified []HistoryItemRef
	// Silent says that the change happened in the background (an automatic fetch): the Graph refreshes only when it is scrolled to the
	// top and shows "Outdated" otherwise.
	Silent bool
}

// HistoryProvider provides the history of a source control (VS Code's SourceControlHistoryProvider). The functions run off the UI
// thread with a context that is cancelled when the view no longer wants the answer. A panic in one fails the extension
// (ExtensionHistoryPanicked).
type HistoryProvider struct {
	// Items makes a page of the history. It is required. An empty history is an empty page, not an error.
	Items func(ctx context.Context, opts HistoryOptions) ([]HistoryItem, error)
	// Refs lists the references: the branches, then the remote branches, then the tags. With ids it lists only the references with these
	// IDs (with their latest revision). Required.
	Refs func(ctx context.Context, ids []string) ([]HistoryItemRef, error)
	// CommonAncestor finds the best common ancestor of the references with these names or IDs, the history item the incoming and the
	// outgoing changes are measured from; empty when there is none. Required.
	CommonAncestor func(ctx context.Context, refs []string) (string, error)
	// OpenCommand is the command the Graph runs when a row is opened (a click, Enter), with the arguments of HistoryArgs: the commit,
	// or for the incoming and the outgoing changes the range. Empty: nothing happens.
	OpenCommand string
}

// HistoryRegistration is a registered history provider of a source control.
type HistoryRegistration interface {
	// SetCurrentRefs sets the reference of the current branch (VS Code's historyItemRef), the one it tracks (historyItemRemoteRef) and
	// the one its merge base is on (historyItemBaseRef); nil for none. The Graph refreshes when the current reference changes and
	// draws the incoming and the outgoing changes from the three.
	SetCurrentRefs(current, remote, base *HistoryItemRef)
	// RefsChanged says that references were added, removed or moved: the Graph refreshes (or shows that it is outdated).
	RefsChanged(change HistoryRefsChange)
	// Dispose takes the provider away; it happens by itself with the source control and with the activation.
	Dispose()
}

// The members of the arguments that a menu of the Graph gives a command (HistoryArgs).
const (
	// HistoryArgItem is the history item the menu is for.
	HistoryArgItem = "historyItem"
	// HistoryArgRef is the ID of the reference a menu of a badge is for.
	HistoryArgRef = "historyItemRef"
	// HistoryArgRange is the range of changes an incoming or outgoing row opens.
	HistoryArgRange = "historyRange"
)

// HistoryRange is the range of changes that the incoming-changes and the outgoing-changes row open, and that the first row above the
// incoming changes opens (VS Code's viewChanges).
type HistoryRange struct {
	// Base and Head are the two ends: the changes are from Base to Head.
	Base, Head string
	// Title is the name of the tab ("Incoming Changes - main ↔ origin/main").
	Title string
}

// HistoryArg is what a menu of the Graph, or the opening of a row, gave a command: VS Code passes (provider, historyItem, refId);
// Emetos gives one list of objects, [{"sourceControl": "scm1"}, {"historyItem": {...}}, {"historyItemRef": "refs/heads/main"}], which
// HistoryArgs reads.
type HistoryArg struct {
	// SourceControl is the Handle of the source control.
	SourceControl string
	// Item is the history item; nil for a command of the title of the view.
	Item *HistoryItem
	// RefID is the ID of the reference, for the menu of a badge; empty for none.
	RefID string
	// Range is set when the row opens a range of changes.
	Range *HistoryRange
}

// HistoryArgs reads the arguments a menu of the Graph gave a command; ok is false for any other value (the command was run from the
// palette or by a key).
func HistoryArgs(args any) (arg HistoryArg, ok bool) {
	var list []any
	switch v := args.(type) {
	case []any:
		list = v
	case []map[string]any:
		for _, m := range v {
			list = append(list, m)
		}
	default:
		return HistoryArg{}, false
	}
	for _, e := range list {
		m, isMap := e.(map[string]any)
		if !isMap {
			continue
		}
		if s, _ := m[SCMArgSourceControl].(string); s != "" {
			arg.SourceControl, ok = s, true
		}
		if it, isItem := m[HistoryArgItem].(map[string]any); isItem {
			item := historyItemOf(it)
			arg.Item, ok = &item, true
		}
		if s, _ := m[HistoryArgRef].(string); s != "" {
			arg.RefID, ok = s, true
		}
		if r, isRange := m[HistoryArgRange].(map[string]any); isRange {
			rg := HistoryRange{}
			rg.Base, _ = r["base"].(string)
			rg.Head, _ = r["head"].(string)
			rg.Title, _ = r["title"].(string)
			arg.Range, ok = &rg, true
		}
	}
	return arg, ok
}

// historyItemOf reads the object of a history item of an argument.
func historyItemOf(m map[string]any) HistoryItem {
	var it HistoryItem
	it.ID, _ = m["id"].(string)
	it.Subject, _ = m["subject"].(string)
	it.Message, _ = m["message"].(string)
	it.DisplayID, _ = m["displayId"].(string)
	it.Author, _ = m["author"].(string)
	it.AuthorEmail, _ = m["authorEmail"].(string)
	if list, isList := m["parentIds"].([]any); isList {
		for _, p := range list {
			if s, isString := p.(string); isString {
				it.ParentIDs = append(it.ParentIDs, s)
			}
		}
	}
	if list, isList := m["references"].([]any); isList {
		for _, r := range list {
			rm, isMap := r.(map[string]any)
			if !isMap {
				continue
			}
			var ref HistoryItemRef
			ref.ID, _ = rm["id"].(string)
			ref.Name, _ = rm["name"].(string)
			ref.Revision, _ = rm["revision"].(string)
			ref.Category, _ = rm["category"].(string)
			it.References = append(it.References, ref)
		}
	}
	return it
}

// Ref returns the reference of the item with the ID.
func (it HistoryItem) Ref(id string) (HistoryItemRef, bool) {
	for _, r := range it.References {
		if r.ID == id {
			return r, true
		}
	}
	return HistoryItemRef{}, false
}
