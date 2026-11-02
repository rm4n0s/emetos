package debug

import (
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/command"
	coredebug "github.com/rm4n0s/emetos/internal/core/debug"
)

func TestTheKindsOfRowsNameTheirItemTypes(t *testing.T) {
	tests := []struct {
		kind RowKind
		want string
	}{
		{RowSession, "session"}, {RowThread, "thread"}, {RowFrame, "stackFrame"}, {RowExpression, "expression"}, {RowVariable, "variable"},
		{RowScope, "variable"}, {RowBreakpoint, "breakpoint"}, {RowFunctionBreakpoint, "functionBreakpoint"}, {RowException, "exceptionBreakpoint"},
		{RowMessage, ""}, {RowSource, ""},
	}
	for _, tc := range tests {
		if got := (Row{Kind: tc.kind}).ItemType(); got != tc.want {
			t.Errorf("kind %d: item type %q, want %q", tc.kind, got, tc.want)
		}
	}
}

func TestClassOfAValue(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		typ       string
		available bool
		changed   bool
		want      valueClass
	}{
		{"a number", "42", "", true, false, classNumber},
		{"a boolean", "true", "", true, false, classBoolean},
		{"a quoted string", `"x"`, "", true, false, classString},
		{"a changed value", "7", "", true, true, classChanged},
		{"an error", "boom", "", false, false, classError},
		{"a node that was not evaluated", coredebug.NotAvailable, "", false, false, classValue},
		{"the declared type wins", "abc", "number", true, false, classNumber},
		{"plain text", "main.Order", "", true, false, classValue},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := classOf(tc.value, tc.typ, tc.available, tc.changed); got != tc.want {
				t.Errorf("classOf = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestTheWelcomeContent(t *testing.T) {
	tests := []struct {
		name  string
		state WelcomeState
		has   []string
		lacks []string
	}{
		{"a debugger and a folder", WelcomeState{DebuggersAvailable: true, ExtensionAvailable: true, InterestedInActiveEditor: true},
			[]string{command.DebugStartCommand, "create a launch.json file"}, []string{"Open a file", "disabled"}},
		{"a debugger but no editor it serves", WelcomeState{DebuggersAvailable: true, ExtensionAvailable: true},
			[]string{"Open a file", command.DebugStartCommand}, nil},
		{"no folder", WelcomeState{DebuggersAvailable: true, ExtensionAvailable: true, InterestedInActiveEditor: true, NoFolder: true},
			[]string{"open a folder"}, []string{"command:" + command.DebugConfigure}},
		{"no debugger", WelcomeState{}, []string{NoDebuggerText}, []string{command.DebugStartCommand}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var all strings.Builder
			for _, p := range WelcomePieces(tc.state) {
				all.WriteString(p.Contents + "\n")
			}
			text := all.String()
			for _, s := range tc.has {
				if !strings.Contains(text, s) {
					t.Errorf("the welcome lacks %q: %s", s, text)
				}
			}
			for _, s := range tc.lacks {
				if strings.Contains(text, s) {
					t.Errorf("the welcome has %q: %s", s, text)
				}
			}
		})
	}
}

func TestTheStatusBarTakesTheColorsOnlyWhileASessionRuns(t *testing.T) {
	tests := []struct {
		name  string
		state coredebug.State
		want  bool
	}{
		{"inactive", coredebug.Inactive, false}, {"starting", coredebug.Initializing, false}, {"stopped", coredebug.Stopped, false},
	}
	for _, tc := range tests {
		// with no session in the list, nothing runs whatever the state says
		if got := InDebugMode(tc.state, nil); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestTheToolbarIsAsWideAsItsButtons(t *testing.T) {
	if a, b := ToolbarWidth(3), ToolbarWidth(6); b <= a {
		t.Errorf("widths %v and %v", a, b)
	}
}
