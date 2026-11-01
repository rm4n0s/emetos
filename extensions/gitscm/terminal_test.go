package gitscm

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
	"github.com/rm4n0s/emetos/extensions/gitscm/helper"
)

// Git in the terminals (step 24.23, SPEC §10.30 "Passwords and editors" Terminals): the variables the Git extension gives the terminals of
// the Terminal extension, over the fake API (terminal_auth_test.go has the real terminal).

// names are the names of the variables of a contribution.
func termNames(vars map[string]string) map[string]bool {
	out := map[string]bool{}
	for k := range vars {
		out[k] = true
	}
	return out
}

func TestTerminalsGetTheVariablesTheSettingsAllow(t *testing.T) {
	tests := []struct {
		name        string
		settings    map[string]any
		wantNames   []string
		description string
	}{
		{name: "the defaults: the askpass program only", wantNames: []string{"GIT_ASKPASS", helper.EnvSocket, helper.EnvToken},
			description: "Enables the following features: git auth provider"},
		{name: "terminalAuthentication off: nothing", settings: map[string]any{"git.terminalAuthentication": false}},
		{name: "useIntegratedAskPass off: nothing, whatever terminalAuthentication says", settings: map[string]any{"git.useIntegratedAskPass": false}},
		{name: "the editor needs terminalGitEditor", settings: map[string]any{"git.terminalGitEditor": true, "git.terminalAuthentication": false},
			wantNames: []string{"GIT_EDITOR", helper.EnvSocket, helper.EnvToken}, description: "Enables the following features: git editor"},
		{name: "the editor needs useEditorAsCommitInput too", settings: map[string]any{"git.terminalGitEditor": true, "git.terminalAuthentication": false, "git.useEditorAsCommitInput": false}},
		{name: "both", settings: map[string]any{"git.terminalGitEditor": true},
			wantNames: []string{"GIT_ASKPASS", "GIT_EDITOR", helper.EnvSocket, helper.EnvToken}, description: "Enables the following features: git auth provider, git editor"},
		{name: "git.enabled off: nothing", settings: map[string]any{"git.enabled": false}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, gittest.NewEnv(t), rigOptions{settings: tc.settings})
			if tc.settings["git.enabled"] != false {
				r.settle()
			}
			if r.x.helperSrv != nil {
				t.Fatal("the server of the programs is running before a terminal asked")
			}
			got := r.api.TerminalEnvironment().Contributions()
			if len(tc.wantNames) == 0 {
				if len(got) != 0 {
					t.Errorf("contributions %+v, want none", got)
				}
				if r.x.helperSrv != nil {
					t.Error("the server was started for nothing")
				}
				return
			}
			if len(got) != 1 || got[0].Extension != "emetos.gitscm" || got[0].Name != "Git" || got[0].Description != tc.description {
				t.Fatalf("contributions %+v", got)
			}
			have := termNames(got[0].Variables)
			if len(have) != len(tc.wantNames) {
				t.Errorf("variables %v, want %v", have, tc.wantNames)
			}
			for _, n := range tc.wantNames {
				if !have[n] {
					t.Errorf("no %s in %v", n, have)
				}
			}
			// no ssh program, no sequence editor: ssh and the todo list of a rebase stay the person's own
			for _, n := range []string{"SSH_ASKPASS", "SSH_ASKPASS_REQUIRE", "GIT_SEQUENCE_EDITOR", "GIT_TERMINAL_PROMPT"} {
				if have[n] {
					t.Errorf("%s is given to a terminal", n)
				}
			}
			srv := r.x.helperSrv
			if srv == nil {
				t.Fatal("the server did not start")
			}
			vars := got[0].Variables
			if vars[helper.EnvSocket] != srv.Socket() || vars[helper.EnvToken] != srv.Token() {
				t.Errorf("the socket and token are not the server's: %v", vars)
			}
			if a, ok := vars["GIT_ASKPASS"]; ok && a != srv.Script(helper.ScriptAskpass) {
				t.Errorf("GIT_ASKPASS = %q", a)
			}
			if e, ok := vars["GIT_EDITOR"]; ok && e != "'"+srv.Script(helper.ScriptEditor)+"'" {
				t.Errorf("GIT_EDITOR = %q", e)
			}
		})
	}
}

func TestTheVariablesFollowTheSettingsAtTheNextTerminal(t *testing.T) {
	r := newRig(t, gittest.NewEnv(t), rigOptions{})
	r.settle()
	if got := r.api.TerminalEnvironment().Contributions(); len(got) != 1 {
		t.Fatalf("contributions %+v", got)
	}
	r.api.SetSetting("git.terminalAuthentication", false)
	r.api.Pump()
	if got := r.api.TerminalEnvironment().Contributions(); len(got) != 0 {
		t.Errorf("after the setting went off: %+v", got)
	}
	r.api.SetSetting("git.terminalAuthentication", true)
	r.api.SetSetting("git.enabled", false)
	r.api.Pump()
	if got := r.api.TerminalEnvironment().Contributions(); len(got) != 0 {
		t.Errorf("with Git disabled: %+v", got)
	}
	r.api.SetSetting("git.enabled", true)
	r.api.Pump()
	if got := r.api.TerminalEnvironment().Contributions(); len(got) != 1 {
		t.Errorf("with Git enabled again: %+v", got)
	}
}

func TestTheVariablesGoWhenTheExtensionIsTurnedOff(t *testing.T) {
	r := newRig(t, gittest.NewEnv(t), rigOptions{})
	r.settle()
	if got := r.api.TerminalEnvironment().Contributions(); len(got) != 1 {
		t.Fatalf("contributions %+v", got)
	}
	r.api.Deactivate()
	if got := r.api.TerminalEnvironment().Contributions(); len(got) != 0 {
		t.Errorf("after the extension was turned off: %+v", got)
	}
}

func TestNoVariablesAfterTheWorkspaceClosed(t *testing.T) {
	r := newRig(t, gittest.NewEnv(t), rigOptions{})
	r.settle()
	if got := r.api.TerminalEnvironment().Contributions(); len(got) != 1 {
		t.Fatalf("contributions %+v", got)
	}
	r.api.Emit(extension.TopicWorkspaceClosed, extension.WorkspaceInfo{})
	r.api.Pump()
	if got := r.api.TerminalEnvironment().Contributions(); len(got) != 0 {
		t.Errorf("after the workspace closed: %+v", got)
	}
}

func TestAServerThatCannotStartGivesTheTerminalsNothingAndIsLoggedOnce(t *testing.T) {
	logs := recordLogs(t)
	e := gittest.NewEnv(t)
	r := newRig(t, e, rigOptions{})
	r.settle()
	file := filepath.Join(e.Root, "not-a-folder")
	writeFile(t, file, "x")
	r.x.opts.RuntimeDir = file
	for range 3 {
		if got := r.api.TerminalEnvironment().Contributions(); len(got) != 0 {
			t.Fatalf("contributions %+v although the server cannot start", got)
		}
	}
	if n := strings.Count(logs.String(), "cannot start"); n != 1 {
		t.Errorf("the failure is logged %d times:\n%s", n, logs.String())
	}
}

// Every command the manifest declares has a handler: the placeholder that answered "is not available yet" for the commands of the steps
// to come is gone (step 24.23).
func TestEveryCommandOfTheManifestHasAHandler(t *testing.T) {
	r := newRig(t, gittest.NewEnv(t), rigOptions{})
	for _, c := range r.x.Manifest().Contributes.Commands {
		if !r.api.HasHandler(c.ID) {
			t.Errorf("%s has no handler", c.ID)
		}
	}
}
