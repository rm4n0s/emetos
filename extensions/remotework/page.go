package remotework

import (
	"context"
	"strconv"
	"strings"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/internal/keyqr/qrui"
	"github.com/rm4n0s/emetos/extensions/internal/pageui"
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

	// ctx ends when the page closes or the extension does: the look for cameras and the dialog of Scan QR Code end with it. scan is the
	// Scan QR Code dialog while it is open; cameras is what the last look for cameras found (step 15.5).
	ctx     context.Context
	cancel  context.CancelFunc
	scan    *qrui.ScanDialog
	cameras cameraCheck

	// notes are the last answers of the actions: the switch, the connection settings and the key box; an error shows in red.
	switchNote, settingsNote, keyNote note
	hostKeyErr                        string
}

// note is a line under a control: what an action answered.
type note = pageui.Note

func newPage(ctx context.Context, h *Hub, api extension.API, tab extension.EditorTab, folder string) *page {
	p := &page{hub: h, api: api, tab: tab, folder: folder}
	if ctx == nil {
		ctx = context.Background()
	}
	p.ctx, p.cancel = context.WithCancel(ctx)
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
	p.lookForCameras()
	return p
}

// close lets go of the hub and the boxes when the tab closes.
func (p *page) close() {
	if p.scan != nil {
		p.scan.Close() // the camera is let go
	}
	p.cancel()
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
				p.switchNote = pageui.Bad(err.Error())
			}
			p.refresh()
		})
	}()
}

// applySettings reads the port and the idle timeout from the boxes and sets them.
func (p *page) applySettings() {
	port, err := strconv.Atoi(strings.TrimSpace(p.port.Value()))
	if err != nil {
		p.settingsNote = pageui.Bad("The port is a number from 1 to 65535.")
		p.refresh()
		return
	}
	minutes, err := strconv.Atoi(strings.TrimSpace(p.idle.Value()))
	if err != nil {
		p.settingsNote = pageui.Bad("The idle timeout is a number of minutes, from 1 to 1440.")
		p.refresh()
		return
	}
	snap := p.hub.Snapshot(p.folder)
	switch {
	case CheckPort(port) != nil:
		p.settingsNote = pageui.Bad("The port is a number from 1 to 65535.")
	case CheckIdle(minutes) != nil:
		p.settingsNote = pageui.Bad("The idle timeout is from 1 minute to 24 hours (1440).")
	default:
		go func() {
			result := pageui.Fine("Saved.")
			if port != snap.Config.Port {
				if err := p.hub.SetPort(port); err != nil {
					result = pageui.Bad(err.Error())
				}
			}
			if minutes != snap.Config.IdleMinutes && !result.Bad {
				if err := p.hub.SetIdleMinutes(minutes); err != nil {
					result = pageui.Bad(err.Error())
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
		p.keyNote = pageui.Bad(err.Error())
	} else {
		p.keyNote = pageui.Fine("Added " + key.Name() + ".")
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
			line, err = sshlogin.ReadPublicKeyFile(path)
			if err == nil {
				p.api.Post(func() { p.addKey(line) })
				return
			}
		}
		if err != nil {
			p.api.Post(func() {
				p.keyNote = pageui.Bad(err.Error())
				p.refresh()
			})
		}
	}()
}

// ---- Scan QR Code ----

// cameraCheck is what the look for cameras found: it is running, or it found a camera (reason is empty), or it found none and
// reason says why, which the disabled button shows.
type cameraCheck struct {
	looking bool
	reason  string
}

// lookForCameras asks the Source for the cameras off the UI thread, so that the button knows whether there is one. It opens each
// device for a moment to ask what it is, and starts nothing.
func (p *page) lookForCameras() {
	src := p.hub.Cameras()
	if src == nil {
		p.cameras = cameraCheck{reason: qrui.NoSourceReason}
		return
	}
	p.cameras = cameraCheck{looking: true}
	go func() {
		cams, err := src.Cameras(p.ctx)
		if p.ctx.Err() != nil {
			return
		}
		reason := qrui.NoCameraReason(cams, err)
		p.api.Post(func() {
			if p.ctx.Err() != nil {
				return
			}
			p.cameras = cameraCheck{reason: reason}
			p.refresh()
		})
	}()
}

// openScan opens the Scan QR Code dialog. The camera is opened by the dialog and released when it closes.
func (p *page) openScan() {
	if p.scan != nil {
		return
	}
	p.scan = qrui.NewScanDialog(qrui.ScanOptions{
		Context: p.ctx, Source: p.hub.Cameras(), Post: p.api.Post, Refresh: p.refresh,
		Add:   func(key sshlogin.AllowedKey) { p.addKey(key.Line()) },
		Close: func() { p.scan = nil; p.refresh() },
	})
	p.refresh()
}

// scanQRButton is the Scan QR Code button next to the ways of adding a key, and the reason under it when there is no camera: it is
// disabled while the look for cameras runs and when it found none.
func (p *page) scanQRButton(locked bool) widgets.Widget {
	return ui.NewButton("Scan QR Code...", ui.ButtonOpts{Secondary: true, Disabled: locked || p.cameras.looking || p.cameras.reason != "", OnPressed: p.openScan})
}

// scanReason is the line under the buttons that says why Scan QR Code cannot be used; nothing when it can.
func (p *page) scanReason(bc widgets.BuildContext, colors ui.Colors) widgets.Widget {
	text := p.cameras.reason
	if p.cameras.looking {
		text = "Looking for cameras..."
	}
	if text == "" {
		return widgets.NewSizedBox(nil, widgets.SizedBoxOpts{})
	}
	return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 6, 0, 0), widgets.NewText("Scan QR Code is not available: "+text, widgets.TextOpts{Style: ui.TextStyle(bc, 12, colors.DescriptionForeground)}))
}

// ---- widgets ----

func gap(h float64) widgets.Widget  { return pageui.Gap(h) }
func hgap(w float64) widgets.Widget { return pageui.HGap(w) }

func monoFamily(bc widgets.BuildContext) string { return pageui.MonoFamily(bc) }

func bold(s *painting.TextStyle) *painting.TextStyle { return pageui.Bold(s) }

func (p *page) paragraph(_ widgets.BuildContext, text string, style *painting.TextStyle) widgets.Widget {
	return pageui.Paragraph(text, style)
}

// section is a heading over its rows, with a line above it.
func (p *page) section(bc widgets.BuildContext, colors ui.Colors, title string, rows ...widgets.Widget) widgets.Widget {
	return pageui.Section(bc, colors, title, rows...)
}

// noteLine shows an answer under a control.
func (p *page) noteLine(bc widgets.BuildContext, colors ui.Colors, n note) widgets.Widget {
	return pageui.NoteLine(bc, colors, n)
}

// field is a labelled input box of a width.
func (p *page) field(bc widgets.BuildContext, colors ui.Colors, label string, c *ui.InputBoxController, width float64, disabled bool) widgets.Widget {
	return pageui.Field(bc, colors, label, c, width, disabled, p.applySettings)
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
	page := widgets.NewColoredBox(colors.EditorBackground, ui.NewScrollView(body, &p.scroll))
	if p.scan == nil {
		return page
	}
	// the dialog of Scan QR Code over the page, on a veil that keeps the pointer from it
	return widgets.NewStack([]widgets.Widget{page, p.scan.Build(bc)}, widgets.StackOpts{Fit: rendering.StackFitExpand})
}

// problem is a message in a box with the color of an error.
func (p *page) problem(bc widgets.BuildContext, colors ui.Colors, msg string) widgets.Widget {
	return pageui.Problem(bc, colors, msg)
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
	buttons = append(buttons, hgap(8), p.scanQRButton(locked))
	rows = append(rows, gap(6), widgets.NewRow(buttons, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}), p.scanReason(bc, colors), p.noteLine(bc, colors, p.keyNote))
	return p.section(bc, colors, "Allowed keys", rows...)
}

// removeKey takes a key out of the list.
func (p *page) removeKey(k sshlogin.AllowedKey) {
	if err := p.hub.RemoveKey(k.Fingerprint()); err != nil {
		p.keyNote = pageui.Bad(err.Error())
	} else {
		p.keyNote = pageui.Fine("Removed " + k.Name() + ".")
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
