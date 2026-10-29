package containerui

import (
	"context"
	"testing"

	"github.com/rm4n0s/errors"

	gui "github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi/containerapitest"
)

// TestGoldenTheExplorer looks at the four sections of the side bar, each in a view of its own, in both themes: the containers
// grouped by Compose project with their state icons, images and ports, the images by repository (the nameless one marked), the
// networks and the volumes.
func TestGoldenTheExplorer(t *testing.T) {
	for _, th := range uitest.Themes() {
		for _, sec := range []struct {
			name string
			size gui.Size
		}{{secContainers, gui.Size{Width: 380, Height: 210}}, {secImages, gui.Size{Width: 380, Height: 190}},
			{secNetworks, gui.Size{Width: 380, Height: 60}}, {secVolumes, gui.Size{Width: 380, Height: 60}}} {
			t.Run(th.Name+"/"+sec.name, func(t *testing.T) {
				r := newRig(t, sampleEngine)
				r.ext.note.seen = true
				ts := r.show(th, sec.size, r.cfg.ViewID(sec.name))
				uitest.MatchesGolden(ts.Tester, "explorer_"+sec.name+"_"+th.Name)
			})
		}
	}
}

// TestGoldenTheConfirmation looks at the question over the containers: the title, the names, the running ones told apart, the
// choices and Cancel.
func TestGoldenTheConfirmation(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := newRig(t, sampleEngine)
			r.ext.note.seen = true
			r.view(secContainers).tree.SetSelection([]string{keyOfContainer(r.id("shop-web-1")), keyOfContainer(r.id("scratch")), keyOfContainer(r.id("blog-app-1"))})
			r.run("containers.remove", nil)
			ts := r.show(th, gui.Size{Width: 380, Height: 360}, r.cfg.ViewID(secContainers))
			ts.refresh()
			uitest.MatchesGolden(ts.Tester, "confirmation_"+th.Name)
		})
	}
}

// TestGoldenTheMissingEngine looks at the Containers view when no engine answers: the reason, the places that were asked and Retry.
func TestGoldenTheMissingEngine(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			fake := containerapitest.NewFake(t, containerapi.KindDocker, containerapitest.FakeOptions{})
			cfg := testConfig(func(context.Context, containerapi.FindOptions) (*containerapi.Found, error) {
				return nil, errors.New("ContainerAPIEngineNotFound", "No Docker engine answered:\n- DOCKER_HOST: not set\n- unix:///var/run/docker.sock: connect: no such file or directory")
			})
			r := activate(t, fake, cfg)
			ts := r.show(th, gui.Size{Width: 380, Height: 170}, cfg.ViewID(secContainers))
			uitest.MatchesGolden(ts.Tester, "missing_engine_"+th.Name)
		})
	}
}

// TestGoldenTheFirstRunNote looks at the note about the Docker socket over the containers, before it is dismissed.
func TestGoldenTheFirstRunNote(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := newRig(t, sampleEngine)
			ts := r.show(th, gui.Size{Width: 380, Height: 230}, r.cfg.ViewID(secContainers))
			uitest.MatchesGolden(ts.Tester, "first_run_note_"+th.Name)
		})
	}
}

// TestGoldenThePullInTheImages looks at a pull that goes on over the images: its title, the last word of the engine, and the bar.
func TestGoldenThePullInTheImages(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := newRig(t, sampleEngine)
			op := &operation{title: "Pulling alpine:3.20", cancel: func() {}, status: "Starting..."}
			op.progress(r.ext, containerapi.PullProgress{ID: "a1b2c3", Status: "Downloading", Current: 1_300_000, Total: 3_400_000})
			op.progress(r.ext, containerapi.PullProgress{ID: "d4e5f6", Status: "Download complete", Current: 700_000, Total: 700_000})
			r.ext.ops = append(r.ext.ops, op)
			ts := r.show(th, gui.Size{Width: 380, Height: 240}, r.cfg.ViewID(secImages))
			uitest.MatchesGolden(ts.Tester, "pull_progress_"+th.Name)
		})
	}
}

// TestGoldenTheLogsPanel looks at the panel with the logs of a container: the tab, the filter, the status, the lines of stdout and
// the lines of stderr in the color of errors.
func TestGoldenTheLogsPanel(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := newRig(t, sampleEngine)
			id := r.id("shop-web-1")
			for _, l := range []struct {
				stream containerapi.Stream
				text   string
			}{
				{containerapi.StreamStdout, "/docker-entrypoint.sh: Configuration complete; ready for start up"},
				{containerapi.StreamStdout, `172.17.0.1 - - "GET / HTTP/1.1" 200 615`},
				{containerapi.StreamStderr, "2026/10/29 11:59:01 [warn] 1#1: the \"user\" directive makes sense only if the master process runs with super-user privileges"},
				{containerapi.StreamStdout, `172.17.0.1 - - "GET /favicon.ico HTTP/1.1" 404 555`},
			} {
				r.fake.Log(id, l.stream, l.text)
			}
			r.ext.viewLogs(id)
			s := r.ext.logs.active
			r.wait("the lines", func() bool { return len(s.lines) == 4 })
			r.ext.logs.runExec(containerapi.Container{ID: id, Names: []string{"shop-web-1"}}, "echo done")
			r.ext.logs.activate(s)
			ts := r.show(th, gui.Size{Width: 640, Height: 190}, r.cfg.LogsViewID())
			ts.refresh()
			uitest.MatchesGolden(ts.Tester, "logs_"+th.Name)
		})
	}
}

// TestGoldenTheDocumentTab looks at the inspect of a container in its read-only tab, highlighted as JSON.
func TestGoldenTheDocumentTab(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := newRig(t, sampleEngine)
			r.api.SetTokenizer(jsonTokens)
			r.ext.docs.open("shop-web-1 (inspect)", "{\n  \"Id\": \"754992d83d03\",\n  \"State\": {\n    \"Status\": \"running\",\n    \"Running\": true,\n    \"ExitCode\": 0\n  },\n  \"Config\": {\n    \"Image\": \"nginx:1.27\",\n    \"Env\": [\n      \"MODE=test\"\n    ]\n  }\n}\n", "json")
			tab := r.api.EditorTabs()[0]
			ts := uitest.Pump(t, th, gui.Size{Width: 520, Height: 300}, widgetFrom(tab.Content.Build))
			uitest.MatchesGolden(ts, "document_"+th.Name)
		})
	}
}

// TestGoldenTheRunForm looks at the form that runs an image, filled in.
func TestGoldenTheRunForm(t *testing.T) {
	for _, th := range uitest.Themes() {
		t.Run(th.Name, func(t *testing.T) {
			r := newRig(t, sampleEngine)
			r.run("images.run", key(r.imageKey("nginx:1.27")))
			f := r.ext.runs.forms[0]
			f.name.SetValue("web")
			f.ports.SetValue("127.0.0.1:8080:80")
			f.env.SetValue("MODE=test")
			ts := uitest.Pump(t, th, gui.Size{Width: 640, Height: 600}, widgetFrom(r.api.EditorTabs()[0].Content.Build))
			uitest.MatchesGolden(ts, "run_form_"+th.Name)
		})
	}
}
