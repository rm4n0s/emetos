package extension

import (
	"context"
	"time"
)

// The Timeline API (SPEC §10.30 "The Timeline view", step 24.21; after vscode.workspace.registerTimelineProvider, TimelineProvider,
// TimelineItem and Timeline of vscode.proposed.timeline.d.ts and extHostTimeline.ts). An extension tells the workbench the history of
// a file, and the Timeline view of the Explorer lists it with the items of every provider, newest first.
//
// Everything here may be called from any goroutine, except where a comment says that Emetos calls the extension.

// TimelineItem is one entry of the history of a file (VS Code's TimelineItem).
type TimelineItem struct {
	// ID identifies the item among the items of its provider ("the handle"): a commit hash. The view merges the pages of one provider by
	// it, so an item that comes again in a later page replaces the one it has. It is not empty.
	ID string
	// Label is the text of the row ("Fix the loader").
	Label string
	// Description is dimmed text after the label ("Ada Example").
	Description string
	// Detail is the tooltip of the row, as Markdown.
	Detail string
	// Timestamp is the time of the item; the rows are ordered by it, newest first, and the view writes its age ("3 days") at the end of
	// the row.
	Timestamp time.Time
	// Icon is the name of a Codicon drawn before the label ("git-commit").
	Icon string
	// ContextValue is the value of the context key timelineItem for the menus of the item (extension.MenuTimelineItemContext):
	// "git:file:commit".
	ContextValue string
	// Command runs when the item is clicked or opened with Enter (VS Code's TimelineItem.command); nil for none.
	Command *Command
	// Args is the JSON-compatible value the commands of the item's menus get as the member "args" of their argument
	// (TimelineArgs). VS Code gives the commands the item object that its provider made, with every member the provider put on it;
	// here the provider says what its commands need to know about the item.
	Args any
}

// TimelineOptions say which items a provider is asked for (VS Code's TimelineOptions).
type TimelineOptions struct {
	// Cursor is the Paging.Cursor the provider gave with the previous page; empty asks for the first page.
	Cursor string
	// Limit is the most items the page may have; 0 asks for all of them (the view asks for the items it shows again, when the history
	// changes). A provider that has more says so with a cursor.
	Limit int
}

// TimelinePaging says where the next page starts (VS Code's Timeline.paging).
type TimelinePaging struct {
	// Cursor is what the provider gets back in TimelineOptions.Cursor for the page after this one; empty when there is none.
	Cursor string
}

// Timeline is a page of items (VS Code's Timeline).
type Timeline struct {
	// Items are the items of the page, in any order.
	Items []TimelineItem
	// Paging tells whether there is another page.
	Paging TimelinePaging
}

// TimelineProvider provides the history of files (VS Code's TimelineProvider).
type TimelineProvider struct {
	// ID is the source of the items: it names the provider in the view's filter ("git-history") and is unique in the window tab.
	ID string
	// Label is its name in the filter ("Git History").
	Label string
	// Schemes are the URI schemes the provider has a history for ("file"); "*" is every scheme. At least one is needed.
	Schemes []string
	// Provide makes a page of the history of the file at uri (file:///path). It runs off the UI thread, with a context that is
	// cancelled when the view no longer wants the answer. A file with no history is an empty page, not an error. A panic fails the
	// extension (ExtensionTimelinePanicked).
	Provide func(ctx context.Context, uri string, opts TimelineOptions) (Timeline, error)
}

// TimelineRegistration is a registered provider.
type TimelineRegistration interface {
	// Changed says that the history of the files may have changed (VS Code's onDidChange): the view loads the items again, as many as it
	// shows, when one of them is the file in front. With no URI every file is meant. It may be called from any goroutine.
	Changed(uris ...string)
	// Dispose removes the provider and its items from the view. It happens by itself when the activation ends.
	Dispose()
}

// Timelines registers the timeline providers of an extension.
type Timelines interface {
	// RegisterProvider adds a provider. It fails with ExtensionTimelineInvalid for a provider with no ID, no label, no scheme or no
	// Provide, ExtTimelineProviderTaken when the window tab has a provider with the ID, ExtensionTimelineUnavailable in a scope that
	// has no Timeline view (a host without a workbench) and ExtensionDeactivated after the activation ended.
	RegisterProvider(provider TimelineProvider) (TimelineRegistration, error)
}

// The members of an argument that a menu of the Timeline view gives a command (TimelineArgs).
const (
	// TimelineArgSource is the ID of the provider of the item.
	TimelineArgSource = "source"
	// TimelineArgID is the ID of the item.
	TimelineArgID = "id"
	// TimelineArgURI is the URI of the file whose history the view shows.
	TimelineArgURI = "uri"
	// TimelineArgContextValue is the ContextValue of the item.
	TimelineArgContextValue = "contextValue"
	// TimelineArgArgs is the Args of the item.
	TimelineArgArgs = "args"
	// TimelineArgLabel is the Label of the item.
	TimelineArgLabel = "label"
)

// TimelineArg is the item that a command of the Timeline view is run for. VS Code passes (item, uri, source, selection); Emetos gives
// the command one value, a list of objects {"source": "git-history", "id": "abc...", "uri": "file:///a.go", "contextValue": "...",
// "label": "...", "args": ...}, which TimelineArgs reads: the item first, then the other selected items when the view has several
// selected (this view selects one row).
type TimelineArg struct {
	// Source is the ID of the provider and ID that of the item.
	Source, ID string
	// URI is the file the timeline is for.
	URI string
	// ContextValue and Label are those of the item.
	ContextValue, Label string
	// Args is the Args of the item.
	Args any
}

// TimelineArgs reads the arguments a menu of the Timeline view (or the click on an item) gave a command: the objects of the list, in
// order; nothing for any other value (the command was run from the palette or by a key).
func TimelineArgs(args any) []TimelineArg {
	var list []any
	switch v := args.(type) {
	case []any:
		list = v
	case []map[string]any:
		for _, m := range v {
			list = append(list, m)
		}
	default:
		return nil
	}
	var out []TimelineArg
	for _, e := range list {
		m, ok := e.(map[string]any)
		if !ok {
			continue
		}
		var a TimelineArg
		a.Source, _ = m[TimelineArgSource].(string)
		a.ID, _ = m[TimelineArgID].(string)
		a.URI, _ = m[TimelineArgURI].(string)
		a.ContextValue, _ = m[TimelineArgContextValue].(string)
		a.Label, _ = m[TimelineArgLabel].(string)
		a.Args = m[TimelineArgArgs]
		if a.ID == "" && a.Source == "" {
			continue
		}
		out = append(out, a)
	}
	return out
}
