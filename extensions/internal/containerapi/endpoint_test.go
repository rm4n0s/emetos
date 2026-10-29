package containerapi

import "testing"

func TestParseHost(t *testing.T) {
	for _, tc := range []struct {
		name, host string
		opts       TLSOptions
		network    string
		address    string
		secure     bool
		route      string
	}{
		{name: "a unix url", host: "unix:///var/run/docker.sock", network: "unix", address: "/var/run/docker.sock"},
		{name: "a bare path", host: "/run/podman/podman.sock", network: "unix", address: "/run/podman/podman.sock"},
		{name: "spaces around it", host: "  unix:///x.sock\n", network: "unix", address: "/x.sock"},
		{name: "tcp with a port", host: "tcp://10.0.0.5:2375", network: "tcp", address: "10.0.0.5:2375"},
		{name: "tcp without a port", host: "tcp://engine.example", network: "tcp", address: "engine.example:2375"},
		{name: "http is plain tcp", host: "http://127.0.0.1:8080", network: "tcp", address: "127.0.0.1:8080"},
		{name: "tcp with TLS gets the TLS port", host: "tcp://engine.example", opts: TLSOptions{Verify: true}, network: "tcp", address: "engine.example:2376", secure: true},
		{name: "https is TLS", host: "https://engine.example:2376", network: "tcp", address: "engine.example:2376", secure: true},
		{name: "an IPv6 address", host: "tcp://[::1]:2375", network: "tcp", address: "[::1]:2375"},
		{name: "skip verify is TLS too", host: "tcp://h:1", opts: TLSOptions{SkipVerify: true}, network: "tcp", address: "h:1", secure: true},
		{name: "empty", host: "", route: "ParseHost.ContainerAPIHostInvalid"},
		{name: "no scheme", host: "docker.sock", route: "ParseHost.ContainerAPIHostInvalid"},
		{name: "a relative unix path", host: "unix://run/x.sock", route: "ParseHost.ContainerAPIHostInvalid"},
		{name: "a tcp host with a path", host: "tcp://host:1/api", route: "ParseHost.ContainerAPIHostInvalid"},
		{name: "no host name", host: "tcp://:2375", route: "ParseHost.ContainerAPIHostInvalid"},
		{name: "an unknown scheme", host: "gopher://x", route: "ParseHost.ContainerAPIHostInvalid"},
		{name: "ssh is not built", host: "ssh://me@box", route: "ParseHost.ContainerAPIHostUnsupported"},
		{name: "npipe is not built", host: "npipe:////./pipe/docker_engine", route: "ParseHost.ContainerAPIHostUnsupported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ep, err := ParseHost(tc.host, tc.opts)
			if tc.route != "" {
				wantRoute(t, err, tc.route)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if ep.Network != tc.network || ep.Address != tc.address || ep.Secure() != tc.secure {
				t.Errorf("endpoint = %+v (secure %v), want %s %s (secure %v)", ep, ep.Secure(), tc.network, tc.address, tc.secure)
			}
		})
	}
}

func TestEndpointString(t *testing.T) {
	if got := UnixEndpoint("/a/b.sock").String(); got != "unix:///a/b.sock" {
		t.Errorf("unix = %q", got)
	}
	if got := (Endpoint{Network: "tcp", Address: "h:1"}).String(); got != "tcp://h:1" {
		t.Errorf("tcp = %q", got)
	}
}
