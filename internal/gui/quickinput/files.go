package quickinput

import (
	"context"
	"sync/atomic"

	"github.com/rm4n0s/emetos/internal/core/theme"
	"github.com/rm4n0s/emetos/internal/core/workspace"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// Ported from VS Code: src/vs/workbench/contrib/search/browser/anythingQuickAccess.ts
// (AnythingQuickAccessProvider: the file part), src/vs/workbench/contrib/search/browser/searchQuickAccess.contribution.ts
// and src/vs/workbench/browser/quickaccess.ts (defaultQuickAccessContextKeyValue)

// The labels of the file mode.
const (
	// FilesPlaceholder is the hint of the input in the file mode. VS Code's text goes on with "to go to
	// symbol), or paste a link", which the file mode does not have.
	FilesPlaceholder = "Search files by name (append : to go to line)"
	// FilesNoResults is the row shown when nothing matches.
	FilesNoResults = "No matching results"
	// FilesRecentSeparator and FilesResultsSeparator are the headings of the two groups of rows.
	FilesRecentSeparator  = "recently opened"
	FilesResultsSeparator = "file results"
	// FilesContextKey is the context key that is true while the file mode is shown (VS Code's
	// defaultQuickAccessContextKeyValue).
	FilesContextKey = "inFilesPicker"
	// QuickOpenCommand is the command that opens the quick input, in the file mode by default.
	QuickOpenCommand = "workbench.action.quickOpen"
)

// FileOpen asks the window to open a file chosen in the file mode.
type FileOpen struct {
	// Path is the absolute path of the file.
	Path string
	// Side opens the file in the group beside the active one.
	Side bool
	// Pinned opens it as a kept editor, not a preview.
	Pinned bool
	// Line and Column are the position to reveal, counted from 1; 0 means none.
	Line, Column int
}

// FilesOptions connect the file mode to the window.
type FilesOptions struct {
	// Search finds the files for a filter (the text typed without its position suffix). It runs on a
	// goroutine of its own and must return when ctx is done. A search for an empty filter lists the
	// recently opened files.
	Search func(ctx context.Context, filter string) (workspace.QuickOpenResult, error)
	// OnChange, when set, calls f when the list of files changed (the walk found more, the watcher
	// reported a change); f may be called on any goroutine and does not block. The picker then searches
	// again, so the rows grow while the folder is still being listed.
	OnChange func(f func()) (stop func())
	// Open opens a file; it is called on the UI thread, after the picker closed and the focus is back.
	Open func(FileOpen)
	// Icon gives the icon of a file for its path, its name and the type of the color theme (false: none).
	// It is called on the UI thread when a row is built.
	Icon func(path, name string, typ theme.Type) (theme.FileIcon, bool)
	// OpenPinned tells whether an editor chosen with Enter is kept: it is unless
	// workbench.editor.enablePreviewFromQuickOpen and workbench.editor.enablePreview are both on. It
	// also decides what Ctrl does: open to the side when it is true, keep the editor when it is false.
	OpenPinned func() bool
	// SideDirection is "right" or "down" (workbench.editor.openSideBySideDirection), which chooses the
	// icon and the tooltip of the button that opens to the side.
	SideDirection func() string
	// RemoveRecent forgets a recently opened file.
	RemoveRecent func(path string)
	// OnError is told of a search that failed for another reason than a newer keystroke.
	OnError func(err error)
}

// NewFilesProvider makes the provider of the file mode: the default mode of the quick input, with the
// empty prefix. It shows the recently opened files first and the files of the folder that match, scored
// on a goroutine for each keystroke (the newest one wins), and a position after the name
// ("main.go:12:5") is revealed in the file that is opened.
func NewFilesProvider(opts FilesOptions) Provider { return &filesProvider{opts: opts} }

type filesProvider struct{ opts FilesOptions }

// Info implements Provider.
func (p *filesProvider) Info() ProviderInfo {
	return ProviderInfo{
		Prefix: "", Placeholder: FilesPlaceholder, ContextKey: FilesContextKey,
		Help: []HelpEntry{{Description: "Go to File", Command: QuickOpenCommand}},
	}
}

// Provide implements Provider.
func (p *filesProvider) Provide(s *Session) Handler {
	pk := s.Picker()
	pk.MatchOnLabel, pk.MatchOnDescription, pk.MatchOnDetail, pk.SortByLabel = false, false, false, false
	h := &filesHandler{p: p, s: s}
	if p.opts.OnChange != nil {
		stop := p.opts.OnChange(h.changed)
		s.OnEnd(stop)
	}
	return h
}

type filesHandler struct {
	BaseHandler
	p *filesProvider
	s *Session

	cancel context.CancelFunc
	seq    int
	// started says that a search ran; lastOriginal and pattern are the filter of the last keystroke and
	// the part of it that names files.
	started      bool
	lastOriginal string
	pattern      string
	// rng is the position the filter asks for.
	rng *workspace.FilterRange
	// refreshing says that a refresh is waiting for the UI thread.
	refreshing atomic.Bool
}

// ValueChanged implements Handler.
func (h *filesHandler) ValueChanged(filter string) {
	pattern, rng, ok := workspace.ExtractRangeFromFilter(filter, "@")
	if !ok {
		pattern, h.rng = filter, nil
	} else {
		h.rng = &rng
	}
	// The text after the name changed only: the results stay, the position is read when one is chosen.
	if h.started && filter != h.lastOriginal && pattern == h.pattern {
		h.lastOriginal = filter
		return
	}
	h.started, h.lastOriginal, h.pattern = true, filter, pattern
	h.search()
}

// changed is told, on any goroutine, that the files changed.
func (h *filesHandler) changed() {
	if !h.refreshing.CompareAndSwap(false, true) {
		return
	}
	h.s.Post(func() {
		h.refreshing.Store(false)
		if h.started {
			h.search()
		}
	})
}

// search starts the search of the current pattern and drops the one before.
func (h *filesHandler) search() {
	if h.cancel != nil {
		h.cancel()
	}
	ctx, cancel := context.WithCancel(h.s.Context())
	h.cancel = cancel
	h.seq++
	seq, pattern := h.seq, h.pattern
	go func() {
		res, err := h.p.opts.Search(ctx, pattern)
		if ctx.Err() != nil {
			return
		}
		var items []Item
		if err != nil {
			if h.p.opts.OnError != nil {
				h.s.Post(func() { h.p.opts.OnError(err) })
			}
			return
		}
		items = h.items(res, pattern)
		h.s.Post(func() {
			if ctx.Err() == nil && seq == h.seq {
				h.s.Picker().SetItems(items)
			}
		})
	}()
}

// items makes the rows of a result: the group of recently opened files, the group of the files of the
// folder, or the row that says that nothing matches.
func (h *filesHandler) items(res workspace.QuickOpenResult, pattern string) []Item {
	var items []Item
	group := func(heading string, hits []workspace.QuickOpenHit, recent bool) {
		if len(hits) == 0 {
			return
		}
		items = append(items, Item{Separator: true, Label: heading})
		for _, hit := range hits {
			items = append(items, h.item(hit, recent))
		}
	}
	group(FilesRecentSeparator, res.Recent, true)
	group(FilesResultsSeparator, res.Files, false)
	if len(items) == 0 && pattern != "" {
		it := Item{Label: FilesNoResults, AlwaysShow: true}
		if !res.Complete {
			it.Description = "Still looking for files…"
		}
		items = append(items, it)
	}
	return items
}

// item makes the row of a file.
func (h *filesHandler) item(hit workspace.QuickOpenHit, recent bool) Item {
	o := h.p.opts
	it := Item{
		ID: hit.Path, Label: hit.Name, Description: hit.Description,
		Highlights: Highlights{Label: hit.LabelMatch, Description: hit.DescriptionMatch},
		Data:       hit.Path,
	}
	if o.Icon != nil {
		it.FileIcon = func(typ theme.Type) (theme.FileIcon, bool) { return o.Icon(hit.Path, hit.Name, typ) }
	}
	side := Button{Icon: fonts.CodiconSplitHorizontal, Tooltip: "Open to the Side"}
	if o.SideDirection != nil && o.SideDirection() == "down" {
		side = Button{Icon: fonts.CodiconSplitVertical, Tooltip: "Open to the Bottom"}
	}
	it.Buttons = []Button{side}
	if recent {
		it.Buttons = append(it.Buttons, Button{Icon: fonts.CodiconClose, Tooltip: "Remove from Recently Opened"})
	}
	path := hit.Path
	it.Accept = func(mods KeyMods) { h.open(path, mods, false) }
	it.Trigger = func(button int, mods KeyMods) TriggerAction {
		switch {
		case button == 0:
			// the picker closes first and the file opens once the focus is back
			h.s.After(func() { h.open(path, mods, true) })
			return TriggerClose
		case recent && button == 1:
			if o.RemoveRecent != nil {
				o.RemoveRecent(path)
			}
			return TriggerRemove
		}
		return TriggerNone
	}
	return it
}

// open asks the window to open a file the way VS Code's openAnything does: with Alt, or with Ctrl
// while editors are kept by default, the file opens to the side; with Ctrl, or when editors are kept by
// default, the editor is kept.
func (h *filesHandler) open(path string, mods KeyMods, forceSide bool) {
	o := h.p.opts
	if o.Open == nil {
		return
	}
	pinnedByDefault := o.OpenPinned == nil || o.OpenPinned()
	req := FileOpen{
		Path:   path,
		Side:   mods.Alt || (pinnedByDefault && mods.Ctrl) || forceSide,
		Pinned: mods.Ctrl || pinnedByDefault,
	}
	if h.rng != nil {
		req.Line, req.Column = h.rng.StartLine, h.rng.StartColumn
	}
	o.Open(req)
}

// Done implements Handler.
func (h *filesHandler) Done(bool) {
	if h.cancel != nil {
		h.cancel()
	}
}

// Accept implements Handler: the picker hides, then the item runs.
func (h *filesHandler) Accept(row Row, mods KeyMods) { h.s.c.AcceptRow(row, mods) }
