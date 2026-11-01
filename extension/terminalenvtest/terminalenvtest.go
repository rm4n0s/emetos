package terminalenvtest

import (
	"reflect"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// Harness is what an implementation gives the suite.
type Harness struct {
	// API is the API of the extension that contributes, in an activation that is on; ID and Name are its id and name.
	API      extension.API
	ID, Name string
	// Second is the API of another extension of the same scope (and the same registry of sources), also on; SecondID and SecondName are
	// its id and name. Anything may read the contributions with it.
	Second               extension.API
	SecondID, SecondName string
	// Deactivate ends the activation of API, as turning the extension off does.
	Deactivate func()
}

// Run runs the suite: newHarness makes a fresh harness for each test.
func Run(t *testing.T, newHarness func(t *testing.T) *Harness) {
	t.Helper()
	tests := []struct {
		name string
		run  func(t *testing.T, h *Harness)
	}{
		{"the sources are asked in the order they were registered, by every extension", testOrder},
		{"a source is asked when the contributions are read, not before", testLazy},
		{"a source that gives no valid variable gives no contribution; an invalid variable is left out", testInvalid},
		{"dispose takes a source away, also twice", testDispose},
		{"a nil source is refused", testNil},
		{"the end of the activation takes the sources away and Contribute fails", testDeactivate},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { tc.run(t, newHarness(t)) })
	}
}

func source(vars map[string]string, description string) extension.TerminalVariables {
	return func() (map[string]string, string) { return vars, description }
}

func testOrder(t *testing.T, h *Harness) {
	if _, err := h.API.TerminalEnvironment().Contribute(source(map[string]string{"A": "1", "B": "2"}, "first")); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Second.TerminalEnvironment().Contribute(source(map[string]string{"B": "3"}, "")); err != nil {
		t.Fatal(err)
	}
	want := []extension.TerminalContribution{
		{Extension: h.ID, Name: h.Name, Description: "first", Variables: map[string]string{"A": "1", "B": "2"}},
		{Extension: h.SecondID, Name: h.SecondName, Variables: map[string]string{"B": "3"}},
	}
	for _, api := range []extension.API{h.API, h.Second} {
		if got := api.TerminalEnvironment().Contributions(); !reflect.DeepEqual(got, want) {
			t.Errorf("contributions %+v, want %+v", got, want)
		}
	}
}

func testLazy(t *testing.T, h *Harness) {
	calls := 0
	value := "one"
	if _, err := h.API.TerminalEnvironment().Contribute(func() (map[string]string, string) {
		calls++
		return map[string]string{"LAZY": value}, ""
	}); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("the source was asked %d times before anyone read", calls)
	}
	value = "two"
	got := h.Second.TerminalEnvironment().Contributions()
	if calls != 1 || len(got) != 1 || got[0].Variables["LAZY"] != "two" {
		t.Errorf("asked %d times, contributions %+v", calls, got)
	}
	h.Second.TerminalEnvironment().Contributions()
	if calls != 2 {
		t.Errorf("asked %d times after two reads, want 2", calls)
	}
}

func testInvalid(t *testing.T, h *Harness) {
	env := h.API.TerminalEnvironment()
	for _, src := range []extension.TerminalVariables{
		source(nil, "nothing"),
		source(map[string]string{}, "empty"),
		source(map[string]string{"": "x", "A=B": "x", "NUL\x00": "x", "VALUE": "a\x00b"}, "all invalid"),
		source(map[string]string{"GOOD": "ok", "A=B": "x", "": "y", "BAD": "a\x00b"}, "some"),
	} {
		if _, err := env.Contribute(src); err != nil {
			t.Fatal(err)
		}
	}
	got := h.Second.TerminalEnvironment().Contributions()
	want := []extension.TerminalContribution{{Extension: h.ID, Name: h.Name, Description: "some", Variables: map[string]string{"GOOD": "ok"}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("contributions %+v, want %+v", got, want)
	}
}

func testDispose(t *testing.T, h *Harness) {
	env := h.API.TerminalEnvironment()
	d1, err := env.Contribute(source(map[string]string{"ONE": "1"}, ""))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.Contribute(source(map[string]string{"TWO": "2"}, "")); err != nil {
		t.Fatal(err)
	}
	d1()
	d1()
	got := h.Second.TerminalEnvironment().Contributions()
	if len(got) != 1 || got[0].Variables["TWO"] != "2" {
		t.Errorf("after dispose %+v", got)
	}
}

func testNil(t *testing.T, h *Harness) {
	_, err := h.API.TerminalEnvironment().Contribute(nil)
	appErr, ok := errors.FromError(err)
	if !ok || appErr.Tag != "ExtensionTerminalEnvironmentInvalid" {
		t.Errorf("error %v, want ExtensionTerminalEnvironmentInvalid", err)
	}
}

func testDeactivate(t *testing.T, h *Harness) {
	if _, err := h.API.TerminalEnvironment().Contribute(source(map[string]string{"GONE": "1"}, "")); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Second.TerminalEnvironment().Contribute(source(map[string]string{"STAYS": "1"}, "")); err != nil {
		t.Fatal(err)
	}
	h.Deactivate()
	got := h.Second.TerminalEnvironment().Contributions()
	if len(got) != 1 || got[0].Extension != h.SecondID {
		t.Errorf("after the end of the activation %+v", got)
	}
	_, err := h.API.TerminalEnvironment().Contribute(source(map[string]string{"LATE": "1"}, ""))
	appErr, ok := errors.FromError(err)
	if !ok || appErr.Tag != "ExtensionDeactivated" {
		t.Errorf("Contribute after the end: %v, want ExtensionDeactivated", err)
	}
}
