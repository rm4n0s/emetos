# Emetos: Specification

Emetos is a code editor in the spirit of Visual Studio Code, written in Go on the [Glutter](https://github.com/rm4n0s/glutter) GUI toolkit, for the Linux desktop. This file is the source of truth: when the code and this file disagree, one of them is wrong, and the user decides which.

The work is split into the eleven areas of VS Code listed in §3. **This version of the spec covers areas 1 (the editor) and 2 (the workbench).** The other areas are listed so that the architecture leaves room for them, and get their own sections when they are planned.

---

## 1. Goals

| # | Goal |
|---|---|
| G1 | **What VS Code has.** Areas 1 and 2 (§3) match VS Code's behavior, defaults and keyboard shortcuts closely enough that a VS Code user feels at home. When in doubt, do what VS Code does |
| G2 | **Private.** No telemetry, no crash reporting, no update checks, no network access of its own (§12) |
| G3 | **Native and light.** One Go binary, no Electron, no Node.js, no cgo. Starts fast, stays responsive on large files, uses no CPU when idle (§14) |
| G4 | **Compatible file formats.** VS Code's color themes, TextMate grammars, language configurations, snippet files, `settings.json` and `keybindings.json` formats work, so existing material can be reused |
| G5 | **Core and UI apart.** The editor core never imports Glutter. It is tested without a display, and the UI only shows and edits its state (§5) |
| G6 | **Testable.** Every feature has tests; UI features have widget tests and golden images that run without a display (§15) |

## 2. Non-goals

- Running VS Code extensions or a Node.js extension host.
- Windows, macOS, the web. Linux desktop only (Wayland and X11 through Glutter).
- Areas 3–11 of §3, until they are planned. Their hooks are listed where they matter (for example, Problems panel exists but has no producers until area 3).
- Microsoft's proprietary features: Marketplace, Settings Sync with a Microsoft/GitHub account, Live Share, Copilot.
- Workspace trust (it guards extensions and tasks, which don't exist yet).
- Multi-root workspaces (`.code-workspace` with several folders). One window opens one folder or loose files.
- Vim/Emacs emulation (in VS Code they are extensions).

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
| Cursors and selections | Multiple cursors (Alt+Click, Ctrl+Alt+Up/Down), add next occurrence (Ctrl+D), select all occurrences (Ctrl+Shift+L), column selection (Shift+Alt+drag and keyboard), expand/shrink selection (by word, brackets, line), cursor undo (Ctrl+U), primary selection on X11 and Wayland |
| Find and replace | Find widget with regex, match case, whole word, find in selection, preserve case, replace one/all, match count, highlight all matches, seed the search from the selection |
| Highlighting | TextMate grammars (§8.3), VS Code color themes (§8.4), current line highlight, selection and word highlights |
| Brackets | Matching bracket highlight, jump to bracket, bracket pair colorization, bracket pair guides, auto-closing and auto-surrounding pairs, auto-closing quotes, over-typing a closing character |
| Indentation | Indent guides (active guide highlighted), indentation rules and on-enter rules from language configurations |
| Folding | Fold by indentation, by `#region` markers, and by brackets; fold/unfold one, recursively, all, by level; folding gutter controls |
| Display | Line numbers (absolute, relative, off), rulers, word wrap (off, on, at column, bounded), render whitespace (none, boundary, selection, trailing, all), render control characters, cursor styles (line, block, underline) and blinking modes, smooth scrolling, font size and zoom, font ligatures on/off, letter spacing, line height |
| Minimap | Rendered blocks of the text with token colors, viewport slider, click and drag to scroll, show on the right or left, scale 1–3, hide setting |
| Sticky scroll | The headers of the folding ranges that contain the top line stay at the top of the editor (up to 5 lines); click jumps to the header |
| Breadcrumbs | The file path above the editor, each segment a picker of its siblings. Symbols come with area 3 |
| Undo/redo | Per document, grouped by typing pauses and by command, with cursor state restored. Survives save, not restart |
| Saving | Save, Save As, Save All, Revert; auto-save modes `off`, `afterDelay`, `onFocusChange`, `onWindowChange`; trim trailing whitespace, insert final newline and trim final newlines on save |
| Hot exit | Unsaved changes of every editor, including untitled ones, are backed up and restored on the next start (§10.10) |
| Snippets | VS Code snippet syntax (tab stops, placeholders, choices, variables, transforms, nested); user snippet files (global and per language); insert snippet command; tab-completion of snippet prefixes (`editor.tabCompletion`) |
| Emmet | HTML and CSS abbreviation expansion with Tab and the "Emmet: Expand Abbreviation" command, in HTML, CSS and their relatives |
| Diff editor | Side-by-side and inline views, character-level changes, ignore trim whitespace, next/previous change, compare two files, compare with saved, read-only original |
| Merge editor | Three-way merge of a file with conflict markers: incoming, current and result panes, accept incoming/current/both per conflict, complete merge |
| Files | Encodings (§6.3), line endings LF and CRLF (detect, convert), BOM, read-only files, files changed on disk (reload or compare when dirty), large files (§6.4), binary files (refused with a notice) |

### 3.2 Area 2 in detail

| Feature | Behavior |
|---|---|
| Layout | Menu bar, activity bar, primary side bar, editor area, panel (bottom, or left/right), status bar; each part can be hidden, and sizes are remembered (§10.1) |
| Editor groups | Split editors right/down, any number of groups in a grid, move/copy editors between groups, focus groups by number, close, maximize a group, join groups |
| Tabs | Preview editors (italic, replaced by the next preview), pinned tabs, dirty indicator, close button, close others/to the right/saved/all, reorder and move by drag, scroll when they overflow, reveal in explorer, copy path |
| Navigation | Back/forward history across editors (Alt+Left/Right), go to line (Ctrl+G), switch editors (Ctrl+Tab), quick open (Ctrl+P) with recent files and fuzzy file search, command palette (Ctrl+Shift+P) |
| Explorer | Folder tree with file watching, `files.exclude`, new file/folder, rename (F2), delete to trash, cut/copy/paste, drag to move, drop files from the file manager, compact folders, reveal active file, Open Editors section, collapse all |
| Status bar | Line/column and selection count, indentation, encoding, line endings, language mode (each one opens its picker), notifications bell, problem counts |
| Notifications | Info, warning and error toasts with actions, notification center, do-not-disturb |
| Panels | Output (channels: the Emetos log, and more later), Problems (empty until area 3; the model exists) |
| Zen mode and full screen | Zen mode hides everything but the editor; full screen; centered layout |
| Windows | New window, open folder or files in a new or the same window, single instance per user (`emetos <path>` reaches the running instance), window state and editors restored on restart |
| Themes | Color theme picker with preview, follow the system's dark/light mode, file icon theme (§10.8), product icons |
| Settings | Default/user/workspace/language-specific layers; `settings.json` (JSON with comments); settings UI with search and categories that edits the JSON and keeps comments |
| Keybindings | Default Linux keymap of VS Code (§11.3); `keybindings.json`; keybindings UI with search by name or by key, record a key, change/remove/reset |
| Profiles | Named sets of settings, keybindings, snippets and UI state; create, switch, delete, export/import as a file |

## 4. Decisions log

| # | Decision | Why |
|---|---|---|
| D1 | Go, Glutter, Linux desktop only | The user's choice; Glutter is the user's toolkit |
| D2 | No cgo, ever | Glutter is cgo-free; `CGO_ENABLED=0 go build` keeps the build to Go alone |
| D3 | The core (`internal/core/...`) never imports Glutter; the UI (`internal/gui/...`) imports the core (§5.2) | Testable core, a UI that can change |
| D4 | No telemetry and no network access of Emetos's own (§12) | G2 |
| D5 | Language intelligence comes only from LSP and debugging only from DAP (areas 3 and 6) | One mechanism for every language |
| D6 | Text is UTF-8 in memory; positions are byte offsets and (line, byte column) pairs; visual columns are computed for display (§6.2) | Matches Glutter's text API; LSP 3.17 can negotiate UTF-8 positions, and UTF-16 is converted when a server can't |
| D7 | VS Code's file formats for themes, grammars, language configurations, snippets, settings and keybindings (G4) | Reuse of existing material |
| D8 | When Glutter lacks something, Emetos works around it, marks the code with `// Glutter:`, and lists it in `docs/GLUTTER-ISSUES.md` (which exists only while there are open issues) | The user's rule (CLAUDE.md) |
| D9 | Hand-made widgets on Glutter's `widgets` layer for the workbench, themed by VS Code workbench colors; Material widgets only for menus where they can be restyled | A dense IDE look, not a phone look |
| D10 | Glutter v1.4.0 or newer: fonts loaded at run time (`RunAppOpts.Fonts`, `PlatformDispatcher.LoadFontFromList`), fonts in `gluttertest` (`Opts.Fonts`), family fallback in order, font queries and the desktop's font settings, `TreeSliver`, tab stops (`ParagraphStyleOpts.TabSize`), the cache of shaped runs | What the editor needs (was Q1) |
| D11 | Regular expressions with `github.com/dlclark/regexp2` (§8.2) | TextMate grammars need lookbehind and backreferences (was Q2) |
| D12 | Workspace settings only in `<folder>/.emetos/settings.json`; `.vscode/` is not read | The user's choice (was Q3) |
| D13 | Bundled languages: the list in §8.1. Go, Markdown, JSON, YAML, Python, JavaScript and TypeScript come first and get the deepest tests | The user's priorities (was Q4) |
| D14 | Fonts: the editor uses the desktop's monospace font (`MonospaceFont()`), then `monospace`; the workbench uses the desktop's UI font (`UIFont()`), then `sans-serif`. Codicons and the Seti icon font are bundled and loaded through `RunAppOpts.Fonts`. Tests use Go Mono (from `golang.org/x/image/font/gofont`) through `gluttertest.Opts.Fonts` | Looks like the desktop; deterministic tests with no extra dependency |

## 5. Architecture

### 5.1 Processes and threads

Emetos is one process. Glutter's UI goroutine owns every widget and render object. The core is safe for use from the UI goroutine and from background goroutines as described per package:

- **Documents** are changed only on the UI goroutine. Every change produces a new immutable **snapshot** of the buffer (§6.1) that background goroutines read freely.
- **Background work** (tokenizing, folding ranges, file watching, fuzzy file search, diffing, backups) runs on goroutines with a `ctx`, reads snapshots, and posts results to the UI goroutine through `WidgetsBinding.Post` or `State.SetState`. A result for an older document version is dropped.
- **Child processes** (language servers, debug adapters, terminals) come with later areas and follow the same rule.

### 5.2 Package layout

```
cmd/emetos/                    main: command line, single instance, RunApp
internal/core/                 no Glutter import anywhere below (checked by internal/archtest)
  buffer/                      piece tree, snapshots, line index (§6.1)
  fileio/                      read/write, encodings, line endings, atomic save, trash (§6.3)
  document/                    document = buffer + metadata + undo + decorations (§7.1)
  edit/                        selections, cursor movement, editing commands (§7.2)
  language/                    language registry, language configurations, bundled assets (§8.1)
  regex/                       Oniguruma-style regular expressions for grammars and find (§8.2)
  textmate/                    grammar loading and line tokenizer (§8.3)
  tokens/                      per-document tokenization state, background tokenizer (§8.3)
  theme/                       color themes, token colors, workbench color registry (§8.4)
  brackets/                    bracket pairs, matching, colorization (§8.5)
  indent/                      indentation rules, detection, on-enter rules (§8.6)
  folding/                     folding range providers (§8.7)
  find/                        find/replace model (§9.8)
  snippet/                     snippet parser and session (§9.9)
  emmet/                       Emmet abbreviations (§9.10)
  diff/                        line and character diff, three-way merge model (§9.11)
  jsonc/                       JSON with comments: parse, and edit keeping comments (§11.1)
  settings/                    settings registry, layers, change events (§11.1)
  contextkey/                  context keys and when-clause expressions (§11.2)
  command/                     command registry (§11.2)
  keybinding/                  keymap, chords, default Linux keymap (§11.3)
  workspace/                   folder, file tree model, watcher, file operations (§10.5)
  fuzzy/                       fuzzy matching and scoring (§10.6)
  storage/                     paths, UI state, backups (hot exit), profiles (§10.10, §10.12)
  ipc/                         single instance over a Unix socket (§10.11)
  logs/                        log channels shown in the Output panel (§13)
  notify/                      notification model (§10.7)
internal/gui/                  imports Glutter and the core
  app/                         RunApp, windows, wiring of the services
  kit/                         the IDE widget kit: theme, icons, split view, tree, list, tabs, inputs, buttons, menus, hovers (§10.2)
  editor/                      the editor widget (§9)
  diffeditor/, mergeeditor/    §9.11
  workbench/                   layout parts, editor groups, title/menu bar, activity bar, status bar (§10)
  quickinput/                  command palette, quick open, pickers (§10.6)
  views/explorer/, views/output/, views/problems/
  settingsui/, keybindingsui/  §11
internal/archtest/             tests of package boundaries and forbidden imports (§5.3, §12)
third_party/                   licenses of bundled assets (§17)
scripts/                       helper scripts (fetching bundled assets, performance runs)
```

### 5.3 Boundary rules (checked by `internal/archtest`)

- No package under `internal/core/` imports `github.com/rm4n0s/glutter/...` or `internal/gui/...`.
- No package imports `net/http`, `net/rpc` or `crypto/tls`. `net` is allowed only in `internal/core/ipc` (Unix sockets) and, later, in the packages that talk to language servers and debug adapters.
- No `import "C"`.

## 6. Text core

### 6.1 Buffer (`core/buffer`)

- A **piece tree**: a red-black tree of pieces pointing into read-only original chunks and an append-only add buffer, as in VS Code's `PieceTreeTextBuffer`. Each node keeps the byte length and the number of line feeds of its subtree.
- Operations: insert, delete, replace at byte offsets; `LineCount`, `LineContent(n)`, `LineLength(n)`, `OffsetAt(line, col)`, `PositionAt(offset)`, `Text(range)`, iteration over chunks and lines. All are O(log n) in the number of pieces plus the size of the result.
- **Snapshots**: `Snapshot()` returns an immutable view in O(1) (the tree is persistent: edits copy the path they change). Snapshots are safe to read from any goroutine.
- Line endings: the buffer stores the text with the file's line endings normalized to `\n`; the document remembers the original (`\n` or `\r\n`) and writes it back on save. A file with mixed endings is normalized to the most common one, and the user is told once.

### 6.2 Positions

- `Position{Line, Col int}`: zero-based line and **byte** column. `Range{Start, End Position}`. `Selection{Anchor, Active Position}`.
- **Visual columns** (for column selection, rulers, `editor.tabSize`, vertical cursor movement): tabs advance to the next tab stop; wide East Asian characters and emoji count 2; combining marks count 0. Computed by `core/edit` with `golang.org/x/text` width tables.
- Grapheme clusters (go-text's segmenter, through Glutter's dependencies) bound cursor movement and deletion.

### 6.3 Files (`core/fileio`)

- Encodings: UTF-8, UTF-8 with BOM, UTF-16 LE/BE (BOM), and the single-byte and CJK encodings of `golang.org/x/text/encoding` that VS Code lists in `files.encoding`. Guessing (`files.autoGuessEncoding`) is off by default, as in VS Code; when on, it tries UTF-8, then the BOMs, then the configured fallback.
- Saving is atomic: write to a temporary file in the same folder, `fsync`, rename over the original, keep the original mode. When the folder isn't writable, write in place. A read-only file needs "Save As" or a confirmation to overwrite (`sudo` saving is out of scope).
- Binary detection: a NUL byte in the first 8 KiB means binary. Binary files open as a notice with "Open anyway".
- Delete moves to the freedesktop.org trash (`$XDG_DATA_HOME/Trash`), with permanent delete as a fallback after confirmation.

### 6.4 Large files

| File size | Behavior |
|---|---|
| < 20 MB | Everything on |
| 20 MB – 256 MB, or more than 300,000 lines | "Large file mode": no tokenization (plain text), no bracket colorization, no folding, no minimap, no word wrap, and a notice saying so |
| > 256 MB | Refused with an error notice (VS Code's default limit) |

Lines longer than 10,000 bytes are drawn in pieces, and tokenization of one line stops after 20,000 bytes (`editor.maxTokenizationLineLength`), as in VS Code.

## 7. Documents and editing

### 7.1 Document (`core/document`)

- A document has: a URI (a file path, or `untitled:<n>`), the buffer, a version number (incremented by each change), language id, encoding, line ending, BOM flag, indentation options (`tabSize`, `insertSpaces`, detected or from settings), dirty flag (compares the version with the saved version, so undoing back to the saved state clears it), and read-only flag.
- **Edits** are applied as a batch of non-overlapping replacements; the document returns the inverse batch.
- **Undo/redo** stack of edit groups. A group stores the edits, their inverses, and the selections before and after. Typing is merged into one group until a pause of 500 ms (from the clock given to the document, so tests are deterministic), a cursor move, or a different kind of edit. Undo and redo restore the selections.
- **Decorations**: ranges that move with edits (with stickiness: grows/doesn't grow at either edge), used for find matches, bracket highlights, snippet tab stops, diff changes, folded regions and, later, diagnostics.
- Change events carry the version, the edits and the new snapshot.

### 7.2 Editing commands (`core/edit`)

Pure functions from (snapshot, selections, options) to (edits, new selections), so they are tested without a document or a UI. They cover every row of §3.1 under "Editing", "Line commands" and "Cursors and selections". Word boundaries follow `editor.wordSeparators` (VS Code's default ``~!@#$%^&*()-=+[{]}\|;:'",.<>/?``) and word parts split camelCase and snake_case.

Cursor movement keeps a **desired visual column** for vertical moves. With word wrap on, vertical movement goes by visual (wrapped) line; this needs the view's line layout, so the editor widget supplies a `LineLayout` interface to `core/edit`.

## 8. Language support without a language server

### 8.1 Languages (`core/language`)

- A **registry** of languages: id, aliases, file extensions, file names, file name globs, first-line regex, MIME types, configuration and grammar. Language of a file: by `files.associations` first, then file name, then extension, then first line; otherwise `plaintext`.
- **Language configuration** in VS Code's `language-configuration.json` format: comments, brackets, colorized bracket pairs, auto-closing pairs (with `notIn`), auto-close-before characters, surrounding pairs, folding markers and `offSide`, word pattern, indentation rules, on-enter rules.
- **Bundled languages** (assets embedded with `embed`, taken from VS Code's repository at a pinned commit by `scripts/fetch-assets.sh`, with their licenses in `third_party/`):
  - First (D13): Go, Markdown, JSON and JSON with Comments, YAML, Python, JavaScript, JavaScript React, TypeScript, TypeScript React.
  - Then: Go module files (`go.mod`, `go.sum`, `go.work`; grammar from `golang/vscode-go`, MIT), Shell, Makefile, Dockerfile, HTML, CSS, SCSS, Less, C, C++, Rust, SQL, XML, INI, Diff, Git commit, Git rebase, Ignore files (`.gitignore` and others), plain text.
  - VS Code has no TOML grammar of its own; TOML is left for later.

### 8.2 Regular expressions (`core/regex`)

TextMate grammars use Oniguruma regular expressions (lookbehind, backreferences, possessive quantifiers, `\G`, `\h`, named groups); Go's `regexp` (RE2) has none of the first three. `core/regex` wraps `github.com/dlclark/regexp2` (D11) and translates Oniguruma-only syntax (`\h`, `\H`, `\A`, `\Z`, `\G`, POSIX bracket classes) to it. Find (§9.8) uses the same engine in ECMAScript mode, so find patterns behave like VS Code's (JavaScript syntax).

Every match runs with a time limit, so a catastrophic pattern can't freeze a line's tokenization; a line that times out is shown as plain text from that point.

### 8.3 Tokenization (`core/textmate`, `core/tokens`)

- TextMate grammar loading: JSON grammars with `patterns`, `repository`, `include` (`#name`, `$self`, `$base`, `source.other#name`), `match`, `begin`/`end`, `begin`/`while`, `captures`/`beginCaptures`/`endCaptures`/`whileCaptures` (including captures with patterns), `contentName`, `name` with `$1`-style back-references, `injections` and `injectionSelector`, embedded languages.
- A line tokenizer: (line text, start state) → (tokens with scope stacks, end state). States are compared for equality so re-tokenization stops when the end state of a line is unchanged.
- Per document: line states cached; edits invalidate from the first edited line; a background goroutine tokenizes the rest in slices of at most 5 ms between checks for a newer version; the visible lines are tokenized first, synchronously, before they are painted.
- Tokens are stored compactly per line: (end offset, theme style index) pairs after theme resolution, plus the scope stacks only when needed (bracket and comment detection use a token-type bit: comment, string, regex, other).

### 8.4 Themes (`core/theme`)

- VS Code color theme JSON: `type` (dark, light, hc-dark, hc-light), `colors` (workbench colors), `tokenColors` (TextMate scope rules: scope selectors with descendant selectors, comma lists, and exclusions), `include` of a parent theme, `semanticHighlighting`/`semanticTokenColors` (stored for area 3).
- Token style resolution follows VS Code's rules (most specific selector wins, parent matching) and produces foreground, background-free font style (bold, italic, underline, strikethrough).
- A **workbench color registry**: every color id Emetos uses (e.g. `editor.background`, `editorCursor.foreground`, `tab.activeBackground`), with VS Code's default for dark, light and high-contrast themes, so incomplete themes still look right.
- Bundled themes: Dark Modern, Light Modern, Dark+, Light+, Dark High Contrast, Light High Contrast (MIT, from VS Code). Default: Dark Modern, or Light Modern when `window.autoDetectColorScheme` is on and the system is light.
- User themes: JSON files in `~/.config/emetos/themes/`.

### 8.5 Brackets (`core/brackets`)

Bracket pairs come from the language configuration; brackets inside comments and strings (by token type) are ignored. A bracket pair tree is maintained incrementally after edits (as VS Code's bracket pair colorizer 2 does) and answers: matching bracket of a position, enclosing pairs of a position (for guides and expand selection), and the nesting level of each bracket (for colors `editorBracketHighlight.foreground1..6`).

### 8.6 Indentation (`core/indent`)

Indentation detection (VS Code's algorithm on the first 10,000 lines), `increaseIndentPattern`/`decreaseIndentPattern`/`indentNextLinePattern`/`unIndentedLinePattern`, on-enter rules, auto-indent on paste (`editor.autoIndent`: none, keep, brackets, advanced, full).

### 8.7 Folding (`core/folding`)

Providers: indentation (with `offSide` languages), markers (`folding.markers` of the language), and brackets as the fallback for languages without markers when `editor.foldingStrategy` is `auto`. Ranges are computed on a snapshot in the background and limited to 5,000 (`editor.foldingMaximumRegions`). Folded ranges are decorations, so they survive edits.

## 9. The editor widget (`gui/editor`)

### 9.1 Structure

A widget tree around one custom render object:

- **`RenderEditorText`**: a leaf `RenderBox` driven by a `Scrollable`'s `ViewportOffset` (vertical) and its own horizontal offset. It lays out and paints only the visible lines plus a margin of 10 lines. It is a repaint boundary, so Glutter's raster cache keeps the text when only the cursor changes.
- Separate layers for the cursors (blink), the gutter (line numbers, folding controls, later breakpoints and diff markers), the minimap, sticky scroll, rulers and overlays (find widget, later hover and completion).
- An editor has a **view model**: the document, the selections, folded regions, word-wrap line breaks, scroll position. Several editors (in several groups) can show one document, each with its own view model.

### 9.2 Text rendering

- **One `ui.Paragraph` per visible line** (per wrapped line with word wrap), built from token spans, cached by (line content, tokens, theme, font settings, wrap width) and dropped by an LRU of 2,000 lines. Glutter caches shaped runs but not paragraphs: a cold code line costs about 41–58 µs, an unchanged one rebuilt 5–10 µs, and a line with one changed span 14–18 µs (Glutter's `docs/PERFORMANCE.md`, steps 22.6 and 22.10). Keeping the paragraphs of unchanged lines is what makes an edit cost about one line (Glutter's PERFORMANCE.md says only the caller can do that).
- **Tabs**: Glutter's tab stops (`ParagraphStyleOpts.TabSize` = `editor.tabSize`, in space widths of the paragraph's font) place tabs, so paragraph offsets equal buffer byte columns. A tab draws nothing; whitespace rendering draws the arrows and dots itself.
- **Fonts**: `editor.fontFamily` defaults to the desktop's monospace font (`PlatformDispatcher.MonospaceFont()`, followed live), then the generic `monospace` (D14). A comma-separated `editor.fontFamily` becomes `FontFamily` plus `FontFamilyFallback`, which Glutter tries in order. Proportional and wide characters are handled through the paragraph's queries (`GetBoxesForRange`, `GetPositionForOffset`), never by assuming a fixed advance, except for the fast path of pure-ASCII lines in a monospace font.
- Ligatures follow `editor.fontLigatures` (`liga` and `calt` features on or off).

### 9.3 Input

- **Keys**: a `Focus` node's key handler resolves key events through the keybinding service (§11.3). Unhandled printable characters reach the editor's `TextInputClient` as text.
- **IME**: the editor implements `services.TextInputClient`. Glutter's input layer keeps a full copy of the text it is given, so Emetos gives it only the current line (with line-local offsets) and maps the results back. The caret rect and composing rect are updated after every frame, so the candidate window follows the cursor. Composition is shown underlined.
- **Mouse**: click places the cursor, double-click selects a word, triple-click a line; drag extends by character/word/line according to the click count; Shift+click extends; Alt+click adds a cursor; Shift+Alt+drag selects a column; Ctrl+click is reserved for "go to definition" (area 3); middle click pastes the primary selection; drag and drop of selected text moves it (Ctrl copies).
- **Scrolling**: mouse wheel with high-resolution deltas, touchpad panning, `editor.mouseWheelScrollSensitivity`, Alt for fast scroll, `editor.smoothScrolling`; scrolling beyond the last line (`editor.scrollBeyondLastLine`); horizontal scrolling with Shift+wheel and touchpad.
- **Clipboard**: copy/cut/paste with multiple cursors (one line per cursor when the counts match, `editor.multiCursorPaste`); copy with no selection copies the line (`editor.emptySelectionClipboard`); the primary selection is set when the selection changes.

### 9.4 Decorations shown

Selections, current line, word highlights (other occurrences of the word under the cursor), find matches, matching brackets, bracket colors and guides, indent guides, rulers, whitespace, folded-region markers, snippet tab stops, IME composition, and, from later areas, diagnostics squiggles, inlay hints and code lenses (the render object reserves the drawing order for them).

### 9.5 Minimap

Blocks (not characters) colored by token, one row per line at scale 1 (2 or 3 with `editor.minimap.scale`), drawn into a cached image that is updated by line ranges after edits and tokenization. The slider shows the viewport; click and drag scroll. Hidden by `editor.minimap.enabled: false` and in large file mode.

### 9.6 Sticky scroll

Uses folding ranges: the header lines of the ranges that contain the first visible line, up to `editor.stickyScroll.maxLineCount` (5). They are drawn over the top of the text with a shadow; a click moves the cursor to that line.

### 9.7 Breadcrumbs

A bar above the editor with the path of the file relative to the folder; each segment opens a picker of its siblings. The symbol part is added in area 3.

### 9.8 Find widget (`core/find`, `gui/editor`)

The model finds matches on a snapshot (incrementally for documents up to 20 MB, capped at 19,999 highlighted matches like VS Code), tracks the current match, and replaces (with regex groups `$1` and preserve case). The widget is an overlay at the top right of the editor: find input, toggles, match count, previous/next, the replace row, and "find in selection".

### 9.9 Snippets (`core/snippet`)

Parser for VS Code's snippet grammar (`$1`, `${1:default}`, `${1|a,b|}`, `$TM_FILENAME` and the other variables, `${1/regex/format/flags}` transforms, `$0`). A snippet session tracks tab stops as decorations, mirrors edits to equal tab stops, and ends when the cursor leaves the snippet or reaches `$0`. Snippet files: `~/.config/emetos/snippets/<language>.json` and `*.code-snippets` (global, with `scope`).

### 9.10 Emmet (`core/emmet`)

Abbreviation parser (child `>`, sibling `+`, climb-up `^`, multiplication `*`, grouping `()`, `#id`, `.class`, `[attr]`, `{text}`, `$` numbering, implicit tag names) and HTML output; CSS abbreviations from Emmet's snippet table (MIT). Expansion on Tab in HTML-like and CSS-like languages when `emmet.triggerExpansionOnTab` is on (off by default, as in VS Code) and with the command.

### 9.11 Diff and merge

- `core/diff`: line diff (Myers, O(ND), with VS Code's heuristics to shift diffs to line boundaries and blank lines), refined to character changes inside changed line blocks; ignore trim whitespace; a time limit (`diffEditor.maxComputationTime`, 5 s) after which a coarser diff is shown. Three-way: parse conflict markers (`<<<<<<<`, `|||||||`, `=======`, `>>>>>>>`) into conflicts with base, current and incoming.
- `gui/diffeditor`: two editors side by side (or inline), scroll-synced, changed lines and characters colored, an overview ruler, next/previous change.
- `gui/mergeeditor`: incoming and current editors on top, the result editor below, with accept buttons per conflict and a "Complete merge" button that saves the result.

## 10. The workbench (`gui/workbench` and others)

### 10.1 Layout

Menu bar (File, Edit, Selection, View, Go, Help) at the top, activity bar on the left, primary side bar, editor area, panel, status bar. Part sizes, visibility and the panel position are saved per window (§10.10). Glutter draws the window decorations only where the compositor doesn't.

### 10.2 Widget kit (`gui/kit`)

Glutter has no tree view, split view or IDE-styled controls, so Emetos builds them on Glutter's `widgets` layer:

- `Theme`: an inherited widget carrying the resolved workbench colors and fonts.
- `SplitView` (resizable sashes, min sizes, double-click to reset), `Tree` (on Glutter's `TreeSliver`; virtualized, keyboard navigation, type-to-filter, multi-select, drag and drop), `List`, `Tabs`, `Button`, `IconButton`, `InputBox` (on Glutter's `EditableText`), `Toggle`, `Checkbox`, `Dropdown`, `ScrollableElement` (VS Code-style overlay scrollbars), `Hover`, `ContextMenu`, `ProgressBar`, `Badge`.
- Icons: VS Code's Codicons font, bundled and loaded through `RunAppOpts.Fonts` (D14), with the codepoints of its `codicon.csv` mapping generated into Go constants.
- Fonts: the workbench uses the desktop's UI font (`UIFont()`), then `sans-serif` (D14).

### 10.3 Editor groups and tabs

A grid of groups (nested split views). Each group has tabs (§3.2) and an active editor. Editors are documents (text editors), diff editors, merge editors, the settings UI, the keybindings UI and, later, others; all implement one `EditorInput` interface (id, title, dirty, save, revert, serialize for restore).

### 10.4 Activity bar and side bar views

Activity bar entries for Explorer (and, later, Search, Source Control, Run and Debug, Extensions), plus Accounts-free Manage menu (settings, keybindings, themes, profiles). Views are registered with an id, title, icon and container, so later areas add their own.

### 10.5 Explorer (`core/workspace`, `gui/views/explorer`)

- The tree model reads folders lazily, sorts like VS Code (`explorer.sortOrder`: folders first, case-insensitive natural order), hides `files.exclude` matches (glob patterns with VS Code's syntax), and watches opened folders with inotify (`golang.org/x/sys/unix`), coalescing events every 50 ms. Watching falls back to polling a folder when the inotify limit is reached, with a notice.
- File operations (new, rename, move, copy, delete to trash, paste) are undoable from the explorer (Ctrl+Z when the explorer has focus), and open editors follow renames and moves.

### 10.6 Quick input (`core/fuzzy`, `gui/quickinput`)

One quick input widget with modes by prefix, as in VS Code: no prefix (files), `>` (commands), `:` (go to line), `?` (help), `@` (symbols, area 3), `#` (workspace symbols, area 3), `view ` (views). File search lists the folder's files in the background (respecting `files.exclude` and `search.exclude`), scores with VS Code's fuzzy scorer (consecutive matches, word starts, path separators), and shows recently opened files first. Command palette lists commands with their keybindings and recently used commands first.

### 10.7 Notifications and dialogs

Notifications (`core/notify`) have a severity, message, actions, an optional progress, and a source. Toasts show at the bottom right, at most 3 at once; the notification center keeps them. Modal dialogs (unsaved changes, overwrite, delete) are in-window, keyboard operable.

### 10.8 Icons for files

VS Code's file icon themes are a font plus a JSON mapping (file extensions, file names, language ids and folder names to font characters and colors, with light-theme variants). Emetos bundles Seti, VS Code's default, loads its font through `RunAppOpts.Fonts`, and reads the mapping in VS Code's icon theme format, so the explorer, tabs, breadcrumbs and quick open show the same icons as VS Code. "None" (no file icons) is the other choice.

### 10.9 Zen mode, full screen, zoom

Zen mode (Ctrl+K Z) hides the activity bar, side bar, panel, status bar and tabs, centers the layout, and goes full screen (`zenMode.*` settings). Window zoom (Ctrl+= / Ctrl+-) scales the whole UI; editor font zoom (`editor.mouseWheelZoom`) changes only the editor.

### 10.10 State, backups and restore (`core/storage`)

- Paths (XDG): settings and user files in `~/.config/emetos/` (or the profile's folder, §10.12); state in `~/.local/state/emetos/`; cache in `~/.cache/emetos/`.
- UI state (per folder and global): open editors and their groups, view state per editor (selections, scroll, folds), layout, recently opened folders and files. Saved as JSON on change (debounced) and on exit.
- **Hot exit** (`files.hotExit`: `onExit` by default): a dirty document is backed up to `~/.local/state/emetos/backups/<window>/` one second after its last change (content, encoding, version). On start, backups are restored into dirty editors. Backups are removed when the document is saved or reverted.

### 10.11 Command line and windows (`cmd/emetos`, `core/ipc`)

`emetos [options] [paths...]`: `-n`/`--new-window`, `-r`/`--reuse-window`, `-g`/`--goto file:line[:col]`, `-d`/`--diff a b`, `-m`/`--merge path1 path2 base result`, `-w`/`--wait` (exit when the files are closed, for `git commit`), `--profile <name>`, `--version`, `--help`. A running instance listens on a Unix socket in `$XDG_RUNTIME_DIR`; a second `emetos` hands its arguments over and exits (or waits with `--wait`). Multiple windows are Glutter windows of one process.

### 10.12 Profiles

A profile is a folder with its own `settings.json`, `keybindings.json`, `snippets/` and UI state, under `~/.config/emetos/profiles/<name>/`; the default profile is `~/.config/emetos/` itself. A window uses one profile; switching reloads the window's settings. Export/import is a single JSON file.

## 11. Configuration

### 11.1 Settings (`core/jsonc`, `core/settings`)

- `settings.json` files are JSON with comments and trailing commas. `core/jsonc` parses them with positions, and edits a value while keeping comments and formatting (as VS Code's `jsonc-parser` does).
- Layers, lowest to highest: defaults, user (profile), workspace (`<folder>/.emetos/settings.json` only, D12), and language-specific blocks (`"[go]": {...}`) inside each layer.
- Every setting is registered with key, type, default, description, enum values and range; unknown keys are kept and reported in the settings file (later through the Problems panel). Change events carry the affected keys.
- The settings UI shows registered settings by category with search, edits user or workspace values, and links to the JSON.

### 11.2 Commands and context keys (`core/command`, `core/contextkey`)

- Commands have an id (VS Code's ids where the command exists in VS Code, e.g. `editor.action.copyLinesDownAction`), a title, a category, a precondition, and a handler. Menus, keybindings and the palette all run commands by id.
- Context keys (e.g. `editorTextFocus`, `editorHasSelection`, `findWidgetVisible`, `explorerViewletFocus`, `resourceExtname`) are scoped by focus, and when-clauses use VS Code's syntax: `!`, `&&`, `||`, `==`, `!=`, `=~` (regex), `<`, `>`, `in`, `not in`, parentheses.

### 11.3 Keybindings (`core/keybinding`)

- The default keymap is VS Code's Linux keymap for the commands Emetos has.
- `keybindings.json` entries: `key` (with chords such as `ctrl+k ctrl+c`), `command` (a leading `-` removes a default), `when`, `args`.
- Keys are matched by the logical key Glutter reports for the Latin layout, so shortcuts work on any keyboard layout. A chord's first key shows "(Ctrl+K) was pressed. Waiting for second key of chord..." in the status bar.
- The keybindings UI lists every command with its key, source (default/user) and when-clause; it searches by name and by recorded key, and edits `keybindings.json`.

## 12. Privacy

- Emetos never opens a network connection of its own: no telemetry, crash reports, update checks, extension galleries or remote fonts. `internal/archtest` fails the build when a package imports `net/http` (§5.3).
- Links in notifications or help open in the user's browser through the desktop portal; that is the only path to the network, and only on a click.
- Logs stay on disk (§13).

## 13. Errors and logging

- Every error uses `github.com/rm4n0s/errors` (CLAUDE.md). Errors shown to the user become notifications with a short message; the full error (`ToJson()`) goes to the log.
- Logs: `log/slog` to `~/.local/state/emetos/logs/<date>/main.log` (kept 7 days), and to the "Emetos" channel of the Output panel. `emetos --log debug` raises the level.
- A panic in a background goroutine is recovered, logged, and reported once as an error notification; the window stays open. A panic in the UI goroutine is handled by Glutter's error reporting, logged, and Emetos tries to save backups (§10.10) before exiting.

## 14. Performance targets

Measured on release builds (`-tags glutter_release`) on the user's machine with its GPU; the numbers and the method go in `docs/PERFORMANCE.md`.

| Metric | Target |
|---|---|
| Typing a character in a 10,000-line Go file with highlighting: UI-thread time of the frame that shows it | < 4 ms |
| Scrolling a 100,000-line file: frames | display refresh rate, no UI-thread frame over 8 ms |
| Opening a 10 MB file: first frame with text | < 300 ms |
| Opening a 200 MB file in large file mode: first frame with text | < 3 s |
| Cold start with no folder, to the first frame | < 400 ms |
| Idle, window focused, cursor blinking | < 1% of a core; no frames at all when the window has no focus |
| Resident memory, one 10,000-line file open | < 200 MB (checked against Glutter's own baseline in step 3.6; the target is revisited there) |
| Fuzzy file search over 100,000 paths, per keystroke | < 30 ms, off the UI thread |

## 15. Testing

- Core packages: table-driven unit tests, fuzz tests for the buffer (random edits checked against a plain string), the JSONC parser, the snippet parser, the when-clause parser, and the tokenizer (no panics, bounded time).
- Tokenizer and themes: tests against expected tokens for each bundled grammar, from small fixture files per language (`testdata/tokenize/<lang>/`), compared as text.
- UI: `gluttertest` widget tests with the fake platform (keys through `SendKeyEvent`, IME through `SendTextInputEvent`, clipboard, file choosers, window close), and golden images with `MatchesGolden`.
- Golden images need deterministic fonts: UI tests load Go Mono (`golang.org/x/image/font/gofont/gomono`, also bold and italic), Codicons and Seti through `gluttertest.Opts.Fonts`, and set `editor.fontFamily` to Go Mono, so no system font is used (D14).
- Display tests (`emetos_display` tag) start the real app under a headless Wayland compositor for proof screenshots (`GLUTTER_SCREENSHOT`, Glutter's self-capture).
- Performance benchmarks (§14) are Go benchmarks plus scripts that run the app with `GLUTTER_FRAME_STATS=1`.

## 16. Build and runtime requirements

- Build: Go 1.27 only. `CGO_ENABLED=0 go build ./cmd/emetos` makes the binary.
- Run: what Glutter needs (Wayland or X11, Vulkan or OpenGL ES, fontconfig), a monospace font, and optionally xdg-desktop-portal for file dialogs and opening links.

## 17. Licensing and third-party assets

Emetos is MIT-licensed (`LICENSE`). Bundled assets are listed in `NOTICE`, each with its license in `third_party/<name>/`:

| Asset | Source | License |
|---|---|---|
| TextMate grammars and language configurations | VS Code repository `extensions/*` (pinned commit); each grammar's upstream is listed in VS Code's `cgmanifest.json` | MIT, and the grammar's own license (checked per grammar when it is added) |
| Color themes | VS Code `extensions/theme-defaults` | MIT |
| Default keybindings and setting defaults | VS Code (data, not code) | MIT |
| Go module grammar | `golang/vscode-go` | MIT |
| Emmet snippets | `@emmetio/snippets` | MIT |
| Seti file icons (font and mapping) | VS Code `extensions/theme-seti` | MIT |
| Codicons (font and `codicon.csv`) | `microsoft/vscode-codicons` | CC BY 4.0 |
| Go Mono (tests only, not bundled) | `golang.org/x/image/font/gofont` | BSD-3-Clause |

## 18. Milestones

| Milestone | Content |
|---|---|
| M0 | Skeleton: module, packages, lint, boundary tests, an empty window |
| M1 | Text core: buffer, files, document, undo, editing commands |
| M2 | Configuration: JSONC, settings, commands, context keys, keybindings |
| M3 | Editor widget: rendering, input, IME, clipboard, scrolling, first performance check |
| M4 | Languages: regex, TextMate, tokenization, themes, brackets, indentation, folding |
| M5 | Editor features: find, word wrap, whitespace, guides, minimap, sticky scroll, snippets, Emmet, large files |
| M6 | Workbench: widget kit, layout, groups and tabs, explorer, quick input, status bar, notifications, panels |
| M7 | Files and sessions: save lifecycle, external changes, hot exit, restore, command line, single instance, windows |
| M8 | UIs for settings, keybindings, themes, profiles; zen mode |
| M9 | Diff and merge editors |
| M10 | Performance pass and release of areas 1–2 |

The steps are in `docs/IMPLEMENTATION.md`.

## 19. Open decisions

Each has a recommendation; the user decides, and the answer moves to §4.

Q1–Q4 are decided (D10–D13).

| # | Question | Recommendation |
|---|---|---|
| Q5 | **Emetos's own extension model** (area 11) | Decide later; the command, context-key, view and settings registries are built so extensions can add to them |

## 20. Risks

| Risk | Mitigation |
|---|---|
| Text rendering through Glutter is too slow for an editor (shaping per paragraph, glyphs as paths) | Per-line paragraph cache, repaint boundary for the text, the performance check in step 3.6 before anything else is built on the editor; fixes go into Glutter (D8). Glutter's own numbers (41–58 µs per cold code line, 5–10 µs for an unchanged one with its shaped-run cache) fit the budget of §14 |
| TextMate grammars written for Oniguruma behave differently with another engine | Translation layer in `core/regex`, tokenization tests per bundled grammar, timeouts |
| Glutter is young; Emetos is its first large app | Workarounds are marked and listed (D8); Glutter fixes are made in the Glutter repository |
| The scope of "what VS Code has" grows | Areas 3–11 are outside this spec until planned; every feature in §3 is a row with a defined behavior |
| IME with a big document | Only the current line is sent to the input method (§9.3) |
