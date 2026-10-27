# The desktop pass (step 17b.5)

Emetos was used the way a user uses it, through the compositor, with a real keyboard and mouse (`internal/testenv/realinput`, SPEC §15.1), in a headless sway. Each area below has a display test in `cmd/emetos/desktop_*_display_test.go` (`TestDesktop...`; run them with `scripts/realinput.sh --shots <dir>`), which finds what to click from `dev.texts`, checks its effect through `dev.workbench`, `dev.editor`, `dev.inputs`, `dev.markdown` or the files, and saves a screenshot (`desktop-<name>.png`) that was looked at. The pass on the user's own desktop (GPU, `scripts/realinput.sh --desktop`) is the manager's.

"Whose" says whose bug it is: Emetos, Glutter, or the test environment. Status is one of: **fixed** (with the regression test), **reported** (to the manager, with a proposed step in the report of the step), **Glutter issue** (`docs/GLUTTER-ISSUES.md`), **by spec** (SPEC says it is not built), **environment**.

## Findings

### D1. A click on a file in the Explorer took the keyboard focus from the tree
- **Did:** clicked `junk.txt` in the Explorer, then pressed F2 (rename), then typed.
- **Happened:** the file opened as a preview and the keyboard focus went to the editor. F2 did nothing; the letters typed went into the file (`zzjunk`). After a click, Delete and Ctrl+Z (undo of a file operation) also went to the editor.
- **Should:** VS Code opens the preview with `preserveFocus` for a click: the focus stays in the tree, so F2, Delete, the arrows and Ctrl+Z still act on the tree; Enter and a double click move the focus into the editor. SPEC §10.5 already said "The keyboard focus stays in the tree when a preview opens".
- **Whose:** Emetos (the editor mounted with `Autofocus`, and `ActiveGroupChanged` asked for the group's focus).
- **Status:** **fixed.** `openOptions.PreserveFocus` (the editor of that open is mounted with `noAutofocus`, and `askFocus` waits two frames, `holdFocus`) and `openOptions.TakeFocus` (Enter and a double click give the focus to the editor, also when the preview that the click opened is shown already). Regression test: `TestDesktopExplorerClickKeepsTheFocus` (also `TestDesktopExplorer/rename`).

### D2. After "Move to Trash" from the tree the focus was in the editor, so Ctrl+Z did not undo the delete
- **Did:** with other editors open, selected a file in the tree, Delete, "Move to Trash", Ctrl+Z.
- **Happened:** the editors of the deleted file closed, the next editor of the group was mounted with the focus (autofocus), the file stayed in the trash and Ctrl+Z went to the editor.
- **Should:** the focus stays in the Explorer (VS Code), Ctrl+Z restores the file from the trash.
- **Whose:** Emetos.
- **Status:** **fixed.** `deleteItems` remembers that the tree had the focus; after the delete `refocusTree` gives it back (two frames later, and to the first editor that takes the focus in the next second, `editorFocus`). Regression test: `TestDesktopExplorer/delete_to_the_trash_and_undo`.

### D3. Typing after a save joined the undo step before the save, so one Undo went past the saved text
- **Did:** typed `fmt.Println("x")`, Ctrl+S, typed `// more`, pressed Ctrl+Z until the saved text came back.
- **Happened:** the typing after the save was part of the same undo step as the typing before it (`// more` and the `x` came off together), so the saved text was never shown again and the tab stayed dirty.
- **Should:** VS Code pushes an undo stop when it saves (`textFileEditorModel.doSave`: "so that the user has a chance to Ctrl+Z back to the saved version"); SPEC §6.3/§7.1.
- **Whose:** Emetos (`core/document`: the save did not close the open undo group).
- **Status:** **fixed.** `Document.begin` (Save, Save As and the participants) pushes a stack element. Regression tests: `TestSaveClosesTheUndoGroup` (core) and `TestDesktopEditGoFile/undo_and_redo` (real input; it failed with "ten undos did not bring the saved text back").

### D4. The tabs of the panel are cut by the Problems filter
- **Did:** opened the Problems panel (Ctrl+Shift+M) in a 1280-pixel window, with the panel tabs Problems, Output, Tasks, Agents: Log in, Go Tools, Go Run and Go Tests.
- **Happened:** with a count badge on Problems and Tasks the tabs are wider, the filter box and the buttons take the room after them (with no badge all seven tabs fit, `desktop-panel-problems-empty.png`), the strip of tabs scrolls inside what is left and the last tab shows cut in the middle of a word ("GO T") with nothing that says there are more (`desktop-problems-opened.png`, `desktop-tasks-problem.png`, `desktop-gopls-problems.png`).
- **Should:** VS Code's panel title keeps its tabs whole and puts what does not fit in a "..." menu, and the view's actions shrink first.
- **Whose:** Emetos (`gui/workbench` `panel`: `panelTitleReserved`, the strip is a horizontal scroll view).
- **Status:** **planned** (step 17b.6; a small step: give the actions of the view a smaller maximum, and show an overflow button with the hidden tabs; the Agents tab goes in M17c, which makes it one tab less).

### D5. Keys that arrive 10 ms apart race the Find box and the Explorer's new input
- **Did:** pressed Ctrl+F and typed `one` with the harness's default key delay of 10 ms; and pressed F2 in the tree and typed at once.
- **Happened:** (a) with the 10 ms delay the cursor of the editor did not move to the first match while typing, so the first Enter selected the first match instead of the second; at 25 ms per key it moves as it should; the model alone (`EnterText`, or three `FindState.Change` calls in a row) is right. (b) A letter typed in the frame after F2 was seen by the tree as the start of its find box while the rename input was not built yet (an empty find box stayed open under the tree).
- **Should:** a person types a key every 50 to 150 ms, so both cannot be seen by one; but keys a frame apart (key repeat, IME, a fast typist) should not be lost to a half built state.
- **Whose:** Emetos (not found in the model; a race between the key events and the widgets built a frame later; not reproduced with the model alone).
- **Status:** **planned** (step 17b.6; not fixed: not reproduced below 25 ms; the tests of this pass type with 25 ms where the effect of a key depends on the key before it, and wait after F2, and say so).

### D6. The hover box stays beside an open context menu
- **Did:** right-clicked a word of the Go file while the pointer rested on it.
- **Happened:** the hover of the word showed (after the delay) next to the context menu and stayed while the menu was open (`desktop-mouse-menu.png`).
- **Should:** VS Code hides the hover on a mouse press and does not show one while a menu is open.
- **Whose:** Emetos (the hover timer of `gui/editor/hover.go` does not know the menu).
- **Status:** **planned** (step 17b.6).

### D7. Find in Files (the Search view) is not built
- SPEC §3, area 4 "Project-wide search and replace: later". Not tested. **by spec.**

### D8. "Open Folder" needs the desktop's folder dialog
- **Did:** clicked "Open Folder" in an empty window.
- **Happened:** under the test environment (no session bus) a notification says "Cannot show the dialog to open a folder: dial unix ...". On a desktop the dialog is the portal's.
- **Whose:** environment. The way a folder is opened without the dialog, File > Open Recent (Ctrl+R), works (`TestDesktopOpenFolder`). **environment.**

### D9. The Markdown preview in the group on the side does not scroll the source
- **Did:** opened the preview to the side of a 700 KB document and scrolled the preview with the wheel.
- **Happened:** the preview scrolled and the source stayed at line 1.
- **Should:** VS Code scrolls the editor with the preview (`markdown.preview.scrollEditorWithPreview`).
- **Whose:** SPEC §10.15 "Not built: scrolling the preview with the Text editor of another group". **by spec.**

### D10. A view of an extension shows its buttons only while the pointer is over its header
- **Did:** opened the Notes view (empty): "No notes yet. Use New Note to make one."
- **Happened:** the `+` that makes a note shows only while the pointer is over the header (as in VS Code), so the text points to a button that is not drawn.
- **Whose:** Emetos (UX; the same as VS Code's behavior, but VS Code's empty views have a "welcome" button).
- **Status:** **planned** (step 17b.6; minor: a welcome content with a button for the empty Notes view).

### D11. The label of a window tab is cut early
- **Did:** opened a Scratchpad window tab.
- **Happened:** the tab shows "Scratchp..." although the strip has room for the whole word (`desktop-wtabs-scratchpad.png`).
- **Should:** the label is cut only when the strip is full.
- **Whose:** Emetos (the width of a window tab; minor).
- **Status:** **planned** (step 17b.6).

### D12. Two tests fail now and then, with or without load
- **Did:** the manager's check runs of the step: `CGO_ENABLED=0 go test ./internal/gui/app -run TestAPictureThatWaitedForOnFrameAndARefreshAreHandedOver -count=30` at a load of 5, and the race runs below.
- **Happened:** "a picture was handed over after Close" 3 times in 30; `TestTheCoreTopicsDriveARealWorkspace` fails about once in 20 runs under `-race`. Both did so before this step too.
- **Should:** a Remote Work view hands nothing to its client after `Close`; both tests pass every time.
- **Whose:** Emetos (a race in the code or in the tests, to be found).
- **Status:** **planned** (step 17b.6).

## Tests of the packages that failed on a loaded machine

Not findings of the pass, but seen while checking it (the machine was shared and its load was 10 to 25): `TestGoldenOfAHeadlessView` (`internal/gui/app`) failed under `-race` with the folder not listed yet, with the changes of the step and with the commit before them (it settled after 300 ms of quiet; `settleQuiet` is now one second, and the test passed three times in a row under `-race`); `TestTheCoreTopicsDriveARealWorkspace` fails once in 12 to 20 runs under `-race` with and without the changes of the step (the same runs of the commit before the step failed 3 times in 60: a race of the test, not changed here); `TestDesktopFindAndReplace` and `TestDesktopExplorer` failed once each on the loaded machine and not on later runs (the first one now types one key at a time, D5; the second one has the second way back of D2).

## Passed

Each of these worked with real input, and its screenshot was looked at.

| Area | Test | What was checked |
|---|---|---|
| Session: open a folder | `TestDesktopOpenFolder` | the empty window, the failed dialog's notification (D8), Open Recent opens the folder, its file opens |
| Explorer | `TestDesktopExplorer` | expand a folder with a click, a click opens a preview (italic tab, replaced by the next), a double click keeps it, new file and new folder (header buttons, typed name, Enter), rename (F2), delete to the trash (the dialog, the file in the trash of the run), undo (Ctrl+Z), drag onto a folder (the confirmation, the move) |
| Explorer focus | `TestDesktopExplorerClickKeepsTheFocus` | D1 |
| Editing a Go file | `TestDesktopEditGoFile` | typing, the brackets the editor closes and types over, save (file and dirty mark), undo and redo (D3), completion with the fake server (the list, Enter), hover (shows, goes away) |
| Problems | `TestDesktopEditProblems` | a diagnostic of the server is in the panel, a click goes to its place |
| Mouse and clipboard in the editor | `TestDesktopEditorMouseAndClipboard` | double click selects a word, a drag selects, the context menu, Ctrl+C and Ctrl+V of a line, closing a dirty tab asks and Don't Save keeps the file |
| Real gopls | `TestDesktopRealGopls` (tag `emetos_gopls`) | completion, hover, an undefined name in Problems |
| Quick Open | `TestDesktopQuickOpen` | typing, the list, Enter opens and the focus goes to the editor, Escape leaves it as it was |
| Command palette | `TestDesktopCommandPalette` | typing, the list with the key shown, Enter runs it (an untitled editor takes typing) |
| Go to Line, Go to Symbol | `TestDesktopGoToLineAndSymbol` | `6:5` is a visible column (after the tab), `@` lists the symbols of the server and Enter goes to it |
| Find and Replace | `TestDesktopFindAndReplace` | count, next and previous, the replace row, replace, replace all, one undo |
| Settings editor | `TestDesktopSettingsEditor` | search, a check box writes `settings.json` and writes it back, a number typed in its box |
| Keybindings editor | `TestDesktopKeybindingsEditor` | search, a double click, the key recorded with real keys, written to `keybindings.json` |
| Theme picker | `TestDesktopThemePicker` | the list, Down previews, Enter writes the setting |
| Diff editor | `TestDesktopDiffEditor` | Select for Compare and Compare with Selected from the Explorer's menu, the two sides, editing the modified side and saving, next change (Alt+F5) |
| Merge editor | `TestDesktopMergeEditor` | the notification's button, Accept Incoming, Complete Merge writes the file and closes the tab |
| Split groups, tabs | `TestDesktopSplitGroupsAndTabDrag` | Ctrl+\\ splits, a file opens in the new group, a tab dragged to the other group, a tab dragged to reorder, a tab dropped on the editor |
| Workspace tabs | `TestDesktopWorkspaceTabs` | the + menu, a Scratchpad tab, back to the first tab (its editor is there), Ctrl+Alt+PageDown, close with the button |
| Panel | `TestDesktopPanelOutput` | the Output panel (Ctrl+K Ctrl+H) shows a logged line, Ctrl+J hides it, Ctrl+Shift+M shows Problems |
| Notes | `TestDesktopNotes` | the view's button, title and body typed, the list shows the note, closing and opening it again keeps its text |
| TODOs | `TestDesktopTodos` | the view lists the comments, a click goes to the line |
| Tasks | `TestDesktopTasks` | Tasks: Run Task from the palette, the output and its end in the panel, an error line is a problem and a click goes to it |
| Markdown preview | `TestDesktopMarkdownPreview` | the editor's title action opens it to the side, its blocks, the wheel scrolls it, an edit of the source adds a block (D9 is by spec) |
| Second launch | `TestDesktopSecondLaunchHandsAFileOver` | `emetos late.txt` opens in the running instance, no second window tab, `--goto file:6:3` puts the cursor there |

Skipped by the step: the Agents extension (removed in M17c).
