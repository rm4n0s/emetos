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
// # Events
//
// Extensions never call each other: they publish events and send requests through API.Events. A topic is dotted names
// (markdown.preview.open); an extension publishes and handles the topics under its own id and those its Manifest
// declares (Manifest.Events), and Emetos publishes the core events (TopicDocumentOpened and its siblings) in every
// workspace. The value of an event is copied as JSON, so no live object crosses and a receiver can do what it likes with
// what it got. Events reach the subscribers of the scope on the UI thread, later, in order; the events of fast topics
// (typing) arrive once per frame as one batch. A request is answered by the handler of its topic, never blocks, and calls
// its reply function once, with the reply or an error (EventNoHandler, EventTimeout, EventCanceled, EventHandlerFailed).
//
// # Other window tabs
//
// Events never leave their scope unless the user allowed it (SPEC §10.15 "Event sharing"). An extension that wants the
// events of other window tabs lists the topics in Manifest.Events.CrossTab, subscribes with FromOtherScopes (or asks with
// ToOtherScopes, or reads with FromScope), and the user is asked in its tab, once, whether the events of the other tab
// may reach it. The answer is a link in one direction; the extension is told with TopicSharingRevoked when it goes. Until
// there is a link nothing arrives, and a topic outside CrossTab is never asked about.
//
// # Languages
//
// An extension adds languages by declaring them: Contributes.Languages is a file system with the package.json of a VS
// Code extension at its root (the languages, the grammars and the settings defaults of languages) and the files it names.
// Emetos adds them to the registry of the process while the extension is enabled in some window tab, before any file
// opens, so the extension needs no code to be a language pack; the bundled languages are such extensions. Several tabs
// may have the extension enabled: the languages are added once and taken away when the last tab lets go of them.
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
