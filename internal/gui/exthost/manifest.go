package exthost

import (
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// validateManifest checks a manifest (extension.CheckManifest, whose error is returned as it is) and that no extension
// of the host has its id already (seen). The duplicate fails with the tag ExtensionIDDuplicate.
func validateManifest(m extension.Manifest, seen map[string]bool) error {
	if err := extension.CheckManifest(m); err != nil {
		return err
	}
	if seen[m.ID] {
		return errors.New("ExtensionIDDuplicate", "two extensions have the same id", "id", m.ID)
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
