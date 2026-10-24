// Package quickinput holds the command palette, quick open and the pickers (SPEC §10.6).
//
// The model files (item.go, picker.go, registry.go, controller.go, listprovider.go, commands.go,
// gotoline.go, help.go, views.go) import no Glutter type: a Picker is the state of one quick pick
// (its value, items, filtering and active item), a Registry maps prefixes to providers, and a
// Controller shows one picker at a time and switches providers as the value changes. They are
// used on the UI thread only and tested without a window. The widget (host.go, rows.go) shows
// the controller's picker.
package quickinput
