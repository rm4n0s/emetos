package containerui

import (
	"flag"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi/containerapitest"
)

// updateGoldens reports whether the tests were run with -update-goldens (a flag that gluttertest registers).
func updateGoldens() bool {
	f := flag.Lookup("update-goldens")
	return f != nil && f.Value.String() == "true"
}

// inspectFrom reads a container and its image from the JSON of testdata/quadlet, answered by the fake engine as Podman's inspect.
func inspectFrom(t *testing.T, name string, withImage bool) (*containerapi.ContainerInspect, *containerapi.ImageInspect) {
	t.Helper()
	read := func(file string) []byte {
		b, err := os.ReadFile(filepath.Join("testdata", "quadlet", file))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	f := containerapitest.NewFake(t, containerapi.KindPodman, containerapitest.FakeOptions{})
	serve := func(b []byte) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(b)
		}
	}
	f.Override("GET", "/containers/"+name+"/json", serve(read(name+".inspect.json")))
	c := f.Client()
	info, err := c.InspectContainer(testCtx, name)
	if err != nil {
		t.Fatal(err)
	}
	if !withImage {
		return info, nil
	}
	f.Override("GET", "/images/", serve(read(name+".image.json")))
	img, err := c.InspectImage(testCtx, info.ImageName)
	if err != nil {
		t.Fatal(err)
	}
	return info, img
}

// TestGenerateQuadlet makes the unit of containers whose inspect is in testdata/quadlet (written as Podman 5 prints it) and compares
// it with the .container file beside it. Run with -update-goldens to write the files again, and read them before keeping them.
func TestGenerateQuadlet(t *testing.T) {
	for _, tt := range []struct {
		name      string
		withImage bool
		golden    string
	}{
		{"web", true, "web.container"},
		{"web", false, "web-without-image.container"},
		{"tricky", false, "tricky.container"},
		{"minimal", false, "minimal.container"},
	} {
		t.Run(tt.golden, func(t *testing.T) {
			info, img := inspectFrom(t, tt.name, tt.withImage)
			got, err := generateQuadlet(info, img)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join("testdata", "quadlet", tt.golden)
			if updateGoldens() {
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if got != string(want) {
				t.Errorf("the unit differs from %s:\n--- got\n%s--- want\n%s", path, got, want)
			}
		})
	}
}

// TestQuadletLeavesOutWhatTheImageHas holds the rules of what the unit holds: only what the user gave the container.
func TestQuadletLeavesOutWhatTheImageHas(t *testing.T) {
	info, img := inspectFrom(t, "web", true)
	got, err := generateQuadlet(info, img)
	if err != nil {
		t.Fatal(err)
	}
	for _, absent := range []string{"NGINX_VERSION", "PATH=", "HOSTNAME", "container=", "HOME=", "TERM=", "Exec=", "Entrypoint=", "maintainer", "io.podman", "com.docker.compose",
		"WorkingDir", "HostName", "443", "run/cache", "Network=podman"} {
		if strings.Contains(got, absent) {
			t.Errorf("the unit has %q:\n%s", absent, got)
		}
	}
	for _, present := range []string{`Environment="GREETING=hello world"`, "Environment=MODE=prod", "PublishPort=127.0.0.1:8080:80", "PublishPort=5353:53/udp",
		"Volume=/srv/site:/usr/share/nginx/html:ro", "Volume=webdata:/data", "Network=backend", "Label=tier=front", `Label="note=100%% sure"`, "Restart=always", "WantedBy=default.target"} {
		if !strings.Contains(got, present) {
			t.Errorf("the unit has no %q:\n%s", present, got)
		}
	}
}

// TestQuadletRefusesWhatItCannotRead holds the failures, with their routes.
func TestQuadletRefusesWhatItCannotRead(t *testing.T) {
	info, _ := inspectFrom(t, "failing", false)
	_, err := generateQuadlet(info, nil)
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("generateQuadlet.ContainerUIQuadletInvalid") {
		t.Errorf("an inspect with no image: %v", err)
	}
	info2, _ := inspectFrom(t, "minimal", false)
	info2.Raw = []byte("{not json")
	_, err = generateQuadlet(info2, nil)
	appErr, ok = errors.FromError(err)
	if !ok || !appErr.HasRoute("generateQuadlet.ContainerUIQuadletInvalid") {
		t.Errorf("an inspect that is not JSON: %v", err)
	}
}

func TestQuadletQuoting(t *testing.T) {
	for _, tt := range []struct {
		name string
		got  string
		want string
	}{
		{"words: plain", quadletWords([]string{"nginx", "-g", "daemon"}), "nginx -g daemon"},
		{"words: a space", quadletWords([]string{"sh", "-c", "echo hi"}), `sh -c "echo hi"`},
		{"words: dollar and percent", quadletWords([]string{"echo", "$HOME", "100%"}), "echo $$HOME 100%%"},
		{"words: empty and semicolon", quadletWords([]string{"a", "", ";"}), `a "" \;`},
		{"words: quote and backslash", quadletWords([]string{`a"b`, `c\d`}), `"a\"b" "c\\d"`},
		{"words: line break", quadletWords([]string{"a\nb"}), `"a\nb"`},
		{"assignment: plain", quadletAssignment("A=b"), "A=b"},
		{"assignment: spaces", quadletAssignment("A=b c"), `"A=b c"`},
		{"assignment: percent", quadletAssignment("A=5%"), "A=5%%"},
		{"assignment: empty value", quadletAssignment("A="), "A="},
		{"text: line break", quadletText("a\r\nb"), "a b"},
		{"file name", quadletFileName("my app/1"), "my-app-1"},
		{"file name of nothing", quadletFileName(""), "container"},
	} {
		if tt.got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.name, tt.got, tt.want)
		}
	}
	for _, tt := range []struct {
		restart, want string
	}{{"always", "always"}, {"unless-stopped", "always"}, {"on-failure", "on-failure"}, {"no", ""}, {"", ""}} {
		if got := quadletRestart(tt.restart); got != tt.want {
			t.Errorf("restart %q: %q, want %q", tt.restart, got, tt.want)
		}
	}
}

// TestGenerateQuadletCommandOpensAnUntitledIniDocument runs the command on a container of the fake and finds the unit in the
// untitled document, with no write to disk.
func TestGenerateQuadletCommandOpensAnUntitledIniDocument(t *testing.T) {
	r, _ := newPodmanRig(t, samplePodman)
	r.run("containers.generateQuadlet", key(keyOfContainer(r.id("web-nginx"))))
	r.wait("the document", func() bool { return len(r.api.UntitledDocuments()) == 1 })
	doc := r.api.UntitledDocuments()[0]
	if doc.LanguageID != "ini" || !strings.Contains(doc.Text, "[Container]\nContainerName=web-nginx\nImage=docker.io/library/nginx:1.27\n") ||
		!strings.Contains(doc.Text, "Environment=MODE=test") || !strings.HasSuffix(doc.Text, "[Install]\nWantedBy=default.target\n") {
		t.Errorf("the document: %q\n%s", doc.LanguageID, doc.Text)
	}
	if !strings.Contains(doc.Text, "# Emetos does not install it.") || !strings.Contains(doc.Text, "in a pod") {
		t.Errorf("the notes of the unit:\n%s", doc.Text)
	}
	// a container that cannot be read says so
	r.fake.FailNext("GET", "/containers/", 404, "no such container")
	r.run("containers.generateQuadlet", key(keyOfContainer(r.id("scratch"))))
	r.wait("the failure", func() bool { return r.hasNote("Cannot generate a Quadlet unit for scratch: ") })
	if len(r.api.UntitledDocuments()) != 1 {
		t.Error("a second document was opened")
	}
	// files are never written: the workspace holds nothing new
	entries, err := os.ReadDir(r.api.Scope().Folder)
	if err != nil || len(entries) != 0 {
		t.Errorf("the workspace holds %v (%v)", entries, err)
	}
}
