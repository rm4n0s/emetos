//go:build emetos_display && emetos_gopls

package main

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/testenv/display"
)

// The same editing with the real gopls (step 17b.5), under the tag emetos_gopls; skipped when gopls is not installed.
// Run with: scripts/realinput.sh -- -tags 'emetos_display emetos_gopls' ./cmd/emetos -run Gopls -v

func TestDesktopRealGopls(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls is not installed")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is not installed")
	}
	a := display.Start(t, display.Options{
		Args: []string{".", "main.go"},
		Project: map[string]string{
			"go.mod":  "module example.com/x\n\ngo 1.21\n",
			"main.go": "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hi\")\n}\n",
		},
		// the editor runs with a temporary HOME: nothing may be fetched, and the local toolchain is the one used
		Env:      []string{"GOTOOLCHAIN=local", "GOPROXY=off", "GOFLAGS=-mod=mod", "GOCACHE=" + t.TempDir(), "GOPATH=" + t.TempDir()},
		KeyDelay: 25 * time.Millisecond,
	})
	a.Settle()
	// completion: gopls takes a while to load the package, so ask until it answers
	a.Click(editorColumnX(20), editorLine(6))
	a.Press("End", "Enter")
	a.Type("fmt.Prin")
	a.EventuallyFor(60*time.Second, "gopls to offer completions", func() (bool, string) {
		if a.Editor().SuggestVisible {
			return true, ""
		}
		// type one more character to ask again
		a.Press("BackSpace")
		a.Type("n")
		return false, editorText(a)
	})
	shot(t, a, "gopls-completion")
	a.Press("Escape")
	// hover over Println on line 6
	a.MoveTo(editorColumnX(8), editorLine(6))
	a.EventuallyFor(30*time.Second, "gopls to answer the hover", func() (bool, string) { return a.Editor().HoverVisible, "no hover" })
	shot(t, a, "gopls-hover")
	// a mistake shows in the Problems panel
	a.Press("ctrl+shift+m")
	a.Click(editorColumnX(3), editorLine(7))
	a.Press("End")
	a.Type("; unknownName()")
	a.EventuallyFor(60*time.Second, "gopls to report the undefined name", func() (bool, string) {
		for _, tx := range a.Texts() {
			if strings.Contains(tx.Text, "undefined: unknownName") {
				return true, ""
			}
		}
		return false, a.Dump()
	})
	shot(t, a, "gopls-problems")
}
