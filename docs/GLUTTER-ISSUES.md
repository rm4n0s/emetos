# Glutter issues

What Glutter is missing or does wrong for Emetos, where Emetos works around it, and what the fix in Glutter would be (CLAUDE.md "Glutter"). An entry is removed when its fix is in the Glutter version Emetos uses; the file is deleted when no entry is left.

## 1. Quitting with IBus logs "closing the input method failed: connection closed by user"

**What is wrong.** On X11 (and on Wayland without text-input-v3), Glutter talks to IBus over its private bus. `dbus.ConnectAddress` (`internal/dbus/bus.go`) dials that bus with `godbus.WithContext(ctx)`, and godbus closes a connection as soon as the context it was made with is done. The context is the input method bridge's own (`b.ctx` of `internal/platform/linux/ime/bridge.go`, passed through `IBusBusFromEnvironment`), which the bridge cancels first when it closes. The worker then closes the input method: `ibusContext.Close` calls `InputContext.Destroy` on a connection that godbus has already closed, and the bridge logs the failure. `dbus.Connect` avoids exactly this (`dialWithoutContext` and its comment); `ConnectAddress` does not.

**What it costs.** Nothing but the log: every quit of Emetos on a desktop with IBus (Linux Mint, Ubuntu, Fedora's GNOME on X11) prints `WARN closing the input method failed err.Tag=DBusCallFailed err.Message="dbus: connection closed by user" ... method:org.freedesktop.IBus.InputContext.Destroy`. IBus drops the input contexts of a client that disconnects, so nothing is left behind. A user reported it (2026-11-17).

**Workaround in Emetos.** None: the connection, the context and the log are Glutter's own.

**The fix in Glutter.** Dial in `ConnectAddress` the way `Connect` does: with `dialWithoutContext`, so that ctx bounds the wait for the connection and nothing else.

## 2. Closing an input context of the IBus portal calls a method the portal does not have

**What is wrong.** When IBus has no private bus Glutter can find, it uses the IBus portal on the session bus (`org.freedesktop.portal.IBus`). `ibusContext.Close` calls `org.freedesktop.IBus.InputContext.Destroy` on the portal's context too, and the portal answers that it has no such method. Seen running Emetos with IBus reached through the portal (a private session bus with `ibus-portal`, 2026-11-17).

**What it costs.** The log again: `WARN closing the input method failed err.Tag=DBusCallFailed err.Message="No such method “Destroy”" ... dest:org.freedesktop.portal.IBus` on every quit, and the context lives until the portal sees the connection end.

**Workaround in Emetos.** None.

**The fix in Glutter.** For the portal's contexts (`dest == ibusPortalDest`), destroy the context through the interface the portal's contexts introspect with (IBus declares `Destroy` on `org.freedesktop.IBus.Service`, which its input contexts extend), and check the same for the private bus.

## 3. gluttertest's release of a gesture keeps the buttons of the press

**What is wrong.** `TestGesture.Up` (`gluttertest/gestures.go`) sends the `PointerUpEvent` with the buttons the gesture was started with (`eventOpts` copies `GestureOpts.Buttons`), so a release in a test still says which button is down. On a desktop the event of a release has no button down any more (the X11 and Wayland backends report 0), and Flutter's `TestPointer.up` sends none either.

**What it costs.** Code that reads the button of a release from the event passes its tests and fails on a desktop. Emetos's terminal did: it reported every release of the secondary and the middle button to a program that tracks the mouse as a release of the primary one (`\x1b[<0;…m`), and its tests, which release with `Up`, saw the right button (found on Linux Mint, 2026-11-17).

**Workaround in Emetos.** The terminal remembers the button of the press (`paneView.reported` in `extensions/terminal/pane.go`), and its test sends the release by hand with no button down (`TestTheMenuOpensOverAProgramThatTracksTheMouse` in `extensions/terminal/pane_test.go`).

**The fix in Glutter.** Send the release with no buttons (`Buttons: foundation.Some(0)`), as the platforms and Flutter do; a test that wants another value can still build the event itself.
