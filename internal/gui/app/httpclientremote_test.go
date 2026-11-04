package app

import (
	"context"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/httpclient"
	"github.com/rm4n0s/emetos/internal/core/command"
)

// Remote Work and the HTTP client (D46, SPEC §10.33 "Privacy and safety", step 27.9): a request would leave from the host's network, so a
// client of a hosted workspace cannot run any command of the extension (the class outside), the client's palette does not list them, and
// the host keeps all of them. The side bar of the client has the container and its views (the registry of the view inherits them from the
// window's scope); what they draw on the client's screen is tested in extensions/httpclient (TestTheViewsSayNothingOfTheHostsCollectionsToAClient).

func TestAClientCannotRunACommandOfTheHTTPClientAndTheHostCan(t *testing.T) {
	r := newDebugRigWith(t, debugFolder(t, nil), rigOpts{extensions: []extension.Factory{httpclient.New}})
	specs := httpclient.New().Manifest().Contributes.Commands
	if len(specs) < 15 {
		t.Fatalf("the manifest declares %d commands: the test would prove little", len(specs))
	}
	r.waitFor("the extension's commands in the window", func() bool {
		for _, c := range specs {
			if _, ok := r.win.Commands.Lookup(c.ID); !ok {
				return false
			}
		}
		return true
	})
	client := r.clientView()

	for _, c := range specs {
		t.Run(c.ID, func(t *testing.T) {
			if _, ok := r.win.Commands.Lookup(c.ID); !ok {
				t.Errorf("the host's palette does not list %s", c.ID)
			}
			if _, ok := client.Commands.Lookup(c.ID); ok {
				t.Errorf("the client's palette lists %s", c.ID)
			}
			err := client.Commands.Run(command.Context{Ctx: context.Background(), Keys: client.Keys, Source: command.SourcePalette}, c.ID, nil)
			appErr, ok := errors.FromError(err)
			if !ok || appErr.Tag != "CommandRefused" || !appErr.HasRoute("Registry.Run.CommandRefused") || appErr.Metadata["class"] != extension.RefusedOutside {
				t.Errorf("a client's run of %s: %v", c.ID, err)
			}
		})
	}
	for _, m := range client.Commands.All() {
		// the view's own commands (quit, the window tabs) are the shell's, which does not bind them; only the extension's are forwards
		if strings.HasPrefix(m.ID, "emetos.httpclient.") || strings.HasPrefix(m.ID, "dev.httpclient.") {
			t.Errorf("the client's list has %s", m.ID)
		}
	}

	// the host runs them as before; focus is a command that asks nothing
	r.run("emetos.httpclient.focus", nil)

	// the side bar: the container and its three views are in the client's registry, and the container is the one of the window's
	if c, ok := client.Views.Registry().Container(httpclient.ContainerID); !ok || c.Title != "HTTP Client" {
		t.Errorf("the client's registry has the container %+v, %v", c, ok)
	}
	for _, id := range []string{httpclient.CollectionsViewID, httpclient.EnvironmentsViewID, httpclient.HistoryViewID} {
		found := false
		for _, v := range client.Views.Registry().Views(httpclient.ContainerID, client.Keys) {
			found = found || v.ID == id
		}
		if !found {
			t.Errorf("the client's registry has no view %s", id)
		}
	}
	if errs := r.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("the framework reported errors: %v", errs)
	}
}

// The client's status bar does not carry the host's environment item of the HTTP client (its click is refused), and keeps the other items
// of extensions.
func TestAClientsStatusBarHasNoHTTPClientItem(t *testing.T) {
	for id, want := range map[string]bool{
		"extension.emetos.httpclient.environment": false, "extension.emetos.httpclient.x": false,
		"extension.git.branch": true, "extension.emetos.httpclientfoo": true, "window.own": false,
	} {
		if got := extensionStatusID(id); got != want {
			t.Errorf("extensionStatusID(%q) = %v, want %v", id, got, want)
		}
	}
}
