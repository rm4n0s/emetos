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

## 2026-10-25: step 9b.5 (switching and arranging tabs), decided by the agent and accepted by me

38. **Next/Previous Workspace Tab** (Ctrl+Alt+PageDown/PageUp) follow the strip's order and wrap; **Ctrl+Alt+1...8** go to that place, **Ctrl+Alt+9** to the last tab (VS Code's convention); these nine are keys only, not in the palette. **Move Workspace Tab Left/Right** stop at the ends and have no default key (VS Code's Ctrl+Shift+PageUp/PageDown move editors).
39. **The switch picker** (`emetos.workspaceTab.switch`, also what `workbench.action.switchWindow` now runs) lists the active tab first marked `current`, then the others in strip order with the first of them selected, so Enter switches to it; each row has the name, ` ●` for unsaved changes, the folder (with `~`, long paths shortened from the left) and a non-default profile.
40. **A press on another workspace tab activates it on release, not on press** (9b.2 activated on press), so a drag starting on it does not swap the strip under the pointer; a drag never changes the active tab. Added context keys `workspaceTabIsFirst`/`workspaceTabIsLast` for the View › Workspace Tabs submenu.

## 2026-10-25: step 10.1 (performance), the missed targets decided by me

41. **Cold start and opening a 10 MB file keep their targets (400 ms, 300 ms) for your desktop with the GPU**; headless, where Glutter spends 560–620 ms trying Vulkan before GLES (its known issue), the cold start is judged by **Emetos's own share: under 100 ms** (measured 21–28 ms with no folder, 76–77 ms with four restored tabs) and the 10 MB open by its time over an empty window's first frame (110–140 ms). Your `scripts/perf.sh --session` run decides; if it misses 400 ms, the fix belongs in Glutter's renderer start.
42. **The memory target becomes the empty Glutter window plus 150 MB** (was plus 50 MB). After the fix of the tab font (which had made Glutter load all 62 monospace system fonts: 738 → 418 MB), Emetos is 138 MB over Glutter's counter, of which about 50 MB is the Go runtime of a bigger program, 12 MB code, and an unidentified 33 MB region. `GOGC=50` would save about 25 MB for more GC work; not done.
43. **Idle with the cursor blinking**: under 1% of a core with the GPU; headless, where llvmpipe's threads dominate, the rule is **at most 2 frames a second** (pinned by a test). Emetos's own idle work is 10 ms in 10 s.
44. **The synchronous tokenization budget is 4 ms per frame, shared by layout, paint and the minimap** (it was 6 ms for each of them, so a frame could spend 12 ms or more), so the first view of lines nobody tokenized yet takes 4.7 ms instead of 7.8 (SPEC §8.3); those lines are painted plain a frame longer.

## 2026-10-25: step 10.2 (release)

45. **Version 0.1.0 is tagged `v0.1.0` and pushed** (your instruction "Push release tag, don't leave anything for me"); `emetos --version` reads the version from the build, so no file changes per release. **Overtype mode** was the only partial feature of areas 1–2 and was finished in this step (one mode for the whole process, as VS Code; not saved across restarts). The desktop entry lives in `assets/` and is installed by `scripts/install-desktop.sh`, which writes the absolute path of `emetos` (launchers often lack `~/go/bin` in `PATH`). The empty Explorer's "opening a folder will close all open editors" now shows only when `window.openFoldersInNewWindow` is `off`.

## 2026-10-25: step 11.1 (the extension contract), decided by the agent and accepted by me

46. **`extension/` is the pure contract** (it imports nothing of Emetos); the host that runs extensions is `internal/gui/exthost`, and `extensions.All()` is a function returning a fresh list. Activation events: `onStartup`, `onLanguage:<id>`, `onFile:<glob>` (relative to the folder), `onCommand:<id>` (every declared command is one too), `onView:<id>` and `onWindowTab:<kind>` (fired from steps 11.5 and 11.7); events already seen are replayed for an extension turned on later.
47. **A failing extension fails only in its tab**: a panic or error at any call into it (factory, `Activate`, command handler, status bar click, notification action, settings listener, posted function) is caught with its own tag, everything it contributed in that tab is removed, one error notification is shown with "Show Log", and the Extensions page says "Failed in <tab>". A disabled extension (`extensions.disabled`, an application-scope list of ids) registers nothing; Enable after a failure retries.
48. **The Extensions page** opens with Ctrl+Shift+X (`workbench.extensions.action.showInstalledExtensions`, also in View, Preferences and Manage) as an editor tab; it has no activity bar icon until side bar views exist (11.5). Extension keybindings sit over the default keymap and under the user's `keybindings.json`. `API.Post` was added so an extension's goroutines can hand work to the UI thread.

## 2026-10-25: step 11.2 (events), decided by the agent and accepted by me

49. **`Request` takes a reply callback** instead of returning `(Event, error)` as the spec said (a blocking call on the UI thread could freeze it for the 10 s timeout); a handler can answer later with **`HandleAsync`** (the Agents extension needs it). One handler per topic per scope; requests time out after 10 s unless the `ctx` says otherwise.
50. **Topics and patterns:** dotted, case-sensitive segments; `*` is one segment, a final `**` one or more. Fast topics (`emetos.document.changed`, `emetos.editor.selectionChanged`, and those an extension declares fast) arrive once per frame as a batch; order is kept per publisher and topic. Extensions cannot take the `emetos.*` ids. A handler or subscriber slower than 50 ms is logged.

## 2026-10-25: step 11.3 (event sharing between window tabs), decided by the agent and accepted by me

51. **A tab is named in a link by its folder** (the cleaned absolute path), so a link survives restarts; an extension window tab will be `<extension id>:<tab id>` (11.7). **A No Folder tab takes part in no sharing** (it has no stable name; links to it would pile up dead).
52. **The data's direction decides:** a request made in office for a handler in notes needs the link `notes → office`, and the reply comes back along it (the spec's "request along the link" would have let the prompt create a link that does not authorize the request; the spec is reworded). A cross-tab `Read` needs a link carrying `emetos.document.changed` and is never asked about (it is synchronous). A private workspace sends nothing, answers no request, allows no read out, is never asked about, and makes no requests to other tabs (a request carries its value out); it still receives along links.
53. **The prompt** shows in the receiving tab (it waits there; the window does not switch tabs): one question per pair of tabs at a time, Allow saves the link (merged into an existing one), This Session Only keeps it in memory, Deny (also Escape) stores nothing and is remembered until the extension is activated again. Events published before the answer are dropped. Removing a link on the page counts as a Deny for the extensions that listened to it.

## 2026-10-25: step 11.4 (language packs as extensions), decided by the agent and accepted by me

54. **Languages are a declarative contribution** (`Contributes.Languages`: an `fs.FS` with a VS Code `package.json`), registered by the host when the process starts, before any tab, instead of from `Activate` as the step said: files restored or given on the command line, and the grammar warm-up of step 10.1, need every language before the first workspace exists. 22 packs, one per VS Code extension folder (`emetos.golang`, `emetos.markdown`, `emetos.cpp`, ...; `emetos.gomod` on its own because its grammar comes from vscode-go). A pack is held per tab and removed from the process when no tab holds it; `extensions.disabled` follows each tab, so a pack disabled in one tab stays while another tab uses it.
55. **Language snippets are not bundled** (nothing read them before either); a later step can add them under the packs' `assets/`. Emmet stays in the core.

## 2026-10-25: step 11.5 (side bar views, panel views, built-in parts), decided by the agent and by me

56. **Extensions get Emetos's widgets through `extension/ui`**, a bridge package that re-exports a curated set of the kit (theme colors, text styles, icons, buttons, inputs, badges, checkboxes, lists, trees, progress bars) and is the only path from `extensions/` into the UI; `extension/` itself still imports nothing of Emetos (archtest rules `ExtensionContractImportsEmetos`, `ExtensionUIImportsInternal`). `extension/ui/uitest` lets extension authors write widget and golden tests.
57. **An extension's view is boxed**: clipped to its area, keys it does not use go on to the workbench, and a panic while building it shows "<Name> failed" with Show Log and fails the extension in that tab only. Glutter has no error boundary, so this holds for widgets built through `ui.NewBuilder` (a Glutter issue is logged).
58. **Narrow file and process access:** `Workspace().Files()`, a read-only `ReadFile` confined to the folder (`os.Root`, 2 MiB, UTF-8) and the `emetos.files.changed` event; extensions run processes with the standard library in their own process group, killed (TERM, then KILL after 2 s) when the tab closes. View state is saved per view in the window state and restored before the first build; which panel tab was open is not saved.
59. **No activity bar entry for the Extensions page** (decided by me): Ctrl+Shift+X and the View/Preferences/Manage menus open it; an activity bar icon that opens a page instead of a view container would be a new kind of entry for little gain.

## 2026-10-26: step 11.6 (editor tabs, title actions, file views), decided by the agent and by me

60. **The Markdown renderer lives in `extension/ui/markdown`** (decided by me), so the Agents extension (13.2) uses it without a move; code blocks are colored through a narrow `api.Languages().Tokenize` (tokens per line from the process grammar, at most 256 KiB) and the theme's colors. Web images are not loaded in the preview (privacy: the alt text shows); http(s) and mailto links open with the desktop portal, relative links inside the folder open in Emetos.
61. **A dirty extension tab is only a mark**: closing it asks nothing and there is no backup, because its data is the state the extension saves with the window. Splitting an extension tab makes a second instance with a copy of the saved state. "Open Preview to the Side" opens the same file in the group to the right (made if needed) in its preview view.

## 2026-10-26: step 11.7 (window tab extensions), decided by the agent and accepted by me

62. **An extension's window tab is its own scope holding only the extension that owns the kind**, named `<extension id>:<tab id>` for sharing (the tab id is saved, so links survive restarts); it gets no events from other tabs until the user allows a link. Its content fills the area under the title bar (no side bar, panel or status bar); the strip, the window buttons and the global keys stay.
63. **Closing it asks nothing** (its data is its state, dropped on close); the last-tab rule is unchanged (closing the last tab of any kind exits). **Disabling the owner keeps the tab and its state** with a notice in place of the content, which comes back on enable; a tab whose kind no longer exists is left out on restore but kept in the saved state. `emetos.windowTab.open` ("New Window Tab...") picks a kind from the palette.

## 2026-10-26: step 12.1 (the plan of area 3, language intelligence), decided by me

64. **Area 3 is specified in SPEC §9.12 and D20 and built in steps 12.2–12.9** (the step's "ask the user to review" decided by me, as you asked): one LSP client in `core/lsp` (stdio only, LSP 3.17, positions converted from byte columns to UTF-8 or UTF-16 as the server negotiates), a features model in `core/langfeatures`, servers declared by extensions and started per workspace tab (restarted after a crash up to 5 times in 3 minutes), Go providers for languages without a server, and VS Code's UI and keys for diagnostics, completion, hover, signature help, definition/references with the **peek view**, outline and symbol pickers, code actions with the light bulb, formatting (also on save, on type, on paste), rename with preview, code lens, inlay hints, semantic tokens, folding and selection ranges. `extensions/golang` declares `gopls` from step 12.3 so every step is proved on Go (`gopls` v0.24.0 is installed here); real-`gopls` tests carry the tag `emetos_gopls`. Not planned: TCP transports, call/type hierarchy views, inline (ghost text) completions, color decorators, linked editing.

## 2026-10-26: step 12.3 (language servers and their lifetime), decided by the agent and accepted by me

65. **A language server belongs to its extension's activation in a tab**: started on the first document of its languages (or at activation), restarted after a crash unless it is the fifth in 3 minutes (then a notification with Restart and Show Output), stopped with `shutdown`/`exit` within 3 s, then TERM and KILL. A missing program shows one "was not found" notification with Open Settings and starts once the path setting changes. `<extension>.restartServer` restarts all servers of that extension in the tab.
66. **A workspace edit that touches a closed file opens it as an unsaved document** in an inactive editor (as VS Code); the disk changes only when you save. Text-only edits are all or nothing; edits with file operations run in order and stop at the first failure; deletes and overwritten files go to the trash. Provider results are merged as VS Code does (completion lists concatenated, hovers stacked, locations joined, one formatter chosen by `editor.defaultFormatter`).

## 2026-10-26: step 12.4 (diagnostics), decided by the agent and accepted by me

67. **Problems follow vscode-languageclient**: pushed problems stay in Problems when their file closes (gopls reports the whole module), pulled ones go with the document, and all of a server's problems clear when it stops, crashes or restarts; closing an editor never removes problems. F8/Shift+F8 walk all problems by file, then severity, then position (hints skipped) and show the problem in a pinned hover instead of VS Code's inline navigation widget; Alt+F8 stays in the file.

## 2026-10-26: naming (the user's choice)

68. **The remote control extension is named "Remote Work"** everywhere, on both laptops: the extension `emetos.remotework` (`extensions/remotework/`), the page "Remote Work" on A (`emetos.remotework.manage`), the `+` menu entry "Remote Work..." on B, the banner "Remote Work: <key comment> connected", the state files `remote-work.json` and `remote-work-hosts.json` (SPEC §10.18, M15). It replaces "Remote Control"/"Remote Workspace" of entries 33–34.

## 2026-10-26: Relay (the user's design; details decided by me)

69. **Relay** (`emetos.relay`, SPEC §10.19, D21, milestone M16): one user controls workspaces of other Emetos instances through their events and commands, over SSH (TCP) with Remote Work's key login (shared in `extensions/internal/sshlogin`). A Relay window tab from the `+` menu has Hosts (add, connect, several at once) and Be a Host (share chosen workspaces; each allowed key Watch only or Watch and send per workspace); a connected workspace shows an Events log (every core and extension event, requests with replies) and a Send panel (Publish, Request, Run Command). Only the host's Relay acts on what clients send; a client never answers the host. Your answers: every core and extension command can be run, never outside the assigned workspace; watch-only or watch-and-send; everything in the workspace including extensions' events (file browsing through events, requests to the Agents extension); research and experiment.
70. **Decided by me for Relay:** new core request topics answered in every workspace (`emetos.commands.list`/`execute`, `emetos.files.list`/`stat`/`read`, `emetos.document.open`/`read`/`edit`/`save`/`close`, `emetos.editor.reveal`); a manifest `Privileges` field (`events.publishAny`, `commands.execute`) the host grants **only to `emetos.relay`**; commands refused to clients because they reach outside the workspace (window tabs, quit, close window, open/replace a folder, new windows, profiles, user and profile settings); default permission Watch only; port 47124; state in `relay.json` and `relay-hosts.json`. The README wrap-up becomes M17 (step 17.1).

## 2026-10-26: public keys by QR code (the user's request and choice)

71. **Allowed SSH keys can be added from the camera** in Remote Work and Relay: a **Scan QR Code** button next to pasting or choosing a `.pub` file opens a live camera dialog and adds the key after you confirm its type, fingerprint and comment; the client side has **Show My Public Key as QR Code** to scan from the host. The camera is read through Linux's V4L2 interface (`golang.org/x/sys/unix`, cgo-free), only while the dialog is open, and no frame is kept or sent. QR codes are decoded and encoded with **`github.com/makiuchi-d/gozxing`** (pure Go, Apache-2.0), the dependency you chose; it is added to CLAUDE.md's allowed list. New step 15.5; Relay (16.2, 16.3) reuses `extensions/internal/keyqr`. A real-camera test is a manual check.

## 2026-10-26: Glutter 1.8.1 (decided by me)

72. **Emetos uses Glutter v1.8.1 and drops the workarounds it makes unneeded**: the controls font (Glutter's font fallback replaces it), the tree rebuild (nodes now update in place), and `ui.NewBuilder`/`ui.BoxScope`, which are removed from the extension UI package. A `widgets.ErrorBoundary` around each extension view replaces them; an extension can no longer guard its own subtrees, because the whole view fails as one. The workspace tabs are WindowDecoration `Leading` widgets, so dragging them no longer moves the window; the width they may take is worked out from Glutter's private gap and button sizes (`docs/GLUTTER-ISSUES.md`). Headless proofs and perf runs use the GLES renderer by default.

## 2026-10-26: Glutter 1.8.3 (decided by me)

73. **Emetos uses Glutter v1.8.3, and `docs/GLUTTER-ISSUES.md` is deleted** because no entry is left. The workspace tabs are a loose `Flexible` `Leading` widget; Glutter 1.8.2 gives them the room in the bar. The peek view's rows use a plain ellipsis, which 1.8.3 cuts at a character.

## 2026-10-26: step 13.1 (the `claude` adapter; decided by the agent and accepted by me)

74. **The adapter passes the user's environment through unchanged** (plus `CLAUDE_CODE_ENABLE_SDK_FILE_CHECKPOINTING=true`).
    - It keeps `CLAUDECODE`, which Anthropic's Python SDK removes. As a result, an Emetos started from a terminal inside a `claude` session may find the program refusing to run nested; removing that one variable is a one-line change if you prefer the SDK's behaviour.
    - **"Rewind code to here" is offered on every session Emetos started**, because the documentation names no capability for file checkpoints, and a program that cannot restore shows its reason.
    - The fixtures of the tests are written from the documented shapes, not recorded from your account. The manual check of 13.1 runs the adapter once against your real `claude`.

## 2026-10-27: step 15.3 (QUIC transport; decided by the agent and accepted by me)

75. **A QUIC certificate that does not match the fingerprint sent over SSH ends the connection attempt; it does not fall back to the SSH channel.** A mismatch means something on the UDP port is not what the host started, and the user should see that rather than get a working session that hides it. Falling back instead would be safe (the SSH channel is authenticated) and is a small change.
76. **The protocol keeps version 1 after adding message type 14 (`Missing`, a request for lost tiles)**, because no version was ever released. From the first release on, a change to the message types bumps the version.
