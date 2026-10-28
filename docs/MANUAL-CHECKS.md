# Manual checks

Checks only the user can do. Each item has the step it belongs to and the exact command to run. Answer in the chat; the answer is recorded in the step's entry of `docs/IMPLEMENTED.md` and the item is removed from this file.

## 3.6 Text input and IME

Run the editor on a scratch file, isolated from your configuration:

```
printf 'hello\n' > /tmp/x.txt
scripts/isolated.sh -- go run ./cmd/emetos /tmp/x.txt
```

1. Type Greek letters (switch your keyboard layout to Greek). Do the letters appear at the cursor, and do shortcuts such as Ctrl+A (select all) and Ctrl+] (indent) still work on the Greek layout?
2. If you have an IBus or Fcitx5 input method (Chinese pinyin, Japanese, Korean, ...): compose a few characters. Is the composed text underlined at the cursor? Does the candidate window sit next to the cursor, and does it follow the cursor when you move it to another line? Does committing a candidate type it into the file?

Say what you saw, and which input method and compositor you used.

## 3.11 and 10.1 Performance with the GPU

**Measured on 2026-10-28 by the manager, in the user's session** (Radeon 8060S, Vulkan): `scripts/perf.sh --session -n 3 -s 15 --no-go --no-huge`; its two tables are in `docs/PERFORMANCE.md`, "Desktop pass (step 17b.6)". Met: idle focused 0.47% of a core, unfocused 0 frames, typing 2.3 ms, the cold starts with the empty window, memory (+49 MB). Missed: the 10 MB open 410 ms against 300 ms, the restored start 570 ms against 400 ms, the worst frame of the scroll 8.24 ms against 8 ms. **What is left for you:** (1) the 200 MB open (< 3 s) and the Go benchmarks, which that run left out (`scripts/perf.sh --session` as below, without `--no-huge` and `--no-go`; about 2 GB of memory); (2) the decision on the three targets still missed: keep them and fix the code, or change SPEC §14 (the proposals are in `docs/PERFORMANCE.md`, "Proposed decisions for the targets still missed"). The targets were not changed.

The targets of SPEC §14 are decided by the numbers in your desktop session, with its GPU; the headless ones are in `docs/PERFORMANCE.md`. Close other heavy programs, then run it from a terminal in your desktop session (it builds the editor, makes its test files in a temporary folder, opens and closes a few dozen editor windows one after the other and one counter window to take the focus away, and runs a few Go benchmarks; it reads nothing of your configuration, and takes about 10 minutes; it needs about 2 GB of memory for the 200 MB file):

```
scripts/perf.sh --session
```

Paste the two tables it prints (Markdown). If something is over its target and you want to see why, run it again with `scripts/perf.sh --session --no-go --profile ~/perf-profiles` and tell me where the folder is: it keeps a CPU profile of the typing, the scrolling and the idle runs, and the heap profile after them, with the binary that made them.

The numbers that decide, in the first table: the cold start with no folder and with the session restored (target < 400 ms), the 10 MB open (< 300 ms), the 200 MB open (< 3 s), the typing and scrolling frames (build under 4 ms and 8 ms), the idle CPU focused (< 1%) and the frames of the unfocused window (none), and the resident memory (the counter + 50 MB).

## 6.14 The folder dialog of the desktop

The tests answer the file chooser portal with a fake; only a desktop session has the real portal. Run the editor with no folder, isolated from your configuration:

```
scripts/isolated.sh -- go run ./cmd/emetos
```

1. Press "Open Folder" in the Explorer (or Ctrl+K Ctrl+O). Does the dialog of your desktop open, titled "Open Folder", choosing folders and not files?
2. Choose a folder with a few files and subfolders. Does the Explorer show it (its name in the header, the files with their icons) and the window title and status bar change from the "no folder" ones?
3. Open the dialog again and cancel it. Does nothing change?
4. Run "Close Workspace" (Ctrl+K F) from the palette once step 6.17 exists, or from File > Close Folder: does the Explorer go back to "You have not yet opened a folder."?

Say what you saw, and which desktop (and portal backend) you used.

## 6.19 Ctrl+Tab on a real keyboard

The tests send the key presses and the release of Ctrl through Glutter's test dispatcher; only a desktop session has the real keyboard events of Wayland or X11. Run the editor on three files, isolated from your configuration:

```
mkdir -p /tmp/nav && printf 'one\n' > /tmp/nav/a.txt && printf 'two\n' > /tmp/nav/b.txt && printf 'three\n' > /tmp/nav/c.txt
scripts/isolated.sh -- go run ./cmd/emetos /tmp/nav/a.txt /tmp/nav/b.txt /tmp/nav/c.txt
```

1. Click the tabs b.txt, then c.txt. Hold Ctrl and press Tab once: does a list of the three editors appear with b.txt highlighted and no input box? Let go of Ctrl: does b.txt open?
2. Hold Ctrl and press Tab twice, then Shift+Tab once, then let go of Ctrl: does the highlight move down, down, up, and does the editor under it open when Ctrl is released?
3. Hold Ctrl+Shift and press Tab: does the list start at the last editor? Let go of Shift first, then Ctrl: does the editor open only when Ctrl is released?
4. Hold Ctrl, press Tab, then switch to another window with Alt+Tab or the mouse and let go of Ctrl there. Come back: is the list closed or does Enter choose the highlighted editor? Is the editor ever stuck with a list that does nothing?
5. Press Ctrl+Alt+- and Ctrl+Shift+- after jumping to a few lines with Ctrl+G: do they go back and forward through the lines (and your keyboard layout's minus key works)?

Say what you saw, and which compositor and keyboard layout you used.

## 8.5 Full screen and zoom on a real compositor

The tests drive full screen through Glutter's tester windows and a fake controller; zoom is checked by goldens. Run the editor with your configuration isolated:

```
scripts/isolated.sh -- go run ./cmd/emetos
```

1. Press F11. Does the window go full screen on your desktop, and back on the second press? Does View > Appearance > Full Screen show its check mark while it is full screen?
2. Press Ctrl+= twice, then Ctrl+-, then Ctrl+Numpad0. Does everything (text, side bar, menus, tabs) scale together and are clicks still hitting what they show? Does the text stay sharp?
3. Press Ctrl+K Z (zen mode): are the side bar, panel, activity bar, status bar and menu bar hidden and the editor centered? Does Escape twice leave zen mode and bring everything back?

Say what you saw and which desktop you used.

## 9b.4 The system tray and close to tray

The tests use a fake desktop; only your desktop has a real tray. Run a release build of Emetos, isolated from your configuration (it opens a window and puts an icon in the tray; GNOME shows the icon only with the AppIndicator extension, for example "AppIndicator and KStatusNotifierItem Support"):

```
mkdir -p /tmp/emetos-tray-a /tmp/emetos-tray-b && printf 'hello\n' > /tmp/emetos-tray-a/a.txt
scripts/isolated.sh -- go run -tags glutter_release ./cmd/emetos /tmp/emetos-tray-a
```

1. Is there an Emetos icon in the tray (a cat's hairball, a round ball of orange and white hair, on a teal cat head)? Is it readable at the size of your panel? Does the tooltip say "Emetos" and "1 workspace"?
2. Open the tray menu (right click). Does it show the tab `emetos-tray-a`, then Open Folder..., New Workspace Tab, Hide Emetos and Quit? Choose Open Folder... and pick `/tmp/emetos-tray-b`: does the tab appear in the window and in the menu, and does the tooltip say "2 workspaces"?
3. Press the close button of the window. Does the window disappear (no task bar entry) while the icon stays, and does the menu now say Show Emetos? Click the icon: does the window come back, with its tabs, in front? Click it again while the window is in front: does it hide?
4. Hide the window, then choose a tab in the menu: does the window come back with that tab active?
5. Type something in `a.txt` without saving, press the close button, and show the window again: is the unsaved text still there? Then choose Quit in the menu: does the question (or, with hot exit, nothing) behave as Ctrl+Q does, and does Emetos end (no icon, `pgrep -a emetos` shows nothing)?
6. If you can, turn the tray off (or use a desktop without one, such as GNOME without the extension): does the close button then quit instead of hiding the window?

Say what you saw, and which desktop you used.

## 10.2 The release: the icon in your launcher, and using Emetos for your own work

Unlike the other checks, this one is meant to use your real desktop and your real configuration: it installs the program and the launcher entry for you. Run it from a checkout of the repository, on the commit that is to be tagged `v0.1.0`:

```
go install -tags glutter_release ./cmd/emetos
emetos --version
scripts/install-desktop.sh
```

(`go install ...@latest` of the published module is the same program: `go install -tags glutter_release github.com/rm4n0s/emetos/cmd/emetos@latest`, once the tag is pushed. `emetos` must be in your `PATH`, or run `scripts/install-desktop.sh --exec "$(go env GOPATH)/bin/emetos"`. The script writes only `~/.local/share/applications/dev.emetos.Emetos.desktop` and `~/.local/share/icons/hicolor/<N>x<N>/apps/dev.emetos.Emetos.png`; `scripts/install-desktop.sh --uninstall` removes them.)

1. Does `emetos --version` print `v0.1.0` (built from the checkout at the `v0.1.0` tag, or `go install ...@v0.1.0`)? A build from an untagged or modified checkout prints Go's pseudo-version (`v0.0.0-<date>-<revision>+dirty`), and `go run` prints `v0.1.0-dev`.
2. Open your application launcher or menu and search for "Emetos" (log out and in if it does not show at once). Is the icon there (a cat's hairball, a round ball of orange and white hair, on a teal cat head), is it sharp at the launcher's size, and does it fit next to the other icons on a light and on a dark theme? Does choosing it start Emetos?
3. While Emetos runs, does the window in your task bar or dock use the same icon and group under the launcher entry (the entry has `StartupWMClass=dev.emetos.Emetos`)? Right click the launcher entry: is there a "New Workspace Tab" action?
4. In your file manager, "Open With" on a text file and on a folder: is Emetos listed, and does it open the file or the folder as a workspace tab?
5. Overtype mode: in a text file press Insert. Does `OVR` show in the status bar (right side, on a colored background) and the cursor become a block? Do typed characters replace the ones after the cursor, and at the end of a line insert? Does pasting replace too (and insert after `"editor.overtypeOnPaste": false`)? Does clicking `OVR` or pressing Insert again go back to insert mode, in every workspace tab? Does it work with your keyboard layout and input method?
6. Use Emetos for your own work for a while (your real folders, your own settings in `~/.config/emetos/settings.json` and `keybindings.json`). Report every problem: each becomes separate fix work after this step.

Say what you saw, and which desktop and icon theme you used.

## 13.1 The `claude` adapter against the real program

The tests of the adapter use a fake program and never reach the user's own `claude`; this check runs the adapter once against the real one, with your own login. It runs `claude auth status`, starts one session in an empty temporary folder in Plan mode, sends one short message ("Reply with the single word: ok. Do not use any tool.") and ends. That is one small request on your account (a few cents at most on a key, a small part of a subscription's limit). It reads none of the program's files and sets no variable of its own but `CLAUDE_CODE_ENABLE_SDK_FILE_CHECKPOINTING`. It is skipped when `claude` is not installed or not logged in:

```
go test -tags emetos_claude -run TestRealClaude -v -timeout 5m ./extensions/agents/claude
```

1. Does it print `logged in: true` with your login method, and does it end with `result: success`? Paste the lines it logs (`init: capabilities [...]`, `commands: N, models: M`, `assistant: "ok" (streamed: "ok")`, `result: ...`).
2. If it fails, paste the whole output: a failure shows how the real program's stream differs from the documented shapes the adapter was written from (`protocol error:` lines name the line that did not fit).
3. Afterwards `pgrep -a claude` must show only the program your own editor or terminal runs, none started by the test.

Say what you saw, and the version of the program (`claude --version`).

## 15.5 Public keys by QR code: the real camera

The tests read a camera only through fakes (the V4L2 ioctls, the mapping and the poll are checked by the sizes and numbers of the kernel, never against a device). This runs the real layer once on your camera. You need a camera (a laptop's or a USB one) that you may use (your user in the `video` group), and a QR code of a public key on another screen or on paper: for example the code that "Show My Public Key as QR Code" of the "Remote Work..." dialog shows in another Emetos, or any QR code made from the line of an `id_ed25519.pub`. Nothing is written or sent; the test opens the first camera it finds (`EMETOS_CAMERA=/dev/video2` chooses another), shows its name, and waits 60 seconds for the code:

```
go test -tags emetos_camera -run TestRealCamera -v ./extensions/internal/keyqr
```

1. Does it list your camera (`camera /dev/video0: ...`) and not its metadata node?
2. Hold the QR code of a public key in front of the camera. Does it print `read a public key: type ..., fingerprint SHA256:..., comment ...`, and does the fingerprint match the key's (`ssh-keygen -lf <file>.pub`)?
3. What size and rate does it print (`N frames of (W,H) in ...`)? Is the camera's light off after the test ends?

Then, in the editor, open the Remote Work page (`scripts/isolated.sh -- go run ./cmd/emetos`, command "Remote Work: Manage") and press "Scan QR Code...": is the picture of your camera shown, does the dialog turn into the confirmation of the key when you hold the code up, and does the camera's light go out when it closes?

Say what you saw: the camera, the format (MJPEG or YUYV, if you know it), whether a large RSA 4096 code was read, and any error text.

