package testext

import (
	"context"
	"strconv"

	"github.com/rm4n0s/emetos/extension"
)

// ID is the id of the extension.
const ID = "emetos.testext"

// New is the Factory of the extension. Each call makes an extension of its own, as the contract asks.
func New() extension.Extension { return &testExt{} }

// testExt is the extension: a command "testext.hello" that shows a notification, a setting that says what it says, a
// keybinding and a menu item for the command, and a status bar item that counts the greetings.
type testExt struct {
	// greetings counts the greetings of this activation; being state, it also makes every extension a value of its own.
	greetings int
}

// Manifest implements extension.Extension. The command is declared, so it is in the palette before the extension is
// activated, and running it activates the extension.
func (*testExt) Manifest() extension.Manifest {
	return extension.Manifest{
		ID: ID, Name: "Test Extension", Version: "0.0.1",
		Description:      "Exercises the extension contract.",
		ActivationEvents: []string{extension.OnLanguage("plaintext")},
		Contributes: extension.Contributes{
			Commands:    []extension.CommandSpec{{ID: "testext.hello", Title: "Say Hello", Category: "Test Extension"}},
			Menus:       []extension.MenuItem{{Menu: extension.MenuCommandPalette, Command: "testext.hello"}},
			Keybindings: []extension.Keybinding{{Command: "testext.hello", Key: "ctrl+alt+h"}},
		},
	}
}

// Activate implements extension.Extension.
func (e *testExt) Activate(_ context.Context, api extension.API) error {
	if err := api.Settings().Register(extension.SettingSpec{
		Key: "emetos.testext.greeting", Type: "string", Default: "Hello", Description: "What the Say Hello command says.",
	}); err != nil {
		return err
	}
	item, err := api.StatusBar().Add(extension.StatusItem{ID: "greetings", Text: "$(smiley) 0", Tooltip: "Greetings", Alignment: extension.StatusRight})
	if err != nil {
		return err
	}
	return api.Commands().Handle("testext.hello", func(context.Context, any) error {
		greeting, err := extension.SettingValue[string](api.Settings(), "emetos.testext.greeting")
		if err != nil {
			return err
		}
		e.greetings++
		item.Update(extension.StatusItem{ID: "greetings", Text: "$(smiley) " + strconv.Itoa(e.greetings), Tooltip: "Greetings", Alignment: extension.StatusRight})
		api.Notifications().Info(greeting + " from " + api.Scope().Name)
		return nil
	})
}
