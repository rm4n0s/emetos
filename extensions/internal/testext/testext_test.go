package testext

import (
	"context"
	"log/slog"
	"slices"
	"testing"

	"github.com/rm4n0s/emetos/extension"
)

// recorder is an extension.API that remembers what the extension did with it. It implements what testext uses; the rest
// of the interface is the embedded one, which is nil, so a call of a part testext does not use panics.
type recorder struct {
	extension.API
	handlers      map[string]extension.CommandHandler
	settings      []string
	statusUpdates []string
	notes         []string
}

func (r *recorder) Scope() extension.ScopeInfo   { return extension.ScopeInfo{Name: "work"} }
func (r *recorder) Log() *slog.Logger            { return slog.Default() }
func (r *recorder) Post(fn func())               { fn() }
func (r *recorder) Commands() extension.Commands { return recCommands{r} }
func (r *recorder) Keybindings() extension.Keybindings {
	return nil
}
func (r *recorder) Menus() extension.Menus                 { return nil }
func (r *recorder) Settings() extension.Settings           { return recSettings{r} }
func (r *recorder) Notifications() extension.Notifications { return recNotes{r} }
func (r *recorder) StatusBar() extension.StatusBar         { return recBar{r} }
func (r *recorder) Documents() extension.Documents         { return nil }
func (r *recorder) Events() extension.Events               { return nil }

type recCommands struct{ r *recorder }

func (c recCommands) Handle(id string, h extension.CommandHandler) error {
	c.r.handlers[id] = h
	return nil
}
func (c recCommands) Register(extension.CommandSpec, extension.CommandHandler) error { return nil }

type recSettings struct{ r *recorder }

func (s recSettings) Register(spec extension.SettingSpec) error {
	s.r.settings = append(s.r.settings, spec.Key)
	return nil
}
func (s recSettings) Get(string) (any, error)  { return "Howdy", nil }
func (s recSettings) Update(string, any) error { return nil }
func (s recSettings) OnChange(func()) func()   { return func() {} }

type recNotes struct{ r *recorder }

func (n recNotes) Info(m string, _ ...extension.NotificationAction) { n.r.notes = append(n.r.notes, m) }
func (n recNotes) Warn(string, ...extension.NotificationAction)     {}
func (n recNotes) Error(string, ...extension.NotificationAction)    {}

type recBar struct{ r *recorder }

func (b recBar) Add(extension.StatusItem) (extension.StatusHandle, error) {
	return recHandle(b), nil
}

type recHandle struct{ r *recorder }

func (h recHandle) Update(i extension.StatusItem) {
	h.r.statusUpdates = append(h.r.statusUpdates, i.Text)
}
func (recHandle) SetVisible(bool) {}
func (recHandle) Dispose()        {}

func TestTheCommandGreetsWithTheSettingAndCountsInTheStatusBar(t *testing.T) {
	r := &recorder{handlers: map[string]extension.CommandHandler{}}
	if err := New().Activate(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.settings, []string{"emetos.testext.greeting"}) {
		t.Errorf("settings = %v", r.settings)
	}
	h := r.handlers["testext.hello"]
	if h == nil {
		t.Fatal("no handler for the declared command")
	}
	for range 2 {
		if err := h(context.Background(), nil); err != nil {
			t.Fatal(err)
		}
	}
	if !slices.Equal(r.notes, []string{"Howdy from work", "Howdy from work"}) {
		t.Errorf("notes = %v", r.notes)
	}
	if !slices.Equal(r.statusUpdates, []string{"$(smiley) 1", "$(smiley) 2"}) {
		t.Errorf("status = %v", r.statusUpdates)
	}
}
