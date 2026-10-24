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
