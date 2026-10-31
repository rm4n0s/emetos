package app

import (
	"context"
	"strconv"
	"sync"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/settings"
)

// What the window gives an extension for the multi-file diff editor (SPEC §10.30 "Diffs of changes", step 24.17): the registry of the
// providers of the sets of files, and the opening of an editor of a set. The pane (multidiffpane.go) asks the provider for the files
// off the UI thread, and a saved editor of a set comes back through the same registry once the extension has started: the saved tab
// holds only the kind and the key, and the provider is asked when it registers.
//
// Ported from VS Code: src/vs/workbench/contrib/multiDiffEditor/browser/multiDiffSourceResolverService.ts

// multiDiffs are the providers of the multi-file diff editor of a window tab, by kind. The mutex guards the map: an extension
// registers from its own goroutine as the window reads it.
type multiDiffs struct {
	mu     sync.Mutex
	byKind map[string]*multiDiffReg
	// fixed counts the fixed sets, for their tab keys.
	fixed int
}

// multiDiffReg is the extension.MultiDiffRegistration of a provider of the window tab.
type multiDiffReg struct {
	w      *Workspace
	owner  string
	p      extension.MultiDiffProvider
	ctx    context.Context
	cancel context.CancelFunc
	once   sync.Once
}

// multiDiffProvider is the provider of a kind, nil when the window tab has none.
func (w *Workspace) multiDiffProvider(kind string) *multiDiffReg {
	w.multi.mu.Lock()
	defer w.multi.mu.Unlock()
	return w.multi.byKind[kind]
}

// ProvideMultiDiff implements exthost.ContentHost: the provider is added to the registry of the window tab, and the saved editors of its
// kind that wait for it ask it for their files. It fails with the tag ExtMultiDiffKindTaken when the window tab has the kind.
func (d extensionDocuments) ProvideMultiDiff(owner string, p extension.MultiDiffProvider) (extension.MultiDiffRegistration, error) {
	w := d.w
	w.multi.mu.Lock()
	if w.multi.byKind[p.Kind] != nil {
		w.multi.mu.Unlock()
		return nil, errors.New("ExtMultiDiffKindTaken", "the window tab has a multi-file diff provider for this kind already", "kind", p.Kind, "extension", owner)
	}
	if w.multi.byKind == nil {
		w.multi.byKind = map[string]*multiDiffReg{}
	}
	ctx, cancel := context.WithCancel(w.ctx)
	reg := &multiDiffReg{w: w, owner: owner, p: p, ctx: ctx, cancel: cancel}
	w.multi.byKind[p.Kind] = reg
	w.multi.mu.Unlock()
	w.onUI(func() {
		if s := w.shell; s != nil && s.Mounted() && ctx.Err() == nil {
			s.multiDiffsChanged(p.Kind, nil)
		}
	})
	return reg, nil
}

// Changed implements extension.MultiDiffRegistration: the open editors of these keys ask the provider again.
func (r *multiDiffReg) Changed(keys ...string) {
	if len(keys) == 0 {
		return
	}
	w := r.w
	w.onUI(func() {
		if s := w.shell; s != nil && s.Mounted() && r.ctx.Err() == nil {
			s.multiDiffsChanged(r.p.Kind, keys)
		}
	})
}

// Dispose implements extension.MultiDiffRegistration.
func (r *multiDiffReg) Dispose() {
	r.once.Do(func() {
		r.cancel()
		r.w.multi.mu.Lock()
		if r.w.multi.byKind[r.p.Kind] == r {
			delete(r.w.multi.byKind, r.p.Kind)
		}
		r.w.multi.mu.Unlock()
	})
}

// OpenMultiDiff implements exthost.ContentHost: the editor opens at once, in the active group, and fills in when the provider has made
// the files; a set that is open is shown again and asked for again.
func (d extensionDocuments) OpenMultiDiff(spec extension.MultiDiffSpec) error {
	s, err := d.contentShell()
	if err != nil {
		return err
	}
	w := d.w
	var input *editorgroup.MultiDiffInput
	if spec.Kind != "" {
		if w.multiDiffProvider(spec.Kind) == nil {
			return errors.New("ExtMultiDiffKindUnknown", "no provider makes the sets of this kind", "kind", spec.Kind)
		}
		input = editorgroup.NewMultiDiffInput(spec.Kind, spec.Key, spec.Title)
	} else {
		items := make([]editorgroup.MultiDiffItem, len(spec.Resources))
		for i, r := range spec.Resources {
			items[i] = multiDiffItem(r)
		}
		w.multi.mu.Lock()
		w.multi.fixed++
		id := strconv.Itoa(w.multi.fixed)
		w.multi.mu.Unlock()
		input = editorgroup.NewFixedMultiDiffInput(id, spec.Title, items)
	}
	s.openMultiDiff(input, spec)
	return nil
}

// multiDiffItem is the item of the input for a file of the contract.
func multiDiffItem(r extension.MultiDiffResource) editorgroup.MultiDiffItem {
	return editorgroup.MultiDiffItem{
		Original: r.Original, Modified: r.Modified, Path: r.Path, File: r.File,
		Letter: r.Letter, Color: r.Color, Tooltip: r.Tooltip, Context: r.Context, Args: r.Args,
	}
}

// openMultiDiff shows the input in the active group, kept or as a preview, and makes it the active editor; an input of the same set
// that the group shows is activated in its place, and asked for its files again.
func (s *shellState) openMultiDiff(input *editorgroup.MultiDiffInput, spec extension.MultiDiffSpec) {
	win := PeekWorkspace(s.Context())
	group := s.activeGroup()
	shown := input
	preview := spec.Preview && settings.Get[bool](win.Settings, "workbench.editor.enablePreview", "")
	s.SetState(func() {
		if spec.PreserveFocus {
			s.holdFocus(widgets.WidgetsBindingOf(s.Context()))
		}
		s.nav.hold++
		opened, isNew := group.Model.OpenEditor(input, editorgroup.OpenOptions{Pinned: !preview, Active: true})
		s.nav.hold--
		if !isNew {
			if m, ok := opened.(*editorgroup.MultiDiffInput); ok {
				shown = m
			}
			input.Dispose()
		}
		s.syncNav()
		if err := s.group.grid.Activate(group); err != nil {
			win.Process.Logger.Error("cannot activate the group", "err", err)
		}
		s.ensurePane(shown)
		if p, ok := s.group.panes[shown].(*multiDiffPane); ok {
			p.revealFile(spec.Reveal)
			if !isNew {
				p.load()
			}
		}
	})
	s.refreshKeys()
}

// multiDiffsChanged tells the panes of the editors of a kind that its provider is there (keys nil: the ones that wait for it ask) or that
// the files of the sets with these keys may have changed (they ask again).
func (s *shellState) multiDiffsChanged(kind string, keys []string) {
	for _, p := range s.multiDiffPanes() {
		in := p.input
		if in.Kind() != kind {
			continue
		}
		if keys == nil {
			if !in.Loaded() {
				p.load()
			}
			continue
		}
		for _, k := range keys {
			if k == in.Key() {
				p.load()
				break
			}
		}
	}
}

// multiDiffPanes are the multi-file diff editors the window has.
func (s *shellState) multiDiffPanes() []*multiDiffPane {
	var out []*multiDiffPane
	for _, p := range s.group.panes {
		if m, ok := p.(*multiDiffPane); ok {
			out = append(out, m)
		}
	}
	return out
}
