package exthost

import (
	"slices"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// privilegedExtensions is the fixed allowlist of the extensions that may declare privileges, and which ones each may declare
// (SPEC §10.19 "Privileges"). It is read-only: nothing, no setting and no manifest, adds to it.
var privilegedExtensions = map[string][]string{
	extension.TerminalID: {extension.PrivilegeCommandsExecute},
}

// validateManifest checks a manifest (extension.CheckManifest, whose error is returned as it is), that no extension of the
// host has its id already (seen), and that the privileges it declares are its to declare. The duplicate fails with the tag
// ExtensionIDDuplicate and a privilege of an extension that is not on the allowlist with ExtensionPrivilegeRefused: the
// extension is left out as a whole (it is not installed and never activated), and the host lists it in Problems, which the
// Extensions page shows.
func validateManifest(m extension.Manifest, seen map[string]bool) error {
	if err := extension.CheckManifest(m); err != nil {
		return err
	}
	if seen[m.ID] {
		return errors.New("ExtensionIDDuplicate", "two extensions have the same id", "id", m.ID)
	}
	for _, p := range m.Privileges {
		if slices.Contains(privilegedExtensions[m.ID], p) {
			continue
		}
		if _, listed := privilegedExtensions[m.ID]; listed {
			return errors.New("ExtensionPrivilegeRefused", "this extension may declare only the privileges the host lists for it (emetos.terminal: commands.execute)", "id", m.ID, "privileges", slices.Clone(m.Privileges))
		}
		return errors.New("ExtensionPrivilegeRefused", "only emetos.terminal may declare privileges", "id", m.ID, "privileges", slices.Clone(m.Privileges))
	}
	return nil
}

// checkTopicOwnership checks that the topics an extension declares to publish or handle are not under the id of another
// extension (ids), and that a topic of Emetos ("emetos." and more) is under the id of the extension itself: the core
// events are Emetos's. The failure has the tag ExtensionTopicForeign.
func checkTopicOwnership(m extension.Manifest, ids []string) error {
	topics := make([]string, 0, len(m.Events.Publishes)+len(m.Events.Handles))
	for _, e := range m.Events.Publishes {
		topics = append(topics, e.Topic)
	}
	topics = append(topics, m.Events.Handles...)
	for _, topic := range topics {
		if strings.HasPrefix(topic, m.ID+".") {
			continue
		}
		if strings.HasPrefix(topic, "emetos.") {
			return errors.New("ExtensionTopicForeign", "a topic under emetos. belongs to Emetos, unless it is under the id of the extension", "id", m.ID, "topic", topic)
		}
		for _, id := range ids {
			if id != m.ID && strings.HasPrefix(topic, id+".") {
				return errors.New("ExtensionTopicForeign", "a topic under the id of another extension is that extension's", "id", m.ID, "topic", topic, "owner", id)
			}
		}
	}
	return nil
}
