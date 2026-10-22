# Emetos: Implementation guide

This file breaks the milestones in `docs/SPEC.md` §18 into small steps. Each step is one unit of work for one coding agent session. Work goes strictly top to bottom. Every finished step is recorded in `docs/IMPLEMENTED.md`.

**Who this is for**: coding agents (and humans) implementing Emetos. You don't need to read the whole file; you need the Rules below and the one step you're doing.

---

## Rules

### Before you start a step

1. **Find your step.** It's the first step in this file that has no entry in `docs/IMPLEMENTED.md`. Do only that step. Never work on two steps at once, and never skip ahead.
2. **Read, in this order:**
   1. `CLAUDE.md`, all of it.
   2. The whole step in this file.
   3. Every section of `docs/SPEC.md` listed under **Read**.
   4. The VS Code files listed under **Reference**. They're under `reference/` (created by step 0.2). If `reference/` is missing, run `scripts/fetch-reference.sh`.
   5. For UI steps, the Glutter APIs the step uses, in Glutter's source in the Go module cache (`go list -m -f '{{.Dir}}' github.com/rm4n0s/glutter`), and Glutter's `docs/GUIDE.md`.
3. **Create the branch.**
   ```sh
   git switch main && git pull --ff-only
   git switch -c feat/step-<id>-<slug>     # e.g. feat/step-1.1-piece-tree
   ```

### While you work

4. **Do exactly the step's Tasks.** Don't do work that belongs to a later step, even if it looks easy. If a later step will need a hook, leave only a comment: `// TODO(step 5.2): ...`.
5. **Follow CLAUDE.md.** In particular: `rm4n0s/errors` for every error, no cgo, no Glutter import under `internal/core`, no network.
6. **Match VS Code.** When VS Code has the behavior, match its defaults, edge cases and command ids. Read its implementation under `reference/vscode` and port the algorithm when it is the right one (VS Code is MIT; a ported file gets a `// Ported from VS Code: <path>` line in its package doc or above the ported code, and `NOTICE` already covers it). Port the relevant cases of the VS Code tests listed under **Tests** as table-driven Go tests, and say how many in `docs/IMPLEMENTED.md`.
7. **Glutter gaps.** When Glutter is missing something or has a bug, work around it in Emetos, mark the code with `// Glutter: <what and why>`, add an entry to `docs/GLUTTER-ISSUES.md` (create it if it doesn't exist), and mention it in the report (CLAUDE.md "Glutter").
8. **Stop and ask the user** when any of these happens:
   - the step is ambiguous
   - the spec looks wrong or contradicts VS Code
   - a tool is missing (give the exact install command, e.g. `sudo dnf install sway grim`)
   - a new dependency seems necessary
   - something needs `sudo`
   - a check still fails after a real attempt to fix it

   Never weaken or delete a test, skip a check, or mark a step done just to get unblocked.

**Processes you start.** Every compositor, editor instance or language tool you start for a test, a proof or a check is stopped when you are done with it: run it under `timeout -k`, in its own process group, and stop it from `t.Cleanup` or `defer` in Go code. Before you finish a step, check with `pgrep -a` that nothing you started is still running. The user works on this desktop and sees every stray window.

### Finishing a step

9. **The standard checks must all pass.** Run them from the repo root:
   ```sh
   test -z "$(gofmt -l .)"
   go vet ./...
   golangci-lint run
   go test -race ./...
   CGO_ENABLED=0 go build ./...
   CGO_ENABLED=0 go test ./...
   ```
   If the step lists **Display tests**, run them too: `go test -race -tags emetos_display <packages>`, under `scripts/headless-wayland.sh` unless `WAYLAND_DISPLAY` is set and the step says to use the user's session.
10. **Proof images.** Only when the step has a **Proof** item:
    - Produce exactly the images it lists, in `docs/implemented-proof-images/`, named `<id>-<name>.png` (e.g. `3.2-editor-text.png`).
    - Use the method it names (A, B or C, see below).
    - **Open every image with your image-viewing tool** (the Read tool shows images) and check that it shows what **Proof** describes. If it doesn't, fix the code. The step isn't done.
    - Never draw, edit, crop or generate a proof image any other way, and never reuse an older image.
11. **Record the step.** On the step branch, add an entry to `docs/IMPLEMENTED.md`, using the format at the top of that file. Embed the proof images.
12. **Commit on the step branch.** Use one commit per step, with the message `Step <id>: <imperative summary>`:
    ```sh
    git add -A && git commit -m "Step 1.1: Add the piece tree buffer"
    ```
13. **Publish every finished step to `main`, right away.**
    - A step counts as finished only when its commit is on `origin/main`.
    - Publish before you start the next step. Never collect several steps into one publish, and never leave a finished step only on its branch.
    - Publish only after rules 9–12 are complete: checks green, proof verified, `IMPLEMENTED.md` entry committed.
    ```sh
    git switch main && git pull --ff-only
    git merge --ff-only feat/step-1.1-piece-tree
    git push origin main
    git branch -d feat/step-1.1-piece-tree
    ```
    If `--ff-only` fails, rebase the step branch onto `main`, run the standard checks again, and retry. If the push is rejected or fails (authentication, network), stop and tell the user. Don't start the next step until the push succeeded.
14. **Report to the user.** Give the step id, a two-line summary, the check results, the proof image paths, new `docs/GLUTTER-ISSUES.md` entries, and the commit hash now on `main`.

**Default "Done when".** A step without a **Done when** line is done when its Tasks are complete, its **Tests** are written and passing, its **Display tests** pass, its **Proof** images are verified, and the standard checks pass.

### Proof methods

| Method | Use it for | How |
|---|---|---|
| **A. Golden render** | a widget on its own (kit widgets, the editor with a given document) | A widget test renders it with `gluttertest` and the test fonts (SPEC §15) and writes the image with `Tester.Screenshot()` to `docs/implemented-proof-images/<file>.png` when run with `-proof` (a test flag added in step 3.1) |
| **B. App self-capture** | anything inside the Emetos window | Run `go run ./cmd/emetos <args>` with `GLUTTER_SCREENSHOT=docs/implemented-proof-images/<file>.png` (and `GLUTTER_SCREENSHOT_AFTER=500ms` when needed). The app saves its own window and exits. To reach a state, set `EMETOS_DEV_COMMANDS` (step 3.1): commands run after the first frame. If `WAYLAND_DISPLAY` is not set, use `scripts/headless-wayland.sh -- <command>` |
| **C. Desktop capture** | several windows, or things outside the window | `scripts/screenshot-headless.sh <file.png> -- <command...>` runs the command in a headless sway session and captures the output with `grim` |
| **C (user desktop)** | things only a real desktop shows: an IME candidate window, a portal file dialog, the GPU's numbers | Ask the user to run the command given in the step and save a screenshot at the given path. Then view and verify the image yourself |

`EMETOS_DEV_COMMANDS` is a `;`-separated list of `commandId` or `commandId {json args}`, plus `sleep <duration>`. It is read only in debug builds (`foundation.DebugMode`), never in release builds.

---

## Milestone overview

| Milestone (SPEC §18) | Steps |
|---|---|
| M0 Skeleton | 0.1–0.3 |
| M1 Text core | 1.1–1.7 |
| M2 Configuration | 2.1–2.4 |
| M3 Editor widget | 3.1–3.6 |
| M4 Languages | 4.1–4.10 |
| M5 Editor features | 5.1–5.7 |
| M6 Workbench | 6.1–6.10 |
| M7 Files and sessions | 7.1–7.5 |
| M8 Settings, keybindings, themes, profiles | 8.1–8.4 |
| M9 Diff and merge | 9.1–9.3 |
| M10 Release of areas 1–2 | 10.1–10.2 |

---

## M0: Skeleton

### 0.1 Module and package layout
**Goal:** an empty but complete repository skeleton that passes all checks.
**Read:** SPEC §5.2, §5.3, §17; CLAUDE.md "Layout".
**Tasks:**
1. Create every package directory of SPEC §5.2 with a `doc.go` whose package comment states its purpose and SPEC section. `cmd/emetos/main.go` is an empty `main`.
2. Add `.golangci.yml` (golangci-lint v2 format): the standard set plus `revive` with `exported` and `package-comments`, `gofmt` as formatter, `run.go: "1.27"`.
3. Add `internal/archtest` with table tests that parse the imports of every package with `go/parser` to enforce SPEC §5.3: no Glutter or `internal/gui` import under `internal/core`, no `net/http`/`net/rpc`/`crypto/tls` anywhere, `net` only in `internal/core/ipc`, no `import "C"`, `regexp2` only in `internal/core/regex`.
4. Add `NOTICE` (Emetos MIT; the rows of SPEC §17 as "to be added by step x.y" until their assets arrive) and `third_party/README.md`.
5. Add `docs/IMPLEMENTED.md`'s first entry.
**Tests:** the archtest cases, each with a fixture package in `testdata` that violates the rule and is reported.
**Done when:** the standard checks pass, and `go mod tidy` leaves `go.mod` with only the dependencies in use.

### 0.2 Reference sources and asset scripts
**Goal:** a pinned local copy of VS Code's sources for porting, and the scripts that copy bundled assets from it.
**Read:** SPEC §8.1, §8.4, §10.2, §10.8, §17.
**Files:** `scripts/fetch-reference.sh`, `scripts/update-assets.sh`, `reference/VERSION` (git-ignored).
**Tasks:**
1. Add `reference/` to `.gitignore`.
2. `scripts/fetch-reference.sh` (bash, `set -euo pipefail`): if `reference/VERSION` exists, check out exactly the commits recorded there; otherwise clone, with `--depth 1 --filter=blob:none --sparse`, the latest release tag of `https://github.com/microsoft/vscode` (sparse: `src/vs/base/common`, `src/vs/editor/common`, `src/vs/editor/contrib`, `src/vs/platform/contextkey`, `src/vs/platform/keybinding`, `src/vs/workbench/services/textfile`, `src/vs/base/browser/ui/codicons`, `extensions`, `LICENSE.txt`, `ThirdPartyNotices.txt`, `cgmanifest.json`), `https://github.com/microsoft/vscode-textmate` (`src`, `LICENSE.md`), `https://github.com/golang/vscode-go` (`extension/syntaxes`, `LICENSE`), `https://github.com/emmetio/emmet` (`src`, `snippets`, `LICENSE`). Write `reference/VERSION` with every repository's commit hash, date and tag.
3. `scripts/update-assets.sh`: copies the assets a step names from `reference/` into the package that embeds them, and their licenses into `third_party/<name>/`. It starts with no assets; later steps add theirs to it.
4. Run the script; check every sparse path exists. If one is missing, find it with `git -C reference/vscode ls-tree -r --name-only HEAD | grep <name>` and **ask the user** before changing the paths.
**Done when:** `scripts/fetch-reference.sh` runs twice in a row (the second run uses `VERSION`), `git status` shows `reference/` ignored, and the standard checks pass.

### 0.3 First window
**Goal:** `go run ./cmd/emetos` opens an empty Emetos window with the bundled fonts loaded; headless runs and screenshots work.
**Read:** SPEC §4 (D10, D14), §10.1, §13, §16; Glutter's `widgets/run_app.go` (`RunAppOpts`, `Fonts`), `README.md` "What runs where".
**Files:** `cmd/emetos`, `internal/gui/app`, `internal/gui/kit/fonts`, `internal/core/logs`, `scripts/headless-wayland.sh`, `scripts/screenshot-headless.sh`.
**Tasks:**
1. Copy `scripts/headless-wayland.sh` and `scripts/screenshot-headless.sh` from Glutter's module directory (same author; keep their header comments, change `glutter` to `emetos` where it names the project).
2. Bundle Codicons (`codicon.ttf` from `src/vs/base/browser/ui/codicons/codicon/`) and the Seti font (`extensions/theme-seti/icons/seti.woff`) through `update-assets.sh` into `internal/gui/kit/fonts` with `embed`, and their licenses into `third_party/`. If Glutter can't load WOFF, convert it to TTF in `update-assets.sh` with a small Go program under `scripts/woff2ttf/` (WOFF 1.0: zlib-compressed tables) and keep the TTF.
3. `internal/core/logs`: `slog` setup writing to `~/.local/state/emetos/logs/<date>/main.log` (SPEC §13), keeping 7 days, with an in-memory channel the Output panel will read later.
4. `internal/gui/app.Run(ctx, opts)`: `widgets.RunApp` with `AppID: "dev.emetos.Emetos"`, title "Emetos", size 1280×800, the bundled fonts in `RunAppOpts.Fonts`, and a `WidgetsApp` whose home is an empty dark surface with a centered Codicon (`codicon-code`) and the text "Emetos". `cmd/emetos` parses `--version`, `--help` and `--log <level>` with the standard `flag` package and calls it.
**Tests:** a widget test that pumps the home and finds the text; a test that every bundled font loads (`gluttertest.Opts.Fonts`); logs tests (file path, retention).
**Proof:** method B, `0.3-first-window.png`: the window with the icon and "Emetos" in the center.
**Done when:** the proof is verified and the standard checks pass.

---

## M1: Text core

### 1.1 Piece tree buffer
**Goal:** `core/buffer` with the piece tree and snapshots of SPEC §6.1.
**Read:** SPEC §6.1, §6.2.
**Reference:** `src/vs/editor/common/model/pieceTreeTextBuffer/` (`pieceTreeBase.ts`, `rbTreeBase.ts`).
**Tasks:**
1. Piece tree with chunks and an append-only add buffer; nodes keep subtree byte length and line-feed count.
2. Insert, delete, replace; `LineCount`, `LineContent`, `LineLength`, `OffsetAt`, `PositionAt`, `Text(range)`, chunk and line iterators.
3. Persistent nodes: `Snapshot()` in O(1), safe from any goroutine; edits copy the path they change.
4. Building from a reader in 64 KiB chunks without splitting a UTF-8 sequence or a `\r\n`.
**Tests:** port the cases of `src/vs/editor/test/common/model/pieceTreeTextBuffer/pieceTreeTextBuffer.test.ts` that apply; a fuzz test of random edits against a plain string (content, line count, every position conversion); snapshots unchanged by later edits; race test reading a snapshot while editing.
**Done when:** the standard checks pass and a benchmark (`BenchmarkInsertTyping`, `BenchmarkLineContent` on a 100,000-line buffer) is recorded in `IMPLEMENTED.md`.

### 1.2 Positions, columns and boundaries
**Goal:** the position types and the text measurements every later step uses.
**Read:** SPEC §6.2, §7.2.
**Reference:** `src/vs/editor/common/core/cursorColumns.ts`, `src/vs/editor/common/core/wordCharacterClassifier.ts`, `src/vs/editor/common/core/wordHelper.ts`.
**Tasks:**
1. `buffer.Position`, `Range`, `Selection` with comparisons, contains, intersect, union, and the edge cases of empty ranges.
2. `core/edit`: visual column ↔ byte column with tab size (tabs to the next stop, wide characters 2, combining marks 0, using `golang.org/x/text/width`); grapheme cluster boundaries (go-text's segmenter); word boundaries by `editor.wordSeparators`; word parts (camelCase, snake_case).
**Tests:** port VS Code's `cursorColumns` tests; tables for every boundary function over ASCII, CJK, emoji with modifiers and ZWJ, combining marks, tabs.

### 1.3 Files
**Goal:** `core/fileio` per SPEC §6.3.
**Read:** SPEC §6.3, §6.4.
**Reference:** `src/vs/workbench/services/textfile/common/encoding.ts`.
**Tasks:**
1. Read: size check against §6.4 limits, binary detection, BOM detection, encoding decode (`golang.org/x/text/encoding` for the `files.encoding` list), line-ending detection and normalization (mixed → most common, reported), into a buffer.
2. Write: encode, line endings, BOM, atomic save with `fsync` and rename keeping the mode, in-place fallback, read-only detection with a tagged error.
3. Trash per the freedesktop.org specification (`$XDG_DATA_HOME/Trash/files` and `info/*.trashinfo`, name clashes, the same-filesystem rule; across filesystems, a tagged error that the caller turns into "delete permanently?").
4. The table of encodings with VS Code's ids and labels.
**Tests:** round trips for every encoding with and without BOM; mixed line endings; binary detection; size limits; atomic save keeps mode and survives a failing write (fault injection through an interface); trash name clashes; route tests for every error tag.

### 1.4 Documents and undo
**Goal:** `core/document` per SPEC §7.1.
**Read:** SPEC §7.1.
**Reference:** `src/vs/editor/common/model/textModel.ts` (edit operations, `pushEditOperations`), `src/vs/editor/common/model/editStack.ts`, `src/vs/editor/common/model/intervalTree.ts` (decorations).
**Tasks:**
1. `Document` with URI, buffer, version, saved version, dirty, language id, encoding, EOL, BOM, indentation options, read-only; created from `fileio` or empty (untitled).
2. Edit batches (non-overlapping replacements, validated) returning inverse batches; change events with the snapshot.
3. Undo/redo groups with selections before/after; grouping by a clock (500 ms pauses), cursor moves and edit kind; `PushStackElement` to close a group; dirty follows the saved version through undo.
4. Decorations in an interval tree with the four stickiness modes, moved by edits; owner ids; queries by range.
**Tests:** port the applicable cases of VS Code's `editStack` and `intervalTree` tests; undo/redo tables including dirty-state round trips; decorations under insert/delete at each edge for each stickiness.

### 1.5 Cursors and movement
**Goal:** selections and every cursor movement of SPEC §3.1 and §7.2.
**Read:** SPEC §3.1 "Cursors and selections", §7.2.
**Reference:** `src/vs/editor/common/cursor/cursorMoveOperations.ts`, `cursorMoveCommands.ts`, `cursorWordOperations.ts`.
**Tasks:**
1. `edit.Cursors`: a primary and secondary selections, sorted and merged when they touch, each with a desired visual column.
2. Movements with and without selection: character, word start/end, word part, line start (smart Home: first non-whitespace, then column 0), line end, page (through a `LineLayout` interface for visible line counts), document start/end, vertical with desired column (logical lines; wrapped lines come through `LineLayout` in step 5.2).
3. Select all, select line, cancel secondary selections, cursor undo/redo history (Ctrl+U).
**Tests:** port the cases of VS Code's `cursor.test.ts` and `wordOperations.test.ts` for these movements.

### 1.6 Editing commands
**Goal:** the editing and line commands of SPEC §3.1.
**Read:** SPEC §3.1 "Editing", "Line commands", §7.2.
**Reference:** `src/vs/editor/common/cursor/cursorTypeOperations.ts`, `cursorDeleteOperations.ts`, `src/vs/editor/contrib/linesOperations/browser/`.
**Tasks:**
1. Type (with overtype mode), backspace/delete by character, word and word part (with VS Code's whitespace rules), new line keeping the current indentation (language rules come in 4.8), tab/indent/outdent with `tabSize` and `insertSpaces`, detect indentation hook (implemented in 4.8).
2. Move/copy lines up/down, delete line, insert line above/below, join lines, transpose characters, sort lines, trim trailing whitespace, upper/lower/title case.
3. Each command is a pure function (snapshot, cursors, options) → (edit batch, new cursors) and works with multiple cursors.
**Tests:** port the cases of VS Code's `linesOperations.test.ts` and the typing/deleting parts of `cursor.test.ts`.

### 1.7 Multi-cursor commands
**Goal:** the multi-cursor commands of SPEC §3.1.
**Read:** SPEC §3.1 "Cursors and selections".
**Reference:** `src/vs/editor/contrib/multicursor/browser/multicursor.ts`, `src/vs/editor/contrib/smartSelect/browser/` (word and line parts only), `src/vs/editor/common/cursor/cursorColumnSelection.ts`.
**Tasks:**
1. Add cursor above/below, add selection to next occurrence (whole-word when the selection was empty), skip/move last occurrence, select all occurrences, insert cursors at line ends of a selection (Shift+Alt+I).
2. Column (box) selection by visual columns, with keyboard (Ctrl+Shift+Alt+arrows) and from a drag rectangle.
3. Expand/shrink selection by word, string/bracket content (bracket parts finished in 4.7) and line.
**Tests:** port the cases of `multicursor.test.ts` and the column-selection tests.

---

## M2: Configuration

### 2.1 JSON with comments
**Goal:** `core/jsonc` per SPEC §11.1.
**Read:** SPEC §11.1.
**Reference:** `src/vs/base/common/json.ts`, `jsonEdit.ts`, `jsonFormatter.ts`.
**Tasks:**
1. A scanner and a tolerant parser producing a tree with offsets (for errors and edits) and a list of errors; comments and trailing commas allowed.
2. `Decode(tree, &v)` into Go values; path queries (`["editor.fontSize"]`, `["[go]", "editor.tabSize"]`).
3. Edits: set, insert and remove a value by path, keeping comments and indentation (VS Code's `jsonEdit.setProperty` algorithm), returning text edits.
**Tests:** port `json.test.ts` and `jsonEdit.test.ts`; fuzz the parser (no panic, offsets in range).

### 2.2 Settings
**Goal:** `core/settings` per SPEC §11.1.
**Read:** SPEC §10.10, §11.1, §4 (D12).
**Reference:** `src/vs/platform/configuration/common/configurationModels.ts`, `configurationRegistry.ts`.
**Tasks:**
1. Registry: key, type (bool, number, integer, string, enum, array, object), default, description, enum descriptions, minimum/maximum, scope (application, window, resource, language-overridable), category.
2. Layers: defaults, user (`~/.config/emetos/settings.json` or a profile's), workspace (`<folder>/.emetos/settings.json`); language blocks in each; `Get[T](key, languageID)` with VS Code's precedence; unknown keys kept and reported.
3. Loading from files, reloading on change (the watcher arrives in 6.6; here a `Reload` call), change events with the affected keys.
4. `Update(key, value, target)` writes through `core/jsonc` edits.
5. Register the settings already used by M1 (`editor.tabSize`, `editor.insertSpaces`, `editor.detectIndentation`, `editor.wordSeparators`, `files.encoding`, `files.eol`, `files.autoGuessEncoding`) with VS Code's defaults and descriptions.
**Tests:** precedence tables; language blocks; invalid values fall back to defaults with a report; updates keep comments.

### 2.3 Context keys and when-clauses
**Goal:** `core/contextkey` per SPEC §11.2.
**Read:** SPEC §11.2.
**Reference:** `src/vs/platform/contextkey/common/contextkey.ts`, `scanner.ts`.
**Tasks:**
1. Scoped contexts (a tree: window → part → widget), set/reset keys, change events.
2. When-clause scanner and parser with VS Code's grammar and error messages; evaluation; normalization and the `keys()` a clause depends on.
**Tests:** port `contextkey.test.ts` and `parser.test.ts`/`scanner.test.ts`; fuzz the parser.

### 2.4 Commands and keybindings
**Goal:** `core/command` and `core/keybinding` per SPEC §11.2, §11.3.
**Read:** SPEC §11.2, §11.3.
**Reference:** `src/vs/platform/commands/common/commands.ts`, `src/vs/platform/keybinding/common/keybindingResolver.ts`, `keybindingsRegistry.ts`, `src/vs/base/common/keybindings.ts`.
**Tasks:**
1. Command registry: id, title, category, precondition (when-clause), handler with `args any` and a context; execution errors are tagged.
2. Key representation independent of Glutter (key names as in `keybindings.json`: `ctrl+shift+k`, `ctrl+k ctrl+c`, special keys), parsing and printing (with the labels shown in menus: `Ctrl+Shift+K`).
3. Resolver: default keymap + user `keybindings.json` (with `-command` removals and `args`), chords with a pending state, when-clause evaluation against a context, VS Code's precedence (later wins, user over default).
4. The default keymap table (`defaults.go`) with VS Code's Linux bindings for every command that exists so far (M1's commands get ids such as `cursorLeft`, `deleteLeft`, `editor.action.copyLinesDownAction`); each later step adds the bindings of its commands.
5. Register M1's commands with VS Code's ids and titles.
**Tests:** port `keybindingResolver.test.ts`; parsing round trips; chords; removals.

---

## M3: Editor widget

### 3.1 Application shell
**Goal:** `cmd/emetos <file>` opens one file in one editor area (no workbench yet); the services are wired; dev commands and proof capture work.
**Read:** SPEC §5.1, §10.10 (paths only), §15; this file's "Proof methods".
**Files:** `internal/gui/app`, `internal/gui/kit/theme.go`, `internal/gui/testutil`.
**Tasks:**
1. `internal/gui/app` builds the services (settings, context keys, commands, keybindings, logs) and passes them down through an inherited widget; the core never sees Glutter types.
2. `kit.Theme`: an inherited widget with the workbench colors Emetos uses so far, filled with Dark Modern's values (the real theme service replaces the values in step 4.5) and the fonts of D14 (desktop UI and monospace fonts from `PlatformDispatcher.UIFont()`/`MonospaceFont()`).
3. `EMETOS_DEV_COMMANDS` (debug builds only): parse and run after the first frame, with `sleep`.
4. `internal/gui/testutil`: a tester constructor with the test fonts (Go Mono regular/bold/italic/bold-italic from `golang.org/x/image/font/gofont`, Codicons, Seti), settings with `editor.fontFamily: "Go Mono"`, and the `-proof` flag of method A.
5. A placeholder editor widget that shows the file's lines as plain `Text` (replaced in 3.2), so the shell can be tested end to end.
**Tests:** shell widget test opening a file from `testdata`; dev-commands parsing; the testutil fonts are used (a golden of a monospace line).
**Proof:** method B, `3.1-shell.png`: a Go file's first lines in the window.

### 3.2 Text rendering
**Goal:** the editor's render object draws text, gutter, selections and cursors, and scrolls, per SPEC §9.1, §9.2.
**Read:** SPEC §9.1, §9.2, §9.4; Glutter: `rendering/box.go` (`InitRenderBox`), `rendering/editable.go` (how `RenderEditable` uses `ViewportOffset`), `widgets/scrollable.go`, `ui/text.go`, `rendering/error_box.go`.
**Tasks:**
1. `gui/editor` view model: document, cursors, scroll positions, line height from font metrics and `editor.lineHeight`.
2. `RenderEditorText`: a leaf `RenderBox` with a `ViewportOffset` (vertical, from `Scrollable`) and a horizontal offset; lays out the visible lines plus 10 lines of margin; one paragraph per line with tabs placed by Glutter's tab stops (`ParagraphStyleOpts.TabSize` from `editor.tabSize`); the LRU paragraph cache; repaint boundary. A `ui.ParagraphBuilder` can't be used after `Build`; make a new one per line.
3. Gutter with line numbers (`editor.lineNumbers`: on, relative, off), current line highlight (`editor.renderLineHighlight`), selections (rounded, multi-line, VS Code's colors), cursors (line style) in their own layer with blinking (`editor.cursorBlinking: blink`) through Glutter timers, stopped when the window has no focus.
4. Vertical and horizontal scrollbars in VS Code's overlay style (a first version of `kit.ScrollableElement`), `editor.scrollBeyondLastLine`.
5. Hit testing: position ↔ (line, byte column) through the paragraph queries, with the ASCII monospace fast path.
**Tests:** widget tests for layout of visible lines only (count the paragraphs built), scroll offsets, hit testing tables, cache hits after a one-line edit; goldens: plain text, selections across lines, multiple cursors, tabs, CJK and emoji line, relative line numbers.
**Proof:** method A, `3.2-editor-text.png`: a Go file with a multi-line selection, three cursors, line numbers and the current line highlight.

### 3.3 Keyboard and text input
**Goal:** keys run commands through the keybinding service; typing and IME go through the editor's `TextInputClient`.
**Read:** SPEC §9.3 "Keys", "IME"; Glutter: `widgets/focus_scope.go`, `services/hardware_keyboard.go`, `services/text_input.go` (`TextInputClient`, `TextInputConnection`), `widgets/editable_text_input.go` (how `EditableText` wires the connection).
**Tasks:**
1. `Focus` node with `OnKeyEvent`: convert Glutter key events to `core/keybinding` keys, resolve with the context (`editorTextFocus`, `editorHasSelection`, `editorHasMultipleSelections`, `editorReadonly`...), run the command, handle chords (status message hook for 6.9).
2. `TextInputClient` on the current line window (SPEC §9.3): typed text and IME commits become type commands; preedit shows as an underlined decoration; caret and composing rects updated after each frame; the window moves when the cursor changes line.
3. Bind every M1 command's default keys; undo/redo; overtype toggle (Insert).
**Tests:** widget tests through `SendKeyEvent` (with characters) for typing, every bound command, chords; IME through `SendTextInputEvent` (preedit, commit, delete-surrounding) and `TextInputStates()` (the window, caret rect).
**Manual checks:** ask the user to type in Greek and, if they have one, with an IBus or Fcitx5 input method (`go run ./cmd/emetos /tmp/x.txt`), and to confirm the candidate window follows the cursor.

### 3.4 Mouse
**Goal:** the mouse behavior of SPEC §9.3 "Mouse" and "Scrolling".
**Read:** SPEC §9.3; Glutter: `gestures/tap_and_drag.go`, `widgets/pointer_widgets.go`, `gestures/events_extra.go`, `widgets/drag_target.go`.
**Tasks:**
1. Click, double-click (word), triple-click (line) with drag extension by unit (`TapAndPanGestureRecognizer`), Shift+click, Alt+click (`editor.multiCursorModifier`), Shift+Alt+drag column selection, text cursor shape (`SystemMouseCursors.Text`).
2. Middle-click paste of the primary selection; drag and drop of selected text (move; Ctrl copies) with a drop-position caret.
3. Wheel with high-resolution deltas, touchpad pan, `editor.mouseWheelScrollSensitivity`, `editor.fastScrollSensitivity` with Alt, Shift+wheel horizontal, `editor.smoothScrolling` (animated), auto-scroll while drag-selecting past the edges.
**Tests:** widget tests with `StartGesture`, `TapAt` series, `ScrollAt`, trackpad gestures; drag-and-drop of text.

### 3.5 Clipboard and editor options
**Goal:** clipboard behavior and the display options of SPEC §3.1 "Display" that don't need later steps.
**Read:** SPEC §3.1 "Display", §9.3 "Clipboard"; Glutter: `services/clipboard.go`.
**Tasks:**
1. Copy, cut, paste with multiple cursors (`editor.multiCursorPaste`), empty-selection line copy/cut and paste above (`editor.emptySelectionClipboard`), primary selection on selection change; VS Code's clipboard metadata (whole-line flag) kept in memory for pastes from Emetos itself.
2. Settings applied live: `editor.fontFamily` (comma list → `FontFamily`/`FontFamilyFallback`), `editor.fontSize`, `editor.fontWeight`, `editor.lineHeight`, `editor.letterSpacing`, `editor.fontLigatures`, `editor.cursorStyle` (line, block, underline, thin variants), `editor.cursorBlinking` (blink, smooth, phase, expand, solid), `editor.cursorWidth`, `editor.lineNumbers`, `editor.renderLineHighlight`, `editor.scrollBeyondLastLine`, `editor.mouseWheelZoom` and font zoom commands.
**Tests:** clipboard tables with the fake clipboard (`ClipboardText`, `PrimaryText`); goldens for cursor styles and ligatures on/off.

### 3.6 Performance check
**Goal:** measure the editor against SPEC §14 before more is built on it.
**Read:** SPEC §14, §20; Glutter's `docs/PERFORMANCE.md` (how it measures, `GLUTTER_FRAME_STATS`).
**Files:** `docs/PERFORMANCE.md`, `scripts/perf.sh`, `internal/gui/editor/bench_test.go`.
**Tasks:**
1. Benchmarks (release tag): typing a character in a 10,000-line file, scrolling a page in a 100,000-line file, opening a 10 MB file to the first laid-out frame, with allocations.
2. `scripts/perf.sh`: runs the app on generated files with `GLUTTER_FRAME_STATS=1` in the headless compositor and in the user's session, and collects frame times, cold start, RSS and idle CPU.
3. Write `docs/PERFORMANCE.md` (machine, method, numbers, targets met/not met). For a target not met: profile, fix in Emetos when the cause is Emetos's, otherwise add an entry to `docs/GLUTTER-ISSUES.md` with the profile, and **ask the user** whether to go on.
**Manual checks:** ask the user to run `scripts/perf.sh --session` on their desktop with the GPU and to paste the summary; record it.
**Done when:** every §14 target that applies to M3 is measured on the user's machine, and each one is met or the user decided how to go on.

---

## M4: Languages

### 4.1 Regular expressions
**Goal:** `core/regex` per SPEC §8.2.
**Read:** SPEC §8.2, §4 (D11).
**Reference:** `reference/vscode-textmate/src/onigLib.ts`; Oniguruma's syntax as used by grammars.
**Tasks:**
1. A `Regex` type over `regexp2` with Oniguruma translation (`\h`, `\H`, `\A`, `\Z`, `\G` handled by the tokenizer through a start position, POSIX bracket classes, `(?x)` comments), compile cache, match at a position with a time limit, captures with byte offsets (regexp2 works in runes; convert).
2. An ECMAScript mode for find (`regexp2.ECMAScript`), with VS Code's replace-string syntax handled in 5.1.
3. A scanner over several patterns (`FindNextMatch(line, pos)`, as `vscode-oniguruma`'s `OnigScanner`) returning the earliest, then lowest-index match.
**Tests:** translation tables; byte/rune offset conversion with multi-byte text; timeouts; scanner tie-breaking.

### 4.2 TextMate tokenizer
**Goal:** `core/textmate` per SPEC §8.3.
**Read:** SPEC §8.3.
**Reference:** `reference/vscode-textmate/src/` (`grammar/`, `rule.ts`, `matcher.ts`, `theme.ts` for scope matching).
**Tasks:**
1. Grammar loading (JSON) and rule compilation with every construct of SPEC §8.3, lazily per rule.
2. The line tokenizer: (line, state) → (tokens with scope stacks, state); `\G` anchor; `while` rules; end-of-line handling; `$n` in names and in `end` patterns; injections; embedded grammars through a grammar registry.
3. State equality for incremental stops; a 20,000-byte line limit.
**Tests:** port vscode-textmate's grammar tests (`test-cases/first-mate`, the cases whose grammars are in `reference/`), compared as text; fuzz (no panic, bounded time).

### 4.3 Languages and the first grammars
**Goal:** `core/language` with the registry and the languages that come first (D13).
**Read:** SPEC §8.1, §17.
**Tasks:**
1. Registry and language detection per SPEC §8.1, with `files.associations`.
2. Language configuration loading (`language-configuration.json`, JSONC).
3. `update-assets.sh` copies, from `reference/vscode/extensions`, the grammars, language configurations and the language contributions of `package.json` (ids, extensions, file names, first lines, embedded languages) for: Go, Markdown (with its embedded code blocks for the bundled languages), JSON, JSON with Comments, YAML, Python, JavaScript, JavaScript React, TypeScript, TypeScript React; licenses into `third_party/`; `NOTICE` rows.
4. Tokenization fixtures in `internal/core/language/testdata/tokenize/<lang>/` (a sample file and the expected tokens as text), checked by a test; a `-update` flag regenerates them for review.
**Tests:** detection tables; each first language's fixtures; every bundled grammar compiles.

### 4.4 Tokenization in the editor
**Goal:** documents are tokenized in the background and the editor shows colors.
**Read:** SPEC §5.1, §8.3, §9.2.
**Reference:** `src/vs/editor/common/model/tokens/` (background tokenization).
**Tasks:**
1. `core/tokens`: per-document line states and compact tokens, invalidation on edit, a background goroutine in 5 ms slices working on snapshots, results posted with the version; synchronous tokenization of the visible lines; token-type bits (comment, string, regex).
2. A temporary token-to-color mapping from Dark Modern's `tokenColors` (replaced by the theme service in 4.5).
3. The editor builds paragraphs with token spans; the paragraph cache key includes the tokens.
**Tests:** tokens after edits equal a full re-tokenization (fuzzed edits on fixture files); the background loop yields between slices and drops stale results; goldens of a highlighted Go file.
**Proof:** method A, `4.4-highlighted-go.png`: a Go file in Dark Modern colors.

### 4.5 Themes
**Goal:** `core/theme` per SPEC §8.4, used by the editor and the kit.
**Read:** SPEC §8.4.
**Reference:** `reference/vscode-textmate/src/theme.ts`, `src/vs/workbench/services/themes/common/colorThemeData.ts`, `src/vs/platform/theme/common/colors/` (the color registry and defaults).
**Tasks:**
1. Theme loading with `include` chains, `colors`, `tokenColors` (inline or a file), scope selectors with VS Code's matching and precedence.
2. The workbench color registry: every id Emetos uses, with VS Code's defaults for dark, light, hc-dark and hc-light (values or derived: `transparent`, `lighten`, `darken`, `oneOf`).
3. Bundled themes via `update-assets.sh` (SPEC §8.4); user themes from `~/.config/emetos/themes/`; `workbench.colorTheme`, `window.autoDetectColorScheme`, `workbench.preferredDarkColorTheme`/`LightColorTheme`, following `MediaQueryPlatformBrightnessOf` live.
4. `kit.Theme` takes its colors from the theme service; token colors come from the theme.
**Tests:** port the selector-matching tests of vscode-textmate's `themes.test.ts`; resolution tables per bundled theme; registry defaults; goldens of the same Go file in each bundled theme.
**Proof:** method A, `4.5-themes.png`: the same Go file in Dark Modern, Light Modern and Dark High Contrast side by side.

### 4.6 The other bundled languages
**Goal:** every language of SPEC §8.1's second list is bundled and tested.
**Read:** SPEC §8.1, §17.
**Tasks:**
1. `update-assets.sh` adds the grammars, configurations and contributions of the second list (Go module files from `reference/vscode-go`), with licenses checked per grammar against `cgmanifest.json` and recorded in `NOTICE`. A grammar whose license isn't permissive is left out, and the user is told.
2. A tokenization fixture per language.
**Tests:** each language's fixtures; detection of its file names and extensions.

### 4.7 Brackets
**Goal:** `core/brackets` per SPEC §8.5, shown in the editor.
**Read:** SPEC §8.5, §9.4.
**Reference:** `src/vs/editor/common/model/bracketPairsTextModelPart/bracketPairsTree/` (`bracketPairsTree.ts`, `ast.ts`, `parser.ts`, `tokenizer.ts`).
**Tasks:**
1. The incremental bracket pair tree over tokens (ignoring brackets in comments and strings), as VS Code's.
2. Queries: matching bracket, enclosing pairs, nesting levels.
3. Editor: matching bracket highlight (`editor.matchBrackets`), jump to bracket (Ctrl+Shift+\\), select to bracket, colorization (`editor.bracketPairColorization.enabled`, colors 1–6 from the theme), bracket pair guides (`editor.guides.bracketPairs`); expand selection gains bracket and string steps (finishing 1.7).
**Tests:** port the cases of VS Code's `bracketPairsTree` tests; incremental equals full after fuzzed edits; goldens with colorized brackets and guides.

### 4.8 Indentation
**Goal:** `core/indent` per SPEC §8.6, used by typing and pasting; indent guides.
**Read:** SPEC §8.6, §3.1 "Indentation".
**Reference:** `src/vs/editor/common/model/indentationGuesser.ts`, `src/vs/editor/common/languages/autoIndent.ts`, `src/vs/editor/common/languages/enterAction.ts`, `src/vs/editor/common/model/guidesTextModelPart.ts`.
**Tasks:**
1. Indentation detection; `editor.detectIndentation` applies it on open; commands "Convert indentation to spaces/tabs", "Indent using spaces/tabs".
2. On-enter rules and indentation rules on new line and on typing (decrease indent on `}` and similar); `editor.autoIndent` levels; auto-indent on paste (`editor.formatOnPaste` is area 3; this is reindentation only).
3. Indent guides with the active guide (`editor.guides.indentation`, `editor.guides.highlightActiveIndentation`).
**Tests:** port `indentationGuesser.test.ts` and the applicable auto-indent cases; goldens with guides.

### 4.9 Auto-closing and comments
**Goal:** auto-closing, auto-surrounding, overtyping and comment toggling.
**Read:** SPEC §3.1 "Brackets", "Line commands"; §8.1 (language configuration).
**Reference:** `src/vs/editor/common/cursor/cursorTypeOperations.ts` (auto-closing), `src/vs/editor/contrib/comment/browser/`.
**Tasks:**
1. `editor.autoClosingBrackets`, `editor.autoClosingQuotes` (`languageDefined`, `beforeWhitespace`, `always`, `never`), `autoCloseBefore`, `notIn` by token type, `editor.autoSurround`, `editor.autoClosingOvertype`, `editor.autoClosingDelete`.
2. Toggle line comment (Ctrl+/), add/remove line comment, toggle block comment (Ctrl+Shift+A), with multi-cursor and the language's comment tokens.
**Tests:** port the auto-closing cases of `cursor.test.ts` and `lineCommentCommand.test.ts`/`blockCommentCommand.test.ts`.

### 4.10 Folding
**Goal:** `core/folding` per SPEC §8.7 and folding in the editor.
**Read:** SPEC §8.7, §3.1 "Folding".
**Reference:** `src/vs/editor/contrib/folding/browser/` (`indentRangeProvider.ts`, `syntaxRangeProvider.ts`, `foldingModel.ts`, `foldingRanges.ts`).
**Tasks:**
1. Indentation and marker providers, brackets as fallback; computed in the background on snapshots; region limit.
2. Folded regions as decorations; hidden lines in the view model (the editor skips them in layout, scrolling and vertical movement).
3. Commands: fold, unfold, toggle, fold/unfold recursively, fold all, unfold all, fold level 1–7, fold all block comments, fold all regions; gutter controls (`editor.showFoldingControls`: always, mouseover, never), the `…` marker after a folded line, click to unfold; cursor entering a folded region unfolds it.
**Tests:** port `indentRangeProvider.test.ts`, `foldingModel.test.ts`; goldens with folded regions and controls.
**Proof:** method B, `4.10-folding.png`: a Go file with two folded functions and the folding controls visible.

---

## M5: Editor features

### 5.1 Find and replace
**Goal:** `core/find` and the find widget per SPEC §9.8.
**Read:** SPEC §3.1 "Find and replace", §9.8.
**Reference:** `src/vs/editor/contrib/find/browser/` (`findModel.ts`, `findState.ts`, `replacePattern.ts`, `findWidget.ts`).
**Tasks:**
1. Model: matches on a snapshot (literal, whole word, case, regex in ECMAScript mode, multiline regex), current match, replace one/all with `$n`, `$&`, `\n`, `\t` and case-changing `\u \l \U \L`, preserve case, find in selection, the 19,999-match highlight cap.
2. The widget (top right overlay): input with history, toggles, count "3 of 42", previous/next, replace row toggle, close (Escape); `Ctrl+F` seeds from the selection (`editor.find.seedSearchStringFromSelection`), `F3`/`Shift+F3`, `Ctrl+H`, `Alt+Enter` selects all matches.
3. Match highlights in the text and the scrollbar.
4. `kit.InputBox` first version (on Glutter's `EditableText`) and `kit.Toggle`, used here.
**Tests:** port `findModel.test.ts` and `replacePattern.test.ts`; widget tests for each binding; goldens of the widget open with replace.
**Proof:** method A, `5.1-find.png`: the find widget with replace open, regex on, and highlighted matches.

### 5.2 Word wrap and rendering options
**Goal:** word wrap and the remaining display options of SPEC §3.1.
**Read:** SPEC §3.1 "Display", §7.2.
**Reference:** `src/vs/editor/common/viewModel/` (line breaks, `modelLineProjection.ts`), `src/vs/editor/browser/viewParts/` (whitespace, rulers).
**Tasks:**
1. `editor.wordWrap` (off, on, wordWrapColumn, bounded), `editor.wordWrapColumn`, `editor.wrappingIndent` (none, same, indent, deepIndent), `editor.wrappingStrategy`; line breaks computed per visible line through paragraph layout at the wrap width; the `LineLayout` used by `core/edit` for wrapped vertical movement and Home/End on wrapped lines.
2. `editor.renderWhitespace` (none, boundary, selection, trailing, all), `editor.renderControlCharacters`, `editor.rulers` (with colors), occurrence highlight of the word under the cursor (`editor.occurrencesHighlight`, `editor.selectionHighlight`).
**Tests:** wrapping tables (positions ↔ wrapped rows); cursor movement on wrapped lines; goldens for each option.

### 5.3 Minimap
**Goal:** the minimap of SPEC §9.5.
**Read:** SPEC §9.5.
**Reference:** `src/vs/editor/browser/viewParts/minimap/minimap.ts`.
**Tasks:**
1. Block rendering from tokens into a cached image updated by line ranges; scale 1–3; `editor.minimap.enabled`, `side`, `scale`, `showSlider` (mouseover, always), `renderCharacters: false` behavior (blocks only).
2. Slider, click to jump, drag to scroll; selections and find matches marked.
**Tests:** the image update covers only changed rows; slider math tables; golden of the minimap with a selection.

### 5.4 Sticky scroll and breadcrumbs
**Goal:** SPEC §9.6 and §9.7.
**Read:** SPEC §9.6, §9.7.
**Reference:** `src/vs/editor/contrib/stickyScroll/browser/`.
**Tasks:**
1. Sticky scroll from folding ranges (`editor.stickyScroll.enabled`, `maxLineCount`), drawn over the text with a shadow, click to jump.
2. Breadcrumbs bar above the editor (`breadcrumbs.enabled`), path segments relative to the folder (or the absolute path), each opening a sibling picker (a simple list overlay; it becomes a `quickinput`-style picker in 6.8).
**Tests:** sticky lines for scroll positions in nested code; goldens of both.
**Proof:** method A, `5.4-sticky-breadcrumbs.png`: a scrolled Go file with two sticky lines and the breadcrumbs bar.

### 5.5 Snippets
**Goal:** `core/snippet` per SPEC §9.9.
**Read:** SPEC §9.9.
**Reference:** `src/vs/editor/contrib/snippet/browser/` (`snippetParser.ts`, `snippetSession.ts`, `snippetVariables.ts`).
**Tasks:**
1. Parser for the full grammar; variables (`TM_*`, `CLIPBOARD`, `CURRENT_*` dates, `RANDOM*`, `UUID`, `LINE_COMMENT`/`BLOCK_COMMENT_*`, `WORKSPACE_*`); transforms with the regex engine.
2. Session: tab stops as decorations, Tab/Shift+Tab, mirrors, choices (a simple list overlay), nested snippets, end conditions; multi-cursor insertion.
3. Snippet files (`~/.config/emetos/snippets/`, per language and `*.code-snippets` with `scope`), reloaded on change; "Insert Snippet" (a picker; finished in 6.8); `editor.tabCompletion` (on, off, onlySnippets).
**Tests:** port `snippetParser.test.ts`, `snippetSession.test.ts`, `snippetVariables.test.ts`.

### 5.6 Emmet
**Goal:** `core/emmet` per SPEC §9.10.
**Read:** SPEC §9.10.
**Reference:** `reference/emmet/src` (abbreviation parser, markup and stylesheet output), `reference/emmet/snippets`.
**Tasks:**
1. Abbreviation parser and HTML output (indentation from the editor's options, `$` numbering, implicit tags by parent, self-closing per `emmet.syntaxProfiles`), CSS abbreviations from Emmet's snippets with fuzzy property matching; `update-assets.sh` copies the snippets and the license.
2. "Emmet: Expand Abbreviation" command, Tab expansion when `emmet.triggerExpansionOnTab` is on, in `emmet.includeLanguages` and the default HTML/CSS-like languages; abbreviation extraction before the cursor.
**Tests:** port a representative set of Emmet's own expansion tests (≥ 100 cases), as tables.

### 5.7 Large files and long lines
**Goal:** SPEC §6.4 end to end.
**Read:** SPEC §6.4, §14.
**Tasks:**
1. Large file mode switches per §6.4 with a notice hook (shown by 6.10), long-line drawing in pieces, the tokenization line limit, `editor.largeFileOptimizations`.
2. Binary files open as a notice editor with "Open anyway"; files over the limit fail with a tagged error.
3. Generated test files (in a temp dir, not committed): 200 MB, 300,000 lines, a 5 MB single line.
**Tests:** mode selection tables; widget test on a long-line document (layout stays bounded); benchmark opening the 200 MB file (SPEC §14 target).

---

## M6: Workbench

### 6.1 Widget kit, part 1
**Goal:** the kit widgets the layout needs (SPEC §10.2).
**Read:** SPEC §10.2, §4 (D9, D14).
**Reference:** `src/vs/base/browser/ui/` (`splitview/`, `scrollbar/`, `button/`, `hover/`, `contextview/`, `menu/`) for behavior and sizes.
**Tasks:**
1. Codicon constants generated from `codicon.csv` (`go generate`, a generator under `internal/gui/kit/internal/gen`).
2. `SplitView` (horizontal/vertical, sashes with `ResizeColumn`/`ResizeRow` cursors, min/max sizes, proportional resize, double-click reset, sizes in/out for persistence), `ScrollableElement` (finished), `Button`, `IconButton`, `Hover` (delayed, positioned, keyboard dismiss), `ContextMenu` (on Glutter's `RawMenuAnchor`/`MenuController`, keyboard navigation, separators, submenus, keybinding labels, checked items), `ActionBar`.
**Tests:** widget tests per widget (drag sashes, min sizes, menu keyboard navigation); goldens in Dark Modern and Light Modern.
**Proof:** method A, `6.1-kit.png`: a gallery of the part-1 widgets.

### 6.2 Widget kit, part 2
**Goal:** the remaining kit widgets.
**Read:** SPEC §10.2; Glutter: `widgets/sliver_tree.go`.
**Reference:** `src/vs/base/browser/ui/tree/`, `list/`, `inputbox/`, `toggle/`, `dropdown/`, `progressbar/`, `countBadge/`.
**Tasks:**
1. `Tree` on `TreeSliver`: lazy children, keyboard navigation (arrows, Home/End, PageUp/Down, Left/Right collapse/expand), type-to-filter/highlight, multi-select (Ctrl/Shift), drag and drop with drop feedback, indent guides, focus and selection colors.
2. `List` (virtualized, same keyboard model), `Tabs` (the tab strip widget, used by 6.4), `InputBox` (finished: validation messages, history), `Toggle`, `Checkbox`, `Dropdown`, `ProgressBar` (infinite and discrete), `Badge`.
**Tests:** keyboard tables for `Tree` and `List`; drag and drop; goldens.
**Proof:** method A, `6.2-kit.png`: a gallery of the part-2 widgets with an expanded tree.

### 6.3 Layout
**Goal:** the workbench layout of SPEC §10.1 and §10.4.
**Read:** SPEC §10.1, §10.4; Glutter: `material/menu_anchor.go` (`MenuBar`).
**Reference:** `src/vs/workbench/browser/layout.ts`, `src/vs/workbench/browser/parts/`.
**Tasks:**
1. Parts: menu bar (`window.menuBarVisibility`: classic, toggle with Alt, hidden), activity bar, primary side bar, editor area, panel (bottom/left/right, maximize), status bar; toggles (Ctrl+B, Ctrl+J, ...), sizes through `SplitView`; layout state in/out (persisted in 7.3).
2. View containers and views registry (id, title, icon, container); the Manage menu (settings, keybindings, themes, profiles entries as commands, implemented later); focus commands between parts and their context keys.
3. Menus registry (menu ids like VS Code's `MenubarFileMenu`, `EditorContext`, with when-clauses and groups), the editor's context menu.
4. The editor area shows one editor (groups come in 6.4).
**Tests:** layout toggles, sizes, focus movement, menus filtered by context.
**Proof:** method B, `6.3-layout.png`: the workbench with menu bar, activity bar, an empty explorer side bar, an editor, the panel and the status bar.

### 6.4 Editor groups and tabs
**Goal:** SPEC §10.3 and the "Editor groups" and "Tabs" rows of §3.2.
**Read:** SPEC §3.2, §10.3.
**Reference:** `src/vs/workbench/common/editor/editorGroupModel.ts`, `src/vs/workbench/browser/parts/editor/` (`editorPart.ts`, `multiEditorTabsControl.ts`, `editorCommands.ts`).
**Tasks:**
1. `EditorInput` interface and the text editor input; documents shared by editors with separate view models; a document lives while an editor shows it.
2. Groups grid (split right/down, move/copy editors, join, close, maximize, `workbench.editor.openSideBySideDirection`), focus by number (Ctrl+1..8).
3. Tabs per SPEC §3.2: preview (`workbench.editor.enablePreview`), pinned, dirty, close actions, drag to reorder and between groups (and to split by dropping on an edge), overflow scrolling, `workbench.editor.showTabs` (multiple, single, none), tab context menu.
4. Group model in/out for persistence (7.3).
**Tests:** port the applicable cases of `editorGroupModel.test.ts`; widget tests for splitting, moving, preview replacement, drag and drop.
**Proof:** method B, `6.4-groups.png`: three groups (two side by side, one below) with several tabs, one dirty and one preview.

### 6.5 Navigation
**Goal:** editor history and switching.
**Read:** SPEC §3.2 "Navigation".
**Reference:** `src/vs/workbench/services/history/browser/historyService.ts`.
**Tasks:**
1. Back/forward history of editor locations (Alt+Left/Right), with VS Code's rules for which cursor moves are recorded; last edit location (Ctrl+K Ctrl+Q).
2. Ctrl+Tab / Ctrl+Shift+Tab editor switcher (most recently used in the group), Ctrl+PageUp/PageDown, reopen closed editor (Ctrl+Shift+T).
**Tests:** history tables; switcher order.

### 6.6 Explorer
**Goal:** `core/workspace` and the explorer view (SPEC §10.5), without file operations.
**Read:** SPEC §3.2 "Explorer", §10.5.
**Reference:** `src/vs/workbench/contrib/files/browser/views/explorerView.ts`, `src/vs/workbench/contrib/files/common/explorerModel.ts`, `src/vs/base/common/glob.ts`.
**Tasks:**
1. Folder model with lazy reading, VS Code's sort orders, `files.exclude` globs (port `glob.ts`), compact folders (`explorer.compactFolders`).
2. inotify watcher with coalescing, recursive watches added as folders are read, polling fallback when the limit is reached; settings and snippets files are reloaded through it (finishing 2.2 and 5.5).
3. The view: tree with Seti icons, open on click (preview) and double-click (pinned), reveal the active file (`explorer.autoReveal`), collapse all, the Open Editors section, "Open Folder" (portal dialog from a goroutine) and the welcome content when no folder is open.
**Tests:** port `glob.test.ts`; model tables; watcher events in a temp dir; widget tests of the view.
**Proof:** method B, `6.6-explorer.png`: the explorer showing this repository with Seti icons and an open Go file revealed.

### 6.7 Explorer file operations
**Goal:** the file operations of SPEC §10.5.
**Read:** SPEC §3.2 "Explorer", §6.3, §10.5.
**Reference:** `src/vs/workbench/contrib/files/browser/fileActions.ts`, `src/vs/workbench/services/workingCopy/common/workingCopyFileService.ts`.
**Tasks:**
1. New file/folder (inline input), rename (F2, inline), delete to trash (Delete) and permanently (Shift+Delete) with confirmations (`explorer.confirmDelete`), cut/copy/paste with name clashes ("copy" suffixes), drag to move (`explorer.confirmDragAndDrop`), drop files from the file manager (copy into the folder), copy path / relative path.
2. Undo/redo of file operations (Ctrl+Z in the explorer), and editors follow renames, moves and deletes (dirty editors of deleted files stay open, marked deleted).
**Tests:** operation tables in a temp dir including undo; editors following renames; drop from outside through `gluttertest`'s `Drop`.

### 6.8 Quick input
**Goal:** SPEC §10.6.
**Read:** SPEC §3.2 "Navigation", §10.6.
**Reference:** `src/vs/base/common/fuzzyScorer.ts`, `src/vs/platform/quickinput/browser/`, `src/vs/workbench/contrib/search/browser/anythingQuickAccess.ts`.
**Tasks:**
1. `core/fuzzy`: port `fuzzyScorer.ts` (scores and highlights for labels and paths).
2. The quick input widget (top center overlay, input, list with highlights and keybinding labels, separators, buttons) and the prefixes `>`, `:`, `?`, `view `, none (files); recently opened files and used commands first.
3. File listing of the folder in the background (respecting excludes), with results per keystroke off the UI thread (SPEC §14 target); `Ctrl+P`, `Ctrl+Shift+P`/`F1`, `Ctrl+G`; the pickers of 5.4 and 5.5 move to it.
**Tests:** port `fuzzyScorer.test.ts`; widget tests for each prefix; a benchmark over 100,000 paths.
**Proof:** method B, `6.8-command-palette.png`: the command palette filtered by "line" with highlighted matches and keybindings.

### 6.9 Status bar
**Goal:** the status bar items of SPEC §3.2 and their pickers.
**Read:** SPEC §3.2 "Status bar".
**Tasks:**
1. Status bar items registry (left/right, priority, tooltip, command); items: Ln/Col and selection count, indentation, encoding, line endings, language mode, notifications bell, problem counts (zero until area 3), chord messages from 3.3.
2. Pickers: "Change Language Mode" (with "Configure File Association"), "Reopen with Encoding"/"Save with Encoding", "Change End of Line Sequence", indentation actions.
**Tests:** widget tests for each item and picker.

### 6.10 Notifications, dialogs and panels
**Goal:** SPEC §10.7 and the Output and Problems panels.
**Read:** SPEC §3.2 "Notifications", "Panels", §10.7, §13.
**Reference:** `src/vs/workbench/browser/parts/notifications/`, `src/vs/workbench/contrib/output/`, `src/vs/workbench/contrib/markers/` (structure only).
**Tasks:**
1. `core/notify` and the toasts, the notification center (Ctrl+K Ctrl+Shift+N in VS Code is unbound; use the bell and the command), do-not-disturb, progress notifications; the large-file and mixed-line-ending notices of earlier steps are shown through it.
2. In-window modal dialogs (message, detail, buttons, checkbox), keyboard operable.
3. Output panel with channels (the Emetos log from `core/logs`), Problems panel with its model (markers by resource, severity, filter) and no producers yet.
**Tests:** widget tests for toasts, actions, the center, dialogs, channel switching.
**Proof:** method B, `6.10-notifications.png`: an info toast with two actions, an error toast, and the Output panel showing the log.

---

## M7: Files and sessions

### 7.1 Save lifecycle
**Goal:** saving, reverting, closing with unsaved changes (SPEC §3.1 "Saving").
**Read:** SPEC §3.1 "Saving", §6.3; Glutter: `desktop/file_chooser.go`, `widgets/app_lifecycle_listener.go`, `widgets/window_controller.go` (close requests).
**Reference:** `src/vs/workbench/services/textfile/common/textFileEditorModel.ts`, `src/vs/workbench/contrib/files/browser/saveParticipants.ts` (trim/insert final newline parts).
**Tasks:**
1. Save, Save As (portal dialog on a goroutine; falls back to an in-window path prompt when the portal is missing), Save All, Revert; untitled documents (`Ctrl+N`, titles "Untitled-1"); save participants (`files.trimTrailingWhitespace`, `files.insertFinalNewline`, `files.trimFinalNewlines`).
2. Closing an editor or group with dirty documents asks Save/Don't Save/Cancel; the window close request (`AppLifecycleListener.OnExitRequested`, `OnWindowCloseRequested`) does the same for all, unless hot exit (7.4) applies.
3. Errors (read-only, permission, disk full) as notifications with "Retry", "Save As", "Overwrite" where they apply.
**Tests:** widget tests with `FakeDesktop().AnswerFileChooser`, `RequestWindowClose`, dialogs answered; participant tables.

### 7.2 Auto save and external changes
**Goal:** auto save modes and files changed on disk.
**Read:** SPEC §3.1 "Saving", "Files".
**Tasks:**
1. `files.autoSave` (`off`, `afterDelay` with `files.autoSaveDelay`, `onFocusChange`, `onWindowChange`).
2. On a watcher event for an open file: reload a clean document keeping the view state; for a dirty one, a notification with "Compare" (opens the diff editor in 9.2; until then "Overwrite"/"Revert") and save conflicts detected by modification time and size.
**Tests:** each auto-save mode with the fake clock and focus changes; external change tables.

### 7.3 UI state and restore
**Goal:** SPEC §10.10's UI state.
**Read:** SPEC §10.10.
**Tasks:**
1. `core/storage`: global and per-folder state files (JSON), debounced writes, atomic replace.
2. Saved and restored: layout (6.3), groups and editors (6.4) with view states (selections, scroll, folds), the explorer's expanded folders, recently opened folders and files (File > Open Recent, "Clear Recently Opened"), `window.restoreWindows`.
**Tests:** round trips of each state; restore with missing files (editors dropped, a notice).

### 7.4 Hot exit
**Goal:** SPEC §10.10's backups.
**Read:** SPEC §3.1 "Hot exit", §10.10.
**Reference:** `src/vs/workbench/services/workingCopy/common/workingCopyBackupService.ts`.
**Tasks:**
1. Backups one second after the last change (content, encoding, version, the saved file's mtime), removed on save/revert; `files.hotExit` (`off`, `onExit`, `onExitAndWindowClose`).
2. On exit with dirty documents and hot exit on: no prompt; on start: restore dirty editors from backups, including untitled ones; backups of a crashed run are restored too.
**Tests:** backup timing with the fake clock; restore round trips; a backup whose file changed on disk opens dirty with a notice.

### 7.5 Command line, single instance and windows
**Goal:** SPEC §10.11.
**Read:** SPEC §10.11; Glutter: `widgets/window_controller.go`, `widgets/window_manager.go`.
**Tasks:**
1. `core/ipc`: a Unix socket in `$XDG_RUNTIME_DIR/emetos-<uid>.sock` with a versioned JSON protocol; stale socket detection; a second `emetos` hands over its arguments (with the working directory) and exits, or waits for `--wait`.
2. The options of SPEC §10.11 (`--diff` and `--merge` open their editors once 9.2/9.3 exist; until then a notice).
3. Multiple windows as Glutter regular windows: "New Window" (Ctrl+Shift+N), opening a folder in a new or the same window (`window.openFoldersInNewWindow`), per-window state, closing the last window exits.
**Tests:** ipc tables with two processes started by the test (under `timeout -k`); argument parsing; multi-window widget tests (`Windows()`).
**Proof:** method C, `7.5-two-windows.png`: two Emetos windows with different folders.

---

## M8: Settings, keybindings, themes, profiles

### 8.1 Settings UI
**Goal:** the settings editor of SPEC §11.1.
**Read:** SPEC §3.2 "Settings", §11.1.
**Reference:** `src/vs/workbench/contrib/preferences/browser/settingsEditor2.ts`, `settingsTree.ts` (behavior only).
**Tasks:**
1. An editor input "Settings" (Ctrl+,): search box with filters (`@modified`, `@lang:go`), User/Workspace tabs, a category tree, rows per setting type (checkbox, number/text input, enum dropdown, "Edit in settings.json" for arrays and objects), modified markers and "Reset Setting".
2. "Open User Settings (JSON)", "Open Workspace Settings (JSON)", "Open Default Settings (JSON)" (read-only, generated from the registry).
**Tests:** widget tests: search, edit each kind, the JSON keeps comments after an edit.
**Proof:** method B, `8.1-settings.png`: the settings editor filtered by "font" with one modified setting.

### 8.2 Keybindings UI
**Goal:** the keybindings editor of SPEC §11.3.
**Read:** SPEC §3.2 "Keybindings", §11.3.
**Reference:** `src/vs/workbench/contrib/preferences/browser/keybindingsEditor.ts` (behavior only).
**Tasks:**
1. An editor input "Keyboard Shortcuts" (Ctrl+K Ctrl+S): table of command title, id, keybinding, when, source; search by text and by recorded keys (the record toggle); sort by precedence.
2. Change (record a key in a dialog, showing conflicts), add, remove, reset, change when expression; writes `keybindings.json` through `core/jsonc`; "Open Keyboard Shortcuts (JSON)".
**Tests:** widget tests for record, conflicts, each edit kind.
**Proof:** method B, `8.2-keybindings.png`: the keybindings editor searching "copy line" with one user binding.

### 8.3 Themes, zoom and zen mode
**Goal:** SPEC §3.2 "Themes", §10.8, §10.9.
**Read:** SPEC §8.4, §10.8, §10.9.
**Tasks:**
1. Color theme picker (Ctrl+K Ctrl+T) with live preview while moving through the list and revert on Escape; file icon theme picker (Seti, None) and the Seti mapping reader (VS Code's icon theme format, light variants).
2. Window zoom (`window.zoomLevel`, Ctrl+=/Ctrl+-/Ctrl+0), zen mode and its settings, full screen (F11), centered layout.
**Tests:** picker preview and revert; zoom levels; zen mode toggles and restores parts.
**Proof:** method B, `8.3-light-theme.png`: the workbench in Light Modern with Seti icons in the explorer.

### 8.4 Profiles
**Goal:** SPEC §10.12.
**Read:** SPEC §10.12.
**Tasks:**
1. Profiles in `core/storage` (create empty or from the current, rename, delete, switch per window, `--profile`), each with its settings, keybindings, snippets and UI state; the profile name in the status bar when it isn't the default.
2. Export/import as one JSON file (through the portal dialog).
**Tests:** switching changes the settings and keybindings in effect; export/import round trip.

---

## M9: Diff and merge

### 9.1 Diff algorithms
**Goal:** `core/diff` per SPEC §9.11.
**Read:** SPEC §9.11.
**Reference:** `src/vs/editor/common/diff/defaultLinesDiffComputer/` (`defaultLinesDiffComputer.ts`, `algorithms/myersDiffAlgorithm.ts`, `heuristicSequenceOptimizations.ts`), `src/vs/workbench/contrib/mergeEditor/browser/model/` (conflict parsing).
**Tasks:**
1. Line diff and character refinement with VS Code's default algorithm and heuristics, ignore trim whitespace, the time limit.
2. Conflict marker parsing into a three-way model; applying accept-current/incoming/both/base per conflict.
**Tests:** port `defaultLinesDiffComputer` test fixtures (`src/vs/editor/test/node/diffing/fixtures`), compared as text; conflict parsing tables.

### 9.2 Diff editor
**Goal:** `gui/diffeditor`.
**Read:** SPEC §3.1 "Diff editor", §9.11.
**Tasks:**
1. Side-by-side and inline (`diffEditor.renderSideBySide`), synced scrolling, line and character change colors, overview ruler, next/previous change (F7/Shift+F7 in VS Code's diff editor: Alt+F5/Shift+Alt+F5), `diffEditor.ignoreTrimWhitespace`.
2. "Compare with Saved", "Compare Active File With...", explorer "Select for Compare"/"Compare with Selected", `emetos --diff a b`, and "Compare" in 7.2's conflict notice.
**Tests:** widget tests for alignment and navigation; goldens.
**Proof:** method B, `9.2-diff.png`: a side-by-side diff of two versions of a Go file with character changes.

### 9.3 Merge editor
**Goal:** `gui/mergeeditor`.
**Read:** SPEC §3.1 "Merge editor", §9.11.
**Tasks:**
1. The three-pane layout (incoming, current, result), conflict navigation, accept incoming/current/both per conflict and for all, editing the result, "Complete Merge" (saves; with `--wait` the process exits).
2. Opening a file with conflict markers offers "Resolve in Merge Editor"; `emetos --merge path1 path2 base result`.
**Tests:** widget tests for each accept action and completion.
**Proof:** method B, `9.3-merge.png`: the merge editor with two conflicts, one resolved.

---

## M10: Release of areas 1–2

### 10.1 Performance pass
**Goal:** every target of SPEC §14 measured and met, or decided by the user.
**Read:** SPEC §14; `docs/PERFORMANCE.md`.
**Tasks:**
1. Run `scripts/perf.sh` for every target, profile the misses, fix them (or Glutter issues with profiles).
2. Update `docs/PERFORMANCE.md`.
**Manual checks:** the user runs the session measurement on their GPU and pastes the summary.

### 10.2 Release
**Goal:** a usable first release of areas 1 and 2.
**Read:** SPEC §1, §3.
**Tasks:**
1. `README.md`: what Emetos is, install (`go install github.com/rm4n0s/emetos/cmd/emetos@latest`), requirements, the feature list of areas 1–2, privacy, keybindings and settings locations, screenshots from the proof images.
2. A `.desktop` file and an install script for `~/.local/share/applications` with the icon.
3. Check every row of SPEC §3.1 and §3.2 against the implementation; list gaps in `IMPLEMENTED.md`.
4. **Ask the user** before tagging a version.
**Manual checks:** the user uses Emetos for a day of their own work and reports problems; each one becomes a fix before the tag.
