package redis

import (
	"sort"
	"strconv"
	"strings"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/internal/pageui"
)

// maxSuggestions is how many command names the console offers under the input.
const maxSuggestions = 8

// consoleFontSize is the size of the text of the console.
const consoleFontSize = 12.5

// refreshConsole builds the console tab again.
func (x *Extension) refreshConsole() {
	x.refreshPage(ConsoleViewID)
}

// buildConsole makes the widget of the Redis Console tab.
func (x *Extension) buildConsole(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	conn := x.chooseTarget()
	if conn == "" {
		return widgets.NewColoredBox(colors.PanelBackground, x.noConnection(bc, colors))
	}
	c := x.consoleFor(conn)
	x.ensureCommandDocs(conn)
	bar := tabBar(
		x.connPicker(conn, func(id string) { x.setTarget(id); x.refreshConsole() }), pageui.HGap(10),
		muted(bc, colors, "database "+itoa(c.db)), widgets.NewSpacer(),
		small("Cancel", true, !c.busy(), c.cancel), pageui.HGap(6),
		small("Copy", true, len(c.entries) == 0, c.copyAll), pageui.HGap(6),
		small("Clear", true, len(c.entries) == 0, c.clear),
	)
	body := widgets.NewColumn([]widgets.Widget{
		bar, hline(colors), widgets.NewExpanded(c.output(bc, colors)), hline(colors), c.suggestionRow(bc, colors), c.inputRow(bc, colors),
	}, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
	return widgets.NewColoredBox(colors.PanelBackground, body)
}

// output is the list of entries, newest at the bottom: a list that starts at the bottom, so a new entry shows without scrolling.
func (c *console) output(bc widgets.BuildContext, colors ui.Colors) widgets.Widget {
	if len(c.entries) == 0 {
		return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(12, 10, 12, 0), widgets.NewAlign(
			widgets.NewText("Type a command below, for example PING or INFO server. Quoting works as in redis-cli.",
				widgets.TextOpts{Style: ui.TextStyle(bc, 12, colors.DescriptionForeground)}), widgets.AlignOpts{Alignment: painting.AlignmentTopLeft}))
	}
	mono := pageui.MonoFamily(bc)
	n := len(c.entries)
	item := func(_ widgets.BuildContext, i int) widgets.Widget {
		if i < 0 || i >= n {
			return nil
		}
		return c.entryWidget(bc, colors, mono, c.entries[n-1-i])
	}
	sliver := widgets.NewSliverListBuilder(item, widgets.SliverBuilderOpts{
		ItemCount: foundation.Some(n), AddAutomaticKeepAlives: foundation.Some(false), AddRepaintBoundaries: foundation.Some(false),
		AddSemanticIndexes: foundation.Some(false),
	})
	view := widgets.NewCustomScrollView([]widgets.Widget{sliver}, widgets.ScrollViewOpts{Controller: c.scroll(), Reverse: true})
	return ui.NewScrollable(view, ui.ScrollableOpts{Controller: c.scroll()})
}

// scroll is the controller of the output list.
func (c *console) scroll() widgets.ScrollController {
	if c.scrollCtl == nil {
		c.scrollCtl = widgets.NewScrollController()
	}
	return c.scrollCtl
}

// entryWidget is one entry: a command with its prompt dimmed, a reply, an error in red, a note.
func (c *console) entryWidget(bc widgets.BuildContext, colors ui.Colors, mono string, e entry) widgets.Widget {
	style := ui.TextStyle(bc, consoleFontSize, colors.Foreground).CopyWith(painting.TextStyleOpts{FontFamily: mono})
	var text widgets.Widget
	switch e.kind {
	case entryCommand:
		prompt, cmd := splitPrompt(e.text)
		dim := style.CopyWith(painting.TextStyleOpts{Color: foundation.Some(colors.DescriptionForeground)})
		bold := pageui.Bold(style)
		text = widgets.NewRichText(painting.NewTextSpan(painting.TextSpanOpts{Children: []painting.InlineSpan{
			painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(prompt), Style: dim}),
			painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(cmd), Style: bold}),
		}}))
	case entryError:
		text = widgets.NewText(e.text, widgets.TextOpts{Style: style.CopyWith(painting.TextStyleOpts{Color: foundation.Some(colors.ErrorForeground)})})
	case entryNote:
		text = widgets.NewText(e.text, widgets.TextOpts{Style: style.CopyWith(painting.TextStyleOpts{
			Color: foundation.Some(colors.DescriptionForeground), FontStyle: gui.FontStyleItalic})})
	default:
		text = widgets.NewText(e.text, widgets.TextOpts{Style: style})
	}
	return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(12, 1, 12, 1), text)
}

// splitPrompt splits "db0> GET key" into the prompt and the command.
func splitPrompt(s string) (prompt, cmd string) {
	if i := strings.Index(s, "> "); i >= 0 {
		return s[:i+2], s[i+2:]
	}
	return "", s
}

// itoa writes a number.
func itoa(n int) string { return strconv.Itoa(n) }

// suggestions are the command names that start with the word being typed, while the first word is.
func (c *console) suggestions() []string {
	text := c.input.Value()
	if text == "" || strings.ContainsAny(text, " \t") {
		return nil
	}
	prefix := strings.ToUpper(text)
	var out []string
	for _, name := range c.x.commandNames(c.conn) {
		if strings.HasPrefix(name, prefix) && name != prefix {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	if len(out) > maxSuggestions {
		out = out[:maxSuggestions]
	}
	return out
}

// suggestionRow is the row of command names under the output; a press completes the word.
func (c *console) suggestionRow(bc widgets.BuildContext, colors ui.Colors) widgets.Widget {
	names := c.suggestions()
	if len(names) == 0 {
		return pageui.Nothing()
	}
	var chips []widgets.Widget
	for _, name := range names {
		chips = append(chips, small(name, true, false, func() {
			c.input.SetValue(name + " ")
			c.x.refreshConsole()
		}), pageui.HGap(4))
	}
	return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(12, 6, 12, 0), widgets.NewRow(chips, widgets.RowOpts{}))
}

// inputRow is the prompt, the input box and the Run button.
func (c *console) inputRow(bc widgets.BuildContext, colors ui.Colors) widgets.Widget {
	mono := pageui.MonoFamily(bc)
	prompt := widgets.NewText(c.prompt(), widgets.TextOpts{Style: ui.TextStyle(bc, consoleFontSize, colors.DescriptionForeground).CopyWith(
		painting.TextStyleOpts{FontFamily: mono})})
	input := ui.NewInputBox(nil, nil, ui.InputBoxOpts{
		Controller: c.input, History: c.hist, Monospace: true, Placeholder: "Redis command", Autofocus: false,
		OnSubmit: c.submit, OnChanged: func(string) { c.x.refreshConsole() },
	})
	return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(12, 6, 12, 8), widgets.NewRow([]widgets.Widget{
		prompt, pageui.HGap(6), widgets.NewExpanded(input), pageui.HGap(8),
		small("Run", false, c.busy() && !c.run.waiting, func() { c.submit(c.input.Value()) }),
	}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}))
}

// copyAll puts the whole output on the clipboard.
func (c *console) copyAll() {
	var b strings.Builder
	for _, e := range c.entries {
		b.WriteString(e.text)
		b.WriteByte('\n')
	}
	c.x.copyText(b.String())
}

// copyLast puts the last reply on the clipboard.
func (c *console) copyLast() {
	for i := len(c.entries) - 1; i >= 0; i-- {
		if k := c.entries[i].kind; k == entryReply || k == entryError {
			c.x.copyText(c.entries[i].text)
			return
		}
	}
}
