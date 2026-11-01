package exthost

import (
	"context"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/terminalenvtest"
)

// terminalEnvTwo makes a tab with two extensions that share the scope's sources, and gives their APIs.
func terminalEnvTwo(t *testing.T) (tb *tab, first, second extension.API) {
	t.Helper()
	fa, _ := factory(manifest("emetos.envtest"), func(f *fake) {
		f.activate = func(_ context.Context, a extension.API) error { first = a; return nil }
	})
	fb, _ := factory(manifest("emetos.envother"), func(f *fake) {
		f.activate = func(_ context.Context, a extension.API) error { second = a; return nil }
	})
	r := newRig(t, fa, fb)
	tb = r.newTab("one")
	tb.scope.Start()
	if first == nil || second == nil {
		t.Fatal("the extensions were not activated")
	}
	return tb, first, second
}

// TestTerminalEnvironmentConformance runs the suite that the fake of the tests of the extensions passes too.
func TestTerminalEnvironmentConformance(t *testing.T) {
	terminalenvtest.Run(t, func(t *testing.T) *terminalenvtest.Harness {
		tb, first, second := terminalEnvTwo(t)
		return &terminalenvtest.Harness{
			API: first, ID: "emetos.envtest", Name: "Ext emetos.envtest",
			Second: second, SecondID: "emetos.envother", SecondName: "Ext emetos.envother",
			Deactivate: func() { tb.disabled = []string{"emetos.envtest"}; tb.scope.Refresh() },
		}
	})
}

func TestTerminalEnvironmentFailsWithItsOwnTags(t *testing.T) {
	_, first, _ := terminalEnvTwo(t)
	_, err := first.TerminalEnvironment().Contribute(nil)
	mustRoute(t, err, "terminalEnvAPI.Contribute.ExtensionTerminalEnvironmentInvalid")
}

func TestASourceThatPanicsFailsItsExtensionAndGivesNothing(t *testing.T) {
	tb, first, second := terminalEnvTwo(t)
	if _, err := first.TerminalEnvironment().Contribute(func() (map[string]string, string) { panic("boom") }); err != nil {
		t.Fatal(err)
	}
	if _, err := second.TerminalEnvironment().Contribute(func() (map[string]string, string) { return map[string]string{"OK": "1"}, "" }); err != nil {
		t.Fatal(err)
	}
	got := second.TerminalEnvironment().Contributions()
	if len(got) != 1 || got[0].Extension != "emetos.envother" {
		t.Errorf("contributions %+v", got)
	}
	tb.pump()
	f := tb.scope.Failure("emetos.envtest")
	if f == nil {
		t.Fatal("the extension did not fail")
	}
	wantRoute(t, f.Err, "terminalSource.ask.func1.ExtensionTerminalEnvironmentPanicked")
	if got := second.TerminalEnvironment().Contributions(); len(got) != 1 {
		t.Errorf("after the failure %+v", got)
	}
}
