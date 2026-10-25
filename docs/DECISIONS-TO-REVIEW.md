# Decisions to review

Decisions taken on the user's behalf while the steps run, newest last, with their reason and where they live. Entries 1–17 were accepted by the user on 2026-10-25.

## 2026-10-25: the plan

1. **Order of the remaining work:** 9.6 → 9.7 → 9.8 → 9.9 (error routes audit) → M9b (9b.1–9b.5, one window with window tabs and the tray) → 10.1 (performance) → 10.2 (release) → M11 (11.1–11.6, extensions) → M12 (12.1, plan area 3). Release before extensions, so 10.2 describes the one-window design and not the multi-window one that goes away. (`docs/IMPLEMENTATION.md`)
2. **Single window details** I chose (SPEC §10.14): the menu becomes a `≡` button (`window.menuBarVisibility` gains `compact`, the new default); window tabs are 120–240 px and scroll when they overflow; duplicate folder names get the parent folder after an em dash; tab jumps are **Ctrl+Alt+1…9** (Alt+1…9 stay VS Code's "open editor at index"); Ctrl+Alt+PageUp/PageDown cycle; Ctrl+Shift+N opens a No Folder tab and Ctrl+Shift+W closes the active window tab; inactive tabs keep running and stay mounted but do not tick or paint; `window.restoreWindows` keeps its name and means "restore tabs"; the close button hides to the tray (`window.closeToTray`, default on) and quits when no tray is shown (plain GNOME without AppIndicator).
3. **The `+` button** after the last tab opens a menu: **Workspace...** (the desktop's folder chooser → new workspace tab), then one entry per extension tab kind from M11. Until M11 it has only "Workspace...".
4. **Extensions** (SPEC §10.15, D18), from our conversation, with these details decided by me:
   - Extension tabs are **window tabs only** (top-level, beside workspaces), never tabs inside a workspace's editor area. Inside a workspace, extensions contribute **file views** (micro-tabs) and **side bar views**.
   - Micro-tabs sit at the **right end of the breadcrumbs row** (a 22 px strip when breadcrumbs are hidden), shown only for files with at least one extension view; **Text** is always first; the choice is remembered per editor tab; `workbench.editor.defaultFileViews` picks a default view per glob; `workbench.action.cycleFileView` cycles.
   - Extensions talk **only through events**, plus **request/reply events** for "call an action of another extension" (10 s default timeout). No typed Go APIs between extensions, no calling each other's commands.
   - Every **window tab is a scope**; events cross scopes only along a **directional link** (`notes → office`) the user allowed, optionally limited to topics; the links live in `~/.local/state/emetos/event-sharing.json` (your choice (a)), written only by Emetos's prompt ("Allow", "This Session Only", "Deny") and its page "Extensions › Event Sharing"; no settings file can change them. A workspace can be **private** (sends nothing). An extension can only ask for cross-tab topics it declared in its manifest.
   - Values in events are JSON copies (nothing live crosses tabs); fast streams like typing are batched per frame.
   - **Language packs** move to `extensions/` (step 11.4) with no change of behavior; the core keeps plain text only. Themes and the Seti icon theme stay in the core.
   - First real extensions to prove the three UI kinds: `extensions/markdown` (preview file view), `extensions/todo` (side bar view of TODO/FIXME comments), `extensions/scratchpad` (a window tab with notes and a "Recent edits" feed that exercises the event-sharing prompt). A web browser is not feasible: Glutter has no web view and a web engine needs cgo or a huge dependency.
   - Disabling: `extensions.disabled` (a list of IDs) and an "Extensions" page with Enable/Disable.
   - Area 3 (LSP, `gopls` in `extensions/golang`) gets its own plan in step 12.1, written by me and logged here instead of asking you.
5. **Error routes:** step 9.9 adds route tests for every untested tag, removes the error-building helpers (AI_GUIDE R4) and the package-level `errors.New` (R5), adds a script `scripts/error-tags.sh --untested`, and puts R4/R5 into CLAUDE.md.
6. **How "ask the user" steps are handled tonight:** 9b.4's icon: I look at it and accept it if it is clean and readable at 16 px; 10.1: a target still missed after profiling is recorded in `docs/PERFORMANCE.md` with the measurement and a Glutter issue where the cause is Glutter, and the work goes on; 10.2: everything is prepared, committed, and the version tag is **pushed** (you said "Push release tag, don't leave anything for me"); the version is `v0.1.0` (the first release of areas 1–2, not yet 1.0 since areas 3–11 are missing); 12.1: the plan is written and logged here, and building it waits for you.
7. **CLAUDE.md** now says "through the last one in the file" instead of "through the last one, 10.2", since the plan grew.

## 2026-10-25: step 9.7 (merge editor layout), decided by the agent and accepted by me

8. A file with conflict markers opened in the merge editor has its text **replaced by the merged result** (base plus the non-conflicting changes, conflicts empty or at base) as one undoable edit, left dirty; one Undo brings the markers back. A `--merge` result file keeps its existing text.
9. The **checkboxes** in the inputs' gutter are already clickable in 9.7 (VS Code shows them too); the code-lens-style accept buttons come with 9.8.
10. The **"This file has merge conflicts."** notice appears only when a file newly opens (not for restored backups or reopened tabs), with the button "Resolve in Merge Editor" (`git.openMergeEditor`, also in the palette).
11. Until 9.8, `emetos --merge ... --wait` ends with exit code 1 when the tab closes; 9.8 makes Complete Merge exit 0.

## 2026-10-25: step 9.8 (merge editor actions), decided by the agent and accepted by me

12. A **manual edit does not mark a conflict handled** (that is what VS Code's model does; the 9.7 SPEC text saying otherwise was corrected). Undo/redo of an accept gives the handled state back.
13. **Accept All** leaves conflicts that are already handled alone (VS Code overwrites them); **Reset Result** resets the conflicts only and keeps your edits outside them (VS Code rewrites the whole result).
14. After **Complete Merge** of a file with markers, the file opens in a normal text editor in the same group; for `--merge` the result is written and nothing else opens.
15. **Ctrl+Enter** completes the merge in the merge editor (VS Code's key); it wins over "Insert Line Below" only there.
16. The merge editor's labels show **file names only** (I sent the agent back to remove an absolute path from the Result header); the tab tooltip keeps the full path.

## 2026-10-25: Glutter v1.7.0

17. Glutter v1.7.0 (its steps 27.1–27.10) answers all twelve entries of `docs/GLUTTER-ISSUES.md`. I added step **9b.1b "Glutter 1.7 and the end of the workarounds"** after 9b.1 (9b.1 rewrites the same window code, so the upgrade waits for it): upgrade, replace each `// Glutter:` workaround with the new API, use the split startup log and the headless focus in `scripts/perf.sh`, remove the fixed entries and delete the issues file when it is empty.

## 2026-10-25: step 9b.1b (Glutter 1.7), decided by the agent and accepted by me

18. `docs/GLUTTER-ISSUES.md` is **not** empty after the upgrade: two entries stay, rewritten to what 1.7 still lacks — the tree cannot set a node's expansion from outside (`TreeSliverNode.SetExpanded` asked), and the cold start loses 560–605 ms to a failed Vulkan try before GLES (ask Glutter to try GLES first where Vulkan presents to no output).
19. The list and tab **drags keep Emetos's own controllers** even though 1.7's `Draggable` could now do it: the custom code already does what the tests pin (scrolling and auto-expand while the pointer rests, Escape, positions across groups); moving would only spread the state over the rows. Possible follow-up, not planned.
20. **Hovers hide on a press anywhere** now (RawTooltip's behavior; before only a press on the target hid them).
21. Tests keep the file watcher off by default (`ProcessOptions.WatchFiles`): the leak that forced it is gone, but a live watcher races with tests that stage on-disk changes.
