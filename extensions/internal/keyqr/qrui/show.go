package qrui

import (
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/internal/keyqr"
	"github.com/rm4n0s/emetos/extensions/internal/sshlogin"
)

// showSide is the side, in pixels, the QR code is made to fit in: the card has 452 logical pixels for it. Each module is a whole
// number of pixels, so the code is a little smaller than this (an RSA 4096 key is 4 pixels a module, 452).
const showSide = 452

// ShowOptions are what NewShowDialog needs.
type ShowOptions struct {
	// Line is the line of the public key to show, in the form of an authorized_keys line (type, base64 and an optional comment).
	Line string
	// Close is called, on the UI thread, once, when the user closes the dialog: the owner drops it.
	Close func()
}

// ShowDialog is the Show My Public Key as QR Code dialog: the QR code of the public half of a key, with the key's type,
// fingerprint and comment, for the other computer's Scan QR Code. The private key never gets here: the dialog is given the line
// of a public key, and refuses anything else.
type ShowDialog struct {
	opts    ShowOptions
	key     sshlogin.AllowedKey
	picture *ui.Picture
	closed  bool
}

// NewShowDialog makes the dialog: the line is parsed under the rules of the login, and drawn. It fails with the tags of
// keyqr.EncodeKey (and of ui.NewPicture, which cannot fail for a code of this size): SSHLoginKeyLineInvalid and SSHLoginKeyUnsupported for a line that is not a public key the login takes, and
// KeyQREncodeFailed for one that does not fit in a QR code; the dialog is not made for them.
func NewShowDialog(opts ShowOptions) (*ShowDialog, error) {
	img, key, err := keyqr.EncodeKey(opts.Line, showSide)
	if err != nil {
		return nil, err
	}
	pic, err := ui.NewPicture(img)
	if err != nil {
		return nil, err
	}
	return &ShowDialog{opts: opts, key: key, picture: pic}, nil
}

// Key is the key the dialog shows.
func (d *ShowDialog) Key() sshlogin.AllowedKey { return d.key }

// Close closes the dialog and tells its owner. It does nothing the second time.
func (d *ShowDialog) Close() {
	if d.closed {
		return
	}
	d.closed = true
	d.picture = nil
	if d.opts.Close != nil {
		d.opts.Close()
	}
}

// Build is the dialog, to be stacked over the content of its owner.
func (d *ShowDialog) Build(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	// the code is black on white whatever the theme is (a scanner reads dark modules on a light ground), is drawn at its own size
	// and is made smaller, in the room that is left, in a window too low for it
	var code widgets.Widget = widgets.NewSizedBox(nil)
	if d.picture != nil {
		code = widgets.NewFlexible(ui.NewImage(d.picture, ui.ImageOpts{Pixelated: true}))
	}
	return Card(bc, column(
		title(bc, colors, "key", colors.Foreground, "My Public Key"),
		vgap(12),
		code,
		vgap(12),
		keyDetails(bc, colors, d.key.Type(), d.key.Fingerprint(), d.key.Comment),
		vgap(10),
		paragraph(bc, "Hold this in front of the camera of the other computer, with its Scan QR Code. Only the public key is shown: the private key never leaves this computer.", colors.DescriptionForeground),
		vgap(14),
		buttons(ui.NewButton("Close", ui.ButtonOpts{OnPressed: d.Close})),
	))
}
