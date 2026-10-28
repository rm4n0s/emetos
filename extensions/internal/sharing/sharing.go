// Package sharing is what Remote Work, Relay and the Sharing status item (extensions/sharing) have in common (SPEC §10.29): the
// state of hosting a workspace as the item shows it, the interface the two hosting extensions implement for it, and the addresses
// of this computer that another computer connects to.
package sharing

import (
	"net"
	"net/netip"
	"os"
	"sort"
	"strconv"
	"strings"
)

// Computer is a computer connected to a hosted workspace.
type Computer struct {
	// Name is the comment of the key it logged in with (its fingerprint when the key has none).
	Name string
	// Disconnect ends its session. It may be called from any goroutine.
	Disconnect func()
}

// State is how one extension hosts one workspace now.
type State struct {
	// On says that the extension hosts the workspace.
	On bool
	// Port is the port the extension listens on while it listens, 0 when it does not.
	Port int
	// Problem says why the extension cannot host although it is on (it cannot listen); empty when all is well.
	Problem string
	// Computers are the computers connected to the workspace through the extension.
	Computers []Computer
}

// Host is an extension that hosts workspaces, as the Sharing item sees it. The two implementations are made by the hubs of Remote
// Work and Relay (Hub.Hosting).
type Host interface {
	// Name is how the item calls it: "Remote Work" or "Relay".
	Name() string
	// Description is the line under its Host entry in the pick of the item.
	Description() string
	// HostCommand and StopCommand are the ids of the commands that start and stop hosting the workspace of the window, and
	// SettingsCommand the one that opens its page or tab.
	HostCommand() string
	StopCommand() string
	SettingsCommand() string
	// State reads how the workspace with the folder is hosted. A folder that is empty (a workspace with none) is never hosted.
	State(folder string) State
	// OnChange calls fn, from any goroutine, after the state changed; the function it returns stops the calls.
	OnChange(fn func()) (stop func())
}

// PortOf is the port of the address a listener is on; fallback when the address is nil or has none.
func PortOf(addr net.Addr, fallback int) int {
	if tcp, ok := addr.(*net.TCPAddr); ok && tcp.Port != 0 {
		return tcp.Port
	}
	return fallback
}

// Computers counts the computers of a state in words: "nobody connected", "1 computer connected (laptop)", "2 computers connected
// (laptop, desktop)".
func Computers(list []Computer) string {
	switch len(list) {
	case 0:
		return "nobody connected"
	case 1:
		return "1 computer connected (" + list[0].Name + ")"
	}
	names := ""
	for i, c := range list {
		if i > 0 {
			names += ", "
		}
		names += c.Name
	}
	return strconv.Itoa(len(list)) + " computers connected (" + names + ")"
}

// Interface is a network interface of this computer, reduced to what the addresses need.
type Interface struct {
	// Name is its name ("eth0").
	Name string
	// Up says that it is up, and Loopback that it is a loopback interface.
	Up, Loopback bool
	// DefaultRoute says that the default route of the computer goes out through it.
	DefaultRoute bool
	// Addrs are its addresses, as net.Interface.Addrs gives them (*net.IPNet or *net.IPAddr).
	Addrs []net.Addr
}

// Interfaces lists the network interfaces of this computer; NetInterfaces is the real one and a test passes a fake.
type Interfaces func() ([]Interface, error)

// NetInterfaces reads the interfaces from the operating system, and marks the ones the default route goes out through.
func NetInterfaces() ([]Interface, error) {
	list, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	defaults := defaultRouteInterfaces()
	out := make([]Interface, 0, len(list))
	for _, ifc := range list {
		addrs, err := ifc.Addrs()
		if err != nil {
			continue // an interface that cannot say its addresses is left out
		}
		out = append(out, Interface{
			Name: ifc.Name, Up: ifc.Flags&net.FlagUp != 0, Loopback: ifc.Flags&net.FlagLoopback != 0,
			DefaultRoute: defaults[ifc.Name], Addrs: addrs,
		})
	}
	return out, nil
}

// defaultRouteInterfaces names the interfaces of the default routes of Linux (IPv4 and IPv6) that have the lowest metric. A table
// that cannot be read gives no interface: the addresses are then in plain order.
func defaultRouteInterfaces() map[string]bool {
	out := map[string]bool{}
	if data, err := os.ReadFile("/proc/net/route"); err == nil {
		for _, name := range parseDefaultRoutes4(string(data)) {
			out[name] = true
		}
	}
	if data, err := os.ReadFile("/proc/net/ipv6_route"); err == nil {
		for _, name := range parseDefaultRoutes6(string(data)) {
			out[name] = true
		}
	}
	return out
}

// Route flags of the kernel that matter here (linux/route.h).
const (
	rtfUp     = 0x1
	rtfReject = 0x200
)

// parseDefaultRoutes4 reads the text of /proc/net/route: the interfaces of the routes that are up with destination and mask 0,
// of the lowest metric.
func parseDefaultRoutes4(text string) []string {
	var best []string
	bestMetric := int64(-1)
	for i, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		// Iface Destination Gateway Flags RefCnt Use Metric Mask MTU Window IRTT
		if i == 0 || len(f) < 8 || f[1] != "00000000" || f[7] != "00000000" {
			continue
		}
		flags, err := strconv.ParseInt(f[3], 16, 64)
		if err != nil || flags&rtfUp == 0 {
			continue
		}
		metric, err := strconv.ParseInt(f[6], 10, 64)
		if err != nil {
			continue
		}
		best, bestMetric = lowest(best, bestMetric, f[0], metric)
	}
	return best
}

// parseDefaultRoutes6 reads the text of /proc/net/ipv6_route: the interfaces of the routes that are up and not rejecting with a
// destination of ::/0, of the lowest metric.
func parseDefaultRoutes6(text string) []string {
	var best []string
	bestMetric := int64(-1)
	zero := strings.Repeat("0", 32)
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		// dest prefixlen src srcprefix nexthop metric refcnt use flags iface
		if len(f) < 10 || f[0] != zero || f[1] != "00" {
			continue
		}
		flags, err := strconv.ParseInt(f[8], 16, 64)
		if err != nil || flags&rtfUp == 0 || flags&rtfReject != 0 {
			continue
		}
		metric, err := strconv.ParseInt(f[5], 16, 64)
		if err != nil {
			continue
		}
		best, bestMetric = lowest(best, bestMetric, f[9], metric)
	}
	return best
}

// lowest keeps the interfaces of the lowest metric: name at metric joins them, replaces them, or is dropped.
func lowest(best []string, bestMetric int64, name string, metric int64) ([]string, int64) {
	switch {
	case bestMetric < 0 || metric < bestMetric:
		return []string{name}, metric
	case metric == bestMetric:
		return append(best, name), bestMetric
	}
	return best, bestMetric
}

// bridgePrefixes are the names of the interfaces that containers and virtual machines make on this computer; another computer
// cannot reach it through them.
var bridgePrefixes = []string{"docker", "br-", "virbr", "veth", "cni", "flannel", "podman", "lxcbr", "lxdbr"}

// bridgeRanges are the ranges that only such bridges use: the default networks of Docker, libvirt and Podman, and the pod network
// of flannel. They tell a bridge whose name says nothing.
var bridgeRanges = []netip.Prefix{
	netip.MustParsePrefix("172.17.0.0/16"), netip.MustParsePrefix("192.168.122.0/24"),
	netip.MustParsePrefix("10.88.0.0/16"), netip.MustParsePrefix("10.244.0.0/16"),
}

// IsBridge says whether the interface looks like the bridge of a container engine or a virtual machine manager: its name begins
// with docker, br-, virbr, veth, cni, flannel, podman, lxcbr or lxdbr, or an address of it is in a range only such bridges use.
func IsBridge(ifc Interface) bool {
	for _, prefix := range bridgePrefixes {
		if strings.HasPrefix(ifc.Name, prefix) {
			return true
		}
	}
	for _, a := range ifc.Addrs {
		if addr, ok := addrOf(a); ok {
			for _, r := range bridgeRanges {
				if r.Contains(addr) {
					return true
				}
			}
		}
	}
	return false
}

// addrOf takes the address of a net.Addr, with an IPv4 inside IPv6 turned to IPv4 and without a zone.
func addrOf(a net.Addr) (netip.Addr, bool) {
	var ip net.IP
	switch v := a.(type) {
	case *net.IPNet:
		ip = v.IP
	case *net.IPAddr:
		ip = v.IP
	}
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return netip.Addr{}, false
	}
	return addr.Unmap().WithZone(""), true
}

// Addresses lists the addresses another computer can connect to, each with the port ("192.168.1.5:47123"), in this order:
//
//   - the addresses of the interface the default route goes out through, IPv4 first;
//   - then the addresses of the other interfaces that are up, not loopback and not the bridge of a container engine or a virtual
//     machine manager (IsBridge), IPv4 first;
//   - when no such address exists, every address of an interface that is up and not loopback, in the same order, so the list is
//     never empty on a computer that has a network.
//
// Link-local, multicast and unspecified addresses are never listed, an address once, and each group is sorted. A list that cannot
// be read gives none.
func Addresses(list Interfaces, port int) []string {
	if list == nil {
		list = DefaultInterfaces()
	}
	ifaces, err := list()
	if err != nil {
		return nil
	}
	out := addressesOf(ifaces, port, true)
	if len(out) == 0 {
		out = addressesOf(ifaces, port, false)
	}
	return out
}

// addressesOf lists the usable addresses of the interfaces that are up and not loopback, without the bridges when noBridges is
// set, the default route's first, IPv4 before IPv6, each group sorted.
func addressesOf(ifaces []Interface, port int, noBridges bool) []string {
	type entry struct {
		text  string
		other bool // not the default route's
		v6    bool
	}
	var entries []entry
	seen := map[netip.Addr]bool{}
	for _, ifc := range ifaces {
		if !ifc.Up || ifc.Loopback || (noBridges && !ifc.DefaultRoute && IsBridge(ifc)) {
			continue
		}
		for _, a := range ifc.Addrs {
			addr, ok := addrOf(a)
			if !ok || seen[addr] || addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsMulticast() || addr.IsUnspecified() {
				continue
			}
			seen[addr] = true
			entries = append(entries, entry{net.JoinHostPort(addr.String(), strconv.Itoa(port)), !ifc.DefaultRoute, addr.Is6()})
		}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.other != b.other {
			return !a.other
		}
		if a.v6 != b.v6 {
			return !a.v6
		}
		return a.text < b.text
	})
	var out []string
	for _, e := range entries {
		out = append(out, e.text)
	}
	return out
}
