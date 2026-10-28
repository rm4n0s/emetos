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
// manifest declares that is malformed, and ExtensionWindowTabIDForeign, ExtensionWindowTabDuplicate and
// ExtensionWindowTabInvalid for the window tab kinds, and ExtensionServerInvalid and ExtensionServerDuplicate for the language servers, and ExtensionSettingDeclInvalid and
// ExtensionSettingDeclDuplicate for the settings it declares, and ExtensionPrivilegeUnknown for a privilege that is none of the two.
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
		case "onLanguage", "onFile", "onCommand", "onView", "onEditor", "onWindowTab":
			if arg == "" {
				return errors.New("ExtensionEventInvalid", "the activation event needs an argument", "id", m.ID, "event", e)
			}
		default:
			return errors.New("ExtensionEventInvalid", "unknown activation event (onStartup, onLanguage:, onFile:, onCommand:, onView:, onEditor: and onWindowTab:)", "id", m.ID, "event", e)
		}
	}
	if err := checkViews(m); err != nil {
		return err
	}
	if err := checkEditors(m); err != nil {
		return err
	}
	if err := checkWindowTabs(m); err != nil {
		return err
	}
	if err := checkLanguageServers(m); err != nil {
		return err
	}
	if err := checkSettings(m); err != nil {
		return err
	}
	if err := checkContextKeys(m); err != nil {
		return err
	}
	if err := checkPrivileges(m); err != nil {
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

// checkEditors checks the editor kinds, title actions and file views a manifest declares: every id is under the id of the
// extension and new, a kind has a name, an action has a command and an icon, and a view has a name and selects files.
func checkEditors(m Manifest) error {
	c := m.Contributes
	prefix := m.ID + "."
	ids := map[string]bool{}
	own := func(kind, id string) error {
		switch {
		case id == "" || len(id) <= len(prefix) || id[:len(prefix)] != prefix:
			return errors.New("ExtensionEditorIDForeign", "the id of an editor kind or a file view starts with the id of the extension and a dot", "id", m.ID, "kind", kind, "editor", id)
		case ids[id]:
			return errors.New("ExtensionEditorDuplicate", "two editor kinds or file views of the extension have the same id", "id", m.ID, "kind", kind, "editor", id)
		}
		ids[id] = true
		return nil
	}
	for _, k := range c.EditorKinds {
		if err := own("editor kind", k.ID); err != nil {
			return err
		}
		if strings.TrimSpace(k.Name) == "" {
			return errors.New("ExtensionEditorInvalid", "an editor kind needs a name", "id", m.ID, "editor", k.ID)
		}
	}
	for _, a := range c.EditorActions {
		if a.Command == "" || a.Icon == "" {
			return errors.New("ExtensionEditorActionInvalid", "an editor title action needs a command and an icon", "id", m.ID, "command", a.Command)
		}
	}
	for _, v := range c.FileViews {
		if err := own("file view", v.ID); err != nil {
			return err
		}
		if strings.TrimSpace(v.Name) == "" || (len(v.Languages) == 0 && len(v.Globs) == 0) {
			return errors.New("ExtensionEditorInvalid", "a file view needs a name and the languages or globs of the files it is for", "id", m.ID, "editor", v.ID)
		}
	}
	return nil
}

// checkWindowTabs checks the window tab kinds a manifest declares: every id is under the id of the extension and new,
// every kind has a title.
func checkWindowTabs(m Manifest) error {
	prefix := m.ID + "."
	seen := map[string]bool{}
	for _, k := range m.Contributes.WindowTabKinds {
		switch {
		case k.ID == "" || len(k.ID) <= len(prefix) || k.ID[:len(prefix)] != prefix:
			return errors.New("ExtensionWindowTabIDForeign", "the id of a window tab kind starts with the id of the extension and a dot", "id", m.ID, "kind", k.ID)
		case seen[k.ID]:
			return errors.New("ExtensionWindowTabDuplicate", "two window tab kinds of the extension have the same id", "id", m.ID, "kind", k.ID)
		case strings.TrimSpace(k.Title) == "":
			return errors.New("ExtensionWindowTabInvalid", "a window tab kind needs a title", "id", m.ID, "kind", k.ID)
		}
		seen[k.ID] = true
	}
	return nil
}

// contextKeyPattern is the name of a context key an extension declares without its id: a word of letters and digits.
var contextKeyPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*$`)

// checkContextKeys checks the context keys a manifest declares: every name is a word and none is listed twice.
func checkContextKeys(m Manifest) error {
	seen := map[string]bool{}
	for _, key := range m.Contributes.ContextKeys {
		switch {
		case !contextKeyPattern.MatchString(key):
			return errors.New("ExtensionContextKeyDeclInvalid", "a context key that an extension declares is a word of letters and digits", "id", m.ID, "key", key)
		case seen[key]:
			return errors.New("ExtensionContextKeyDeclDuplicate", "two context keys of the extension have the same name", "id", m.ID, "key", key)
		}
		seen[key] = true
	}
	return nil
}

// settingTypeNames are the JSON types a SettingSpec may have.
var settingTypeNames = map[string]bool{"string": true, "number": true, "integer": true, "boolean": true, "array": true, "object": true}

// ValidSettingType tells whether the type is one a SettingSpec may have: string, number, integer, boolean, array or object.
func ValidSettingType(typ string) bool { return settingTypeNames[typ] }

// checkSettings checks the settings a manifest declares: every key is a dotted name and new, and every type is a JSON type.
func checkSettings(m Manifest) error {
	seen := map[string]bool{}
	for _, s := range m.Contributes.Settings {
		key := s.Key
		switch {
		case key == "" || strings.HasPrefix(key, ".") || strings.HasSuffix(key, ".") || strings.Contains(key, ".."):
			return errors.New("ExtensionSettingDeclInvalid", "the key of a setting is a dotted name", "id", m.ID, "key", key)
		case !ValidSettingType(s.Type):
			return errors.New("ExtensionSettingDeclInvalid", "the type of a setting is string, number, integer, boolean, array or object", "id", m.ID, "key", key, "type", s.Type)
		case seen[key]:
			return errors.New("ExtensionSettingDeclDuplicate", "two settings of the extension have the same key", "id", m.ID, "key", key)
		}
		seen[key] = true
	}
	return nil
}
