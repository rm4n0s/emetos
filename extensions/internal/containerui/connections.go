package containerui

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
)

// The Connections of Podman (SPEC §10.20 "The side bar view"): the list of `podman system connection list`, with the rootless and the
// rootful engine of each, and the choice of the one this view talks to. A connection that is a Unix socket or a tcp address can be
// used; one that goes over ssh:// cannot (Emetos does not open ssh connections to engines, SPEC §10.20 "Not built").

// connection is a row of `podman system connection list`.
type connection struct {
	// Name and URI are what Podman says; Identity is the key file of an ssh connection.
	Name, URI, Identity string
	// Default is the connection `podman` uses when none is named.
	Default bool
	// ReadWrite is false for a connection to a read-only engine.
	ReadWrite bool
	// Machine says that a Podman machine made the connection.
	Machine bool
}

// wireConnection is the JSON of one connection (Podman prints Name, URI, Identity, IsMachine, Default and ReadWrite).
type wireConnection struct {
	Name      string `json:"Name"`
	URI       string `json:"URI"`
	Identity  string `json:"Identity"`
	IsMachine bool   `json:"IsMachine"`
	Default   bool   `json:"Default"`
	ReadWrite *bool  `json:"ReadWrite"`
}

// parseConnections reads the output of `podman system connection list --format json`; no output at all is no connection.
func parseConnections(out string) ([]connection, error) {
	out = strings.TrimSpace(out)
	if out == "" || out == "null" {
		return nil, nil
	}
	var rows []wireConnection
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		return nil, errors.NewErr("ContainerUIConnectionsInvalid", err)
	}
	cs := make([]connection, 0, len(rows))
	for _, r := range rows {
		if r.Name == "" {
			continue
		}
		c := connection{Name: r.Name, URI: r.URI, Identity: r.Identity, Default: r.Default, Machine: r.IsMachine, ReadWrite: true}
		if r.ReadWrite != nil {
			c.ReadWrite = *r.ReadWrite
		}
		cs = append(cs, c)
	}
	return cs, nil
}

// rootful tells whether the connection is to a Podman that runs as root: its socket is /run/podman/podman.sock, or Podman machine's
// "-root" connection. A socket under /run/user/<uid> is the rootless engine of that user.
func (c connection) rootful() bool {
	if strings.HasSuffix(c.Name, "-root") {
		return true
	}
	u, err := url.Parse(c.URI)
	if err != nil {
		return false
	}
	return strings.HasPrefix(u.Path, "/run/podman/") || strings.HasPrefix(u.Path, "/var/run/podman/")
}

// usable tells whether Emetos can connect there, and when it cannot, why not.
func (c connection) usable() (bool, string) {
	u, err := url.Parse(c.URI)
	if err != nil || u.Scheme == "" {
		return false, "the address " + c.URI + " is not one Emetos understands"
	}
	switch u.Scheme {
	case "unix", "tcp", "http", "https":
		return true, ""
	case "ssh":
		return false, "Emetos does not connect over ssh:// (start the Podman service of the connection and use its socket or a tcp:// address)"
	}
	return false, "Emetos does not connect to " + u.Scheme + "://"
}

// kind is "rootful" or "rootless" for a row.
func (c connection) kind() string {
	if c.rootful() {
		return "rootful"
	}
	return "rootless"
}

// connectionTimeout bounds `podman system connection list`.
const connectionTimeout = 15 * time.Second

// loadConnections reads the connections with the command line, on a goroutine. A failure (no podman program, a Podman that does
// not know the command) leaves the list empty and is shown in the section.
func (e *ext) loadConnections() {
	l := &e.loads[resConnections]
	if l.busy {
		l.again = true
		return
	}
	l.busy = true
	go func() {
		cs, err := e.readConnections(e.ctx)
		e.api.Post(func() {
			l.busy = false
			if err != nil {
				e.connections, l.err = nil, reasonOf(err)
			} else {
				e.connections, l.err = cs, ""
			}
			l.loaded = true
			if l.again {
				l.again = false
				e.loadConnections()
			}
			e.rebuild(resConnections)
		})
	}()
}

// readConnections runs `podman system connection list --format json`.
func (e *ext) readConnections(ctx context.Context) ([]connection, error) {
	cli, err := e.findCLI()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, connectionTimeout)
	defer cancel()
	var out, errOut strings.Builder
	code, err := runProcess(ctx, cliJob{argv: []string{cli, "system", "connection", "list", "--format", "json"}, env: e.cfg.environ()}, func(stderr bool, line string) {
		if stderr {
			errOut.WriteString(line + "\n")
		} else {
			out.WriteString(line + "\n")
		}
	})
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, errors.New("ContainerUIConnectionsFailed", "`"+e.cfg.cliName()+" system connection list` ended with status "+strconv.Itoa(code)+": "+strings.TrimSpace(errOut.String()),
			"status", code)
	}
	return parseConnections(out.String())
}

// useConnection connects to the engine of a connection (name "" is the automatic search). It is a choice of this view only: Podman's
// own default connection is not changed.
func (e *ext) useConnection(name string) {
	host := ""
	if name != "" {
		var found *connection
		for i := range e.connections {
			if e.connections[i].Name == name {
				found = &e.connections[i]
			}
		}
		if found == nil {
			e.api.Notifications().Warn("There is no connection called " + name + ".")
			return
		}
		if ok, why := found.usable(); !ok {
			e.api.Notifications().Warn("Cannot use the connection " + name + ": " + why + ".")
			return
		}
		host = found.URI
	}
	if name == e.picked && host == e.pickedHost {
		return
	}
	e.picked, e.pickedHost = name, host
	e.connect()
	e.rebuild(resConnections)
}

// ---- the section ----

type connectionsSection struct{}

func (*connectionsSection) name() string  { return secConnections }
func (*connectionsSection) res() resource { return resConnections }
func (*connectionsSection) title() string { return "Connections" }

// keyConnection starts the key of the row of a connection; the row of the automatic search is keyAutomatic.
const (
	keyConnection = "y:"
	keyAutomatic  = "y:"
)

// connectionNode is a row of the Connections section.
type connectionNode struct {
	c connection
	// automatic is the row that stands for the automatic search of the engine.
	automatic bool
	// current says that this is the engine in use.
	current bool
}

func (s *connectionsSection) items(e *ext) ([]ui.TreeItem, string) {
	l := e.loads[resConnections]
	if !l.loaded {
		return nil, ""
	}
	items := []ui.TreeItem{{Key: keyAutomatic, Label: "Automatic", Data: connectionNode{automatic: true, current: e.picked == ""}}}
	for _, c := range e.connections {
		items = append(items, ui.TreeItem{Key: keyConnection + c.Name, Label: c.Name, Data: connectionNode{c: c, current: c.Name == e.picked}})
	}
	if l.err != "" {
		return items, "Cannot read the connections: " + l.err
	}
	if len(e.connections) == 0 {
		return items, "`" + e.cfg.cliName() + " system connection list` lists none."
	}
	return items, ""
}

func (s *connectionsSection) render(bc widgets.BuildContext, v *sectionView, node ui.TreeNode, st ui.RowState) widgets.Widget {
	colors := ui.Theme(bc)
	e := v.e
	d, ok := node.Data.(connectionNode)
	if !ok {
		return text(bc, node.Label, 13, st.Foreground)
	}
	icon := "vm"
	if d.current {
		icon = "check"
	}
	var label, desc string
	if d.automatic {
		label, desc = "Automatic", "the socket of the user, the rootful socket, or a service of Emetos's own"
	} else {
		label = d.c.Name
		desc = strings.Join(nonEmpty(d.c.kind(), mark(d.c.Default, "default"), mark(!d.c.ReadWrite, "read-only"), d.c.URI), " · ")
	}
	row := []widgets.Widget{ui.Icon(icon, 16, st.Foreground), gap(6)}
	row = append(row, labelAndDescription(bc, colors, st, label, desc)...)
	if !d.current {
		name := d.c.Name
		if ok, _ := d.c.usable(); ok || d.automatic {
			row = append(row, rowButton(st, "check", "Use", func() { e.useConnection(name) }))
		}
	}
	return widgets.NewRow(row, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
}

func (s *connectionsSection) menu(v *sectionView, keys []string) []ui.MenuItem {
	e := v.e
	if len(keys) != 1 || !strings.HasPrefix(keys[0], keyConnection) {
		return nil
	}
	name := strings.TrimPrefix(keys[0], keyConnection)
	items := []ui.MenuItem{{Label: "Use", OnRun: func() { e.useConnection(name) }, Disabled: name == e.picked}}
	if name != "" {
		items = append(items, ui.MenuItem{Label: "Inspect", OnRun: func() { e.inspectConnection(name) }})
	}
	return items
}

func (s *connectionsSection) open(v *sectionView, node ui.TreeNode) {
	if d, ok := node.Data.(connectionNode); ok && !d.current {
		v.e.useConnection(d.c.Name)
	}
}

// inspectConnection opens the description of a connection in a read-only tab.
func (e *ext) inspectConnection(name string) {
	for _, c := range e.connections {
		if c.Name == name {
			ok, why := c.usable()
			raw, _ := json.Marshal(map[string]any{
				"Name": c.Name, "URI": c.URI, "Identity": c.Identity, "Default": c.Default, "ReadWrite": c.ReadWrite, "IsMachine": c.Machine,
				"Engine": c.kind(), "UsableByEmetos": ok, "Why": why,
			})
			e.docs.open(name+" (inspect)", indentJSON(raw), "json")
			return
		}
	}
}

// pickConnection asks which connection to use (the picker of the title bar of the view).
func (e *ext) pickConnection() {
	items := []extension.QuickPickItem{{Label: "Automatic", Description: mark(e.picked == "", "(in use)"), Detail: "the socket of the user, the rootful socket, or a service of Emetos's own"}}
	names := []string{""}
	for _, c := range e.connections {
		ok, why := c.usable()
		detail := c.kind()
		if !ok {
			detail += " · " + why
		}
		items = append(items, extension.QuickPickItem{Label: c.Name, Description: strings.Join(nonEmpty(c.URI, mark(c.Name == e.picked, "(in use)")), " "), Detail: detail})
		names = append(names, c.Name)
	}
	err := e.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: "Podman connection", Items: items, MatchOnDescription: true}, func(i int) {
		if i >= 0 && i < len(names) {
			e.useConnection(names[i])
		}
	})
	if err != nil {
		e.api.Log().Warn("the quick pick cannot be shown", "err", err)
	}
}
