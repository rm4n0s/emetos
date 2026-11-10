//go:build !linux

package sharing

import "testing"

func TestParseRouteInterface(t *testing.T) {
	tests := []struct {
		name, text, want string
	}{
		{"a route", "   route to: default\ndestination: default\n       mask: default\n    gateway: 192.168.1.1\n  interface: en0\n      flags: <UP,GATEWAY,DONE,STATIC,PRCLONING,GLOBAL>\n", "en0"},
		{"no interface", "route: writing to routing socket: not in table\n", ""},
		{"nothing", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseRouteInterface(tt.text); got != tt.want {
				t.Errorf("parseRouteInterface = %q, want %q", got, tt.want)
			}
		})
	}
}
