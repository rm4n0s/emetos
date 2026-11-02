package debug

import (
	"path/filepath"
	"sort"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/debug"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// LoadedPane is the Loaded Scripts view: the sources the focused session reports with loadedSources and loadedSource events, as a flat
// list of names with their folders dimmed after them (decision 1193; VS Code groups them in a tree of folders). It is shown while the
// adapter supports the request (loadedScriptsSupported) and loads nothing while it is folded.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/loadedScriptsView.ts
type LoadedPane struct {
	*base
	session *debug.Session
	sources []debug.Source
	gen     int
}

func newLoadedPane(opts Opts) *LoadedPane {
	l := &LoadedPane{}
	l.base = newBase(opts, "workbench.debug.loadedScriptsView", kit.TreeModelOpts{})
	return l
}

func (l *LoadedPane) refresh(ev debug.Event) {
	focus := l.opts.Service.Model().Focus()
	s := focus.Session
	if s == nil || !s.Capabilities().SupportsLoadedSourcesRequest {
		if l.session != nil || len(l.sources) > 0 {
			l.session, l.sources = nil, nil
			l.gen++
			l.show()
		}
		return
	}
	if s == l.session && ev.Kind != debug.EventLoadedSource {
		return
	}
	if !l.visible() {
		l.session = nil // read again when it is shown
		return
	}
	l.session = s
	l.gen++
	gen, ctx := l.gen, l.ctx
	go func() {
		sources, err := s.LoadedSources(ctx)
		if err != nil {
			return
		}
		l.opts.Post(func() {
			if gen == l.gen {
				l.sources = sources
				l.show()
			}
		})
	}()
}

func (l *LoadedPane) show() {
	l.rows = map[string]Row{}
	sorted := append([]debug.Source(nil), l.sources...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Key() < sorted[j].Key() })
	items := make([]kit.TreeItem, 0, len(sorted))
	used := keys{}
	for _, s := range sorted {
		key := "src:" + used.of(s.Key())
		row := Row{Kind: RowSource, Key: key, Source: s}
		l.rows[key] = row
		items = append(items, kit.TreeItem{Key: key, Data: row, Label: s.Name})
	}
	_ = l.model.SetItems(items)
	l.notify()
}

// Sources are the sources the view lists.
func (l *LoadedPane) Sources() []debug.Source { return l.sources }

// Render builds the contents of a row.
func (l *LoadedPane) Render(bc widgets.BuildContext, n kit.TreeNode, st kit.RowState) widgets.Widget {
	row, ok := n.Data.(Row)
	if !ok {
		return kit.NewHighlightedLabel(bc, n.Label, st)
	}
	base := kit.TextStyleOf(bc, rowFontSize, st.Foreground)
	dim := base.CopyWith(painting.TextStyleOpts{Color: foundation.Some(st.Foreground.WithOpacity(0.6))})
	spans := []painting.InlineSpan{painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(row.Source.Name), Style: base})}
	if row.Source.Path != "" {
		spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some("  " + filepath.Dir(row.Source.Path)), Style: dim}))
	}
	return widgets.NewRow([]widgets.Widget{
		widgets.NewIcon(fonts.CodiconIcon(fonts.CodiconFiles), widgets.IconOpts{Size: foundation.Some(16.0), Color: foundation.Some(st.Foreground)}),
		widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(4.0)}),
		widgets.NewFlexible(widgets.NewRichText(painting.NewTextSpan(painting.TextSpanOpts{Children: spans}),
			widgets.RichTextOpts{MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})),
	}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
}
