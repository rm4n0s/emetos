package dbconn

import (
	"context"
	"slices"
	"testing"

	"github.com/godbus/dbus/v5"
	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// RequirementKeyring is the id of the requirement of the keyring (SPEC §10.36, step 34.3).
const RequirementKeyring = "keyring"

// KeyringRequirement is the requirement of the desktop's keyring (the Secret Service) for an extension that keeps secrets in it: the
// Databases extension and the HTTP Client. purpose says what the extension keeps there. The check only asks the session bus whether a
// Secret Service is there (Available): no session is opened, nothing is read, unlocked or prompted for.
func KeyringRequirement(purpose string, s *SecretService) extension.Requirement {
	return extension.Requirement{
		ID: RequirementKeyring, Name: "Secret Service keyring", Kind: extension.RequirementService, Optional: true,
		Purpose: purpose + " (GNOME Keyring, KWallet or KeePassXC on the session bus)",
		Install: extension.Install{Docs: "https://specifications.freedesktop.org/secret-service/latest/", Commands: []extension.InstallCommand{
			{Family: extension.FamilyFedora, Command: "sudo dnf install gnome-keyring"},
			{Family: extension.FamilyDebian, Command: "sudo apt install gnome-keyring"},
			{Family: extension.FamilyArch, Command: "sudo pacman -S gnome-keyring"},
			{Family: extension.FamilySuse, Command: "sudo zypper install gnome-keyring"},
		}},
		Check: func(ctx context.Context, _ extension.RequirementEnv) extension.RequirementResult {
			return s.Available(ctx)
		},
	}
}

// Available tells whether a Secret Service is on the bus: its name is owned now, or the bus can start it. It opens no session with the
// service, so nothing is unlocked and no prompt is shown. Under `go test` without a Dial it does not reach the user's bus and says so.
func (s *SecretService) Available(ctx context.Context) extension.RequirementResult {
	var conn *dbus.Conn
	var err error
	switch {
	case s.Dial != nil:
		conn, err = s.Dial(ctx)
	case testing.Testing():
		return extension.RequirementResult{State: extension.RequirementUnknown, Detail: "a test does not ask the session bus of the user"}
	default:
		conn, err = dbus.ConnectSessionBus(dbus.WithContext(ctx))
	}
	if err != nil {
		return extension.RequirementResult{State: extension.RequirementMissing, Detail: "the session bus cannot be reached: " + errorMessage(err)}
	}
	defer func() { _ = conn.Close() }()
	bus := conn.BusObject()
	var owned bool
	if err := bus.CallWithContext(ctx, "org.freedesktop.DBus.NameHasOwner", 0, ssBusName).Store(&owned); err != nil {
		return extension.RequirementResult{State: extension.RequirementUnknown, Detail: "the session bus did not answer: " + errorMessage(err)}
	}
	if owned {
		return extension.RequirementResult{State: extension.RequirementFound, Path: ssBusName, Detail: "a Secret Service runs on the session bus"}
	}
	var activatable []string
	if err := bus.CallWithContext(ctx, "org.freedesktop.DBus.ListActivatableNames", 0).Store(&activatable); err == nil && slices.Contains(activatable, ssBusName) {
		return extension.RequirementResult{State: extension.RequirementFound, Path: ssBusName, Detail: "the session bus starts a Secret Service when it is asked"}
	}
	return extension.RequirementResult{State: extension.RequirementMissing, Detail: "no Secret Service is on the session bus, so secrets cannot be saved: they are kept in memory until Emetos quits"}
}

// errorMessage is the message of an error of this package's kind, else its text.
func errorMessage(err error) string {
	if appErr, ok := errors.FromError(err); ok && appErr.Message != "" {
		return appErr.Message
	}
	return err.Error()
}
