//go:build emetos_display && emetos_gopls

package main

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/testenv/display"
)

// Find All References and Find All Implementations with the real gopls (step 17e.3), under the tag emetos_gopls; skipped when
// gopls or go is not installed. Run with:
// scripts/realinput.sh -- -tags 'emetos_display emetos_gopls' ./cmd/emetos -run FindAllReferencesWithGopls -v

// goplsProject is a module with an interface, two types that implement it, calls of a method in two files and a variable.
var goplsProject = map[string]string{
	"go.mod": "module example.com/x\n\ngo 1.21\n",
	"speaker.go": "package main\n\ntype Speaker interface {\n    Speak() string\n}\n\ntype Dog struct{}\n\nfunc (Dog) Speak() string { return \"woof\" }\n\n" +
		"type Cat struct{}\n\nfunc (Cat) Speak() string { return \"meow\" }\n\nfunc Describe(s Speaker) string { return s.Speak() }\n",
	"main.go":  "package main\n\nimport \"fmt\"\n\nfunc main() {\n    d := Dog{}\n    fmt.Println(d.Speak())\n    fmt.Println(Describe(Cat{}))\n    count := 3\n    fmt.Println(count, count+1)\n}\n",
	"extra.go": "package main\n\nfunc Extra() string { return Dog{}.Speak() }\n",
}

// wordAtColumn is the x of the middle of the first occurrence of word on a line of a file of goplsProject.
func wordX(t *testing.T, file string, line int, word string) float64 {
	t.Helper()
	text := strings.Split(goplsProject[file], "\n")[line-1]
	col := strings.Index(text, word)
	if col < 0 {
		t.Fatalf("%q is not on line %d of %s", word, line, file)
	}
	return editorColumnX(col + len(word)/2)
}

func TestDesktopFindAllReferencesWithGopls(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls is not installed")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is not installed")
	}
	a := display.Start(t, display.Options{
		Args: []string{".", "main.go"}, Project: goplsProject,
		// nothing may be fetched: the local toolchain is the one used, in a module of the test
		Env: []string{"GOTOOLCHAIN=local", "GOPROXY=off", "GOFLAGS=-mod=mod", "GOCACHE=" + t.TempDir(), "GOPATH=" + t.TempDir()},
	})
	a.Settle()

	// a method: right click on Speak in d.Speak() of main.go
	referencesMenu(t, a, wordX(t, "main.go", 7, "Speak"), editorLine(7), "Find All References")
	a.WaitText("Find All Implementations")
	a.ClickText("Find All References")
	a.EventuallyFor(60*time.Second, "gopls to answer", func() (bool, string) {
		_, ok := a.Text("4 results in 3 files")
		return ok, a.Dump()
	})
	shot(t, a, "gopls-references")
	// the view has the calls of the method in two files besides its declaration; a click on a place opens the file
	a.ClickText("extra.go")
	a.EventuallyFor(10*time.Second, "the call in extra.go", func() (bool, string) { _, ok := textContaining(a, ".Speak()"); return ok, a.Dump() })
	tx, _ := textContaining(a, ".Speak()") // the first from the top: extra.go comes before main.go
	a.Click(tx.X+tx.Width/2, tx.Y+tx.Height/2)
	a.Eventually("extra.go to open", func() (bool, string) { return activeTab(a) == "extra.go", joinNames(tabNames(a)) })

	// an interface method: the types that implement it
	a.Press("ctrl+p")
	a.Eventually("quick open", func() (bool, string) { return len(a.Inputs()) > 0, a.Dump() })
	a.Type("speaker.go")
	a.WaitFocusedText("speaker.go")
	a.Press("Enter")
	a.Eventually("speaker.go to open", func() (bool, string) { return activeTab(a) == "speaker.go", joinNames(tabNames(a)) })
	referencesMenu(t, a, wordX(t, "speaker.go", 4, "Speak"), editorLine(4), "Find All Implementations")
	a.ClickText("Find All Implementations")
	a.EventuallyFor(60*time.Second, "gopls to find the implementations", func() (bool, string) {
		_, ok := a.Text("2 results in 1 file")
		return ok, a.Dump()
	})
	a.WaitText("IMPLEMENTATIONS")
	shot(t, a, "gopls-implementations")

	// a variable, with the key
	a.Press("ctrl+p")
	a.Eventually("quick open", func() (bool, string) { return len(a.Inputs()) > 0, a.Dump() })
	a.Type("main.go")
	a.WaitFocusedText("main.go")
	a.Press("Enter")
	a.Eventually("main.go to open", func() (bool, string) { return activeTab(a) == "main.go", joinNames(tabNames(a)) })
	a.Click(wordX(t, "main.go", 9, "count"), editorLine(9))
	a.Press("shift+alt+f12")
	a.EventuallyFor(60*time.Second, "the key to list the places of the variable", func() (bool, string) {
		_, ok := a.Text("3 results in 1 file")
		return ok, a.Dump()
	})
	a.WaitText("REFERENCES")
	shot(t, a, "gopls-variable")
}
