package hosting

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rm4n0s/emetos/extension"
	common "github.com/rm4n0s/emetos/extensions/internal/sharing"
)

// The ids of the extension.
const (
	// ID is the id of the extension (SPEC §10.29; "emetos.sharing" is kept for the events of Emetos, so the extension is emetos.hosting).
	ID = "emetos.hosting"
	// ItemID is the id of its status bar item among the items of the extension; the bar knows it as "extension.emetos.hosting.status".
	ItemID = "status"
	// MenuCommand opens the menu of the item from the Command Palette. A client of a shared workspace may not run it (it starts with "emetos.hosting.").
	MenuCommand = "emetos.hosting.menu"
)

// priority puts the item at the left end of the status bar, before the problems item (VS Code puts its remote indicator there).
const priority = 100

// The texts of the item.
const (
	idleText    = "$(broadcast)"
	hostingText = "$(broadcast) Hosting"
	idleTip     = "This workspace is not shared. Click to host it."
	noFolderTip = "This workspace has no folder, so it cannot be shared. Open a folder first."
)

// NewFactory makes the Factory of the extension over the extensions that host workspaces, in the order the menu lists them. The
// instances it makes, one for each workspace, share them.
func NewFactory(hosts ...common.Host) extension.Factory {
	return func() extension.Extension { return &hostingExt{hosts: hosts} }
}

// sharing is the extension in one workspace.
type hostingExt struct {
	hosts []common.Host
	api   extension.API
	item  extension.StatusHandle
	// folder is the folder of the workspace, empty for one with none.
	folder string
}

// Manifest implements extension.Extension.
func (*hostingExt) Manifest() extension.Manifest {
	return extension.Manifest{
		ID: ID, Name: "Sharing", Version: "0.1.0",
		Description: "Shows in the status bar whether Remote Work hosts the workspace, and starts and stops it from one menu.",
		// the item must be there from the start: it says that the workspace is shared
		ActivationEvents: []string{extension.OnStartup},
		Contributes: extension.Contributes{
			Commands: []extension.CommandSpec{{
				ID: MenuCommand, Title: "Show Hosting Menu", Category: "Sharing",
				Description: "Opens the menu of the Sharing status item: host this workspace with Remote Work, stop, disconnect a computer.",
			}},
		},
	}
}

// Activate implements extension.Extension. The status bar belongs to workspaces; a window tab of an extension has none.
func (s *hostingExt) Activate(ctx context.Context, api extension.API) error {
	s.api = api
	if api.Scope().Kind != extension.ScopeWorkspace {
		return nil
	}
	if f := api.Scope().Folder; f != "" {
		s.folder = filepath.Clean(f)
	}
	if err := api.Commands().Handle(MenuCommand, func(context.Context, any) error { s.menu(); return nil }); err != nil {
		return err
	}
	item, err := api.StatusBar().Add(s.entry())
	if err != nil {
		return err
	}
	s.item = item
	for _, h := range s.hosts {
		stop := h.OnChange(func() { api.Post(s.refresh) })
		context.AfterFunc(ctx, stop)
	}
	return nil
}

// states reads how each host hosts the workspace, in the order of the hosts.
func (s *hostingExt) states() []common.State {
	out := make([]common.State, len(s.hosts))
	for i, h := range s.hosts {
		out[i] = h.State(s.folder)
	}
	return out
}

// refresh shows the state now. The UI thread.
func (s *hostingExt) refresh() {
	if s.item != nil {
		s.item.Update(s.entry())
	}
}

// entry is the item for the state now.
func (s *hostingExt) entry() extension.StatusItem {
	item := extension.StatusItem{
		ID: ItemID, Name: "Sharing", Text: idleText, Tooltip: idleTip, Alignment: extension.StatusLeft, Priority: priority, OnClick: s.menu,
	}
	if s.folder == "" {
		item.Tooltip = noFolderTip
		return item
	}
	var parts []string
	connected := 0
	for i, st := range s.states() {
		if !st.On {
			continue
		}
		connected += len(st.Computers)
		parts = append(parts, describe(s.hosts[i].Name(), st))
	}
	if len(parts) == 0 {
		return item
	}
	item.Kind = extension.StatusRemote
	item.Text = hostingText
	if connected > 0 {
		item.Text += " · " + strconv.Itoa(connected)
	}
	item.Tooltip = strings.Join(parts, " ")
	return item
}

// describe is the part of the tooltip about one extension: what hosts the workspace, on which port, and who is connected.
func describe(name string, st common.State) string {
	switch {
	case st.Problem != "":
		return name + " cannot listen: " + st.Problem + "."
	case st.Port == 0:
		return name + " is starting to listen."
	}
	return name + " on port " + strconv.Itoa(st.Port) + ": " + common.Computers(st.Computers) + "."
}

// menu opens the quick pick of the item. The UI thread.
func (s *hostingExt) menu() {
	if s.folder == "" {
		s.api.Notifications().Warn(noFolderTip)
		return
	}
	items, actions := menuItems(s.hosts, s.states())
	err := s.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: "Host this workspace, or stop hosting it", Items: items}, func(index int) {
		if index >= 0 && index < len(actions) && actions[index] != nil {
			actions[index]()
		}
	})
	if err != nil {
		s.api.Log().Warn("the menu of the Sharing item cannot be shown", "err", err)
	}
}

// menuItems are the rows of the menu for the states, and for each row what to do besides running its command (a Disconnect row).
func menuItems(hosts []common.Host, states []common.State) ([]extension.QuickPickItem, []func()) {
	var items []extension.QuickPickItem
	var actions []func()
	add := func(item extension.QuickPickItem, action func()) {
		items = append(items, item)
		actions = append(actions, action)
	}
	for i, h := range hosts {
		if states[i].On {
			add(extension.QuickPickItem{Label: "$(stop-circle) Stop Hosting with " + h.Name(), Description: "Ends the sessions of this workspace", Command: h.StopCommand()}, nil)
		} else {
			add(extension.QuickPickItem{Label: "$(broadcast) Host with " + h.Name(), Description: h.Description(), Command: h.HostCommand()}, nil)
		}
	}
	for i, h := range hosts {
		for _, c := range states[i].Computers {
			c := c
			add(extension.QuickPickItem{Label: "$(debug-disconnect) Disconnect " + c.Name, Description: h.Name(), Detail: "Ends this computer's session; its key stays allowed"}, c.Disconnect)
		}
	}
	for _, h := range hosts {
		add(extension.QuickPickItem{Label: "$(settings-gear) " + h.Name() + " Settings...", Description: "Keys, port and the guide", Command: h.SettingsCommand()}, nil)
	}
	return items, actions
}
