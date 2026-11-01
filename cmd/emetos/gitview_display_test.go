//go:build emetos_display

package main

import (
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/testenv/display"
)

// git push typed in the terminal of the view of a client (step 24.25, SPEC §10.18 "As built (step 24.25)", §10.25, D50, D51): the real editor
// makes the headless view a client of Remote Work has (dev.view.show), the keys of the client are events of the view (dev.view.type), the
// real Terminal and Git extensions and a real git against git http-backend on 127.0.0.1 behind Basic authentication. The password box is in
// the view of the client: the window of the host shows none, and the answer typed in the view reaches git and the push arrives.

func TestRealInputAPushTypedInTheTerminalOfAViewAsksInTheView(t *testing.T) {
	w := newTerminalGitWorld(t)
	a := display.Start(t, w.options(""))
	a.Settle()
	a.Dev("dev.view.show", map[string]any{"width": 1280, "height": 800})
	a.Dev("dev.view.run", map[string]any{"command": "workbench.view.scm"})
	a.Dev("dev.view.run", map[string]any{"command": "workbench.action.terminal.toggleTerminal"})
	time.Sleep(3 * time.Second) // the shell starts in the view's panel
	a.Dev("dev.view.run", map[string]any{"command": "workbench.action.terminal.focus"})
	time.Sleep(time.Second)
	a.Dev("dev.view.type", map[string]any{"text": "git push origin main", "enter": true})

	prompt := "Password for '" + withUserHost(w.host) + "'"
	time.Sleep(4 * time.Second) // git runs the askpass program, which asks the window over the socket: the box is in the view
	if w.remoteHead() == w.head {
		t.Fatal("the push went through before the password was typed")
	}
	if _, open := a.Text(prompt); open {
		t.Error("the window of the host asked for the password: it belongs to the view of the client who typed")
	}
	if in, ok := a.Focused(); ok && strings.Contains(in.Text, "Password") {
		t.Errorf("the host's window has a box: %+v", in)
	}
	// the client types the password in its view
	a.Dev("dev.view.type", map[string]any{"text": typedPassword, "enter": true})
	a.Eventually("the push to reach the remote", func() (bool, string) { return w.remoteHead() == w.head, w.remoteHead() })
	if strings.Contains(a.Log(), typedPassword) {
		t.Errorf("the log of the editor has the password:\n%s", a.Log())
	}
	a.Quit()
}
