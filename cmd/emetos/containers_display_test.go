//go:build emetos_display

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/testenv/display"
)

// The Docker extension's context menus of the Explorer, with the real mouse and keyboard (step 18.3): a right click on a Dockerfile
// offers Build Image, one on a Compose file offers Compose Up and its relatives, and Build Image builds that very file, through a
// fake docker program on PATH (which records its arguments), streaming into the panel. The engine of the machine is never asked:
// the setting containers.host names a socket that nothing listens on, so the Containers view only says there is no engine.

const fakeDockerScript = `#!/bin/sh
printf '%s\n' "$*" >> "$FAKE_DOCKER_LOG"
case "$1" in
build) echo "#1 [internal] load build definition from Dockerfile"; echo "#2 exporting to image" ;;
esac
exit 0
`

func TestDesktopContainersWorkflowsFromTheExplorer(t *testing.T) {
	bin := t.TempDir()
	logPath := filepath.Join(bin, "calls.log")
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(fakeDockerScript), 0o755); err != nil {
		t.Fatal(err)
	}
	a := display.Start(t, display.Options{
		Args:    []string{"."},
		Project: map[string]string{"Dockerfile": "FROM busybox\n", "compose.yaml": "services: {}\n", "main.go": "package main\n"},
		Config:  map[string]string{"settings.json": `{"containers.host": "unix:///nonexistent/emetos-test.sock"}`},
		Env:     []string{"PATH=" + bin + ":" + os.Getenv("PATH"), "FAKE_DOCKER_LOG=" + logPath},
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
		if has("Build Image...") || has("Compose Up") {
			t.Errorf("the menu of main.go offers Docker commands: %s", a.Dump())
		}
		a.Press("Escape")
	})
	t.Run("a Compose file offers Compose", func(t *testing.T) {
		rightClick("compose.yaml")
		a.WaitText("Compose Up")
		for _, want := range []string{"Compose Up - Select Services", "Compose Down", "Compose Restart", "Compose Logs"} {
			if !has(want) {
				t.Errorf("no %q in the menu: %s", want, a.Dump())
			}
		}
		if has("Build Image...") {
			t.Error("Build Image is offered for a Compose file")
		}
		shot(t, a, "containers-compose-menu")
		a.Press("Escape")
	})
	t.Run("Build Image builds the Dockerfile that was pressed", func(t *testing.T) {
		rightClick("Dockerfile")
		a.WaitText("Build Image...")
		if has("Compose Up") {
			t.Error("Compose Up is offered for a Dockerfile")
		}
		a.ClickText("Build Image...")
		a.Settle()
		a.Press("ctrl+a")
		a.Type("demo:1")
		a.Press("Enter")
		a.WaitText("CONTAINERS LOGS")
		a.Eventually("the build in the panel", func() (bool, string) {
			_, ok := a.Text("Finished")
			return ok, a.Dump()
		})
		data, err := os.ReadFile(logPath)
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(string(data)); got != "build --pull --rm -f Dockerfile -t demo:1 ." {
			t.Errorf("docker was run as %q", got)
		}
		shot(t, a, "containers-build")
	})
}
