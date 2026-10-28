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
- **Status:** **fixed** (step 17b.6). The title strip of the panel (`workbench/paneloverflow.go`) measures its tabs: the tabs and the buttons that maximize and hide the panel get the room first, the actions of the view what is left (and none when that is under 130 px), and tabs that do not fit go into the menu of a "..." button (the active tab always shows, a tab is shown whole or not at all). Regression tests: `TestDesktopPanelTabsAreWholeAndOverflow` (real input: 1280 and 1024 px, no tab cut, then 760 px with the menu), `TestPanelTabsAreWholeAndOverflowIntoAMenu` and `TestPanelOverflowMenuListsTheHiddenTabs`.

### D5. Keys that arrive 10 ms apart race the Find box and the Explorer's new input
- **Did:** pressed Ctrl+F and typed `one` with the harness's default key delay of 10 ms; and pressed F2 in the tree and typed at once.
- **Happened:** (a) with the 10 ms delay the cursor of the editor did not move to the first match while typing, so the first Enter selected the first match instead of the second; at 25 ms per key it moves as it should; the model alone (`EnterText`, or three `FindState.Change` calls in a row) is right. (b) A letter typed in the frame after F2 was seen by the tree as the start of its find box while the rename input was not built yet (an empty find box stayed open under the tree).
- **Should:** a person types a key every 50 to 150 ms, so both cannot be seen by one; but keys a frame apart (key repeat, IME, a fast typist) should not be lost to a half built state.
- **Whose:** Emetos (not found in the model; a race between the key events and the widgets built a frame later; not reproduced with the model alone).
- **Status:** **fixed** (step 17b.6). The cause was not the model: a key that came before the input was built was handled against the state of the old widgets. (a) `kit.TypeAhead` put what it took into the text of the input and never told the owner, so the find box held the text and the editor never searched; (b) a hardware handler cannot keep the focus tree from seeing a key, so the first letter after F2 also started the find of the Explorer's tree, Enter made a new line or was lost, and the keys typed after Ctrl+, while the editor went away reached nothing (Glutter's early handlers are not called while no node has the focus, `docs/GLUTTER-ISSUES.md`). `TypeAhead` now claims the keys it takes from the focus tree, tells the owner (`onChange`), keeps Enter, the arrows, Home, End and Delete and sends them to the input once it has the focus, in order, and `StartTypeAheadLater` does it for an editor that is still to be built (the settings and keybindings editors). The harness got `NoKeyDelay`, `TypeBatch`, `PressBatch`, `Hold`, `TextCombos`, `SetKeyRepeat`. Regression tests: `TestDesktopFastKeys{Find,Rename,NewFile,QuickOpen,CommandPalette,SettingsSearch,KeybindingsSearch,ThemePicker}` (`Notes` was the last one until step 17e.1 removed the extension; the theme picker took its place) (each at 10 ms a key, with no delay and in one batch), `TestDesktopFastKeysHeld` (a key held for 1.6 s at 25 and 50 a second, in the editor and in the find box), `TestFastKeysAfterCtrlFSearch`, `TestKeysTypedRightAfterTheKeyThatOpensTheInputGoToIt`, and the `TypeAhead` tests of `kit`.

### D6. The hover box stays beside an open context menu
- **Did:** right-clicked a word of the Go file while the pointer rested on it.
- **Happened:** the hover of the word showed (after the delay) next to the context menu and stayed while the menu was open (`desktop-mouse-menu.png`).
- **Should:** VS Code hides the hover on a mouse press and does not show one while a menu is open.
- **Whose:** Emetos (the hover timer of `gui/editor/hover.go` does not know the menu).
- **Status:** **fixed** (step 17b.6): `secondaryDown` hides the hover and cancels the one on its way (the right button is pressed within the hover delay of the pointer's arrival; the hover then came after the menu). Regression test: `TestDesktopHoverHidesWhenTheContextMenuOpens`.

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
- **Status:** **fixed** (step 17b.6): the empty Notes view shows "No notes yet." over a "New Note" button. Regression test: `TestDesktopEmptyNotesViewHasAWelcomeButton`. **Gone** (step 17e.1): the Notes extension was removed at the user's request (SPEC §10.26, D28), and the test went with it; no other extension has a view with a button in its header, so nothing else shows this case. The behaviour of an empty view with a welcome button is not built in the host, only in what the extension drew.

### D11. The label of a window tab is cut early
- **Did:** opened a Scratchpad window tab (the extension was removed in step 17e.1; the regression test now opens the Relay tab).
- **Happened:** the tab shows "Scratchp..." although the strip has room for the whole word (`desktop-wtabs-scratchpad.png`).
- **Should:** the label is cut only when the strip is full.
- **Whose:** Emetos (the width of a window tab; minor).
- **Status:** **fixed** (step 17b.6). The width of a workspace tab was the measured width of its label, a fraction of a pixel narrower than the drawn paragraph (a label of 69.27 px drawn, 69 measured), so the paragraph showed an ellipsis; `tabNaturalWidth` adds `tabLabelSlack` (1 px). `dev.texts` reports `cut` for a paragraph narrower than its text, which the test reads. Regression test: `TestDesktopWindowTabLabelIsNotCutWhenThereIsRoom`.

### D12. Two tests fail now and then, with or without load
- **Did:** the manager's check runs of the step: `CGO_ENABLED=0 go test ./internal/gui/app -run TestAPictureThatWaitedForOnFrameAndARefreshAreHandedOver -count=30` at a load of 5, and the race runs below.
- **Happened:** "a picture was handed over after Close" 3 times in 30; `TestTheCoreTopicsDriveARealWorkspace` fails about once in 20 runs under `-race`. Both did so before this step too.
- **Should:** a Remote Work view hands nothing to its client after `Close`; both tests pass every time.
- **Whose:** both tests were wrong, and the first one pointed at a window in the code.
- **Status:** **fixed** (step 17b.6). `TestAPictureThatWaitedForOnFrameAndARefreshAreHandedOver`: the view keeps drawing (the caret blinks), so pictures made before `Close` waited in the buffered channel the test read after it ("queued before Close: 1"); the test now counts the calls that start after `Close` has returned. The code had a real but narrow window as well: a frame that had passed the check of `closed` could call the function a moment after `Close` returned, so `Close` now waits (at most 250 ms, for a function that closes the view itself) for the call that is running (`headlessView.waitCalls`; `extension.HeadlessView.OnFrame` says so). Regression tests: `TestCloseWaitsForTheCallOfOnFrameThatIsRunning`, `TestAFunctionOfOnFrameMayCloseTheView`; the first test passes 200 times in a row without cgo. `TestTheCoreTopicsDriveARealWorkspace`: the reply of `emetos.document.open` comes from the UI thread as soon as the model has changed, and the test read the tab strip, a widget the next frames build, right after the reply; `ask` now pumps the frames first (200 runs in a row under `-race`).

### D13. `TestRealInputBigPreviewOpensAndScrolls` failed on the desktop: "a frame took 105 ms of the UI thread"
- **Did:** the manager ran `scripts/realinput.sh --desktop` (`GLUTTER_RENDERER=auto`, Radeon 8060S, Vulkan) 4 times: the test failed 4 times with "scrolling and jumping: 492 frames, the UI thread 1.02ms on average and 105.32ms at worst, the renderer 3.91ms and 896.13ms", and the log line `slow frame build=105.32ms raster=896.13ms` (111, 121, 112 ms and 875 to 927 ms in the other runs). Headless (GLES on llvmpipe) it passed, with the same frame (`build=99.97ms raster=4.23s`) outside the counted window.
- **Happened:** that frame is not the preview's. It is the first frame of the editor's window. Evidence: (1) a run that logs every slow frame with its time shows `msg="first frame presented" since_run=4.4s` on llvmpipe (0.97 s on the GPU) logged at the same millisecond as the 105 ms / 4 to 5 s slow frame, and that frame ended seconds before the command that opens the preview; (2) the preview's own frame (the one that builds its first blocks, 87 ms after the command) is a different line, 42 to 47 ms of the UI thread in the debug build and 8 to 9 ms in the release build, and it costs the same on the second and the third open of the preview in one process (47 and 49 ms), so nothing in it is a first use; (3) the `GLUTTER_FRAME_STATS` window is logged once a second by the first frame that comes a second after the window began, and the first window begins with the first frame of the editor, so the window that was "taken right after `openBigPreview`" (`before := len(a.FrameWindows())`) was often not yet logged, and the window with the start of the window in it was counted as "scrolling and jumping". The UI-thread part of that frame (100 to 114 ms in the debug build the display tests run, 10 to 20 ms in the release build) is the first build of the whole workbench with a 700 KB file in the editor; the renderer part is the first draw, below.
- **Should:** the test measures the scrolling and jumping; the start of the editor and the open of the preview are other numbers.
- **Whose:** the test (the window of the measurement). Of the two numbers behind it: the UI thread of the first frame is the debug build's (Glutter's `UIThread.AssertCurrent` reads the goroutine id from `runtime.Stack` in `RenderObject.MarkNeedsLayout`, `Element.MarkNeedsBuild`, `ChangeNotifier.NotifyListeners` and the like: 63 ms of the roughly 150 ms of non-renderer, non-highlighting CPU of a CPU profile that held the start and two opens; Emetos's own Markdown code was 4 ms of it; `docs/GLUTTER-ISSUES.md`), and the renderer part is the driver's and Glutter's first draw, a first start only (`docs/PERFORMANCE.md` "Desktop pass (step 17b.6)": GPU context or device 570 to 590 ms in the nested sway, shaders 287 ms with no cache and 0.1 ms with Glutter's pipeline cache or Mesa's warm; llvmpipe 4 to 5 s with no Mesa cache, 0.3 s with it). `display.Start` gives every run a new `XDG_CACHE_HOME`, so every test run is a first start for the driver.
- **Status:** **fixed** (step 17b.6). `App.WaitFrameWindow` waits for the next window of statistics after the open, and the scrolling is measured from there; `App.SlowFrames` (`ParseSlowFrames`) reads the slow frames with their time, and the test checks the UI-thread time of the frames of the open on their own (at most 100 ms in the debug build; with the budget at 10 ms the test fails with "a frame of the open of the preview took 45.64ms of the UI thread"). The budget of the scrolling is unchanged. Regression tests: `TestRealInputBigPreviewOpensAndScrolls` (passes headless 3 of 3 and on the desktop 2 of 2, also with the release build) and `TestParseSlowFrames`. Nothing in the preview needed a fix. The manager's reading ("the first frame of the preview is slow") was the first frame of the window; there is no pipeline or shader made for the first time by the preview (Vulkan makes its 6 pipelines in the first draw, the preview adds none).

## Tests of the packages that failed on a loaded machine

Not findings of the pass, but seen while checking it (the machine was shared and its load was 10 to 25): `TestGoldenOfAHeadlessView` (`internal/gui/app`) failed under `-race` with the folder not listed yet, with the changes of the step and with the commit before them (it settled after 300 ms of quiet; `settleQuiet` is now one second, and the test passed three times in a row under `-race`); `TestTheCoreTopicsDriveARealWorkspace` fails once in 12 to 20 runs under `-race` with and without the changes of the step (the same runs of the commit before the step failed 3 times in 60: a race of the test, not changed here); `TestDesktopFindAndReplace` and `TestDesktopExplorer` failed once each on the loaded machine and not on later runs (the first one types at the pace of the harness since D5 was fixed; the second one has the second way back of D2).

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
| Workspace tabs | `TestDesktopWorkspaceTabs` | the + menu, a Relay tab (a Scratchpad tab before step 17e.1), back to the first tab (its editor is there), Ctrl+Alt+PageDown, close with the button |
| Panel | `TestDesktopPanelOutput` | the Output panel (Ctrl+K Ctrl+H) shows a logged line, Ctrl+J hides it, Ctrl+Shift+M shows Problems |
| Notes, TODOs | (removed in step 17e.1, SPEC §10.26) | `TestDesktopNotes` and `TestDesktopTodos` went with the extensions |
| Tasks | `TestDesktopTasks` | Tasks: Run Task from the palette, the output and its end in the panel, an error line is a problem and a click goes to it |
| Markdown preview | `TestDesktopMarkdownPreview` | the editor's title action opens it to the side, its blocks, the wheel scrolls it, an edit of the source adds a block (D9 is by spec) |
| Second launch | `TestDesktopSecondLaunchHandsAFileOver` | `emetos late.txt` opens in the running instance, no second window tab, `--goto file:6:3` puts the cursor there |

Skipped by the step: the Agents extension (removed in M17c). Removed later: Notes, TODOs and Scratchpad (step 17e.1).

- **Update (2026-10-28):** `TestRealInputBigPreviewStartDoesNotWaitForThePreview` (17b.2), which compared two cold starts, failed now and then by 241 to 672 ms on the loaded machine: every start paid the Mesa shader compile of a fresh `XDG_CACHE_HOME` (4 to 5 s, about ±500 ms of noise). It now gives all its starts one `MESA_SHADER_CACHE_DIR`, warmed by throwaway starts until a first frame is under 1 s, and compares the fastest of three alternating small and big starts against the same 150 ms (after: +2 to +40 ms in five runs; a 300 ms sleep in the preview's open still fails it).

## The hosting pass (step 17e.4)

The user, on 2026-10-28: "remote work and relay are not clear on how one workspace can become host and how to disable it." Before §10.29 was built, Remote Work and Relay were used as a new user would use them: two Emetos processes in one headless sway on 127.0.0.1, a fresh state for the host, test keys made for the run, and the keys and the pointer of the compositor (`internal/testenv/realinput`) for everything. The host looked for a way to host, found the Remote Work page and the Relay tab, turned hosting on, added the key of the other computer, let that computer connect, and then looked for a way to stop. The screenshots of the old interface were made in a scratch folder of the run and looked at; they are not kept, and the findings below say what each showed.

The fixes are tested by `TestRealInputHostWithRemoteWorkFromTheStatusItem` and `TestRealInputHostWithRelayFromTheStatusItem` (`cmd/emetos/hosting_display_test.go`, the same two runs with the new interface; `scripts/realinput.sh --shots <dir> -- ./cmd/emetos -run TestRealInputHostWith -v`) and by the tests named in each finding.

### H1. A hosted workspace shows nothing
- **Did:** turned Remote Work on in the page, and Relay on in its tab, then closed both and looked at the workspace.
- **Happened:** nothing in the workspace says it is hosted: the status bar, the title and the tabs are the same as before. The only sign is a banner "Remote Work: alice@laptop connected" while a computer is connected; with nobody connected a hosted workspace and one that is not look the same. The tab of Relay shows "Listening on port ..." only while it is open.
- **Should:** a status bar item, as VS Code's remote indicator, that says at once whether the workspace is hosted, by what, and with how many computers.
- **Whose:** Emetos.
- **Status:** **fixed.** The Sharing item (`extensions/hosting`): the icon alone when not hosted, "Hosting" with the number of computers (a blue background, `statusBarItem.remoteBackground`) when hosted, and a tooltip with the port and the computers. Tests: `TestTheStatusItemShowsWhatHostsTheWorkspace`, `TestTheItemFollowsTheHostsWhileItIsOnScreen`, the golden `statusbar_hosting_*`, and the real-input tests.

### H2. There is no command to host a workspace or to stop hosting it
- **Did:** opened the Command Palette and typed "host", then "remote", then "relay".
- **Happened:** "host" found "No matching commands". "remote" found only "Remote Work: Manage". "relay" found nothing: Relay has no command at all, only an entry of the `+` menu that opens a tab.
- **Should:** "Host This Workspace" and "Stop Hosting This Workspace" for each extension, and a way to open the Relay tab.
- **Whose:** Emetos.
- **Status:** **fixed.** `emetos.remotework.host` and `.stopHosting`, `emetos.relay.host`, `.stopHosting` and `.open`, and `emetos.hosting.menu` (the menu of the item); a Relay client may not run any of them (`command.ClientRefusal`). Tests: `TestTheHostCommandsStartAndStopHosting` (both extensions), `TestClientRefusal` (the five commands and the menu).

### H3. Hosting can be turned on with no key, and nothing says a key is needed
- **Did:** on a fresh machine, opened the page and turned the switch on.
- **Happened:** "On. Listening on port 34489, for the keys below." Two screens further down: "No key is listed, so nobody can log in." Nothing connects the two lines, and the switch is on with a port open that nobody can use.
- **Should:** the first thing a new user does is add the key of the computer that will connect; the page says so before it offers a switch, and Host with no key opens the page at that step.
- **Whose:** Emetos (UX).
- **Status:** **fixed.** The first-time guide: three steps with a check mark when each is done ("Add the public key of the computer that will connect", "Turn hosting on", "On the other computer, connect to:"); Host with no key opens the page or the tab and does not turn anything on; the keys come first on the page. Tests: `TestTheGuideShowsWhatIsDone` (both), `TestTheHostCommandsStartAndStopHosting` ("host with no key opens the page at the guide").

### H4. Nothing says where to connect
- **Did:** turned Remote Work on and went to the other computer's "Remote Work..." tab.
- **Happened:** its form asks for a host ("laptop.local or 192.168.1.20") and a port; the host's page names only the port. The user has to find the address of the host elsewhere.
- **Should:** the page lists the addresses of this computer, with a button to copy each, and the plain line under the switch names one.
- **Whose:** Emetos.
- **Status:** **fixed.** Step 3 of the guide lists each non-loopback address of the up interfaces with the port and a Copy button (`sharing.Addresses`), and the line under the switch says "On. Computers with an allowed key can connect to 192.168.1.5:47123." Tests: `TestAddressesAreTheOnesAnotherComputerCanConnectTo` (a fake list of interfaces), `TestTheAddressesAreTheNonLoopbackOnesOfThisComputer` (both), `TestTheSwitchTurnsRemoteWorkOnAndOff`, `TestThePlainLineUnderTheSwitch`.

### H5. The fingerprint to compare is at the bottom of the page
- **Did:** looked for the fingerprint that the other computer asks to compare the first time.
- **Happened:** it is under "Host key of this computer", below Connection, Allowed keys and the start of Security; with the page at its normal size it is out of view, and nothing near the switch points to it.
- **Should:** next to the address, in the step that says what to do on the other computer.
- **Whose:** Emetos.
- **Status:** **fixed.** Step 3 of the guide shows "Host key fingerprint, to compare when it asks:" under the addresses; the old section is gone. Tests: `TestTheGuideShowsWhatIsDone`.

### H6. Relay has two switches for one idea, and listening alone does nothing
- **Did:** turned on "Listen for other computers" in the Be a Host section of the Relay tab.
- **Happened:** "Listening on port 34413, for the keys below." Nothing is shared yet: the workspaces to share are a block of checkboxes further down. The first computer that connects is given no workspace. Nothing connects the two.
- **Should:** one idea, "host", in the words of the page: the switch says what it does, and the line under it says when it has nothing to host.
- **Whose:** Emetos (UX).
- **Status:** **fixed.** The switch is "Host the shared workspaces"; its line says "On, but no workspace is shared yet. Check one under Workspaces to share." until a workspace is checked; Host this workspace (the command and the item) checks the workspace and turns listening on in one step. Tests: `TestThePlainLineUnderTheSwitch`, `TestHostWorkspaceSharesAndListens`.

### H7. Nothing says "stop", and stopping needs the right control
- **Did:** with a computer connected, looked for a way to stop hosting.
- **Happened:** the word "stop" is nowhere. In Remote Work the way is to open the page again (only the palette's "Remote Work: Manage" opens it) and uncheck "Allow other computers to use project". In Relay, unchecking the workspace leaves the port open ("Listening on port ...") and unchecking "Listen for other computers" stops every workspace; the two look alike.
- **Should:** a Stop Hosting button beside the switch, a command, an entry in the menu of the status item, and a button on the banner.
- **Whose:** Emetos.
- **Status:** **fixed.** Stop Hosting: the button beside the switch (shown while it is on), `emetos.remotework.stopHosting` and `emetos.relay.stopHosting`, "Stop Hosting with ..." in the menu of the item, and the button of the banner of Remote Work. Relay turns listening off when no workspace is shared any more, and ends at once the sessions of the clients that were given only that workspace. Tests: `TestStopHostingStopsOneWorkspaceAndListeningWhenNoneIsLeft`, `TestStopHostingTellsTheClientThatOnlyHadThatWorkspace`, `TestStopHostingEndsTheSessionsAndTheClientIsTold`, `TestTheHostingBannerFollowsTheState`, the real-input tests.

### H8. The three words for the same thing
- **Did:** read the two pages one after the other.
- **Happened:** Remote Work says "Allow other computers to use project", Relay says "Listen for other computers" and "Workspaces to share", the status line says "Listening", the banner says "connected". Four words for one state.
- **Should:** one word: host.
- **Whose:** Emetos (UX).
- **Status:** **fixed.** "Host this workspace" (Remote Work), "Host the shared workspaces" (Relay), "Hosting" (the item and the messages), "Stop Hosting" (every button, command and menu entry), "How to host" (the guides and the README).

### H9. The client of Remote Work is told twice, in the host's words, that something was turned off
- **Did:** stopped Remote Work on the host while the other computer was connected.
- **Happened:** the client's card says "Remote Work was turned off on 127.0.0.1:34489" and under it "Remote Work was turned off for the workspace".
- **Should:** one clear sentence: "The host stopped sharing this workspace."
- **Whose:** Emetos.
- **Status:** **fixed.** Title "The host stopped sharing this workspace." with the line "Remote Work was turned off on <host>." Test: `TestTheHostEndingTheSessionShowsItsReasonOnTheTab` (the case "Remote Work is turned off").

### H10. The client of Relay is told in lower case, twice
- **Did:** the same with Relay.
- **Happened:** the Hosts section of the client shows "Ended: Hosting was turned off on 127.0.0.1:36355" and, under it, "hosting was turned off" in lower case.
- **Should:** the same sentence as Remote Work.
- **Whose:** Emetos.
- **Status:** **fixed.** The same title, and the reason of the host as a sentence under it ("Hosting was turned off.", "The port changed."). Tests: `TestTheClientIsToldWhyInWords`, `TestAHostThatEndsTheConversationIsNotConnectedAgain`, `TestStopHostingTellsTheClientThatOnlyHadThatWorkspace`.

### H11. The host banner of Remote Work appears only when a computer is connected
- **Did:** turned Remote Work on and waited with nobody connected.
- **Happened:** no banner; the first one is the banner of a session. Relay is the same.
- **Should:** while the workspace is hosted, the workspace says so, and the banner has the way to stop.
- **Whose:** Emetos.
- **Status:** **fixed for Remote Work** ("This workspace is shared with Remote Work (1 computer connected)" with Stop Hosting, from the moment hosting is on; the banners of the sessions stay, with their Disconnect). **Not for Relay, by decision 200 (`docs/DECISIONS-TO-REVIEW.md`):** its sessions keep their banners, and the Sharing item says that the workspace is hosted. Test: `TestTheHostingBannerFollowsTheState`.
