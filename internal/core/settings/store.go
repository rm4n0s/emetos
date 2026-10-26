package settings

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/rm4n0s/emetos/internal/core/fileio"
	"github.com/rm4n0s/emetos/internal/core/jsonc"
	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/errors"
)

// settingsFile is the name of the file of both layers.
const settingsFile = "settings.json"

// UserSettingsPath is the settings.json of a profile (SPEC §10.12).
func UserSettingsPath(paths storage.Paths, profile string) (string, error) {
	dir, err := paths.ConfigDir(profile)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, settingsFile), nil
}

// WorkspaceSettingsPath is <folder>/.emetos/settings.json. Emetos reads no other
// folder's settings (SPEC decision D12).
func WorkspaceSettingsPath(folder string) string {
	return filepath.Join(folder, ".emetos", settingsFile)
}

// StoreOptions say where the files of a Store are.
type StoreOptions struct {
	// UserPath is the user's settings.json; empty means the store has no user layer.
	UserPath string
	// WorkspacePath is the workspace's settings.json; empty means no folder is open.
	WorkspacePath string
}

// ProblemKind says what is wrong with a setting in a file.
type ProblemKind int

const (
	// ProblemSyntax is a syntax error of the file; Offset and Length say where.
	ProblemSyntax ProblemKind = iota + 1
	// ProblemNotObject is a file that holds something else than an object.
	ProblemNotObject
	// ProblemConflict is a key ignored because another one made its place a value that is
	// not an object.
	ProblemConflict
	// ProblemUnknownKey is a key that is not registered. It is kept in the file.
	ProblemUnknownKey
	// ProblemInvalidValue is a value the setting does not allow; the default is used.
	ProblemInvalidValue
	// ProblemNotInWorkspace is a setting a workspace cannot set; it is ignored.
	ProblemNotInWorkspace
)

// Problem is something wrong in a settings file, for the UI to show.
type Problem struct {
	Target  Target
	Kind    ProblemKind
	Key     string
	Message string
	// Offset and Length are set for ProblemSyntax.
	Offset, Length int
}

// layer is a loaded settings file.
type layer struct {
	model    *Model
	problems []Problem
}

// Store holds the layers of settings (defaults, user, workspace) and answers for a
// language (SPEC §11.1). It is safe for concurrent use.
type Store struct {
	reg  *Registry
	opts StoreOptions

	writeMu sync.Mutex // one write at a time

	mu           sync.RWMutex
	defaults     *Model
	user         layer
	workspace    layer
	consolidated *Model
	regVersion   int
	listeners    []*func(ChangeEvent)
	// source, when set, gives the text of the documents that are open in place of the files
	// (textsource.go).
	source TextSource
}

// NewStore makes a store with the defaults of the registry and empty files; call Reload to
// read the files.
func NewStore(reg *Registry, opts StoreOptions) *Store {
	s := &Store{reg: reg, opts: opts}
	s.user.model, s.workspace.model = NewModel(nil, nil, nil), NewModel(nil, nil, nil)
	s.rebuild()
	return s
}

// Paths are the files of the user's and the workspace's settings; the workspace's is empty while no folder is open.
func (s *Store) Paths() (user, workspace string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.opts.UserPath, s.opts.WorkspacePath
}

// Registry returns the registry of the store.
func (s *Store) Registry() *Registry { return s.reg }

// rebuild recomputes the defaults and the consolidated model. The caller holds mu for
// writing, or owns the store.
func (s *Store) rebuild() {
	s.regVersion = s.reg.Version()
	s.defaults = s.reg.defaultsModel()
	s.consolidated = s.defaults.Merge(s.user.model, s.workspace.model)
}

// current returns the models, rebuilding them when something was registered.
func (s *Store) current() (defaults, consolidated *Model) {
	s.mu.RLock()
	if s.regVersion == s.reg.Version() {
		defer s.mu.RUnlock()
		return s.defaults, s.consolidated
	}
	s.mu.RUnlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.regVersion != s.reg.Version() {
		s.rebuild()
	}
	return s.defaults, s.consolidated
}

// defaultsModel builds the model of the registered defaults.
func (r *Registry) defaultsModel() *Model {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m := NewModel(nil, nil, nil)
	for _, key := range r.order {
		addToTree(m.contents, key, deepClone(r.settings[key].Default), nil)
		m.keys = append(m.keys, key)
	}
	for _, id := range r.languageOrder {
		values := r.languageDefaults[id]
		m.overrides = append(m.overrides, Override{
			Identifiers: []string{id},
			Keys:        sortedKeys(values),
			Contents:    deepClone(toTree(values, nil)).(map[string]any),
		})
	}
	return m
}

// misuse reports a programming error: a panic in debug builds.
func misuse(message string, kv ...any) {
	if debugBuild {
		panic(errors.New("SettingMisuse", message, kv...))
	}
}

// value returns the effective value of a registered key for a language, falling back to
// the default for a value the setting does not allow.
func (s *Store) value(key, languageID string) (any, bool) {
	setting, ok := s.reg.Lookup(key)
	if !ok {
		misuse("the setting is not registered", "key", key)
		return nil, false
	}
	defaults, consolidated := s.current()
	if v, ok := consolidated.Override(languageID).GetValue(key); ok {
		if setting.validate(v) == "" {
			return v, true
		}
	}
	return defaults.Override(languageID).GetValue(key)
}

// Section returns the value of a dotted section of the consolidated settings, whether or not a setting is registered
// under it, or false when there is none: "go.gopls" gives the object that "go.gopls": {...} and "go.gopls.ui.x": 1
// make together, defaults under the user's and the workspace's values. It is how a language server is given the
// settings of a section it asks for (SPEC §9.12 "As built (step 12.3)"). The value is a copy the caller may keep. An
// empty section is the whole tree.
func (s *Store) Section(section string) (any, bool) {
	_, consolidated := s.current()
	v, ok := consolidated.GetValue(section)
	if !ok {
		return nil, false
	}
	return deepClone(v), true
}

// Get returns the value of a setting for a language ("" for none): a language block of the
// workspace or the user's file wins over the plain settings of both files, and the
// workspace wins over the user, which wins over the defaults. A value that the setting does
// not allow gives the default. T must be able to hold the setting's value (an int for a
// number, a []string for an array of strings); a key that is not registered, or a T that
// cannot hold the value, panics in debug builds and gives the zero value in release builds.
func Get[T any](s *Store, key, languageID string) T {
	var zero T
	v, ok := s.value(key, languageID)
	if !ok {
		return zero
	}
	out, ok := convert[T](v)
	if !ok {
		misuse("the value of the setting cannot be stored in the type asked for", "key", key)
		return zero
	}
	return out
}

// convert stores a setting's value in a T.
func convert[T any](v any) (T, bool) {
	var zero T
	if t, ok := v.(T); ok {
		return t, true
	}
	if f, ok := v.(float64); ok {
		switch any(zero).(type) {
		case int:
			return any(int(f)).(T), true
		case int64:
			return any(int64(f)).(T), true
		case int32:
			return any(int32(f)).(T), true
		case uint:
			return any(uint(f)).(T), true
		case float32:
			return any(float32(f)).(T), true
		}
	}
	data, err := json.Marshal(v)
	if err != nil {
		return zero, false
	}
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		return zero, false
	}
	return out, true
}

// Source says which layer a value comes from.
type Source int

const (
	// SourceDefault is the registry's default.
	SourceDefault Source = iota
	// SourceUser is the user's settings.json.
	SourceUser
	// SourceWorkspace is the workspace's settings.json.
	SourceWorkspace
)

// Inspected is where the value of a setting comes from, for the settings UI.
type Inspected struct {
	// Default, User and Workspace are the values of the layers without language blocks.
	Default, User, Workspace Optional
	// DefaultLanguage, UserLanguage and WorkspaceLanguage are the values of the language
	// blocks of the language asked about.
	DefaultLanguage, UserLanguage, WorkspaceLanguage Optional
	// Effective is the value Get returns, and Source the layer it comes from.
	Effective Optional
	Source    Source
	// WorkspaceIgnored is true when the workspace file sets the setting but its scope does
	// not allow it, so the value does not count.
	WorkspaceIgnored bool
}

// Inspect says what each layer has for a setting.
func (s *Store) Inspect(key, languageID string) Inspected {
	setting, registered := s.reg.Lookup(key)
	defaults, _ := s.current()
	s.mu.RLock()
	user, workspace := s.user.model, s.workspace.model
	s.mu.RUnlock()
	var in Inspected
	d, u, w := defaults.Inspect(key, languageID), user.Inspect(key, languageID), workspace.Inspect(key, languageID)
	in.Default, in.User, in.Workspace = d.Value, u.Value, w.Value
	if languageID != "" {
		in.DefaultLanguage, in.UserLanguage, in.WorkspaceLanguage = d.Override, u.Override, w.Override
	}
	in.WorkspaceIgnored = registered && (w.Value.Ok || w.Override.Ok) && !setting.Scope.inWorkspace()
	if registered {
		in.Effective.Value, in.Effective.Ok = s.value(key, languageID)
	} else {
		_, consolidated := s.current()
		in.Effective = optionalOf(consolidated.Override(languageID).GetValue(key))
	}
	usable := func(o Optional) bool { return o.Ok && (!registered || setting.validate(o.Value) == "") }
	switch {
	case languageID != "" && !in.WorkspaceIgnored && usable(in.WorkspaceLanguage):
		in.Source = SourceWorkspace
	case languageID != "" && usable(in.UserLanguage):
		in.Source = SourceUser
	case languageID != "" && usable(in.DefaultLanguage):
		in.Source = SourceDefault
	case !in.WorkspaceIgnored && usable(in.Workspace):
		in.Source = SourceWorkspace
	case usable(in.User):
		in.Source = SourceUser
	}
	return in
}

// Problems lists what is wrong in the files read last, for the UI.
func (s *Store) Problems() []Problem {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append(slices.Clone(s.user.problems), s.workspace.problems...)
}

// OnChange registers fn to be called, in the goroutine that changed the settings, after
// every change. The returned function removes it.
func (s *Store) OnChange(fn func(ChangeEvent)) (unsubscribe func()) {
	p := &fn
	s.mu.Lock()
	s.listeners = append(s.listeners, p)
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.listeners = slices.DeleteFunc(s.listeners, func(x *func(ChangeEvent)) bool { return x == p })
	}
}

// readLayer reads and parses one file. A file that does not exist is an empty layer.
func (s *Store) readLayer(ctx context.Context, path string, target Target) (layer, error) {
	if path == "" {
		return layer{model: NewModel(nil, nil, nil)}, nil
	}
	text := ""
	if open, ok := s.openText(path); ok {
		text = open
	} else {
		res, err := fileio.Read(ctx, path, fileio.ReadOptions{})
		if err == nil {
			text = res.Text
		} else if appErr, ok := errors.FromError(err); !ok || appErr.Tag != "FileNotFound" {
			return layer{}, err
		}
	}
	opts := ParseOptions{}
	if target == TargetWorkspace {
		opts.Scopes = []Scope{ScopeWindow, ScopeResource, ScopeLanguageOverridable}
	}
	parsed := s.reg.ParseModel(text, opts)
	return layer{model: parsed.Model, problems: s.diagnose(target, parsed)}, nil
}

// diagnose lists what is wrong in a parsed file.
func (s *Store) diagnose(target Target, parsed ParseResult) []Problem {
	var out []Problem
	for _, e := range parsed.Errors {
		out = append(out, Problem{Target: target, Kind: ProblemSyntax, Message: e.Message(), Offset: e.Offset, Length: e.Length})
	}
	if parsed.NotObject {
		out = append(out, Problem{Target: target, Kind: ProblemNotObject, Message: "the file does not hold a JSON object"})
	}
	for _, c := range parsed.Conflicts {
		out = append(out, Problem{Target: target, Kind: ProblemConflict, Message: c})
	}
	raw := parsed.Model.rawConfiguration()
	trees := []map[string]any{raw.contents}
	for _, o := range raw.overrides {
		trees = append(trees, o.Contents)
	}
	unknown := map[string]bool{}
	for _, tree := range trees {
		invalid := map[string]bool{}
		for _, key := range s.reg.flatten(tree) {
			setting, ok := s.reg.Lookup(key)
			if !ok {
				if !unknown[key] {
					unknown[key] = true
					out = append(out, Problem{Target: target, Kind: ProblemUnknownKey, Key: key, Message: "unknown setting"})
				}
				continue
			}
			if target == TargetWorkspace && !setting.Scope.inWorkspace() {
				if !unknown[key] {
					unknown[key] = true
					out = append(out, Problem{Target: target, Kind: ProblemNotInWorkspace, Key: key, Message: "this setting cannot be set in a workspace"})
				}
				continue
			}
			v, _ := valueAt(tree, key)
			if msg := setting.validate(v); msg != "" && !invalid[key] {
				invalid[key] = true
				out = append(out, Problem{Target: target, Kind: ProblemInvalidValue, Key: key, Message: msg})
			}
		}
	}
	return out
}

// Reload reads both files again and tells the listeners what changed. A file that cannot
// be read keeps its old settings and the error is returned after the other one is applied.
// The file watcher calls it when a file changes (SPEC §10.5).
func (s *Store) Reload(ctx context.Context) (ChangeEvent, error) {
	s.mu.RLock()
	userPath, workspacePath := s.opts.UserPath, s.opts.WorkspacePath
	s.mu.RUnlock()
	user, userErr := s.readLayer(ctx, userPath, TargetUser)
	workspace, wsErr := s.readLayer(ctx, workspacePath, TargetWorkspace)

	s.mu.Lock()
	if s.opts.UserPath != userPath || s.opts.WorkspacePath != workspacePath {
		// SetUserPath or SetWorkspacePath moved a layer while the files were read: what was read belongs to the
		// old file, and the call that moved the layer reads the new one
		s.mu.Unlock()
		return ChangeEvent{}, nil
	}
	oldConsolidated := s.consolidated
	oldUserKeys, oldWsKeys := modelSignature(s.user.model), modelSignature(s.workspace.model)
	var ev ChangeEvent
	if userErr == nil {
		s.user = user
	}
	if wsErr == nil {
		s.workspace = workspace
	}
	s.rebuild()
	ev = s.reg.diff(oldConsolidated, s.consolidated)
	if userErr == nil && modelSignature(s.user.model) != oldUserKeys {
		ev.Targets = append(ev.Targets, TargetUser)
	}
	if wsErr == nil && modelSignature(s.workspace.model) != oldWsKeys {
		ev.Targets = append(ev.Targets, TargetWorkspace)
	}
	listeners := slices.Clone(s.listeners)
	s.mu.Unlock()

	if !ev.IsEmpty() || len(ev.Targets) > 0 {
		for _, l := range listeners {
			(*l)(ev)
		}
	}
	if userErr != nil {
		return ev, userErr
	}
	return ev, wsErr
}

// SetWorkspacePath moves the workspace layer to another settings.json, that of the folder a window
// opens (an empty path is no folder), and reads the files again: the listeners hear of the settings
// that change with it. The window calls it when its folder changes (SPEC §10.11); the errors are those
// of Reload. The layer of the old folder is dropped even when the new file cannot be read.
func (s *Store) SetWorkspacePath(ctx context.Context, path string) (ChangeEvent, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.mu.Lock()
	s.opts.WorkspacePath = path
	s.workspace = layer{model: NewModel(nil, nil, nil)}
	s.mu.Unlock()
	return s.Reload(ctx)
}

// SetUserPath moves the user layer to another settings.json, that of another profile (SPEC §10.12), and reads
// the files again: the listeners hear of the settings that change with it (the targets of the event include the
// user's settings when its keys differ), so a window that follows the settings takes the new profile's values in
// place. An empty path leaves the store with no user layer. The errors are those of Reload. The layer of the old
// file is dropped even when the new file cannot be read.
func (s *Store) SetUserPath(ctx context.Context, path string) (ChangeEvent, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.mu.Lock()
	s.opts.UserPath = path
	s.user = layer{model: NewModel(nil, nil, nil)}
	s.mu.Unlock()
	return s.Reload(ctx)
}

// modelSignature is a string that differs when the settings of a model differ.
func modelSignature(m *Model) string {
	data, _ := json.Marshal(m.rawConfiguration().contents)
	return string(data)
}

// Update sets a setting in a file, keeping the comments and the layout of the file, and
// creates the file and its folder when they do not exist. The store reloads and the
// listeners are told. It fails with the tags SettingUnregistered, SettingValueInvalid,
// SettingScopeInvalid, SettingsNoWorkspace and SettingsFileInvalid (the file has syntax
// errors, which an edit would make worse), and with those of core/fileio.
func (s *Store) Update(ctx context.Context, key string, value any, target Target) error {
	return s.write(ctx, key, "", value, false, target)
}

// UpdateForLanguage is Update inside the block of a language, "[go]": {…}.
func (s *Store) UpdateForLanguage(ctx context.Context, key, languageID string, value any, target Target) error {
	return s.write(ctx, key, languageID, value, false, target)
}

// Remove removes a setting from a file, so the layers below show again.
func (s *Store) Remove(ctx context.Context, key string, target Target) error {
	return s.write(ctx, key, "", nil, true, target)
}

// RemoveForLanguage removes a setting from the block of a language.
func (s *Store) RemoveForLanguage(ctx context.Context, key, languageID string, target Target) error {
	return s.write(ctx, key, languageID, nil, true, target)
}

func (s *Store) pathOf(target Target) (string, error) {
	var path string
	s.mu.RLock()
	switch target {
	case TargetUser:
		path = s.opts.UserPath
	case TargetWorkspace:
		path = s.opts.WorkspacePath
	}
	s.mu.RUnlock()
	if path == "" {
		return "", errors.New("SettingsNoWorkspace", "there is no file for the settings of that kind", "target", target.String())
	}
	return path, nil
}

func (s *Store) write(ctx context.Context, key, languageID string, value any, remove bool, target Target) error {
	setting, ok := s.reg.Lookup(key)
	if !ok {
		return errors.New("SettingUnregistered", "the setting is not registered", "key", key)
	}
	path, err := s.pathOf(target)
	if err != nil {
		return err
	}
	if target == TargetWorkspace && !setting.Scope.inWorkspace() {
		return errors.New("SettingScopeInvalid", "a workspace cannot set this setting", "key", key)
	}
	if !remove {
		value, err = normalize(value)
		if err != nil {
			return err
		}
		if msg := setting.validate(value); msg != "" {
			return errors.New("SettingValueInvalid", "the value is not allowed: "+msg, "key", key)
		}
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	// A document that is open for the file takes the edit (textsource.go): the store edits the text
	// the document has, and the source saves the document only when it was not dirty.
	if text, open := s.openText(path); open {
		applied, err := s.editDocument(ctx, path, text, key, languageID, value, remove)
		if applied {
			// the document has the edit, even if saving it failed: the layers follow it
			if _, rerr := s.Reload(ctx); err == nil {
				err = rerr
			}
			return err
		}
		if err != nil {
			return err
		}
	}

	text, encoding, lineEnding, hasBOM := "", fileio.UTF8, fileio.LF, false
	res, err := fileio.Read(ctx, path, fileio.ReadOptions{})
	if err == nil {
		text, encoding, lineEnding, hasBOM = res.Text, res.Encoding, res.LineEnding, res.HasBOM
	} else if appErr, ok := errors.FromError(err); !ok || appErr.Tag != "FileNotFound" {
		return err
	}
	root, errs := jsonc.ParseTree(text, jsonc.ParseOptions{AllowEmptyContent: true})
	if len(errs) > 0 || (root != nil && root.Type != jsonc.ObjectNode) {
		return errors.New("SettingsFileInvalid", "the settings file has errors; fix them first", "path", path)
	}

	updated, _, changed, err := s.editText(text, jsonPath(root, key, languageID), value, remove)
	if err != nil {
		return err
	}
	if changed {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return errors.NewErr("SettingsWriteFailed", err).SetMetadata("path", path)
		}
		if _, err := fileio.Write(ctx, path, updated, fileio.WriteOptions{Encoding: encoding, LineEnding: lineEnding, BOM: hasBOM}); err != nil {
			return err
		}
	}
	_, err = s.Reload(ctx)
	return err
}

// openText is the text of the document that is open for a settings file, when the store has a
// source and one is.
func (s *Store) openText(path string) (string, bool) {
	if src := s.textSource(); src != nil {
		return src.OpenText(path)
	}
	return "", false
}

// editDocument makes the edit of a setting in the text of an open document and hands it to the
// source. It fails with the tag SettingsFileInvalid when the document has syntax errors, as the
// file would.
func (s *Store) editDocument(ctx context.Context, path, text, key, languageID string, value any, remove bool) (applied bool, err error) {
	root, errs := jsonc.ParseTree(text, jsonc.ParseOptions{AllowEmptyContent: true})
	if len(errs) > 0 || (root != nil && root.Type != jsonc.ObjectNode) {
		return false, errors.New("SettingsFileInvalid", "the settings file has errors; fix them first", "path", path)
	}
	_, edits, changed, err := s.editText(text, jsonPath(root, key, languageID), value, remove)
	if err != nil {
		return false, err
	}
	if !changed {
		// nothing to change, as a file that is already as asked is not written
		return true, nil
	}
	return s.textSource().EditText(ctx, path, edits)
}

// jsonPath is where a key goes in a file: the flat key "editor.tabSize" unless the file
// writes that setting as nested objects, and inside the "[go]" block for a language.
func jsonPath(root *jsonc.Node, key, languageID string) jsonc.Path {
	var base jsonc.Path
	scope := root
	if languageID != "" {
		base = jsonc.Path{OverrideKey(languageID)}
		scope = jsonc.FindNodeAtLocation(root, base)
	}
	if scope != nil && jsonc.FindNodeAtLocation(scope, jsonc.Path{key}) == nil {
		segments := strings.Split(key, ".")
		nested := make(jsonc.Path, len(segments))
		for i, seg := range segments {
			nested[i] = seg
		}
		if len(segments) > 1 && jsonc.FindNodeAtLocation(scope, nested) != nil {
			return append(base, nested...)
		}
	}
	return append(base, key)
}

// editText computes the edit for a path: the new text, the edits that make it from the text, and whether
// the text changes.
func (s *Store) editText(text string, path jsonc.Path, value any, remove bool) (string, []jsonc.Edit, bool, error) {
	opts := jsonc.FormattingOptions{
		TabSize:      max(1, Get[int](s, "editor.tabSize", "")),
		InsertSpaces: Get[bool](s, "editor.insertSpaces", ""),
		EOL:          "\n",
	}
	if size, spaces, ok := detectIndentation(text); ok {
		opts.TabSize, opts.InsertSpaces = size, spaces
	}
	var edits []jsonc.Edit
	var err error
	if remove {
		edits, err = jsonc.Remove(text, path, opts)
	} else {
		edits, err = jsonc.SetValue(text, path, value, opts)
	}
	if err != nil {
		return "", nil, false, err
	}
	updated, err := jsonc.ApplyEdits(text, edits)
	if err != nil {
		return "", nil, false, err
	}
	if remove && updated != text && len(path) >= 2 {
		// the last setting of a language block takes the block with it (VS Code's ConfigurationEditing does
		// not leave "[go]": {} behind)
		if block, ok := path[0].(string); ok && IsOverrideKey(block) {
			pruned, err := s.pruneEmptyBlock(updated, jsonc.Path{block}, opts)
			if err != nil {
				return "", nil, false, err
			}
			if pruned != updated {
				updated = pruned
				edits = []jsonc.Edit{diffEdit(text, updated)}
			}
		}
	}
	return updated, edits, updated != text, nil
}

// pruneEmptyBlock removes the object at path from the text when it has no property left.
func (s *Store) pruneEmptyBlock(text string, path jsonc.Path, opts jsonc.FormattingOptions) (string, error) {
	root, errs := jsonc.ParseTree(text, jsonc.ParseOptions{AllowEmptyContent: true})
	if len(errs) > 0 || root == nil {
		return text, nil
	}
	node := jsonc.FindNodeAtLocation(root, path)
	if node == nil || node.Type != jsonc.ObjectNode || len(node.Children) > 0 {
		return text, nil
	}
	edits, err := jsonc.Remove(text, path, opts)
	if err != nil {
		return "", err
	}
	return jsonc.ApplyEdits(text, edits)
}

// diffEdit is the one edit that turns before into after (see jsonc.DiffEdit).
func diffEdit(before, after string) jsonc.Edit { return jsonc.DiffEdit(before, after) }

// Keys returns the dotted keys set in a file, sorted; the settings UI lists them.
func (s *Store) Keys(target Target) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m := s.user.model
	if target == TargetWorkspace {
		m = s.workspace.model
	}
	keys := s.reg.flatten(m.rawConfiguration().contents)
	sort.Strings(keys)
	return keys
}

// DetectIndentation finds how a text is indented from its first indented line (see the indentation a settings
// file keeps when the store edits it): with tabs, or with that many spaces; ok is false when no line is indented
// or the indentation mixes both.
func DetectIndentation(text string) (tabSize int, insertSpaces, ok bool) {
	return detectIndentation(text)
}

// detectIndentation finds how a file is indented from its first indented line: with tabs,
// or with that many spaces. The file's own indentation wins over editor.tabSize and
// editor.insertSpaces, so an edit does not re-indent a file written another way.
func detectIndentation(text string) (tabSize int, insertSpaces, ok bool) {
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == "" || len(trimmed) == len(line) {
			continue
		}
		lead := line[:len(line)-len(trimmed)]
		if strings.Trim(lead, "\t") == "" {
			return 4, false, true
		}
		if strings.Trim(lead, " ") == "" {
			return len(lead), true, true
		}
		return 0, false, false
	}
	return 0, false, false
}
