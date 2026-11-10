//go:build linux

package sharing

import "os"

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
