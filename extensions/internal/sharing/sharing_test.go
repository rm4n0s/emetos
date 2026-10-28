package sharing

import (
	"net"
	"reflect"
	"testing"

	"github.com/rm4n0s/errors"
)

func ipnet(ip string, bits int) net.Addr {
	parsed := net.ParseIP(ip)
	size := 32
	if parsed.To4() == nil {
		size = 128
	}
	return &net.IPNet{IP: parsed, Mask: net.CIDRMask(bits, size)}
}

func TestAddressesAreTheOnesAnotherComputerCanConnectTo(t *testing.T) {
	tests := []struct {
		name string
		list []Interface
		err  error
		want []string
	}{
		{"nothing", nil, nil, nil},
		{"a failure gives none", nil, errors.New("FakeInterfacesFailed", "no interfaces"), nil},
		{"a home network", []Interface{
			{Name: "lo", Up: true, Loopback: true, Addrs: []net.Addr{ipnet("127.0.0.1", 8), ipnet("::1", 128)}},
			{Name: "eth0", Up: true, Addrs: []net.Addr{ipnet("192.168.1.5", 24), ipnet("fe80::1", 64)}},
		}, nil, []string{"192.168.1.5:47123"}},
		{"an interface that is down is left out", []Interface{
			{Name: "eth0", Up: true, Addrs: []net.Addr{ipnet("192.168.1.5", 24)}},
			{Name: "wlan0", Up: false, Addrs: []net.Addr{ipnet("10.0.0.7", 8)}},
		}, nil, []string{"192.168.1.5:47123"}},
		{"IPv4 first, each sorted, the same address once", []Interface{
			{Name: "a", Up: true, Addrs: []net.Addr{ipnet("2001:db8::2", 64), ipnet("192.168.1.9", 24), &net.IPAddr{IP: net.ParseIP("10.0.0.2")}}},
			{Name: "b", Up: true, Addrs: []net.Addr{ipnet("10.0.0.2", 8), ipnet("2001:db8::1", 64)}},
		}, nil, []string{"10.0.0.2:47123", "192.168.1.9:47123", "[2001:db8::1]:47123", "[2001:db8::2]:47123"}},
		{"multicast, unspecified and an IPv4 inside IPv6 are handled", []Interface{
			{Name: "a", Up: true, Addrs: []net.Addr{ipnet("224.0.0.1", 4), ipnet("0.0.0.0", 0), ipnet("::ffff:192.168.7.7", 120), &net.UnixAddr{Name: "x", Net: "unix"}}},
		}, nil, []string{"192.168.7.7:47123"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Addresses(func() ([]Interface, error) { return tc.list, tc.err }, 47123)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Addresses = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTheGuideListsTheDefaultRouteFirstAndLeavesTheBridgesOut(t *testing.T) {
	lo := Interface{Name: "lo", Up: true, Loopback: true, Addrs: []net.Addr{ipnet("127.0.0.1", 8)}}
	tests := []struct {
		name string
		list []Interface
		want []string
	}{
		{"the default route comes first, then the other physical addresses, IPv4 first", []Interface{
			lo,
			{Name: "wlan0", Up: true, Addrs: []net.Addr{ipnet("2001:db8::7", 64), ipnet("198.51.100.7", 24)}},
			{Name: "enp3s0", Up: true, DefaultRoute: true, Addrs: []net.Addr{ipnet("2001:db8::10", 64), ipnet("192.0.2.10", 24)}},
			{Name: "eth1", Up: true, Addrs: []net.Addr{ipnet("203.0.113.3", 24)}},
		}, []string{"192.0.2.10:47123", "[2001:db8::10]:47123", "198.51.100.7:47123", "203.0.113.3:47123", "[2001:db8::7]:47123"}},
		{"no default route: plain order", []Interface{
			{Name: "wlan0", Up: true, Addrs: []net.Addr{ipnet("198.51.100.7", 24)}},
			{Name: "enp3s0", Up: true, Addrs: []net.Addr{ipnet("192.0.2.10", 24)}},
		}, []string{"192.0.2.10:47123", "198.51.100.7:47123"}},
		{"bridges of containers and virtual machines are left out by their names", []Interface{
			{Name: "docker0", Up: true, Addrs: []net.Addr{ipnet("203.0.113.1", 16)}},
			{Name: "br-3f2a9c", Up: true, Addrs: []net.Addr{ipnet("203.0.113.2", 16)}},
			{Name: "virbr0", Up: true, Addrs: []net.Addr{ipnet("203.0.113.3", 24)}},
			{Name: "veth12ab", Up: true, Addrs: []net.Addr{ipnet("203.0.113.4", 24)}},
			{Name: "cni0", Up: true, Addrs: []net.Addr{ipnet("203.0.113.5", 24)}},
			{Name: "flannel.1", Up: true, Addrs: []net.Addr{ipnet("203.0.113.6", 24)}},
			{Name: "podman1", Up: true, Addrs: []net.Addr{ipnet("203.0.113.7", 24)}},
			{Name: "enp3s0", Up: true, DefaultRoute: true, Addrs: []net.Addr{ipnet("192.0.2.10", 24)}},
		}, []string{"192.0.2.10:47123"}},
		{"bridges are left out by the range of their addresses when the name says nothing", []Interface{
			{Name: "netA", Up: true, Addrs: []net.Addr{ipnet("172.17.0.5", 16)}},
			{Name: "netB", Up: true, Addrs: []net.Addr{ipnet("192.168.122.5", 24)}},
			{Name: "netC", Up: true, Addrs: []net.Addr{ipnet("10.88.0.1", 16)}},
			{Name: "netD", Up: true, Addrs: []net.Addr{ipnet("10.244.0.1", 24)}},
			{Name: "enp3s0", Up: true, Addrs: []net.Addr{ipnet("192.0.2.10", 24)}},
		}, []string{"192.0.2.10:47123"}},
		{"a LAN in 172.16.0.0/12 outside the range of Docker is not a bridge", []Interface{
			{Name: "enp3s0", Up: true, Addrs: []net.Addr{ipnet("172.20.1.9", 16)}},
		}, []string{"172.20.1.9:47123"}},
		{"the interface of the default route is listed even when it looks like a bridge", []Interface{
			{Name: "br0", Up: true, DefaultRoute: true, Addrs: []net.Addr{ipnet("192.0.2.10", 24)}},
			{Name: "br-1", Up: true, DefaultRoute: true, Addrs: []net.Addr{ipnet("198.51.100.7", 24)}},
		}, []string{"192.0.2.10:47123", "198.51.100.7:47123"}},
		{"only bridges: every address of an interface that is up is listed, so the guide is never empty", []Interface{
			lo,
			{Name: "virbr0", Up: true, Addrs: []net.Addr{ipnet("192.168.122.5", 24)}},
			{Name: "docker0", Up: true, Addrs: []net.Addr{ipnet("172.17.0.5", 16), ipnet("fe80::1", 64)}},
			{Name: "br-1", Up: false, Addrs: []net.Addr{ipnet("172.18.0.5", 16)}},
		}, []string{"172.17.0.5:47123", "192.168.122.5:47123"}},
		{"nothing but loopback and interfaces that are down gives none", []Interface{
			lo, {Name: "wlan0", Up: false, Addrs: []net.Addr{ipnet("198.51.100.7", 24)}},
		}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Addresses(func() ([]Interface, error) { return tc.list, nil }, 47123)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Addresses = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTheDefaultRouteIsReadFromTheKernelsTables(t *testing.T) {
	const route4 = "Iface\tDestination\tGateway \tFlags\tRefCnt\tUse\tMetric\tMask\t\tMTU\tWindow\tIRTT\n" +
		"wlan0\t00000000\t0100A8C0\t0003\t0\t0\t600\t00000000\t0\t0\t0\n" +
		"enp3s0\t00000000\t0100A8C0\t0003\t0\t0\t100\t00000000\t0\t0\t0\n" +
		"docker0\t000011AC\t00000000\t0001\t0\t0\t0\t0000FFFF\t0\t0\t0\n" +
		"tun0\t00000000\t0100A8C0\t0002\t0\t0\t10\t00000000\t0\t0\t0\n" +
		"bad\t00000000\t0100A8C0\tzz\t0\t0\t10\t00000000\t0\t0\t0\n"
	const zero = "00000000000000000000000000000000"
	route6 := zero + " 00 " + zero + " 00 " + zero + " 00000400 00000001 00000000 00000003 eth0\n" +
		zero + " 00 " + zero + " 00 " + zero + " 00000100 00000001 00000000 00000003 wlan0\n" +
		zero + " 00 " + zero + " 00 " + zero + " 00000100 00000001 00000000 00000203 lo\n" +
		"20010db8000000000000000000000000 20 " + zero + " 00 " + zero + " 00000100 00000001 00000000 00000001 br0\n" +
		"short line\n"
	for _, tc := range []struct {
		name  string
		parse func(string) []string
		text  string
		want  []string
	}{
		{"IPv4: the up route with the lowest metric", parseDefaultRoutes4, route4, []string{"enp3s0"}},
		{"IPv4: nothing", parseDefaultRoutes4, "Iface\tDestination\n", nil},
		{"IPv4: equal metrics keep both", parseDefaultRoutes4, "h\na 00000000 0 0001 0 0 5 00000000 0 0 0\nb 00000000 0 0001 0 0 5 00000000 0 0 0\n", []string{"a", "b"}},
		{"IPv6: ::/0, up, not rejecting, the lowest metric", parseDefaultRoutes6, route6, []string{"wlan0"}},
		{"IPv6: nothing", parseDefaultRoutes6, "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.parse(tc.text); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTheDevKnobGivesTheInterfacesOfTheDocumentationRanges(t *testing.T) {
	good := `[{"name":"lo","up":true,"loopback":true,"addrs":["127.0.0.1/8"]},` +
		`{"name":"enp3s0","up":true,"defaultRoute":true,"addrs":["192.0.2.10/24","2001:db8::10/64"]},` +
		`{"name":"docker0","up":true,"addrs":["172.17.0.5/16"]}]`
	list, err := parseDevInterfaces(good)
	if err != nil {
		t.Fatal(err)
	}
	got := Addresses(func() ([]Interface, error) { return list, nil }, 47123)
	if want := []string{"192.0.2.10:47123", "[2001:db8::10]:47123"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Addresses = %v, want %v", got, want)
	}
	t.Setenv(DevInterfacesEnv, good)
	fromEnv, err := DefaultInterfaces()()
	if err != nil || len(fromEnv) != 3 {
		t.Errorf("DefaultInterfaces()() = %v, %v; want the 3 interfaces of the variable (a test binary is a debug build)", fromEnv, err)
	}
	for _, tc := range []struct{ name, text, route string }{
		{"not JSON", "eth0", "parseDevInterfaces.SharingDevInterfacesInvalid"},
		{"an address without a prefix length", `[{"name":"a","up":true,"addrs":["192.0.2.10"]}]`, "parseDevInterfaces.SharingDevInterfacesInvalid"},
	} {
		_, err := parseDevInterfaces(tc.text)
		appErr, ok := errors.FromError(err)
		if !ok {
			t.Fatalf("%s: expected *errors.Error, got %T", tc.name, err)
		}
		if !appErr.HasRoute(tc.route) {
			t.Errorf("%s: unexpected failure path: %s", tc.name, appErr.Route())
		}
	}
}

func TestTheRealInterfacesAreRead(t *testing.T) {
	list, err := NetInterfaces()
	if err != nil {
		t.Skipf("this machine cannot list its interfaces: %v", err)
	}
	for _, a := range Addresses(nil, 1) {
		host, port, err := net.SplitHostPort(a)
		if err != nil || port != "1" || net.ParseIP(host) == nil || net.ParseIP(host).IsLoopback() {
			t.Errorf("address %q of %d interfaces is not one to connect to", a, len(list))
		}
	}
}

func TestComputersCountsThemInWords(t *testing.T) {
	c := func(names ...string) []Computer {
		var out []Computer
		for _, n := range names {
			out = append(out, Computer{Name: n})
		}
		return out
	}
	for _, tc := range []struct {
		list []Computer
		want string
	}{
		{nil, "nobody connected"},
		{c("laptop"), "1 computer connected (laptop)"},
		{c("laptop", "tablet"), "2 computers connected (laptop, tablet)"},
		{c("a", "b", "c"), "3 computers connected (a, b, c)"},
	} {
		if got := Computers(tc.list); got != tc.want {
			t.Errorf("Computers = %q, want %q", got, tc.want)
		}
	}
}

func TestPortOfTakesTheListenersPort(t *testing.T) {
	for _, tc := range []struct {
		addr net.Addr
		want int
	}{
		{&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 5000}, 5000},
		{&net.TCPAddr{IP: net.ParseIP("127.0.0.1")}, 47123},
		{nil, 47123},
		{&net.UnixAddr{Name: "x"}, 47123},
	} {
		if got := PortOf(tc.addr, 47123); got != tc.want {
			t.Errorf("PortOf(%v) = %d, want %d", tc.addr, got, tc.want)
		}
	}
}
