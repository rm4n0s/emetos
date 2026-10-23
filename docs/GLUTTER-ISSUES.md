# Glutter issues

Things Glutter is missing or gets wrong, found while building Emetos. Each entry says what is wrong, where Emetos works around it (if it does), and what the fix in Glutter would be. An entry is removed when the fix is in the Glutter version Emetos uses.

## Cold start is dominated by the first draw (3.11)

- **What:** from the start of the process to the first presented frame takes about 0.85 s in a headless compositor with software GL (llvmpipe), against the 400 ms target of SPEC §14. Glutter's own `examples/counter` takes 0.93 s in the same run, so none of it is Emetos's: the build of Emetos's first frame is 0.05 ms, and the whole startup before the first draw is under 30 ms. The log of `GLUTTER_FRAME_STATS=1` puts 0.85 s of it in the startup stage "first draw (renderer, GL context, shaders, first frame)". Before it, the Vulkan renderer is tried and given up (`VulkanSwapchainFailed`: "the swapchain of the window cannot be made or used") on the headless compositor, and then the GLES renderer makes its context and compiles its shaders; with a cold Mesa shader cache the first run takes 1.2 s.
- **Where the workaround is:** none; there is nothing to work around in Emetos. `scripts/perf.sh` keeps the Mesa shader cache between its runs and warms it first, so the numbers are not those of a cache that has just been emptied.
- **What Glutter could provide:** compile the shaders lazily or in parallel with the creation of the window, draw the first frame before the shaders of the rarer paths are ready, and cache the compiled programs itself (its own cache next to the Mesa one); report the time of each part of the first draw so a start can be profiled below the single "first draw" figure.
- **Status:** headless numbers don't decide (SPEC §14); the figure with the GPU is the manual check `scripts/perf.sh --session` (`docs/MANUAL-CHECKS.md`). Step 3.11 went on without a decision from the user, see its entry in `docs/IMPLEMENTED.md`.

## A headless compositor gives its windows no keyboard focus events (3.11)

- **What:** the headless sway (`scripts/headless-wayland.sh`) has no keyboard, so a Glutter window never receives `ViewFocusStateFocused` or `Unfocused`: `WidgetsBindingObserver.DidChangeViewFocus` is never called, however many windows are open. A window is assumed focused until told otherwise. The idle-and-unfocused target (no frames at all, SPEC §14) cannot be measured headless.
- **Where the workaround is:** `scripts/perf.sh` measures that one only with `--session`; the unit tests send the focus events through `gluttertest`.
- **What Glutter could provide:** an initial `ViewFocusEvent` when a window is created (so the app knows whether it has the focus), and, for a seat without a keyboard, a way to follow the compositor's active window (xdg-toplevel state `activated`) as the focus of the window.

## RawTooltip cannot take a hover delay that changes, report its hiding or be driven from outside (6.1)

- **What:** `widgets.RawTooltip` reads `HoverDelay` from its options, so the delay cannot depend on the moment the pointer enters (VS Code shows a hover at once when another hover hid less than 200 ms ago); it has no callback for "hidden" (needed to start that 200 ms); its state exposes `EnsureTooltipVisible` but no `Hide` and no constructor that gives the caller the state, so the keyboard navigation of an action bar cannot show the tooltip of the item that got the focus; and nothing dismisses a tooltip on a key press (Escape) without the caller finding the state.
- **Where the workaround is:** `kit.Hover` (`internal/gui/kit/hover.go`, the `// Glutter:` comment above the type) builds its own `OverlayPortal` with `NewOverlayPortalOverlayChildLayoutBuilder`, the way RawTooltip does, and adds the delay rule, a `HoverController`, a `HoverDelegate` shared by a group of hovers and a hardware keyboard handler that hides the hover.
- **What Glutter could provide:** `RawTooltipOpts.HoverDelayFunc func() time.Duration` (read when the pointer enters), `OnShown` and `OnHidden` callbacks, and a `RawTooltipController` (`Show`, `Hide`, `IsShowing`) that can be passed in the options; plus a `DismissOnKey` option that hides the tooltip on a key down without handling the key.

## A pressed pointer does not keep the cursor of the region it was pressed on (6.2)

- **What:** the mouse cursor is chosen by the `MouseRegion` the pointer is over now. While a sash is dragged, the pointer can outrun it (a pane that cannot shrink any more stops the sash) and the cursor falls back to the one of whatever is under the pointer, though VS Code keeps the resize cursor for the whole drag (`* { cursor: ... !important }`).
- **Where the workaround is:** `SplitView` (`internal/gui/kit/splitview.go`, the `// Glutter:` comment in `content`) puts one `MouseRegion` over the whole split view, always there so that the panes keep their state, and gives it the cursor of the sash while a sash is dragged. It does not cover a pane that sets a cursor of its own (a text editor's I-beam) and it ends at the edge of the split view.
- **What Glutter could provide:** a way for a pointer that is pressed to keep the cursor of the region it was pressed on until it is released (`MouseRegionOpts.HoldCursorWhilePressed`), or an app-wide cursor override that a widget can set and clear.
