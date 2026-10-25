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

## 3.11 Performance with the GPU

The targets of SPEC §14 are decided by the numbers in your desktop session, with its GPU; the headless ones are in `docs/PERFORMANCE.md`. Close other heavy programs, then run (it opens and closes a few editor windows, and one counter window to take the focus away; it reads nothing of your configuration):

```
scripts/perf.sh --session
```

Say what the table shows (or paste it), in particular the cold start (target < 400 ms), the 10 MB open (< 300 ms), the idle CPU (< 1%) and the frames of the unfocused window (none).

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

1. Is there an Emetos icon in the tray (a rounded indigo square with a white `</>`)? Is it readable at the size of your panel? Does the tooltip say "Emetos" and "1 workspace"?
2. Open the tray menu (right click). Does it show the tab `emetos-tray-a`, then Open Folder..., New Workspace Tab, Hide Emetos and Quit? Choose Open Folder... and pick `/tmp/emetos-tray-b`: does the tab appear in the window and in the menu, and does the tooltip say "2 workspaces"?
3. Press the close button of the window. Does the window disappear (no task bar entry) while the icon stays, and does the menu now say Show Emetos? Click the icon: does the window come back, with its tabs, in front? Click it again while the window is in front: does it hide?
4. Hide the window, then choose a tab in the menu: does the window come back with that tab active?
5. Type something in `a.txt` without saving, press the close button, and show the window again: is the unsaved text still there? Then choose Quit in the menu: does the question (or, with hot exit, nothing) behave as Ctrl+Q does, and does Emetos end (no icon, `pgrep -a emetos` shows nothing)?
6. If you can, turn the tray off (or use a desktop without one, such as GNOME without the extension): does the close button then quit instead of hiding the window?

Say what you saw, and which desktop you used.
