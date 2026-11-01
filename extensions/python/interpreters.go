package python

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

// Interpreters is the interpreter of one scope (SPEC §10.34 "As built (step 24b.4)"): the one the user chose for the folder, kept in
// Emetos's state, or the one the rules of ResolveInterpreter give. The language server gets it, and the debugger and the Testing
// provider of the pack (M25, M26) read it with Current and follow it with OnChange, so that all of them run the same Python.
//
// Resolve, Chosen and Current may be called from any goroutine; Choose, Refresh, OnChange, Select and the picker belong to the UI thread.
type Interpreters struct {
	api extension.API
	ctx context.Context
	// environ makes the Env of this machine for the tab, with the settings the caller gives (nil for none).
	environ func(s *langserver.Settings) langserver.Env

	mu   sync.Mutex
	cur  Interpreter
	why  string
	have bool
	// generation numbers the refreshes, so that a slow one does not replace a newer answer.
	generation int
	subs       []*func(Interpreter, bool)
	// work waits for the goroutines that look for interpreters, so that a test can see that none is left.
	work sync.WaitGroup
	// updated is called on the UI thread whenever Current changed or was found again (the pack draws the item).
	updated func()
}

// newInterpreters makes the interpreter choice of the activation. environ makes the Env of the machine for the tab.
func newInterpreters(ctx context.Context, api extension.API, environ func(s *langserver.Settings) langserver.Env) *Interpreters {
	return &Interpreters{api: api, ctx: ctx, environ: environ}
}

// Chosen is the path the user chose for the folder of the tab, kept in the state; "" when none was.
func (s *Interpreters) Chosen() string {
	v, _ := extension.StateValue[string](s.api.State(), StateKey)
	return v
}

// Resolve is the interpreter the tab uses now, with the rule that decided (ResolveInterpreter); the version is not told (Describe does).
func (s *Interpreters) Resolve(env langserver.Env) (Interpreter, string, bool) {
	return ResolveInterpreter(env, s.Chosen())
}

// Current is the interpreter in use as Refresh last found it, with its version and environment name; false when the tab has none.
func (s *Interpreters) Current() (Interpreter, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cur, s.have
}

// OnChange calls fn, on the UI thread, whenever the interpreter in use changes (another path, or none), with the new one and whether there
// is one; it returns the function that stops the calls.
func (s *Interpreters) OnChange(fn func(Interpreter, bool)) (cancel func()) {
	p := &fn
	s.mu.Lock()
	s.subs = append(s.subs, p)
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		for i, q := range s.subs {
			if q == p {
				s.subs = append(s.subs[:i], s.subs[i+1:]...)
				return
			}
		}
	}
}

// Choose makes the program at path (or the environment in the folder at path) the interpreter of this folder, keeps the choice in the
// state, and restarts the language server, which gets the new one. An empty path forgets the choice, and the rules of ResolveInterpreter
// decide again. It fails with PythonInterpreterInvalid when path is no program, and with the error of State().Set. UI thread.
func (s *Interpreters) Choose(path string) error {
	if strings.TrimSpace(path) == "" {
		if err := s.api.State().Set(StateKey, nil); err != nil {
			return err
		}
	} else {
		resolved, err := Normalize(s.environ(nil), path)
		if err != nil {
			return err
		}
		if err := s.api.State().Set(StateKey, resolved); err != nil {
			return err
		}
	}
	s.Refresh()
	if err := s.api.LanguageServers().Restart(ServerID); err != nil && !benign(err) {
		s.api.Log().Warn("the language server cannot be restarted after another interpreter was chosen", "err", err)
	}
	return nil
}

// Refresh finds the interpreter in use again, off the UI thread, and tells the subscribers and the pack when it changed. UI thread.
func (s *Interpreters) Refresh() {
	s.mu.Lock()
	s.generation++
	gen := s.generation
	s.mu.Unlock()
	settings := s.snapshot()
	s.work.Add(1)
	go func() {
		defer s.work.Done()
		ctx, cancel := context.WithTimeout(s.ctx, 20*time.Second)
		defer cancel()
		env := s.environ(settings)
		i, why, ok := s.Resolve(env)
		if ok {
			i = Describe(ctx, env, i)
		}
		s.api.Post(func() { s.found(gen, i, why, ok) })
	}()
}

// snapshot reads the setting that decides the interpreter on the UI thread, where the API allows it, for the goroutine that follows.
func (s *Interpreters) snapshot() *langserver.Settings {
	v, _ := extension.SettingValue[string](s.api.Settings(), DefaultInterpreterSetting)
	return langserver.NewSettings(func(key string) (any, bool) {
		if key == DefaultInterpreterSetting {
			return v, true
		}
		return nil, false
	})
}

// found is the end of a Refresh.
func (s *Interpreters) found(gen int, i Interpreter, why string, ok bool) {
	s.mu.Lock()
	if gen != s.generation {
		s.mu.Unlock()
		return
	}
	changed := ok != s.have || i != s.cur
	s.cur, s.why, s.have = i, why, ok
	subs := append([]*func(Interpreter, bool){}, s.subs...)
	updated := s.updated
	s.mu.Unlock()
	if changed {
		for _, fn := range subs {
			(*fn)(i, ok)
		}
	}
	if updated != nil {
		updated()
	}
}

// wait waits for the lookups in flight to end (the tests use it).
func (s *Interpreters) wait() { s.work.Wait() }

// reason is the rule that decided the interpreter in use, "" when there is none.
func (s *Interpreters) reason() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.why
}

// benign tells whether an error only says that the host has no such UI or the extension ended.
func benign(err error) bool {
	appErr, ok := errors.FromError(err)
	return ok && (strings.HasSuffix(appErr.Tag, "Unavailable") || appErr.Tag == "ExtensionDeactivated")
}

// ---- the picker ----

// pickerItem is a row of "Python: Select Interpreter".
type pickerItem struct {
	item extension.QuickPickItem
	// path is the interpreter the row chooses; empty for the other rows.
	path string
	// run is called for a row that is no interpreter.
	run func()
}

// pickerItems makes the rows: the ways to enter a path or forget the choice, then the interpreters by where they are from, the one in use
// marked.
func (s *Interpreters) pickerItems(env langserver.Env, found []Interpreter, inUse string) []pickerItem {
	items := []pickerItem{{item: extension.QuickPickItem{Label: "$(add) Enter interpreter path...", Description: "a python program, or the folder of an environment"}, run: s.enterPath}}
	if s.Chosen() != "" {
		items = append(items, pickerItem{item: extension.QuickPickItem{Label: "$(discard) Use the default interpreter", Description: "forget the choice for this folder"}, run: func() {
			if err := s.Choose(""); err != nil {
				s.fail("The choice cannot be forgotten", err)
			}
		}})
	}
	var last Kind
	for n, i := range found {
		if n == 0 || i.Kind != last {
			items = append(items, pickerItem{item: extension.QuickPickItem{Label: i.Kind.title(), Separator: true}})
			last = i.Kind
		}
		label := i.Label()
		switch {
		case i.Path == inUse:
			label = "$(check) " + label
		case i.Kind == KindWorkspace:
			label = "$(star-full) " + label
		default:
			label = "$(python) " + label
		}
		items = append(items, pickerItem{item: extension.QuickPickItem{Label: label, Description: shorten(env, i.Path), AlwaysShow: false}, path: i.Path})
	}
	return items
}

// shorten writes a path for the picker: relative to the folder when it is inside, ~ for the home folder.
func shorten(env langserver.Env, path string) string {
	if env.Folder != "" && within(path, env.Folder) {
		if rel, err := filepath.Rel(env.Folder, path); err == nil {
			return rel
		}
	}
	if env.Home != "" && within(path, env.Home) {
		if rel, err := filepath.Rel(env.Home, path); err == nil {
			return filepath.Join("~", rel)
		}
	}
	return path
}

// Select shows the picker "Select Interpreter" over the editor: the interpreters of this machine are found off the UI thread, then listed.
// A client of a hosted workspace is told the interpreter is the host's choice (SPEC §10.18).
func (s *Interpreters) Select() {
	if s.api.Origins().Current() != extension.HostOrigin {
		s.api.Notifications().Info("The Python interpreter is a choice of the host and cannot be changed from here.")
		return
	}
	settings := s.snapshot()
	s.work.Add(1)
	go func() {
		defer s.work.Done()
		ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
		defer cancel()
		env := s.environ(settings)
		found := Discover(env)
		for n := range found {
			found[n] = Describe(ctx, env, found[n])
		}
		s.api.Post(func() { s.showPicker(env, found) })
	}()
}

// showPicker shows the rows.
func (s *Interpreters) showPicker(env langserver.Env, found []Interpreter) {
	inUse := ""
	if cur, ok := s.Current(); ok {
		inUse = cur.Path
	}
	rows := s.pickerItems(env, found, inUse)
	items := make([]extension.QuickPickItem, len(rows))
	for n, r := range rows {
		items[n] = r.item
	}
	placeholder := "Select the Python interpreter"
	if len(found) == 0 {
		placeholder = "No Python interpreter was found: enter the path of one"
	}
	err := s.api.QuickPick().Show(extension.QuickPickOptions{Title: "Select Interpreter", Placeholder: placeholder, Items: items, MatchOnDescription: true}, func(n int) {
		if n < 0 || n >= len(rows) {
			return
		}
		switch r := rows[n]; {
		case r.path != "":
			if err := s.Choose(r.path); err != nil {
				s.fail("The interpreter cannot be chosen", err)
			}
		case r.run != nil:
			r.run()
		}
	})
	if err != nil && !benign(err) {
		s.api.Log().Warn("the list of interpreters cannot be shown", "err", err)
	}
}

// enterPath asks for the path of a program or of an environment.
func (s *Interpreters) enterPath() {
	env := s.environ(nil)
	err := s.api.InputBox().Show(extension.InputBoxOptions{
		Prompt: "Path of a python program, or of an environment's folder", Message: "Saved for this folder. A folder is an environment: its bin/python is used.",
		Validate: func(v string) string { return CheckInterpreter(env, v) },
	}, func(value string, ok bool) {
		if !ok {
			return
		}
		if err := s.Choose(value); err != nil {
			s.fail("The interpreter cannot be chosen", err)
		}
	})
	if err != nil && !benign(err) {
		s.api.Log().Warn("the box for the interpreter cannot be shown", "err", err)
	}
}

// fail tells the user why a choice did not work.
func (s *Interpreters) fail(what string, err error) {
	msg := err.Error()
	if appErr, ok := errors.FromError(err); ok {
		msg = appErr.Message
	}
	s.api.Notifications().Error(what + ": " + msg)
}

// ---- the offer of the folder's own environment ----

// workspaceOffer is the environment of the folder to offer: the first of .venv, venv and env that has a program, with the version its
// pyvenv.cfg gives. ok is false when the folder has none.
func workspaceOffer(env langserver.Env) (Interpreter, bool) {
	for _, i := range Discover(env) {
		if i.Kind == KindWorkspace {
			return Describe(context.Background(), env, i), true
		}
	}
	return Interpreter{}, false
}

// offered remembers which tabs were offered an environment; it is per activation.
type offer struct {
	done bool
}

// maybeOffer asks, once per tab, whether to use the environment the folder has when the user has chosen nothing: Emetos runs the
// programs of the interpreter it uses (the language server runs it to learn its packages), so a folder's own environment is used only when
// the user says so. The answer is kept like any choice. UI thread.
func (s *Interpreters) maybeOffer(o *offer) {
	if o.done || s.Chosen() != "" {
		return
	}
	o.done = true
	env := s.environ(s.snapshot())
	i, ok := workspaceOffer(env)
	if !ok {
		return
	}
	if cur, _, have := ResolveInterpreter(env, ""); have && cur.Path == i.Path {
		return
	}
	name := i.Name
	if name == "" {
		name = shorten(env, i.Prefix())
	}
	s.api.Notifications().Info("This folder has a Python environment, "+name+". Use it for the language server, debugging and tests? Emetos then runs the Python programs inside it.",
		extension.NotificationAction{Label: "Use " + name, Run: func() {
			if err := s.Choose(i.Path); err != nil {
				s.fail("The interpreter cannot be chosen", err)
			}
		}},
		extension.NotificationAction{Label: "Select Another...", Run: s.Select},
	)
}
