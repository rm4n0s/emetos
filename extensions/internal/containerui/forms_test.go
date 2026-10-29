package containerui

import (
	"reflect"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
)

// wantRoute fails the test unless err is an error of this module whose route has the given part (CLAUDE.md "Testing errors").
func wantRoute(t *testing.T, err error, route string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute(route) {
		t.Errorf("unexpected failure path: %s, want %s", appErr.Route(), route)
	}
}

func TestParsePorts(t *testing.T) {
	tests := []struct {
		name, in string
		want     []containerapi.PortMapping
		route    string
	}{
		{"nothing", "", nil, ""},
		{"host and container", "8080:80", []containerapi.PortMapping{{HostPort: 8080, ContainerPort: 80, Protocol: "tcp"}}, ""},
		{"an address", "127.0.0.1:8080:80/tcp", []containerapi.PortMapping{{HostIP: "127.0.0.1", HostPort: 8080, ContainerPort: 80, Protocol: "tcp"}}, ""},
		{"udp", "53:53/udp", []containerapi.PortMapping{{HostPort: 53, ContainerPort: 53, Protocol: "udp"}}, ""},
		{"a container port alone", "5432", []containerapi.PortMapping{{ContainerPort: 5432, Protocol: "tcp"}}, ""},
		{"blank lines and spaces", "\n  8080:80  \n\n9090:90\n", []containerapi.PortMapping{
			{HostPort: 8080, ContainerPort: 80, Protocol: "tcp"}, {HostPort: 9090, ContainerPort: 90, Protocol: "tcp"}}, ""},
		{"a word", "web", nil, "parsePorts.ContainerUIPortInvalid"},
		{"too large", "70000:80", nil, "parsePorts.ContainerUIPortInvalid"},
		{"zero", "0:80", nil, "parsePorts.ContainerUIPortInvalid"},
		{"too many parts", "1.2.3.4:5:80:90", nil, "parsePorts.ContainerUIPortInvalid"},
		{"an empty address", ":8080:80", nil, "parsePorts.ContainerUIPortInvalid"},
		{"a protocol that does not exist", "80/icmp", nil, "parsePorts.ContainerUIPortInvalid"},
	}
	for _, tc := range tests {
		got, err := parsePorts(tc.in)
		if tc.route != "" {
			if err == nil {
				t.Errorf("%s: no error", tc.name)
				continue
			}
			wantRoute(t, err, tc.route)
			continue
		}
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %v, %v; want %v", tc.name, got, err, tc.want)
		}
	}
}

func TestParseEnv(t *testing.T) {
	tests := []struct {
		name, in string
		want     []string
		route    string
	}{
		{"nothing", "", nil, ""},
		{"two", "A=1\n B=two words \n", []string{"A=1", "B=two words"}, ""},
		{"an empty value", "A=", []string{"A="}, ""},
		{"a value with an equals sign", "URL=http://x?a=b", []string{"URL=http://x?a=b"}, ""},
		{"no equals sign", "A", nil, "parseEnv.ContainerUIEnvInvalid"},
		{"no name", "=1", nil, "parseEnv.ContainerUIEnvInvalid"},
		{"a space in the name", "A B=1", nil, "parseEnv.ContainerUIEnvInvalid"},
	}
	for _, tc := range tests {
		got, err := parseEnv(tc.in)
		if tc.route != "" {
			if err == nil {
				t.Errorf("%s: no error", tc.name)
			} else {
				wantRoute(t, err, tc.route)
			}
			continue
		}
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %v, %v; want %v", tc.name, got, err, tc.want)
		}
	}
}

func TestParseBinds(t *testing.T) {
	tests := []struct {
		name, in string
		want     []string
		route    string
	}{
		{"nothing", "", nil, ""},
		{"a volume", "data:/var/lib/data", []string{"data:/var/lib/data"}, ""},
		{"a folder, read only", "/srv/site:/usr/share/nginx/html:ro", []string{"/srv/site:/usr/share/nginx/html:ro"}, ""},
		{"no path in the container", "data", nil, "parseBinds.ContainerUIVolumeInvalid"},
		{"a relative path in the container", "data:var", nil, "parseBinds.ContainerUIVolumeInvalid"},
		{"no source", ":/x", nil, "parseBinds.ContainerUIVolumeInvalid"},
		{"too many parts", "a:/b:ro:x", nil, "parseBinds.ContainerUIVolumeInvalid"},
	}
	for _, tc := range tests {
		got, err := parseBinds(tc.in)
		if tc.route != "" {
			if err == nil {
				t.Errorf("%s: no error", tc.name)
			} else {
				wantRoute(t, err, tc.route)
			}
			continue
		}
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %v, %v; want %v", tc.name, got, err, tc.want)
		}
	}
}

func TestSplitCommand(t *testing.T) {
	tests := []struct {
		name, in string
		want     []string
		route    string
	}{
		{"nothing", "  ", nil, ""},
		{"words", "sleep  60", []string{"sleep", "60"}, ""},
		{"double quotes", `sh -c "echo hi there"`, []string{"sh", "-c", "echo hi there"}, ""},
		{"single quotes keep a backslash", `echo 'a\b'`, []string{"echo", `a\b`}, ""},
		{"an escaped space", `echo a\ b`, []string{"echo", "a b"}, ""},
		{"an empty quoted word", `echo ""`, []string{"echo", ""}, ""},
		{"a quote that is not closed", `echo "hi`, nil, "splitCommand.ContainerUICommandInvalid"},
		{"a backslash at the end", `echo a\`, nil, "splitCommand.ContainerUICommandInvalid"},
	}
	for _, tc := range tests {
		got, err := splitCommand(tc.in)
		if tc.route != "" {
			if err == nil {
				t.Errorf("%s: no error", tc.name)
			} else {
				wantRoute(t, err, tc.route)
			}
			continue
		}
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %q, %v; want %q", tc.name, got, err, tc.want)
		}
	}
}
