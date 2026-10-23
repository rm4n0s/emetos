# Emetos: Specification

Emetos is a code editor in the spirit of Visual Studio Code, written in Go on the [Glutter](https://github.com/rm4n0s/glutter) GUI toolkit, for the Linux desktop. This file is the source of truth: when the code and this file disagree, one of them is wrong, and the user decides which.

The work is split into the eleven areas of VS Code listed in §3. **This version of the spec covers areas 1 (the editor) and 2 (the workbench).** The other areas are listed so that the architecture leaves room for them, and get their own sections when they are planned.

"VS Code" below means VS Code 1.143.0, the version pinned in `scripts/reference-versions.txt` (§17). Where this spec says "as VS Code", VS Code's source at that version is the definition, and this spec's description is a summary.

---

## 1. Goals

| # | Goal |
|---|---|
| G1 | **What VS Code has.** Areas 1 and 2 (§3) match VS Code's behavior, defaults, command ids and Linux keyboard shortcuts closely enough that a VS Code user feels at home. When in doubt, do what VS Code does |
| G2 | **Private.** No telemetry, no crash reporting, no update checks, no network access of its own (§12) |
| G3 | **Native and light.** One Go binary, no Electron, no Node.js, no cgo. Starts fast, stays responsive on large files, uses no CPU when idle (§14) |
| G4 | **Compatible file formats.** VS Code's color themes, file icon themes, TextMate grammars, language configurations, snippet files, `settings.json` and `keybindings.json` formats work, so existing material can be reused |
| G5 | **Core and UI apart.** The editor core never imports Glutter. It is tested without a display, and the UI only shows and edits its state (§5) |
| G6 | **Testable.** Every feature has tests; UI features have widget tests and golden images that run without a display (§15) |

## 2. Non-goals

- Running VS Code extensions or a Node.js extension host.
- Windows, macOS, the web. Linux desktop only (Wayland and X11 through Glutter).
- Areas 3–11 of §3, until they are planned. Their hooks are listed where they matter (for example, the Problems panel exists but has no producers until area 3).
- Microsoft's proprietary features: Marketplace, Settings Sync with a Microsoft/GitHub account, Live Share, Copilot.
- Workspace trust (it guards extensions and tasks, which don't exist yet).
- Multi-root workspaces (`.code-workspace` with several folders). One window opens one folder or loose files.
- Vim/Emacs emulation (in VS Code they are extensions).
- Moved-code detection in the diff editor, and the accessible diff viewer.

## 3. Areas

| # | Area | Status |
|---|---|---|
| 1 | **The editor**: editing, multi-cursor, find/replace, highlighting, brackets, folding, minimap, sticky scroll, breadcrumbs, undo, auto-save, hot exit, snippets, Emmet, diff and merge editors, encodings, line endings, large files | **this spec** |
| 2 | **The workbench**: layout, editor groups and tabs, explorer, command palette, quick open, status bar, notifications, Output and Problems panels, zen mode, windows, themes, keybindings, settings and their UIs, profiles | **this spec** |
| 3 | Language intelligence through LSP | later |
| 4 | Project-wide search and replace | later |
| 5 | Source control (git) | later |
| 6 | Run and debug through DAP | later |
| 7 | Integrated terminal | later |
| 8 | Tasks | later |
| 9 | Remote development | later |
| 10 | Notebooks, testing UI, multi-root workspaces, settings sync | later |
| 11 | Extensions | later |

### 3.1 Area 1 in detail

| Feature | Behavior |
|---|---|
| Editing | Typing, overtype mode, delete/backspace by character and word (and word part), new line with auto-indent, indent/outdent, tab size and spaces/tabs per file, detect indentation from content |
| Line commands | Move line up/down, copy line up/down, delete line, insert line above/below, join lines, transpose, sort lines ascending/descending, trim trailing whitespace, change case (upper, lower, title), toggle line comment, toggle block comment |
| Cursors and selections | Multiple cursors (Alt+Click, Ctrl+Alt+Up/Down... as VS Code's Linux keymap), add next occurrence (Ctrl+D), select all occurrences (Ctrl+Shift+L), column selection (Shift+Alt+drag and keyboard), expand/shrink selection (by word, string, brackets, line), cursor undo (Ctrl+U), primary selection on X11 and Wayland |
| Find and replace | Find widget with regex, match case, whole word, find in selection, preserve case, replace one/all, match count, highlight all matches, seed the search from the selection |
| Highlighting | TextMate grammars (§8.3), VS Code color themes (§8.4), current line highlight, selection and word highlights |
| Brackets | Matching bracket highlight, jump to bracket, bracket pair colorization, bracket pair guides, auto-closing and auto-surrounding pairs, auto-closing quotes, over-typing an auto-inserted closing character |
| Indentation | Indent guides (active guide highlighted), indentation rules and on-enter rules from language configurations, convert/reindent commands |
| Folding | Fold by indentation and by `#region` markers (§8.7); fold/unfold one, recursively, all, by level; folding gutter controls |
| Display | Line numbers (absolute, relative, off), rulers, word wrap (off, on, at column, bounded), render whitespace (none, boundary, selection, trailing, all), render control characters, cursor styles and blinking modes, smooth scrolling, font size and zoom, font ligatures on/off, letter spacing, line height |
| Minimap | Rendered blocks of the text with token colors, viewport slider, click and drag to scroll, show on the right or left, scale 1–3, hide setting (§9.5) |
| Sticky scroll | The headers of the folding ranges that contain the top line stay at the top of the editor (up to 5 lines); click jumps to the header |
| Breadcrumbs | The file path above the editor, each segment a picker of its siblings. Symbols come with area 3 |
| Undo/redo | Per document, grouped as VS Code groups edits, with cursor state restored. Survives save, not restart |
| Saving | Save, Save As, Save All, Revert; auto-save modes `off`, `afterDelay`, `onFocusChange`, `onWindowChange`; trim trailing whitespace, insert final newline and trim final newlines on save |
| Hot exit | Unsaved changes of every editor, including untitled ones, are backed up and restored on the next start (§10.10) |
| Snippets | VS Code snippet syntax (tab stops, placeholders, choices, variables, transforms, nested); user snippet files (global and per language); insert snippet command; tab-completion of snippet prefixes (`editor.tabCompletion`) |
| Emmet | HTML and CSS abbreviation expansion with the "Emmet: Expand Abbreviation" command, and with Tab when `emmet.triggerExpansionOnTab` is on |
| Diff editor | Side-by-side and inline views, character-level changes, ignore trim whitespace, next/previous change (Alt+F5, Shift+Alt+F5), compare two files, compare with saved, read-only original (§9.11) |
| Merge editor | Three-way merge (incoming, current, result panes) of a file with conflict markers or of `--merge` inputs, accept incoming/current/both per conflict, complete merge (§9.11) |
| Files | Encodings (§6.3), line endings LF and CRLF (detect, convert), BOM, read-only files, files changed on disk (reload or compare when dirty), large files (§6.4), binary files (refused with a notice) |

### 3.2 Area 2 in detail

| Feature | Behavior |
|---|---|
| Layout | Menu bar, activity bar, primary side bar, editor area, panel (bottom, or left/right), status bar; each part can be hidden, and sizes are remembered (§10.1) |
| Editor groups | Split editors right/down, any number of groups in a grid, move/copy editors between groups, focus groups by number, close, maximize a group, join groups |
| Tabs | Preview editors (italic, replaced by the next preview), pinned tabs, dirty indicator, close button, close others/to the right/saved/all, reorder and move by drag, scroll when they overflow, reveal in explorer, copy path |
| Navigation | Back/forward history across editors (Ctrl+Alt+- / Ctrl+Shift+-, VS Code's Linux keys), go to line (Ctrl+G), switch editors (Ctrl+Tab), quick open (Ctrl+P) with recent files and fuzzy file search, command palette (Ctrl+Shift+P) |
| Explorer | Folder tree with file watching, `files.exclude`, new file/folder, rename (F2), delete to trash, cut/copy/paste, drag to move, drop files from the file manager, compact folders, reveal active file, Open Editors section, collapse all |
| Status bar | Line/column and selection count, indentation, encoding, line endings, language mode (each one opens its picker), notifications bell, problem counts |
| Notifications | Info, warning and error toasts with actions, notification center, do-not-disturb |
| Panels | Output (channels: the Emetos log, and more later), Problems (empty until area 3; the model exists) |
| Zen mode and full screen | Zen mode hides everything but the editor; full screen; centered layout |
| Windows | New window, open folder or files in a new or the same window, single instance per user (`emetos <path>` reaches the running instance), window state and editors restored on restart |
| Themes | Color theme picker with preview, follow the system's dark/light mode, file icon theme (§10.8) |
| Settings | Default/user/workspace/language-specific layers; `settings.json` (JSON with comments); settings UI with search and categories that edits the JSON and keeps comments |
| Keybindings | VS Code's Linux keymap (§11.3); `keybindings.json`; keybindings UI with search by name or by key, record a key, change/remove/reset |
| Profiles | Named sets of settings, keybindings, snippets and UI state; create, rename, switch, delete, export/import as a file (§10.12) |

## 4. Decisions log

| # | Decision | Why |
|---|---|---|
| D1 | Go, Glutter, Linux desktop only | The user's choice; Glutter is the user's toolkit |
| D2 | No cgo, ever | Glutter is cgo-free; `CGO_ENABLED=0 go build` keeps the build to Go alone |
| D3 | The core (`internal/core/...`) never imports Glutter; the UI (`internal/gui/...`) imports the core (§5.2) | Testable core, a UI that can change |
| D4 | No telemetry and no network access of Emetos's own (§12) | G2 |
| D5 | Language intelligence comes only from LSP and debugging only from DAP (areas 3 and 6) | One mechanism for every language |
| D6 | Text is UTF-8 in memory; positions are zero-based lines and byte columns; visual columns are computed for display (§6.2) | Matches Glutter's text API; LSP 3.17 can negotiate UTF-8 positions, and UTF-16 is converted when a server can't |
| D7 | VS Code's file formats for themes, icon themes, grammars, language configurations, snippets, settings and keybindings (G4) | Reuse of existing material |
| D8 | When Glutter lacks something, Emetos works around it, marks the code with `// Glutter:`, and lists it in `docs/GLUTTER-ISSUES.md` (which exists only while there are open issues) | The user's rule (CLAUDE.md) |
| D9 | Every workbench widget, including the menu bar and menus, is Emetos's own (`gui/kit`) on Glutter's `widgets` layer, themed by VS Code workbench colors. No Material widgets | A dense IDE look, one look for every menu |
| D10 | Glutter v1.4.0 or newer: fonts loaded at run time (`RunAppOpts.Fonts`, `PlatformDispatcher.LoadFontFromList`), fonts in `gluttertest` (`Opts.Fonts`), family fallback in order, font queries and the desktop's font settings, `TreeSliver`, tab stops (`ParagraphStyleOpts.TabSize`), the cache of shaped runs | What the editor needs |
| D11 | Regular expressions with `github.com/dlclark/regexp2` (§8.2) | TextMate grammars need lookbehind and backreferences |
| D12 | Workspace settings only in `<folder>/.emetos/settings.json`; `.vscode/` is not read | The user's choice |
| D13 | Bundled languages: the list in §8.1. Go, Markdown, JSON, YAML, Python, JavaScript and TypeScript come first and get the deepest tests | The user's priorities |
| D14 | Fonts: the editor uses the desktop's monospace font (`MonospaceFont()`), then `monospace`; the workbench uses the desktop's UI font (`UIFont()`), then `sans-serif`. Codicons and the Seti icon font are bundled and loaded through `RunAppOpts.Fonts`. Tests use Go Mono (from `golang.org/x/image/font/gofont`) through `gluttertest.Opts.Fonts` | Looks like the desktop; deterministic tests with no extra dependency |
| D15 | Debug-only **dev commands** (`EMETOS_DEV_COMMANDS`, §15.2) run commands after the first frame, to reach states for proof screenshots and performance runs. They exist in debug builds and in builds with the `emetos_dev` tag, never in release builds | Proofs and measurements without a mouse |
| D16 | Users install release builds: `go install -tags glutter_release github.com/rm4n0s/emetos/cmd/emetos@latest` | Glutter's debug builds are 8–10 times slower in the framework |

## 5. Architecture

### 5.1 Processes, windows and threads

Emetos is one process with one or more windows. Glutter's UI goroutine owns every widget and render object.

**Per process**: settings registry and default/user layers, command registry, default keymap, theme service, language and grammar registries, the notification and log sinks, `core/ipc`, the profile in use by default.

**Per window** (a `Window` value created by `gui/app` for each Glutter window): the folder (workspace) or none, the workspace settings layer, the root context key scope, the documents (the same file open in two windows is two documents, as in VS Code's separate windows), editor groups, the file watcher, UI state, backups, the profile in use by that window.

Threads:
- **Documents** are changed only on the UI goroutine. Every change produces a new immutable **snapshot** of the buffer (§6.1) that background goroutines read freely.
- **Background work** (tokenizing, folding ranges, wrapping, file watching, fuzzy file search, diffing, backups) runs on goroutines with a `ctx`, reads snapshots, and posts results to the UI goroutine through `WidgetsBinding.Post` or `State.SetState`. A result for an older document version is dropped.
- **Child processes** (language servers, debug adapters, terminals) come with later areas and follow the same rule.

### 5.2 Package layout

```
cmd/emetos/                    main: command line, single instance, app start
internal/core/                 no Glutter import anywhere below (checked by internal/archtest)
  buffer/                      positions, piece tree, snapshots, line index (§6.1, §6.2)
  fileio/                      read/write, encodings, line endings, atomic save, trash (§6.3)
  document/                    document = buffer + metadata + undo + decorations (§7.1)
  edit/                        columns, boundaries, selections, cursor movement, editing commands (§6.2, §7.2)
  vstest/                      test helpers: VS Code positions (1-based, UTF-16) to Emetos positions (§15.1)
  glob/                        VS Code glob patterns, and .gitignore matching (§10.5, §10.6)
  language/                    language registry, language configurations, bundled grammar assets (§8.1)
  regex/                       Oniguruma-style regular expressions for grammars, ECMAScript mode for find (§8.2)
  textmate/                    grammar loading and line tokenizer (§8.3)
  tokens/                      per-document tokenization state, background tokenizer (§8.3)
  theme/                       color themes, token colors, workbench color registry (§8.4)
  brackets/                    bracket pairs, matching, colorization (§8.5)
  indent/                      indentation rules, detection, on-enter rules, guides (§8.6)
  folding/                     folding range providers and model (§8.7)
  find/                        find/replace model (§9.8)
  snippet/                     snippet parser, variables and session (§9.9)
  emmet/                       Emmet abbreviations (§9.10)
  diff/                        line and character diff, three-way merge model (§9.11)
  jsonc/                       JSON with comments: parse, and edit keeping comments (§11.1)
  plist/                       XML property lists, the format of TextMate themes and of one grammar (§8.1, §8.4)
  settings/                    settings registry, layers, change events (§11.1)
  contextkey/                  context keys and when-clause expressions (§11.2)
  command/                     command registry (§11.2)
  keybinding/                  keys, keymap, chords, resolver, default Linux keymap (§11.3)
  editorgroup/                 editor inputs, editor groups and their grid, as data (§10.3)
  workspace/                   folder, file tree model, watcher, file operations (§10.5)
  fuzzy/                       fuzzy matching and scoring (§10.6)
  storage/                     paths, UI state, backups (hot exit), profiles (§10.10, §10.12)
  ipc/                         single instance over a Unix socket (§10.11)
  logs/                        log channels shown in the Output panel (§13)
  notify/                      notification model (§10.7)
internal/gui/                  imports Glutter and the core
  app/                         app start, windows, wiring of the services, dev commands (§5.1, §15.2)
  kit/                         the IDE widget kit: theme, icons, split view, tree, list, tabs, inputs, buttons, menus, hovers, dialogs (§10.2)
  kit/fonts/                   bundled icon fonts and their family names
  editor/                      the editor widget (§9)
  diffeditor/, mergeeditor/    §9.11
  workbench/                   layout parts, editor group views, title/menu bar, activity bar, status bar (§10)
  quickinput/                  command palette, quick open, pickers (§10.6)
  views/explorer/, views/output/, views/problems/
  settingsui/, keybindingsui/  §11
  testutil/                    widget-test helpers: fonts, isolated paths, proof images (§15)
internal/archtest/             tests of package boundaries and forbidden imports (§5.3, §12)
third_party/                   licenses of bundled assets (§17)
scripts/                       fetching references, copying assets, proofs, headless runs, performance
```

Directories hold a `doc.go` only when they hold Go code.

### 5.3 Boundary rules (checked by `internal/archtest`)

- No package under `internal/core/` imports `github.com/rm4n0s/glutter/...` or `internal/gui/...`.
- No package imports `net/http`, `net/rpc` or `crypto/tls`. `net` is allowed only in `internal/core/ipc` and, later, in the packages that talk to language servers and debug adapters.
- No `import "C"`.
- `github.com/dlclark/regexp2` only in `internal/core/regex`.
- No import of the standard library `errors`, and no call of `fmt.Errorf` (CLAUDE.md "Error handling").

### 5.4 Key handling

One **key dispatcher** per window, a `Focus` node at the root of the window's widget tree, resolves every key event through `core/keybinding` against the context key scope of the focused widget (§11.2), so a shortcut works wherever focus is (editor, explorer, quick input). Widgets below it handle a key themselves only for text entry. A key with no binding is left unhandled, and Glutter types its character into the focused text input client. Modifier-only key events never cancel a pending chord.

**Implementation (step 3.5).** `app.KeyDispatcher` sits above the app in the window's tree: it registers an early key handler of the focus manager, so a bound key is used before focus traversal and before the focused widget; `ChordOf` turns a key event into a chord (`KeyOf`: a Latin letter or digit comes from the logical key, any other key, and a letter on a layout without Latin letters, from the name of the physical key on a US keyboard); `Window.DispatchKey` resolves it with the window's `Dispatcher` in the context key scope of the focus node (`kit.FocusScopes` maps nodes to scopes; the nearest ancestor with a scope counts, the window's root scope otherwise), runs the command and reports whether the key was used. Key ups dispatch nothing; a press of a modifier alone keeps a pending chord; a chord's status is passed to `Window.OnChordStatus` (logged until the status bar shows it). A command that fails is logged; the key counts as used. The editor (`editor.EditorOpts`) makes its own scope under the window's with `editorTextFocus`, `editorFocus`, `textInputFocus`, `editorHasSelection` (any selection with text), `editorHasMultipleSelections`, `editorColumnSelection`, `editorReadonly`, `editorTabMovesFocus` (false) and `editorLangId`, and binds the handlers of its commands (`editor.BindCommands`, which uses the registry's handler stack) while it has the focus and unbinds them when it loses it.

**Text input (step 3.6).** The editor connects to the platform's text input (`services.TextInputConnection`) when its focus node gets the focus and closes it when it loses it. `editor/input.go`: the client speaks the delta model and gives the input method the current line of the primary cursor (a selection over several lines is a caret at its active end; the value is given again whenever the line, its text or the selection differs, which moves the input method's window with the cursor). A change of the input method is the difference between the line it was given and the text it holds apart from the composing range, found as one replaced stretch aligned to characters; it is typed at every cursor (`Commands.Type` for a stretch that is the selection, `CompositionType` with the characters before and after the cursor for a replacement around it). The composing range is the view model's `Preedit`: drawn above the text at the primary cursor, on the editor's color, with a line below it and a thicker one below the part the method highlights, with the cursor inside it; it is never in the document or the undo history, and it ends when the editor loses the focus. After each frame in which the editor moved, scrolled or changed, `SetEditableSizeAndTransform` and `SetCaretRect` tell the method where the editor and its caret are (the caret rect is the cursor, or the method's cursor inside the preedit): Glutter's `SetComposingRect` does not reach the platform. Characters of keys that no binding takes reach the editor through Glutter's `HandleUnhandledKey` (a key with a character commits it; Enter commits "\n", typed as a new line with the indentation of the line; keys with Control, Alt or Meta type nothing).

## 6. Text core

### 6.1 Buffer (`core/buffer`)

- A **piece tree**: a balanced tree of pieces pointing into read-only original chunks and an append-only add buffer, using VS Code's `PieceTreeTextBuffer` algorithms. Each node keeps the byte length and the number of line feeds of its subtree.
- **Persistent**: nodes are never changed after they are shared; an edit copies the path from the root to the changed nodes (no parent pointers). `Snapshot()` returns an immutable view in O(1), safe to read from any goroutine.
- Operations: insert, delete, replace at byte offsets; `LineCount`, `LineContent(n)`, `LineLength(n)`, `OffsetAt(Position)`, `PositionAt(offset)`, `Text(Range)`, iteration over chunks and lines. All are O(log n) in the number of pieces plus the size of the result.
- The buffer holds only `\n` line endings: `fileio` converts `\r\n` and lone `\r` when it reads (§6.3), and the document remembers the file's line ending for saving.

### 6.2 Positions and columns

- `Position{Line, Col int}`: zero-based line and **byte** column. `Range{Start, End Position}`. `Selection{Anchor, Active Position}`.
- **Visual columns** (column selection, rulers, `editor.tabSize`, vertical cursor movement, the status bar's "Col") follow VS Code's `CursorColumns`: walk the grapheme clusters of the line; for each cluster, take its first code point: a tab advances to the next tab stop, a full-width character or an emoji counts 2, anything else 1. The status bar's "Col" is not the visual column: as VS Code's `toStatusbarColumn`, it counts every code point as 1 (a tab goes to the next tab stop) and adds one, so a wide character or an emoji counts 1 there.
- Grapheme clusters (go-text's segmenter, through Glutter's dependencies) bound cursor movement and deletion.

### 6.3 Files (`core/fileio`)

- Reading returns the text (line endings converted to `\n`), the encoding, the BOM flag, the line ending (`\n` or `\r\n`; mixed endings become the most common one and are reported), and a `FileStat` (modification time, size, mode). Writing returns the new `FileStat`, so the watcher can ignore Emetos's own writes (§10.5).
- Encodings: UTF-8, UTF-8 with BOM, UTF-16 LE/BE (BOM), and the encodings of VS Code's `files.encoding` list that `golang.org/x/text/encoding` has; the others are left out of the list. `files.autoGuessEncoding` (off by default, as in VS Code) tries, in order, a BOM, valid UTF-8, then `files.encoding`; VS Code's statistical guessing (jschardet) is not done.
- Saving is atomic: write to a temporary file in the same folder, `fsync`, rename over the original, keep the original mode. When the folder isn't writable, write in place. A read-only file fails with a tagged error; "Overwrite" adds the owner's write permission (`chmod u+w`) and saves, as VS Code does.
- Binary detection, as VS Code's `detectEncodingFromBuffer`: a byte order mark decides the encoding; without one, the first 512 bytes are looked at: if every other byte is a NUL the file is UTF-16 (little or big endian), which is true of Latin-1 text and not of other scripts (those open with "Reopen with Encoding"); any other NUL byte means binary. Binary files open as a notice with "Open anyway".
- Delete moves to the freedesktop.org home trash (`$XDG_DATA_HOME/Trash`, with `.trashinfo` files); a file on another filesystem than the home trash can't be trashed, and the caller asks to delete permanently. A trashed item can be restored (used by undo in the explorer).

### 6.4 Large files and long lines

| File size | Behavior |
|---|---|
| < 20 MB and < 300,000 lines | Everything on |
| 20 MB – 256 MB, or ≥ 300,000 lines | "Large file mode" (`editor.largeFileOptimizations`): no tokenization (plain text), no bracket colorization, no folding, no minimap, no word wrap, and a notice saying so. The decision is made when the document opens and stored on the document; every feature checks it |
| > 256 MB | Refused with an error notice (VS Code's default limit) |

Long lines, as VS Code: a line is drawn up to `editor.stopRenderingLineAfter` (10,000 characters) and the rest is not drawn (cursor positions beyond it are computed with the monospace advance); tokenization of a line stops after `editor.maxTokenizationLineLength` (20,000 bytes). Word wrap does not wrap a line longer than `editor.stopRenderingLineAfter`.

## 7. Documents and editing

### 7.1 Document (`core/document`)

- A document has: a URI, the buffer, a version id (incremented by every change), an **alternative version id** (as VS Code's: incremented by a change, and restored by undo/redo to the value it had at that point), the saved alternative version id, language id, encoding, line ending, BOM flag, indentation options (`tabSize`, `insertSpaces`, detected or from settings), large-file flag, read-only flag, and the `FileStat` of the last read or write.
- **Dirty** means the alternative version id differs from the saved one, so undoing back to the saved state clears it.
- URI schemes: `file:` (a path), `untitled:<n>` (the lowest free number; title "Untitled-n"), and `emetos-virtual:` (read-only generated content, such as the default settings JSON or the saved version of a file for "Compare with Saved").
- **Edits** are applied as a batch of non-overlapping replacements; the document returns the inverse batch.
- **Undo/redo** stack of edit groups, as VS Code's `EditStack`: a group stores the edits, their inverses, and the selections before and after; consecutive edits of the same kind (typing, deleting left, deleting right, other) from the same cursors merge into one group, and a cursor move by other means, a different kind, or an explicit `PushStackElement` closes the group. Typing also follows VS Code's rule for spaces: a space typed after other characters starts a new group, spaces typed one after the other stay together, and the characters typed after a single space join it. Undo and redo restore the selections.
- **Decorations**: ranges that move with edits, with VS Code's four stickiness modes, kept in an interval tree; used for find matches, bracket highlights, snippet tab stops, auto-closed characters, diff changes, folded regions and, later, diagnostics.
- Change events carry the version, the edits and the new snapshot.

### 7.2 Editing commands (`core/edit`)

Pure functions from (snapshot, cursors, options) to (edit batch, new cursors, edit kind), so they are tested without a document or a UI. A cursor whose edits overlap the edits of an earlier cursor is dropped from the result, as in VS Code. They cover every row of §3.1 under "Editing", "Line commands" and "Cursors and selections". One options struct carries everything they read (tab size, insert spaces, word separators, auto-closing, indentation rules...), so later features add fields, not parameters. Word boundaries follow `editor.wordSeparators` (VS Code's default) and word parts split camelCase and snake_case.

Cursor movement keeps a **desired visual column** for vertical moves. Movements that depend on the view (page up/down, vertical moves over wrapped or folded lines, Home/End on a wrapped line) ask a `LineLayout` interface, which the editor's view model implements; its default implementation treats every model line as one row.

## 8. Language support without a language server

### 8.1 Languages (`core/language`)

- A **registry** of languages: id, aliases, file extensions, file names, file name globs, first-line regex, MIME types, configuration, grammar, embedded languages and token types. Language of a file, as VS Code's `languagesAssociations.ts`: the user's `files.associations` first (globs, on the whole path when they have a slash and on the file name otherwise; the longest glob wins), then the languages' own: an exact file name, then the longest glob (`filenamePatterns`), then the longest extension; among equals the one registered last wins; the first line is looked at only when nothing else claims the file; otherwise `plaintext`. Paths and extensions are matched without regard to case. A `firstLine` that matches the empty text is left out.
- The registry (`language.Registry`) is filled by `AddExtension(fs.FS)` from the `package.json` of a VS Code extension at the root of the file system: `contributes.languages` (merged when several extensions contribute to one id, as `languagesRegistry.ts` does) and `contributes.grammars` (`language`, `scopeName`, `path`, `embeddedLanguages`, `tokenTypes`, `injectTo`). Entries that are not valid are left out and listed in `Problems`. Languages have a number (0 none, 1 plain text, then in order of registration), which the tokens of the grammars carry as their language. `Registry` is the `textmate.Registry` of the grammars (a grammar file is read when first asked for; `Injections` of a scope include those of the scopes that start it, as `TMGrammarFactory` does) and `NewGrammar(language)` makes the tokenizer of a language with its embedded languages (those of the injected grammars too) and token types. Grammars must be JSON; the one grammar of the reference extensions in the plist format (`source.js.regexp`) is converted to JSON by `scripts/update-assets.sh` (with `internal/core/language/internal/plist2json`, which also changes the path in the copied `package.json`) when JavaScript is bundled (step 4.6b). `language.Bundled()` makes a registry of its own from the embedded extensions (`internal/core/language/assets/<extension>/`, copied by `copy_extension` of `scripts/update-assets.sh`: `package.json`, the language configurations, `syntaxes/` and `cgmanifest.json`). The oracle test (`oracle_test.go`) tokenizes the colorize fixtures of the bundled languages as the Snapper of VS Code does and compares each line with `colorize-results`; the lines where Emetos differs are listed in `testdata/expected-differences.txt` (`<fixture> <lines> <reason>`), and an entry whose lines no longer differ fails the test. A rule whose included grammar is not bundled is left out, as vscode-textmate does, so a fenced block of such a language is not highlighted.
- **Language configuration** in VS Code's `language-configuration.json` format: comments, brackets, colorized bracket pairs, auto-closing pairs (with `notIn`), auto-close-before characters, surrounding pairs, folding markers and `offSide`, word pattern, indentation rules, on-enter rules. Its regular expressions are JavaScript syntax (as a string or `{pattern, flags}`) and compile with `core/regex`'s ECMAScript mode (the `u` flag turns on `\p{..}` property escapes, which `core/regex` translates). A field of the wrong type, or a regular expression that does not compile, is left out and listed as a problem; the other fields are kept (`ParseConfiguration`).
- **Bundled languages** (assets embedded with `embed`, copied from the pinned references by `scripts/update-assets.sh`, with their licenses in `third_party/`):
  - First (D13): Go, Markdown (with embedded code blocks for the bundled languages), JSON, JSON with Comments, YAML, Python, JavaScript, JavaScript React, TypeScript, TypeScript React.
  - Then: Go module files (`go.mod`, `go.sum`; `go.work` uses the `go.mod` grammar; from `golang/vscode-go`), Shell, Makefile, Dockerfile, HTML, CSS, SCSS, Less, C, C++, Rust, SQL, XML, INI, Diff, Git commit, Git rebase, Ignore files (`.gitignore` and others), plain text.
  - VS Code has no TOML grammar of its own; TOML is left for later.

### 8.2 Regular expressions (`core/regex`)

TextMate grammars use Oniguruma regular expressions; Go's `regexp` (RE2) lacks lookbehind and backreferences. `core/regex` wraps `github.com/dlclark/regexp2` (D11):

- Translation of Oniguruma-only syntax to regexp2: `\h`, `\H`, `\A`, `\z`, `\Z`, `\G` (an anchor position given by the tokenizer), `\K`, possessive quantifiers (as atomic groups), POSIX bracket classes, `\p{Alpha}`-style property names, class intersection `&&`, `(?x)` comments, and `(?m)` (Oniguruma's dot-matches-newline). A pattern that can't be translated is reported with a tag, and the rule using it never matches.
  - The pattern is parsed into a tree and written out again (`regex.Compile`), as vscode-oniguruma compiles it (`ONIG_OPTION_CAPTURE_GROUP`): every group captures and the groups are numbered by their opening parentheses, named ones included (regexp2 numbers named groups after the others, so names become numbers: `\k<name>` is `\k<n>`). `^` and `$` are lines anchors (`^` does not match after a final line break); `(?m)` is dot-all; `(?i)` and `(?x)` last to the end of the group, alternatives that follow included (`a(?i)b|c` is `a(?i:b|c)`); `(?x)` whitespace and comments are removed by the translator.
  - Repetition: `a*+` as an atomic group; `{n,m}+` repeats the interval (the Ruby syntax); `{n}?` is optional; `{,m}` is `{0,m}`; `{n,m}` with `n > m` is `{m,n}` that does not give back.
  - Classes: nested classes, `&&`, POSIX brackets (`[:alpha:]`, `[:^alpha:]`) and `\p{..}` (POSIX names, general categories long and short, Go's script tables) for Unicode text; `\x{85 2028}` (several code points) and `\x{...}`, `\h`, `\H`, `\R`, `\X`, `\N`, `\O`.
  - Calls of groups (`\g<name>`, `\g<1>`, `\g<-1>`, `\g<0>`) are replaced by a copy of the group that does not capture, with the ignore-case flag of the group; a group may be called inside itself 6 times, and the copy at the limit never matches, so recursive patterns (balanced parentheses) match up to that depth. When the pattern gets longer than 256 KB plus twice its source, the depth is lowered until it fits (`RegexTooLarge` when depth 1 does not).
  - `\K` is an empty group that marks the start of the match; the absence operator `(?~x)` is `(?:(?!x)[\s\S])*`; conditionals `(?(1)a|b)` and `(?(<name>)a|b)`.
  - Not supported, with a tag: options `W D S P y`, `\C-x`, `\M-x`, `\y`, `\Y`, levels in references (`\k<name+1>`).
  - `regex.Compile(pattern, Options)` takes the syntax, ignore case, the timeout, and `DisableA`/`DisableG` (the scanner asks for `\A` and `\G` that never match when the position is not the start of the text or the anchor). `regex.Cache` compiles a pattern once, failures included.
  A pattern that can't be translated is reported with a tag, and the rule using it never matches. All 5,900 patterns of the grammars under `reference/` compile (the corpus test of step 4.1).
- A **scanner** over several patterns, as vscode-textmate's `OnigScanner`: `Find(line, pos, anchorPos, isFirstLine)` returns the earliest match, ties broken by the lowest pattern index, with captures as byte offsets. A line is converted to runes once (`regex.NewLine`, which the tokenizer makes once per line and uses for every search of it); invalid UTF-8 bytes are treated as U+FFFD, one rune per byte, with the byte mapping kept. `\G` is allowed when `pos == anchorPos` and `\A` on the first line, as vscode-textmate's `compileAG` resolves them: the scanner compiles a variant of a pattern with the anchor that never matches the first time a line needs it. A pattern that does not compile never matches (`Scanner.Err` gives the reason).
- Every match runs with a time limit (100 ms, tag `RegexTimeout`); a line whose tokenization times out is plain text from that point.
- An **ECMAScript mode** (`regexp2.ECMAScript`) for find (§9.8) and for language configurations (§8.1).

### 8.3 Tokenization (`core/textmate`, `core/tokens`)

- Grammar loading (JSON) with `patterns`, `repository`, `include` (`#name`, `$self`, `$base`, `source.other#name`), `match`, `begin`/`end`, `begin`/`while`, all captures (including captures with patterns), `contentName`, `name` with `$1`-style back-references, `injections` and `injectionSelector`, embedded grammars through a grammar registry (scope name → grammar).
- A line tokenizer: (line, start state) → (tokens with scope stacks, end state). States compare for equality so re-tokenization stops when a line's end state is unchanged.
  - `textmate.ParseGrammar` reads a JSON grammar into a `RawGrammar` (read-only, shared by all documents); a `Registry` (implemented by the language registry of step 4.5) gives a grammar by scope name. `textmate.NewGrammar(scope, registry, opts)` makes the `Grammar`, which compiles its rules when the first line is tokenized and is safe for concurrent use. `Grammar.TokenizeLine(line, prev *State, timeLimit)` returns the tokens (byte offsets, each with its `ScopeStack`, a persistent list that shares its tail with the stack it was made from), the end `State` (a persistent list too; `nil` is the start of a document) and whether it stopped early. The tokenizer is a port of vscode-textmate (`tokenizeString.ts`, `rule.ts`, `grammar.ts`): the line is tokenized with a line break added, the token of the break is dropped, and the tokens are cut at the end of the line; an empty line has one empty token. The theme is not applied here: styles are resolved from the scopes later.
  - `begin`/`while` rules check their while pattern at the start of every line, outermost first: a rule whose pattern does not match is closed with the rules inside it, one that matches gives the line its `whileCaptures` and moves the start of the line past its match. Injections are the `injections` of the grammar (selector → rule, in the order of the file) and the grammars the registry says are injected into it (`Registry.Injections`, their `injectionSelector`); selectors have vscode-textmate's syntax (`L:`/`R:` priorities, `-`, `,`, `|`, parentheses; a name matches a scope that is it or starts with it and a dot). An injection that starts earlier than the rule of the scope wins, and so does one with `L:` that starts at the same place. Each token also has its **standard type** (`TokenType`: comment, string, regex or other — from the innermost scope that names one; `meta.embedded` resets to other — and then `GrammarOptions.TokenTypes` selectors) and its **language id** (`GrammarOptions.EmbeddedLanguages`: the innermost scope that starts with one of the scope names of the map; `InitialLanguage` elsewhere); the scope stack keeps both for each of its scopes.
  - A rule whose pattern does not compile never matches (`Grammar.Problems` lists why, once each); a pattern that runs past its limit (§8.2) ends the line as plain text in the scopes where it stopped (`StoppedEarly`). A line of `editor.maxTokenizationLineLength` bytes or more (default 20,000) is one token without scopes and leaves the state as it was. `\z` in a pattern is the end of the text without its line break, as in vscode-textmate.
- Each token also gets, as vscode-textmate does: a **standard token type** (comment, string, regex, other) from its scopes (the `\b(comment|string|regex|meta\.embedded)\b` rule plus the grammar's `tokenTypes`), and the **language id** of its embedded language (from the grammar's `embeddedLanguages`). Brackets, auto-closing and comments use them.
- Per document: line states cached; edits invalidate from the first edited line; a background goroutine tokenizes the rest in slices of at most 5 ms, checking for a newer version between slices; the visible lines are tokenized first, synchronously, before they are painted. Changing a document's language re-tokenizes it.
- Tokens are stored compactly per line: (end offset, style index, token type, language index).

### 8.4 Themes (`core/theme`)

- VS Code color theme JSON (JSON with comments): `type` (dark, light, hc-dark, hc-light), `colors` (workbench colors; a value of `default` takes the color back to its default), `tokenColors` (inline, or the path of a TextMate `.tmTheme` file, a plist), `include` of a parent theme (the parent's colors and rules come first, and the child's override), `semanticHighlighting`/`semanticTokenColors` (stored for area 3). The old format (a `settings` list) and a `.tmTheme` file as the theme itself are read as VS Code reads them: the rule without scope sets the editor's colors (`background`, `foreground`, `selection`, `caret`, ... of `themeCompatibility.ts`). `theme.Load(fsys, file, defaultType)` reads a theme; a theme file that includes itself, or a chain deeper than 16, has the tag `ThemeIncludeCycle`.
- Token style resolution follows vscode-textmate's `theme.ts` (`ParseRules`, `NewTokenTheme`: scope selectors with descendant parts, `>` child combinators and comma lists, the rule of the deepest scope wins, then the longer parent scopes, then the later rule) and produces a foreground color, a background color and a font style (bold, italic, underline, strikethrough) as numbers into the theme's color map. `TokenTheme.Match(scopes)` is `Theme.match` of vscode-textmate (the rule that fits the scopes as they are); `Resolve(scopes)` is the style of a token: the defaults changed by the rule that fits each prefix of the scopes, outermost first, which is how the tokenizer of vscode-textmate merges attributes. `Theme.TokenRules(custom)` and `TokenTheme(custom)` build the rules as VS Code's `ColorThemeData.tokenColors` does: a default rule with `editor.foreground`/`editor.background` (a theme without them gets VS Code's defaults for its type until the color registry of step 4.8 holds all defaults), the theme's rules, the user's `editor.tokenColorCustomizations` (`custom`), and the `token.info-token`... rules unless the theme has its own. VS Code's colorize results are the oracle: the foreground color of every token of all fixtures, for the eight default themes, equals the one Emetos resolves.
- A **workbench color registry**: every color id Emetos uses, registered by the step that first uses it, with VS Code's defaults for dark, light, hc-dark and hc-light (from `editorColorRegistry.ts`, `workbench/common/theme.ts` and the platform color files), including derived defaults (`transparent`, `lighten`, `darken`, `oneOf`).
- Bundled themes: Dark Modern, Light Modern, Dark+, Light+, Dark High Contrast, Light High Contrast (MIT, from VS Code), with the Visual Studio Dark and Light themes that Dark+ and Light+ include (files only, not offered). `theme.Bundled()` lists them with their names from the extension's `package.nls.json`, `theme.LoadBundled(name)` loads one by label or id. Default: Dark Modern, or the preferred light theme when `window.autoDetectColorScheme` is on and the system is light.
- User themes: JSON and `.tmTheme` files in `$XDG_CONFIG_HOME/emetos/themes/` (`storage.Paths.ThemesDir`), shared by the profiles; `theme.LoadUserThemes(fsys)` lists them by file name, with the file name as the id and the theme's `name` as the label; a file that cannot be read is among the problems, not an error.

### 8.5 Brackets (`core/brackets`)

Bracket pairs come from the language configuration; brackets inside comments and strings (by token type) are ignored. A bracket pair structure is kept up to date after edits (VS Code's bracket pair tree is the reference; a simpler incremental scheme is fine if it passes the ported tests and the "incremental equals full" fuzz test) and answers: matching bracket of a position, enclosing pairs of a position (guides, expand selection), and the nesting level of each bracket (colors `editorBracketHighlight.foreground1..6`).

### 8.6 Indentation (`core/indent`)

Indentation detection (VS Code's `indentationGuesser`), `increaseIndentPattern`/`decreaseIndentPattern`/`indentNextLinePattern`/`unIndentedLinePattern`, on-enter rules, type-time re-indentation (as VS Code's `getIndentActionForType`), `editor.autoIndent` levels (none, keep, brackets, advanced, full), `editor.autoIndentOnPaste` (VS Code's default: false), and the indent and bracket pair guides (VS Code's `guidesTextModelPart`).

### 8.7 Folding (`core/folding`)

As VS Code without language servers: the indentation provider (`indentRangeProvider`, which also handles the language's `folding.markers` and `offSide`) for every language; `editor.foldingStrategy: auto` will prefer a language server's ranges in area 3. Ranges are computed on a snapshot in the background and limited to `editor.foldingMaximumRegions` (5,000). Folded ranges are decorations, so they survive edits. Arrow keys move over a folded range; only commands that reveal a position inside it (go to line, find, undo) unfold it.

## 9. The editor widget (`gui/editor`)

### 9.1 Structure

- An editor has a **view model**: the document, the cursors, scroll positions, and the **line projection** from model lines to view rows: identity at first; folding hides rows (§8.7), word wrap splits a line into several rows (§9.2), and view zones insert rows of a given height between lines (used by the diff editor, §9.11, and later by area 3). Layout, scrolling, hit testing, vertical movement (`LineLayout`), the minimap and sticky scroll all go through the projection. Several editors (in several groups) can show one document, each with its own view model.
- **`RenderEditorText`**: a leaf `RenderBox` driven by a `Scrollable`'s `ViewportOffset` (vertical) and its own horizontal offset. It lays out and paints only the visible rows plus a margin of 10 rows. It is a repaint boundary, so Glutter's raster cache keeps the text when only the cursor changes.
- Separate layers for the cursors (blink), the gutter (line numbers, folding controls, later breakpoints and diff markers), the minimap, sticky scroll, rulers and overlays (find widget, later hover and completion). They share the view model's line layout cache.
- Editor view state (selections, scroll position, folded ranges) can be read and restored, for history (§10.3) and restore (§10.10).

- **Scrollbars and hit testing**: the editor is wrapped in `kit.ScrollableElement` with a `ScrollModel` made of the scroll controller (vertical), the view model (horizontal) and what the text layer measured (`SetTextMetrics`: text area size, vertical extent, the widest row laid out since the last document or font change, plus four characters of room). The scrollbars are VS Code's: 14 pixels wide and 12 pixels high, overlaid, shown while the pointer is over the editor or after a scroll and faded away 500 ms later (100 ms in, 800 ms out), slider of at least 20 pixels, a click on the track jumps the slider there (or pages with `ScrollByPage`), a drag goes on outside the scrollbar, and a shadow marks scrolled content. The text layer turns a point into a position (`RenderEditorText.PositionAt`: the row from the projection, the column nearest to x at a grapheme boundary: `columnAt` counts characters for plain ASCII without tabs in a monospace font and otherwise searches the grapheme boundaries with the x of each from the paragraph's boxes) and a position into the rectangle of its character (`RectOf`).

### 9.2 Text rendering

- **One `ui.Paragraph` per visible row**, built with a new `ui.ParagraphBuilder` from token spans, cached by (line content, tokens, theme, font settings, wrap width) and dropped by an LRU of 2,000 lines. Glutter caches shaped runs but not paragraphs: a cold code line costs about 41–58 µs, an unchanged one rebuilt 5–10 µs (Glutter's `docs/PERFORMANCE.md`, steps 22.6 and 22.10). Keeping the paragraphs of unchanged lines is what makes an edit cost about one line.
- **Tabs**: Glutter's tab stops (`ParagraphStyleOpts.TabSize` = `editor.tabSize`) place tabs, so paragraph offsets equal buffer byte columns. A tab draws nothing; whitespace rendering draws the arrows and dots itself. `editor.renderControlCharacters` draws control characters as a box over their (zero-width) position, so offsets stay equal.
- **Fonts**: `editor.fontFamily` defaults to the desktop's monospace font (`PlatformDispatcher.MonospaceFont()`, followed live), then the generic `monospace` (D14). The setting's registered default is the empty string, which means that (VS Code's default is a list of platform fonts); `editor.fontSize` defaults to 14. A comma-separated `editor.fontFamily` becomes `FontFamily` plus `FontFamilyFallback`, which Glutter tries in order. Positions come from the paragraph's queries (`GetBoxesForRange`, `GetPositionForOffset`), except for the fast path of pure-ASCII lines in a monospace font.
- **Line height**, as VS Code's `fontInfo.ts`: `editor.lineHeight` 0 means `round(1.35 × fontSize)`; a value below 8 is a multiplier of the font size; otherwise pixels.
- Ligatures follow `editor.fontLigatures` (`liga` and `calt` features on or off).
- **Font weight and letter spacing**: `editor.fontWeight` (`normal` 400, `bold` 700, or a number from 1 to 1000) and `editor.letterSpacing` (pixels between letters, which widens every character: columns, hit testing and the cursor use the measured width) go into the paragraph style and its cache key. **Zoom**: the zoom of the window (`editor.Zoom`, shared by its editors; level -5 to 20) makes the font and the lines `1 + level/10` times their size, over the settings; `editor.action.fontZoomIn`, `fontZoomOut` and `fontZoomReset` change it, and with `editor.mouseWheelZoom` Control with the mouse wheel does (a notch is a level; a touchpad pan of 200 pixels is a level). Every setting of this section is applied live: the shell re-reads the options when `editor.*` changes.
- **Word wrap** breaks lines at the wrap width: lines that are pure ASCII in a monospace font are broken by counting columns (VS Code's `simple` wrapping strategy), others by paragraph layout; breaks for the whole document are computed in the background in slices, with an estimate (one row per line) until they're known, and the scroll position is anchored to the top visible line when a line's row count changes.

### 9.3 Input

- **Keys** go through the window's key dispatcher (§5.4). Key names are VS Code's (`ctrl+shift+k`, `ctrl+k ctrl+c`): letters and digits come from Glutter's logical key (Latin layout, so they work on any keyboard layout); punctuation and other keys come from the physical key, named by its US-layout key (VS Code's `keyboard.dispatch: code`).
- **IME**: the editor implements `services.TextInputClient` with the delta model (`EnableDeltaModel`, `UpdateEditingValueWithDeltas`). Glutter's input layer keeps a full copy of the text it is given, so Emetos gives it only the current line (line-local offsets) and maps the deltas back. A committed text is typed at every cursor, as VS Code does. The preedit (composition) text is drawn as an underlined overlay at the primary cursor, not inserted into the document, so it never enters undo. The caret rect (`SetCaretRect`, which places the candidate window) is updated after every frame.
- **Mouse**: click places the cursor, double-click selects a word, triple-click a line; drag extends by character/word/line according to the click count; Shift+click extends; Alt+click adds a cursor (`editor.multiCursorModifier`); Shift+Alt+drag selects a column; Ctrl+click is reserved for "go to definition" (area 3); middle click pastes the primary selection. The mouse is a raw `Listener` on the row of gutter and text (not a gesture recognizer): the editor counts the clicks itself (a press within 400 ms and 5 pixels of the one before is the next click; four or more select everything) and reads the modifier keys from the binding's `HardwareKeyboard` at the press. What a press or drag does is VS Code's `ViewController.dispatchMouse`: Shift extends from the anchor (by words or lines after a double or triple click); `editor.multiCursorModifier` (`alt`, or `ctrlCmd`, which is Control on Linux) makes a click add a cursor (a click inside an existing selection or cursor removes it), a double or triple click add a word or a line, and dragging go on selecting with the cursor added last; with it and Shift, or with Alt when it is not the modifier (or `editor.columnSelection`, for every drag), the drag selects a column, from the anchor to the column under the pointer, also past the end of short lines; a press on the line numbers selects lines, and in the rest of the gutter goes to the start of the line. Only the primary button selects; the other buttons come with middle-click paste (3.8) and the context menu. A touch only places the cursor. `editor.mouseStyle` (`text`, `default`, `copy`) is the pointer over the text; the gutter has the default arrow. The word and line selection by unit are pure functions in `core/edit` (`Mover.WordSelect`, `WordSelectDrag`, `LineSelect`, `LineSelectDrag`).
- **Drag and drop of text** (`editor.dragAndDrop`): a single click without Alt on the text of the primary selection (not in a read-only document, not with `editor.columnSelection`) arms a drag of the selected text. The selection pressed is the one dragged when the pointer moves (VS Code takes the one under the first move; Emetos takes the pressed one first, so a quick first move does not lose the drag), a dotted drop caret (2 pixels, in the cursor color) follows the pointer except over the dragged text, the pointer is the copy cursor while Control is held and the default arrow otherwise, and Escape cancels. On release inside the editor the text moves (`edit.Commands.DragAndDrop`, one undo step; the dropped text is selected), or is copied with Control, unless the position is inside the selection (a copy beside it is allowed); outside the editor nothing happens. A release without a move is a click: the cursor goes there (the other selections stay; Shift extends the primary selection from its anchor). It is the editor's own pointer logic, not `Draggable`.
- **Scrolling**: the editor claims the pointer signal through the `PointerSignalResolver` when the scroll would move it (so `Scrollable` does not scroll twice), and the `Scrollable` is given a scroll behavior in which only a finger or a pen drags it (the touchpad is the editor's). A wheel notch is 53 logical pixels in Glutter and scrolls 50 pixels at `editor.mouseWheelScrollSensitivity` 1 (a smaller delta, from a high-resolution wheel, scrolls in proportion; a step is rounded away from zero); Alt multiplies by `editor.fastScrollSensitivity`; Shift scrolls sideways; a touchpad pan scrolls 1:1 with the same multipliers, opposite to the pan. `editor.smoothScrolling` animates the vertical scroll of the wheel over 125 ms with an ease-out cubic, and the next notch goes on from where the animation ends; the animation moves the position with `JumpTo` on every frame, because `Scrollable` ignores pointer events while its own animation runs (Emetos's scrolling to reveal the cursor and the touchpad are not animated). Scrolling beyond the last line is `editor.scrollBeyondLastLine`. While a selection is dragged beyond the top or bottom of the editor it scrolls every frame at VS Code's speed (30 to 200 lines a second, faster the farther the pointer is) and the end of the selection goes to the edge line, at the pointer's column, or to the start or end of the line when the editor cannot scroll further; beyond the left or right edge it scrolls sideways and the selection goes to the start or end of the pointer's line. The scrolling stops with the button, or when the pointer comes back inside.
- **Middle button** (`editor.selectionClipboard`, Linux): a middle click places the cursor and, on release, pastes the primary selection of the desktop there (`GetPrimaryData`, in the callback on the UI thread); a middle press that moves selects like the primary button and pastes nothing. Without the setting the middle button selects a column (VS Code's behavior).
- **Clipboard**: `editor.action.clipboardCopyAction`, `…CutAction` and `…PasteAction` (Ctrl+C, Ctrl+X, Ctrl+V, Shift+Insert; they are the editor's commands while it has the focus). Copy puts the selections on the system clipboard, one after the other in the order of the text with a line break between them; with no selection it copies the line with its break (`editor.emptySelectionClipboard`; the cursors of one line copy it once), and with the setting off an empty selection copies nothing. Cut copies and deletes (the line, with its break, of an empty selection, the last line taking the break before it; in a read-only document it only copies). Paste is applied in the callback of Glutter's asynchronous clipboard, on the UI thread, as one undo step, and does nothing in a read-only document. Every cursor gets the whole text, except: the pieces of a copy made by as many cursors go one to each cursor from the top; with `editor.multiCursorPaste` `spread` (the default) a text with as many lines as there are cursors goes line by line (a trailing line break is not a line); a block copied from a column selection into one cursor goes down the lines at the same visual column, padded to the width of the block, past the end of the text if it must; and a line copied from an empty selection goes above the line of an empty cursor, which stays in its text. The editor knows how its last copy was made (`ClipboardMemory`, shared by the editors of a window, matched by the copied text, as VS Code's in-memory metadata); a text copied by another program is plain. The primary selection of the desktop (`editor.selectionClipboard`, Linux) is set 100 ms after the selections stop changing, when every cursor has a selection and the editor has the focus, to the selected text in the order of the text, and the middle button pastes it (above). Copy with syntax highlighting is not implemented.

### 9.4 Decorations shown

Selections, current line, word and selection highlights, find matches, matching brackets, bracket colors and guides, indent guides, rulers, whitespace, folded-region markers, snippet tab stops, IME composition, and, from later areas, diagnostics squiggles, inlay hints and code lenses (the render object reserves the drawing order for them).

Drawing order and layers: the layer below the text draws the current line (`editor.renderLineHighlight`: `none`, `gutter`, `line`, `all`; `editor.renderLineHighlightOnlyWhenFocus`; the text is highlighted only while no selection has text; a theme with a transparent line highlight gets the 2-pixel border) and the selections (`editor.roundedSelection`: 3-pixel corners, reversed corners where a row reaches further than its neighbor, rows extended by one character where the line break is selected, the inactive color without focus); the text layer follows; the layer above the text draws the line cursors (`editor.cursorWidth`, 0 meaning 2 pixels). Each layer is a repaint boundary, so a blink repaints only the cursors. The cursors blink every 500 ms with timers of the window's binding (`editor.cursorBlinking`: `solid` does not blink; `blink` toggles on a timer; `smooth`, `phase` and `expand` run VS Code's keyframe animation on every frame, after half a second shown, for 20 half-second iterations, every other one backwards, ease-in-out between the keyframes: `smooth` and `phase` fade the cursor out by 60% and 90% of an iteration, `expand` shrinks its height towards its middle by 80%); every move or edit shows them and starts counting again, and an editor without the window's focus has no cursors and no timer. `editor.cursorStyle` has VS Code's six styles: `line` and `line-thin` (a bar of `editor.cursorWidth` or 2 pixels, or 1, between the characters; a bar wider than 2 pixels shows the character it covers), `block` (the character's cell, with the character in the opposite color of the cursor's), `underline` and `underline-thin` (2 or 1 pixel at the bottom of the cell) and `block-outline` (a 1-pixel border); a block covers a tab with the width of one character and the end of a line with one character. The overtype cursor style is not implemented.

### 9.5 Minimap

Blocks colored by token, with VS Code's sizes (`minimap.ts`: row height per scale, `editor.minimap.maxColumn`, `size`), drawn into an image (`ui.NewImageFromRGBA`) from a CPU buffer that is patched by line ranges after edits and tokenization; for long files only the window of lines the minimap shows is drawn, scrolled proportionally as VS Code does. The slider shows the viewport; click and drag scroll; selections and find matches are marked. `editor.minimap.renderCharacters` is registered with VS Code's default (true) but Emetos always draws blocks (a deviation). Hidden by `editor.minimap.enabled: false` and in large file mode.

### 9.6 Sticky scroll

Uses folding ranges: the header lines of the ranges that contain the first visible line, up to `editor.stickyScroll.maxLineCount` (5). They are drawn over the top of the text with a shadow; a click moves the cursor to that line and scrolls so it isn't hidden under the sticky lines.

### 9.7 Breadcrumbs

A bar above the editor with the path of the file (relative to the folder when there is one, else absolute); each segment opens a picker of its siblings. The symbol part is added in area 3.

### 9.8 Find widget (`core/find`, `gui/editor`)

The model finds matches on a snapshot line by line (a multiline regex searches a bounded window of lines), capped at 19,999 highlighted matches like VS Code; tracks the current match; replaces with VS Code's replace pattern (`$n`, `$&`, `\n`, `\t`, `\u \l \U \L`) and preserve case. The widget is an overlay at the top right of the editor: find input, toggles, match count, previous/next, the replace row, and "find in selection".

### 9.9 Snippets (`core/snippet`)

Parser for VS Code's snippet grammar (`$1`, `${1:default}`, `${1|a,b|}`, the variables, `${1/regex/format/flags}` transforms, `$0`). Variables are resolved synchronously from values the UI gathers first (the clipboard text is read before inserting). A snippet session tracks tab stops as decorations, mirrors edits to equal tab stops, and ends when the cursor leaves the snippet or reaches `$0`. Snippet files: `~/.config/emetos/snippets/<language>.json` and `*.code-snippets` (global, with `scope`). Tab priority follows VS Code's when-clauses (`inSnippetMode`, `hasNextTabstop`, `hasSnippetCompletions`, Emmet's).

### 9.10 Emmet (`core/emmet`)

Abbreviation parser and HTML output; CSS abbreviations from Emmet's snippets (MIT); abbreviation extraction before the cursor; `emmet.includeLanguages`, `emmet.syntaxProfiles`, `emmet.triggerExpansionOnTab` (off by default, as in VS Code).

### 9.11 Diff and merge

- `core/diff`: VS Code's default ("advanced") lines diff: dynamic programming for small inputs and Myers O(ND) for large ones, VS Code's heuristics (shifting to line and blank-line boundaries), refinement to character changes, ignore trim whitespace, and a time limit (`diffEditor.maxComputationTime`, 5 s) after which the unrefined line diff is shown. Moved-code detection is out of scope.
- Three-way merge: (a) **conflict markers**: parse `<<<<<<<`, optional `|||||||` (diff3 base), `=======`, `>>>>>>>` into conflicts (VS Code's `merge-conflict` parser); the current and incoming inputs are the file with every conflict taken from one side, the base is the `|||||||` sections or, without them, empty conflict regions; (b) **`--merge` inputs**: base, input 1 (incoming) and input 2 (current) are diffed against the base, and overlapping changes become conflicts (VS Code's `ModifiedBaseRange`). The result starts as the base with every non-conflicting change applied and each conflict region holding the base text, marked unhandled.
- `gui/diffeditor`: two editors side by side (or inline), aligned with view zones, scroll-synced, changed lines and characters colored, an overview ruler, next/previous change (Alt+F5, Shift+Alt+F5); the original side is read-only; save and dirty apply to the modified side; narrow widths switch to inline (`diffEditor.useInlineViewWhenSpaceIsLimited`).
- `gui/mergeeditor`: incoming (left) and current (right) on top, the result below, with accept incoming/current/both (current then incoming) per conflict and for all, and "Complete Merge": for a marker file it saves the file; for `--merge` it writes the `result` path and exits 0 when `--wait` is on (1 if the window closes without completing).

## 10. The workbench (`gui/workbench` and others)

### 10.1 Layout

Menu bar (File, Edit, Selection, View, Go, Help) at the top, activity bar on the left, primary side bar, editor area, panel, status bar. Part sizes, visibility and the panel position are saved per window (§10.10). The window's title bar is the compositor's, or Glutter's where the compositor draws none (`DecorationsAuto`); Emetos adds nothing to it except the title (`<file> - <folder> - Emetos`).

### 10.2 Widget kit (`gui/kit`)

Glutter has no IDE-styled controls, so Emetos builds them on Glutter's `widgets` layer (D9):

- `Theme`: an inherited widget carrying the resolved workbench colors and fonts.
- `SplitView` (resizable sashes, min/max sizes, VS Code's layout priorities so the editor area absorbs window resizes, double-click to reset), `ScrollableElement` (VS Code-style overlay scrollbars), `Tree` (on Glutter's `TreeSliver`), `List`, `Tabs`, `Button`, `IconButton`, `ActionBar`, `InputBox` (on Glutter's `EditableText`), `Toggle`, `Checkbox`, `Dropdown`, `Hover` (on `RawTooltip`), `Menu` and `MenuBar` and `ContextMenu` (on `RawMenuAnchor`/`MenuController`), `Dialog` (in-window, modal), `ProgressBar`, `Badge`, `ListOverlay` (a simple anchored list used before quick input exists).
- Icons: VS Code's Codicons font, bundled and loaded through `RunAppOpts.Fonts` (D14), with code points generated from the font package's `codicon.csv`.
- Fonts: the workbench uses the desktop's UI font (`UIFont()`), then `sans-serif` (D14).

### 10.3 Editor groups and tabs (`core/editorgroup`, `gui/workbench`)

A grid of groups (nested split views). Each group has its editors in order, a preview editor, pinned editors, an active editor and a most-recently-used order. Editors are text editors, diff editors, merge editors, the settings UI, the keybindings UI and, later, others; all implement one `EditorInput` interface (type id, resource, title, dirty, save, revert, serialize/deserialize for restore). `core/editorgroup` holds this as data; `gui/workbench` shows it.

Navigation history records editor locations as VS Code's history service does; Back/Forward use VS Code's Linux keys (Ctrl+Alt+-, Ctrl+Shift+-).

### 10.4 Activity bar and side bar views

Activity bar entries for Explorer (and, later, Search, Source Control, Run and Debug, Extensions), plus the Manage menu (settings, keybindings, themes, profiles). Views are registered with an id, title, icon and container, so later areas add their own.

### 10.5 Explorer (`core/workspace`, `gui/views/explorer`)

- `core/glob` has VS Code's glob patterns (`Parse`, `ParseRelative`, `ParseExpression` for `files.exclude`-style objects with `when` sibling clauses, `BasenameTerms` and `PathTerms` for searches) and a gitignore matcher (`Ignore`: nested `.gitignore` and `.ignore` files, negation, folder-only and anchored patterns, `**`, character classes, and the rule that a path below an excluded folder stays excluded; checked against `git check-ignore` by a test that needs git). Patterns run on Go's `regexp`. Sibling checks are synchronous (VS Code also accepts promises).
- The tree model reads folders lazily, sorts like VS Code (`explorer.sortOrder`: folders first, case-insensitive natural order), hides `files.exclude` matches (`core/glob`, VS Code's glob syntax), and watches with inotify (`golang.org/x/sys/unix`): the opened folder (recursively, as folders are read) and the parent folder of every open file outside it; events are coalesced every 50 ms; watching a directory (not inodes) sees files replaced by rename. Events for a file whose `FileStat` matches Emetos's own last write are ignored. Watching falls back to polling a folder when the inotify limit is reached, with a notice.
- File operations (new, rename, move, copy, delete to trash, paste) are undoable from the explorer (Ctrl+Z when the explorer has focus; undoing a delete restores from the trash), and open editors follow renames and moves. Cut/copy/paste uses an internal clipboard and also puts the files' URIs on the system clipboard (`text/uri-list`), so a file manager can paste them.

### 10.6 Quick input (`core/fuzzy`, `gui/quickinput`)

One quick input widget with modes by prefix, as in VS Code: no prefix (files), `>` (commands), `:` (go to line, revealing the line while typing), `?` (help), `@` (symbols, area 3), `#` (workspace symbols, area 3), `view ` (views). It reports the active item while the user moves through the list (used for theme preview), and restores focus to the previous widget when it closes. File search lists the folder's files in the background, respecting `files.exclude`, `search.exclude` and, with `search.useIgnoreFiles` (default true), `.gitignore` and `.ignore` files; it scores with VS Code's fuzzy scorer and shows recently opened files first. The command palette lists commands with their keybindings and recently used commands first. Recent items are kept in memory until UI state persistence exists (§10.10).

### 10.7 Notifications and dialogs

Notifications (`core/notify`) have a severity, message, actions, an optional progress, and a source. Any part of Emetos can post one from the start (before the UI shows them, they go to the log). Toasts show at the bottom right, at most 3 at once; the notification center keeps them; do-not-disturb hides non-error toasts. Modal dialogs (unsaved changes, overwrite, delete) are in-window (`kit.Dialog`), keyboard operable.

### 10.8 Icons for files

VS Code's file icon themes are a font plus a JSON mapping (file extensions, file names, language ids and folder names to font characters and colors, with light-theme variants). Emetos bundles Seti, VS Code's default (`extensions/theme-seti/icons/seti.woff` and `vs-seti-icon-theme.json`), loads its font through `RunAppOpts.Fonts`, and reads the mapping in VS Code's icon theme format, so the explorer, tabs, breadcrumbs and quick open show the same icons as VS Code. "None" (no file icons) is the other choice.

### 10.9 Zen mode, full screen, zoom

- Zen mode (Ctrl+K Z) hides the activity bar, side bar, panel, status bar and tabs, centers the layout, and goes full screen (`zenMode.*` settings); full screen (F11); centered layout (`workbench.editor.centeredLayout*`).
- **Window zoom** (`window.zoomLevel`; Ctrl+=, Ctrl+-, Ctrl+Numpad0, as VS Code's Linux keys) scales the whole window by 1.2 per level: a `kit.Zoom` widget at the root lays out its child with the constraints divided by the factor and paints it scaled (Glutter has no zoom of its own; if this proves poor, it becomes a Glutter issue). Editor font zoom (`editor.mouseWheelZoom`, the font zoom commands) changes only the editor's font size.

### 10.10 State, backups and restore (`core/storage`)

- Paths (XDG): user configuration in `$XDG_CONFIG_HOME/emetos/` (default profile) or `$XDG_CONFIG_HOME/emetos/profiles/<name>/`; state in `$XDG_STATE_HOME/emetos/` (with `profiles/<name>/` for a profile's UI state); cache in `$XDG_CACHE_HOME/emetos/`; the socket in `$XDG_RUNTIME_DIR`. Every path is computed from these variables, so tests and proofs can redirect them (§15).
- **Window identity**: a window with a folder is identified by the first 16 hex digits of the SHA-256 of the folder's absolute path; a window without a folder gets a random id stored in the global state.
- UI state (global and per window id): open editors and their groups, view state per editor (selections, scroll, folds), layout, the explorer's expanded folders, window size, maximized and full-screen state, recently opened folders and files. Saved as JSON on change (debounced 1 s, atomic replace) and on exit.
- **Hot exit** (`files.hotExit`):
  - Backups: one second after a dirty document's last change, its content, encoding and the `FileStat` of the file it came from are written to `$XDG_STATE_HOME/emetos/backups/<window id>/<SHA-256 of the URI>.json` (atomically); a backup is removed after a successful save or a revert.
  - `onExit` (default): when the application quits (the last window closes or "Quit"), dirty documents are not asked about; their backups are restored on the next start. Closing a window that isn't the last asks about its dirty documents.
  - `onExitAndWindowClose`: closing any window doesn't ask either; its backups are restored when its folder (or the empty window) opens again.
  - `off`: always ask; no backups are restored.
  - A crash: each window writes a `running` marker in its state folder and removes it on a clean close; a marker found at start means the last run crashed, and that window's backups are restored whatever `files.hotExit` says.
  - A backup whose file changed on disk since opens dirty with a notice.

### 10.11 Command line and windows (`cmd/emetos`, `core/ipc`)

- `emetos [options] [paths...]`: `-n`/`--new-window`, `-r`/`--reuse-window`, `-g`/`--goto file:line[:col]`, `-d`/`--diff <a> <b>`, `-m`/`--merge <path1> <path2> <base> <result>`, `-w`/`--wait` (exit when the files are closed, for `git commit`), `--profile <name>`, `--log <level>`, `--version`, `--help`. A folder path opens it; file paths open the files. The options are parsed by hand (short and long forms, and positional counts for `--diff` and `--merge`).
- A running instance listens on `$XDG_RUNTIME_DIR/emetos-<uid>.sock` (versioned JSON messages); a second `emetos` hands its arguments and working directory over and exits, or waits with `--wait`. A stale socket (nobody listening) is removed. Two launches at once: the one that binds the socket wins; the other retries the hand-over.
- Multiple windows are Glutter windows of one process (§5.1). "Open Folder" in a window that has no dirty documents (or after their prompts) replaces its workspace: the window's services are disposed and created again for the new folder. Closing a window asks about its dirty documents per `files.hotExit` (§10.10); closing the last window quits.

### 10.12 Profiles

A profile is a name with its own `settings.json`, `keybindings.json`, `snippets/` (in its configuration folder) and UI state (in its state folder), §10.10. The default profile is the plain `emetos/` folders. Names are 1–64 characters of letters, digits, spaces, `-` and `_`. A window uses one profile: the one last used with its folder, else the default; `--profile` chooses it. Switching a window's profile reloads its settings, keybindings, snippets and theme in place (editors stay open). Backups belong to windows, not profiles. Export/import is one JSON file: `{"name", "settings", "keybindings", "snippets": {file name: text}}`, with the files' texts as strings.

## 11. Configuration

### 11.1 Settings (`core/jsonc`, `core/settings`)

- `settings.json` files are JSON with comments and trailing commas. `core/jsonc` parses them with positions, and edits a value while keeping comments and formatting (as VS Code's `jsonc-parser` does), given formatting options (tab size, spaces, line ending).
- Layers, lowest to highest: defaults, user (profile), workspace (`<folder>/.emetos/settings.json` only, D12), and language-specific blocks (`"[go]": {...}`) inside each layer.
- Every setting is registered with key, type, default, description, enum values and descriptions, range, scope, and category, by the step that first uses it, with VS Code's values (`editorOptions.ts` for `editor.*`, the workbench's configuration contributions for the rest). Reading an unregistered key is a programmer error (a panic in debug builds). Unknown keys in a file are kept and reported. Change events carry the affected keys. Writing a value creates the file and its folder when needed.
- **Resolution** (as VS Code): the layers are merged (objects key by key, anything else replaced) and then the `[language]` blocks of the merged result are applied for the language asked about, so a block of any layer wins over the plain settings of every layer, a workspace block over a user block, and a block of one language alone over a block shared with others (`"[go][json]"`). A language's defaults (a language contributes them like VS Code's `configurationDefaults`) are blocks of the defaults layer. Settings of the application scope are read from the user file only; a workspace file that sets one gets a problem and the value is ignored. A value that its setting does not allow (type, enum, range) falls back to the default and is listed as a problem; so are syntax errors, unknown keys and ignored keys. Writing a value keeps the comments and the file's own indentation, line ending and byte order mark, edits a setting written as nested objects where it is, and creates the file and its folder.
- The settings UI shows registered settings by category with search, edits user or workspace values, and links to the JSON.

### 11.2 Commands and context keys (`core/command`, `core/contextkey`)

- Commands have an id (VS Code's ids where the command exists in VS Code), a title, a category, a precondition, and a handler. The registry holds metadata first; the handler is attached by the package that implements it (an editor command's handler lives in `gui/editor` and calls `core/edit`). Menus, keybindings, the palette and dev commands all run commands by id, with optional JSON-compatible arguments. A command VS Code does not list in the palette (the cursor commands) has no title, and the palette skips it. Setting a handler on a command that has one stacks it: the last one runs, and its remover brings the previous one back. `Run` does not check the precondition; menus, the palette and the dispatcher call `Enabled` first, as VS Code does.
- Context keys (e.g. `editorTextFocus`, `editorHasSelection`, `findWidgetVisible`, `explorerViewletFocus`, `resourceExtname`) live in scopes: a tree of scopes (window → part → widget); a widget gets a child scope from its parent's and sets its keys there; a lookup walks up the tree; the dispatcher (§5.4) evaluates against the scope of the focused widget. When-clauses use VS Code's syntax: `!`, `&&`, `||`, `==`, `!=`, `=~` (regex), `<`, `>`, `in`, `not in`, parentheses. Evaluation follows JavaScript's rules where VS Code relies on them (`==` is loose, a key that is not set is tested as the string "undefined" by `=~`, `<` and `>` parse the value as a float). Expressions are kept in VS Code's normal form (flat, sorted, `||` outermost) so that `Negate`, `Equals` and `Implies` behave as in VS Code. Regular expressions use Go's engine until `core/regex` has its ECMAScript mode; a pattern it cannot run (lookahead, backreferences) is accepted and never matches.

### 11.3 Keybindings (`core/keybinding`)

- The default keymap is VS Code's Linux keymap for the commands Emetos has. VS Code registers its defaults in code (`registerKeybinding`/`KeybindingsRegistry` calls in `coreCommands.ts` and the contributions); each step that adds a command adds its default binding from there.
- `keybindings.json` entries: `key` (with chords such as `ctrl+k ctrl+c`), `command` (a leading `-` removes a default), `when`, `args`. A leading `^` in a default's command id means the key is not consumed. An entry whose key, command id or when-clause cannot be read is reported as a problem and skipped (VS Code keeps an entry with an unknown key as an unbound command, so a typo in a removal would remove every key of the command). The `[KeyCode]` spelling of keys is not supported.
- Keys are VS Code's `KeyCode`s; the resolver matches the string `ctrl+shift+alt+meta+<Key>` of a pressed chord (the UI name of the key, such as `K`, `LeftArrow`, `[`). A pressed sequence resolves to no match, more chords needed, or a command; of several bindings that match, the last one registered whose when-clause holds wins, after the sort by weight (core 0, contributions 100, workbench 200), command id and secondary-key order. The dispatcher (`keybinding.Dispatcher`) keeps the pending chords and forgets them after 5 seconds (`Expire`, called from a timer); a key that completes no binding ends chord mode and is consumed, and shows "The key combination (Ctrl+K, X) is not a command.". Keybindings made of a lone modifier ("shift shift") resolve, but the dispatcher does not yet detect the double press (it needs key-up events: the workbench step).
- A chord's first key shows "(Ctrl+K) was pressed. Waiting for second key of chord..." in the status bar.
- A **recording mode** (for the keybindings UI) hands every key event to a recorder before resolution.
- The keybindings UI lists every command with its key, source (default/user) and when-clause; it searches by name and by recorded key, and edits `keybindings.json`.

## 12. Privacy

- Emetos never opens a network connection of its own: no telemetry, crash reports, update checks, extension galleries or remote fonts. `internal/archtest` fails the build when a package imports `net/http` (§5.3).
- Links in notifications or help open in the user's browser through the desktop portal; that is the only path to the network, and only on a click.
- Logs stay on disk (§13).

## 13. Errors and logging

- Every error uses `github.com/rm4n0s/errors` (CLAUDE.md). Errors shown to the user become notifications with a short message; the full error goes to the log as a `slog` attribute (`"err", err`).
- Logs: `log/slog` to `$XDG_STATE_HOME/emetos/logs/<date>/main.log` (kept 7 days), and to the "Emetos" channel of the Output panel. `emetos --log debug` raises the level.
- A panic in a background goroutine is recovered, logged, and reported once as an error notification; the window stays open. A panic in the UI goroutine is handled by Glutter's error reporting, logged, and Emetos tries to write backups (§10.10) before exiting.

## 14. Performance targets

Measured on release builds (`-tags glutter_release,emetos_dev`, so dev commands can drive the app) on the user's machine, in their desktop session with its GPU; headless numbers (software rendering) are recorded too but don't decide. The numbers and the method go in `docs/PERFORMANCE.md`.

| Metric | Target | First measured |
|---|---|---|
| Typing a character in a 10,000-line Go file: UI-thread time of the frame that shows it | < 4 ms | step 3.11 (no highlighting), again with highlighting in step 4.9 |
| Scrolling a 100,000-line file: frames | display refresh rate, no UI-thread frame over 8 ms | 3.11 |
| Opening a 10 MB file: first frame with text | < 300 ms | 3.11 |
| Opening a 200 MB file in large file mode: first frame with text | < 3 s | 5.12 |
| Cold start with no folder, to the first frame | < 400 ms | 3.11, again with the workbench in 10.1 |
| Idle, window focused, cursor blinking | < 1% of a core; no frames at all when the window has no focus | 3.11 |
| Resident memory, one 10,000-line file open | no more than an empty Glutter window (its `counter` example) plus 50 MB; measured 271 MB against the baseline's 287 MB headless, where Mesa's software GL dominates (revised in step 3.11 from "< 200 MB", which the graphics stack alone exceeds; the number to hold is the one with the GPU, `scripts/perf.sh --session`) | 3.11 |
| Fuzzy file search over 100,000 paths, per keystroke | < 30 ms, off the UI thread | 6.18 |

## 15. Testing

### 15.1 Tests

- Core packages: table-driven unit tests, fuzz tests for the buffer (random edits checked against a plain string), the JSONC parser, the snippet parser, the when-clause parser, and the tokenizer (no panics, bounded time).
- **Ported VS Code tests** use 1-based lines and UTF-16 columns; `core/vstest` converts them to Emetos positions. A case with non-ASCII text has its expected positions worked out again for byte columns, with a comment saying so. A case that needs VS Code's TypeScript harness (a full editor, services) is skipped, and the count of skipped cases is recorded.
- Tokenization: VS Code's own oracle, `extensions/vscode-colorize-tests/test/colorize-fixtures` and `colorize-results` (scopes per token), for every bundled language that has fixtures there; small fixtures of Emetos's own for the others.
- UI: `gluttertest` widget tests with the fake platform (keys through `SendKeyEvent`, IME through `SendTextInputEvent`, clipboard, file choosers, window close), and golden images with `MatchesGolden`.
- Golden images need deterministic fonts: UI tests load Go Mono (regular, bold, italic, bold italic), Codicons and Seti through `gluttertest.Opts.Fonts`, and set `editor.fontFamily` to Go Mono (D14). Glyphs Go Mono lacks (CJK, emoji) show as boxes, so their layout is checked through the paragraph queries, not by goldens.
- **Isolation**: no test, proof or check reads or writes the user's real configuration, state, cache, trash or socket. Go tests call a helper that points every `XDG_*` variable at `t.TempDir()`; scripts run the app through `scripts/isolated.sh`, which does the same with a temporary folder (and can copy a fixture configuration in).
- Display tests (`emetos_display` tag) start the real app under a headless Wayland compositor.
- Performance benchmarks (§14) are Go benchmarks plus `scripts/perf.sh`, which runs the app with `GLUTTER_FRAME_STATS=1`.

### 15.2 Dev commands

`EMETOS_DEV_COMMANDS` holds a JSON array of steps run after the first frame, each `["<command id>", <args>]` or `["sleep", "<duration>"]`, for example `[["workbench.action.quickOpen", ">line"], ["sleep", "200ms"]]`. Any registered command can be used; commands whose id starts with `dev.` exist only for this (opening a file at a position in a group, posting a notification, typing text) and are registered only in debug and `emetos_dev` builds (D15).

## 16. Build and runtime requirements

- Build: Go 1.27 only. `CGO_ENABLED=0 go build ./cmd/emetos` makes the binary; users build with `-tags glutter_release` (D16).
- Run: what Glutter needs (Wayland or X11, Vulkan or OpenGL ES, fontconfig), a monospace font, and optionally xdg-desktop-portal for file dialogs and opening links.

## 17. Licensing and third-party assets

Emetos is MIT-licensed (`LICENSE`). The reference sources are pinned in `scripts/reference-versions.txt` (committed). Bundled assets are listed in `NOTICE`, each with its license in `third_party/<name>/`:

| Asset | Source | License |
|---|---|---|
| TextMate grammars and language configurations | VS Code `extensions/*`; each grammar's upstream is listed in VS Code's `cgmanifest.json` files | MIT, and the grammar's own license (checked per grammar when it is added; a grammar without a permissive license is left out and recorded) |
| Color themes | VS Code `extensions/theme-defaults` | MIT |
| Ported code, default keybindings and setting defaults | VS Code `src/vs` | MIT |
| TextMate tokenizer algorithms | `microsoft/vscode-textmate` | MIT |
| Go module grammar | `golang/vscode-go` | MIT |
| Emmet parser algorithms and snippets | `emmetio/emmet` | MIT |
| Seti file icons (font and mapping) | VS Code `extensions/theme-seti` | MIT |
| Codicons (font and `codicon.csv`) | npm package `@vscode/codicons` | CC BY 4.0 (icons), MIT (code) |
| Go Mono (tests only, not bundled) | `golang.org/x/image/font/gofont` | BSD-3-Clause |
| `scripts/headless-wayland.sh`, `scripts/screenshot-headless.sh` | Glutter | BSD-3-Clause |

## 18. Milestones

| Milestone | Content |
|---|---|
| M0 | Skeleton: module, packages, lint, boundary tests, references, paths and logs, an empty window, proof scripts |
| M1 | Text core: positions, buffer, files, documents, undo, decorations, editing commands |
| M2 | Configuration: JSONC, settings, context keys, commands, keybindings, globs |
| M3 | Editor widget: rendering, cursors, keys, IME, mouse, scrolling, clipboard, options, first performance check |
| M4 | Languages: regex, TextMate, languages, themes, tokenization, brackets, indentation, guides, auto-closing, comments, folding |
| M5 | Editor features: find, word wrap, whitespace, minimap, sticky scroll, breadcrumbs, snippets, Emmet, large files |
| M6 | Workbench: widget kit, layout, menus, editor groups and tabs, notifications, panels, explorer, quick input, navigation, status bar |
| M7 | Files and sessions: saving, closing, auto save, external changes, UI state, hot exit, command line, windows |
| M8 | UIs for settings and keybindings, themes, zoom and zen mode, profiles |
| M9 | Diff and merge |
| M10 | Performance pass and release of areas 1–2 |

The steps are in `docs/IMPLEMENTATION.md`.

## 19. Open decisions

| # | Question | Recommendation |
|---|---|---|
| Q5 | **Emetos's own extension model** (area 11) | Decide later; the command, context-key, view, menu and settings registries are built so extensions can add to them |

## 20. Risks

| Risk | Mitigation |
|---|---|
| Text rendering through Glutter is too slow for an editor | Per-line paragraph cache, repaint boundary for the text, the performance check in step 3.11 before anything else is built on the editor; fixes go into Glutter (D8). Glutter's own numbers (41–58 µs per cold code line, 5–10 µs for an unchanged one) fit the budget of §14 |
| TextMate grammars written for Oniguruma behave differently with regexp2 | Translation layer in `core/regex`, a test that compiles every pattern of every bundled grammar, VS Code's colorize oracles, timeouts |
| Glutter is young; Emetos is its first large app | Workarounds are marked and listed (D8); Glutter fixes are made in the Glutter repository |
| Glutter's close requests for the first window are app-wide | Step 7.8 checks which hook reaches each window, and records a Glutter issue if the first window can't be told apart |
| The scope of "what VS Code has" grows | Areas 3–11 are outside this spec until planned; every feature in §3 is a row with a defined behavior |
