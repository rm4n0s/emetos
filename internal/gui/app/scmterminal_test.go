package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/scm"
)

// Open in Integrated Terminal and Open in External Terminal of a repository (step 24.23).

// runWith runs a command with the arguments a menu of a repository gives it.
func runWith(t *testing.T, w *Workspace, id string, args any) error {
	t.Helper()
	return w.Commands.Run(command.Context{Ctx: context.Background(), Source: command.SourceMenu}, id, args)
}

func repoArgs(s *scm.Source) any { return []any{map[string]any{"sourceControl": s.Key()}} }

func TestOpenInIntegratedTerminalAsksTheTerminalForAFolder(t *testing.T) {
	w := settingsWindow(t, nil)
	t.Cleanup(w.release)
	root := t.TempDir()
	a := w.SCM.Add(scm.SourceSpec{ProviderID: "git", Label: "Git", RootURI: "file://" + root})
	b := w.SCM.Add(scm.SourceSpec{ProviderID: "git", Label: "Git", RootURI: "file:///work/b"})
	var got []any
	if err := w.Commands.Register(command.Meta{ID: terminalNewCommand}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Commands.SetHandler(terminalNewCommand, func(_ command.Context, args any) error { got = append(got, args); return nil }); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		args any
		want []any
	}{
		{"no source control: nothing", nil, nil},
		{"a source control that is not there: nothing", []any{map[string]any{"sourceControl": "nope"}}, nil},
		{"one repository", repoArgs(a), []any{map[string]any{"cwd": root}}},
		{"several: the first the model knows", []any{map[string]any{"sourceControl": "nope"}, map[string]any{"sourceControl": b.Key()}, map[string]any{"sourceControl": a.Key()}},
			[]any{map[string]any{"cwd": "/work/b"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got = nil
			if err := runWith(t, w, "scm.openInIntegratedTerminal", tc.args); err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tc.want) || (len(got) == 1 && got[0].(map[string]any)["cwd"] != tc.want[0].(map[string]any)["cwd"]) {
				t.Errorf("the terminal was asked %v, want %v", got, tc.want)
			}
		})
	}
}

func TestOpenInIntegratedTerminalSaysWhenTheTerminalIsOff(t *testing.T) {
	w := settingsWindow(t, nil)
	t.Cleanup(w.release)
	s := w.SCM.Add(scm.SourceSpec{ProviderID: "git", Label: "Git", RootURI: "file:///work/a"})
	if err := runWith(t, w, "scm.openInIntegratedTerminal", repoArgs(s)); err != nil {
		t.Fatal(err)
	}
	items := w.Notify.Items()
	if len(items) != 1 || !strings.Contains(items[0].Message, "The terminal is turned off") {
		t.Errorf("notifications %+v", items)
	}
}

func TestOpenInTerminalStartsTheProgramOfTheSettingInTheFolder(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	program := filepath.Join(dir, "fake-terminal")
	if err := os.WriteFile(program, []byte("#!/bin/sh\npwd > '"+out+".tmp'\nmv '"+out+".tmp' '"+out+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	w := settingsWindow(t, map[string]any{"terminal.external.linuxExec": program})
	t.Cleanup(w.release)
	root := t.TempDir()
	s := w.SCM.Add(scm.SourceSpec{ProviderID: "git", Label: "Git", RootURI: "file://" + root})
	if err := runWith(t, w, "scm.openInTerminal", repoArgs(s)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		data, err := os.ReadFile(out)
		if err == nil {
			want, _ := filepath.EvalSymlinks(root)
			got, _ := filepath.EvalSymlinks(strings.TrimSpace(string(data)))
			if got != want {
				t.Errorf("the terminal started in %q, want %q", got, want)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the program did not run")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if n := len(w.Notify.Items()); n != 0 {
		t.Errorf("%d notifications", n)
	}
	// nothing without a repository
	if err := runWith(t, w, "scm.openInTerminal", nil); err != nil {
		t.Fatal(err)
	}
}

func TestOpenInTerminalSaysWhenTheProgramCannotStart(t *testing.T) {
	w := settingsWindow(t, map[string]any{"terminal.external.linuxExec": "emetos-no-such-terminal"})
	t.Cleanup(w.release)
	s := w.SCM.Add(scm.SourceSpec{ProviderID: "git", Label: "Git", RootURI: "file://" + t.TempDir()})
	if err := runWith(t, w, "scm.openInTerminal", repoArgs(s)); err != nil {
		t.Fatal(err)
	}
	items := w.Notify.Items()
	if len(items) != 1 || !strings.Contains(items[0].Message, "The external terminal could not be opened") {
		t.Errorf("notifications %+v", items)
	}
}

// TestTheMenuRowsFollowTheKindOfTerminal checks the rows of the context menu of a repository and the setting that chooses them.
func TestTheMenuRowsFollowTheKindOfTerminal(t *testing.T) {
	menus := command.DefaultMenus(command.DefaultRegistry())
	tests := []struct {
		kind                 string
		integrated, external bool
	}{
		{"integrated", true, false}, {"external", false, true}, {"both", true, true}, {"", false, false},
	}
	for _, tc := range tests {
		t.Run(tc.kind, func(t *testing.T) {
			keys := contextkey.MapContext{"scmProviderHasRootUri": true, "config.terminal.sourceControlRepositoriesKind": tc.kind}
			shown := map[string]bool{}
			for _, it := range menus.All() {
				if it.Menu == command.SCMSourceControl && (it.Command == "scm.openInIntegratedTerminal" || it.Command == "scm.openInTerminal") {
					shown[it.Command] = it.When == nil || it.When.Evaluate(keys)
					if it.Group != "99_terminal" {
						t.Errorf("%s is in the group %q", it.Command, it.Group)
					}
				}
			}
			if shown["scm.openInIntegratedTerminal"] != tc.integrated || shown["scm.openInTerminal"] != tc.external {
				t.Errorf("shown %v, want integrated %v external %v", shown, tc.integrated, tc.external)
			}
			keys["scmProviderHasRootUri"] = false
			for _, it := range menus.All() {
				if it.Menu == command.SCMSourceControl && strings.HasPrefix(it.Command, "scm.openIn") && it.When != nil && it.When.Evaluate(keys) {
					t.Errorf("%s is shown for a provider with no root", it.Command)
				}
			}
		})
	}
}
