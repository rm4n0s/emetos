package containerui

import (
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
)

func TestHumanSize(t *testing.T) {
	tests := []struct {
		in   int64
		want string
	}{
		{-1, "unknown size"}, {0, "0B"}, {999, "999B"}, {1000, "1kB"}, {1500, "1.5kB"}, {12_345_678, "12.3MB"},
		{187_000_000, "187MB"}, {2_500_000_000, "2.5GB"}, {1_000_000_000_000, "1TB"},
	}
	for _, tc := range tests {
		if got := humanSize(tc.in); got != tc.want {
			t.Errorf("humanSize(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestAge(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		t    time.Time
		want string
	}{
		{"zero", time.Time{}, ""}, {"epoch", time.Unix(0, 0), ""},
		{"seconds", now.Add(-20 * time.Second), "less than a minute ago"},
		{"one minute", now.Add(-time.Minute), "1 minute ago"}, {"minutes", now.Add(-5 * time.Minute), "5 minutes ago"},
		{"hours", now.Add(-3 * time.Hour), "3 hours ago"}, {"a day", now.Add(-25 * time.Hour), "1 day ago"},
		{"days", now.Add(-72 * time.Hour), "3 days ago"}, {"months", now.Add(-70 * 24 * time.Hour), "2 months ago"},
		{"years", now.Add(-800 * 24 * time.Hour), "2 years ago"},
	}
	for _, tc := range tests {
		if got := ago(tc.t, now); got != tc.want {
			t.Errorf("%s: ago = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestStatusText(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		c    containerapi.Container
		want string
	}{
		{"the engine's own sentence", containerapi.Container{State: "running", Status: "Up 2 hours"}, "Up 2 hours"},
		{"running from the start time", containerapi.Container{State: "running", StartedAt: now.Add(-3 * time.Hour)}, "Up 3 hours"},
		{"running, no time", containerapi.Container{State: "running"}, "Up"},
		{"paused", containerapi.Container{State: "paused"}, "Up (Paused)"},
		{"exited", containerapi.Container{State: "exited"}, "Exited"},
		{"created", containerapi.Container{State: "created"}, "Created"},
		{"no state", containerapi.Container{}, ""},
	}
	for _, tc := range tests {
		if got := statusText(tc.c, now); got != tc.want {
			t.Errorf("%s: statusText = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestPortsText(t *testing.T) {
	tests := []struct {
		name  string
		ports []containerapi.Port
		want  string
	}{
		{"none", nil, ""},
		{"published", []containerapi.Port{{IP: "0.0.0.0", PrivatePort: 80, PublicPort: 8080, Type: "tcp"}}, "8080:80/tcp"},
		{"the same port for IPv4 and IPv6 once", []containerapi.Port{
			{IP: "0.0.0.0", PrivatePort: 80, PublicPort: 8080, Type: "tcp"}, {IP: "::", PrivatePort: 80, PublicPort: 8080, Type: "tcp"}}, "8080:80/tcp"},
		{"exposed only", []containerapi.Port{{PrivatePort: 5432, Type: "tcp"}}, "5432/tcp"},
		{"udp and no type", []containerapi.Port{{PrivatePort: 53, PublicPort: 53, Type: "udp"}, {PrivatePort: 22}}, "53:53/udp, 22/tcp"},
	}
	for _, tc := range tests {
		if got := portsText(tc.ports); got != tc.want {
			t.Errorf("%s: portsText = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestPublishedHTTP(t *testing.T) {
	tests := []struct {
		name     string
		c        containerapi.Container
		host     string
		port     uint16
		wantOkay bool
	}{
		{"none", containerapi.Container{}, "", 0, false},
		{"exposed only", containerapi.Container{Ports: []containerapi.Port{{PrivatePort: 80, Type: "tcp"}}}, "", 0, false},
		{"every interface is localhost", containerapi.Container{Ports: []containerapi.Port{{IP: "0.0.0.0", PrivatePort: 80, PublicPort: 8080, Type: "tcp"}}}, "localhost", 8080, true},
		{"loopback stays", containerapi.Container{Ports: []containerapi.Port{{IP: "127.0.0.1", PrivatePort: 80, PublicPort: 8081, Type: "tcp"}}}, "127.0.0.1", 8081, true},
		{"udp is skipped", containerapi.Container{Ports: []containerapi.Port{{PrivatePort: 53, PublicPort: 53, Type: "udp"}, {PrivatePort: 80, PublicPort: 81, Type: "tcp"}}}, "localhost", 81, true},
	}
	for _, tc := range tests {
		host, port, ok := publishedHTTP(tc.c)
		if host != tc.host || port != tc.port || ok != tc.wantOkay {
			t.Errorf("%s: got %q %d %v, want %q %d %v", tc.name, host, port, ok, tc.host, tc.port, tc.wantOkay)
		}
	}
}

func TestSplitRepoTag(t *testing.T) {
	tests := []struct{ in, repo, tag string }{
		{"nginx:1.27", "nginx", "1.27"}, {"nginx", "nginx", ""}, {"ghcr.io/owner/name:v1", "ghcr.io/owner/name", "v1"},
		{"localhost:5000/app", "localhost:5000/app", ""}, {"localhost:5000/app:dev", "localhost:5000/app", "dev"},
	}
	for _, tc := range tests {
		if repo, tag := splitRepoTag(tc.in); repo != tc.repo || tag != tc.tag {
			t.Errorf("splitRepoTag(%q) = %q, %q; want %q, %q", tc.in, repo, tag, tc.repo, tc.tag)
		}
	}
}

func TestGroupContainers(t *testing.T) {
	mk := func(name, project string) containerapi.Container {
		c := containerapi.Container{ID: "id-" + name, Names: []string{name}}
		if project != "" {
			c.Labels = map[string]string{containerapi.LabelCompose: project}
		}
		return c
	}
	names := func(gs []containerGroup) map[string][]string {
		out := map[string][]string{}
		for _, g := range gs {
			for _, c := range g.containers {
				out[g.name] = append(out[g.name], c.Name())
			}
		}
		return out
	}
	tests := []struct {
		name   string
		in     []containerapi.Container
		groups []string
		want   map[string][]string
	}{
		{"nothing", nil, nil, map[string][]string{}},
		{"no project: one flat group with no name", []containerapi.Container{mk("b", ""), mk("a", "")}, []string{""}, map[string][]string{"": {"a", "b"}}},
		{"projects by name, the others last", []containerapi.Container{mk("z", ""), mk("web", "shop"), mk("app", "blog"), mk("db", "shop")},
			[]string{"blog", "shop", otherGroup}, map[string][]string{"blog": {"app"}, "shop": {"db", "web"}, otherGroup: {"z"}}},
		{"only projects", []containerapi.Container{mk("a", "p")}, []string{"p"}, map[string][]string{"p": {"a"}}},
	}
	for _, tc := range tests {
		gs := groupContainers(tc.in, false)
		var got []string
		for _, g := range gs {
			got = append(got, g.name)
		}
		if !reflect.DeepEqual(got, tc.groups) {
			t.Errorf("%s: groups %v, want %v", tc.name, got, tc.groups)
		}
		if !reflect.DeepEqual(names(gs), tc.want) {
			t.Errorf("%s: members %v, want %v", tc.name, names(gs), tc.want)
		}
	}
}

func TestGroupImages(t *testing.T) {
	old, young := time.Unix(1000, 0), time.Unix(2000, 0)
	images := []containerapi.Image{
		{ID: "sha256:a", RepoTags: []string{"nginx:1.27", "nginx:latest"}, Created: old},
		{ID: "sha256:b", RepoTags: []string{"nginx:1.26"}, Created: young},
		{ID: "sha256:c", RepoTags: []string{"<none>:<none>"}},
		{ID: "sha256:d"},
		{ID: "sha256:e", RepoTags: []string{"alpine:3"}},
	}
	tests := []struct {
		name     string
		dangling bool
		want     map[string][]string
		order    []string
	}{
		{"with the dangling ones", true, map[string][]string{"alpine": {"3"}, "nginx": {"1.26", "1.27", "latest"}, noRepository: {"d"}}, []string{"alpine", "nginx", noRepository}},
		{"without", false, map[string][]string{"alpine": {"3"}, "nginx": {"1.26", "1.27", "latest"}}, []string{"alpine", "nginx"}},
	}
	for _, tc := range tests {
		gs := groupImages(images, tc.dangling)
		got := map[string][]string{}
		var order []string
		for _, g := range gs {
			order = append(order, g.repo)
			for _, r := range g.rows {
				got[g.repo] = append(got[g.repo], r.label())
			}
		}
		// the newest image of a repository first
		if len(got["nginx"]) == 3 && got["nginx"][0] != "1.26" {
			t.Errorf("%s: the newest image is not first: %v", tc.name, got["nginx"])
		}
		for _, v := range got {
			sort.Strings(v)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: rows %v, want %v", tc.name, got, tc.want)
		}
		if !reflect.DeepEqual(order, tc.order) {
			t.Errorf("%s: order %v, want %v", tc.name, order, tc.order)
		}
	}
}

func TestMatchAny(t *testing.T) {
	tests := []struct {
		filter string
		fields []string
		want   bool
	}{
		{"", []string{"x"}, true}, {"  ", nil, true}, {"WEB", []string{"shop-web-1"}, true}, {"db", []string{"shop-web-1", "postgres"}, false},
		{"post", []string{"shop-web-1", "postgres:16"}, true},
	}
	for _, tc := range tests {
		if got := matchAny(tc.filter, tc.fields...); got != tc.want {
			t.Errorf("matchAny(%q, %v) = %v, want %v", tc.filter, tc.fields, got, tc.want)
		}
	}
}

func TestPlural(t *testing.T) {
	if plural(1, "container") != "1 container" || plural(0, "container") != "0 containers" || plural(3, "image") != "3 images" {
		t.Error("plural is wrong")
	}
}

func TestBuiltinNetwork(t *testing.T) {
	for name, want := range map[string]bool{"bridge": true, "host": true, "none": true, "podman": true, "shop_default": false} {
		if got := builtinNetwork(containerapi.Network{Name: name}); got != want {
			t.Errorf("builtinNetwork(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestIndentJSON(t *testing.T) {
	got := indentJSON([]byte(`{"b":[1,2],"a":"<x>"}`))
	want := "{\n  \"b\": [\n    1,\n    2\n  ],\n  \"a\": \"<x>\"\n}\n" // the order of the engine, and no escaping of <
	if got != want {
		t.Errorf("indentJSON = %q, want %q", got, want)
	}
	if got := indentJSON([]byte("not json")); got != "not json" {
		t.Errorf("text that is not JSON changed: %q", got)
	}
}

func TestParseListing(t *testing.T) {
	got := parseListing("etc/\nbin/\n./\n../\nREADME\n\nalpha\r\n")
	want := []fileEntry{{"bin", true}, {"etc", true}, {"README", false}, {"alpha", false}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseListing = %v, want %v", got, want)
	}
}

func TestParseFileKey(t *testing.T) {
	tests := []struct {
		key, cid, path string
		dir            bool
	}{
		{keyContainer + "abc", "abc", "/", true}, {fileKey("abc", "/etc/"), "abc", "/etc/", true}, {fileKey("abc", "/etc/hosts"), "abc", "/etc/hosts", false},
	}
	for _, tc := range tests {
		cid, p, dir := parseFileKey(tc.key)
		if cid != tc.cid || p != tc.path || dir != tc.dir {
			t.Errorf("parseFileKey(%q) = %q %q %v", tc.key, cid, p, dir)
		}
	}
}

func TestShortList(t *testing.T) {
	if got := shortList([]string{"a", "b"}, 3); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("a short list changed: %v", got)
	}
	if got := shortList([]string{"a", "b", "c", "d", "e"}, 3); !reflect.DeepEqual(got, []string{"a", "b", "c", "and 2 more"}) {
		t.Errorf("a long list: %v", got)
	}
}
