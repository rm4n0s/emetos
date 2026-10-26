package exthost

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/notify"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/workbench"
)

// api is the extension.API of one activation: every contribution goes through it and is tracked by the instance, so that
// the end of the activation takes it away. After the end every method that changes something fails with
// ExtensionDeactivated.
type api struct{ inst *instance }

var _ extension.API = (*api)(nil)

// Scope implements extension.API.
func (a *api) Scope() extension.ScopeInfo { return a.inst.scope.opts.Info }

// Log implements extension.API.
func (a *api) Log() *slog.Logger { return a.inst.log }

// Post implements extension.API.
func (a *api) Post(fn func()) {
	in := a.inst
	if fn == nil || in.isDead() {
		return
	}
	in.scope.post(func() { in.scope.runPosted(in, fn) })
}

// Commands implements extension.API.
func (a *api) Commands() extension.Commands { return commandsAPI{a.inst} }

// Keybindings implements extension.API.
func (a *api) Keybindings() extension.Keybindings { return keybindingsAPI{a.inst} }

// Menus implements extension.API.
func (a *api) Menus() extension.Menus { return menusAPI{a.inst} }

// Settings implements extension.API.
func (a *api) Settings() extension.Settings { return settingsAPI{a.inst} }

// Notifications implements extension.API.
func (a *api) Notifications() extension.Notifications { return notificationsAPI{a.inst} }

// StatusBar implements extension.API.
func (a *api) StatusBar() extension.StatusBar { return statusBarAPI{a.inst} }

// Documents implements extension.API.
func (a *api) Documents() extension.Documents { return documentsAPI{a.inst} }

// ---- commands ----

type commandsAPI struct{ inst *instance }

// Handle implements extension.Commands. It fails with ExtensionCommandNotDeclared when the manifest does not declare
// the command.
func (c commandsAPI) Handle(id string, handler extension.CommandHandler) error {
	in := c.inst
	if in.isDead() {
		return errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	declared := false
	for _, spec := range in.slot.info.Manifest.Contributes.Commands {
		declared = declared || spec.ID == id
	}
	if !declared {
		return errors.New("ExtensionCommandNotDeclared", "the manifest of the extension does not declare this command; use Register", "extension", in.id, "command", id)
	}
	in.scope.mu.Lock()
	in.slot.handlers[id] = handler
	in.scope.mu.Unlock()
	return nil
}

// Register implements extension.Commands. The registry's errors (CommandDuplicate, CommandIDInvalid) are returned as
// they are.
func (c commandsAPI) Register(spec extension.CommandSpec, handler extension.CommandHandler) error {
	in := c.inst
	if in.isDead() {
		return errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	meta, err := commandMeta(spec)
	if err != nil {
		return err
	}
	reg := in.scope.opts.Commands
	if err := reg.Register(meta); err != nil {
		return err
	}
	id := spec.ID
	remove, err := reg.SetHandler(id, func(cc command.Context, args any) error {
		return in.scope.runCommand(in, id, handler, cc, args)
	})
	if err != nil {
		reg.Unregister(id)
		return err
	}
	in.track(func() { remove(); reg.Unregister(id) })
	return nil
}

// ---- keybindings and menus ----

type keybindingsAPI struct{ inst *instance }

// Add implements extension.Keybindings.
func (k keybindingsAPI) Add(b extension.Keybinding) error {
	in := k.inst
	if in.isDead() {
		return errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	remove, err := in.scope.addKeybinding(b)
	if err != nil {
		return err
	}
	in.track(remove)
	return nil
}

type menusAPI struct{ inst *instance }

// Add implements extension.Menus.
func (m menusAPI) Add(item extension.MenuItem) error {
	in := m.inst
	if in.isDead() {
		return errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	remove, err := in.scope.addMenu(item)
	if err != nil {
		return err
	}
	in.track(remove)
	return nil
}

// ---- settings ----

type settingsAPI struct{ inst *instance }

// settingTypes are the names of the types of a SettingSpec.
var settingTypes = map[string]settings.Type{
	"string": settings.String, "number": settings.Number, "integer": settings.Integer,
	"boolean": settings.Boolean, "array": settings.Array, "object": settings.Object,
}

// Register implements extension.Settings. It fails with ExtensionSettingKeyForeign when the key is not under the id of
// the extension, ExtensionSettingTypeInvalid for a type that is not a JSON type, ExtensionSettingDuplicate when the
// activation registered the key already and ExtensionSettingTaken when another extension did; the registry's errors
// (SettingDuplicate for a setting of the editor, SettingDefaultInvalid) are returned as they are.
func (s settingsAPI) Register(spec extension.SettingSpec) error {
	in := s.inst
	if in.isDead() {
		return errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	if !strings.HasPrefix(spec.Key, in.id+".") {
		return errors.New("ExtensionSettingKeyForeign", "the key of a setting of an extension starts with the id of the extension and a dot", "extension", in.id, "key", spec.Key)
	}
	if _, ok := settingTypes[spec.Type]; !ok {
		return errors.New("ExtensionSettingTypeInvalid", "the type of a setting is string, number, integer, boolean, array or object", "extension", in.id, "key", spec.Key, "type", spec.Type)
	}
	in.mu.Lock()
	registered := in.settingKeys[spec.Key]
	in.mu.Unlock()
	if registered {
		return errors.New("ExtensionSettingDuplicate", "the extension registered this setting already", "extension", in.id, "key", spec.Key)
	}
	category := spec.Category
	if category == "" {
		category = in.name
	}
	def, _ := settingFromSpec(spec, category)
	err := in.scope.h.holdSetting(in.id, def)
	if err != nil {
		return err
	}
	in.mu.Lock()
	in.settingKeys[spec.Key] = true
	in.mu.Unlock()
	key := spec.Key
	in.track(func() { in.scope.h.releaseSetting(key) })
	return nil
}

// Get implements extension.Settings. It fails with ExtensionSettingUnknown for a key that is not registered.
func (s settingsAPI) Get(key string) (any, error) {
	in := s.inst
	store := in.scope.opts.Settings
	if _, ok := store.Registry().Lookup(key); !ok {
		return nil, errors.New("ExtensionSettingUnknown", "no setting has this key", "extension", in.id, "key", key)
	}
	return settings.Get[any](store, key, ""), nil
}

// OnChange implements extension.Settings. The calls are made on the UI thread and stop with the activation.
func (s settingsAPI) OnChange(fn func()) (cancel func()) {
	in := s.inst
	if in.isDead() || fn == nil {
		return func() {}
	}
	stop := in.scope.opts.Settings.OnChange(func(e settings.ChangeEvent) {
		if e.IsEmpty() {
			return
		}
		in.scope.post(func() { in.scope.runSettingsListener(in, fn) })
	})
	in.track(stop)
	return stop
}

// ---- notifications ----

type notificationsAPI struct{ inst *instance }

// Info implements extension.Notifications.
func (n notificationsAPI) Info(message string, actions ...extension.NotificationAction) {
	n.show(notify.Info, message, actions)
}

// Warn implements extension.Notifications.
func (n notificationsAPI) Warn(message string, actions ...extension.NotificationAction) {
	n.show(notify.Warning, message, actions)
}

// Error implements extension.Notifications.
func (n notificationsAPI) Error(message string, actions ...extension.NotificationAction) {
	n.show(notify.Error, message, actions)
}

// show sends a notification with the name of the extension as its source; it does nothing after the activation ended.
func (n notificationsAPI) show(severity notify.Severity, message string, actions []extension.NotificationAction) {
	in := n.inst
	if in.isDead() {
		return
	}
	var list []notify.Action
	for _, a := range actions {
		a := a
		act := notify.Action{Label: a.Label}
		if a.Run != nil {
			act.Run = func() { in.scope.runNotificationAction(in, a.Label, a.Run) }
		}
		list = append(list, act)
	}
	in.scope.opts.Notify.Notify(notify.Notification{Severity: severity, Source: in.name, Message: message, Actions: list})
}

// ---- status bar ----

type statusBarAPI struct{ inst *instance }

// statusIDPrefix and statusCommandPrefix build the id of an item in the bar and the id of the hidden command that
// calls its OnClick.
const (
	statusIDPrefix      = "extension."
	statusCommandPrefix = "emetos.extension.statusItem."
)

// Add implements extension.StatusBar. It fails with ExtensionStatusItemInvalid for an item with no ID and
// ExtensionStatusItemDuplicate when the extension has an item with the ID.
func (b statusBarAPI) Add(item extension.StatusItem) (extension.StatusHandle, error) {
	in := b.inst
	if in.isDead() {
		return nil, errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	if in.scope.opts.StatusBar == nil {
		return nil, errors.New("ExtensionStatusBarUnavailable", "the scope has no status bar (a window tab of an extension has none)", "extension", in.id)
	}
	if item.ID == "" {
		return nil, errors.New("ExtensionStatusItemInvalid", "a status bar item needs an ID", "extension", in.id)
	}
	in.mu.Lock()
	taken := in.statusIDs[item.ID]
	in.statusIDs[item.ID] = true
	in.mu.Unlock()
	if taken {
		return nil, errors.New("ExtensionStatusItemDuplicate", "the extension has a status bar item with this ID", "extension", in.id, "item", item.ID)
	}
	h := &statusHandle{inst: in, item: item.ID, commandID: statusCommandPrefix + in.id + "." + item.ID}
	alignment := workbench.StatusLeft
	if item.Alignment == extension.StatusRight {
		alignment = workbench.StatusRight
	}
	h.accessor = in.scope.opts.StatusBar.Add(statusIDPrefix+in.id+"."+item.ID, alignment, item.Priority, h.entry(item))
	h.syncClick(item)
	in.track(h.Dispose)
	return h, nil
}

// statusHandle is the extension.StatusHandle of one item.
type statusHandle struct {
	inst      *instance
	item      string
	commandID string
	accessor  *workbench.StatusAccessor

	mu       sync.Mutex
	disposed bool
	remove   func() // takes the hidden command of the click away; nil while there is none
}

// entry is what the bar shows for an item. A click calls OnClick through the hidden command, else it runs the
// command of the item.
func (h *statusHandle) entry(item extension.StatusItem) workbench.StatusEntry {
	name := item.Name
	if name == "" {
		name = h.inst.name
	}
	e := workbench.StatusEntry{Name: name, Text: item.Text, AriaLabel: item.Text, Tooltip: item.Tooltip, Command: item.Command, Args: item.Args}
	if item.OnClick != nil {
		e.Command, e.Args = h.commandID, nil
	}
	return e
}

// syncClick makes the hidden command of the click what the item needs now: one that calls OnClick when the item has it,
// none when it has not.
func (h *statusHandle) syncClick(item extension.StatusItem) {
	reg := h.inst.scope.opts.Commands
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.disposed {
		return
	}
	if h.remove != nil {
		h.remove()
		h.remove = nil
	}
	if item.OnClick == nil {
		return
	}
	click := item.OnClick
	if err := reg.Register(command.Meta{ID: h.commandID}); err != nil {
		h.inst.log.Warn("the click of a status bar item cannot be bound", "item", h.item, "err", err)
		return
	}
	remove, err := reg.SetHandler(h.commandID, func(command.Context, any) error {
		h.inst.scope.runStatusClick(h.inst, h.item, click)
		return nil
	})
	if err != nil {
		reg.Unregister(h.commandID)
		h.inst.log.Warn("the click of a status bar item cannot be bound", "item", h.item, "err", err)
		return
	}
	id := h.commandID
	h.remove = func() { remove(); reg.Unregister(id) }
}

// Update implements extension.StatusHandle.
func (h *statusHandle) Update(item extension.StatusItem) {
	if h.inst.isDead() {
		return
	}
	h.syncClick(item)
	h.accessor.Update(h.entry(item))
}

// SetVisible implements extension.StatusHandle.
func (h *statusHandle) SetVisible(visible bool) {
	if h.inst.isDead() {
		return
	}
	h.accessor.SetVisible(visible)
}

// Dispose implements extension.StatusHandle.
func (h *statusHandle) Dispose() {
	h.accessor.Dispose()
	h.mu.Lock()
	remove := h.remove
	h.remove = nil
	h.disposed = true
	h.mu.Unlock()
	if remove != nil {
		remove()
	}
	h.inst.mu.Lock()
	delete(h.inst.statusIDs, h.item)
	h.inst.mu.Unlock()
}

// ---- documents ----

type documentsAPI struct{ inst *instance }

// Read implements extension.Documents. The window's error is returned as it is.
func (d documentsAPI) Read(ctx context.Context, uri string, opts ...extension.ReadOption) (string, error) {
	in := d.inst
	if in.isDead() {
		return "", errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	var o extension.ReadOptions
	for _, opt := range opts {
		opt(&o)
	}
	if o.Scope != "" && o.Scope != in.scope.ID() {
		return in.scope.readFrom(ctx, in, o.Scope, uri)
	}
	src := in.scope.opts.Documents
	if src == nil {
		return "", errors.New("ExtensionDocumentUnavailable", "the scope has no documents", "extension", in.id, "uri", uri)
	}
	return src.Read(ctx, uri)
}

// Open implements extension.Documents.
func (d documentsAPI) Open() []string {
	src := d.inst.scope.opts.Documents
	if src == nil || d.inst.isDead() {
		return nil
	}
	return src.Open()
}

// DocumentShower is the optional part of a DocumentSource that shows a document in an editor.
type DocumentShower interface {
	// Show opens the file with the URI in the active editor group with the cursor at the line and column (from 1; the
	// column counts UTF-16 units).
	Show(uri string, line, column int, preview bool) error
}

// Show implements extension.Documents. The window's error is returned as it is.
func (d documentsAPI) Show(uri string, line, column int, preview bool) error {
	in := d.inst
	if in.isDead() {
		return errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	shower, ok := in.scope.opts.Documents.(DocumentShower)
	if !ok {
		return errors.New("ExtensionDocumentUnavailable", "the scope cannot show documents", "extension", in.id, "uri", uri)
	}
	return shower.Show(uri, line, column, preview)
}
