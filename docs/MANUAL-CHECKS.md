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
scripts/isolated.sh -- go run ./cmd/emetos /tmp/emetos-tray-a
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


## 19.1 The real keyring (Secret Service)

The tests of the keyring run against a fake Secret Service on a private D-Bus that the test starts; they never touch your session bus. This runs the real client once against **your** keyring (GNOME Keyring, KWallet's Secret Service, KeePassXC...). You need a desktop session with a Secret Service on the session bus. The test stores a secret under the label "Emetos: manual check" with the attributes `application=emetos`, `kind=database-connection` and a random `connection` id (`emetos-manual-check-...`), reads it back, replaces it, deletes it and checks that it is gone. It does nothing else and lists nothing else. If your keyring is locked, it asks you to unlock it (a prompt of the keyring, not of Emetos):

```
go test -tags emetos_keyring ./extensions/internal/dbconn -run TestRealKeyring -v -count=1
```

1. Does it pass (`--- PASS: TestRealKeyring`), and does a prompt of the keyring appear first when the keyring is locked? Which keyring is it (GNOME Keyring, KWallet, KeePassXC)?
2. Afterwards open your keyring manager (Seahorse or KWalletManager): is there no "Emetos: manual check" item left?
3. In the editor (`scripts/isolated.sh -- go run ./cmd/emetos`) run "Databases: New Connection", fill the form of any engine that is listed (the engines come with the next steps; until one is, the form only shows when the command is given an engine), type a password, keep "Save the password in the keyring" on and Save. Is an item "Emetos: <name>" in your keyring, and does `grep -r <the password> ~/.config/emetos ~/.local/state/emetos` (with the isolated folders of the script) find nothing?

Say what you saw: the keyring and any error text.

## 24.23 Git with your own keys, agent, credential helper and GPU

The tests of Git use throw-away repositories with their own `HOME`, a local HTTP server and a fake `ssh`; they never read your `~/.ssh`, your agent, your credential helper or your `~/.gitconfig`. These checks do, on purpose, with a remote of your choice that you may push to (make an empty one for this, not a repository you care about). `scripts/isolated.sh` gives the editor a `HOME` of its own, which would hide exactly what these checks are about, so run the editor as it is, with your session and your Emetos configuration (the checks touch only Git):

```
git clone <your remote> /tmp/emetos-git-check
go run ./cmd/emetos /tmp/emetos-git-check
```

1. **ssh with a passphrase-protected key and your agent.** Use an `ssh://` or `git@host:` remote whose key has a passphrase and is **not** loaded in the agent. Make a commit and run Git: Push (palette). Does a box open over the editor titled `Enter passphrase for key '...'` with "SSH Key: <file>" under it, with dots when you type, and does the push go through after Enter? Then load the key (`ssh-add`) and push again: is there no box, because the agent answers? Press Escape in a box once: does the push fail with git's "Permission denied (publickey)" and no crash?
2. **A host you have not connected to.** Remove the host from `~/.ssh/known_hosts` (a copy first) and push: does a list open asking "Are you sure you want to continue connecting?" with the fingerprint, and do Yes and No do what they say?
3. **HTTPS and your credential helper.** Use an `https://` remote that needs your login and has a credential helper (`git config credential.helper`: libsecret, cache, store, manager...). Run Git: Fetch. If the helper has the credential, does the fetch finish with no box? Clear the stored credential (`git credential reject`) and fetch again: does a "Username for" box and then a "Password for" box open, and does the helper store what you typed afterwards (as git does in a terminal)? Is the password never shown in the Git output channel or a notification?
4. **The same in a terminal.** Open the integrated terminal (Ctrl+`) in that folder and type `git push` (HTTPS remote, helper cleared). Does the box open in the window, not in the terminal, and does the push finish after you answer? Turn `git.terminalAuthentication` off, open a new terminal and push again: does git ask in the terminal itself? Hover the tab of the first terminal: does it say "The following extensions have contributed to this terminal's environment: Git"?
5. **Open in External Terminal.** Set `terminal.sourceControlRepositoriesKind` to `both` and `terminal.external.linuxExec` to your terminal program (or leave it empty), right-click the repository in the Source Control view (make it show with `scm.alwaysShowRepositories`): do "Open in Integrated Terminal" and "Open in External Terminal" both open a terminal in the repository's folder, the second as a window of your desktop?
6. **The milestone on the real GPU.** Run the screens of the milestone with your GPU, in a nested window you can watch:
   ```
   scripts/realinput.sh --desktop --shots /tmp/emetos-git-shots -- ./cmd/emetos -run 'TestRealInputGit|TestRealInputTheWholeOfGit|TestRealInputSCM|TestRealInputMultiDiff|TestRealInputThePeek' -v
   ```
   Open the pictures in `/tmp/emetos-git-shots`: do the Source Control view, the Graph, the Timeline, the quick diff stripe and the peek, the blame text and the password box look as they do in the screenshots of `docs/implemented-proof-images/` (24.x), with the colors, the spacing and the fonts of your desktop?

Say what you saw: the ssh agent and key type, the credential helper, the desktop and the GPU, and any error text.

## 24b.10 The language servers on your desktop and the real GPU

The tests of M24b run each server headless, with a temporary home folder, on a project of `testdata/`. What they cannot show is your desktop: the real GPU, your compositor, your fonts, and a hover that arrives after the window has sat idle. These checks run each server in an editor window on your desktop. `scripts/langservers-desktop.sh` copies the project of one language into a temporary folder, writes a `settings.json` that points the server's path setting at the tool, and starts Emetos through `scripts/isolated.sh` (a `HOME` of its own: your configuration, state and cache are not touched; the NVIDIA card is kept out by `scripts/gpu-env.sh`). The servers are the ones of `/var/tmp/emetos-tools` (`scripts/fetch-tools.sh`, pinned in `scripts/tool-versions.txt`; the script downloads only into that folder, and csharp-ls is installed with `dotnet tool` from a verified file) and your system's `clangd`, Java 21 or newer and .NET SDK.

Get the tools once (skip the ones you do not want to check):

```
scripts/fetch-tools.sh rust-analyzer python-tools pyright typescript-language-server typescript typescript5 jdtls csharp-ls
```

Then, for each language, run its line, wait for the status bar item of the server to show the check mark (rust-analyzer, jdtls and csharp-ls take from a few seconds to about a minute on a first start), and do the four things below the list. Close the window before you start the next one.

```
scripts/langservers-desktop.sh rust
scripts/langservers-desktop.sh cpp
scripts/langservers-desktop.sh python
scripts/langservers-desktop.sh typescript
scripts/langservers-desktop.sh java
scripts/langservers-desktop.sh csharp
```

1. **Problems.** Open the Problems panel (Ctrl+Shift+M). Does it list the error of the project (Rust: "expected i32, found &'static str", and "mismatched types" a moment later from `cargo check`; C++: "undeclared identifier 'missing'"; Python: `"missing_name" is not defined`; TypeScript: "not assignable"; Java: "Type mismatch"; C#: "Cannot implicitly convert"), and does a click on it go to the line, with a red squiggle under it?
2. **Hover.** Rest the pointer on a name (a function, a type, a variable) and do not touch anything else. Does a box with the signature or type appear after about half a second, and go away when you move off?
3. **Completion.** Put the cursor at the end of a line, type a name and a dot (`names.`, `r.`, `Point::`) or Ctrl+Space. Does the list open with the members of the type, with their icons, and does Enter insert one?
4. **Go to definition.** F12 (or Ctrl+click) on a name that is defined in another file of the project (`Greeter`, `add`, `geometry`'s `Point`, `makeRectangle`). Does the other file open at the definition? Alt+Left goes back.

**The hover after the window has been idle** (the finding of the dropped F# work that the display test `TestHoverAnsweredAfterTheWindowWentIdleIsDrawnWithoutInput` found fine in the headless compositor, and only your desktop can confirm). With one of the slower servers (`java` or `csharp`; the first hover after the load takes a second or two): put the pointer on a name, then take your hand off the mouse and keyboard and do not move or press anything. Is the hover box drawn when the server answers, without you moving the mouse or pressing a key? Do the same after leaving the window alone for a minute (put the pointer on a different name, then do nothing). If the box only appears when you move the mouse, say so: that is a frame that was never asked for, which Glutter's `Post` should have asked.

Say what you saw: the desktop and compositor, the GPU, the languages you checked, which of the four things failed, and the hover idle result.

## 25.12 Debugging on your desktop and the real GPU

The tests of M25 debug with the fake adapter and, under `emetos_dlv`, `emetos_debugpy`, `emetos_gdb`, `emetos_lldbdap` and `emetos_jsdebug`, with the real tools, headless (software rendering, a compositor of its own). What they cannot show is your desktop: the real GPU, your compositor, your fonts, the window coming to the front on a stop, and how stepping feels. Both checks use the pinned `dlv` of `/var/tmp/emetos-tools/bin` (`scripts/fetch-tools.sh dlv` if it is not there) and keep your configuration, state and cache out of it; the NVIDIA card is kept out by `scripts/gpu-env.sh`.

**1. The whole pass, watched.** The end-to-end display test of this step (open the Go fixture, F9, F5, the hover, a watch, F10, the Debug Console, Shift+F5) runs in a nested sway window on your desktop, with the real GPU. Keep the window visible while it runs (a hidden one gets no frames):

```
EMETOS_REALINPUT_RACE=0 scripts/realinput.sh --desktop --shots /tmp/emetos-25.12 -- -tags emetos_display ./cmd/emetos -run TestRealInputTheWholeOfDebugging -v -count=1
```

Does it pass, and do the pictures in `/tmp/emetos-25.12/` (`debug-desktop-*.png`) show what the names say: the red dot in the margin, the highlighted stopped line, the hover with a value, the watch with its value, the Debug Console with the result, and the status bar orange while the session runs and normal after Shift+F5?

**2. By hand, with your own keys.** Copy the Go fixture into a folder of your own and open it in an editor that uses the settings of the proofs (they point the Go extension at the pinned dlv):

```
rm -rf /tmp/orders && cp -r testdata/proof/25.7/project /tmp/orders
scripts/isolated.sh --config testdata/proof/25.7/config -- go run ./cmd/emetos /tmp/orders /tmp/orders/main.go
```

1. Click line 12 and press F9: does a red dot appear in the margin? Press F5: does the program build and stop on line 12 (the line highlighted, the window brought to the front if it was behind, the status bar orange)?
2. Rest the pointer on `rate` and on `order`: does the hover show the value after about half a second, and a tree for `order` that you can open?
3. Press F10 three times, F11 into a call, Shift+F11 out: does each key move the highlighted line at once (it should feel instant, well under a tenth of a second)? Does the Variables view mark the values that changed in the last step?
4. In the Variables view, right-click `order` and choose Add to Watch, then press F10: does the Watch view show the expression with its value, and update?
5. Press Ctrl+Shift+Y, type `order.Total * 2` and Enter: does the Debug Console show the result? Does Up bring the line back?
6. Press Shift+F5: does the session end, the status bar go back to its normal color, and the stopped line disappear?
7. Try it with the editor maximized on a high-DPI screen or at a zoom of 150% (Ctrl+=): are the glyph dots, the stopped line, the inline values and the toolbar sharp and aligned?

Say what you saw: the desktop and compositor, the GPU, and which of the steps failed or felt slow.

## 26.10 Testing on your desktop and the real GPU

The tests of M26 run the five providers with fake runners and, one at a time, with the real tools (`go` in the normal run; `emetos_pytest`, `emetos_cargo`, `emetos_node`, `emetos_ctest`), headless (software rendering, a compositor of its own). What they cannot show is your desktop: the real GPU, your compositor, your fonts, the glyphs and the peek at your scale, and how a run streaming 5,000 results feels. Both checks keep your configuration, state and cache out of the editor (`scripts/isolated.sh`, the test harness) and the NVIDIA card out of it (`scripts/gpu-env.sh`).

**1. The whole pass, watched.** The end-to-end display test of this step (open the Go fixture, the Test Explorer, Run All Tests, the glyph of the failed test, the peek, Rerun Failed Tests, Run with Coverage, the coverage of `cart.go`, Escape) runs in a nested sway window on your desktop, with the real GPU. Keep the window visible while it runs (a hidden one gets no frames):

```
EMETOS_REALINPUT_RACE=0 scripts/realinput.sh --desktop --shots /tmp/emetos-26.10 -- -tags emetos_display ./cmd/emetos -run TestRealInputTheWholeOfTesting -v -count=1
```

Does it pass, and do the pictures in `/tmp/emetos-26.10/` (`testing-desktop-*.png`) show what the names say: the green run icons in the margin before the run, the red icon and the message after the line of the failure after it, the peek with `175` and `170` side by side, the Test Coverage view with `86.67%` and `cart.go` with green and red lines beside the test file?

**2. By hand, with your own keys.** Copy the Go fixture into a folder of your own and open it with the settings of the proofs:

```
rm -rf /tmp/shop && cp -r testdata/proof/26.5/project /tmp/shop
scripts/isolated.sh --config testdata/proof/26.5/config -- go run ./cmd/emetos /tmp/shop /tmp/shop/cart_test.go
```

1. Open Testing (the flask in the activity bar): does the Test Explorer list `TestTotal`, `TestDiscount` and `TestLabel` within a second or two? Are the green run icons in the margin on the lines of the three tests?
2. Press Ctrl+; then A: do the icons turn to a green check or a red cross as the tests finish, and does the summary read `5/6 tests passed (83.3%)`? Does the failed line show its message after the text?
3. Click the red icon of `TestDiscount` (line 24): does it run only that test? Press Ctrl+; then Ctrl+M with the cursor on line 29: does the peek show the message, and `175` against `170`? Press Alt+F8, then Escape: does it close?
4. Press Ctrl+; then E (rerun the failed tests), then Ctrl+; Ctrl+Shift+A (run with coverage): does the Test Coverage view list `cart.go` at 86.67%, and do the lines of `cart.go` show green, red and the counts (open it from the view)? Are the bars sharp, and the colors of the lines readable on your screen?
5. Press Ctrl+; Ctrl+O (show the output): does Test Results show the output of the last run with its colors?
6. Zoom to 150% (Ctrl+=): are the icons, the peek and the summary sharp and aligned?
7. A project of your own, if you have one: does Testing find its tests (Python with pytest or unittest, Rust with `cargo test`, Jest, Vitest or Node's runner, a CMake project that is built), and does Run, and Coverage where the README says it exists, work on it? Say which you tried.

Say what you saw: the desktop and compositor, the GPU, which of the steps failed or felt slow.

## 27.9 The HTTP client: the real keyring, an OAuth 2.0 login and your desktop

The tests of M27 send every request to a server of the test on 127.0.0.1 (plain or TLS) or to a fake authorization server, keep secrets and tokens in a fake keyring, and run headless with a software renderer. What they cannot show is your keyring, a real identity provider, your browser, and your desktop with the real GPU. All three checks keep your own configuration, state and cache out of the editor (`scripts/isolated.sh`, the test harness) and the NVIDIA card out of it (`scripts/gpu-env.sh`). The first and the third need nothing from outside; the second sends requests to the provider you choose, which is the point of it.

**1. The real keyring: a secret variable and an OAuth token.** You need a desktop session with a Secret Service on the session bus (GNOME Keyring, KWallet's Secret Service, KeePassXC). The test stores a secret variable and an OAuth 2.0 token with the keys and labels the HTTP client uses ("Emetos HTTP Client: ...") under a folder name of its own (`/emetos-manual-check-...`), reads them back, replaces and deletes them, and checks that they are gone. It touches no other item. If your keyring is locked it asks you to unlock it.

```
go test -tags emetos_keyring ./extensions/httpclient -run TestRealKeyring -v -count=1
```

1. Does it pass (`--- PASS: TestRealKeyringHoldsASecretVariableAndAnOAuthToken`), and does a prompt of the keyring appear first when it is locked? Which keyring is it?
2. Afterwards open your keyring manager (Seahorse or KWalletManager): is no item "Emetos HTTP Client: ..." left from the check?
3. In the editor (`scripts/isolated.sh -- go run ./cmd/emetos /tmp/http-check`, a new empty folder), make an environment with a variable of the kind Secret (the lock in the environment editor), type a value, Save. Is an item "Emetos HTTP Client: ..." in your keyring now, and does `grep -r <the value> /tmp/http-check` and the isolated state and config folders find nothing? Does Delete Environment take the item away?

**2. An OAuth 2.0 login with a provider of your choice.** The grant is the authorization code with PKCE and a loopback redirect (`http://127.0.0.1:<random port>/callback`, RFC 8252), so the provider must let a client of the kind "desktop" or "native" use a loopback address with any port. GitHub (an OAuth App), Google (a "Desktop app" client), Microsoft Entra ID (a "Mobile and desktop" platform with `http://127.0.0.1`), Auth0 (a Native application), Keycloak and Okta (a native app with `http://127.0.0.1:*/callback`) all do. Register an application there, note its client ID (and its secret when it has one), and pick a URL of the provider that answers "who am I" with a bearer token (GitHub: `https://api.github.com/user`; Google: `https://openidconnect.googleapis.com/v1/userinfo`).

```
mkdir -p /tmp/http-oauth && scripts/isolated.sh -- go run ./cmd/emetos /tmp/http-oauth
```

1. Make a collection and a request to the "who am I" URL. In its Auth tab choose OAuth 2.0 and the grant Authorization Code; fill in the authorization URL, the token URL, the client ID, the scope the provider documents for it, and keep "Use PKCE" on. Put the client secret, if there is one, in a secret variable of an environment and write `{{clientSecret}}` in its box.
2. Press Get New Access Token: does your browser open the provider's consent page, and after you accept does the page that Emetos serves say you can close the tab, and does the Auth tab show the token (masked) with its type and expiry? While it waits, is a `127.0.0.1` port listening (`ss -ltnp | grep emetos`), and is it gone when the login ends? Cancel in the middle once: does the button go back to Get New Access Token and the port close?
3. Send: does the response carry your name or login? Does the history entry show the token as `********`?
4. Is an item "Emetos HTTP Client: OAuth 2.0 token of ..." in your keyring, and does `grep -r <the access token> /tmp/http-oauth` and the isolated config and state folders find nothing (the request file keeps no token; the client secret is only `{{clientSecret}}`)?
5. If the provider gives refresh tokens (Google needs `access_type=offline` in the authorization URL), wait until the token expires or lower its lifetime and Send again: does it refresh without the browser? Press Clear Token: is the item gone from the keyring, and does Send now say to get a new token?
6. Use the collection in the runner (Run Collection) with the token stored: does it run without opening the browser?

Say which provider, what worked, and any error text (the message names the part that failed: the redirect, the state, the code exchange, the refresh).

**3. The whole pass on your desktop and the real GPU.** The end-to-end display test of this step (make a collection and a request, send it to the dev echo server, extract a token into a variable, use it in a second request, run the collection) runs in a nested sway window on your desktop with the real GPU. Keep the window visible while it runs (a hidden one gets no frames):

```
EMETOS_REALINPUT_RACE=0 scripts/realinput.sh --desktop --shots /tmp/emetos-27.9 -- -tags emetos_display ./cmd/emetos -run TestRealInputTheWholeOfTheHTTPClient -v -count=1
```

Does it pass, and do the pictures in `/tmp/emetos-27.9/` (`httpclient-desktop-*.png`) show what the names say: the Collections view with the new collection and its two requests, the request tab with the response of the echo server, the second request's response with the extracted token in its header, and the runner with both results passed? Then by hand in the editor with the settings of the proofs (`scripts/isolated.sh --config testdata/proof/27.3/config -- go run ./cmd/emetos testdata/proof/27.3/project`, a copy of it in `/tmp` so that nothing is written into the repository): are the method badges, the response pane with a large JSON, the runner's table and the dialogs sharp at 100% and at a zoom of 150% (Ctrl+=)?

Say what you saw: the desktop and compositor, the GPU, which of the steps failed or felt slow.

## 36.4 Playing a video with sound

Play a real video of yours, with sound, isolated from your configuration (FFmpeg must be installed; on Fedora `ffmpeg-free` plays VP9, AV1 and others, the full FFmpeg of RPM Fusion also H.264 and HEVC):

```
scripts/isolated.sh -- go run -tags debug_mode ./cmd/emetos <a video>
```

Press Space (or the Play button). Do the picture and the sound play together, without stutter? Do seeking (the slider, Left and Right), Mute, the volume slider and Loop work? Does switching to another tab pause it? Say what you saw, and what video (container and codecs) you played.
