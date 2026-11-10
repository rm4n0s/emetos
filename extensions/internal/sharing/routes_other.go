//go:build !linux

package sharing

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// routeTimeout ends `route` when it does not answer.
const routeTimeout = 3 * time.Second

// defaultRouteInterfaces names the interfaces of the default routes (IPv4 and IPv6) of macOS and the BSDs, which `route -n get default`
// prints as "interface: en0". A route that cannot be asked gives no interface: the addresses are then in plain order.
func defaultRouteInterfaces() map[string]bool {
	out := map[string]bool{}
	for _, family := range []string{"-inet", "-inet6"} {
		ctx, cancel := context.WithTimeout(context.Background(), routeTimeout)
		text, err := exec.CommandContext(ctx, "/sbin/route", "-n", "get", family, "default").Output() //nolint:gosec // a fixed system program
		cancel()
		if err != nil {
			continue
		}
		if name := parseRouteInterface(string(text)); name != "" {
			out[name] = true
		}
	}
	return out
}

// parseRouteInterface reads the interface of the route from the output of `route -n get`: the line "interface: en0".
func parseRouteInterface(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if name, ok := strings.CutPrefix(strings.TrimSpace(line), "interface:"); ok {
			return strings.TrimSpace(name)
		}
	}
	return ""
}
