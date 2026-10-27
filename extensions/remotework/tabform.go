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
	"golang.org/x/crypto/ssh"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/internal/sshlogin"
)

// agentKey is a key of the ssh-agent the form offers.
type agentKey struct {
	// Fingerprint is its SHA256:... fingerprint and Label what the form shows ("ssh-ed25519  SHA256:...").
	Fingerprint, Label string
}

// tabForm is the connect dialog: the host, the port, and the key (a file, or one of the keys of the ssh-agent). Its boxes keep
// what the user typed when the tab is shown again, and the state of the tab is what they hold.
type tabForm struct {
	t *remoteTab

	host, port, keyFile *ui.InputBoxController
	// source is keyFile or keyAgent.
	source string
	// fingerprint is the agent key chosen; empty offers every key of the agent.
	fingerprint string

	// agent are the keys of the ssh-agent, listed when the form is shown with the agent chosen and when it is chosen; agentNote says
	// why there are none, and agentLoading that the list is being read.
	agent        []agentKey
	agentNote    string
	agentLoading bool
	agentStop    context.CancelFunc
}

func newTabForm(t *remoteTab, st tabState) *tabForm {
	f := &tabForm{
		t: t, source: st.Key, fingerprint: st.Fingerprint,
		host: ui.NewInputBoxController(st.Host), port: ui.NewInputBoxController(strconv.Itoa(st.Port)), keyFile: ui.NewInputBoxController(st.KeyFile),
	}
	if f.source == keyAgent {
		f.listAgent()
	}
	return f
}

// dispose lets go of the boxes and stops the read of the agent.
func (f *tabForm) dispose() {
	if f.agentStop != nil {
		f.agentStop()
	}
	f.host.Dispose()
	f.port.Dispose()
	f.keyFile.Dispose()
}

// state is the form as the user filled it.
func (f *tabForm) state() tabState {
	port, err := strconv.Atoi(strings.TrimSpace(f.port.Value()))
	if err != nil {
		port = 0 // checked when the user presses Connect; a state with a port of 0 comes back as the default
	}
	return tabState{Host: strings.TrimSpace(f.host.Value()), Port: port, Key: f.source, KeyFile: strings.TrimSpace(f.keyFile.Value()), Fingerprint: f.fingerprint}
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
	f.keyFile.SetValue(st.KeyFile)
	f.source, f.fingerprint = st.Key, st.Fingerprint
	if f.source == keyAgent {
		f.listAgent()
	}
}

// listAgent reads the keys of the ssh-agent off the UI thread.
func (f *tabForm) listAgent() {
	if f.agentStop != nil {
		f.agentStop()
	}
	t := f.t
	ctx, cancel := context.WithCancel(context.Background())
	f.agentStop = cancel
	f.agentLoading, f.agentNote = true, ""
	socket := t.opts.agentSocket()
	if !t.spawn(func() {
		keys, err := sshlogin.AgentKeys(ctx, socket)
		if ctx.Err() != nil {
			return
		}
		t.api.Post(func() {
			f.agentLoading = false
			f.agent = f.agent[:0]
			switch {
			case err != nil:
				f.agentNote = describeError("the ssh-agent", err).title
			case len(keys) == 0:
				f.agentNote = "The ssh-agent has no key. Add one with ssh-add."
			}
			for _, k := range keys {
				f.agent = append(f.agent, agentKey{Fingerprint: ssh.FingerprintSHA256(k), Label: k.Type() + "  " + ssh.FingerprintSHA256(k)})
			}
			t.tab.Refresh()
		})
	}) {
		cancel()
		f.agentLoading = false
	}
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
		widgets.NewText("Key", widgets.TextOpts{Style: label}), gap(4),
		ui.NewDropdown([]ui.DropdownOption{{Label: "Private key file"}, {Label: "SSH agent"}}, ui.DropdownOpts{
			Selected: map[string]int{keyFile: 0, keyAgent: 1}[f.source], Disabled: connecting, Width: foundation.Some(220.0),
			OnChanged: func(i int) {
				f.source = []string{keyFile, keyAgent}[i]
				if f.source == keyAgent {
					f.listAgent()
				}
				t.stateChanged()
				t.tab.Refresh()
			},
		}),
		gap(8),
	}
	if f.source == keyAgent {
		items = append(items, f.agentRow(bc, colors, connecting))
	} else {
		items = append(items, widgets.NewRow([]widgets.Widget{
			widgets.NewExpanded(ui.NewInputBox(nil, nil, ui.InputBoxOpts{Controller: f.keyFile, Disabled: connecting, Placeholder: "/path/to/id_ed25519", OnSubmit: submit, OnChanged: func(string) { t.stateChanged() }})),
			hgap(8), ui.NewButton("Choose...", ui.ButtonOpts{Secondary: true, Disabled: connecting, OnPressed: t.chooseKeyFile}),
		}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}))
	}
	items = append(items, gap(8), widgets.NewText("The private key never leaves this computer, and a passphrase is asked for when it is needed and is not kept.", widgets.TextOpts{Style: dim}))
	if qr := t.keyQRSlot(bc); qr != nil {
		items = append(items, gap(10), qr)
	}
	items = append(items, gap(18), widgets.NewRow([]widgets.Widget{
		ui.NewButton("Connect", ui.ButtonOpts{Disabled: connecting, OnPressed: t.connect}),
	}, widgets.RowOpts{}))
	if t.note.text != "" {
		color := colors.DescriptionForeground
		if t.note.bad {
			color = colors.ErrorForeground
		}
		items = append(items, gap(10), widgets.NewText(t.note.text, widgets.TextOpts{Style: ui.TextStyle(bc, bodySize, color)}))
	}
	return widgets.NewColoredBox(colors.EditorBackground, ui.NewScrollView(widgets.NewAlign(
		widgets.NewConstrainedBox(rendering.NewBoxConstraints(rendering.BoxConstraintsOpts{MaxWidth: foundation.Some(columnWidth)}),
			widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(24, 20, 24, 24), widgets.NewColumn(items, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}))),
		widgets.AlignOpts{Alignment: painting.AlignmentTopCenter}), &t.scroll))
}

// keyQRSlot is the place of the "Show My Public Key as QR Code" button of step 15.5, under the choice of the key; there is none yet.
func (t *remoteTab) keyQRSlot(widgets.BuildContext) widgets.Widget { return nil }

// agentRow is the choice among the keys of the ssh-agent.
func (f *tabForm) agentRow(bc widgets.BuildContext, colors ui.Colors, disabled bool) widgets.Widget {
	dim := ui.TextStyle(bc, bodySize, colors.DescriptionForeground)
	options := []ui.DropdownOption{{Label: "Any key of the agent"}}
	selected := 0
	for i, k := range f.agent {
		options = append(options, ui.DropdownOption{Label: k.Label})
		if k.Fingerprint == f.fingerprint {
			selected = i + 1
		}
	}
	rows := []widgets.Widget{ui.NewDropdown(options, ui.DropdownOpts{
		Selected: selected, Disabled: disabled || f.agentLoading || len(f.agent) == 0 && f.agentNote != "", Width: foundation.Some(520.0),
		OnChanged: func(i int) {
			f.fingerprint = ""
			if i > 0 {
				f.fingerprint = f.agent[i-1].Fingerprint
			}
			f.t.stateChanged()
			f.t.tab.Refresh()
		},
	})}
	switch {
	case f.agentLoading:
		rows = append(rows, gap(6), widgets.NewText("Reading the keys of the ssh-agent...", widgets.TextOpts{Style: dim}))
	case f.agentNote != "":
		rows = append(rows, gap(6), widgets.NewText(f.agentNote, widgets.TextOpts{Style: ui.TextStyle(bc, bodySize, colors.ErrorForeground)}))
	}
	return widgets.NewColumn(rows, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart})
}

// chooseKeyFile asks for the private key file with the dialog of the desktop.
func (t *remoteTab) chooseKeyFile() {
	t.spawn(func() {
		path, err := t.api.FileChooser().Open(t.ctxOrBackground(), extension.FileChooserOptions{Title: "Choose the private key", AcceptLabel: "Use"})
		t.api.Post(func() {
			switch {
			case err != nil:
				t.note = note{err.Error(), true}
			case path != "":
				t.form.keyFile.SetValue(path)
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
	t.note = note{msg, true}
	t.tab.Refresh()
}
