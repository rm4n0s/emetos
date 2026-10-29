package containerapi

import (
	"context"
	"encoding/json"
	"net/url"
	"reflect"
	"testing"
	"time"
)

func TestFlexTime(t *testing.T) {
	for _, tc := range []struct {
		name, in string
		want     time.Time
	}{
		{name: "seconds", in: `1793229025`, want: time.Unix(1793229025, 0)},
		{name: "RFC 3339 with nanoseconds", in: `"2026-10-29T01:10:25.442263612+02:00"`, want: time.Date(2026, 10, 29, 1, 10, 25, 442263612, time.FixedZone("", 2*3600))},
		{name: "null", in: `null`},
		{name: "an empty string", in: `""`},
		{name: "the zero time", in: `"0001-01-01T00:00:00Z"`},
		{name: "a negative number (libpod's ExitedAt of a container that never ran)", in: `-62135596800`},
		{name: "text that is not a time", in: `"yesterday"`},
		{name: "an object", in: `{"a":1}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var ft flexTime
			if err := json.Unmarshal([]byte(tc.in), &ft); err != nil {
				t.Fatal(err)
			}
			if !ft.Equal(tc.want) {
				t.Errorf("time = %v, want %v", ft.Time, tc.want)
			}
		})
	}
}

func TestFlexStrings(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{`["a","b c"]`, []string{"a", "b c"}},
		{`"docker-entrypoint.sh --flag"`, []string{"docker-entrypoint.sh", "--flag"}},
		{`""`, nil},
		{`null`, nil},
		{`5`, nil},
	} {
		var fs flexStrings
		if err := json.Unmarshal([]byte(tc.in), &fs); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual([]string(fs), tc.want) {
			t.Errorf("%s = %q, want %q", tc.in, []string(fs), tc.want)
		}
	}
}

func TestCommandLine(t *testing.T) {
	for in, want := range map[string]string{`"sh -c 'echo hi'"`: "sh -c 'echo hi'", `["sh","-c","echo hi"]`: "sh -c echo hi", ``: "", `null`: "", `3`: ""} {
		if got := commandLine(json.RawMessage(in)); got != want {
			t.Errorf("commandLine(%s) = %q, want %q", in, got, want)
		}
	}
}

func TestPortsOfReadsBothEnginesAndExpandsRanges(t *testing.T) {
	var rows []wirePort
	in := `[{"IP":"127.0.0.1","PrivatePort":80,"PublicPort":8080,"Type":"tcp"},{"PrivatePort":443,"Type":"tcp"},` +
		`{"host_ip":"127.0.0.1","container_port":5000,"host_port":6000,"range":3,"protocol":"udp"},{"container_port":81,"protocol":"tcp"}]`
	if err := json.Unmarshal([]byte(in), &rows); err != nil {
		t.Fatal(err)
	}
	want := []Port{
		{IP: "127.0.0.1", PrivatePort: 80, PublicPort: 8080, Type: "tcp"},
		{PrivatePort: 443, Type: "tcp"},
		{IP: "127.0.0.1", PrivatePort: 5000, PublicPort: 6000, Type: "udp"},
		{IP: "127.0.0.1", PrivatePort: 5001, PublicPort: 6001, Type: "udp"},
		{IP: "127.0.0.1", PrivatePort: 5002, PublicPort: 6002, Type: "udp"},
		{PrivatePort: 81, Type: "tcp"},
	}
	if got := portsOf(rows); !reflect.DeepEqual(got, want) {
		t.Errorf("ports = %+v\nwant %+v", got, want)
	}
	huge := []wirePort{{ContainerPort: 1, Range: 60000}}
	if n := len(portsOf(huge)); n != maxPortRange {
		t.Errorf("a range of 60000 expanded to %d ports, want the limit %d", n, maxPortRange)
	}
}

func TestSplitImageRef(t *testing.T) {
	for _, tc := range []struct{ ref, repo, tag string }{
		{"busybox", "busybox", "latest"},
		{"busybox:1.37.0", "busybox", "1.37.0"},
		{"docker.io/library/busybox:1.37.0", "docker.io/library/busybox", "1.37.0"},
		{"localhost:5000/app", "localhost:5000/app", "latest"},
		{"localhost:5000/app:v2", "localhost:5000/app", "v2"},
		{"ghcr.io/org/app@sha256:abc", "ghcr.io/org/app", "sha256:abc"},
	} {
		if repo, tag := splitImageRef(tc.ref); repo != tc.repo || tag != tc.tag {
			t.Errorf("splitImageRef(%q) = %q, %q; want %q, %q", tc.ref, repo, tag, tc.repo, tc.tag)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		sign int
	}{
		{"1.41", "1.41", 0}, {"1.40", "1.41", -1}, {"1.52", "1.47", 1}, {"1.9", "1.41", -1}, {"4.0.0", "4", 0},
		{"5.8.9", "4.0.0", 1}, {"1.41", "1.41.1", -1}, {"", "1.0", -1}, {"2.0", "1.99", 1},
	} {
		got := compareVersions(tc.a, tc.b)
		if (got < 0) != (tc.sign < 0) || (got > 0) != (tc.sign > 0) {
			t.Errorf("compareVersions(%q, %q) = %d, want sign %d", tc.a, tc.b, got, tc.sign)
		}
	}
}

func TestEngineMessage(t *testing.T) {
	long := make([]byte, 3000)
	for i := range long {
		long[i] = 'x'
	}
	for _, tc := range []struct {
		name   string
		body   string
		status int
		want   string
	}{
		{"docker", `{"message":"No such container: x"}`, 404, "No such container: x"},
		{"libpod", `{"cause":"no such container","message":"no container with name x found: no such container","response":404}`, 404, "no container with name x found: no such container"},
		{"plain text", "  page not found\n", 404, "page not found"},
		{"empty", "", 500, "Internal Server Error"},
		{"JSON without a message", `{"cause":"x"}`, 500, `{"cause":"x"}`},
		{"a long body is cut", string(long), 500, string(long[:1024])},
	} {
		if got := engineMessage([]byte(tc.body), tc.status); got != tc.want {
			t.Errorf("%s: message = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestDecodePrune(t *testing.T) {
	for _, tc := range []struct {
		name, in, key string
		want          PruneResult
		route         string
	}{
		{"docker containers", `{"ContainersDeleted":["a","b"],"SpaceReclaimed":300}`, "ContainersDeleted", PruneResult{Deleted: []string{"a", "b"}, SpaceReclaimed: 300}, ""},
		{"docker nothing", `{"ContainersDeleted":null,"SpaceReclaimed":0}`, "ContainersDeleted", PruneResult{}, ""},
		{"docker images", `{"ImagesDeleted":[{"Untagged":"x:1"},{"Deleted":"sha256:ab"}],"SpaceReclaimed":9}`, "ImagesDeleted", PruneResult{Deleted: []string{"x:1", "sha256:ab"}, SpaceReclaimed: 9}, ""},
		{"docker volumes", `{"VolumesDeleted":["v"],"SpaceReclaimed":4}`, "VolumesDeleted", PruneResult{Deleted: []string{"v"}, SpaceReclaimed: 4}, ""},
		{"docker networks", `{"NetworksDeleted":["n"]}`, "NetworksDeleted", PruneResult{Deleted: []string{"n"}}, ""},
		{"libpod reports", `[{"Id":"a","Err":null,"Size":5},{"Id":"b","Err":"in use","Size":7},{"Id":"c","Err":"","Size":1}]`, "", PruneResult{Deleted: []string{"a", "c"}, SpaceReclaimed: 6}, ""},
		{"libpod networks", `[{"Name":"n","Error":null}]`, "", PruneResult{Deleted: []string{"n"}}, ""},
		{"libpod nothing", `[]`, "", PruneResult{}, ""},
		{"nothing at all", ``, "", PruneResult{}, ""},
		{"a list that is not a list of reports", `[1,2]`, "", PruneResult{}, "decodePrune.ContainerAPIDecodeFailed"},
		{"an object that is not", `{"SpaceReclaimed":"lots"}`, "", PruneResult{}, "decodePrune.ContainerAPIDecodeFailed"},
		{"deleted that is not a list", `{"ContainersDeleted":5}`, "ContainersDeleted", PruneResult{}, "decodePrune.ContainerAPIDecodeFailed"},
		{"an item that is neither a name nor an image", `{"ImagesDeleted":[5]}`, "ImagesDeleted", PruneResult{}, "decodePrune.ContainerAPIDecodeFailed"},
		{"broken JSON in an object", `{"a":`, "", PruneResult{}, "decodePrune.ContainerAPIDecodeFailed"},
		{"broken JSON in a list", `[{"Id":`, "", PruneResult{}, "decodePrune.ContainerAPIDecodeFailed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decodePrune(json.RawMessage(tc.in), tc.key)
			if tc.route != "" {
				wantRoute(t, err, tc.route)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("result = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestSmallHelpers(t *testing.T) {
	if got := imagePath("docker.io/library/busy box:1.0", "/json"); got != "/images/docker.io/library/busy%20box:1.0/json" {
		t.Errorf("imagePath = %q", got)
	}
	if got := timeoutQuery("t", 1500*time.Millisecond).Encode(); got != "t=1" {
		t.Errorf("timeoutQuery(1.5 s) = %q", got)
	}
	if got := timeoutQuery("t", 100*time.Millisecond).Encode(); got != "t=1" {
		t.Errorf("a timeout under a second is a second, got %q", got)
	}
	if got := timeoutQuery("timeout", 0).Encode(); got != "" {
		t.Errorf("no timeout = %q", got)
	}
	q := url.Values{}
	Filters{"label": {"a=b", "c"}}.addTo(q)
	if got := q.Get("filters"); got != `{"label":["a=b","c"]}` {
		t.Errorf("filters = %q", got)
	}
	Filters(nil).addTo(q)
	if ShortID("sha256:0123456789abcdef") != "0123456789ab" || ShortID("abc") != "abc" {
		t.Error("ShortID")
	}
	c := Container{ID: "0123456789abcdef", Labels: map[string]string{LabelCompose: "proj"}, State: "paused"}
	if c.Name() != "0123456789ab" || c.ComposeProject() != "proj" || !c.Running() {
		t.Errorf("Container helpers: %q %q %v", c.Name(), c.ComposeProject(), c.Running())
	}
	for _, tt := range []struct {
		name   string
		labels map[string]string
		want   string
	}{
		{"docker's label", map[string]string{LabelCompose: "shop"}, "shop"},
		{"podman-compose's label", map[string]string{LabelPodmanCompose: "blog"}, "blog"},
		{"both: docker's wins", map[string]string{LabelCompose: "shop", LabelPodmanCompose: "blog"}, "shop"},
		{"none", map[string]string{"a": "b"}, ""},
	} {
		if got := (Container{Labels: tt.labels}).ComposeProject(); got != tt.want {
			t.Errorf("%s: ComposeProject = %q, want %q", tt.name, got, tt.want)
		}
	}
	if (Container{State: "exited"}).Running() {
		t.Error("an exited container is not running")
	}
	if (DiskUsage{Images: Usage{Size: 1}, Containers: Usage{Size: 2}, Volumes: Usage{Size: 3}, BuildCache: Usage{Size: 4}}).Total() != 10 {
		t.Error("Total")
	}
}

func TestEncodeFailureHasItsRoute(t *testing.T) {
	c := New(KindDocker, UnixEndpoint("/nonexistent/engine.sock"), Options{})
	_, err := c.do(context.Background(), request{method: "POST", path: "/x", unversioned: true, body: make(chan int)})
	wantRoute(t, err, "Client.do.ContainerAPIEncodeFailed")
}

func TestTheRequestCannotBeBuilt(t *testing.T) {
	c := New(KindDocker, UnixEndpoint("/nonexistent/engine.sock"), Options{})
	_, err := c.do(context.Background(), request{method: "BAD METHOD", path: "/x", unversioned: true})
	wantRoute(t, err, "Client.do.ContainerAPIRequestFailed")
}
