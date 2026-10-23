package app

import (
	"strconv"
	"strings"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// placeholderMaxLines is how many lines the placeholder editor shows. Step 3.2 replaces it
// with the real editor, which draws only the lines in view.
const placeholderMaxLines = 2000

// editorPlaceholder shows the lines of a document as plain text in the editor font, with
// line numbers. It stands in for the editor widget until step 3.2.
type editorPlaceholder struct {
	widgets.StatelessWidget
	doc *document.Document
}

func newEditorPlaceholder(doc *document.Document) *editorPlaceholder {
	return &editorPlaceholder{StatelessWidget: widgets.NewStatelessWidgetBase(), doc: doc}
}

// Build implements widgets.StatelessWidgetInterface.
func (p *editorPlaceholder) Build(bc widgets.BuildContext) widgets.Widget {
	win := WindowOf(bc)
	colors := kit.ThemeOf(bc)
	text := editorTextStyle(bc, win.Settings, colors.EditorForeground)
	numbers := editorTextStyle(bc, win.Settings, colors.EditorLineNumberForeground)

	lines := strings.Split(p.doc.Text(), "\n")
	if len(lines) > placeholderMaxLines {
		lines = lines[:placeholderMaxLines]
	}
	digits := len(strconv.Itoa(len(lines)))
	rows := make([]widgets.Widget, len(lines))
	for i, line := range lines {
		number := strings.Repeat(" ", digits-len(strconv.Itoa(i+1))) + strconv.Itoa(i+1)
		// A tab has no glyph in most fonts; the real editor lays tabs out itself (step 3.2).
		line = strings.ReplaceAll(line, "\t", "    ")
		if line == "" {
			line = " " // an empty line keeps its height
		}
		rows[i] = widgets.NewRow([]widgets.Widget{
			widgets.NewText(number, widgets.TextOpts{Style: numbers, SoftWrap: foundation.Some(false)}),
			widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(24.0)}),
			widgets.NewText(line, widgets.TextOpts{Style: text, SoftWrap: foundation.Some(false)}),
		})
	}
	return widgets.NewContainer(
		widgets.NewSingleChildScrollView(
			widgets.NewPadding(painting.NewEdgeInsetsSymmetric(8, 16),
				widgets.NewColumn(rows, widgets.ColumnOpts{
					CrossAxisAlignment: rendering.CrossAxisAlignmentStart,
					MainAxisSize:       rendering.MainAxisSizeMin,
				}))),
		widgets.ContainerOpts{Color: foundation.Some(colors.EditorBackground)})
}

// editorTextStyle is the text style of the editor: the font family and size of the settings
// editor.fontFamily and editor.fontSize. A comma-separated family is tried in order; an
// empty one is the desktop's monospace font; the generic monospace family is the last
// resort (SPEC D14).
func editorTextStyle(bc widgets.BuildContext, store *settings.Store, color ui.Color) *painting.TextStyle {
	families := fontFamilies(settings.Get[string](store, "editor.fontFamily", ""))
	if len(families) == 0 {
		if desktop, _ := widgets.WidgetsBindingOf(bc).PlatformDispatcher().MonospaceFont(); desktop != "" {
			families = append(families, desktop)
		}
	}
	families = append(families, "monospace")
	return painting.NewTextStyle(painting.TextStyleOpts{
		FontFamily:         families[0],
		FontFamilyFallback: families[1:],
		FontSize:           foundation.Some(settings.Get[float64](store, "editor.fontSize", "")),
		Color:              foundation.Some(color),
	})
}

// fontFamilies splits the value of editor.fontFamily: comma-separated names, each trimmed
// and without the quotes around a name with spaces.
func fontFamilies(value string) []string {
	var out []string
	for _, name := range strings.Split(value, ",") {
		name = strings.Trim(strings.TrimSpace(name), `'"`)
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}
