package containerui

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
	"github.com/rm4n0s/emetos/extensions/internal/pageui"
)

// The form that runs an image (SPEC §10.20 "Image actions": a name, ports, environment, volumes and --rm) is a tab of the editor
// area, so that it has the room for its fields.

// runState is what the tab is made from.
type runState struct {
	// Image is the reference (or the id) of the image to run, Label the text the tab shows for it.
	Image string `json:"image"`
	Label string `json:"label"`
}

// runForms makes the tabs.
type runForms struct {
	e *ext
	// forms are the forms that are open.
	forms []*runForm
}

func newRunForms(e *ext) *runForms { return &runForms{e: e} }

// provide sets how the tabs are made.
func (r *runForms) provide() error {
	return r.e.api.Editors().Provide(r.e.cfg.RunKind(), extension.EditorKind{Create: r.create})
}

// runImage opens the form that runs an image.
func (e *ext) runImage(row imageRow) {
	if _, ok := e.ready(); !ok {
		return
	}
	label := row.ref
	if label == "" {
		label = containerapi.ShortID(row.image.ID)
	}
	_, err := e.api.Editors().Open(e.cfg.RunKind(), runState{Image: row.target(), Label: label}, extension.EditorOpenOptions{})
	if err != nil {
		e.api.Notifications().Error("Cannot open the form: " + err.Error())
	}
}

// runForm is one open form.
type runForm struct {
	e     *ext
	tab   extension.EditorTab
	state runState

	name, network, command *ui.InputBoxController
	ports, env, volumes    *ui.InputBoxController
	autoRemove             bool
	scroll                 ui.ScrollHandle
	message                pageui.Note
	busy                   bool
	cancel                 context.CancelFunc
}

// create makes the content of a tab.
func (r *runForms) create(tab extension.EditorTab, raw json.RawMessage) (extension.EditorTabContent, error) {
	var st runState
	if err := json.Unmarshal(raw, &st); err != nil {
		return extension.EditorTabContent{}, errors.NewErr("ContainerUIRunStateInvalid", err)
	}
	tab.SetTitle("Run " + st.Label)
	f := &runForm{
		e: r.e, tab: tab, state: st, autoRemove: true,
		name: ui.NewInputBoxController(""), network: ui.NewInputBoxController(""), command: ui.NewInputBoxController(""),
		ports: ui.NewInputBoxController(""), env: ui.NewInputBoxController(""), volumes: ui.NewInputBoxController(""),
	}
	r.forms = append(r.forms, f)
	return extension.EditorTabContent{Build: f.build, SaveState: func() (any, error) { return st, nil }, OnClose: func() { f.close(); r.drop(f) }}, nil
}

// drop forgets a form that closed.
func (r *runForms) drop(f *runForm) {
	for i, o := range r.forms {
		if o == f {
			r.forms = append(r.forms[:i], r.forms[i+1:]...)
			return
		}
	}
}

// close lets go of the fields and ends a run that is going on.
func (f *runForm) close() {
	if f.cancel != nil {
		f.cancel()
	}
	for _, c := range []*ui.InputBoxController{f.name, f.network, f.command, f.ports, f.env, f.volumes} {
		c.Dispose()
	}
}

// spec reads the fields into what the engine is asked to make.
func (f *runForm) spec() (containerapi.ContainerSpec, error) {
	spec := containerapi.ContainerSpec{Image: f.state.Image, Name: strings.TrimSpace(f.name.Value()), AutoRemove: f.autoRemove,
		Network: strings.TrimSpace(f.network.Value())}
	var err error
	if spec.Ports, err = parsePorts(f.ports.Value()); err != nil {
		return spec, err
	}
	if spec.Env, err = parseEnv(f.env.Value()); err != nil {
		return spec, err
	}
	if spec.Binds, err = parseBinds(f.volumes.Value()); err != nil {
		return spec, err
	}
	if spec.Cmd, err = splitCommand(f.command.Value()); err != nil {
		return spec, err
	}
	return spec, nil
}

// run makes the container and starts it.
func (f *runForm) run() {
	if f.busy {
		return
	}
	c, ok := f.e.ready()
	if !ok {
		return
	}
	spec, err := f.spec()
	if err != nil {
		f.message = pageui.Bad(reasonOf(err))
		f.tab.Refresh()
		return
	}
	f.busy = true
	f.message = pageui.Fine("Starting...")
	f.tab.SetBusy(true)
	f.tab.Refresh()
	ctx, cancel := context.WithCancel(f.e.ctx)
	f.cancel = cancel
	go func() {
		res, err := c.CreateContainer(ctx, spec)
		if err == nil {
			err = c.StartContainer(ctx, res.ID) // a container that does not start is left for the user to look at
		}
		f.e.api.Post(func() {
			cancel()
			f.cancel = nil
			f.busy = false
			f.tab.SetBusy(false)
			if err != nil {
				f.message = pageui.Bad(reasonOf(err))
				f.tab.Refresh()
				return
			}
			f.e.api.Notifications().Info("Started a container of " + f.state.Label + ".")
			f.tab.Close()
		})
	}()
}

// build makes the widget of the form.
func (f *runForm) build(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	body := pageui.BodySize
	field := func(label, hint string, c *ui.InputBoxController, multi bool, placeholder string) widgets.Widget {
		col := []widgets.Widget{
			widgets.NewText(label, widgets.TextOpts{Style: pageui.Bold(ui.TextStyle(bc, 12, colors.Foreground))}),
			pageui.Gap(4),
			ui.NewInputBox(nil, nil, ui.InputBoxOpts{Controller: c, MultiLine: multi, Placeholder: placeholder, Disabled: f.busy,
				MaxHeight: foundation.Some(120.0)}),
		}
		if hint != "" {
			col = append(col, pageui.Gap(3), widgets.NewText(hint, widgets.TextOpts{Style: ui.TextStyle(bc, 12, colors.DescriptionForeground)}))
		}
		return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 0, 0, 14), widgets.NewColumn(col, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}))
	}
	form := widgets.NewColumn([]widgets.Widget{
		widgets.NewText("Run "+f.state.Label, widgets.TextOpts{Style: pageui.Bold(ui.TextStyle(bc, 18, colors.Foreground))}),
		pageui.Gap(16),
		field("Name", "", f.name, false, "chosen by the engine when empty"),
		field("Ports", "One per line: 8080:80, 127.0.0.1:8080:80/tcp or 5432. Without an address a port is published on every interface.", f.ports, true, "8080:80"),
		field("Environment", "One per line: NAME=value.", f.env, true, "NAME=value"),
		field("Volumes", "One per line: a volume or a folder, a colon and a path in the container: data:/var/lib/data or /srv/site:/usr/share/nginx/html:ro.", f.volumes, true, "data:/var/lib/data"),
		field("Network", "", f.network, false, "the engine's default network"),
		field("Command", "Replaces the command of the image. Quotes group words.", f.command, false, "the command of the image"),
		widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(0, 0, 0, 14), ui.NewCheckbox("Remove the container when it stops (--rm)", ui.CheckboxOpts{
			Checked: f.autoRemove, Disabled: f.busy, OnChanged: func(on bool) { f.autoRemove = on; f.tab.Refresh() },
		})),
		widgets.NewRow([]widgets.Widget{
			ui.NewButton("Run", ui.ButtonOpts{Disabled: f.busy, OnPressed: f.run}), pageui.HGap(8),
			ui.NewButton("Cancel", ui.ButtonOpts{Secondary: true, OnPressed: func() { f.tab.Close() }}),
		}, widgets.RowOpts{}),
		pageui.NoteLine(bc, colors, f.message),
	}, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
	_ = body
	return widgets.NewColoredBox(colors.EditorBackground, ui.NewScrollView(
		widgets.NewPadding(painting.NewEdgeInsetsAll(20), widgets.NewAlign(widgets.NewConstrainedBox(rendering.NewBoxConstraints(rendering.BoxConstraintsOpts{MaxWidth: foundation.Some(560.0)}), form), widgets.AlignOpts{Alignment: painting.AlignmentTopLeft})),
		&f.scroll))
}

// ---- parsing the fields ----

// lines returns the non-empty lines of a field, trimmed.
func lines(text string) []string {
	var out []string
	for _, l := range strings.Split(text, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// parsePorts reads the ports field: [address:]host:container[/protocol] or a container port alone (the engine picks the host
// port).
func parsePorts(text string) ([]containerapi.PortMapping, error) {
	var out []containerapi.PortMapping
	for _, l := range lines(text) {
		spec, proto, _ := strings.Cut(l, "/")
		if proto == "" {
			proto = "tcp"
		}
		if proto != "tcp" && proto != "udp" {
			return nil, errors.New("ContainerUIPortInvalid", "the protocol of a port is tcp or udp: "+l, "line", l)
		}
		parts := strings.Split(spec, ":")
		var pm containerapi.PortMapping
		pm.Protocol = proto
		num := func(s string) (uint16, bool) {
			n, err := strconv.Atoi(s)
			return uint16(n), err == nil && n > 0 && n < 65536
		}
		var ok bool
		switch len(parts) {
		case 1:
			pm.ContainerPort, ok = num(parts[0])
		case 2:
			var a, b bool
			pm.HostPort, a = num(parts[0])
			pm.ContainerPort, b = num(parts[1])
			ok = a && b
		case 3:
			var a, b bool
			pm.HostIP = parts[0]
			pm.HostPort, a = num(parts[1])
			pm.ContainerPort, b = num(parts[2])
			ok = a && b && parts[0] != ""
		}
		if !ok {
			return nil, errors.New("ContainerUIPortInvalid", "a port is written 8080:80, 127.0.0.1:8080:80/tcp or 5432: "+l, "line", l)
		}
		out = append(out, pm)
	}
	return out, nil
}

// parseEnv reads the environment field: NAME=value lines.
func parseEnv(text string) ([]string, error) {
	var out []string
	for _, l := range lines(text) {
		name, _, ok := strings.Cut(l, "=")
		if !ok || strings.TrimSpace(name) == "" || strings.ContainsAny(name, " \t") {
			return nil, errors.New("ContainerUIEnvInvalid", "a variable is written NAME=value: "+l, "line", l)
		}
		out = append(out, l)
	}
	return out, nil
}

// parseBinds reads the volumes field: source:destination[:options] lines.
func parseBinds(text string) ([]string, error) {
	var out []string
	for _, l := range lines(text) {
		parts := strings.Split(l, ":")
		if len(parts) < 2 || len(parts) > 3 || parts[0] == "" || !strings.HasPrefix(parts[1], "/") {
			return nil, errors.New("ContainerUIVolumeInvalid", "a volume is written source:/path[:options], with the path of the container absolute: "+l, "line", l)
		}
		out = append(out, l)
	}
	return out, nil
}

// splitCommand cuts a command line into words; single and double quotes group words and a backslash escapes the next character.
func splitCommand(text string) ([]string, error) {
	var words []string
	var cur strings.Builder
	in, quote := false, rune(0)
	escaped := false
	for _, r := range strings.TrimSpace(text) {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
		case r == '\\' && quote != '\'':
			escaped, in = true, true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, in = r, true
		case r == ' ' || r == '\t':
			if in {
				words = append(words, cur.String())
				cur.Reset()
				in = false
			}
		default:
			cur.WriteRune(r)
			in = true
		}
	}
	if quote != 0 || escaped {
		return nil, errors.New("ContainerUICommandInvalid", "a quote is not closed in the command: "+text, "command", text)
	}
	if in {
		words = append(words, cur.String())
	}
	return words, nil
}
