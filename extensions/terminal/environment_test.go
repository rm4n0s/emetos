package terminal

import (
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

func TestApplyContributions(t *testing.T) {
	tests := []struct {
		name string
		env  []string
		list []extension.TerminalContribution
		want []string
	}{
		{"nothing", []string{"A=1"}, nil, []string{"A=1"}},
		{"a new variable is added", []string{"A=1"}, []extension.TerminalContribution{{Variables: map[string]string{"B": "2"}}}, []string{"A=1", "B=2"}},
		{"an existing variable is replaced", []string{"A=1", "B=2"}, []extension.TerminalContribution{{Variables: map[string]string{"A": "9"}}}, []string{"A=9", "B=2"}},
		{"a later contribution wins, the variables of one go by name", nil, []extension.TerminalContribution{
			{Variables: map[string]string{"Z": "1", "Y": "1", "X": "first"}}, {Variables: map[string]string{"X": "second"}}},
			[]string{"X=second", "Y=1", "Z=1"}},
		{"a value with an equals sign and a prefix of another name", []string{"AB=1"}, []extension.TerminalContribution{{Variables: map[string]string{"A": "x=y"}}}, []string{"AB=1", "A=x=y"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := applyContributions(slices.Clone(tc.env), tc.list); !slices.Equal(got, tc.want) {
				t.Errorf("env %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEnvironmentHover(t *testing.T) {
	tests := []struct {
		name string
		list []extension.TerminalContribution
		want string
	}{
		{"no extension, no lines", nil, ""},
		{"a name and a description", []extension.TerminalContribution{{Extension: "emetos.gitscm", Name: "Git", Description: "Enables the following features: git auth provider", Variables: map[string]string{"TOKEN": "secret"}}},
			"\nThe following extensions have contributed to this terminal's environment:\n- Git: Enables the following features: git auth provider"},
		{"no description, no name", []extension.TerminalContribution{{Extension: "x.y", Variables: map[string]string{"A": "1"}}},
			"\nThe following extensions have contributed to this terminal's environment:\n- x.y"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := environmentHover(tc.list)
			if got != tc.want {
				t.Errorf("hover %q, want %q", got, tc.want)
			}
			if strings.Contains(got, "secret") {
				t.Error("the hover shows a value")
			}
		})
	}
}

// contributor makes the fake API of a second extension that shares the registry of the sources with the terminal's.
func contributor(t *testing.T, r *extRig) *fakeapi.API {
	t.Helper()
	m := extension.Manifest{ID: "emetos.gitscm", Name: "Git"}
	other := fakeapi.New(t, m, r.api.Scope())
	shared := fakeapi.NewTerminalSources()
	r.api.ShareTerminalEnvironment(shared)
	other.ShareTerminalEnvironment(shared)
	return other
}

func TestATerminalStartsWithWhatTheExtensionsContribute(t *testing.T) {
	r := newExtRig(t, "")
	other := contributor(t, r)
	r.run(CmdNew, nil)
	before := r.ext.target()
	if before == nil {
		t.Fatal("no terminal")
	}
	enabled := true
	dispose, err := other.TerminalEnvironment().Contribute(func() (map[string]string, string) {
		if !enabled {
			return nil, ""
		}
		return map[string]string{"GIT_ASKPASS": "/run/emetos/askpass.sh", "EMETOS_GIT_TOKEN": "tok"}, "Enables the following features: git auth provider"
	})
	if err != nil {
		t.Fatal(err)
	}
	env := func(e *entry) []string { return e.term.Environ() }
	if slices.Contains(env(before), "GIT_ASKPASS=/run/emetos/askpass.sh") {
		t.Error("a terminal that was running got the variable")
	}
	r.run(CmdNew, nil)
	after := r.ext.target()
	if after == before {
		t.Fatal("no new terminal")
	}
	for _, want := range []string{"GIT_ASKPASS=/run/emetos/askpass.sh", "EMETOS_GIT_TOKEN=tok"} {
		if !slices.Contains(env(after), want) {
			t.Errorf("the new terminal lacks %s", want)
		}
	}
	hover := r.ext.hoverText(after)
	if !strings.Contains(hover, "\n- Git: Enables the following features: git auth provider") || strings.Contains(hover, "tok") {
		t.Errorf("hover %q", hover)
	}
	if strings.Contains(r.ext.hoverText(before), "contributed") {
		t.Error("the hover of the older terminal names the extension")
	}
	// the source says no (a setting turned off): the next terminal has nothing; so for one disposed
	enabled = false
	r.run(CmdNew, nil)
	if third := r.ext.target(); slices.Contains(env(third), "GIT_ASKPASS=/run/emetos/askpass.sh") || strings.Contains(r.ext.hoverText(third), "contributed") {
		t.Error("a source that gives nothing still reached a terminal")
	}
	enabled = true
	dispose()
	r.run(CmdNew, nil)
	if fourth := r.ext.target(); slices.Contains(env(fourth), "GIT_ASKPASS=/run/emetos/askpass.sh") {
		t.Error("a disposed source still reached a terminal")
	}
}
