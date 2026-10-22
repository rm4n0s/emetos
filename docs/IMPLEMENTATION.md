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
   4. The files listed under **Reference** and **Tests**. They're under `reference/` (step 0.2). If `reference/` is missing, run `scripts/fetch-reference.sh`. Paths are written from the repository root, e.g. `reference/vscode/src/vs/base/common/glob.ts`.
   5. For UI steps, the Glutter APIs the step names, in Glutter's source in the Go module cache (`go list -m -f '{{.Dir}}' github.com/rm4n0s/glutter`), and Glutter's `docs/GUIDE.md`.
3. **Create the branch.**
   ```sh
   git switch main && git pull --ff-only
   git switch -c feat/step-<id>-<slug>     # e.g. feat/step-1.1-piece-tree
   ```

### While you work

4. **Do exactly the step's Tasks.** Don't do work that belongs to a later step, even if it looks easy. If a later step will need a hook, leave only a comment: `// TODO(step 5.3): ...`.
5. **Follow CLAUDE.md.** In particular: `rm4n0s/errors` for every error, no cgo, no Glutter import under `internal/core`, no network, isolated paths in every test and script.
6. **Match VS Code.** When VS Code has the behavior, match its defaults, edge cases and command ids. Read its implementation under `reference/vscode` and port the algorithm when it is the right one (a ported file gets a `// Ported from VS Code: <path>` line in its package doc or above the ported code). Port the relevant cases of the tests listed under **Tests** as table-driven Go tests:
   - Convert VS Code's 1-based lines and UTF-16 columns with `core/vstest` (step 1.3). Work out the expected positions of non-ASCII cases again for byte columns, with a comment saying so.
   - Skip a case that needs VS Code's TypeScript harness (a full editor or services), with a one-line reason in a comment.
   - In `docs/IMPLEMENTED.md`, say how many cases you ported and how many you skipped.
7. **Register what you add.** Every command, setting, default keybinding, workbench color and (from step 6.7 on) menu item that your step adds or first uses is registered with VS Code's id, title, default, description and when-clause:
   - settings: `reference/vscode/src/vs/editor/common/config/editorOptions.ts` for `editor.*`, the `registerConfiguration` calls of the matching contribution for the rest;
   - keybindings: the `registerKeybinding`/`KeybindingsRegistry`/`keybinding:` entries of the command's VS Code source (Linux values);
   - colors: `reference/vscode/src/vs/editor/common/core/editorColorRegistry.ts`, `reference/vscode/src/vs/workbench/common/theme.ts`, `reference/vscode/src/vs/platform/theme/common/colors/`.
8. **Glutter gaps.** When Glutter is missing something or has a bug, work around it in Emetos, mark the code with `// Glutter: <what and why>`, add an entry to `docs/GLUTTER-ISSUES.md` (create it if it doesn't exist), and mention it in the report (CLAUDE.md "Glutter").
9. **Stop and ask the user** when any of these happens:
   - the step is ambiguous
   - the spec looks wrong or contradicts VS Code
   - a tool is missing (give the exact install command, e.g. `sudo dnf install sway grim`)
   - a new dependency seems necessary
   - something needs `sudo`
   - a check still fails after a real attempt to fix it
   - the step says **ask the user**

   Never weaken or delete a test, skip a check, or mark a step done just to get unblocked. **Manual checks** don't stop the work: follow CLAUDE.md "Working through the steps".

**Processes you start.** Every compositor, editor instance or language tool you start for a test, a proof or a check is stopped when you are done with it: run it under `timeout -k`, in its own process group, and stop it from `t.Cleanup` or `defer` in Go code. Before you finish a step, check with `pgrep -a` that nothing you started is still running. The user works on this desktop and sees every stray window.

**Isolation.** Nothing you run may use the user's real Emetos configuration, state, cache, trash or socket (SPEC §15.1). Go tests call `storagetest.Isolate(t)` (step 0.3); scripts run the app through `scripts/isolated.sh` or `scripts/proof.sh` (step 0.4).

### Finishing a step

10. **The standard checks must all pass.** Run them from the repo root:
    ```sh
    test -z "$(gofmt -l .)"
    go vet ./...
    golangci-lint run
    go test -race ./...
    CGO_ENABLED=0 go build ./...
    CGO_ENABLED=0 go test ./...
    ```
    If the step lists **Display tests**, run them too: `scripts/headless-wayland.sh -- go test -race -tags emetos_display <packages>`.
11. **Proof images.** Only when the step has a **Proof** item:
    - Produce exactly the images it lists, in `docs/implemented-proof-images/`, named `<id>-<name>.png` (e.g. `3.3-cursors.png`), with the command the step gives.
    - **Open every image with your image-viewing tool** (the Read tool shows images) and check that it shows what **Proof** describes. If it doesn't, fix the code. The step isn't done.
    - Never draw, edit, crop or generate a proof image any other way, and never reuse an older image.
12. **Record the step.** On the step branch, add an entry to `docs/IMPLEMENTED.md`, using the format at the top of that file. Embed the proof images.
13. **Commit on the step branch.** Use one commit per step, with the message `Step <id>: <imperative summary>`:
    ```sh
    git add -A && git commit -m "Step 1.1: Add the piece tree"
    ```
14. **Publish every finished step to `main`, right away.**
    - A step counts as finished only when its commit is on `origin/main`.
    - Publish before you start the next step. Never collect several steps into one publish, and never leave a finished step only on its branch.
    - Publish only after rules 10–13 are complete: checks green, proof verified, `IMPLEMENTED.md` entry committed.
    ```sh
    git switch main && git pull --ff-only
    git merge --ff-only feat/step-1.1-piece-tree
    git push origin main
    git branch -d feat/step-1.1-piece-tree
    ```
    If `--ff-only` fails, rebase the step branch onto `main`, run the standard checks again, and retry. If the push is rejected or fails (authentication, network), stop and tell the user. Don't start the next step until the push succeeded.
15. **Report to the user.** Give the step id, a two-line summary, the check results, the proof image paths, new `docs/GLUTTER-ISSUES.md` entries, new manual checks, and the commit hash now on `main`. Then start the next step.

**Default "Done when".** A step without a **Done when** line is done when its Tasks are complete, its **Tests** are written and passing, its **Display tests** pass, its **Proof** images are verified, and the standard checks pass.

### Proof methods

| Method | Use it for | How |
|---|---|---|
| **A. Widget render** | a widget on its own (kit widgets, the editor with a given document) | A widget test renders it with `internal/gui/testutil` (test fonts, isolated paths) and, when run with `-proof`, writes `Tester.Screenshot()` as a PNG to `docs/implemented-proof-images/<file>.png`. Run only that package: `go test ./internal/gui/<pkg> -run <TestName> -proof` |
| **B. App self-capture** | anything inside one Emetos window | `scripts/proof.sh docs/implemented-proof-images/<file>.png [--config <dir>] [--after <duration>] [--commands '<json>'] -- <emetos args>`: builds a debug binary in a temporary folder, isolates the `XDG_*` folders (copying `<dir>` into the configuration folder), runs it in a headless compositor with `GLUTTER_SCREENSHOT`, and checks that the PNG was written. Fixtures (configuration, sample files) live in `testdata/proof/<step id>/`. `--commands` sets `EMETOS_DEV_COMMANDS` (SPEC §15.2). Glutter captures only the first window |
| **C. Output capture** | several windows, or things outside a window | `scripts/proof.sh <file> --output ...`: the same, but captures the whole headless output with `grim` (through `scripts/screenshot-headless.sh`) |
| **D. User desktop** | things only a real desktop shows: an IME candidate window, a portal file dialog | A manual check: the user runs the given command and saves a screenshot at the given path. When it arrives, view and verify it, and record it |

Every **Proof** item gives its exact command. A state no existing command reaches is reached by a `dev.*` command (SPEC §15.2) that the step adds.

---

## Milestone overview

| Milestone (SPEC §18) | Steps |
|---|---|
| M0 Skeleton | 0.1–0.4 |
| M1 Text core | 1.1–1.11 |
| M2 Configuration | 2.1–2.6 |
| M3 Editor widget | 3.1–3.11 |
| M4 Languages | 4.1–4.20 |
| M5 Editor features | 5.1–5.12 |
| M6 Workbench | 6.1–6.20 |
| M7 Files and sessions | 7.1–7.8 |
| M8 Settings, keybindings, themes, profiles | 8.1–8.6 |
| M9 Diff and merge | 9.1–9.8 |
| M10 Release of areas 1–2 | 10.1–10.2 |

---

## M0: Skeleton

### 0.1 Module and package layout
**Goal:** an empty but complete repository skeleton that passes all checks.
**Read:** SPEC §5.2, §5.3, §17; CLAUDE.md "Layout".
**Tasks:**
1. For every package of SPEC §5.2 that the plan fills, create the directory with a `doc.go` whose package comment states its purpose and SPEC section. `cmd/emetos/main.go` is an empty `main`. Directories that only group others (`internal/core`, `internal/gui`, `internal/gui/views`) get no `doc.go`.
2. Add `.golangci.yml` (golangci-lint v2 format): the standard set plus `revive` with `exported` and `package-comments`, `gofmt` as formatter, `run.go: "1.27"`.
3. Add `internal/archtest` with table tests that parse the imports and calls of every package with `go/parser` and enforce SPEC §5.3: no Glutter or `internal/gui` import under `internal/core`, no `net/http`/`net/rpc`/`crypto/tls`, `net` only in `internal/core/ipc`, no `import "C"`, `regexp2` only in `internal/core/regex`, no standard library `errors` import, no `fmt.Errorf` call. The checker is a function that takes a root folder, so each rule is tested on small fixture trees under `internal/archtest/testdata/` (gofmt-clean, never built).
4. Add `NOTICE` (Emetos MIT; the rows of SPEC §17, each marked with the step that adds its asset) and `third_party/README.md`.
5. Run `go mod tidy`: it removes the requirements nothing imports yet; the steps that first import them add them back (CLAUDE.md "Go conventions").
**Tests:** the archtest rules, each with a fixture that violates it and is reported, and one that passes.

### 0.2 Reference sources and asset scripts
**Goal:** the pinned references in `reference/`, and the scripts that check and copy from them.
**Read:** SPEC §17; `scripts/reference-versions.txt`.
**Files:** `scripts/fetch-reference.sh`, `scripts/update-assets.sh`, `scripts/check-references.sh`.
**Tasks:**
1. Add `reference/` to `.gitignore`.
2. `scripts/fetch-reference.sh` (bash, `set -euo pipefail`) reads `scripts/reference-versions.txt`:
   - `git` lines: `git init reference/<repo name>`, `git remote add origin https://github.com/<name>.git`, `git sparse-checkout set <paths>`, `git fetch --depth 1 --filter=blob:none origin <commit>`, `git checkout FETCH_HEAD`. Sparse paths: `microsoft/vscode`: `src/vs`, `extensions`, `LICENSE.txt`, `ThirdPartyNotices.txt`, `cgmanifest.json`; `golang/vscode-go`: `extension/syntaxes`, `extension/languages`, `LICENSE`; the others: everything.
   - `npm` lines: download the tarball and unpack it into `reference/<package name without scope>/` (e.g. `reference/codicons/package/dist/codicon.ttf`).
   - It is idempotent: a repository already at its commit is left alone.
3. `scripts/check-references.sh`: extracts every `reference/...` path from `docs/IMPLEMENTATION.md` and `docs/SPEC.md` (expanding `{a,b}` lists) and fails, listing them, when one doesn't exist.
4. `scripts/update-assets.sh`: copies the assets a step names from `reference/` into the package that embeds them, and their licenses into `third_party/<name>/`. It starts with no assets; later steps add theirs.
**Done when:** `scripts/fetch-reference.sh` runs twice in a row, `scripts/check-references.sh` passes (if a path is missing, **ask the user**: the plan names a wrong path), `git status` shows `reference/` ignored, and the standard checks pass.

### 0.3 Paths and logs
**Goal:** `core/storage` paths, the test isolation helper, and `core/logs`.
**Read:** SPEC §10.10 (paths), §13, §15.1.
**Tasks:**
1. `core/storage`: `Paths` computed from `XDG_CONFIG_HOME`, `XDG_STATE_HOME`, `XDG_CACHE_HOME`, `XDG_DATA_HOME`, `XDG_RUNTIME_DIR` (with the XDG defaults when unset), with the profile folders of SPEC §10.10.
2. `core/storage/storagetest.Isolate(t)`: sets every `XDG_*` variable to folders under `t.TempDir()` with `t.Setenv`.
3. `core/logs`: `slog` setup writing to the logs folder of SPEC §13, keeping 7 days, plus an in-memory channel store (name → lines, change events) that the Output panel will read.
**Tests:** paths with and without each variable; log file location, rotation by date, retention (with a clock); channel events.

### 0.4 First window and proof scripts
**Goal:** `go run ./cmd/emetos` opens an empty window with the bundled fonts loaded; isolated runs and proofs work.
**Read:** SPEC §4 (D10, D14, D15), §10.2 (icons), §15; Glutter: `widgets/run_app.go` (`RunAppOpts`, `Fonts`, `FontSource`), `README.md` "What runs where"; Glutter's `scripts/headless-wayland.sh` and `scripts/screenshot-headless.sh`.
**Reference:** `reference/codicons/package/dist/codicon.ttf`, `reference/codicons/package/dist/codicon.csv`, `reference/vscode/extensions/theme-seti/icons/seti.woff`.
**Tasks:**
1. Copy `scripts/headless-wayland.sh` and `scripts/screenshot-headless.sh` from Glutter's module directory (keep their header comments; change "Glutter" to "Emetos" where it names the project; `chmod +x`, as the module cache is read-only).
2. `scripts/isolated.sh [--config <dir>] -- <command>`: runs the command with every `XDG_*` variable (except `XDG_RUNTIME_DIR` when already set by the headless script) pointing into a fresh temporary folder, copying `<dir>` into `$XDG_CONFIG_HOME/emetos/`, and removes the folder afterwards.
3. `scripts/proof.sh` as described in this file's "Proof methods" (B and C). It builds `go build -o <tmp>/emetos ./cmd/emetos` first (a debug build, so dev commands work), and runs the binary, never `go run`.
4. `update-assets.sh` copies `codicon.ttf`, `codicon.csv` and `seti.woff` into `internal/gui/kit/fonts` (embedded; Glutter reads WOFF through go-text; if loading fails, stop and **ask the user**), with licenses in `third_party/`; `internal/gui/kit/fonts` exports the family names as constants (`Codicon`, `Seti`) and a `Sources()` list for `RunAppOpts.Fonts`; a `go generate` generator (`internal/gui/kit/fonts/internal/gen`) turns `codicon.csv` into constants (`fonts.CodiconCode` and the rest).
5. `internal/gui/app.Run(ctx, opts)`: `widgets.RunApp` with `AppID: "dev.emetos.Emetos"`, title "Emetos", size 1280×800, the bundled fonts, and a `WidgetsApp` whose home is an empty surface in Dark Modern's `editor.background` (`#1F1F1F`) with the `code` codicon and the text "Emetos" centered. `cmd/emetos` parses `--version` (from `debug.ReadBuildInfo`), `--help` and `--log <level>` and calls it.
**Tests:** a widget test that pumps the home and finds the text; every bundled font loads (`gluttertest.Opts.Fonts`); the generated codicon constants match the CSV; `scripts/isolated.sh` leaves no folder behind (a test that runs it with `env`).
**Proof:** method B, `scripts/proof.sh docs/implemented-proof-images/0.4-first-window.png`: the window with the icon and "Emetos" in the center.

---

## M1: Text core

### 1.1 Positions and the piece tree
**Goal:** the position types and a persistent piece tree with snapshots (SPEC §6.1, §6.2).
**Read:** SPEC §6.1, §6.2, §5.1.
**Reference:** `reference/vscode/src/vs/editor/common/model/pieceTreeTextBuffer/pieceTreeBase.ts`, `reference/vscode/src/vs/editor/common/model/pieceTreeTextBuffer/rbTreeBase.ts`, `reference/vscode/src/vs/editor/common/core/position.ts`, `reference/vscode/src/vs/editor/common/core/range.ts`, `reference/vscode/src/vs/editor/common/core/selection.ts`.
**Tasks:**
1. `buffer.Position`, `Range`, `Selection` with VS Code's comparisons, contains, intersect, union and empty-range rules (zero-based, byte columns).
2. Pieces over read-only chunks and an append-only add buffer, in a balanced tree whose nodes keep subtree byte length and line-feed count. Port VS Code's piece algorithms (splitting, inserting, deleting pieces), not its node structure: nodes are immutable once shared; an edit copies the path from the root (no parent pointers).
3. Insert, delete and replace at byte offsets; `Len`, `Text(start, end)` by offsets; `Snapshot()` in O(1), safe from any goroutine.
**Tests:** a fuzz test of random edits against a plain string (content after every edit); snapshots unchanged by later edits; a race test reading a snapshot while editing.

### 1.2 Line index and builder
**Goal:** line-based access and building a buffer from a reader.
**Read:** SPEC §6.1.
**Reference:** `reference/vscode/src/vs/editor/common/model/pieceTreeTextBuffer/pieceTreeBase.ts` (line lookups), `reference/vscode/src/vs/editor/common/model/pieceTreeTextBuffer/pieceTreeTextBufferBuilder.ts`.
**Tests (port):** `reference/vscode/src/vs/editor/test/common/model/pieceTreeTextBuffer/pieceTreeTextBuffer.test.ts` (the cases without `\r`: the buffer holds only `\n`).
**Tasks:**
1. `LineCount`, `LineContent`, `LineLength`, `OffsetAt(Position)`, `PositionAt(offset)`, `Text(Range)`, chunk and line iterators; positions past a line's end clamp as VS Code's `validatePosition`.
2. A builder from an `io.Reader` in 64 KiB chunks that never splits a UTF-8 sequence (the text is already `\n`-only; SPEC §6.1).
3. Benchmarks on a 100,000-line buffer: `BenchmarkInsertTyping` (one byte at a time in the middle), `BenchmarkLineContent`, `BenchmarkPositionAt`.
**Tests:** the ported cases; the fuzz test of 1.1 extended to every line and position conversion.
**Done when:** the standard checks pass and the benchmark numbers are recorded in `IMPLEMENTED.md`, with `BenchmarkInsertTyping` under 5 µs per insert and `BenchmarkLineContent` under 2 µs per line.

### 1.3 Columns, boundaries and the test helper
**Goal:** visual columns, grapheme, word and word-part boundaries, and `core/vstest`.
**Read:** SPEC §6.2, §7.2, §15.1.
**Reference:** `reference/vscode/src/vs/editor/common/core/cursorColumns.ts`, `reference/vscode/src/vs/editor/common/core/wordCharacterClassifier.ts`, `reference/vscode/src/vs/editor/common/core/wordHelper.ts`, `reference/vscode/src/vs/base/common/strings.ts` (`isFullWidthCharacter`, `isEmojiImprecise`).
**Tests (port):** the `CursorColumns` cases under `reference/vscode/src/vs/editor/test/common/` (search for `visibleColumnFromColumn`; there may be none, then write tables from `cursorColumns.ts`).
**Tasks:**
1. `core/edit` (imports `core/buffer`): visual column ↔ byte column exactly as SPEC §6.2; grapheme cluster boundaries (go-text's segmenter); word boundaries by `editor.wordSeparators`; word parts (camelCase, snake_case).
2. `core/vstest`: convert a VS Code (1-based line, UTF-16 column) to an Emetos position for a given text, and back; table tests over ASCII, Greek, CJK and astral-plane text.
**Tests:** the ported cases; tables for every boundary function over ASCII, CJK, emoji with modifiers and ZWJ, combining marks, tabs.

### 1.4 Reading files
**Goal:** `core/fileio` reading per SPEC §6.3, §6.4.
**Read:** SPEC §6.3, §6.4.
**Reference:** `reference/vscode/src/vs/workbench/services/textfile/common/encoding.ts`.
**Tasks:**
1. `Read(ctx, path)`: size check against SPEC §6.4 (tagged errors), binary detection, BOM detection, decoding (`golang.org/x/text/encoding`), line-ending detection and conversion to `\n` (`\r\n` and lone `\r`; mixed reported), `FileStat`; the large-file decision (size and line count) returned with the result.
2. The table of encodings with VS Code's ids and labels, keeping only those `x/text` has (list the dropped ones in `IMPLEMENTED.md`).
3. `files.autoGuessEncoding` behavior of SPEC §6.3 as an option of `Read`.
**Tests:** decoding of every encoding with and without BOM; line-ending tables; binary detection; size limits with an injected limit (no 256 MB files in tests); route tests for every error tag.

### 1.5 Writing files and the trash
**Goal:** `core/fileio` writing and trash per SPEC §6.3.
**Read:** SPEC §6.3.
**Tasks:**
1. `Write(ctx, path, text, opts)`: encode, line endings, BOM, atomic save with `fsync` and rename keeping the mode, in-place fallback when the folder isn't writable, `FileReadOnly` tagged error, `Overwrite` option (`chmod u+w`), returning the new `FileStat`.
2. Trash per the freedesktop.org specification, home trash only: `Trash(ctx, path) (TrashedItem, error)` (name clashes, `.trashinfo`, a tagged error for another filesystem), `Restore(ctx, TrashedItem)`, `Delete(ctx, path)` (permanent).
**Tests:** round trips with 1.4 for every encoding; mode kept; a failing write leaves the original intact (fault injection through an interface); trash name clashes and restore; route tests.

### 1.6 Documents and undo
**Goal:** `core/document` per SPEC §7.1 (without decorations).
**Read:** SPEC §7.1.
**Reference:** `reference/vscode/src/vs/editor/common/model/textModel.ts` (`pushEditOperations`, `_alternativeVersionId`), `reference/vscode/src/vs/editor/common/model/editStack.ts`, `reference/vscode/src/vs/editor/common/cursor/cursor.ts` (when a stack element is pushed).
**Tests (port):** `reference/vscode/src/vs/editor/test/common/model/editStack.test.ts`, the undo/redo and version cases of `reference/vscode/src/vs/editor/test/common/model/textModel.test.ts`.
**Tasks:**
1. `Document` with the fields of SPEC §7.1, URIs (`file:`, `untitled:`, `emetos-virtual:`), created from `fileio` results or empty.
2. Edit batches (non-overlapping, validated) returning inverse batches; change events with the snapshot.
3. Undo/redo groups with selections before/after, the merging rules of SPEC §7.1 (an edit-kind argument: typing, deleteLeft, deleteRight, other), `PushStackElement`; version and alternative version ids; dirty.
**Tests:** the ported cases; dirty-state round trips through undo/redo and save.

### 1.7 Decorations
**Goal:** decorations that move with edits.
**Read:** SPEC §7.1.
**Reference:** `reference/vscode/src/vs/editor/common/model/intervalTree.ts`.
**Tests (port):** `reference/vscode/src/vs/editor/test/common/model/intervalTree.test.ts`.
**Tasks:** an interval tree of decorations on the document with VS Code's four stickiness modes, owner ids, options (an opaque class name and data), queries by range and by owner; moved by every edit batch.
**Tests:** the ported cases; insert/delete at each edge for each stickiness.

### 1.8 Cursors and movement
**Goal:** cursors and every cursor movement of SPEC §3.1 and §7.2.
**Read:** SPEC §3.1 "Cursors and selections", §7.2.
**Reference:** `reference/vscode/src/vs/editor/common/cursor/cursorMoveOperations.ts`, `reference/vscode/src/vs/editor/common/cursor/cursorMoveCommands.ts`, `reference/vscode/src/vs/editor/common/cursor/cursorWordOperations.ts`.
**Tests (port):** the movement cases of `reference/vscode/src/vs/editor/test/browser/controller/cursor.test.ts` and `reference/vscode/src/vs/editor/contrib/wordOperations/test/browser/wordOperations.test.ts` that don't need wrapping.
**Tasks:**
1. `edit.Cursors`: a primary and secondary selections, sorted and merged when they touch, each with a desired visual column.
2. `edit.LineLayout` interface: `PageRows() int`, `MoveVertically(pos Position, rows int, desiredVisualCol int) Position`, `RowStart(pos) Position`, `RowEnd(pos) Position`; `edit.ModelLineLayout` implements it with one row per line.
3. Movements with and without selection: character, word start/end, word part, line start (smart Home), line end, page, document start/end, vertical with desired column; select all, select line, cancel secondary selections, cursor undo/redo history (Ctrl+U).
**Tests:** the ported cases.

### 1.9 Typing and deleting
**Goal:** typing, deleting and indenting commands.
**Read:** SPEC §3.1 "Editing", §7.2.
**Reference:** `reference/vscode/src/vs/editor/common/cursor/cursorTypeOperations.ts`, `reference/vscode/src/vs/editor/common/cursor/cursorDeleteOperations.ts`, `reference/vscode/src/vs/editor/common/cursorCommon.ts` (the options).
**Tests (port):** the typing and deleting cases of `reference/vscode/src/vs/editor/test/browser/controller/cursor.test.ts` that don't need language features.
**Tasks:**
1. `edit.Options` (one struct, SPEC §7.2) with tab size, insert spaces, word separators, and room for what later steps add.
2. Type (with overtype mode), composition type (replace the N characters before each cursor, as VS Code's `compositionType`), backspace/delete by character, word and word part, new line keeping the current indentation, tab/indent/outdent.
3. Each command: (snapshot, cursors, options) → (edit batch, new cursors, edit kind), for multiple cursors.
**Tests:** the ported cases.

### 1.10 Line commands
**Goal:** the line commands of SPEC §3.1 (without comments, which come in 4.18).
**Read:** SPEC §3.1 "Line commands".
**Reference:** `reference/vscode/src/vs/editor/contrib/linesOperations/browser/`.
**Tests (port):** `reference/vscode/src/vs/editor/contrib/linesOperations/test/browser/`.
**Tasks:** move/copy lines up/down, delete line, insert line above/below, join lines, transpose characters, sort lines ascending/descending, trim trailing whitespace, upper/lower/title case; multi-cursor.
**Tests:** the ported cases.

### 1.11 Multi-cursor commands
**Goal:** the multi-cursor commands of SPEC §3.1 (bracket and string steps of expand selection come in 4.13).
**Read:** SPEC §3.1 "Cursors and selections".
**Reference:** `reference/vscode/src/vs/editor/contrib/multicursor/browser/multicursor.ts`, `reference/vscode/src/vs/editor/common/cursor/cursorColumnSelection.ts`, `reference/vscode/src/vs/editor/contrib/smartSelect/browser/wordSelections.ts`.
**Tests (port):** `reference/vscode/src/vs/editor/contrib/multicursor/test/browser/multicursor.test.ts`, the column-selection cases of `reference/vscode/src/vs/editor/test/browser/controller/cursor.test.ts`.
**Tasks:**
1. Add cursor above/below, add selection to next occurrence (whole word when the selection was empty), skip/move last occurrence, select all occurrences, insert cursors at line ends (Shift+Alt+I).
2. Column selection by visual columns, from the keyboard and from a rectangle.
3. Expand/shrink selection by word and line.
**Tests:** the ported cases.

---

## M2: Configuration

### 2.1 JSONC parsing
**Goal:** `core/jsonc` parsing per SPEC §11.1.
**Read:** SPEC §11.1.
**Reference:** `reference/vscode/src/vs/base/common/json.ts`.
**Tests (port):** `reference/vscode/src/vs/base/test/common/json.test.ts`.
**Tasks:** a scanner and a tolerant parser producing a tree with offsets and an error list; comments and trailing commas allowed; `Decode(node, &v)` into Go values; path queries.
**Tests:** the ported cases; fuzz the parser (no panic, offsets in range).

### 2.2 JSONC editing
**Goal:** editing a JSONC document while keeping comments.
**Read:** SPEC §11.1.
**Reference:** `reference/vscode/src/vs/base/common/jsonEdit.ts`, `reference/vscode/src/vs/base/common/jsonFormatter.ts`.
**Tests (port):** `reference/vscode/src/vs/base/test/common/jsonEdit.test.ts`, `reference/vscode/src/vs/base/test/common/jsonFormatter.test.ts`.
**Tasks:** `SetValue(text, path, value, FormattingOptions{TabSize, InsertSpaces, EOL})` and `Remove(text, path, opts)` returning text edits, with VS Code's algorithm; `ApplyEdits`.
**Tests:** the ported cases.

### 2.3 Settings
**Goal:** `core/settings` per SPEC §11.1.
**Read:** SPEC §10.10, §11.1, §4 (D12).
**Reference:** `reference/vscode/src/vs/platform/configuration/common/configurationModels.ts`, `reference/vscode/src/vs/platform/configuration/common/configurationRegistry.ts`, `reference/vscode/src/vs/editor/common/config/editorOptions.ts`, `reference/vscode/src/vs/workbench/contrib/files/browser/files.contribution.ts`.
**Tests (port):** the applicable cases of `reference/vscode/src/vs/platform/configuration/test/common/configurationModels.test.ts`.
**Tasks:**
1. Registry: key, type, default, description, enum values and descriptions, minimum/maximum, scope, category.
2. A `Store` with the layers of SPEC §11.1 and language blocks; `settings.Get[T](s *Store, key, languageID string) T` (a function: Go methods can't have type parameters); an unregistered key panics in debug builds; invalid values fall back to the default and are reported.
3. Loading from files and `Reload()` (the watcher calls it from 6.13); change events with the affected keys.
4. `Update(ctx, key, value, target)` through `core/jsonc` edits; creates the file and its folder when needed (atomically, through `fileio`).
5. Register the settings M1 reads (`editor.tabSize`, `editor.insertSpaces`, `editor.detectIndentation`, `editor.wordSeparators`, `files.encoding`, `files.eol`, `files.autoGuessEncoding`) per rule 7.
**Tests:** the ported cases; precedence tables; language blocks; updates keep comments; isolation through `storagetest`.

### 2.4 Context keys and when-clauses
**Goal:** `core/contextkey` per SPEC §11.2.
**Read:** SPEC §11.2.
**Reference:** `reference/vscode/src/vs/platform/contextkey/common/contextkey.ts`, `reference/vscode/src/vs/platform/contextkey/common/scanner.ts`.
**Tests (port):** `reference/vscode/src/vs/platform/contextkey/test/common/`.
**Tasks:**
1. `Scope` API: `NewRoot()`, `scope.Child()`, `Set(key, value)`, `Reset(key)`, `Get(key)` (walks up), `OnChange(func(keys))`, `Dispose()`.
2. When-clause scanner and parser with VS Code's grammar and error messages; evaluation against a scope; the keys an expression depends on.
**Tests:** the ported cases; fuzz the parser.

### 2.5 Commands and keybindings
**Goal:** `core/command` and `core/keybinding` per SPEC §11.2, §11.3.
**Read:** SPEC §5.4, §9.3 "Keys", §11.2, §11.3.
**Reference:** `reference/vscode/src/vs/platform/commands/common/commands.ts`, `reference/vscode/src/vs/platform/keybinding/common/keybindingResolver.ts`, `reference/vscode/src/vs/platform/keybinding/common/keybindingsRegistry.ts`, `reference/vscode/src/vs/base/common/keybindings.ts`, `reference/vscode/src/vs/base/common/keyCodes.ts`, `reference/vscode/src/vs/editor/browser/coreCommands.ts`.
**Tests (port):** `reference/vscode/src/vs/platform/keybinding/test/common/keybindingResolver.test.ts`.
**Tasks:**
1. Command registry: metadata (id, title, category, precondition) registered first; `SetHandler(id, func(ctx command.Context, args any) error)` attached later by the implementing package; running a command without a handler is a tagged error.
2. Keys independent of Glutter: VS Code's key names (`ctrl+shift+k`, `ctrl+k ctrl+c`, special keys, US-layout punctuation names), parsing and printing (labels such as `Ctrl+Shift+K`).
3. Resolver: default keymap + user `keybindings.json` (removals with `-command`, `args`), chords with a pending state that modifier-only keys don't cancel, when-clauses against a scope, VS Code's precedence; a recording mode that hands keys to a recorder first.
4. The default keymap table (`defaults.go`); register the metadata and Linux bindings of M1's commands with VS Code's ids (from `coreCommands.ts` and the contributions of 1.8–1.11), per rule 7.
**Tests:** the ported cases; parsing round trips; chords; removals; recording mode.

### 2.6 Globs
**Goal:** `core/glob` with VS Code's glob patterns and `.gitignore` matching.
**Read:** SPEC §8.1, §10.5, §10.6.
**Reference:** `reference/vscode/src/vs/base/common/glob.ts`.
**Tests (port):** `reference/vscode/src/vs/base/test/common/glob.test.ts`.
**Tasks:**
1. Port `glob.ts`: patterns, `**`, braces, sibling clauses (`when`), expressions (`files.exclude` objects), case rules on Linux.
2. A `.gitignore`/`.ignore` matcher (gitignore semantics: negation, directory-only, anchoring, nested files).
**Tests:** the ported cases; gitignore tables.

---

## M3: Editor widget

### 3.1 Application shell
**Goal:** `cmd/emetos <file>` opens one file in one editor area (no workbench yet); per-process and per-window services exist; dev commands work.
**Read:** SPEC §5.1, §5.4, §10.7 (notify model), §15; this file's "Proof methods".
**Reference:** `reference/vscode/extensions/theme-defaults/themes/dark_modern.json` and its `include` chain (`dark_plus.json`, `dark_vs.json`).
**Tasks:**
1. `gui/app`: a process value (settings registry, command registry, keymap, logs, notify) and a `Window` value per Glutter window (documents, root context key scope, settings store with the workspace layer) passed down through inherited widgets; the core never sees Glutter types.
2. `core/notify`: notifications with severity, message, actions, source; a sink that logs them until the UI shows them (6.11).
3. `kit.Theme`: an inherited widget with these workbench colors, valued from Dark Modern's `colors` (resolved through its include chain by hand): `editor.background`, `editor.foreground`, `editor.lineHighlightBackground`, `editor.lineHighlightBorder`, `editor.selectionBackground`, `editor.inactiveSelectionBackground`, `editorCursor.foreground`, `editorLineNumber.foreground`, `editorLineNumber.activeForeground`, `editorGutter.background`, `editorWhitespace.foreground`, `editorIndentGuide.background1`, `editorRuler.foreground`, `scrollbarSlider.background`, `scrollbarSlider.hoverBackground`, `scrollbarSlider.activeBackground`, `scrollbar.shadow`, `focusBorder`, `foreground`, `widget.shadow`. Fonts per D14 (`UIFont()`, `MonospaceFont()`). Step 4.8 replaces the values with the theme service.
4. Dev commands (SPEC §15.2): parse `EMETOS_DEV_COMMANDS`, run after the first frame, only when `foundation.DebugMode` or the `emetos_dev` tag; a `dev.` command registration helper.
5. `gui/testutil`: a tester constructor with the test fonts (Go Mono regular, bold, italic, bold italic from `golang.org/x/image/font/gofont`; Codicons; Seti), isolated paths, settings with `editor.fontFamily: "Go Mono"`, and the `-proof` flag of method A (finds the repository root, PNG-encodes `Tester.Screenshot()`).
6. A placeholder editor widget that shows the file's lines as plain `Text` (replaced in 3.2), opened from `cmd/emetos <file>`.
**Tests:** shell widget test opening a file from `testdata`; dev-commands parsing and gating; the testutil fonts are used.
**Proof:** method B, `scripts/proof.sh docs/implemented-proof-images/3.1-shell.png -- testdata/proof/3.1/sample.go`: a Go file's first lines in the window.

### 3.2 View model and text layer
**Goal:** the editor's view model, line projection and text render object (SPEC §9.1, §9.2).
**Read:** SPEC §9.1, §9.2; Glutter: `rendering/box.go` (`InitRenderBox`), `rendering/editable.go` (`RenderEditable` and `ViewportOffset`), `widgets/scrollable.go`, `ui/text.go`, `ui/text_style.go`, `rendering/error_box.go`.
**Reference:** `reference/vscode/src/vs/editor/common/config/fontInfo.ts`, `reference/vscode/src/vs/editor/common/viewModel/modelLineProjection.ts`.
**Tasks:**
1. `gui/editor` view model: document, cursors, scroll positions, the line projection (identity for now, with the interface that folding, wrapping and view zones will extend: model line ↔ view rows, row heights), the line height rule of SPEC §9.2, and the `edit.LineLayout` implementation.
2. `RenderEditorText`: a leaf `RenderBox` with a vertical `ViewportOffset` (from `Scrollable`) and a horizontal offset; lays out the visible rows plus 10; one paragraph per row with `TabSize` from `editor.tabSize`; the LRU paragraph cache shared through the view model; repaint boundary.
3. The gutter with line numbers (`editor.lineNumbers`: on, relative, off) and its width rules.
4. View state (scroll position) read and restore.
**Tests:** only visible rows are built (count paragraph builds); cache hits after a one-line edit; scroll offsets; line height table; goldens: plain Go text, tabs, relative line numbers.

### 3.3 Selections and cursors
**Goal:** selections, cursors and the current line drawn as VS Code does.
**Read:** SPEC §9.1, §9.4; Glutter: `gestures/binding.go` (`NewTimer`), `widgets/binding.go` (`WidgetsBindingObserver`).
**Reference:** `reference/vscode/src/vs/editor/browser/viewParts/viewCursors/viewCursors.ts`, `reference/vscode/src/vs/editor/browser/viewParts/currentLineHighlight/currentLineHighlight.ts`, `reference/vscode/src/vs/editor/browser/viewParts/selections/selections.ts`.
**Tasks:**
1. Current line highlight (`editor.renderLineHighlight`), selections (rounded corners, multi-line, the inactive color when unfocused), cursors (line style) in their own layer.
2. Blinking (`editor.cursorBlinking: blink`) with Glutter timers, stopped when the window loses focus (`DidChangeViewFocus`) and restarted on any edit or move.
**Tests:** goldens: a multi-line selection, three cursors, the current line; blink timing with the fake clock; no timer while unfocused.
**Proof:** method A, `go test ./internal/gui/editor -run TestProofCursors -proof` → `3.3-cursors.png`: a Go file with a multi-line selection, three cursors, line numbers and the current line highlight.

### 3.4 Scrollbars and hit testing
**Goal:** VS Code-style scrollbars and position ↔ pixel mapping.
**Read:** SPEC §9.1, §9.2, §10.2.
**Reference:** `reference/vscode/src/vs/base/browser/ui/scrollbar/`.
**Tasks:**
1. `kit.ScrollableElement` (first version): overlay vertical and horizontal scrollbars with VS Code's sizes, fade, drag and click-to-page; `editor.scrollBeyondLastLine`.
2. Hit testing: pixel → (line, byte column) through the projection and the paragraph queries, with the ASCII monospace fast path; (line, column) → pixel rect.
**Tests:** scrollbar math tables and drag; hit testing tables (ASCII, tabs, a line with Greek text); CJK and emoji positions checked through the paragraph queries, not goldens.

### 3.5 Key dispatch
**Goal:** keys run commands through the window's key dispatcher (SPEC §5.4).
**Read:** SPEC §5.4, §9.3 "Keys", §11.2; Glutter: `widgets/focus_scope.go`, `widgets/focus_manager.go`, `services/hardware_keyboard.go`, `services/keyboard_key_gen.go`, `services/text_input.go` (`HandleUnhandledKey`).
**Tasks:**
1. The dispatcher: a `Focus` node at the window root; converts Glutter key events to `core/keybinding` keys (letters and digits from the logical key, other keys from the physical key's US-layout name, SPEC §9.3); resolves against the focused widget's context key scope; returns "handled" for bound keys, "ignored" otherwise so Glutter types the character; consumes Tab, arrows and Escape when bound, before focus traversal.
2. The editor's context key scope (`editorTextFocus`, `editorFocus`, `editorHasSelection`, `editorHasMultipleSelections`, `editorReadonly`, `editorLangId`) and the handlers of M1's commands (in `gui/editor`, calling `core/edit` and applying the result to the document).
3. A chord status hook (shown by the status bar in 6.20; logged until then).
**Tests:** every M1 command through `SendKeyEvent`; chords and modifier-only keys; a key on a non-Latin layout (a Greek character with the physical key of `/` still runs `ctrl+/`'s binding); unbound printable keys reach text input.

### 3.6 Text input and IME
**Goal:** typing and input methods through the editor's `TextInputClient` (SPEC §9.3 "IME").
**Read:** SPEC §9.3; Glutter: `services/text_input.go` (`TextInputClient`, `DeltaTextInputClient`, `TextInputConnection`), `services/text_input_configuration.go` (`EnableDeltaModel`), `widgets/editable_text_input.go`.
**Tasks:**
1. Attach on focus with the delta model; the current line as the editing value (line-local offsets); deltas become type/composition-type/delete commands at every cursor; the window moves when the primary cursor changes line.
2. Preedit as an underlined overlay at the primary cursor (not in the document); commit types the text.
3. `SetEditableSizeAndTransform` and `SetCaretRect` after every frame (Glutter's `SetComposingRect` doesn't reach the platform).
**Tests:** `SendTextInputEvent` (preedit, commit, delete-surrounding) and `TextInputStates()` (the window, the caret rect); typing through `SendKeyEvent` with characters.
**Manual checks:** the user types Greek, and, if they have one, uses an IBus or Fcitx5 input method in `scripts/isolated.sh -- go run ./cmd/emetos /tmp/x.txt`, and says whether the candidate window follows the cursor.

### 3.7 Mouse selection
**Goal:** selecting with the mouse (SPEC §9.3 "Mouse").
**Read:** SPEC §9.3; Glutter: `gestures/tap_and_drag.go` (`TapAndPanGestureRecognizer`), `widgets/gesture_detector.go`, `widgets/pointer_widgets.go`, `services/mouse_cursor.go`.
**Tasks:** click, double-click (word), triple-click (line) with drag extension by unit; Shift+click; Alt+click adds a cursor (`editor.multiCursorModifier`); Shift+Alt+drag column selection; the text cursor shape; register the settings per rule 7.
**Tests:** `TapAt` series, `StartGesture` drags for each unit, modifiers.

### 3.8 Scrolling and text drag
**Goal:** wheel and touchpad scrolling, middle-click paste and drag-and-drop of text.
**Read:** SPEC §9.3 "Scrolling", "Drag and drop of text"; Glutter: `gestures/pointer_signal_resolver.go`, `gestures/events_extra.go`.
**Tasks:**
1. The editor claims pointer signals through the `PointerSignalResolver`: wheel with high-resolution deltas, touchpad pan, `editor.mouseWheelScrollSensitivity`, Alt with `editor.fastScrollSensitivity`, Shift+wheel horizontal, `editor.smoothScrolling` (an animation), auto-scroll while drag-selecting past the edges.
2. Middle-click pastes the primary selection; text drag and drop per SPEC §9.3 (`editor.dragAndDrop`).
**Tests:** `ScrollAt`, trackpad gestures, smooth scrolling with the fake clock; drag and drop moves and copies; a press without movement inside a selection places the cursor.

### 3.9 Clipboard
**Goal:** clipboard behavior of SPEC §9.3.
**Read:** SPEC §9.3 "Clipboard"; Glutter: `services/clipboard.go`.
**Reference:** `reference/vscode/src/vs/editor/contrib/clipboard/browser/clipboard.ts`.
**Tasks:** copy, cut, paste with multiple cursors (`editor.multiCursorPaste`), empty-selection line copy/cut and line paste (`editor.emptySelectionClipboard`, the whole-line flag kept in memory for Emetos's own copies), the primary selection set on selection change; pastes applied in the clipboard callback on the UI thread.
**Tests:** tables with the fake clipboard (`ClipboardText`, `PrimaryText`).

### 3.10 Editor display options
**Goal:** the display settings of SPEC §3.1 "Display" that don't need later steps.
**Read:** SPEC §3.1 "Display", §9.2.
**Reference:** `reference/vscode/src/vs/editor/browser/viewParts/viewCursors/viewCursors.ts`, `reference/vscode/src/vs/editor/browser/viewParts/viewCursors/viewCursor.ts`, `reference/vscode/src/vs/editor/common/config/editorOptions.ts`.
**Tasks:** applied live: `editor.fontFamily` (comma list → `FontFamily`/`FontFamilyFallback`), `editor.fontSize`, `editor.fontWeight`, `editor.lineHeight`, `editor.letterSpacing`, `editor.fontLigatures`, `editor.cursorStyle` (all six), `editor.cursorBlinking` (blink, smooth, phase, expand, solid, as `viewCursors.ts`), `editor.cursorWidth`, `editor.mouseWheelZoom` and the font zoom commands.
**Tests:** goldens for each cursor style; the font features passed to the paragraph style for ligatures on/off (Go Mono has no ligatures, so not a golden); blinking modes with the fake clock.

### 3.11 Performance check
**Goal:** measure the editor against SPEC §14 before more is built on it.
**Read:** SPEC §14, §20; Glutter's `docs/PERFORMANCE.md` (method, `GLUTTER_FRAME_STATS`).
**Files:** `docs/PERFORMANCE.md`, `scripts/perf.sh`, `internal/gui/editor/bench_test.go`, `dev.*` commands for typing and scrolling.
**Tasks:**
1. Benchmarks (`-tags glutter_release`): typing a character in a 10,000-line file, scrolling a page in a 100,000-line file, opening a 10 MB file to the first laid-out frame; with allocations.
2. `dev.type {"text"}` and `dev.scroll {"rows"}` commands; `scripts/perf.sh [--session]` builds with `-tags glutter_release,emetos_dev`, generates the files in a temporary folder, runs the app isolated with `GLUTTER_FRAME_STATS=1` (headless by default, in the user's session with `--session`), drives it with dev commands, and collects frame times, cold start, RSS and idle CPU.
3. `docs/PERFORMANCE.md`: machine, method, numbers, and the targets of SPEC §14 marked "3.11" (typing without highlighting, scrolling, 10 MB open, cold start, idle, memory) as met or not. For a target not met: profile; fix it when the cause is Emetos's; otherwise add an entry to `docs/GLUTTER-ISSUES.md` with the profile and **ask the user** whether to go on.
**Manual checks:** the user runs `scripts/perf.sh --session` on their desktop and pastes the summary; record it in `docs/PERFORMANCE.md`.
**Done when:** every target marked 3.11 is measured headless and each one is met, or the user decided how to go on.

---

## M4: Languages

### 4.1 Regular expression translation
**Goal:** `core/regex` compiles every pattern of VS Code's grammars (SPEC §8.2).
**Read:** SPEC §8.2, §4 (D11).
**Reference:** `reference/vscode-textmate/src/onigLib.ts`; the grammars in `reference/vscode/extensions/*/syntaxes/`; regexp2's documentation in the module cache.
**Tasks:**
1. `regex.Compile(pattern, opts)` over regexp2 with the translations of SPEC §8.2, a compile cache, and an ECMAScript mode.
2. A test that walks every `match`, `begin`, `end` and `while` string of every grammar under `reference/vscode/extensions/*/syntaxes/` and `reference/vscode-go/extension/syntaxes/`, compiles it, and lists the failures; every failure is fixed in the translation or recorded as unsupported with a reason (the test then expects exactly that list). The test is skipped when `reference/` is missing.
**Tests:** translation tables; the corpus test.

### 4.2 Scanner and limits
**Goal:** the multi-pattern scanner the tokenizer uses (SPEC §8.2).
**Read:** SPEC §8.2.
**Reference:** `reference/vscode-textmate/src/onigLib.ts`, `reference/vscode-textmate/src/grammar/grammar.ts` (how `\G` and the first line are used).
**Tasks:** `NewScanner(patterns)`, `Find(line, pos, anchorPos, isFirstLine) (Match, bool)` with byte-offset captures; one rune conversion per line with invalid UTF-8 as U+FFFD and the byte mapping kept; the 100 ms limit with the `RegexTimeout` tag.
**Tests:** tie-breaking; `\G` and `\A`; byte offsets with multi-byte text and invalid UTF-8; a catastrophic pattern times out.

### 4.3 TextMate tokenizer
**Goal:** the grammar model and the core of the line tokenizer (SPEC §8.3).
**Read:** SPEC §8.3.
**Reference:** `reference/vscode-textmate/src/rule.ts`, `reference/vscode-textmate/src/grammar/grammar.ts`, `reference/vscode-textmate/src/grammar/grammarDependencies.ts`.
**Tasks:**
1. Grammar loading (JSON) into a model; rule compilation, lazily per rule; a `Registry` interface (scope name → grammar) that 4.5 implements.
2. The tokenizer for `match`, `begin`/`end`, captures (including captures with patterns), `contentName`, names with `$n`, `include` of every kind, `$n` in `end`; scope stacks; states that compare for equality; the 20,000-byte line limit.
**Tests:** the first-mate cases of `reference/vscode-textmate/test-cases/first-mate/` whose grammars use only these features (each test case carries its grammar), compared as text.

### 4.4 TextMate: while, injections, embedded languages
**Goal:** the rest of the tokenizer (SPEC §8.3).
**Read:** SPEC §8.3.
**Reference:** `reference/vscode-textmate/src/grammar/grammar.ts` (injections), `reference/vscode-textmate/src/matcher.ts`, `reference/vscode-textmate/src/grammar/basicScopesAttributeProvider.ts`, `reference/vscode/src/vs/workbench/services/textMate/`.
**Tasks:** `begin`/`while`; injections with selectors (`matcher.ts`); embedded grammars through the registry; the standard token type and the embedded language id of each token (SPEC §8.3); end-of-line handling as vscode-textmate.
**Tests:** the remaining first-mate cases; token type and language tables; a fuzz test (no panic, bounded time).

### 4.5 Languages and configurations
**Goal:** `core/language` registry, detection and language configurations (SPEC §8.1).
**Read:** SPEC §8.1.
**Reference:** `reference/vscode/src/vs/editor/common/languages/languageConfiguration.ts`, `reference/vscode/src/vs/workbench/contrib/codeEditor/common/languageConfigurationExtensionPoint.ts`, `reference/vscode/src/vs/editor/common/services/languagesRegistry.ts`.
**Tasks:**
1. The registry, filled from VS Code extension `package.json` contributions (`languages`, `grammars` with `embeddedLanguages`, `tokenTypes`, `injectTo`); detection per SPEC §8.1 with `core/glob` for `files.associations` and file-name globs; it implements `textmate.Registry`.
2. Language configuration loading (JSONC through `core/jsonc`; regexes in ECMAScript mode).
3. Register `files.associations` per rule 7.
**Tests:** detection tables (by association, name, extension, first line); configuration loading for a sample of VS Code's configuration files.

### 4.6 The first languages
**Goal:** the languages of D13 bundled and tokenized as VS Code does.
**Read:** SPEC §8.1, §15.1, §17.
**Reference:** `reference/vscode/extensions/{go,markdown-basics,json,yaml,python,javascript,typescript-basics}/` (`package.json`, `syntaxes/`, `language-configuration.json`, `cgmanifest.json`), `reference/vscode/extensions/vscode-colorize-tests/test/colorize-fixtures/`, `reference/vscode/extensions/vscode-colorize-tests/test/colorize-results/`.
**Tasks:**
1. `update-assets.sh` copies the contributions, grammars and configurations of those extensions into `internal/core/language/assets/` (embedded), with licenses and `NOTICE` rows.
2. An oracle test: for each colorize fixture of these languages, tokenize and compare every token's text and scopes with `colorize-results/<fixture>.json`. A mismatch is fixed, or (for a documented regexp2 difference) listed in an expected-differences file with the reason.
**Tests:** the oracle test; every bundled grammar compiles.

### 4.7 Theme loading and token colors
**Goal:** `core/theme` themes and token styles (SPEC §8.4).
**Read:** SPEC §8.4.
**Reference:** `reference/vscode-textmate/src/theme.ts`, `reference/vscode/src/vs/workbench/services/themes/common/colorThemeData.ts`, `reference/vscode/extensions/theme-defaults/`.
**Tests (port):** `reference/vscode-textmate/src/tests/themes.test.ts` (selector matching and resolution cases).
**Tasks:** theme JSON (JSONC) with `include` chains and `tokenColors` files; selectors and resolution as `theme.ts`; `update-assets.sh` bundles the six themes of SPEC §8.4; user themes from `$XDG_CONFIG_HOME/emetos/themes/`.
**Tests:** the ported cases; resolution tables per bundled theme.

### 4.8 Color registry and theme service
**Goal:** the workbench color registry and the theme the UI uses (SPEC §8.4).
**Read:** SPEC §8.4; Glutter: `widgets/media_query.go` (`MediaQueryPlatformBrightnessOf`).
**Reference:** `reference/vscode/src/vs/platform/theme/common/colorUtils.ts`, `reference/vscode/src/vs/platform/theme/common/colors/`, `reference/vscode/src/vs/editor/common/core/editorColorRegistry.ts`, `reference/vscode/src/vs/workbench/common/theme.ts`.
**Tasks:**
1. The registry with dark/light/hc-dark/hc-light defaults and derived values; register the ids of 3.1's list per rule 7.
2. A theme service (per process): `workbench.colorTheme`, `window.autoDetectColorScheme`, `workbench.preferredDarkColorTheme`/`LightColorTheme`; `gui/app` passes the platform brightness to it and follows changes.
3. `kit.Theme` takes its colors from the theme service.
**Tests:** registry defaults and derivations; theme switching with brightness changes; goldens of the editor in each bundled theme.
**Proof:** method A, `go test ./internal/gui/editor -run TestProofThemes -proof` → `4.8-themes.png`: the same text in Dark Modern, Light Modern and Dark High Contrast side by side (three `kit.Theme` wrappers in a row).

### 4.9 Tokenization in the editor
**Goal:** documents are tokenized in the background and the editor shows colors (SPEC §8.3).
**Read:** SPEC §5.1, §8.3, §9.2.
**Reference:** `reference/vscode/src/vs/editor/common/model/textModelTokens.ts`, `reference/vscode/src/vs/workbench/services/textMate/`.
**Tasks:**
1. `core/tokens`: line states, compact tokens with token type and language, invalidation on edit, the background loop in 5 ms slices on snapshots with stale results dropped, synchronous tokenization of the visible lines, re-tokenizing when the language changes; skipped in large file mode.
2. The editor builds paragraphs with token spans (the cache key includes the tokens) and repaints rows when background results arrive.
3. Re-measure the typing target of SPEC §14 with highlighting (`scripts/perf.sh`) and update `docs/PERFORMANCE.md`.
**Tests:** tokens after fuzzed edits equal a full re-tokenization; the loop yields between slices and drops stale results; goldens of a highlighted Go file.
**Proof:** method B, `scripts/proof.sh docs/implemented-proof-images/4.9-highlighted-go.png -- testdata/proof/4.9/sample.go`: a highlighted Go file in Dark Modern.

### 4.10 More languages: web and scripts
**Goal:** HTML, CSS, SCSS, Less, XML, SQL, Shell, Makefile, Dockerfile and INI bundled and tested.
**Read:** SPEC §8.1, §17.
**Reference:** `reference/vscode/extensions/{html,css,scss,less,xml,sql,shellscript,make,docker,ini}/`, the colorize fixtures and results.
**Tasks:** as 4.6 for these languages; HTML embeds CSS and JavaScript. A grammar whose license (`cgmanifest.json`) isn't permissive is left out, recorded in `IMPLEMENTED.md` and `NOTICE`, and named in the report.
**Tests:** the oracle test for each; detection of their file names and extensions.

### 4.11 More languages: systems and git
**Goal:** C, C++, Rust, Diff, Git commit, Git rebase, Ignore files and Go module files bundled and tested.
**Read:** SPEC §8.1, §17.
**Reference:** `reference/vscode/extensions/{cpp,rust,diff,git-base}/`, `reference/vscode-go/extension/syntaxes/go.mod.tmGrammar.json`, `reference/vscode-go/extension/syntaxes/go.sum.tmGrammar.json`, `reference/vscode-go/extension/languages/`.
**Tasks:** as 4.10; `go.work` files use the `go.mod` grammar; the C++ grammar gets a timing test (it is the heaviest).
**Tests:** the oracle test for each; small fixtures of Emetos's own for the languages without VS Code fixtures (`go.mod`, `go.sum`, Git rebase).

### 4.12 Bracket pairs
**Goal:** `core/brackets` (SPEC §8.5).
**Read:** SPEC §8.5.
**Reference:** `reference/vscode/src/vs/editor/common/model/bracketPairsTextModelPart/bracketPairsTree/`.
**Tests (port):** `reference/vscode/src/vs/editor/test/common/model/bracketPairColorizer/`.
**Tasks:** the incremental bracket structure over tokens (ignoring comments and strings); queries: matching bracket, enclosing pairs, nesting levels; skipped in large file mode.
**Tests:** the ported cases; incremental equals full after fuzzed edits.

### 4.13 Brackets in the editor
**Goal:** bracket features in the editor (SPEC §3.1 "Brackets").
**Read:** SPEC §3.1, §8.5, §9.4.
**Reference:** `reference/vscode/src/vs/editor/contrib/bracketMatching/browser/bracketMatching.ts`, `reference/vscode/src/vs/editor/contrib/smartSelect/browser/bracketSelections.ts`.
**Tasks:** matching bracket highlight (`editor.matchBrackets`), jump to bracket, select to bracket, colorization (`editor.bracketPairColorization.enabled`, colors 1–6), expand/shrink selection gains string and bracket steps.
**Tests:** command tables; goldens with colorized brackets.

### 4.14 Indentation detection
**Goal:** detection and the indentation commands (SPEC §8.6).
**Read:** SPEC §8.6.
**Reference:** `reference/vscode/src/vs/editor/common/model/indentationGuesser.ts`, `reference/vscode/src/vs/editor/contrib/indentation/browser/indentation.ts`.
**Tests (port):** the `guessIndentation` cases of `reference/vscode/src/vs/editor/test/common/model/textModel.test.ts`.
**Tasks:** detection applied on open when `editor.detectIndentation`; Convert Indentation to Spaces/Tabs, Indent Using Spaces/Tabs, Change Tab Display Size, Detect Indentation from Content.
**Tests:** the ported cases; command tables.

### 4.15 Indentation rules and Enter
**Goal:** language-aware indentation while typing and pasting (SPEC §8.6).
**Read:** SPEC §8.6.
**Reference:** `reference/vscode/src/vs/editor/common/languages/autoIndent.ts`, `reference/vscode/src/vs/editor/common/languages/enterAction.ts`, `reference/vscode/src/vs/editor/contrib/indentation/browser/indentation.ts` (paste, reindent).
**Tests (port):** `reference/vscode/src/vs/editor/contrib/indentation/test/browser/indentation.test.ts` (cases without the harness).
**Tasks:** on-enter rules and indentation rules on new line; type-time re-indentation (`getIndentActionForType`); `editor.autoIndent` levels; `editor.autoIndentOnPaste`; Reindent Lines and Reindent Selected Lines.
**Tests:** the ported cases.

### 4.16 Guides
**Goal:** indent guides and bracket pair guides (SPEC §8.6).
**Read:** SPEC §8.6, §9.4.
**Reference:** `reference/vscode/src/vs/editor/common/model/guidesTextModelPart.ts`, `reference/vscode/src/vs/editor/browser/viewParts/indentGuides/indentGuides.ts`.
**Tests (port):** the `getLinesIndentGuides` and `getLinesBracketGuides` cases of `reference/vscode/src/vs/editor/test/common/model/textModelWithTokens.test.ts`.
**Tasks:** `editor.guides.indentation`, `editor.guides.highlightActiveIndentation`, `editor.guides.bracketPairs`, `editor.guides.bracketPairsHorizontal`, with VS Code's interplay between the two kinds.
**Tests:** the ported cases; goldens with both kinds of guides.

### 4.17 Auto-closing and surrounding
**Goal:** auto-closing pairs and quotes (SPEC §3.1 "Brackets").
**Read:** SPEC §3.1, §8.1.
**Reference:** `reference/vscode/src/vs/editor/common/cursor/cursorTypeOperations.ts` (auto-closing, surround, overtype), `reference/vscode/src/vs/editor/common/cursor/cursorDeleteOperations.ts` (`autoClosingDelete`).
**Tests (port):** the auto-closing cases of `reference/vscode/src/vs/editor/test/browser/controller/cursor.test.ts`.
**Tasks:** `editor.autoClosingBrackets`, `editor.autoClosingQuotes` (`languageDefined`, `beforeWhitespace`, `always`, `never`), `autoCloseBefore`, `notIn` by token type (at the cursor's embedded language), `editor.autoSurround`, `editor.autoClosingOvertype` (only auto-inserted closers, tracked as decorations, as VS Code), `editor.autoClosingDelete`.
**Tests:** the ported cases.

### 4.18 Comments
**Goal:** comment toggling (SPEC §3.1 "Line commands").
**Read:** SPEC §3.1, §8.1.
**Reference:** `reference/vscode/src/vs/editor/contrib/comment/browser/`.
**Tests (port):** `reference/vscode/src/vs/editor/contrib/comment/test/browser/`.
**Tasks:** toggle line comment (Ctrl+/), add/remove line comment, toggle block comment (Ctrl+Shift+A), multi-cursor, using the comment tokens of the language at each position (embedded code).
**Tests:** the ported cases; an embedded-language case (code in Markdown).

### 4.19 Folding model
**Goal:** `core/folding` (SPEC §8.7).
**Read:** SPEC §8.7.
**Reference:** `reference/vscode/src/vs/editor/contrib/folding/browser/indentRangeProvider.ts`, `reference/vscode/src/vs/editor/contrib/folding/browser/foldingModel.ts`, `reference/vscode/src/vs/editor/contrib/folding/browser/foldingRanges.ts`.
**Tests (port):** `reference/vscode/src/vs/editor/contrib/folding/test/browser/indentRangeProvider.test.ts`, `reference/vscode/src/vs/editor/contrib/folding/test/browser/foldingModel.test.ts`.
**Tasks:** the indentation provider with markers and `offSide`, run in the background on snapshots; the region limit; the model of folded regions as decorations; the commands: fold, unfold, toggle, recursively, fold all, unfold all, fold level 1–7, fold all block comments, fold all regions; skipped in large file mode.
**Tests:** the ported cases.

### 4.20 Folding in the view
**Goal:** folded regions in the editor (SPEC §3.1 "Folding", §9.1).
**Read:** SPEC §8.7, §9.1.
**Reference:** `reference/vscode/src/vs/editor/contrib/folding/browser/folding.ts`.
**Tasks:** hidden rows in the line projection (layout, scrolling, vertical movement and hit testing follow); gutter controls (`editor.showFoldingControls`: always, mouseover, never); the `…` marker after a folded line, click to unfold; reveal rules of SPEC §8.7; folds in the editor view state; a `dev.openFile {"path","line","column","group"}` command.
**Tests:** projection tables with folds; movement over folds; goldens with folded regions and controls.
**Proof:** method B, `scripts/proof.sh docs/implemented-proof-images/4.20-folding.png --commands '[["dev.openFile",{"path":"testdata/proof/4.20/sample.go"}],["editor.foldLevel1",null]]' --`: a Go file with its top-level functions folded and the controls visible.

---

## M5: Editor features

### 5.1 Find model
**Goal:** `core/find` (SPEC §9.8).
**Read:** SPEC §9.8.
**Reference:** `reference/vscode/src/vs/editor/contrib/find/browser/findModel.ts`, `reference/vscode/src/vs/editor/contrib/find/browser/findState.ts`, `reference/vscode/src/vs/editor/contrib/find/browser/replacePattern.ts`, `reference/vscode/src/vs/editor/common/model/textModelSearch.ts`.
**Tests (port):** `reference/vscode/src/vs/editor/contrib/find/test/browser/findModel.test.ts`, `reference/vscode/src/vs/editor/contrib/find/test/browser/replacePattern.test.ts`, `reference/vscode/src/vs/editor/test/common/model/textModelSearch.test.ts`.
**Tasks:** matches line by line (literal, whole word, case, ECMAScript regex, multiline regex over a bounded window), the 19,999 cap, current match, replace one/all with VS Code's replace pattern and preserve case, find in selection.
**Tests:** the ported cases.

### 5.2 Find widget
**Goal:** the find widget (SPEC §9.8).
**Read:** SPEC §3.1 "Find and replace", §9.8.
**Reference:** `reference/vscode/src/vs/editor/contrib/find/browser/findWidget.ts`, `reference/vscode/src/vs/editor/contrib/find/browser/findController.ts`.
**Tasks:**
1. `kit.InputBox` (first version, on `EditableText`), `kit.Toggle`, `kit.IconButton` (first versions; 6.1 and 6.5 finish them).
2. The widget, its bindings (Ctrl+F seeding from the selection, F3/Shift+F3, Ctrl+H, Alt+Enter selects all matches, Escape), match highlights in the text and the scrollbar.
**Tests:** widget tests for each binding; goldens of the widget with replace open.
**Proof:** method A, `go test ./internal/gui/editor -run TestProofFind -proof` → `5.2-find.png`: the find widget with replace open, regex on, and highlighted matches.

### 5.3 Word wrap
**Goal:** word wrap through the line projection (SPEC §9.2).
**Read:** SPEC §6.4, §9.1, §9.2, §7.2.
**Reference:** `reference/vscode/src/vs/editor/common/viewModel/modelLineProjection.ts`, `reference/vscode/src/vs/editor/common/viewModel/monospaceLineBreaksComputer.ts`, `reference/vscode/src/vs/editor/common/viewModel/viewModelLines.ts`.
**Tasks:** `editor.wordWrap` (off, on, wordWrapColumn, bounded), `editor.wordWrapColumn`, `editor.wrappingIndent`, `editor.wrappingStrategy`; breaks computed per SPEC §9.2 (column counting for ASCII monospace lines, paragraph layout for others, background slices with an estimate, scroll anchoring); the `LineLayout` methods for wrapped rows (vertical moves, Home/End on a row); no wrapping in large file mode or for lines over `editor.stopRenderingLineAfter`.
**Tests:** break tables; projection with wraps and folds together; cursor movement on wrapped lines; scroll anchoring.

### 5.4 Whitespace, rulers and highlights
**Goal:** the remaining display options of SPEC §3.1.
**Read:** SPEC §3.1 "Display", §9.2, §9.4.
**Reference:** `reference/vscode/src/vs/editor/browser/viewParts/whitespace/whitespace.ts`, `reference/vscode/src/vs/editor/browser/viewParts/rulers/rulers.ts`, `reference/vscode/src/vs/editor/contrib/wordHighlighter/browser/wordHighlighter.ts`, `reference/vscode/src/vs/editor/contrib/multicursor/browser/multicursor.ts` (`SelectionHighlighter`).
**Tasks:** `editor.renderWhitespace` (none, boundary, selection, trailing, all), `editor.renderControlCharacters` (SPEC §9.2), `editor.rulers` (with colors), `editor.occurrencesHighlight`, `editor.selectionHighlight`.
**Tests:** goldens for each option.

### 5.5 Minimap
**Goal:** the minimap (SPEC §9.5).
**Read:** SPEC §9.5; Glutter: `ui/image.go` (`NewImageFromRGBA`).
**Reference:** `reference/vscode/src/vs/editor/browser/viewParts/minimap/minimap.ts`.
**Tasks:** block rendering into a CPU buffer patched by line ranges and turned into an image; VS Code's sizes; `editor.minimap.enabled`, `side`, `scale`, `showSlider`, `maxColumn`, `size`, `renderCharacters` (registered, always blocks); slider, click and drag; selections and find matches marked; proportional window for long files.
**Tests:** only changed rows are patched; slider math tables; golden with a selection.

### 5.6 Sticky scroll
**Goal:** sticky scroll (SPEC §9.6).
**Read:** SPEC §9.6.
**Reference:** `reference/vscode/src/vs/editor/contrib/stickyScroll/browser/`.
**Tasks:** sticky lines from folding ranges (`editor.stickyScroll.enabled`, `maxLineCount`), drawn with a shadow; click jumps and scrolls the target clear of the sticky lines.
**Tests:** sticky lines for scroll positions in nested code; golden.

### 5.7 Breadcrumbs and the list overlay
**Goal:** breadcrumbs (SPEC §9.7) and `kit.ListOverlay`.
**Read:** SPEC §9.7, §10.2.
**Tasks:** `kit.ListOverlay` (an anchored list with keyboard navigation and filtering, reused by snippet choices); the breadcrumbs bar (`breadcrumbs.enabled`) with path segments (absolute until a folder exists in 6.13) and a sibling picker from `os.ReadDir` with a `ctx`.
**Tests:** list overlay keyboard tables; breadcrumbs segments; golden.
**Proof:** method A, `go test ./internal/gui/editor -run TestProofSticky -proof` → `5.7-sticky-breadcrumbs.png`: a scrolled Go file with two sticky lines and the breadcrumbs bar.

### 5.8 Snippet parser and variables
**Goal:** `core/snippet` parsing and variables (SPEC §9.9).
**Read:** SPEC §9.9.
**Reference:** `reference/vscode/src/vs/editor/contrib/snippet/browser/snippetParser.ts`, `reference/vscode/src/vs/editor/contrib/snippet/browser/snippetVariables.ts`.
**Tests (port):** `reference/vscode/src/vs/editor/contrib/snippet/test/browser/snippetParser.test.ts`, `reference/vscode/src/vs/editor/contrib/snippet/test/browser/snippetVariables.test.ts`.
**Tasks:** the full grammar; variables resolved from a value set the caller gathers first (`TM_*`, `CLIPBOARD`, dates, `RANDOM*`, `UUID`, comment variables, `WORKSPACE_*` empty until 6.13); transforms with `core/regex`.
**Tests:** the ported cases; fuzz the parser.

### 5.9 Snippet sessions and files
**Goal:** inserting snippets and the snippet files (SPEC §9.9).
**Read:** SPEC §9.9.
**Reference:** `reference/vscode/src/vs/editor/contrib/snippet/browser/snippetSession.ts`, `reference/vscode/src/vs/editor/contrib/snippet/browser/snippetController2.ts`, `reference/vscode/src/vs/workbench/contrib/snippets/browser/snippetsFile.ts`, `reference/vscode/src/vs/workbench/contrib/snippets/browser/tabCompletion.ts`.
**Tests (port):** `reference/vscode/src/vs/editor/contrib/snippet/test/browser/snippetSession.test.ts`.
**Tasks:** the session (tab stops as decorations, Tab/Shift+Tab, mirrors, choices through `kit.ListOverlay`, nesting, end conditions, multi-cursor); snippet files (global and per language, `*.code-snippets` with `scope` and prefix arrays) with a `Reload` method (the watcher calls it from 6.13); "Insert Snippet" (a `kit.ListOverlay` picker until 6.17); `editor.tabCompletion`; the context keys `inSnippetMode`, `hasNextTabstop`, `hasPrevTabstop`, `hasSnippetCompletions`.
**Tests:** the ported cases; files loading; Tab priority tables.

### 5.10 Emmet markup
**Goal:** Emmet abbreviations to HTML (SPEC §9.10).
**Read:** SPEC §9.10.
**Reference:** `reference/emmet/src/` (the abbreviation parser and markup output), `reference/emmet/src/snippets/html.json`.
**Tests (port):** the markup cases of `reference/emmet/test/` (at least 60).
**Tasks:** the parser (child, sibling, climb-up, multiplication, groups, id, class, attributes, text, numbering, implicit tags) and HTML output with the editor's indentation and `emmet.syntaxProfiles`; `update-assets.sh` copies the snippets and the license.
**Tests:** the ported cases.

### 5.11 Emmet CSS and expansion
**Goal:** CSS abbreviations and expansion in the editor (SPEC §9.10).
**Read:** SPEC §9.10.
**Reference:** `reference/emmet/src/` (stylesheet output), `reference/emmet/src/snippets/css.json`, `reference/vscode/extensions/emmet/src/` (abbreviation extraction, `includeLanguages`, Tab handling).
**Tests (port):** the stylesheet cases of `reference/emmet/test/` (at least 40).
**Tasks:** CSS abbreviations with fuzzy property matching; extraction before the cursor; "Emmet: Expand Abbreviation"; Tab expansion when `emmet.triggerExpansionOnTab` is on, in `emmet.includeLanguages` and the default HTML-like and CSS-like languages, with the when-clauses of SPEC §9.9.
**Tests:** the ported cases; extraction tables; Tab priority with snippets.

### 5.12 Large files and long lines
**Goal:** SPEC §6.4 end to end.
**Read:** SPEC §6.4, §14.
**Tasks:**
1. The large-file flag (from 1.4) switches off tokenization, colorization, folding, minimap and wrapping (each feature already checks it; verify and fix), and posts a notification through `core/notify`.
2. Long lines: drawing up to `editor.stopRenderingLineAfter`, positions beyond by the monospace advance, `editor.maxTokenizationLineLength` wired.
3. Binary files open as a notice widget in `gui/editor` with an "Open anyway" callback; files over the limit fail with the tagged error.
4. A benchmark opening a generated 200 MB file (skipped unless `-bench`), recorded in `docs/PERFORMANCE.md` against SPEC §14.
**Tests:** mode tables; a 5 MB single-line document stays responsive (layout bounded); the size limit with an injected limit.

---

## M6: Workbench

### 6.1 Kit: buttons, action bar, hover
**Goal:** the first kit widgets of SPEC §10.2.
**Read:** SPEC §10.2, §4 (D9); Glutter: `widgets/raw_tooltip.go`.
**Reference:** `reference/vscode/src/vs/base/browser/ui/button/`, `reference/vscode/src/vs/base/browser/ui/actionbar/`, `reference/vscode/src/vs/base/browser/ui/hover/`.
**Tasks:** `Button` (primary, secondary), `IconButton` (finished), `ActionBar`, `Hover` (delayed, positioned, keyboard dismiss); their colors registered per rule 7.
**Tests:** widget tests; goldens in Dark Modern and Light Modern.

### 6.2 Kit: split view and scrollable element
**Goal:** `SplitView` and the finished `ScrollableElement`.
**Read:** SPEC §10.2.
**Reference:** `reference/vscode/src/vs/base/browser/ui/splitview/splitview.ts`, `reference/vscode/src/vs/base/browser/ui/sash/sash.ts`, `reference/vscode/src/vs/base/browser/ui/scrollbar/`.
**Tasks:** `SplitView` (orientations, sashes with resize cursors, min/max sizes, layout priorities, double-click reset, sizes in/out); `ScrollableElement` for any child.
**Tests:** sash drags, min sizes, window resize with priorities.

### 6.3 Kit: menus
**Goal:** `Menu`, `ContextMenu` and `MenuBar` (D9).
**Read:** SPEC §10.2; Glutter: `widgets/raw_menu_anchor.go` (`RawMenuAnchor`, `RawMenuAnchorGroup`, `MenuController`).
**Reference:** `reference/vscode/src/vs/base/browser/ui/menu/menu.ts`, `reference/vscode/src/vs/base/browser/ui/menu/menubar.ts`.
**Tasks:** menus with keyboard navigation, separators, submenus, keybinding labels, checked items, mnemonics; a context menu at the pointer; a menu bar of menus (Alt shows mnemonics, `window.menuBarVisibility` behaviors).
**Tests:** keyboard tables; goldens.

### 6.4 Kit: tree and list
**Goal:** `Tree` and `List` (SPEC §10.2).
**Read:** SPEC §10.2; Glutter: `widgets/sliver_tree.go`.
**Reference:** `reference/vscode/src/vs/base/browser/ui/tree/`, `reference/vscode/src/vs/base/browser/ui/list/`.
**Tasks:**
1. `Tree` on `TreeSliver`, knowing its limits: an unread folder needs a placeholder child to show a chevron; nodes can't be removed, so changes rebuild the node tree keeping the expansion state; set the row extent and `AnimationStyleNoAnimation`; node contents are small comparable values (lookups use `reflect.DeepEqual`); focus, selection and keyboard handling are Emetos's own; it lives in a `CustomScrollView`.
2. Keyboard navigation (arrows, Home/End, PageUp/Down, Left/Right), type-to-filter/highlight, multi-select (Ctrl/Shift), drag and drop with drop feedback, indent guides.
3. `List` with the same keyboard model.
**Tests:** keyboard tables; drag and drop; rebuild keeps expansion.
**Proof:** method A, `go test ./internal/gui/kit -run TestProofTree -proof` → `6.4-tree.png`: an expanded tree with a selected row and indent guides.

### 6.5 Kit: inputs and dialogs
**Goal:** the remaining kit widgets.
**Read:** SPEC §10.2, §10.7.
**Reference:** `reference/vscode/src/vs/base/browser/ui/inputbox/`, `reference/vscode/src/vs/base/browser/ui/toggle/`, `reference/vscode/src/vs/base/browser/ui/dropdown/`, `reference/vscode/src/vs/base/browser/ui/progressbar/`, `reference/vscode/src/vs/base/browser/ui/countBadge/`, `reference/vscode/src/vs/base/browser/ui/dialog/`.
**Tasks:** `InputBox` (finished: validation messages, history), `Toggle` (finished), `Checkbox`, `Dropdown`, `ProgressBar` (infinite, discrete), `Badge`, `Dialog` (message, detail, buttons, checkbox; modal, keyboard operable; `Show` returns the answer through a callback).
**Tests:** widget tests; goldens.
**Proof:** method A, `go test ./internal/gui/kit -run TestProofKit -proof` → `6.5-kit.png`: a gallery of the kit widgets and a dialog.

### 6.6 Layout parts
**Goal:** the workbench parts (SPEC §10.1).
**Read:** SPEC §10.1.
**Reference:** `reference/vscode/src/vs/workbench/browser/layout.ts`, `reference/vscode/src/vs/workbench/browser/parts/`.
**Tasks:** menu bar area, activity bar, primary side bar, editor area (one editor), panel (bottom/left/right, maximize), status bar area; toggles (Ctrl+B, Ctrl+J...); sizes through `SplitView`; layout state in/out (persisted in 7.5); focus commands between parts and their context keys; the window title (SPEC §10.1).
**Tests:** toggles, sizes, focus movement.

### 6.7 Views, menus and the menu bar
**Goal:** registries for views and menus, and the menus of the window (SPEC §10.4).
**Read:** SPEC §10.1, §10.4, §11.2.
**Reference:** `reference/vscode/src/vs/platform/actions/common/actions.ts` (`MenuId`, `MenuRegistry`), `reference/vscode/src/vs/workbench/browser/parts/titlebar/menubarControl.ts`.
**Tasks:** view containers and views (id, title, icon, container); the activity bar; the menus registry (VS Code's menu ids, groups, order, when-clauses, dynamic submenus); the menu bar (File, Edit, Selection, View, Go, Help) and the editor context menu, with entries for every command registered so far; the Manage menu; a `dev.openMenu {"id"}` command.
**Tests:** menus filtered by context; dynamic submenu.
**Proof:** method B, `scripts/proof.sh docs/implemented-proof-images/6.7-layout.png --commands '[["dev.openMenu",{"id":"MenubarEditMenu"}]]' -- testdata/proof/6.7/sample.go`: the workbench with the Edit menu open, the activity bar, an editor, the panel and the status bar.

### 6.8 Editor groups and tabs
**Goal:** `core/editorgroup` and one group with its tab strip (SPEC §10.3).
**Read:** SPEC §3.2 "Tabs", §10.3.
**Reference:** `reference/vscode/src/vs/workbench/common/editor/editorGroupModel.ts`, `reference/vscode/src/vs/workbench/browser/parts/editor/multiEditorTabsControl.ts`.
**Tests (port):** `reference/vscode/src/vs/workbench/test/browser/parts/editor/editorGroupModel.test.ts`.
**Tasks:** `EditorInput` and the text editor input; the group model (order, preview, pinned, active, MRU) in `core/editorgroup`; documents shared by editors with separate view models, kept while an editor shows them; the tab strip (`kit.Tabs`): preview italic, pinned, dirty, close button, `workbench.editor.enablePreview`, `workbench.editor.showTabs`.
**Tests:** the ported cases; tab widget tests.

### 6.9 Editor grid
**Goal:** several groups (SPEC §3.2 "Editor groups").
**Read:** SPEC §3.2, §10.3.
**Reference:** `reference/vscode/src/vs/workbench/browser/parts/editor/editorPart.ts`, `reference/vscode/src/vs/workbench/browser/parts/editor/editorCommands.ts`.
**Tasks:** the grid in `core/editorgroup` (split right/down, move/copy editors, join, close, maximize, `workbench.editor.openSideBySideDirection`), focus by number (Ctrl+1..8), the grid view with `SplitView`s; grid state in/out.
**Tests:** grid operation tables; focus.

### 6.10 Tab interactions
**Goal:** the rest of SPEC §3.2 "Tabs".
**Read:** SPEC §3.2.
**Reference:** `reference/vscode/src/vs/workbench/browser/parts/editor/multiEditorTabsControl.ts`, `reference/vscode/src/vs/workbench/browser/parts/editor/editorActions.ts`.
**Tasks:** drag to reorder, between groups and to split by dropping on an edge; overflow scrolling; the tab context menu and close actions (others, to the right, saved, all); copy path, reveal in explorer (wired in 6.14).
**Tests:** drag tables; close actions.
**Proof:** method B, `scripts/proof.sh docs/implemented-proof-images/6.10-groups.png --commands '<JSON>' --`, where `<JSON>` opens files from `testdata/proof/6.10/` with `dev.openFile` into three groups (two side by side, one below), makes one dirty with `dev.type`, and leaves one as a preview; record the exact JSON in `IMPLEMENTED.md`. The image shows three groups with several tabs, one dirty and one preview.

### 6.11 Notifications
**Goal:** notifications in the window (SPEC §10.7).
**Read:** SPEC §3.2 "Notifications", §10.7.
**Reference:** `reference/vscode/src/vs/workbench/browser/parts/notifications/`.
**Tasks:** toasts (at most 3, actions, progress, close), the notification center, do-not-disturb; `core/notify`'s sink now shows them; the `dev.notify {"severity","message","actions"}` command.
**Tests:** widget tests for toasts, actions, the center.
**Proof:** method B, `scripts/proof.sh docs/implemented-proof-images/6.11-notifications.png --commands '[["dev.notify",{"severity":"info","message":"Info with actions","actions":["Yes","No"]}],["dev.notify",{"severity":"error","message":"An error"}]]' --`: an info toast with two actions and an error toast.

### 6.12 Output and Problems panels
**Goal:** the panels of SPEC §3.2.
**Read:** SPEC §3.2 "Panels", §13.
**Reference:** `reference/vscode/src/vs/workbench/contrib/output/browser/`, `reference/vscode/src/vs/workbench/contrib/markers/browser/` (structure only).
**Tasks:** the Output panel with channels from `core/logs` (a read-only editor, follow tail, channel picker); the Problems panel with its model (markers by resource, severity, filter) and no producers yet.
**Tests:** channel switching and tailing; markers model tables.

### 6.13 Workspace model and watcher
**Goal:** `core/workspace` (SPEC §10.5).
**Read:** SPEC §5.1, §10.5.
**Reference:** `reference/vscode/src/vs/workbench/contrib/files/common/explorerModel.ts`.
**Tasks:** the folder of a window; lazy reading, sorting (`explorer.sortOrder`), `files.exclude`, compact folders; the inotify watcher of SPEC §10.5 (folder recursively, parents of open files outside it, coalescing, own-write filtering through `FileStat`, polling fallback); settings and snippet files reloaded through it; `cmd/emetos <folder>` opens a folder; breadcrumbs become folder-relative and `WORKSPACE_*` snippet variables get values.
**Tests:** model tables; watcher events in a temporary folder (create, delete, rename, replace by rename, own writes ignored).

### 6.14 Explorer view
**Goal:** the explorer (SPEC §3.2 "Explorer", §10.8).
**Read:** SPEC §10.5, §10.8.
**Reference:** `reference/vscode/src/vs/workbench/contrib/files/browser/views/explorerView.ts`, `reference/vscode/src/vs/workbench/contrib/files/browser/views/openEditorsView.ts`, `reference/vscode/src/vs/workbench/services/themes/browser/fileIconThemeData.ts`, `reference/vscode/extensions/theme-seti/icons/vs-seti-icon-theme.json`.
**Tasks:** the Seti mapping reader (VS Code's icon theme format) and `update-assets.sh` copying the mapping; the view: tree with icons, open on click (preview) and double-click (pinned), reveal the active file (`explorer.autoReveal`), collapse all, the Open Editors section, the welcome content and "Open Folder" (portal dialog on a goroutine, `desktop.OpenFiles` with `Directory`).
**Tests:** mapping tables (extensions, names, languages, folders, light variants); view widget tests; the folder dialog with `FakeDesktop().AnswerFileChooser`.
**Proof:** method B, `scripts/proof.sh docs/implemented-proof-images/6.14-explorer.png -- testdata/proof/6.14/project testdata/proof/6.14/project/main.go`: the explorer showing a small project with Seti icons and `main.go` open and revealed.

### 6.15 Explorer file operations
**Goal:** the file operations of SPEC §10.5.
**Read:** SPEC §3.2 "Explorer", §6.3, §10.5.
**Reference:** `reference/vscode/src/vs/workbench/contrib/files/browser/fileActions.ts`, `reference/vscode/src/vs/workbench/contrib/files/browser/fileImportExport.ts`.
**Tasks:** new file/folder and rename (inline inputs), delete to trash and permanently with confirmations (`explorer.confirmDelete`, `kit.Dialog`), cut/copy/paste (SPEC §10.5) with VS Code's "copy" name suffixes, drag to move (`explorer.confirmDragAndDrop`), drop from the file manager (`DragTarget` with `ExternalDragData`), copy path/relative path; undo/redo of file operations (restore from trash); editors follow renames, moves and deletes.
**Tests:** operation tables in a temporary folder including undo; editors following renames; an external drop through `Tester.Drop`.

### 6.16 Fuzzy scorer
**Goal:** `core/fuzzy` (SPEC §10.6).
**Read:** SPEC §10.6, §14.
**Reference:** `reference/vscode/src/vs/base/common/fuzzyScorer.ts`, `reference/vscode/src/vs/base/common/filters.ts`.
**Tests (port):** `reference/vscode/src/vs/base/test/common/fuzzyScorer.test.ts`.
**Tasks:** port the scorer (scores and highlights for labels and paths); a benchmark over 100,000 generated paths (SPEC §14).
**Tests:** the ported cases; the benchmark recorded in `docs/PERFORMANCE.md`.

### 6.17 Quick input
**Goal:** the quick input widget and its non-file modes (SPEC §10.6).
**Read:** SPEC §10.6.
**Reference:** `reference/vscode/src/vs/platform/quickinput/browser/`, `reference/vscode/src/vs/workbench/contrib/codeEditor/browser/quickaccess/gotoLineQuickAccess.ts`, `reference/vscode/src/vs/platform/quickinput/browser/commandsQuickAccess.ts`.
**Tasks:** the widget (input, list with highlights and keybinding labels, separators, buttons, active-item events, focus restore); `>` (commands, recent first, in memory), `:` (go to line, revealing while typing), `?`, `view `; Ctrl+Shift+P/F1, Ctrl+G; the pickers of 5.7 and 5.9 move to it; `workbench.action.quickOpen` takes the initial text as its argument.
**Tests:** widget tests for each mode.
**Proof:** method B, `scripts/proof.sh docs/implemented-proof-images/6.17-command-palette.png --commands '[["workbench.action.quickOpen",">line"]]' --`: the command palette filtered by "line" with highlighted matches and keybindings.

### 6.18 Quick open
**Goal:** file search in quick input (SPEC §10.6).
**Read:** SPEC §10.6, §14.
**Reference:** `reference/vscode/src/vs/workbench/contrib/search/browser/anythingQuickAccess.ts`.
**Tasks:** the background file listing of the folder (respecting `files.exclude`, `search.exclude`, and `.gitignore`/`.ignore` through `core/glob` with `search.useIgnoreFiles`), scoring off the UI thread per keystroke, recently opened files first (in memory until 7.5), Seti icons, open in preview.
**Tests:** listing with excludes and ignore files; result order; the SPEC §14 target measured and recorded.

### 6.19 Navigation
**Goal:** history and editor switching (SPEC §3.2 "Navigation").
**Read:** SPEC §3.2, §10.3.
**Reference:** `reference/vscode/src/vs/workbench/services/history/browser/historyService.ts`, `reference/vscode/src/vs/workbench/browser/parts/editor/editorQuickAccess.ts`.
**Tasks:** back/forward of editor locations with VS Code's recording rules and Linux keys (Ctrl+Alt+-, Ctrl+Shift+-), last edit location; Ctrl+Tab/Ctrl+Shift+Tab through quick input (MRU in the group; the pick happens on Ctrl release, or on Enter/click if the release is missed because focus moved); Ctrl+PageUp/PageDown; reopen closed editor (Ctrl+Shift+T).
**Tests:** history tables; switcher order and release handling.

### 6.20 Status bar
**Goal:** the status bar items and their pickers (SPEC §3.2 "Status bar").
**Read:** SPEC §3.2, §6.2.
**Reference:** `reference/vscode/src/vs/workbench/browser/parts/statusbar/`, `reference/vscode/src/vs/workbench/browser/parts/editor/editorStatus.ts`.
**Tasks:** the item registry (left/right, priority, tooltip, command); Ln/Col (the visual column plus one) and selection count; indentation, encoding, line endings and language mode with their pickers (Change Language Mode with Configure File Association; Reopen/Save with Encoding; Change End of Line Sequence; the indentation actions of 4.14 and 4.15); the notifications bell; problem counts; the chord message of 3.5.
**Tests:** widget tests for each item and picker.

---

## M7: Files and sessions

### 7.1 Saving
**Goal:** saving and reverting (SPEC §3.1 "Saving").
**Read:** SPEC §3.1, §6.3, §7.1; Glutter: `desktop/file_chooser.go`.
**Reference:** `reference/vscode/src/vs/workbench/services/textfile/common/textFileEditorModel.ts`, `reference/vscode/src/vs/workbench/contrib/codeEditor/browser/saveParticipants.ts`.
**Tasks:** Save, Save As (portal dialog on a goroutine; when it fails with `FileChooserUnavailable`, a quick input path prompt), Save All, Revert; untitled documents (Ctrl+N; Save As changes the URI from `untitled:` to the path, detects the language again, and updates the editor inputs); save participants (`files.trimTrailingWhitespace`, `files.insertFinalNewline`, `files.trimFinalNewlines`); errors as notifications with Retry, Save As and Overwrite (SPEC §6.3).
**Tests:** widget tests with `FakeDesktop().AnswerFileChooser`; participant tables; Save As identity; error routes.

### 7.2 Closing with unsaved changes
**Goal:** prompts when editors, groups or windows close (SPEC §10.10, §10.11).
**Read:** SPEC §10.10, §10.11; Glutter: `widgets/app_lifecycle_listener.go` (`OnExitRequested`), `ui/platform_dispatcher.go` (`OnWindowCloseRequested`), `widgets/window_controller.go` (`RegularWindowControllerDelegate.OnWindowCloseRequested`).
**Tasks:**
1. Closing an editor or group with dirty documents asks Save/Don't Save/Cancel (one dialog listing the files, as VS Code).
2. Window close: the hook returns "cancel" at once, shows the dialog, and after the answer closes the window itself (`controller.Destroy()`, or `ExitApplication` for the last window); a second close request while the dialog is open is ignored. Find which hook reaches the first (implicit) window and write the dirty check per window, so 7.8 can use it for every window; if the first window can't be told apart, add a `docs/GLUTTER-ISSUES.md` entry.
3. `files.hotExit` decides whether to ask (SPEC §10.10); until 7.6 exists, `off` behavior.
**Tests:** `RequestWindowClose` with dialogs answered each way; re-entry; editor and group close.

### 7.3 Auto save
**Goal:** `files.autoSave` (SPEC §3.1 "Saving").
**Read:** SPEC §3.1; Glutter: `widgets/binding.go` (`DidChangeViewFocus`, `AppLifecycleState`).
**Reference:** `reference/vscode/src/vs/workbench/services/filesConfiguration/common/filesConfigurationService.ts`, `reference/vscode/src/vs/workbench/browser/parts/editor/editorAutoSave.ts`.
**Tasks:** `off`, `afterDelay` (`files.autoSaveDelay`), `onFocusChange` (the editor loses focus or another editor becomes active), `onWindowChange` (the window loses focus).
**Tests:** each mode with the fake clock and focus changes.

### 7.4 External changes
**Goal:** files changed on disk (SPEC §3.1 "Files").
**Read:** SPEC §3.1, §10.5.
**Reference:** `reference/vscode/src/vs/workbench/services/textfile/common/textFileEditorModel.ts` (`handleFileChanges`), `reference/vscode/src/vs/workbench/contrib/files/browser/editors/textFileSaveErrorHandler.ts`.
**Tasks:** a watcher event for an open file: a clean document reloads keeping its view state; a dirty one shows a notification with "Compare" (opens the diff editor once 9.6 exists; until then "Overwrite"/"Revert"); a save whose `FileStat` differs from the last read is a conflict with the same choices; a deleted file marks its editors deleted (the title struck through), and saving recreates it.
**Tests:** tables in a temporary folder for each case.

### 7.5 UI state and restore
**Goal:** SPEC §10.10's UI state.
**Read:** SPEC §10.10.
**Tasks:** `core/storage` state files (global and per window id, debounced atomic writes); the window id of SPEC §10.10; saved and restored: layout (6.6), groups and editors (6.8, 6.9) with view states (3.2, 4.20), the explorer's expanded folders, window size, maximized and full-screen state, recent folders and files (now persisted; File > Open Recent and "Clear Recently Opened"); `window.restoreWindows` for the one window (the rest in 7.8).
**Tests:** round trips of each state; restore with missing files (editors dropped, a notice).

### 7.6 Hot exit
**Goal:** SPEC §10.10's backups.
**Read:** SPEC §3.1 "Hot exit", §10.10.
**Reference:** `reference/vscode/src/vs/workbench/services/workingCopy/common/workingCopyBackupService.ts`, `reference/vscode/src/vs/workbench/services/workingCopy/common/workingCopyBackupTracker.ts`.
**Tasks:** backups with the format, naming and timing of SPEC §10.10; the three `files.hotExit` modes and the crash marker; restore on start, including untitled documents; a backup whose file changed opens dirty with a notice; 7.2's prompts follow the mode.
**Tests:** backup timing with the fake clock; restore round trips; each mode; crash restore.

### 7.7 Command line and single instance
**Goal:** SPEC §10.11's command line and `core/ipc`.
**Read:** SPEC §10.11.
**Tasks:** the option parser (by hand, short and long forms, positional counts); `core/ipc` (socket path, versioned JSON, stale socket removal, the race of two launches, hand-over with the working directory, `--wait` until the files close); `--goto`; `--diff` and `--merge` show a notice until 9.6 and 9.7.
**Tests:** parser tables; ipc tests with two processes started by the test helper (a re-exec of the test binary that runs only the ipc code, no window), isolated, under `timeout -k`.

### 7.8 Multiple windows
**Goal:** several windows in one process (SPEC §5.1, §10.11).
**Read:** SPEC §5.1, §10.10, §10.11; Glutter: `widgets/window_controller.go`, `widgets/window_manager.go`.
**Tasks:** New Window (Ctrl+Shift+N) as a Glutter regular window with its own `Window` services; opening a folder in a new or the same window (`window.openFoldersInNewWindow`; the same window disposes and recreates its services after the dirty prompts); per-window close using 7.2's handling for every window; closing the last window quits; `window.restoreWindows` for all windows; the IPC hand-over opens in the right window; a `dev.openFolderInNewWindow {"path"}` command.
**Tests:** multi-window widget tests (`Windows()`), close order, restore of two windows.
**Proof:** method C, `scripts/proof.sh docs/implemented-proof-images/7.8-two-windows.png --output --commands '[["dev.openFolderInNewWindow",{"path":"testdata/proof/7.8/second"}]]' -- testdata/proof/7.8/first`: two Emetos windows with different folders.

---

## M8: Settings, keybindings, themes, profiles

### 8.1 Settings editor
**Goal:** the settings UI (SPEC §11.1).
**Read:** SPEC §3.2 "Settings", §11.1.
**Reference:** `reference/vscode/src/vs/workbench/contrib/preferences/browser/settingsEditor2.ts`, `reference/vscode/src/vs/workbench/contrib/preferences/browser/settingsTree.ts`.
**Tasks:** an editor input "Settings" (Ctrl+,): User/Workspace tabs, a category tree, rows per type (checkbox, number/text input, enum dropdown, "Edit in settings.json" for arrays and objects) with variable heights, modified markers, "Reset Setting"; edits go through `core/settings`; a dirty open `settings.json` editor gets the change as an edit instead of a file write.
**Tests:** edit each kind; reset; the JSON keeps comments; the dirty-editor case.

### 8.2 Settings search and JSON views
**Goal:** search and the JSON commands (SPEC §11.1).
**Read:** SPEC §7.1 (`emetos-virtual:`), §11.1.
**Reference:** `reference/vscode/src/vs/workbench/contrib/preferences/browser/preferencesSearch.ts`.
**Tasks:** search with filters `@modified`, `@lang:<id>` (shows and edits the values of that language's block), `@id:`; `workbench.action.openSettings` takes the search text as its argument; "Open User Settings (JSON)", "Open Workspace Settings (JSON)", "Open Default Settings (JSON)" (an `emetos-virtual:` read-only document generated from the registry).
**Tests:** search tables; the virtual document is read-only.
**Proof:** method B, `scripts/proof.sh docs/implemented-proof-images/8.2-settings.png --config testdata/proof/8.2/config --commands '[["workbench.action.openSettings","font"]]' --`: the settings editor filtered by "font" with one modified setting (from the fixture).

### 8.3 Keybindings editor
**Goal:** the keybindings UI (SPEC §11.3).
**Read:** SPEC §3.2 "Keybindings", §11.3.
**Reference:** `reference/vscode/src/vs/workbench/contrib/preferences/browser/keybindingsEditor.ts`, `reference/vscode/src/vs/workbench/contrib/preferences/browser/keybindingWidgets.ts`.
**Tasks:** an editor input "Keyboard Shortcuts" (Ctrl+K Ctrl+S, `workbench.action.openGlobalKeybindings` with the search text as its argument): table of title, id, keybinding, when, source; search by text and by recorded keys (exact match of the chord, as VS Code's quoted search); record through the keybinding service's recording mode; change (a dialog showing conflicts), add, remove (a user `-command` entry for a default), reset (removes user entries), change when; writes `keybindings.json`; "Open Keyboard Shortcuts (JSON)"; Manage menu entry.
**Tests:** record, conflicts, each edit kind.
**Proof:** method B, `scripts/proof.sh docs/implemented-proof-images/8.3-keybindings.png --config testdata/proof/8.3/config --commands '[["workbench.action.openGlobalKeybindings","copy line"]]' --`: the keybindings editor searching "copy line" with one user binding (from the fixture).

### 8.4 Theme and icon pickers
**Goal:** choosing themes (SPEC §3.2 "Themes").
**Read:** SPEC §8.4, §10.6, §10.8.
**Tasks:** the color theme picker (Ctrl+K Ctrl+T) previewing on the active-item event and reverting on Escape; the file icon theme picker (Seti, None).
**Tests:** preview and revert; the setting written on Enter.
**Proof:** method B, `scripts/proof.sh docs/implemented-proof-images/8.4-light-theme.png --config testdata/proof/8.4/config -- testdata/proof/8.4/project`: the workbench in Light Modern with Seti icons in the explorer.

### 8.5 Zoom, zen mode, full screen
**Goal:** SPEC §10.9.
**Read:** SPEC §10.9.
**Reference:** `reference/vscode/src/vs/workbench/browser/actions/layoutActions.ts`.
**Tasks:** `kit.Zoom` and `window.zoomLevel` with VS Code's Linux keys (Ctrl+=, Ctrl+-, Ctrl+Numpad0); zen mode and its settings; full screen (F11); centered layout.
**Tests:** zoom levels (layout and hit testing scaled); zen mode toggles and restores the parts.

### 8.6 Profiles
**Goal:** SPEC §10.12.
**Read:** SPEC §10.10, §10.12.
**Reference:** `reference/vscode/src/vs/platform/userDataProfile/common/userDataProfile.ts` (behavior only).
**Tasks:** profiles in `core/storage` (create empty or from the current, rename, delete, switch per window, `--profile`, the last profile per folder, name rules); commands and pickers for each, in the Manage menu; the profile name in the status bar when it isn't the default; export/import in the format of SPEC §10.12 through the portal dialog.
**Tests:** switching changes the settings and keybindings in effect without closing editors; export/import round trip; name validation.

---

## M9: Diff and merge

### 9.1 Line diff
**Goal:** `core/diff` line diffs (SPEC §9.11).
**Read:** SPEC §9.11.
**Reference:** `reference/vscode/src/vs/editor/common/diff/defaultLinesDiffComputer/defaultLinesDiffComputer.ts`, `reference/vscode/src/vs/editor/common/diff/defaultLinesDiffComputer/algorithms/`, `reference/vscode/src/vs/editor/common/diff/defaultLinesDiffComputer/heuristicSequenceOptimizations.ts`.
**Tasks:** the line sequence, dynamic programming for small inputs and Myers for large ones (VS Code's threshold), the heuristics, ignore trim whitespace.
**Tests:** tables; random inputs whose diff applied to the original gives the modified text.

### 9.2 Character diff and limits
**Goal:** character refinement and the time limit.
**Read:** SPEC §9.11.
**Reference:** `reference/vscode/src/vs/editor/common/diff/defaultLinesDiffComputer/defaultLinesDiffComputer.ts` (`refineDiff`), `reference/vscode/src/vs/editor/test/node/diffing/fixtures/`.
**Tasks:** refinement inside changed line blocks; `diffEditor.maxComputationTime` (the unrefined line diff after it); the fixture test: for each fixture folder, compare with its expected "advanced" result, skipping the moved-code parts (SPEC §2).
**Tests:** the fixtures.

### 9.3 Three-way merge model
**Goal:** conflicts from markers and from `--merge` inputs (SPEC §9.11).
**Read:** SPEC §9.11.
**Reference:** `reference/vscode/extensions/merge-conflict/src/mergeConflictParser.ts`, `reference/vscode/src/vs/workbench/contrib/mergeEditor/browser/model/modifiedBaseRange.ts`, `reference/vscode/src/vs/workbench/contrib/mergeEditor/browser/model/mergeEditorModel.ts`.
**Tasks:** the marker parser (with and without `|||||||`); the inputs and base of SPEC §9.11 (a); the diff3 of SPEC §9.11 (b); the result model (initial content, conflict regions as decorations with handled/unhandled state, accept incoming/current/both/base per conflict).
**Tests:** parser tables; diff3 tables; accept operations.

### 9.4 View zones
**Goal:** rows inserted between lines in the editor (SPEC §9.1).
**Read:** SPEC §9.1.
**Reference:** `reference/vscode/src/vs/editor/browser/viewParts/viewZones/viewZones.ts`.
**Tasks:** view zones in the line projection (after a model line, a height, an optional widget), with layout, scrolling, hit testing and vertical movement following; an API to add, change and remove zones.
**Tests:** projection tables with zones, folds and wraps together.

### 9.5 Side-by-side diff editor
**Goal:** `gui/diffeditor` side by side (SPEC §9.11).
**Read:** SPEC §3.1 "Diff editor", §9.11.
**Reference:** `reference/vscode/src/vs/editor/browser/widget/diffEditor/`.
**Tasks:** two editors aligned with view zones, scroll-synced; line and character colors; the overview ruler; next/previous change (Alt+F5, Shift+Alt+F5); the read-only original; a `DiffEditorInput` (save and dirty on the modified side, serialize).
**Tests:** alignment tables; navigation; golden.

### 9.6 Inline diff and compare commands
**Goal:** the inline view and the ways to open a diff.
**Read:** SPEC §3.1, §9.11, §10.11.
**Tasks:** the inline view (`diffEditor.renderSideBySide`, `diffEditor.useInlineViewWhenSpaceIsLimited`) with deleted lines in view zones; "Compare with Saved" (an `emetos-virtual:` original), "Compare Active File With...", explorer "Select for Compare"/"Compare with Selected", `emetos --diff a b`, and "Compare" in 7.4's notice.
**Tests:** inline layout; each entry point.
**Proof:** method B, `scripts/proof.sh docs/implemented-proof-images/9.6-diff.png -- --diff testdata/proof/9.6/old.go testdata/proof/9.6/new.go`: a side-by-side diff with character changes.

### 9.7 Merge editor layout
**Goal:** `gui/mergeeditor` showing a merge (SPEC §9.11).
**Read:** SPEC §3.1 "Merge editor", §9.11.
**Reference:** `reference/vscode/src/vs/workbench/contrib/mergeEditor/browser/view/`.
**Tasks:** the layout of SPEC §9.11 (incoming left, current right, result below) with conflict decorations and navigation; opening a file with conflict markers offers "Resolve in Merge Editor" (a notification on open); `emetos --merge path1 path2 base result`.
**Tests:** layout and navigation; both ways of opening.

### 9.8 Merge editor actions
**Goal:** resolving and completing a merge (SPEC §9.11).
**Read:** SPEC §9.11.
**Tasks:** accept incoming/current/both per conflict and for all; editing the result; "Complete Merge" (the file or the `result` path, SPEC §9.11) and the exit code with `--wait`; a `dev.acceptMerge {"conflict","side"}` command.
**Tests:** each action; completion for both kinds; exit codes.
**Proof:** method B, `scripts/proof.sh docs/implemented-proof-images/9.8-merge.png --commands '[["dev.openMergeEditor",{"path":"testdata/proof/9.8/conflicted.go"}],["dev.acceptMerge",{"conflict":0,"side":"incoming"}]]' --`: the merge editor with two conflicts, the first resolved (`dev.openMergeEditor` opens a marker file in the merge editor; add it in this step).

---

## M10: Release of areas 1–2

### 10.1 Performance pass
**Goal:** every target of SPEC §14 measured and met, or decided by the user.
**Read:** SPEC §14; `docs/PERFORMANCE.md`.
**Tasks:**
1. Extend `scripts/perf.sh` to every target of SPEC §14 (the 200 MB open, fuzzy search, cold start with the workbench, state restore and grammars).
2. Profile the misses (`go test -cpuprofile`, `runtime/pprof` behind `emetos_dev`), fix them, or add Glutter issues with profiles.
3. Update `docs/PERFORMANCE.md`. For a target still missed, **ask the user**.
**Manual checks:** the user runs `scripts/perf.sh --session` and pastes the summary.

### 10.2 Release
**Goal:** a usable first release of areas 1 and 2.
**Read:** SPEC §1, §3, §16, §17.
**Tasks:**
1. `README.md`: what Emetos is, install with `go install -tags glutter_release github.com/rm4n0s/emetos/cmd/emetos@latest` (D16), requirements, the feature list, privacy, where settings and keybindings live, screenshots from the proof images.
2. An icon drawn by a small Go program with Glutter's canvas (rendered by a test at 16–512 px into `assets/icons/`), a `dev.emetos.Emetos.desktop` file (`Exec=emetos %F`, matching the app id), and `scripts/install-desktop.sh` for `~/.local/share/applications` and the icon theme folders.
3. A coverage table in `docs/IMPLEMENTED.md`: every row of SPEC §3.1 and §3.2, the step(s) and tests that cover it, and its status; a `NOTICE` and `third_party/` audit (Codicons CC BY attribution).
4. **Ask the user** before tagging a version.
**Manual checks:** the user looks at the icon, and uses Emetos for their own work and reports problems; each problem becomes separate fix work after this step.
