package httpclient

import (
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
)

// clientNoteText is what the side bar views say on the screen of a client of a hosted workspace (SPEC §10.33 "Privacy and safety", D46):
// a request would leave from the host's network, so the commands of the extension are refused to clients (the class outside) and the
// views list nothing of the host's collections, environments or history.
const clientNoteText = "The HTTP Client is not available in a shared workspace: a request would leave from the host's network. The collections are files in .emetos/http, which you can read and edit as files."

// forClient is the note a view shows instead of its contents when it is drawn on the screen of a client of a hosted workspace (the
// view is built once for each screen that shows it, ui.OriginOf), and nil on the screen of the host.
func forClient(bc widgets.BuildContext) widgets.Widget {
	if ui.OriginOf(bc) == extension.HostOrigin {
		return nil
	}
	return column(note(bc, clientNoteText, ui.Theme(bc).DescriptionForeground))
}
