// Package welcome draws the welcome content of a view (VS Code's viewsWelcome, SPEC §10.30 step 24.3): the Markdown that a view
// shows in place of its rows while it has none. A line is a paragraph, and a line that is only a link is a button; the pieces of
// content of a view come one below the other. The Source Control view and the Explorer use it.
//
// Ported from VS Code: src/vs/workbench/browser/parts/views/viewPane.ts (ViewWelcomeController), src/vs/base/common/linkedText.ts
// and src/vs/workbench/browser/parts/views/media/views.css
package welcome

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui/markdown"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// Piece is one piece of welcome content.
type Piece struct {
	// Contents is the Markdown.
	Contents string
	// Disabled dims the buttons of the piece and stops them (the Enablement of the piece does not hold).
	Disabled bool
}

// Block is a line of a piece: a paragraph of text, or a button.
type Block struct {
	// Text is the Markdown of a paragraph; empty for a button.
	Text string
	// Label and Target are the text and the destination of a button: a line that is only [Label](Target).
	Label, Target string
	// Disabled says that the piece of the block is disabled.
	Disabled bool
}

// IsButton tells whether the block is a button.
func (b Block) IsButton() bool { return b.Target != "" }

// buttonLine is a line that is a link and nothing else.
var buttonLine = regexp.MustCompile(`^\[([^\]]+)\]\(([^)\s]+)\)$`)

// Parse splits pieces into blocks: each non-empty line is a paragraph, or a button when it is a link alone.
func Parse(pieces []Piece) []Block {
	var out []Block
	for _, p := range pieces {
		for _, line := range strings.Split(p.Contents, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if m := buttonLine.FindStringSubmatch(line); m != nil {
				out = append(out, Block{Label: m[1], Target: m[2], Disabled: p.Disabled})
				continue
			}
			out = append(out, Block{Text: line, Disabled: p.Disabled})
		}
	}
	return out
}

// CommandOf reads a command link: "command:git.init" and "command:git.clone?%5B%22a%22%5D" (the arguments as URL-encoded JSON).
// It says whether the target is a command link.
func CommandOf(target string) (id, args string, ok bool) {
	rest, ok := strings.CutPrefix(target, "command:")
	if !ok {
		return "", "", false
	}
	id, query, _ := strings.Cut(rest, "?")
	if decoded, err := url.QueryUnescape(query); err == nil {
		query = decoded
	}
	return id, query, id != ""
}

// Opts are the options of New.
type Opts struct {
	// Key is the key of the widget.
	Key foundation.Key
	// Pieces are the pieces to show.
	Pieces []Piece
	// OnLink is called with the target of a link or a button that the user pressed (a command: link or a web address).
	OnLink func(target string)
}

// New makes the welcome content: the paragraphs and the buttons of the pieces, one below the other, with VS Code's margins.
func New(opts Opts) widgets.Widget {
	blocks := Parse(opts.Pieces)
	return widgets.NewBuilder(func(bc widgets.BuildContext) widgets.Widget {
		var children []widgets.Widget
		for _, b := range blocks {
			switch {
			case b.IsButton():
				target := b.Target
				children = append(children, widgets.NewPadding(painting.NewEdgeInsetsSymmetric(4, 0),
					kit.NewButton(b.Label, kit.ButtonOpts{Expand: true, Disabled: b.Disabled, OnPressed: func() {
						if opts.OnLink != nil {
							opts.OnLink(target)
						}
					}})))
			default:
				children = append(children, widgets.NewPadding(painting.NewEdgeInsetsSymmetric(4, 0),
					markdown.NewView(b.Text, markdown.Options{Flow: true, OnLink: opts.OnLink})))
			}
		}
		return widgets.NewKeyedSubtree(
			widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(20, 4, 20, 0),
				widgets.NewColumn(children, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch, MainAxisSize: rendering.MainAxisSizeMin})),
			widgets.WidgetOpts{Key: opts.Key})
	})
}
