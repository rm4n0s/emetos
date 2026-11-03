//go:build emetos_node

package javascript

import (
	"os"
	"path/filepath"
	"testing"
)

// toolsModules is the node_modules of the tools of the tests: Jest, Vitest and its coverage package, installed by scripts/fetch-tools.sh into
// /var/tmp/emetos-tools/node-tests (EMETOS_TOOLS names another folder).
func toolsModules() string {
	d := os.Getenv("EMETOS_TOOLS")
	if d == "" {
		d = "/var/tmp/emetos-tools"
	}
	return filepath.Join(d, "node-tests", "node_modules")
}

// linkTools gives the project a node_modules whose entries are links to those of the tools (a folder of its own, so that what the tools write
// beside their packages, Vitest's cache among it, stays in the test's folder). The test is skipped when the tools are not installed.
func linkTools(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(toolsModules())
	if err != nil {
		t.Skip("the tools are not installed (scripts/fetch-tools.sh): " + err.Error())
	}
	nm := filepath.Join(dir, "node_modules")
	if err := os.MkdirAll(nm, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if err := os.Symlink(filepath.Join(toolsModules(), e.Name()), filepath.Join(nm, e.Name())); err != nil {
			t.Fatal(err)
		}
	}
}

// toolProject copies the project testdata/tests/<name> to dir and links the tools in.
func toolProject(t *testing.T, name, dir string) {
	t.Helper()
	copyProjectTo(t, name, dir)
	linkTools(t, dir)
}
