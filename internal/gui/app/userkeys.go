package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/fileio"
	"github.com/rm4n0s/emetos/internal/core/jsonc"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
	"github.com/rm4n0s/emetos/internal/core/settings"
)

// The keybindings of the user in the window (SPEC §11.3, step 8.3): keybindings.json is read when the window opens
// and again whenever it changes (the watcher, a document of it that is saved, an edit of the keybindings editor),
// and the resolver of the window takes the new keybindings in place, so the keys, the menus and the status bar
// follow at once.
//
// The file is edited as the settings are (SPEC §11.1 "Settings in an open editor"): when a document of it is open,
// the edit goes into the document, which is saved when it was not dirty before; a dirty document stays dirty and
// the disk is left alone. VS Code refuses to edit a dirty keybindings.json; Emetos edits the document, as it
// does for the settings.
//
// Ported from VS Code: src/vs/workbench/services/keybinding/common/keybindingEditing.ts (resolveAndValidate,
// the model that is edited and saved)

// userKeys is the keybindings.json of the user for a window.
type userKeys struct {
	win  *Window
	path string

	mu        sync.Mutex
	items     []keybinding.Item
	problems  []keybinding.Problem
	model     *keybinding.Model
	listeners []*func()
}

func newUserKeys(win *Window, path string) *userKeys {
	return &userKeys{win: win, path: path}
}

// Problems are what was wrong in the file the last time it was read.
func (k *userKeys) Problems() []keybinding.Problem {
	k.mu.Lock()
	defer k.mu.Unlock()
	return slices.Clone(k.problems)
}

// subscribe calls f, on the UI thread, whenever the keybindings change, and gives the function that stops it.
func (k *userKeys) subscribe(f func()) (stop func()) {
	p := &f
	k.mu.Lock()
	k.listeners = append(k.listeners, p)
	k.mu.Unlock()
	return func() {
		k.mu.Lock()
		defer k.mu.Unlock()
		if i := slices.Index(k.listeners, p); i >= 0 {
			k.listeners = slices.Delete(k.listeners, i, i+1)
		}
	}
}

// text is the text of keybindings.json: the document that shows it, else the file ("" when there is none).
func (k *userKeys) text(ctx context.Context) (string, error) {
	if text, open := k.win.settingsDocs.OpenText(k.path); open {
		return text, nil
	}
	res, err := fileio.Read(ctx, k.path, fileio.ReadOptions{})
	if err != nil {
		if appErr, ok := errors.FromError(err); ok && appErr.Tag == "FileNotFound" {
			return "", nil
		}
		return "", err
	}
	return res.Text, nil
}

// reload reads keybindings.json again and gives the window its keybindings. A file that cannot be read keeps the
// keybindings the window has; an entry that is not a keybinding is skipped and logged. async says that the caller
// is not the UI thread (the watcher): the resolver is replaced there.
func (k *userKeys) reload(ctx context.Context, async bool) {
	text, err := k.text(ctx)
	w := k.win
	if err != nil {
		w.Process.Logger.Warn("reading keybindings.json failed", "err", err)
		return
	}
	items, problems := keybinding.ParseUserFile(text)
	for _, p := range problems {
		w.Process.Logger.Warn("keybindings.json problem", "message", p.Describe())
	}
	resolver := keybinding.NewResolver(w.Process.Keymap.Defaults(), items, nil)
	apply := func() {
		w.Keybindings.Replace(resolver)
		w.Dispatcher.LeaveChordMode()
		k.mu.Lock()
		k.items, k.problems, k.model = items, problems, nil
		listeners := slices.Clone(k.listeners)
		k.mu.Unlock()
		for _, f := range listeners {
			(*f)()
		}
	}
	if async {
		w.onUI(apply)
	} else {
		apply()
	}
}

// onUI runs f on the UI thread: at once when the window has no shell on screen (its callers are on that thread
// or there is none), else through the binding.
func (w *Window) onUI(f func()) {
	if s := w.shell; s != nil && s.Mounted() {
		widgets.WidgetsBindingOf(s.Context()).Post(f)
		return
	}
	f()
}

// KeybindingsModel is the model of the keybindings editor: the rows of the built-in keybindings, the user's and the
// commands of the window. It is made again when the keybindings change.
func (w *Window) KeybindingsModel() *keybinding.Model {
	k := w.userKeys
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.model == nil {
		var commands []keybinding.CommandInfo
		for _, m := range w.Commands.All() {
			if strings.HasPrefix(m.ID, "dev.") {
				continue
			}
			commands = append(commands, keybinding.CommandInfo{ID: m.ID, Title: m.Title, Category: m.Category})
		}
		k.model = keybinding.NewModel(w.Process.Keymap.Defaults(), k.items, commands)
	}
	return k.model
}

// formatOptions are the indentation of the lines an edit adds: that of the file when it has one, else the
// settings of the editor.
func (k *userKeys) formatOptions(text string) jsonc.FormattingOptions {
	w := k.win
	opts := jsonc.FormattingOptions{
		TabSize:      max(1, settings.Get[int](w.Settings, "editor.tabSize", "")),
		InsertSpaces: settings.Get[bool](w.Settings, "editor.insertSpaces", ""),
		EOL:          "\n",
	}
	if size, spaces, ok := settings.DetectIndentation(text); ok {
		opts.TabSize, opts.InsertSpaces = size, spaces
	}
	return opts
}

// Edit changes the text of keybindings.json with op and gives the window the keybindings that result. A document
// that is open takes the edit (and is saved when it was clean); else the file is written, made when it does not
// exist. It fails with the errors of op, and with the tag KeybindingsFileWriteFailed.
func (k *userKeys) Edit(ctx context.Context, op func(text string, e keybinding.Editing) (string, error)) error {
	text, err := k.text(ctx)
	if err != nil {
		return err
	}
	updated, err := op(text, keybinding.Editing{Format: k.formatOptions(text)})
	if err != nil {
		return err
	}
	if updated == text {
		return nil
	}
	if _, open := k.win.settingsDocs.OpenText(k.path); open {
		applied, err := k.win.settingsDocs.EditText(ctx, k.path, []jsonc.Edit{jsonc.DiffEdit(text, updated)})
		if applied {
			// the document has the edit, even if saving it failed: the keybindings follow it
			k.reload(ctx, false)
			return err
		}
		if err != nil {
			return err
		}
	}
	if err := k.write(ctx, updated); err != nil {
		return err
	}
	k.reload(ctx, false)
	return nil
}

// write puts the text in the file, keeping its encoding, line ends and BOM when it exists.
func (k *userKeys) write(ctx context.Context, text string) error {
	opts := fileio.WriteOptions{Encoding: fileio.UTF8, LineEnding: fileio.LF}
	if res, err := fileio.Read(ctx, k.path, fileio.ReadOptions{}); err == nil {
		opts = fileio.WriteOptions{Encoding: res.Encoding, LineEnding: res.LineEnding, BOM: res.HasBOM}
	}
	if err := os.MkdirAll(filepath.Dir(k.path), 0o755); err != nil {
		return errors.NewErr("KeybindingsFileWriteFailed", err).SetMetadata("path", k.path)
	}
	if _, err := fileio.Write(ctx, k.path, text, opts); err != nil {
		return errors.NewErr("KeybindingsFileWriteFailed", err).SetMetadata("path", k.path)
	}
	return nil
}

// startRecording puts the dispatcher of the window in recording mode: every key press goes to the recorder and
// nothing is resolved. It gives the function that ends the mode; a recorder that another one replaced does not
// end the mode of the other.
func (w *Window) startRecording(r keybinding.Recorder) (stop func()) {
	w.Dispatcher.SetRecorder(r)
	w.recorder = r
	return func() {
		if w.recorder == r {
			w.recorder = nil
			w.Dispatcher.SetRecorder(nil)
		}
	}
}

// keyLabelOf is the label of the keybinding of a command ("Ctrl+K Ctrl+S") for the tooltips of the editors, or "".
func (w *Window) keyLabelOf(commandID string) string {
	if item, ok := w.Keybindings.LookupPrimaryKeybinding(commandID, w.Keys, false); ok && item.Keybinding != nil {
		return item.Keybinding.Label()
	}
	return ""
}

// ensure makes keybindings.json when it does not exist, with the comment and the empty array that
// VS Code starts it with, so that an editor can open it. It fails with the tag KeybindingsFileCreateFailed.
func (k *userKeys) ensure(ctx context.Context) error {
	if _, err := os.Stat(k.path); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(k.path), 0o755); err != nil {
		return errors.NewErr("KeybindingsFileCreateFailed", err).SetMetadata("path", k.path)
	}
	if _, err := fileio.Write(ctx, k.path, keybinding.EmptyUserFile, fileio.WriteOptions{Encoding: fileio.UTF8, LineEnding: fileio.LF}); err != nil {
		return errors.NewErr("KeybindingsFileCreateFailed", err).SetMetadata("path", k.path)
	}
	return nil
}

// runCommand runs a command for a button of the editors, as VS Code's buttons do.
func (w *Window) runCommand(id string, args any) error {
	return w.Commands.Run(command.Context{Ctx: w.ctx, Keys: w.Keys, Source: command.SourceAPI}, id, args)
}
