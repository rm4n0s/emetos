package exthost

import (
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

func TestDialogAsksAndAnswersOnce(t *testing.T) {
	st := newServerTest(t, simple(), nil)
	tb := st.startTab("one")
	var got []extension.DialogResult
	spec := extension.DialogSpec{Type: extension.DialogWarning, Message: "Do you trust?", Detail: "Git can run programs.",
		Buttons: []extension.DialogButton{{Label: "Trust Repository"}, {Label: "Don't Trust", Cancel: true}}}
	if err := st.api.Dialogs().Ask(spec, func(r extension.DialogResult) { got = append(got, r) }); err != nil {
		t.Fatal(err)
	}
	if len(tb.dialogs) != 1 || tb.dialogs[0].Spec.Message != "Do you trust?" || len(tb.dialogs[0].Spec.Buttons) != 2 {
		t.Fatalf("dialogs = %+v", tb.dialogs)
	}
	req := tb.dialogs[0]
	req.Answered(extension.DialogResult{Button: 1, Cancelled: true})
	req.Answered(extension.DialogResult{Button: 0}) // a second answer is ignored
	if len(got) != 1 || got[0].Button != 1 || !got[0].Cancelled {
		t.Errorf("answers = %+v", got)
	}
	// no function to call is fine
	if err := st.api.Dialogs().Ask(spec, nil); err != nil {
		t.Fatal(err)
	}
	tb.dialogs[1].Answered(extension.DialogResult{})
}

func TestDialogRefusals(t *testing.T) {
	ok := []extension.DialogButton{{Label: "OK"}}
	tests := []struct {
		name  string
		spec  extension.DialogSpec
		noUI  bool
		route string
	}{
		{"no button", extension.DialogSpec{Message: "x"}, false, "dialogsAPI.Ask.ExtensionDialogInvalid"},
		{"two cancel buttons", extension.DialogSpec{Message: "x", Buttons: []extension.DialogButton{{Label: "a", Cancel: true}, {Label: "b", Cancel: true}}}, false, "dialogsAPI.Ask.ExtensionDialogInvalid"},
		{"no dialogs in the scope", extension.DialogSpec{Message: "x", Buttons: ok}, true, "dialogsAPI.Ask.ExtensionDialogsUnavailable"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := newServerTest(t, simple(), nil)
			if tc.noUI {
				st.rig.noDialog["one"] = true
			}
			st.startTab("one")
			wantRoute(t, st.api.Dialogs().Ask(tc.spec, nil), tc.route)
		})
	}
	st := newServerTest(t, simple(), nil)
	st.startTab("one")
	st.rig.host.Shutdown()
	wantRoute(t, st.api.Dialogs().Ask(extension.DialogSpec{Message: "x", Buttons: ok}, nil), "dialogsAPI.Ask.ExtensionDeactivated")
}

func TestADialogCallbackThatPanicsFailsTheExtension(t *testing.T) {
	st := newServerTest(t, simple(), nil)
	tb := st.startTab("one")
	if err := st.api.Dialogs().Ask(extension.DialogSpec{Message: "x", Buttons: []extension.DialogButton{{Label: "OK"}}}, func(extension.DialogResult) { panic("boom") }); err != nil {
		t.Fatal(err)
	}
	tb.dialogs[0].Answered(extension.DialogResult{})
	waitFor(t, tb, "the extension to fail", func() bool { return tb.scope.State(testExt) == StateFailed })
	appErr, ok := errors.FromError(tb.scope.Failure(testExt).Err)
	if !ok || appErr.Tag != "ExtensionDialogCallbackPanicked" || !appErr.HasRoute("Scope.runDialogDone.func1.ExtensionDialogCallbackPanicked") {
		t.Errorf("failure = %v", tb.scope.Failure(testExt).Err)
	}
}
