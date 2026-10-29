//go:build emetos_display

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/testenv/display"
)

// The Podman extension's context menus of the Explorer, with the real mouse and keyboard (step 18.4): a right click on a Kubernetes
// YAML file offers Play Kube and Kube Down (told apart from Docker's items by "Podman: "), one on a Dockerfile offers Build Image of
// Docker and of Podman, and Play Kube plays that very file through a fake podman program on PATH (which records its arguments),
// streaming into the panel. The engine of the machine is never asked: the settings podman.host and containers.host name sockets that
// nothing listens on, so the views only say there is no engine, and the fake program lists no connection.

const fakePodmanScript = `#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_PODMAN_LOG"
case "$1 $2" in
"kube play") echo "Pod:"; echo "0123456789ab" ;;
"system connection") echo "[]" ;;
esac
exit 0
`

const kubePodYAML = `apiVersion: v1
kind: Pod
metadata:
  name: web
spec:
  containers:
  - image: docker.io/library/nginx:1.27
    name: nginx
`

func TestDesktopPodmanKubeFromTheExplorer(t *testing.T) {
	bin := t.TempDir()
	logPath := filepath.Join(bin, "calls.log")
	if err := os.WriteFile(filepath.Join(bin, "podman"), []byte(fakePodmanScript), 0o755); err != nil {
		t.Fatal(err)
	}
	a := display.Start(t, display.Options{
		Args:    []string{"."},
		Project: map[string]string{"Dockerfile": "FROM busybox\n", "pod.yaml": kubePodYAML, "main.go": "package main\n"},
		Config:  map[string]string{"settings.json": `{"podman.host": "unix:///nonexistent/emetos-test-podman.sock", "containers.host": "unix:///nonexistent/emetos-test.sock"}`},
		Env:     []string{"PATH=" + bin + ":" + os.Getenv("PATH"), "FAKE_PODMAN_LOG=" + logPath},
	})
	a.Settle()
	rows := inRect(0, 90, 348, 750)
	rightClick := func(name string) {
		t.Helper()
		tx, ok := textWhere(a, name, rows)
		if !ok {
			t.Fatalf("no row %q: %s", name, a.Dump())
		}
		a.RightClick(tx.X+tx.Width/2, tx.Y+tx.Height/2)
	}
	has := func(text string) bool { _, ok := a.Text(text); return ok }

	t.Run("a Go file has none of it", func(t *testing.T) {
		rightClick("main.go")
		a.WaitText("Rename...")
		a.Settle()
		if has("Podman: Play Kube...") || has("Podman: Build Image...") {
			t.Errorf("the menu of main.go offers Podman commands: %s", a.Dump())
		}
		a.Press("Escape")
	})
	t.Run("a Dockerfile offers the build of both engines, told apart", func(t *testing.T) {
		rightClick("Dockerfile")
		a.WaitText("Podman: Build Image...")
		if !has("Build Image...") || has("Podman: Play Kube...") {
			t.Errorf("the menu of the Dockerfile: %s", a.Dump())
		}
		a.Press("Escape")
	})
	t.Run("a YAML file offers Play Kube and Kube Down, and Play Kube plays it", func(t *testing.T) {
		rightClick("pod.yaml")
		a.WaitText("Podman: Play Kube...")
		if !has("Podman: Kube Down...") || has("Compose Up") {
			t.Errorf("the menu of pod.yaml: %s", a.Dump())
		}
		shot(t, a, "podman-kube-menu")
		a.ClickText("Podman: Play Kube...")
		a.WaitText("PODMAN LOGS")
		a.Eventually("the play in the panel", func() (bool, string) {
			_, ok := a.Text("Finished")
			return ok, a.Dump()
		})
		data, err := os.ReadFile(logPath)
		if err != nil {
			t.Fatal(err)
		}
		var plays []string
		for _, l := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			if strings.HasPrefix(l, "kube play") {
				plays = append(plays, l)
			}
		}
		if len(plays) != 1 || plays[0] != "kube play pod.yaml" {
			t.Errorf("podman was run as %q", plays)
		}
		shot(t, a, "podman-kube-play")
	})
}
