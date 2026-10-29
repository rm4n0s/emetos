package containerui

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/internal/pageui"
)

// The documents are the read-only tabs of the editor area: the inspect of a container, an image, a network, a volume or a context,
// the history of an image and a file of a container. They are one kind of editor tab; a tab saves its text, so that it is still
// there after a restart (a tab shows what the engine said when it was opened, it does not follow it).

// maxHighlighted is the most bytes of a document that are highlighted; a longer one is shown plain.
const maxHighlighted = 256 << 10

// maxSavedText is the most bytes of a document that its tab keeps for the next session.
const maxSavedText = 512 << 10

// docState is what a document tab is made from and what it keeps.
type docState struct {
	// Title is the name on the tab, Hint its tooltip.
	Title string `json:"title"`
	Hint  string `json:"hint,omitempty"`
	// Text is the text, Language the name or the id of its language ("json"; empty for plain text).
	Text     string `json:"text"`
	Language string `json:"language,omitempty"`
}

// documents makes the tabs.
type documents struct{ e *ext }

func newDocuments(e *ext) *documents { return &documents{e: e} }

// provide sets how the tabs are made.
func (d *documents) provide() error {
	return d.e.api.Editors().Provide(d.e.cfg.DocumentKind(), extension.EditorKind{Create: d.create})
}

// open shows a text in a new tab.
func (d *documents) open(title, text, language string) { d.openLanguage(title, "", text, language) }

// openLanguage shows a text in a new tab with a tooltip.
func (d *documents) openLanguage(title, hint, text, language string) {
	_, err := d.e.api.Editors().Open(d.e.cfg.DocumentKind(), docState{Title: title, Hint: hint, Text: text, Language: language}, extension.EditorOpenOptions{})
	if err != nil {
		d.e.api.Notifications().Error("Cannot open " + title + ": " + err.Error())
	}
}

// docView is the content of one tab.
type docView struct {
	e      *ext
	state  docState
	tab    extension.EditorTab
	lines  []string
	tokens [][]extension.Token
	model  *ui.ListViewModel
	ctl    *ui.ListController
}

// create makes the content of a tab from its state.
func (d *documents) create(tab extension.EditorTab, raw json.RawMessage) (extension.EditorTabContent, error) {
	var st docState
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &st); err != nil {
			return extension.EditorTabContent{}, errors.NewErr("ContainerUIDocumentStateInvalid", err)
		}
	}
	tab.SetTitle(st.Title)
	tab.SetHint(st.Hint)
	v := &docView{e: d.e, state: st, tab: tab, ctl: ui.NewListController()}
	v.lines = strings.Split(strings.TrimSuffix(st.Text, "\n"), "\n")
	v.model = ui.NewListViewModel(ui.ListViewModelOpts{RowHeight: foundation.Some(18.0)})
	items := make([]ui.ListViewItem, len(v.lines))
	for i := range items {
		items[i] = ui.ListViewItem{Key: "l" + strconv.Itoa(i), Data: i}
	}
	if err := v.model.SetItems(items); err != nil {
		return extension.EditorTabContent{}, err
	}
	v.highlight()
	return extension.EditorTabContent{Build: v.build, SaveState: v.save}, nil
}

// highlight tokenizes the text for its language; a text that cannot be highlighted stays plain.
func (v *docView) highlight() {
	if v.state.Language == "" || len(v.state.Text) > maxHighlighted {
		return
	}
	langs := v.e.api.Languages()
	id, ok := langs.Resolve(v.state.Language)
	if !ok {
		return
	}
	toks, err := langs.Tokenize(id, strings.TrimSuffix(v.state.Text, "\n"))
	if err != nil {
		v.e.api.Log().Debug("a document cannot be highlighted", "language", id, "err", err)
		return
	}
	v.tokens = toks
}

// save keeps the state of the tab (without a text that is too long).
func (v *docView) save() (any, error) {
	st := v.state
	if len(st.Text) > maxSavedText {
		st.Text = st.Text[:maxSavedText] + "\n[The rest is not kept after a restart.]\n"
	}
	return st, nil
}

// build makes the widget of the tab: the title with Copy, and the lines.
func (v *docView) build(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	look := ui.TokenLooker(bc)
	mono := ui.TextStyle(bc, 12, colors.EditorForeground).CopyWith(painting.TextStyleOpts{FontFamily: pageui.MonoFamily(bc)})
	head := widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(12, 6, 12, 6), widgets.NewRow([]widgets.Widget{
		widgets.NewExpanded(widgets.NewText(v.state.Title+descriptionOf(v.state.Hint), widgets.TextOpts{
			Style: ui.TextStyle(bc, 13, colors.DescriptionForeground), MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis,
		})),
		ui.NewButton("Copy All", ui.ButtonOpts{Small: true, Secondary: true, OnPressed: func() { v.e.copyText(v.state.Text) }}),
	}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}))
	list := ui.NewList(v.model, ui.ListOpts{Controller: v.ctl, Render: func(bc widgets.BuildContext, row ui.ListViewRow, st ui.RowState) widgets.Widget {
		i, _ := row.Data.(int)
		return widgets.NewRichText(v.span(i, mono, look, st.Foreground))
	}})
	return widgets.NewColoredBox(colors.EditorBackground, widgets.NewColumn([]widgets.Widget{head, widgets.NewExpanded(list)},
		widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}))
}

// descriptionOf is the hint after the title.
func descriptionOf(hint string) string {
	if hint == "" {
		return ""
	}
	return "   " + hint
}

// span is the text of a line, in the colors of its tokens.
func (v *docView) span(i int, style *painting.TextStyle, look func([]string, gui.Color) ui.TokenLook, fg gui.Color) painting.InlineSpan {
	if i < 0 || i >= len(v.lines) {
		return painting.NewTextSpan(painting.TextSpanOpts{})
	}
	line := v.lines[i]
	if i >= len(v.tokens) || len(v.tokens[i]) == 0 {
		return painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(line), Style: style})
	}
	var spans []painting.InlineSpan
	at := 0
	for _, t := range v.tokens[i] {
		start, end := min(max(t.Start, at), len(line)), min(t.End, len(line))
		if start > at {
			spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(line[at:start])}))
		}
		if end > start {
			l := look(t.Scopes, fg)
			ts := painting.TextStyleOpts{Color: foundation.Some(l.Color)}
			if l.Bold {
				ts.FontWeight = gui.FontWeightBold
			}
			if l.Italic {
				ts.FontStyle = gui.FontStyleItalic
			}
			spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(line[start:end]), Style: style.CopyWith(ts)}))
		}
		at = max(at, end)
	}
	if at < len(line) {
		spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(line[at:])}))
	}
	return painting.NewTextSpan(painting.TextSpanOpts{Style: style, Children: spans})
}
