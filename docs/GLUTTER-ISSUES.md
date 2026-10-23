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
