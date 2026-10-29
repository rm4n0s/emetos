package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions"
	"github.com/rm4n0s/emetos/internal/core/command"
)

// outsideExtensions are the extensions whose commands reach outside the workspace: the container engines and the databases (SPEC
// §10.24, D25). A client of a shared workspace may run none of their commands.
var outsideExtensions = []string{"emetos.containers", "emetos.podman", "emetos.databases", "emetos.sql", "emetos.mongodb", "emetos.redis"}

// Every command the container and database extensions declare is refused to a client with the class outside, so a command added to
// one of them without a thought for the clients of a shared workspace fails here. (None of them adds a command at run time with Commands().Register.)
func TestEveryCommandOfTheContainerAndDatabaseExtensionsIsRefusedToClients(t *testing.T) {
	seen := map[string]int{}
	for _, f := range extensions.All() {
		m := f().Manifest()
		if !slices.Contains(outsideExtensions, m.ID) {
			continue
		}
		for _, c := range m.Contributes.Commands {
			seen[m.ID]++
			ref, refused := command.ClientRefusal(c.ID)
			if !refused || ref.Class != extension.RefusedOutside || ref.Reason == "" {
				t.Errorf("%s: the command %s is not refused to clients with the class outside: %+v, %v", m.ID, c.ID, ref, refused)
			}
		}
	}
	for _, id := range outsideExtensions {
		if seen[id] == 0 {
			t.Errorf("the extension %s declares no command, or is not in the list of extensions", id)
		}
	}
}

// The scan of the commands that look risky, over every extension: a command whose id names a database, a container engine or one of
// the things that run on them is refused, or listed here with the reason it may stay open.
func TestNoCommandOfAnExtensionThatLooksOutsideIsForgotten(t *testing.T) {
	risky := []string{"database", "container", "docker", "podman", "sql", "mongo", "redis", "kube", "quadlet", "pod."}
	// allowed on purpose: they act inside the workspace
	allowed := map[string]string{}
	for _, f := range extensions.All() {
		m := f().Manifest()
		for _, c := range m.Contributes.Commands {
			lower := strings.ToLower(c.ID)
			if !slices.ContainsFunc(risky, func(w string) bool { return strings.Contains(lower, w) }) {
				continue
			}
			if _, refused := command.ClientRefusal(c.ID); !refused && allowed[c.ID] == "" {
				t.Errorf("%s: the command %s looks like one that reaches outside the workspace and is not refused; refuse it in clientRules or allow it here with a reason", m.ID, c.ID)
			}
		}
	}
}
