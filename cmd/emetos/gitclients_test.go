package main

import (
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extensions/gitscm"
	"github.com/rm4n0s/emetos/internal/core/command"
)

// Git for the client of a shared workspace (step 24.23, SPEC §10.30 "Security and privacy", D38): there is no refusal class for Git.

// No command of the Git extension, and none of the host's own commands about source control except the two that open a terminal, is
// refused to a client (D38: there is no refusal class for Git). The two that open a terminal are the Terminal's rule: a terminal runs
// programs on the computer of the host.
func TestNoGitCommandIsRefusedToAClient(t *testing.T) {
	var ids []string
	for _, c := range gitscm.New().Manifest().Contributes.Commands {
		ids = append(ids, c.ID)
	}
	if len(ids) < 100 {
		t.Fatalf("only %d commands: the manifest is not read", len(ids))
	}
	for _, c := range command.DefaultRegistry().All() {
		if strings.HasPrefix(c.ID, "scm.") || strings.HasPrefix(c.ID, "workbench.scm.") || c.ID == "workbench.view.scm" {
			ids = append(ids, c.ID)
		}
	}
	for _, id := range ids {
		ref, refused := command.ClientRefusal(id)
		switch id {
		case "scm.openInIntegratedTerminal", "scm.openInTerminal":
			if !refused || ref.Class != "terminal" {
				t.Errorf("%s is %+v, %v: want refused with the class terminal", id, ref, refused)
			}
		default:
			if refused {
				t.Errorf("%s is refused to clients (%s: %s), but Git has no refusal (D38)", id, ref.Class, ref.Reason)
			}
		}
	}
	for _, prefix := range command.RefusedPrefixes() {
		if strings.HasPrefix(prefix, "git.") || strings.HasPrefix(prefix, "scm.") || strings.HasPrefix(prefix, "workbench.scm.") {
			t.Errorf("the refusal list has the prefix %q", prefix)
		}
	}
	for _, id := range command.RefusedExactIDs() {
		if strings.HasPrefix(id, "git.") {
			t.Errorf("the refusal list has the command %q", id)
		}
	}
}
