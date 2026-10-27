# Glutter issues

Things Glutter is missing or gets wrong, found while building Emetos. Each entry says what is wrong, where Emetos works around it (if it does), and what the fix in Glutter would be. An entry is removed when the fix is in the Glutter version Emetos uses.

## A reverse list does not keep what the user reads in place when items are added at its start (16.3)

- **What:** the Events panel of the Relay tab (SPEC §10.19) is a live log whose newest row is at the bottom and which follows the newest row while the user is at the end. A `widgets.ListView` with `Reverse: true` does that (offset 0 is the newest row), but its offset is measured from item 0, so when rows are added at item 0 and the user has scrolled up to read an older row, that row moves away by the height of the new rows. Glutter 1.9.1 (checked again: nothing in 1.9.x touches it) has nothing to keep the content in place (an anchor item, or a "maintain scroll offset" option; `ScrollView.Anchor` is Flutter's anchor of the zero offset, not that); `SliverList` has no `maintainScrollOffset` either.
- **Where the workaround is:** `extensions/relay/clienttab.go` (`keepPlace`, with the `// Glutter:` comment): after the rows are added it moves the offset with `ScrollController.JumpTo` by the number of added rows times the height of a row (every row that arrives is collapsed, and the extent of each row is known, so the sum is exact), and only when the offset is not 0 (the user is not at the end).
- **What Glutter could provide:** a `ListView`/`SliverList` option that keeps the first visible item in place when items are inserted before it (the scroll offset changes by the extent of what was inserted), for logs, chats and feeds.

## An `offscreen.View` has no clipboard of its own (Glutter 1.9.1)

- **What:** the clipboard calls of an `offscreen.View` go to the clipboard of its parent binding (`viewHost.SetClipboardText` and the others call `parent.PlatformDispatcher()`), which is the application's, and `WidgetsBinding.Clipboard()` has no way to be replaced. A view that another machine types in (Remote Work, SPEC §10.18) must not read the clipboard or the primary selection of the desktop of the host or replace them, and its client must be told of the text its user copies and be able to give it the text to paste (`HeadlessView.OnCopy`, `SetClipboard`). Glutter's `EditableText` (every input box) reaches the clipboard through `WidgetsBindingOf(ctx).Clipboard()` and its own state methods, so it cannot be redirected with actions.
- **Where the workaround is:** `internal/gui/app/headless_clipboard.go` (`viewClipboard`, `clipboardHost`, `newClipboardParent`, with the `// Glutter:` comment): the parent of the view is not the application's binding but a binding of its own that never draws, with the same UI thread, error reporter, debug flags, assets and text engine, whose platform host is the view's clipboard. Every clipboard path of the view is then the view's, with no widget changed (the editors, the input boxes, the primary selection, the paste check); the tests assert that the host's clipboard and primary selection are neither read nor written.
- **What Glutter could provide:** an option of `offscreen.ViewOpts` for a `ui.ClipboardHost`/`ui.RichClipboardHost` that the view's dispatcher uses in place of the parent's; `clipboardParent` is then not needed.

## An `offscreen.View` starts with a default platform configuration and a blank window (Glutter 1.9.1)

- **What:** the view's platform configuration (fonts, locales, text scale, brightness, caret blink time) is the default one, not the application's (Glutter's own step 29.7 lists it as a follow-up), and its `ui.ViewConfiguration` has a blank `Window` (no decoration, not activated, not maximized), which an app that draws its own title bar and its inactive state reads. `ViewOpts` cannot set either, and `View.SetSize` writes the view's own copy of the configuration with a blank `Window` again, so a window set by `UpdateViewConfiguration` is lost at the next resize. The tree is mounted inside `NewView`, so a state that reads the configuration in `InitState` sees the default one unless something sets it first.
- **Where the workaround is:** `internal/gui/app/headless.go` (`viewSetup`, `configure` and `applyWindow`, with the `// Glutter:` comment): the root of the view is a widget whose `InitState` (it starts before the shell's) calls `UpdatePlatformConfiguration` with the application's values and `UpdateViewConfiguration` with the window, and `Resize` calls `applyWindow` after `SetSize`.
- **What Glutter could provide:** `ViewOpts` with the platform configuration and the `ui.WindowInfo` of the view (or a copy of the parent's configuration, following its changes), and `SetSize` that keeps the rest of the view's configuration.

## `testing` is linked into every Glutter program (Glutter 1.9.1)

- **What:** `internal/platform/linux/dl/layout.go` is not a test file and imports `testing` (for `AssertLayout`), so every program that links the Linux platform, the editor included, links the `testing` package. `go list -deps ./cmd/emetos | grep -E '^testing$'` prints it. It registers no flags (Go does that in a test binary only), so it costs size and nothing else.
- **Where the workaround is:** none.
- **What Glutter could provide:** `AssertLayout` in a `_test.go` file or in a test helper package.
