// Package extension is the contract between Emetos and its extensions (SPEC §10.15, D18).
//
// An extension is a Go package compiled into Emetos: a folder under extensions/ that has a function of type Factory,
// listed in extensions.All. There is no plugin loading and no extension process. This package is the only Emetos
// package an extension may import (internal/archtest checks it), so everything an extension can do is in the API
// interface below and the interfaces it returns.
//
// # Lifetime
//
// Every window tab is a scope. Emetos calls the Factory of an extension to get a fresh Extension for each scope that
// needs it, once one of the ActivationEvents in its Manifest happens in that scope, and calls Activate with the Context
// of the activation and an API bound to the scope. The Context is cancelled when the extension is deactivated: its
// window tab closes, the user disables it, or Emetos quits. Everything the extension started (goroutines, timers,
// processes) must end with it. Everything it contributed through the API (commands, keybindings, menu items,
// settings, status bar items) is removed by Emetos at the same time.
//
// # Failures stay inside the extension
//
// An error returned by Activate, and a panic in any call Emetos makes into the extension (Activate, a command handler,
// a status bar callback, a notification action) are caught at that call. The extension is deactivated in that scope for
// the rest of the session, a notification says so once, and the log gets the error with its route. Other scopes, and
// the editor, are not touched. An error that a command handler returns is not a failure of the extension: it is the
// error of the command, and is shown to the user like the error of any command.
//
// # An example
//
//	package hello
//
//	import (
//		"context"
//
//		"github.com/rm4n0s/emetos/extension"
//	)
//
//	// New is the Factory of the extension; extensions/all.go lists it.
//	func New() extension.Extension { return &hello{} }
//
//	type hello struct{}
//
//	func (*hello) Manifest() extension.Manifest {
//		return extension.Manifest{
//			ID: "hello", Name: "Hello", Version: "0.1.0",
//			Description:      "Greets the user.",
//			ActivationEvents: []string{extension.OnCommand("hello.greet")},
//			Contributes: extension.Contributes{
//				Commands: []extension.CommandSpec{{ID: "hello.greet", Title: "Greet", Category: "Hello"}},
//			},
//		}
//	}
//
//	func (*hello) Activate(ctx context.Context, api extension.API) error {
//		return api.Commands().Handle("hello.greet", func(ctx context.Context, args any) error {
//			api.Notifications().Info("Hello from " + api.Scope().Name)
//			return nil
//		})
//	}
//
// The command is in the palette before the extension is activated; running it activates the extension in the scope
// where it was run, and then runs the handler.
package extension
