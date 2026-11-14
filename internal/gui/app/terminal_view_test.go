package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/services"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/terminal"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
)

// The terminals of the editor area in the view of a Remote Work client (SPEC §10.25 "Clients of a shared workspace"), with the real
// Terminal extension and a real shell: a client opens a terminal in the editor area of its view and types in it.

// typeInView types a line in the view of a client, as its keyboard does.
func typeInView(v *headlessView, line string) {
	for _, r := range line {
		v.SendKey(extension.KeyEvent{Kind: extension.KeyDown, Logical: uint64(r), Text: string(r)})
		v.SendKey(extension.KeyEvent{Kind: extension.KeyUp, Logical: uint64(r)})
	}
	enter := services.PhysicalKeyboardKeyEnter.USBHidUsage()
	v.SendKey(extension.KeyEvent{Kind: extension.KeyDown, Physical: enter, Logical: services.LogicalKeyboardKeyEnter.KeyID()})
	v.SendKey(extension.KeyEvent{Kind: extension.KeyUp, Physical: enter, Logical: services.LogicalKeyboardKeyEnter.KeyID()})
}

func TestAClientOpensATerminalInTheEditorAreaOfItsViewAndTypesInIt(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("the test runs a shell: /bin/sh is not there")
	}
	t.Setenv("SHELL", "/bin/bash") // the default profile of the terminal is the shell of the user
	env := newStateEnv(t, nil)
	run := env.startWith(env.root, ProcessOptions{Extensions: []extension.Factory{terminal.New}}, filepath.Join(env.root, stateFiles[0]))
	run.pump()
	r := &shRig{t: t, run: run}
	v := r.view()
	file := filepath.Base(stateFiles[0])

	if err := v.ws.Commands.Run(command.Context{Ctx: t.Context(), Keys: v.ws.Keys, Source: command.SourcePalette}, terminal.CmdCreateEditor, nil); err != nil {
		t.Fatal(err)
	}
	run.pump()
	run.waitFor("the terminal tab in the view", func() bool { return strings.HasSuffix(tabsOf(v.ws.shell), "*") && tabsOf(v.ws.shell) != file+"*" })
	// the tab is in the window of the host too, behind the file it shows
	if got := tabsOf(run.shell()); !strings.HasPrefix(got, file+"*, ") {
		t.Errorf("the window of the host has %q: want the file in front and the terminal behind it", got)
	}
	// the client types a command: the shell of the host runs it
	marker := filepath.Join(t.TempDir(), "typed-by-the-client")
	// a moment later, as a person types: the keyboard must still be in the terminal once the window has settled
	for range 40 {
		run.pump()
		time.Sleep(20 * time.Millisecond)
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		typeInView(v, "touch "+marker)
		run.pump()
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the shell did not run what the client typed; the view has %q, the focused view key is %v", tabsOf(v.ws.shell), v.ws.Keys.Get("focusedView"))
		}
		time.Sleep(200 * time.Millisecond)
	}
	// and nothing of it went into the file the view showed before
	doc, ok := run.win.Documents.Get(document.FileURI(filepath.Join(env.root, stateFiles[0])))
	if !ok || strings.Contains(doc.Text(), "touch") {
		t.Errorf("what the client typed is in the file (open %v)", ok)
	}
	// the client looks at the file again and kills "the terminal": the one of the editor area it used last, which closes its tab
	// on both screens
	r.inView(v, "workbench.action.previousEditor", nil)
	if got := tabsOf(v.ws.shell); !strings.HasPrefix(got, file+"*") {
		t.Fatalf("the view has %q: want the file in front", got)
	}
	r.inView(v, terminal.CmdKill, nil)
	run.waitFor("the tab to close on both", func() bool { return tabsOf(v.ws.shell) == file+"*" && tabsOf(run.shell()) == file+"*" })
}
