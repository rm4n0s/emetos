package remotework

import (
	"context"
	"io"
	"os"
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
	"github.com/rm4n0s/emetos/extensions/internal/sshlogin"
)

// The look of the page.
const (
	columnWidth = 720.0
	bodySize    = 13.0
)

// securityNotes are the notes of SPEC §10.18 that the page shows the user.
var securityNotes = []string{
	"A listed key gives the power of the user in this workspace: whoever has the key can edit and delete files and run tasks, as you can.",
	"The first connection from another computer is only as safe as the comparison of the host fingerprint above with the one that computer shows.",
	"Over the internet this is as safe as SSH with key-only login. A VPN is better still.",
	"Remote Work listens only while a workspace has it on. Passwords are never accepted, and 5 failed attempts in a minute block an address for a minute.",
	"Every login and every refused attempt is logged and shown as a notification.",
}

// page is the Remote Work page: an editor tab of one workspace that shows and changes the hub's state.
type page struct {
	hub    *Hub
	api    extension.API
	tab    extension.EditorTab
	folder string
	stop   func()

	port, idle, key *ui.InputBoxController
	scroll          ui.ScrollHandle

	// notes are the last answers of the actions: the switch, the connection settings and the key box; an error shows in red.
	switchNote, settingsNote, keyNote note
	hostKeyErr                        string
}

// note is a line under a control: what an action answered.
type note struct {
	text string
	bad  bool
}

func newPage(h *Hub, api extension.API, tab extension.EditorTab, folder string) *page {
	p := &page{hub: h, api: api, tab: tab, folder: folder}
	snap := h.Snapshot(folder)
	p.port = ui.NewInputBoxController(strconv.Itoa(snap.Config.Port))
	p.idle = ui.NewInputBoxController(strconv.Itoa(snap.Config.IdleMinutes))
	p.key = ui.NewInputBoxController("")
	// the user can read the fingerprint before the first client asks for it
	if snap.LoadErr == "" && snap.HostFingerprint == "" {
		if _, err := h.EnsureHostKey(); err != nil {
			p.hostKeyErr = err.Error()
		}
	}
	p.stop = h.OnChange(func() { api.Post(tab.Refresh) })
	return p
}

// close lets go of the hub and the boxes when the tab closes.
func (p *page) close() {
	p.stop()
	p.port.Dispose()
	p.idle.Dispose()
	p.key.Dispose()
}

// hasFolder tells whether the workspace has a folder, which is what Remote Work needs to name it.
func (p *page) hasFolder() bool { return p.folder != "." && p.folder != "" }

// refresh builds the page again.
func (p *page) refresh() { p.tab.Refresh() }

// ---- actions ----

// toggle turns Remote Work on or off for this workspace.
// The hub starts and stops the listener, and waits for the sessions it ends, so it works off the UI thread.
func (p *page) toggle(on bool) {
	go func() {
		err := p.hub.SetWorkspace(p.folder, on)
		p.api.Post(func() {
			p.switchNote = note{}
			if err != nil {
				p.switchNote = note{err.Error(), true}
			}
			p.refresh()
		})
	}()
}

// applySettings reads the port and the idle timeout from the boxes and sets them.
func (p *page) applySettings() {
	port, err := strconv.Atoi(strings.TrimSpace(p.port.Value()))
	if err != nil {
		p.settingsNote = note{"The port is a number from 1 to 65535.", true}
		p.refresh()
		return
	}
	minutes, err := strconv.Atoi(strings.TrimSpace(p.idle.Value()))
	if err != nil {
		p.settingsNote = note{"The idle timeout is a number of minutes, from 1 to 1440.", true}
		p.refresh()
		return
	}
	snap := p.hub.Snapshot(p.folder)
	switch {
	case CheckPort(port) != nil:
		p.settingsNote = note{"The port is a number from 1 to 65535.", true}
	case CheckIdle(minutes) != nil:
		p.settingsNote = note{"The idle timeout is from 1 minute to 24 hours (1440).", true}
	default:
		go func() {
			result := note{"Saved.", false}
			if port != snap.Config.Port {
				if err := p.hub.SetPort(port); err != nil {
					result = note{err.Error(), true}
				}
			}
			if minutes != snap.Config.IdleMinutes && !result.bad {
				if err := p.hub.SetIdleMinutes(minutes); err != nil {
					result = note{err.Error(), true}
				}
			}
			p.api.Post(func() {
				p.settingsNote = result
				p.refresh()
			})
		}()
		return
	}
	p.refresh()
}

// addKey adds the line of the key box to the list.
func (p *page) addKey(line string) {
	key, err := p.hub.AddKey(line)
	if err != nil {
		p.keyNote = note{err.Error(), true}
	} else {
		p.keyNote = note{"Added " + key.Name() + ".", false}
		p.key.SetValue("")
	}
	p.refresh()
}

// chooseKeyFile asks for a .pub file with the dialog of the desktop and adds the key in it.
func (p *page) chooseKeyFile() {
	go func() {
		path, err := p.api.FileChooser().Open(context.Background(), extension.FileChooserOptions{
			Title: "Choose a public key", AcceptLabel: "Add",
			Filters: []extension.FileFilter{{Name: "Public keys", Patterns: []string{"*.pub"}}, {Name: "All files", Patterns: []string{"*"}}},
		})
		if err == nil && path != "" {
			var line string
			line, err = readPublicKeyFile(path)
			if err == nil {
				p.api.Post(func() { p.addKey(line) })
				return
			}
		}
		if err != nil {
			p.api.Post(func() {
				p.keyNote = note{err.Error(), true}
				p.refresh()
			})
		}
	}()
}

// maxPublicKeyFile is the largest .pub file that is read.
const maxPublicKeyFile = 16 << 10

// readPublicKeyFile reads the line of a public key file. It fails with RemoteWorkKeyFileInvalid for a file that is not a small
// regular file, and with the error of the file system.
func readPublicKeyFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", errors.NewErr("RemoteWorkKeyFileUnreadable", err).SetMetadata("path", path)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return "", errors.NewErr("RemoteWorkKeyFileUnreadable", err).SetMetadata("path", path)
	}
	if !info.Mode().IsRegular() || info.Size() > maxPublicKeyFile {
		return "", errors.New("RemoteWorkKeyFileInvalid", "a public key file is a small regular file", "path", path)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxPublicKeyFile))
	if err != nil {
		return "", errors.NewErr("RemoteWorkKeyFileUnreadable", err).SetMetadata("path", path)
	}
	return string(data), nil
}

// scanQRButton is the place of the "Scan QR Code" button of step 15.5, next to the ways of adding a key; there is none yet.
func (p *page) scanQRButton() widgets.Widget { return nil }

// ---- widgets ----

func gap(h float64) widgets.Widget {
	return widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(h)})
}

func hgap(w float64) widgets.Widget {
	return widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(w)})
}

func monoFamily(bc widgets.BuildContext) string {
	family, _ := widgets.WidgetsBindingOf(bc).PlatformDispatcher().MonospaceFont()
	if family == "" {
		return "monospace"
	}
	return family
}

func bold(s *painting.TextStyle) *painting.TextStyle {
	return s.CopyWith(painting.TextStyleOpts{FontWeight: gui.FontWeightW600})
}

func (p *page) paragraph(bc widgets.BuildContext, text string, style *painting.TextStyle) widgets.Widget {
	return widgets.NewText(text, widgets.TextOpts{Style: style})
}

// section is a heading over its rows, with a line above it.
func (p *page) section(bc widgets.BuildContext, colors ui.Colors, title string, rows ...widgets.Widget) widgets.Widget {
	head := widgets.NewText(title, widgets.TextOpts{Style: bold(ui.TextStyle(bc, 15, colors.Foreground))})
	line := widgets.NewColoredBox(colors.PanelBorder, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(1.0)}))
	children := []widgets.Widget{gap(18), line, gap(14), head, gap(10)}
	children = append(children, rows...)
	return widgets.NewColumn(children, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
}

// noteLine shows an answer under a control.
func (p *page) noteLine(bc widgets.BuildContext, colors ui.Colors, n note) widgets.Widget {
	if n.text == "" {
		return widgets.NewSizedBox(nil, widgets.SizedBoxOpts{})
	}
	color := colors.DescriptionForeground
	if n.bad {
		color = colors.ErrorForeground
	}
	return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 6, 0, 0), widgets.NewText(n.text, widgets.TextOpts{Style: ui.TextStyle(bc, 12, color)}))
}

// field is a labelled input box of a width.
func (p *page) field(bc widgets.BuildContext, colors ui.Colors, label string, c *ui.InputBoxController, width float64, disabled bool) widgets.Widget {
	return widgets.NewColumn([]widgets.Widget{
		widgets.NewText(label, widgets.TextOpts{Style: ui.TextStyle(bc, 12, colors.DescriptionForeground)}),
		gap(4),
		widgets.NewSizedBox(ui.NewInputBox(nil, nil, ui.InputBoxOpts{
			Controller: c, Disabled: disabled, OnSubmit: func(string) { p.applySettings() },
		}), widgetsWidth(width)),
	}, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart})
}

func widgetsWidth(w float64) widgets.SizedBoxOpts {
	return widgets.SizedBoxOpts{Width: foundation.Some(w)}
}

// build is the widget of the page.
func (p *page) build(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	snap := p.hub.Snapshot(p.folder)
	dim := ui.TextStyle(bc, bodySize, colors.DescriptionForeground)
	text := ui.TextStyle(bc, bodySize, colors.Foreground)
	mono := ui.TextStyle(bc, 12, colors.Foreground).CopyWith(painting.TextStyleOpts{FontFamily: monoFamily(bc)})
	locked := snap.LoadErr != ""

	items := []widgets.Widget{
		widgets.NewText("Remote Work", widgets.TextOpts{Style: bold(ui.TextStyle(bc, 22, colors.Foreground))}),
		gap(6),
		p.paragraph(bc, "Use this workspace from another computer that runs Emetos. It logs in over SSH with a key you list below, and works in a second view of the workspace.", dim),
	}
	if locked {
		items = append(items, gap(12), p.problem(bc, colors, "remote-work.json cannot be used, so Remote Work stays off and nothing here can be changed until it is fixed: "+snap.LoadErr))
	}
	items = append(items, p.workspaceSection(bc, colors, snap, text, dim, locked))
	items = append(items, p.connectionSection(bc, colors, snap, locked))
	items = append(items, p.keysSection(bc, colors, snap, text, dim, mono, locked))
	items = append(items, p.hostSection(bc, colors, snap, text, dim, mono))
	notes := make([]widgets.Widget, 0, 2*len(securityNotes))
	for _, n := range securityNotes {
		notes = append(notes, widgets.NewRow([]widgets.Widget{
			widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 1, 8, 0), ui.Icon("shield", 14, colors.NotificationsWarningIconForeground)),
			widgets.NewExpanded(p.paragraph(bc, n, text)),
		}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart}), gap(6))
	}
	items = append(items, p.section(bc, colors, "Security", notes...))
	items = append(items, gap(24))

	body := widgets.NewAlign(
		widgets.NewConstrainedBox(rendering.NewBoxConstraints(rendering.BoxConstraintsOpts{MaxWidth: foundation.Some(columnWidth)}),
			widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(24, 20, 24, 0), widgets.NewColumn(items, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}))),
		widgets.AlignOpts{Alignment: painting.AlignmentTopCenter})
	return widgets.NewColoredBox(colors.EditorBackground, ui.NewScrollView(body, &p.scroll))
}

// problem is a message in a box with the color of an error.
func (p *page) problem(bc widgets.BuildContext, colors ui.Colors, msg string) widgets.Widget {
	return widgets.NewColoredBox(colors.InputValidationErrorBackground, widgets.NewPadding(painting.NewEdgeInsetsAll(10),
		widgets.NewText(msg, widgets.TextOpts{Style: ui.TextStyle(bc, bodySize, colors.InputValidationErrorForeground)})))
}

// workspaceSection is the switch of this workspace, what the listener does, and the connected clients.
func (p *page) workspaceSection(bc widgets.BuildContext, colors ui.Colors, snap Snapshot, text, dim *painting.TextStyle, locked bool) widgets.Widget {
	name := p.api.Scope().Name
	var rows []widgets.Widget
	switch {
	case !p.hasFolder():
		rows = append(rows, p.paragraph(bc, "This workspace has no folder, so it cannot be shared. Open a folder to turn Remote Work on for it.", dim))
	default:
		rows = append(rows,
			ui.NewCheckbox("Allow other computers to use "+name, ui.CheckboxOpts{Checked: snap.On, Disabled: locked, OnChanged: p.toggle}),
			gap(4),
			p.paragraph(bc, p.folder, dim))
		status, bad := "Off. Nothing listens for Remote Work.", false
		switch {
		case snap.On && snap.ListenErr != "":
			status, bad = "On, but it cannot listen: "+snap.ListenErr, true
		case snap.On && snap.Listening:
			status = "On. Listening on " + portOf(snap.Addr, snap.Config.Port) + ", for the keys below."
		case snap.On:
			status = "On. Starting to listen."
		case snap.Listening:
			status = "Off for this workspace. Another workspace has it on, so the port is open for that one."
		}
		style := dim
		if bad {
			style = ui.TextStyle(bc, bodySize, colors.ErrorForeground)
		}
		rows = append(rows, gap(8), p.paragraph(bc, status, style), p.noteLine(bc, colors, p.switchNote))
		for _, s := range snap.Sessions {
			s := s
			rows = append(rows, gap(10), widgets.NewRow([]widgets.Widget{
				ui.Icon("remote", 14, colors.NotificationsInfoIconForeground), hgap(8),
				widgets.NewExpanded(p.paragraph(bc, s.Key+" connected from "+hostOf(s.Remote)+" since "+s.Started.Format("15:04"), text)),
				ui.NewButton("Disconnect", ui.ButtonOpts{Small: true, Secondary: true, OnPressed: s.Disconnect}),
			}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}))
		}
	}
	return p.section(bc, colors, "This workspace", rows...)
}

// portOf is the port the listener is on, from its address, or the configured one.
func portOf(addr string, configured int) string {
	if i := strings.LastIndex(addr, ":"); i >= 0 && i+1 < len(addr) {
		return "port " + addr[i+1:]
	}
	return "port " + strconv.Itoa(configured)
}

// connectionSection is the port and the idle timeout.
func (p *page) connectionSection(bc widgets.BuildContext, colors ui.Colors, snap Snapshot, locked bool) widgets.Widget {
	dim := ui.TextStyle(bc, bodySize, colors.DescriptionForeground)
	return p.section(bc, colors, "Connection",
		widgets.NewRow([]widgets.Widget{
			p.field(bc, colors, "Port (TCP, and UDP for the picture)", p.port, 150, locked), hgap(16),
			p.field(bc, colors, "Idle timeout (minutes)", p.idle, 150, locked), hgap(16),
			widgets.NewColumn([]widgets.Widget{gap(20), ui.NewButton("Apply", ui.ButtonOpts{Disabled: locked, OnPressed: p.applySettings})}, widgets.ColumnOpts{}),
		}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart}),
		p.noteLine(bc, colors, p.settingsNote),
		gap(8),
		p.paragraph(bc, "A new port closes the connections that are open. The idle timeout ends a session that has no traffic for that long.", dim))
}

// keysSection lists the allowed keys and adds more.
func (p *page) keysSection(bc widgets.BuildContext, colors ui.Colors, snap Snapshot, text, dim, mono *painting.TextStyle, locked bool) widgets.Widget {
	rows := []widgets.Widget{p.paragraph(bc, "Only these public keys may log in. Paste a line of an authorized_keys file (the contents of an id_ed25519.pub), or choose the .pub file.", dim), gap(10)}
	if len(snap.Keys) == 0 {
		rows = append(rows, p.paragraph(bc, "No key is listed, so nobody can log in.", dim), gap(6))
	}
	for _, k := range snap.Keys {
		k := k
		small := mono.CopyWith(painting.TextStyleOpts{Color: foundation.Some(colors.DescriptionForeground)})
		rows = append(rows, widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 4, 0, 4), widgets.NewRow([]widgets.Widget{
			ui.Icon("key", 16, colors.Foreground), hgap(8),
			widgets.NewExpanded(widgets.NewColumn([]widgets.Widget{
				widgets.NewText(k.Name(), widgets.TextOpts{Style: bold(text), MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis}),
				widgets.NewText(k.Type()+"  "+k.Fingerprint(), widgets.TextOpts{Style: small, MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis}),
			}, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart})),
			ui.NewButton("Remove", ui.ButtonOpts{Small: true, Secondary: true, Disabled: locked, OnPressed: func() { p.removeKey(k) }}),
		}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})))
	}
	buttons := []widgets.Widget{
		widgets.NewExpanded(ui.NewInputBox(nil, nil, ui.InputBoxOpts{Controller: p.key, Disabled: locked, Placeholder: "ssh-ed25519 AAAA... comment", OnSubmit: p.addKey})),
		hgap(8), ui.NewButton("Add", ui.ButtonOpts{Disabled: locked, OnPressed: func() { p.addKey(p.key.Value()) }}),
		hgap(8), ui.NewButton("Choose .pub File...", ui.ButtonOpts{Secondary: true, Disabled: locked, OnPressed: p.chooseKeyFile}),
	}
	if scan := p.scanQRButton(); scan != nil {
		buttons = append(buttons, hgap(8), scan)
	}
	rows = append(rows, gap(6), widgets.NewRow(buttons, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}), p.noteLine(bc, colors, p.keyNote))
	return p.section(bc, colors, "Allowed keys", rows...)
}

// removeKey takes a key out of the list.
func (p *page) removeKey(k sshlogin.AllowedKey) {
	if err := p.hub.RemoveKey(k.Fingerprint()); err != nil {
		p.keyNote = note{err.Error(), true}
	} else {
		p.keyNote = note{"Removed " + k.Name() + ".", false}
	}
	p.refresh()
}

// hostSection shows the fingerprint of the host key.
func (p *page) hostSection(bc widgets.BuildContext, colors ui.Colors, snap Snapshot, text, dim, mono *painting.TextStyle) widgets.Widget {
	var rows []widgets.Widget
	switch {
	case snap.HostFingerprint != "":
		rows = append(rows,
			p.paragraph(bc, "The other computer shows this fingerprint the first time it connects. Log in only if it is the same.", dim), gap(8),
			widgets.NewRow([]widgets.Widget{ui.Icon("lock", 16, colors.Foreground), hgap(8), widgets.NewText("ssh-ed25519  "+snap.HostFingerprint, widgets.TextOpts{Style: mono})}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}))
	case snap.HostKeyErr != "" || p.hostKeyErr != "":
		msg := snap.HostKeyErr
		if msg == "" {
			msg = p.hostKeyErr
		}
		rows = append(rows, p.problem(bc, colors, "The host key cannot be used: "+msg))
	default:
		rows = append(rows, p.paragraph(bc, "The host key is made the first time Remote Work is turned on.", dim))
	}
	return p.section(bc, colors, "Host key of this computer", rows...)
}
