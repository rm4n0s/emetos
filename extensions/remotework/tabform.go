package remotework

import (
	"context"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/internal/keyform"
	"github.com/rm4n0s/emetos/extensions/internal/keyqr/qrui"
	"github.com/rm4n0s/emetos/extensions/internal/pageui"
)

// tabForm is the connect dialog: the host, the port, and the key (a file, or one of the keys of the ssh-agent). Its boxes keep
// what the user typed when the tab is shown again, and the state of the tab is what they hold.
type tabForm struct {
	t *remoteTab

	host, port *ui.InputBoxController
	// keys is the choice of the key (extensions/internal/keyform).
	keys *keyform.Form
}

func newTabForm(t *remoteTab, st tabState) *tabForm {
	f := &tabForm{t: t, host: ui.NewInputBoxController(st.Host), port: ui.NewInputBoxController(strconv.Itoa(st.Port))}
	f.keys = keyform.New(keyform.Env{
		Spawn: t.spawn, Post: t.api.Post, Refresh: func() { t.tab.Refresh() }, AgentSocket: t.opts.agentSocket, Changed: t.stateChanged,
	}, st.Key, st.KeyFile, st.Fingerprint)
	return f
}

// dispose lets go of the boxes and stops the read of the agent.
func (f *tabForm) dispose() {
	f.keys.Dispose()
	f.host.Dispose()
	f.port.Dispose()
}

// state is the form as the user filled it.
func (f *tabForm) state() tabState {
	port, err := strconv.Atoi(strings.TrimSpace(f.port.Value()))
	if err != nil {
		port = 0 // checked when the user presses Connect; a state with a port of 0 comes back as the default
	}
	return tabState{Host: strings.TrimSpace(f.host.Value()), Port: port, Key: f.keys.Source, KeyFile: f.keys.Path(), Fingerprint: f.keys.Fingerprint}
}

// address is host:port as typed, for the messages.
func (f *tabForm) address() string {
	s := f.state()
	if s.Port == 0 {
		return s.Host
	}
	return s.Host + ":" + strconv.Itoa(s.Port)
}

// set puts a state in the boxes.
func (f *tabForm) set(st tabState) {
	f.host.SetValue(st.Host)
	f.port.SetValue(strconv.Itoa(st.Port))
	f.keys.Set(st.Key, st.KeyFile, st.Fingerprint)
}

// formView is the connect dialog.
func (t *remoteTab) formView(bc widgets.BuildContext, colors ui.Colors) widgets.Widget {
	f := t.form
	dim := ui.TextStyle(bc, bodySize, colors.DescriptionForeground)
	label := ui.TextStyle(bc, 12, colors.DescriptionForeground)
	connecting := t.phase == phaseConnecting || t.phase == phasePicker
	submit := func(string) { t.connect() }
	field := func(title string, c *ui.InputBoxController, width float64, placeholder string) widgets.Widget {
		return widgets.NewColumn([]widgets.Widget{
			widgets.NewText(title, widgets.TextOpts{Style: label}), gap(4),
			widgets.NewSizedBox(ui.NewInputBox(nil, nil, ui.InputBoxOpts{Controller: c, Disabled: connecting, Placeholder: placeholder, OnSubmit: submit, OnChanged: func(string) { t.stateChanged() }}), widgetsWidth(width)),
		}, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart})
	}

	items := []widgets.Widget{
		widgets.NewText("Remote Work", widgets.TextOpts{Style: bold(ui.TextStyle(bc, 22, colors.Foreground))}),
		gap(6),
		widgets.NewText("Use a workspace of another computer that runs Emetos. You log in over SSH with a key that computer lists, and work in a second view of its workspace.", widgets.TextOpts{Style: dim}),
		gap(18),
		widgets.NewRow([]widgets.Widget{
			widgets.NewExpanded(field("Host", f.host, 1000, "laptop.local or 192.168.1.20")), hgap(12),
			field("Port", f.port, 110, strconv.Itoa(DefaultPort)),
		}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart}),
		gap(16),
	}
	items = append(items, f.keys.Rows(bc, colors, keyform.RowOptions{Disabled: connecting, Submit: submit, Choose: t.chooseKeyFile})...)
	items = append(items, gap(8), widgets.NewText("The private key never leaves this computer, and a passphrase is asked for when it is needed and is not kept.", widgets.TextOpts{Style: dim}))
	if qr := t.keyQRSlot(bc); qr != nil {
		items = append(items, gap(10), qr)
	}
	items = append(items, gap(18), widgets.NewRow([]widgets.Widget{
		ui.NewButton("Connect", ui.ButtonOpts{Disabled: connecting, OnPressed: t.connect}),
	}, widgets.RowOpts{}))
	if t.note.Text != "" {
		color := colors.DescriptionForeground
		if t.note.Bad {
			color = colors.ErrorForeground
		}
		items = append(items, gap(10), widgets.NewText(t.note.Text, widgets.TextOpts{Style: ui.TextStyle(bc, bodySize, color)}))
	}
	return widgets.NewColoredBox(colors.EditorBackground, ui.NewScrollView(widgets.NewAlign(
		widgets.NewConstrainedBox(rendering.NewBoxConstraints(rendering.BoxConstraintsOpts{MaxWidth: foundation.Some(columnWidth)}),
			widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(24, 20, 24, 24), widgets.NewColumn(items, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}))),
		widgets.AlignOpts{Alignment: painting.AlignmentTopCenter}), &t.scroll))
}

// keyQRSlot is the Show My Public Key as QR Code button under the choice of the key, with what it is for.
func (t *remoteTab) keyQRSlot(bc widgets.BuildContext) widgets.Widget {
	busy := t.phase == phaseConnecting || t.phase == phasePicker
	return keyform.QRSlot(bc, t.showMyKey, busy)
}

// showMyKey shows the public half of the key that is chosen as a QR code: the .pub file next to the key file, or the public key read
// out of the key file itself (an OpenSSH key keeps it in the clear, so no passphrase is asked for; another format is asked for the
// passphrase it needs), or the key of the ssh-agent that is chosen. The private key is never drawn, kept or sent: only the public
// key is given to the dialog, which refuses anything else.
func (t *remoteTab) showMyKey() {
	f := t.form.keys
	if f.Source == keyform.Agent {
		line, problem := f.AgentLine()
		if problem != "" {
			t.note = pageui.Bad(problem)
			t.tab.Refresh()
			return
		}
		t.showQR(line)
		return
	}
	path := f.Path()
	if path == "" {
		t.note = pageui.Bad("Choose the private key file first: its public key is what is shown.")
		t.tab.Refresh()
		return
	}
	t.spawn(func() {
		line, err := keyform.PublicLineOfKeyFile(path, t.prompter.Passphrase(path))
		t.api.Post(func() {
			if err != nil {
				t.failForm(err)
				return
			}
			t.showQR(line)
		})
	})
}

// showQR opens the dialog with the QR code of the line of a public key. The UI thread.
func (t *remoteTab) showQR(line string) {
	d, err := qrui.NewShowDialog(qrui.ShowOptions{Line: line, Close: func() { t.qr = nil; t.tab.Refresh() }})
	if err != nil {
		t.failForm(err)
		return
	}
	t.qr, t.note = d, note{}
	t.tab.Refresh()
}

// chooseKeyFile asks for the private key file with the dialog of the desktop.
func (t *remoteTab) chooseKeyFile() {
	t.spawn(func() {
		path, err := t.api.FileChooser().Open(t.ctxOrBackground(), extension.FileChooserOptions{Title: "Choose the private key", AcceptLabel: "Use"})
		t.api.Post(func() {
			switch {
			case err != nil:
				t.note = pageui.Bad(err.Error())
			case path != "":
				t.form.keys.KeyFile.SetValue(path)
				t.note = note{}
				t.stateChanged()
			}
			t.tab.Refresh()
		})
	})
}

// ctxOrBackground is the context of the extension, or a background one before it is known.
func (t *remoteTab) ctxOrBackground() context.Context {
	if t.ctx != nil {
		return t.ctx
	}
	return context.Background()
}

// failForm shows the reason a form cannot be used. The UI thread.
func (t *remoteTab) failForm(err error) {
	msg := err.Error()
	if appErr, ok := errors.FromError(err); ok {
		msg = appErr.Message
	}
	t.note = pageui.Bad(msg)
	t.tab.Refresh()
}
