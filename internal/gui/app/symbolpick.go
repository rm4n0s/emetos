package app

import (
	"cmp"
	"context"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/emetos/internal/core/fuzzy"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/quickinput"
)

// The pickers of symbols (SPEC §9.12 "Go to Symbol", "As built (step 12.7)"): "@" lists the symbols of the active editor and "#"
// those of the workspace. "@:" (a colon first in the filter) groups the symbols of the editor by kind.
//
// Ported from VS Code: src/vs/workbench/contrib/codeEditor/browser/quickaccess/gotoSymbolQuickAccess.ts and
// src/vs/workbench/contrib/search/browser/symbolsQuickAccess.ts

const (
	// symbolPrefix and workspaceSymbolPrefix select the two pickers.
	symbolPrefix          = "@"
	workspaceSymbolPrefix = "#"
	// symbolScope is what a filter starts with to group the symbols by kind.
	symbolScope = ":"
	// workspaceSymbolDelay is how long the picker waits after a keystroke before it asks the servers.
	workspaceSymbolDelay = 200 * time.Millisecond
)

// symbolProviders are the providers of the two pickers.
func (s *shellState) symbolProviders() []quickinput.Provider {
	editorSymbols := &quickinput.ListProvider{
		ProviderInfo: quickinput.ProviderInfo{
			Prefix: symbolPrefix, Placeholder: "Go to symbol in editor...",
			Help: []quickinput.HelpEntry{
				{Description: "Go to Symbol in Editor", Command: "workbench.action.gotoSymbol"},
				{Prefix: symbolPrefix + symbolScope, Description: "Go to Symbol in Editor by Category", Command: "workbench.action.gotoSymbol"},
			},
		},
		Picks: s.editorSymbolPicks,
	}
	workspaceSymbols := &quickinput.ListProvider{
		ProviderInfo: quickinput.ProviderInfo{
			Prefix: workspaceSymbolPrefix, Placeholder: "Search workspace symbols by name",
			Help: []quickinput.HelpEntry{{Description: "Go to Symbol in Workspace", Command: "workbench.action.showAllSymbols"}},
		},
		NoResults:  "No matching workspace symbols",
		PicksAsync: s.workspaceSymbolPicks,
	}
	return []quickinput.Provider{&symbolProvider{ListProvider: editorSymbols, s: s}, workspaceSymbols}
}

// symbolProvider is the provider of "@": it asks for the symbols again when the editor gets them (they come after the picker
// opened), which the list provider cannot know.
type symbolProvider struct {
	*quickinput.ListProvider
	s *shellState
}

// Provide implements quickinput.Provider.
func (p *symbolProvider) Provide(sess *quickinput.Session) quickinput.Handler {
	h := &symbolHandler{Handler: p.ListProvider.Provide(sess)}
	if e := p.s.activeEditor(); e != nil {
		handle := e.view.AddSymbolsListener(func() { h.ValueChanged(h.last) })
		sess.OnEnd(func() { e.view.RemoveSymbolsListener(handle) })
	}
	return h
}

// symbolHandler remembers the filter the list was made for.
type symbolHandler struct {
	quickinput.Handler
	last string
}

// ValueChanged implements quickinput.Handler.
func (h *symbolHandler) ValueChanged(filter string) {
	h.last = filter
	h.Handler.ValueChanged(filter)
}

// editorSymbolPicks makes the items of "@" for the symbols of the active editor.
func (s *shellState) editorSymbolPicks(filter string) []quickinput.Item {
	e := s.activeEditor()
	if e == nil {
		return []quickinput.Item{{Label: "To go to a symbol, first open a text editor with symbol information."}}
	}
	syms, ok := e.view.Symbols()
	if !ok {
		return []quickinput.Item{{Label: "The active text editor does not provide symbol information."}}
	}
	items := symbolItems(syms, filter, func(sym *langfeatures.DocumentSymbol) { e.view.RevealSymbol(sym) })
	if len(items) == 0 {
		if filter == "" {
			return []quickinput.Item{{Label: "No symbols found in the editor."}}
		}
		return []quickinput.Item{{Label: "No matching editor symbols"}}
	}
	return items
}

// symbolItems makes the items of the symbols for a filter: the symbols in the order of the document with the ones the filter
// does not match left out and the best match first, or, for a filter that starts with the colon, grouped by kind with a heading
// for each. open goes to a symbol.
func symbolItems(syms []langfeatures.DocumentSymbol, filter string, open func(*langfeatures.DocumentSymbol)) []quickinput.Item {
	byKind := strings.HasPrefix(filter, symbolScope)
	filter = strings.TrimSpace(strings.TrimPrefix(filter, symbolScope))
	type pick struct {
		sym   *langfeatures.DocumentSymbol
		cont  string
		score int
		match []fuzzy.Match
	}
	var picks []pick
	for _, f := range langfeatures.FlattenSymbols(syms) {
		p := pick{sym: f.Symbol, cont: f.Container}
		if filter != "" {
			m, ok := fuzzy.MatchesFuzzy(filter, f.Symbol.Name, true)
			if !ok {
				continue
			}
			p.match = m
			p.score = fuzzy.ScoreFuzzy(f.Symbol.Name, filter, strings.ToLower(filter), true).Score
		}
		picks = append(picks, p)
	}
	switch {
	case byKind:
		slices.SortStableFunc(picks, func(a, b pick) int { return cmp.Compare(a.sym.Kind, b.sym.Kind) })
	case filter != "":
		slices.SortStableFunc(picks, func(a, b pick) int { return cmp.Compare(b.score, a.score) })
	}
	counts := map[int]int{}
	for _, p := range picks {
		counts[p.sym.Kind]++
	}
	var items []quickinput.Item
	last := -1
	for _, p := range picks {
		if byKind && p.sym.Kind != last {
			last = p.sym.Kind
			items = append(items, quickinput.Item{Separator: true, Label: langfeatures.SymbolKindLabel(last) + " (" + strconv.Itoa(counts[last]) + ")"})
		}
		sym := p.sym
		items = append(items, quickinput.Item{
			Label: sym.Name, Description: p.cont, Detail: "", Icon: editor.SymbolIconCode(sym.Kind),
			Highlights: quickinput.Highlights{Label: p.match}, Data: sym,
			Accept: func(quickinput.KeyMods) { open(sym) },
		})
	}
	return items
}

// workspaceSymbolPicks asks the servers for the symbols of the workspace, off the UI thread, a moment after the last keystroke.
func (s *shellState) workspaceSymbolPicks(ctx context.Context, filter string) []quickinput.Item {
	win := PeekWorkspace(s.Context())
	scope := win.Extensions()
	if scope == nil {
		return []quickinput.Item{{Label: "No language server provides symbols of the workspace."}}
	}
	filter = strings.TrimSpace(filter)
	if filter == "" {
		return nil
	}
	select {
	case <-time.After(workspaceSymbolDelay):
	case <-ctx.Done():
		return nil
	}
	reg := scope.Features()
	syms, errs := reg.WorkspaceSymbols(ctx, filter)
	if ctx.Err() != nil {
		return nil
	}
	for _, pe := range errs {
		win.Process.Logger.Debug("a server cannot give workspace symbols", "err", pe.Err)
	}
	items := workspaceSymbolItems(syms, filter, win.Folder, func(sym langfeatures.WorkspaceSymbol, side bool) {
		s.openWorkspaceSymbol(reg, sym, side)
	})
	return items
}

// workspaceSymbolItems makes the items of "#" for the symbols the servers answered, in the order of their score for the filter.
func workspaceSymbolItems(syms []langfeatures.WorkspaceSymbol, filter, folder string, open func(langfeatures.WorkspaceSymbol, bool)) []quickinput.Item {
	type pick struct {
		sym   langfeatures.WorkspaceSymbol
		score int
		match []fuzzy.Match
	}
	picks := make([]pick, 0, len(syms))
	for _, sym := range syms {
		p := pick{sym: sym}
		if m, ok := fuzzy.MatchesFuzzy(filter, sym.Name, true); ok {
			p.match = m
			p.score = fuzzy.ScoreFuzzy(sym.Name, filter, strings.ToLower(filter), true).Score
		}
		picks = append(picks, p)
	}
	slices.SortStableFunc(picks, func(a, b pick) int { return cmp.Compare(b.score, a.score) })
	items := make([]quickinput.Item, 0, len(picks))
	for _, p := range picks {
		sym := p.sym
		items = append(items, quickinput.Item{
			Label: sym.Name, Description: sym.ContainerName, Detail: relativeTo(folder, sym.Location.URI.Path),
			Icon: editor.SymbolIconCode(sym.Kind), Highlights: quickinput.Highlights{Label: p.match}, Data: sym,
			Accept: func(m quickinput.KeyMods) { open(sym, m.Ctrl) },
		})
	}
	return items
}

// relativeTo is a path relative to a folder when it is in it, else the path.
func relativeTo(folder, path string) string {
	if folder != "" {
		if rel, err := filepath.Rel(folder, path); err == nil && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	return path
}

// openWorkspaceSymbol goes to a symbol of the workspace; a symbol the server left unresolved (no range yet) is resolved first,
// on a goroutine.
func (s *shellState) openWorkspaceSymbol(reg *langfeatures.Registry, sym langfeatures.WorkspaceSymbol, side bool) {
	win := PeekWorkspace(s.Context())
	show := func(sym langfeatures.WorkspaceSymbol) {
		s.navOpen(sym.Location, editor.OpenLocation{Side: side})
	}
	if !sym.Unresolved || !reg.CanResolveWorkspaceSymbol(sym) {
		show(sym)
		return
	}
	s.Go(func(life context.Context) {
		resolved, err := reg.ResolveWorkspaceSymbol(life, sym)
		win.onUI(func() {
			if !s.Mounted() {
				return
			}
			if err != nil {
				win.Notify.Error("Emetos", describe(err))
				return
			}
			show(resolved)
		})
	})
}
