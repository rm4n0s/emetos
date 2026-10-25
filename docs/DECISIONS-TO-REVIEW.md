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

## 2026-10-25: panel views for extensions (the user's request)

22. Extensions get a fourth UI contribution, **panel views**: a tab in the bottom panel (beside Problems and Output), per workspace tab, with no tie to files, with title actions, a badge, state kept across panel tab switches and restored when the extension saves it, a "Toggle <Name>" command and a View menu entry; the same containment, `kit`/theme, API-only and events-only rules as the other kinds. Added to SPEC §10.15 and step 11.5 ("Side bar views, panel views and file views"); its first real extension is `extensions/tasks`, a panel that runs a command in the workspace folder (e.g. `go test ./...`) and streams the output.

## 2026-10-25: extension UI completed (the user's requests)

23. The extension UI kinds are now seven (SPEC §10.15 table): **window tabs**, **side bar views**, **panel views** (bottom panel: terminal, tasks, logs), **editor tabs** (tabs in the editor area with no file, several instances: an agent's sessions), **editor title actions** (micro icons at the right of the editor tab strip: Claude Code, opencode, ... each opening a new session tab), **file views** (micro-tabs inside a file's tab) and **contributions to the built-in parts** (Output channels, Problems markers, status bar). One extension can contribute any number of each. This replaces entry 4's "extension tabs are window tabs only" and entry 22. Steps: 11.5 (side bar views, panel views, built-in parts; `extensions/todo`, `extensions/tasks`), 11.6 (editor tabs, editor title actions, file views; `extensions/markdown`, `extensions/notes` as the agent-style example), 11.7 (window tabs; `extensions/scratchpad`). Moving a view between side bar and panel, and a secondary side bar, are not planned.

## 2026-10-25: network access allowed (the user's request)

24. The rule "never import `net/http`; `net` only in `internal/core/ipc`" is removed from CLAUDE.md, so features and extensions may use the network (the Claude Code integration channel, remote workspaces, future HTTP clients). What I kept, unless you say otherwise: **no telemetry, crash reports or update checks** of Emetos's own (D4 and §12 become "no telemetry" instead of "no network"). Done after step 9b.3: G2, D4, §5.3 and §12 rewritten, the archtest rules `NetworkImport` and `NetOutsideIPC` and their testdata removed.

## 2026-10-25: step 9b.3 (launches open tabs), decided by the agent and accepted by me

25. Open Folder, Open Recent and the empty Explorer's button now follow `window.openFoldersInNewWindow` like a launch (default: **a new tab**); 7.8's rule "a folder picked inside the app replaces the window" is gone. Use `-r`, the setting `off`, or Close Folder to stay in the same tab.
26. Files open in the **active tab** (not the tab whose folder contains them, as in 7.8). `window.openFilesInNewWindow` `on` opens a new No Folder tab for them; `default` behaves like `off`.
27. `--profile` with a folder that is not open opens a new tab only when the active tab lacks that profile (an untouched No Folder tab is replaced instead), as §10.12 and VS Code do, rather than always a new tab.

## 2026-10-25: step 9b.4 (icon, tray, close to tray), decided by the agent

28. **The icon** is a rounded indigo square (gradient `#7467F7` to `#4A3DD4`, a 1 px darker border) with a white `</>` mark, the symbol of the home page; its files are `assets/icons/emetos-16.png` ... `emetos-512.png`. The step's "ask the user to look at it" was decided by me: I looked at every size on light and dark panels and at the small sizes magnified (16 px reads as `</>`; the mark is drawn a little wider at small sizes so that its parts do not run together). Look at `assets/icons/emetos-256.png` and `emetos-16.png`. Changing it later is a change of `kit/appicon.go` and `go test ./internal/gui/kit -run TestAppIconPNGs -update-icons`.
29. **`assets/icons` is a Go package** (it only embeds the PNG files), the one outside `cmd` and `internal`, because `go:embed` cannot reach a folder above its package and the files must live in `assets/icons/` for the icon theme folders of step 10.2 (SPEC §5.2).
30. **The tray uses the pictures drawn at 16, 22, 24, 32 and 48 px** instead of scaling one picture with `TrayIconFromRGBA` (the same five sizes): the pictures drawn at their own size are sharper at 16 px.
31. **The window is hidden after a frame that shows "Show Emetos"** in the tray menu (a hidden window gets no frames, so the menu could not change afterwards); the backups are written before that. The tray counts as shown until Glutter reports a tray error (a few ms after the start); an error that arrives after the window was hidden shows the window again.
32. **Click on the icon** hides the window only when it is in front; Hide Emetos / Show Emetos in the menu always toggles.

## 2026-10-25: remote control (the user's design and answers)

33. **Remote control of one workspace** between two Emetos instances (milestone M15): A turns it on per workspace with a port and a list of allowed SSH public keys, kept in `~/.local/state/emetos/remote-control.json` and edited only through Emetos's "Remote Control" page; A listens only while it is on, with an idle timeout (30 minutes) and a visible "connected" banner with Disconnect. B opens a "Remote Workspace..." window tab with host, port and private key. B gets **its own view** of the workspace at its tab size (own cursor and scroll, same files), not a mirror of A's screen.
34. **Transport (the user's choice): QUIC over UDP by default, SSH for login.** B logs in over an embedded SSH server on A (TCP, `golang.org/x/crypto/ssh`: only listed keys, no shell, host key pinned like `known_hosts`, keys read directly with passphrase and `ssh-agent` support); inside that authenticated channel A gives B a long random session token and the fingerprint of A's QUIC certificate; B then opens **QUIC** (UDP, `github.com/quic-go/quic-go`, TLS 1.3 inside) on the same port number with the token, so the QUIC certificate is checked against the fingerprint learned over SSH. Picture tiles go as QUIC **datagrams** (latest picture wins, losses repaired by the next frame and a periodic refresh); input, clipboard and notifications go on reliable QUIC streams. If UDP is blocked, the session falls back to channels of the SSH connection (TCP). Both dependencies were approved by the user and added to CLAUDE.md's allowed list.

## 2026-10-25: the three requested extensions planned (M13–M15)

35. **The Claude Code extension is the "Agents" extension** (`extensions/agents`, SPEC §10.16, M13). Anthropic's terms forbid naming a third-party product "Claude Code" or using its logos, so the extension and its parts are named "Agents" and its first adapter "Claude CLI"; its tooltip says in plain text that it "runs Claude Code". It runs **your own, unmodified `claude`** with your own login (subscription or key): never `--bare`, never a separate config folder, and Emetos never reads, stores or passes a token; "Log in" runs `claude auth login`. The control messages (permissions, interrupt, model) are not formally documented, so the adapter detects features from what the program reports and is tested against pinned CLI versions. **For you:** Anthropic's terms allow "an end user signing in to the unmodified Claude Code binary with their own subscription", but they do not say outright whether an editor driving it counts as "offering claude.ai login"; for certainty Anthropic suggests asking them in writing. The terminal `claude`'s editor-integration server (the IDE lock files) is not built, since the session tab does not need it.
36. **Extensions get two new `extension/` calls**, because extensions import only `extension/`: `api.Editors().OpenProposedChange` (the diff editor for an agent's proposed edit, with Accept/Reject and the edited text returned) and `api.Workspace().NewHeadlessView(size)` (a second, off-screen view of a workspace for remote control). The Markdown renderer moves from `extensions/markdown` to `extension/ui/markdown` so the Agents extension can use it.
37. **Order:** M13 Agents (13.1–13.3), M14 Go (14.1–14.3, after M12's language-server steps, which step 12.1 writes) and M15 Remote control (15.1–15.4) come after M12. Running the real `claude` and the real `gopls` are tests behind build tags (`emetos_claude`, `emetos_gopls`), and the real `claude` one is a manual check because it uses your account.
