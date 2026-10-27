package keyform

import (
	"context"
	"os"
	"strings"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/widgets"
	"golang.org/x/crypto/ssh"

	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/internal/pageui"
	"github.com/rm4n0s/emetos/extensions/internal/sshlogin"
)

// The ways a form chooses its key.
const (
	// File is a private key file named by the form.
	File = "file"
	// Agent is a key of the ssh-agent of SSH_AUTH_SOCK.
	Agent = "agent"
)

// AgentKey is a key of the ssh-agent the form offers.
type AgentKey struct {
	// Fingerprint is its SHA256:... fingerprint and Label what the form shows ("ssh-ed25519  SHA256:...").
	Fingerprint, Label string
	// Line is the public key as an authorized_keys line (the agent tells no comment), which Show My Public Key as QR Code draws.
	Line string
}

// Env is what a form needs of the tab it is in.
type Env struct {
	// Spawn runs f on a goroutine that the tab waits for when it closes; it returns false (and runs nothing) once the tab is closing.
	Spawn func(f func()) bool
	// Post runs a function on the UI thread (the Post of the extension API) and Refresh builds the tab again.
	Post    func(func())
	Refresh func()
	// AgentSocket gives the socket of the ssh-agent.
	AgentSocket func() string
	// Changed says that what the form holds changed (the tab saves its state).
	Changed func()
}

// Form is the choice of the key a client logs in with: a private key file, or one of the keys of the ssh-agent. Its boxes keep
// what the user typed when the tab is shown again. The UI thread only.
type Form struct {
	env Env

	// Source is File or Agent. Fingerprint is the agent key chosen (empty offers every key of the agent), and KeyFile the box of the
	// path of the private key file.
	Source      string
	Fingerprint string
	KeyFile     *ui.InputBoxController

	// Agent are the keys of the ssh-agent, listed when the form is shown with the agent chosen and when it is chosen; AgentNote says
	// why there are none, and AgentLoading that the list is being read.
	Agent        []AgentKey
	AgentNote    string
	AgentLoading bool
	agentStop    context.CancelFunc
}

// New makes a form with the choice a state held. With the agent chosen it reads the agent's keys.
func New(env Env, source, keyFile, fingerprint string) *Form {
	f := &Form{env: env, Source: source, Fingerprint: fingerprint, KeyFile: ui.NewInputBoxController(keyFile)}
	if f.Source == Agent {
		f.ListAgent()
	}
	return f
}

// Dispose lets go of the box and stops the read of the agent.
func (f *Form) Dispose() {
	if f.agentStop != nil {
		f.agentStop()
	}
	f.KeyFile.Dispose()
}

// Set puts a choice in the form.
func (f *Form) Set(source, keyFile, fingerprint string) {
	f.KeyFile.SetValue(keyFile)
	f.Source, f.Fingerprint = source, fingerprint
	if f.Source == Agent {
		f.ListAgent()
	}
}

// Path is the private key file as typed.
func (f *Form) Path() string { return strings.TrimSpace(f.KeyFile.Value()) }

// ListAgent reads the keys of the ssh-agent off the UI thread.
func (f *Form) ListAgent() {
	if f.agentStop != nil {
		f.agentStop()
	}
	ctx, cancel := context.WithCancel(context.Background())
	f.agentStop = cancel
	f.AgentLoading, f.AgentNote = true, ""
	socket := f.env.AgentSocket()
	if !f.env.Spawn(func() {
		keys, err := sshlogin.AgentKeys(ctx, socket)
		if ctx.Err() != nil {
			return
		}
		f.env.Post(func() {
			f.AgentLoading = false
			f.Agent = f.Agent[:0]
			switch {
			case err != nil:
				f.AgentNote = AgentProblem(err)
			case len(keys) == 0:
				f.AgentNote = "The ssh-agent has no key. Add one with ssh-add."
			}
			for _, k := range keys {
				f.Agent = append(f.Agent, AgentKey{Fingerprint: ssh.FingerprintSHA256(k), Label: k.Type() + "  " + ssh.FingerprintSHA256(k), Line: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(k)))})
			}
			f.env.Refresh()
		})
	}) {
		cancel()
		f.AgentLoading = false
	}
}

// AgentProblem says in a line why the keys of the ssh-agent cannot be had.
func AgentProblem(err error) string {
	appErr, ok := errors.FromError(err)
	if !ok {
		return "The connection failed"
	}
	switch appErr.Tag {
	case "SSHLoginAgentUnavailable":
		return "There is no ssh-agent"
	case "SSHLoginAgentNoKey", "SSHLoginAgentFailed":
		return "The ssh-agent has no such key"
	}
	return "The connection failed"
}

// AgentLine is the line of the key of the ssh-agent the form has chosen, or what is in the way: the keys are being read, the agent
// has none, or it has several and none is chosen.
func (f *Form) AgentLine() (line, problem string) {
	switch {
	case f.AgentLoading:
		return "", "The keys of the ssh-agent are still being read."
	case len(f.Agent) == 0:
		if f.AgentNote != "" {
			return "", f.AgentNote
		}
		return "", "The ssh-agent has no key. Add one with ssh-add."
	case f.Fingerprint != "":
		for _, k := range f.Agent {
			if k.Fingerprint == f.Fingerprint {
				return k.Line, ""
			}
		}
		return "", "That key is not in the ssh-agent any more."
	case len(f.Agent) == 1:
		return f.Agent[0].Line, ""
	}
	return "", "The ssh-agent has several keys: choose the one to show."
}

// PublicLineOfKeyFile is the line of the public key of a private key file: the .pub file beside it when there is one, else the public
// key read from the key file. The .pub file is the user's own record of the key, with its comment. It fails with the tags of
// sshlogin.ReadPublicKeyFile for a .pub file that cannot be used, and of sshlogin.PublicKeyOfKeyFile for the key file.
func PublicLineOfKeyFile(path string, ask sshlogin.PassphraseFunc) (string, error) {
	line, err := sshlogin.ReadPublicKeyFile(path + ".pub")
	if err == nil {
		return line, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	pub, err := sshlogin.PublicKeyOfKeyFile(path, ask)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub))), nil
}

// RowOptions are what Rows needs of the tab.
type RowOptions struct {
	// Disabled dims the controls (a connection is being made).
	Disabled bool
	// Submit is called when Enter is pressed in the box of the key file, and Choose when the button next to it is pressed (the dialog of
	// the desktop; it sets the box).
	Submit func(string)
	Choose func()
}

// Rows are the controls of the choice: the label "Key", the drop-down of the way, and the box of the key file with Choose... or the
// keys of the ssh-agent.
func (f *Form) Rows(bc widgets.BuildContext, colors ui.Colors, o RowOptions) []widgets.Widget {
	label := ui.TextStyle(bc, 12, colors.DescriptionForeground)
	changed := func(string) { f.env.Changed() }
	items := []widgets.Widget{
		widgets.NewText("Key", widgets.TextOpts{Style: label}), pageui.Gap(4),
		ui.NewDropdown([]ui.DropdownOption{{Label: "Private key file"}, {Label: "SSH agent"}}, ui.DropdownOpts{
			Selected: map[string]int{File: 0, Agent: 1}[f.Source], Disabled: o.Disabled, Width: foundation.Some(220.0),
			OnChanged: func(i int) {
				f.Source = []string{File, Agent}[i]
				if f.Source == Agent {
					f.ListAgent()
				}
				f.env.Changed()
				f.env.Refresh()
			},
		}),
		pageui.Gap(8),
	}
	if f.Source == Agent {
		return append(items, f.agentRow(bc, colors, o.Disabled))
	}
	return append(items, widgets.NewRow([]widgets.Widget{
		widgets.NewExpanded(ui.NewInputBox(nil, nil, ui.InputBoxOpts{Controller: f.KeyFile, Disabled: o.Disabled, Placeholder: "/path/to/id_ed25519", OnSubmit: o.Submit, OnChanged: changed})),
		pageui.HGap(8), ui.NewButton("Choose...", ui.ButtonOpts{Secondary: true, Disabled: o.Disabled, OnPressed: o.Choose}),
	}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}))
}

// agentRow is the choice among the keys of the ssh-agent.
func (f *Form) agentRow(bc widgets.BuildContext, colors ui.Colors, disabled bool) widgets.Widget {
	dim := ui.TextStyle(bc, pageui.BodySize, colors.DescriptionForeground)
	options := []ui.DropdownOption{{Label: "Any key of the agent"}}
	selected := 0
	for i, k := range f.Agent {
		options = append(options, ui.DropdownOption{Label: k.Label})
		if k.Fingerprint == f.Fingerprint {
			selected = i + 1
		}
	}
	rows := []widgets.Widget{ui.NewDropdown(options, ui.DropdownOpts{
		Selected: selected, Disabled: disabled || f.AgentLoading || len(f.Agent) == 0 && f.AgentNote != "", Width: foundation.Some(520.0),
		OnChanged: func(i int) {
			f.Fingerprint = ""
			if i > 0 {
				f.Fingerprint = f.Agent[i-1].Fingerprint
			}
			f.env.Changed()
			f.env.Refresh()
		},
	})}
	switch {
	case f.AgentLoading:
		rows = append(rows, pageui.Gap(6), widgets.NewText("Reading the keys of the ssh-agent...", widgets.TextOpts{Style: dim}))
	case f.AgentNote != "":
		rows = append(rows, pageui.Gap(6), widgets.NewText(f.AgentNote, widgets.TextOpts{Style: ui.TextStyle(bc, pageui.BodySize, colors.ErrorForeground)}))
	}
	return widgets.NewColumn(rows, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart})
}

// QRSlot is the Show My Public Key as QR Code button under the choice of the key, with what it is for.
func QRSlot(bc widgets.BuildContext, onPressed func(), disabled bool) widgets.Widget {
	colors := ui.Theme(bc)
	return widgets.NewRow([]widgets.Widget{
		ui.NewButton("Show My Public Key as QR Code", ui.ButtonOpts{Secondary: true, Disabled: disabled, OnPressed: onPressed}), pageui.HGap(10),
		widgets.NewExpanded(widgets.NewText("to scan with the Scan QR Code of the other computer, where it is added to the keys that may log in", widgets.TextOpts{Style: ui.TextStyle(bc, 12, colors.DescriptionForeground)})),
	}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
}
