package remotework

import (
	"path/filepath"
	"slices"
	"strconv"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/sharing"
)

// The ids of the commands that host a workspace (SPEC §10.29). They start with "emetos.remotework.", so a client of a shared workspace may not run
// them (command.ClientRefusal).
const (
	// HostCommand turns Remote Work on for the workspace of the window, or opens the page at the guide when no key is listed.
	HostCommand = "emetos.remotework.host"
	// StopCommand turns it off and ends the sessions of the workspace.
	StopCommand = "emetos.remotework.stopHosting"
)

// hosting is the sharing.Host of Remote Work: the Sharing status item reads the state of a workspace from it.
type hosting struct{ hub *Hub }

// Hosting returns the view of the hub that the Sharing status item uses.
func (h *Hub) Hosting() sharing.Host { return hosting{h} }

// Name implements sharing.Host.
func (hosting) Name() string { return "Remote Work" }

// Description implements sharing.Host.
func (hosting) Description() string { return "Allowed computers see this window and control it" }

// HostCommand implements sharing.Host.
func (hosting) HostCommand() string { return HostCommand }

// StopCommand implements sharing.Host.
func (hosting) StopCommand() string { return StopCommand }

// SettingsCommand implements sharing.Host.
func (hosting) SettingsCommand() string { return ManageCommand }

// State implements sharing.Host.
func (g hosting) State(folder string) sharing.State { return g.hub.HostingState(folder) }

// OnChange implements sharing.Host.
func (g hosting) OnChange(fn func()) func() { return g.hub.OnChange(fn) }

// HostingState reads how Remote Work hosts the workspace with the folder, for the Sharing status item and the banner. It touches no
// file.
func (h *Hub) HostingState(folder string) sharing.State {
	if folder == "" {
		return sharing.State{}
	}
	folder = filepath.Clean(folder)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.load()
	st := sharing.State{On: h.loadErr == nil && h.cfg.On(folder)}
	if !st.On {
		return st
	}
	if h.srv != nil {
		st.Port = sharing.PortOf(h.addr, h.srvPort)
	}
	if h.listenErr != nil && h.srv == nil {
		st.Problem = h.listenErr.Error()
	}
	var live []*Session
	for s := range h.sessions {
		if s.folder == folder {
			live = append(live, s)
		}
	}
	slices.SortFunc(live, func(a, b *Session) int { return a.Started.Compare(b.Started) })
	for _, s := range live {
		s := s
		st.Computers = append(st.Computers, sharing.Computer{Name: s.Key.Name(), Disconnect: func() { s.End(reasonHost, "the user of the host pressed Disconnect") }})
	}
	return st
}

// Addresses lists the addresses another computer connects to: the non-loopback addresses of this computer with the port of Remote
// Work (the one the listener is on, else the configured one).
func (h *Hub) Addresses() []string {
	h.mu.Lock()
	h.load()
	port := h.cfg.Port
	if h.srv != nil {
		port = sharing.PortOf(h.addr, h.srvPort)
	}
	h.mu.Unlock()
	return sharing.Addresses(h.opts.Interfaces, port)
}

// bannerText is the line of the hosting banner for a number of connected computers.
func bannerText(n int) string {
	switch n {
	case 0:
		return "This workspace is shared with Remote Work (nobody connected)"
	case 1:
		return "This workspace is shared with Remote Work (1 computer connected)"
	}
	return "This workspace is shared with Remote Work (" + strconv.Itoa(n) + " computers connected)"
}

// syncBanners shows, updates or takes away the hosting banner of every workspace the extension is active in, on the UI thread of
// each. The state is read when the banner is made, so the order of the calls does not matter.
func (h *Hub) syncBanners() {
	h.mu.Lock()
	var all []*workspaceHost
	for _, list := range h.workspaces {
		all = append(all, list...)
	}
	h.mu.Unlock()
	for _, w := range all {
		w := w
		w.api.Post(func() { h.syncBanner(w) })
	}
}

// syncBanner is syncBanners for one workspace; it runs on the UI thread of the workspace, which is the only place that touches
// w.banner.
func (h *Hub) syncBanner(w *workspaceHost) {
	h.mu.Lock()
	h.load()
	list := h.workspaces[w.folder]
	want := h.loadErr == nil && h.cfg.On(w.folder) && len(list) > 0 && list[0] == w
	n := 0
	for s := range h.sessions {
		if s.folder == w.folder {
			n++
		}
	}
	h.mu.Unlock()
	switch {
	case want && w.banner == nil:
		b, err := w.api.Banners().Show(w.hostBanner(h, n))
		if err != nil {
			h.log.Warn("Remote Work: the hosting banner was not shown", "err", err)
			return
		}
		w.banner = b
	case want:
		w.banner.Update(w.hostBanner(h, n))
	case w.banner != nil:
		w.banner.Dispose()
		w.banner = nil
	}
}

// hostBanner is the banner that says the workspace is hosted, with the button that stops it.
func (w *workspaceHost) hostBanner(h *Hub, n int) extension.Banner {
	return extension.Banner{
		Text: bannerText(n), Icon: "broadcast",
		Actions: []extension.BannerAction{{Label: "Stop Hosting", Run: func() {
			go func() {
				if err := h.SetWorkspace(w.folder, false); err != nil {
					h.log.Warn("Remote Work: Stop Hosting failed", "err", err)
					w.api.Post(func() { w.api.Notifications().Error("Remote Work could not stop hosting: " + err.Error()) })
				}
			}()
		}}},
	}
}
