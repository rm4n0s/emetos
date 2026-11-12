package app

import (
	"context"
	"slices"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/notify"
	"github.com/rm4n0s/emetos/internal/gui/exthost"
	"github.com/rm4n0s/emetos/internal/gui/quickinput"
	"github.com/rm4n0s/emetos/internal/gui/workbench"
)

// What the extensions give the workbench is in the headless view of a client (step 24.25, SPEC §10.18 "As built (step 24.25)", D50, D51): the
// commands (forwards to the handlers of the window's scope, with the view as their origin), the menu items, the views, the status items and
// the Source Control and Timeline views. The questions the handlers ask are shown in the view that ran the command.

// extCommand registers a command in the registry of the window as the scope of an extension does, and records the origins it ran with.
func extCommand(t *testing.T, w *Workspace, id, when string, ran *[]extension.Origin) {
	t.Helper()
	meta := command.Meta{ID: id, Title: id}
	if when != "" {
		expr, err := contextkey.Parse(when)
		if err != nil {
			t.Fatal(err)
		}
		meta.Precondition = expr
	}
	if err := w.Commands.Register(meta); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Commands.SetHandler(id, func(cc command.Context, _ any) error {
		*ran = append(*ran, cc.Origin)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestTheCommandsOfTheExtensionsAreForwardedFromTheView(t *testing.T) {
	r := newViewRig(t, "one\n", extension.Size{Width: 800, Height: 500})
	view := r.view.ws
	if view.origin == extension.HostOrigin {
		t.Fatal("the view has the origin of the host")
	}
	var ran []extension.Origin
	extCommand(t, r.win, "git.fetch", "", &ran)
	extCommand(t, r.win, "git.gated", "gitReady", &ran)
	extCommand(t, r.win, "workbench.action.terminal.new", "", &ran)
	extCommand(t, r.win, "emetos.databases.connect", "", &ran)
	cc := command.Context{Ctx: context.Background(), Keys: view.Keys, Source: command.SourcePalette}

	t.Run("a command of an extension is in the registry of the view and runs with the view's origin", func(t *testing.T) {
		if _, ok := view.Commands.Lookup("git.fetch"); !ok {
			t.Fatal("the view does not know git.fetch")
		}
		if err := view.Commands.Run(cc, "git.fetch", nil); err != nil {
			t.Fatal(err)
		}
		if len(ran) != 1 || ran[0] != view.origin {
			t.Errorf("ran with %v, want the origin %d", ran, view.origin)
		}
		ran = nil
		if err := r.win.Commands.Run(command.Context{Ctx: context.Background()}, "git.fetch", nil); err != nil || len(ran) != 1 || ran[0] != extension.HostOrigin {
			t.Errorf("the host's own run: %v, %v", err, ran)
		}
	})
	t.Run("its when clause is read in the keys of the view", func(t *testing.T) {
		if view.Commands.Enabled("git.gated", view.Keys) {
			t.Error("the command is enabled before the key is set")
		}
		view.Keys.Set("gitReady", true)
		if !view.Commands.Enabled("git.gated", view.Keys) {
			t.Error("the command is not enabled with the key of the view")
		}
		if r.win.Commands.Enabled("git.gated", r.win.Keys) {
			t.Error("the key of the view reached the window")
		}
	})
	t.Run("the terminal is the client's too", func(t *testing.T) {
		ran = nil
		if err := view.Commands.Run(cc, "workbench.action.terminal.new", nil); err != nil || len(ran) != 1 || ran[0] != view.origin {
			t.Errorf("terminal.new: %v, ran %v", err, ran)
		}
	})
	t.Run("a command a client may not run is refused, and not listed", func(t *testing.T) {
		ran = nil
		err := view.Commands.Run(cc, "emetos.databases.connect", nil)
		appErr, ok := errors.FromError(err)
		if !ok || appErr.Tag != "CommandRefused" || !appErr.HasRoute("Registry.Run.CommandRefused") || appErr.Metadata["class"] != extension.RefusedOutside {
			t.Errorf("error %v", err)
		}
		if len(ran) != 0 {
			t.Error("the refused command ran")
		}
		if _, ok := view.Commands.Lookup("emetos.databases.connect"); ok {
			t.Error("the palette would list it")
		}
		for _, m := range view.Commands.All() {
			if m.ID == "emetos.databases.connect" {
				t.Error("All lists it")
			}
		}
	})
	t.Run("a command that is not there is not found", func(t *testing.T) {
		appErr, _ := errors.FromError(view.Commands.Run(cc, "no.such", nil))
		if appErr == nil || appErr.Tag != "CommandNotFound" || !appErr.HasRoute("Registry.Run.CommandNotFound") {
			t.Errorf("error %v", appErr)
		}
	})
}

func TestTheMenuItemsViewsAndStatusItemsOfTheExtensionsAreInTheView(t *testing.T) {
	r := newViewRig(t, "one\n", extension.Size{Width: 800, Height: 500})
	view := r.view.ws
	var ran []extension.Origin
	extCommand(t, r.win, "ext.fromExtension", "", &ran)
	extCommand(t, r.win, "ext.plain", "", &ran)
	if _, err := r.win.Menus.AddRemovable(command.MenuItem{Menu: command.SCMTitle, Command: "ext.fromExtension", Title: "From an extension", Group: "navigation", Extension: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.win.Menus.AddRemovable(command.MenuItem{Menu: command.SCMTitle, Command: "ext.plain", Title: "Not an extension's", Group: "navigation"}); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range view.Menus.Items(command.SCMTitle, contextkey.NewRoot()) {
		got = append(got, e.Command)
	}
	if len(got) == 0 || !slices.Contains(got, "ext.fromExtension") || slices.Contains(got, "ext.plain") {
		t.Errorf("the menu of the view has %v: the items of the extensions, and no other item of the window", got)
	}

	// a panel of an extension is a tab of the panel of the view
	if err := r.win.Views.Registry().RegisterContainer(command.ViewContainer{ID: "ext.panel", Title: "Ext", Location: command.ViewLocationPanel}); err != nil {
		t.Fatal(err)
	}
	if _, ok := view.Views.Registry().Container("ext.panel"); !ok {
		t.Error("the container of the extension is not in the registry of the view")
	}
	found := false
	for _, c := range view.Views.Registry().Containers(command.ViewLocationPanel) {
		found = found || c.ID == "ext.panel"
	}
	if !found {
		t.Error("the panel tab of the extension is not listed in the view")
	}

	// the status items of the extensions, not those of the window
	changes := 0
	view.StatusBar.OnChange(func() { changes++ })
	acc := r.win.StatusBar.Add("extension.git.branch", workbench.StatusLeft, 100, workbench.StatusEntry{Text: "main"})
	r.win.StatusBar.Add("window.own", workbench.StatusLeft, 100, workbench.StatusEntry{Text: "own"})
	ids := map[string]bool{}
	for _, it := range view.StatusBar.Items(workbench.StatusLeft) {
		ids[it.ID] = true
	}
	if !ids["extension.git.branch"] || ids["window.own"] {
		t.Errorf("the status items of the view: %v", ids)
	}
	if changes == 0 {
		t.Error("the view's bar was not told of the new item")
	}
	acc.Dispose()
	if _, ok := view.StatusBar.Get("extension.git.branch"); ok {
		t.Error("the item stays after it was removed")
	}
}

func TestTheViewHasTheSourceControlAndTimelineViewsOfTheWindow(t *testing.T) {
	r := newViewRig(t, "one\n", extension.Size{Width: 800, Height: 500})
	r.frame(t)
	view := r.view.ws
	if view.SCM == nil || view.SCM != r.win.SCM || view.Timeline != r.win.Timeline {
		t.Fatal("the view does not share the Source Control model and the timeline of the window")
	}
	s := view.shell
	if s.scm.view == nil || s.graph.pane == nil || s.timeline.pane == nil {
		t.Errorf("the view's shell has the Changes view %v, the Graph %v, the Timeline %v", s.scm.view != nil, s.graph.pane != nil, s.timeline.pane != nil)
	}
	// the context keys of the model are set in the keys of the view too
	r.win.SCM.Context()
	for key, want := range r.win.SCM.Context() {
		r.pump()
		if got, hostValue := view.Keys.Get(key), r.win.Keys.Get(key); got != hostValue {
			t.Errorf("the key %s is %v in the view, %v in the window (%v)", key, got, hostValue, want)
		}
	}
}

func TestTheQuestionsOfAnExtensionAreShownOnTheScreenThatAsked(t *testing.T) {
	r := newViewRig(t, "one\n", extension.Size{Width: 800, Height: 500})
	r.frame(t)
	view := r.view.ws
	pick := func(origin extension.Origin) (hidden *int) {
		hidden = new(int)
		r.win.extensionQuickPick(exthost.QuickPickRequest{
			Placeholder: "Pick", Items: []exthost.QuickPickEntry{{Label: "one"}}, Origin: origin,
			Chosen: func(int, string) {}, Hidden: func() { *hidden++ },
		})
		r.pump() // the list is handed to the UI thread of its screen
		return hidden
	}
	t.Run("a list of a client is in its view", func(t *testing.T) {
		pick(view.origin)
		if !view.QuickInput.Visible() || r.win.QuickInput.Visible() {
			t.Errorf("shown in the view %v, in the window %v", view.QuickInput.Visible(), r.win.QuickInput.Visible())
		}
		view.QuickInput.Hide()
	})
	t.Run("a list nobody asked for is in the window", func(t *testing.T) {
		pick(extension.HostOrigin)
		if r.win.QuickInput.Visible() == false || view.QuickInput.Visible() {
			t.Errorf("shown in the view %v, in the window %v", view.QuickInput.Visible(), r.win.QuickInput.Visible())
		}
		r.win.QuickInput.Hide()
	})
	t.Run("a box of a client is in its view and its answer comes back", func(t *testing.T) {
		var answered string
		r.win.extensionInputBox(exthost.InputBoxRequest{Prompt: "Name for the remote", Origin: view.origin, Accepted: func(v string) { answered = v }, Hidden: func() {}})
		r.pump()
		if !view.QuickInput.Visible() || r.win.QuickInput.Visible() {
			t.Fatalf("shown in the view %v, in the window %v", view.QuickInput.Visible(), r.win.QuickInput.Visible())
		}
		if !view.QuickInput.Prompting() {
			t.Fatal("the box of the view is not an input box")
		}
		view.QuickInput.SetValue("secret")
		view.QuickInput.Accept(quickinput.KeyMods{})
		r.pump() // the answer comes once the focus is back
		if answered != "secret" {
			t.Errorf("the answer is %q (visible %v)", answered, view.QuickInput.Visible())
		}
	})
	t.Run("a dialog of a client is in its view", func(t *testing.T) {
		r.win.extensionDialog(exthost.DialogRequest{
			Spec:   extension.DialogSpec{Message: "Sure?", Buttons: []extension.DialogButton{{Label: "Yes"}}},
			Origin: view.origin, Answered: func(extension.DialogResult) {},
		})
		if !view.Dialogs.IsShowing() || r.win.Dialogs.IsShowing() {
			t.Errorf("shown in the view %v, in the window %v", view.Dialogs.IsShowing(), r.win.Dialogs.IsShowing())
		}
	})
	t.Run("a question of a client that has gone is answered with a cancel", func(t *testing.T) {
		if h := pick(99); *h != 1 {
			t.Errorf("Hidden called %d times", *h)
		}
		var res extension.DialogResult
		r.win.extensionDialog(exthost.DialogRequest{
			Spec:   extension.DialogSpec{Message: "Sure?", Buttons: []extension.DialogButton{{Label: "Yes"}}},
			Origin: 99, Answered: func(x extension.DialogResult) { res = x },
		})
		if !res.Cancelled {
			t.Error("the dialog of a gone client was not cancelled")
		}
	})
}

func TestAToastOfAClientsCommandIsInItsViewOnly(t *testing.T) {
	r := newViewRig(t, "one\n", extension.Size{Width: 800, Height: 500})
	view := r.view.ws
	n := notify.Notification{Severity: notify.Info, Message: "Pushed", Origin: uint64(view.origin)}
	r.win.Notify.Notify(n)
	r.pump()
	if len(view.Notifications.Toasts()) != 1 || len(r.win.Notifications.Toasts()) != 0 {
		t.Errorf("toasts: view %d, window %d", len(view.Notifications.Toasts()), len(r.win.Notifications.Toasts()))
	}
}

// The dev commands that make the view of a client in one window (devview.go, SPEC §15.2): the view is made once, and the other two need it.
func TestTheDevCommandsOfTheViewNeedAViewAndMakeOneOnce(t *testing.T) {
	r := newViewRig(t, "one\n", extension.Size{Width: 400, Height: 300})
	r.frame(t)
	cc := commandContext(t, r.win)
	for _, id := range []string{"dev.view.run", "dev.view.type"} {
		err := r.win.Commands.Run(cc, id, map[string]any{"command": "workbench.view.scm", "text": "x"})
		appErr, ok := errors.FromError(err)
		if !ok || !appErr.HasRoute("devView") || appErr.Tag != "DevViewMissing" {
			t.Errorf("%s with no view: %v", id, err)
		}
	}
	if err := r.win.Commands.Run(cc, "dev.view.show", map[string]any{"width": 400.0, "height": 300.0}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.win.dev.view.Close() })
	appErr, ok := errors.FromError(r.win.Commands.Run(cc, "dev.view.show", nil))
	if !ok || appErr.Tag != "DevViewShown" || !appErr.HasRoute("devViewShow") {
		t.Errorf("a second view: %v", appErr)
	}
	if err := r.win.Commands.Run(cc, "dev.view.type", map[string]any{"text": "ab", "enter": true}); err != nil {
		t.Errorf("typing in the view: %v", err)
	}
}
