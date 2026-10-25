package extension

import (
	"regexp"
	"strings"

	"github.com/rm4n0s/errors"
)

// idPattern is the form of an extension id: lowercase letters, digits and hyphens in segments joined by dots.
var idPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*(\.[a-z][a-z0-9-]*)*$`)

// CheckManifest checks a manifest the way Emetos does before it keeps an extension: the id, the name and the activation
// events and the topics it declares. An extension's own test can call it. The errors have the tags ExtensionIDInvalid,
// ExtensionIDReserved, ExtensionNameMissing, ExtensionEventInvalid (an unknown event, onStartup with an argument, an
// event that takes an argument without one), and EventTopicInvalid or EventPatternInvalid for a topic or pattern the
// manifest declares that is malformed.
func CheckManifest(m Manifest) error {
	if !idPattern.MatchString(m.ID) {
		return errors.New("ExtensionIDInvalid", "the id of an extension is lowercase, dotted and not empty (emetos.markdown)", "id", m.ID)
	}
	if reservedIDs[m.ID] {
		return errors.New("ExtensionIDReserved", "this id is kept for the events of Emetos", "id", m.ID)
	}
	if strings.TrimSpace(m.Name) == "" {
		return errors.New("ExtensionNameMissing", "an extension needs a name", "id", m.ID)
	}
	for _, e := range m.ActivationEvents {
		name, arg := SplitEvent(e)
		switch name {
		case "onStartup":
			if arg != "" || e != OnStartup {
				return errors.New("ExtensionEventInvalid", "onStartup takes no argument", "id", m.ID, "event", e)
			}
		case "onLanguage", "onFile", "onCommand", "onView", "onWindowTab":
			if arg == "" {
				return errors.New("ExtensionEventInvalid", "the activation event needs an argument", "id", m.ID, "event", e)
			}
		default:
			return errors.New("ExtensionEventInvalid", "unknown activation event (onStartup, onLanguage:, onFile:, onCommand:, onView: and onWindowTab:)", "id", m.ID, "event", e)
		}
	}
	if err := checkViews(m); err != nil {
		return err
	}
	return checkEventDecl(m)
}

// checkViews checks the views a manifest declares: every id is under the id of the extension and new, every view has a
// name, and every title action names a view of the extension and a command.
func checkViews(m Manifest) error {
	c := m.Contributes
	prefix := m.ID + "."
	ids := map[string]bool{}
	own := func(kind, id string) error {
		switch {
		case id == "" || len(id) <= len(prefix) || id[:len(prefix)] != prefix:
			return errors.New("ExtensionViewIDForeign", "the id of a container or a view starts with the id of the extension and a dot", "id", m.ID, "kind", kind, "view", id)
		case ids[id]:
			return errors.New("ExtensionViewDuplicate", "two containers or views of the extension have the same id", "id", m.ID, "kind", kind, "view", id)
		}
		ids[id] = true
		return nil
	}
	containers := map[string]bool{}
	for _, v := range c.ViewContainers {
		if err := own("container", v.ID); err != nil {
			return err
		}
		if strings.TrimSpace(v.Title) == "" {
			return errors.New("ExtensionViewInvalid", "a container needs a title", "id", m.ID, "view", v.ID)
		}
		containers[v.ID] = true
	}
	views := map[string]bool{}
	for _, v := range c.Views {
		if err := own("view", v.ID); err != nil {
			return err
		}
		if strings.TrimSpace(v.Name) == "" || v.Container == "" {
			return errors.New("ExtensionViewInvalid", "a view needs a name and a container", "id", m.ID, "view", v.ID)
		}
		views[v.ID] = true
	}
	for _, v := range c.PanelViews {
		if err := own("panel view", v.ID); err != nil {
			return err
		}
		if strings.TrimSpace(v.Title) == "" {
			return errors.New("ExtensionViewInvalid", "a panel view needs a title", "id", m.ID, "view", v.ID)
		}
		views[v.ID] = true
	}
	for _, a := range c.ViewActions {
		if !views[a.View] || a.Command == "" || a.Icon == "" {
			return errors.New("ExtensionViewActionInvalid", "a title action names a view of the extension, a command and an icon", "id", m.ID, "view", a.View, "command", a.Command)
		}
	}
	return nil
}

// checkEventDecl checks the topics and patterns that a manifest declares. The errors of CheckTopic and CheckPattern are
// returned as they are.
func checkEventDecl(m Manifest) error {
	for _, e := range m.Events.Publishes {
		if err := CheckTopic(e.Topic); err != nil {
			return err
		}
	}
	for _, t := range m.Events.Handles {
		if err := CheckTopic(t); err != nil {
			return err
		}
	}
	for _, p := range m.Events.Subscribes {
		if err := CheckPattern(p); err != nil {
			return err
		}
	}
	for _, p := range m.Events.CrossTab {
		if err := CheckPattern(p); err != nil {
			return err
		}
	}
	return nil
}
