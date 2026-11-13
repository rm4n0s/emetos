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
3. **Create the branch.** If `docs/IMPLEMENTATION.md` is not on `main` (the plan was written on `feat/spec`), stop and ask the user to merge it first.
   ```sh
   git switch main && git pull --ff-only
   git switch -c feat/step-<id>-<slug>     # e.g. feat/step-1.1-piece-tree
   ```

### While you work

4. **Do exactly the step's Tasks.** Don't do work that belongs to a later step, even if it looks easy. If a later step will need a hook, leave only a comment: `// TODO(step 5.3): ...`.
5. **Follow CLAUDE.md.** In particular: `rm4n0s/errors` for every error, no cgo, no Glutter import under `internal/core`, no network, isolated paths in every test and script.
6. **Match VS Code.** When VS Code has the behavior, match its defaults, edge cases and command ids. Read its implementation under `reference/vscode` and port the algorithm when it is the right one (a ported file gets a `// Ported from VS Code: <path>` line in its package doc or above the ported code). Port the relevant cases of the tests listed under **Tests** as table-driven Go tests:
   - Convert VS Code's 1-based lines and UTF-16 columns with `core/vstest` (step 1.1). Work out the expected positions of non-ASCII cases again for byte columns, with a comment saying so.
   - Skip a case that needs VS Code's TypeScript harness (a full editor or services), with a one-line reason in a comment.
   - In `docs/IMPLEMENTED.md`, say how many cases you ported and how many you skipped.
7. **Register what you add.** From the step that creates each registry on (settings 2.3, commands and keybindings 2.5, colors 4.8, menus 6.7), every command, setting, default keybinding, workbench color and menu item that your step adds or first uses is registered (2.3, 2.5 and 4.8 register what earlier steps used) with VS Code's id, title, default, description and when-clause:
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
    test -z "$(gofmt -l $(go list -f '{{.Dir}}' ./...))"
    go vet ./...
    golangci-lint run
    go test -race -tags debug_mode ./...   # a debug build of Glutter, which a build without tags is not since Glutter 1.17
    CGO_ENABLED=0 go build ./...
    CGO_ENABLED=0 go test ./...            # the tests again, as a release build (SPEC §15.1)
    ```
    If the step lists **Display tests**, run them too: `scripts/headless-wayland.sh -- go test -race -tags 'emetos_display debug_mode' <packages>`. Golden tests have `Golden` in their name, so `go test ./... -run Golden -update-goldens` finds them all.
11. **Proof images.** Only when the step has a **Proof** item:
    - Produce exactly the images it lists, in `docs/implemented-proof-images/`, named `<id>-<name>.png` (e.g. `3.3-cursors.png`), with the command the step gives.
    - **Open every image with your image-viewing tool** (the Read tool shows images) and check that it shows what **Proof** describes. If it doesn't, fix the code. The step isn't done.
    - Never draw, edit, crop or generate a proof image any other way, and never reuse an older image.
12. **Record the step.** On the step branch, add an entry to `docs/IMPLEMENTED.md`, using the format at the top of that file. Embed the proof images.
13. **Commit on the step branch.** Use one commit per step, with the message `Step <id>: <imperative summary>`:
    ```sh
    git add -A && git commit -m "Step 1.1: Add the piece tree"
    ```
14. **Merge every finished step into `main`; publish to GitHub once per milestone.**
    - Merge a step only after rules 10–13 are complete: checks green, proof verified, `IMPLEMENTED.md` entry committed. Never collect several steps into one merge, and never leave a finished step only on its branch.
    - A step counts as finished when its commit is on the local `main` and tagged `step-<id>`; start the next step from there.
    ```sh
    git switch main
    git merge --ff-only feat/step-1.1-piece-tree
    git tag step-1.1
    git branch -d feat/step-1.1-piece-tree
    ```
    - **Publish a milestone.** When the last step of a milestone (a `## M<n>` section below) is merged, push `main` and the tags of its steps. The milestone counts as published when that commit is on `origin/main`.
    ```sh
    git push origin main
    git push origin --tags
    ```
    If `--ff-only` fails, rebase the step branch onto `main`, run the standard checks again, and retry. If a push is rejected or fails (authentication, network), retry a few times; if it keeps failing, stop and tell the user. Don't start the next milestone's first step until the milestone's push succeeded.
15. **Report to the user.** Give the step id, a two-line summary, the check results, the proof image paths, new `docs/GLUTTER-ISSUES.md` entries, new manual checks, and the commit hash now on `main` (and, at the end of a milestone, that it is on `origin/main`). Then start the next step.

**Default "Done when".** A step without a **Done when** line is done when its Tasks are complete, its **Tests** are written and passing, its **Display tests** pass, its **Proof** images are verified, and the standard checks pass.

### Proof methods

| Method | Use it for | How |
|---|---|---|
| **A. Widget render** | a widget on its own (kit widgets, the editor with a given document) | A widget test renders it with `internal/gui/testutil` (test fonts, isolated paths) and, when run with `-proof`, writes `Tester.Screenshot()` as a PNG to `docs/implemented-proof-images/<file>.png`. Run only that package: `go test ./internal/gui/<pkg> -run <TestName> -proof` |
| **B. App self-capture** | anything inside one Emetos window | `scripts/proof.sh docs/implemented-proof-images/<file>.png [--config <dir>] [--after <duration>] [--commands '<json>'] [--launch '<args>']... -- <emetos args>`: builds a debug binary in a temporary folder, isolates the `XDG_*` folders (copying `<dir>` into the configuration folder), runs it in a headless compositor with `GLUTTER_SCREENSHOT`, and checks that the PNG was written. With `--commands`, the capture waits 1 s after the commands by default (`GLUTTER_SCREENSHOT_AFTER`; `--after` changes it), because Glutter otherwise captures the first idle frame. Fixtures (configuration, sample files) live in `testdata/proof/<step id>/`. `--commands` sets `EMETOS_DEV_COMMANDS` (SPEC §15.2). Each `--launch '<args>'` (repeatable, step 9b.3) runs `emetos <args>` again inside the same isolated environment once the first instance listens on its socket (`scripts/run-with-launches.sh`), so that it is handed over to the first instance; the capture waits 1 s by default with `--launch` too. Glutter captures only the first window |
| **C. Output capture** | several windows, or things outside a window | `scripts/proof.sh <file> --output ...`: the same, but captures the whole headless output with `grim` (through `scripts/screenshot-headless.sh`). The windows are tiled (`HEADLESS_FLOATING=0`, unless the environment sets it), so that several windows share the output and none hides another; `--after <duration>` is how long the capture waits once the first window is mapped (`SCREENSHOT_SETTLE`; 1 s by default with `--commands`) |
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
| M9 Diff and merge | 9.1–9.9 |
| M9b One window: workspace tabs and the tray | 9b.1, 9b.1b, 9b.2–9b.5 |
| M10 Release of areas 1–2 | 10.1–10.2 |
| M11 Extensions | 11.1–11.7 |
| M12 Language intelligence (area 3) | 12.1–12.9 |
| M13 The Agents extension | 13.1–13.3 |
| M14 The Go extension | 14.1–14.3 |
| M15 Remote Work | 15.1–15.5 |
| M16 Core request topics, privileges and confinement | 16.1 |
| M17 Wrap-up | 17.1 |
| M17b Bugs found on the desktop | 17b.1–17b.6 |
| M17c Remove the Agents extension | 17c.1 |
| M17d The terminal | 17d.1–17d.7 |
| M17e What the user asked for on 2026-10-28 | 17e.1–17e.4 |
| M18 Containers | 18.1–18.4 |
| M19 SQL databases | 19.1–19.3 |
| M20 MongoDB | 20.1–20.2 |
| M21 Redis | 21.1–21.2 |
| M22 Wrap-up of M18–M21 | 22.1–22.2 |
| M23 Remove an extension the user no longer wanted | 23.1 |
| M24 Git and Source Control | 24.1–24.25 |
| M24b Language servers of more languages | 24b.1–24b.10 |
| M25 Run and Debug | 25.1–25.13 |
| M26 Testing | 26.1–26.10 |
| M27 HTTP Client | 27.1–27.10 |
| M28 A terminal as a window tab | 28.1 |
| M29 A "+" button in the editor tab strip | 29.1 |
| M30 The Emetos Tutor | 30.1–30.2 |
| M31 Extensions on and off in the Settings editor | 31.1 |
| M32 The mascot | 32.1 |
| M33 A missing tool is told once | 33.1 |
| M34 What the extensions need from the system | 34.1–34.5 |
| M35 An untrusted Git repository in the status bar | 35.1 |

---

## M0: Skeleton

### 0.1 Module and package layout
**Goal:** an empty but complete repository skeleton that passes all checks.
**Read:** SPEC §5.2, §5.3, §17; CLAUDE.md "Layout".
**Tasks:**
1. For every package of SPEC §5.2 that the plan fills, create the directory with a `doc.go` whose package comment states its purpose and SPEC section. `cmd/emetos/main.go` is an empty `main`. Directories that only group others (`internal/core`, `internal/gui`, `internal/gui/views`) get no `doc.go`.
2. Add `.golangci.yml` (golangci-lint v2 format): the standard set plus `revive` with `exported` and `package-comments`, `gofmt` as formatter, `run.go: "1.27"`, and `reference` excluded.
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
   - It writes `reference/go.mod` (`module reference`), so the nested module keeps VS Code's Go test files out of `./...`.
3. `scripts/check-references.sh`: extracts from `docs/IMPLEMENTATION.md` and `docs/SPEC.md` every match of `reference/(vscode|vscode-textmate|emmet|vscode-go|codicons)/[^ `)<>;]*`, strips a trailing `,`, `.` or `:`, skips matches containing `<`, expands `{a,b}` lists and `*` with bash globbing, and fails, listing them, when a path doesn't exist.
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
1. `go get github.com/rm4n0s/glutter@v1.4.0` (0.1's `go mod tidy` removed it). Copy `scripts/headless-wayland.sh` and `scripts/screenshot-headless.sh` from Glutter's module directory (keep their header comments; change "Glutter" to "Emetos" where it names the project; `chmod +x`, as the module cache is read-only).
2. `scripts/isolated.sh [--config <dir>] -- <command>`: first exports `GOCACHE`, `GOMODCACHE` and `GOPATH` from `go env` (so Go keeps its caches); then points `XDG_CONFIG_HOME`, `XDG_STATE_HOME`, `XDG_CACHE_HOME` and `XDG_DATA_HOME` into a fresh temporary folder, and `XDG_RUNTIME_DIR` to a private folder in it with a symlink to the current `$WAYLAND_DISPLAY` socket when one is set (so the app still reaches the compositor, but never the user's Emetos socket); copies `<dir>` into `$XDG_CONFIG_HOME/emetos/`; and removes the folder afterwards. fontconfig rebuilds its cache in the temporary folder, so the first run is slower.
3. `scripts/proof.sh` as described in this file's "Proof methods" (B and C). It builds `go build -o <tmp>/emetos ./cmd/emetos` first (a debug build, so dev commands work), and runs the binary, never `go run`.
4. `update-assets.sh` copies `codicon.ttf`, `codicon.csv` and `seti.woff` into `internal/gui/kit/fonts` (embedded; Glutter reads WOFF through go-text; if loading fails, stop and **ask the user**), with licenses in `third_party/`; `internal/gui/kit/fonts` exports the family names as constants (`Codicon`, `Seti`) and a `Sources()` list for `RunAppOpts.Fonts`; a `go generate` generator (`internal/gui/kit/fonts/internal/gen`) turns `codicon.csv` into constants (`fonts.CodiconCode` and the rest).
5. `internal/gui/app.Run(ctx, opts)`: `widgets.RunApp` with `AppID: "dev.emetos.Emetos"`, title "Emetos", size 1280×800, the bundled fonts, and a `WidgetsApp` whose home is an empty surface in Dark Modern's `editor.background` (`#1F1F1F`) with the `code` codicon and the text "Emetos" centered. `cmd/emetos` parses `--version` (from `debug.ReadBuildInfo`), `--help` and `--log <level>` and calls it.
**Tests:** a widget test that pumps the home and finds the text; every bundled font loads (`gluttertest.Opts.Fonts`); the generated codicon constants match the CSV; `scripts/isolated.sh` leaves no folder behind (a test that runs it with `env`).
**Proof:** method B, `scripts/proof.sh docs/implemented-proof-images/0.4-first-window.png`: the window with the icon and "Emetos" in the center.

---

## M1: Text core

### 1.1 Positions and the test helper
**Goal:** the position types and `core/vstest` (SPEC §6.2, §15.1).
**Read:** SPEC §6.2, §15.1.
**Reference:** `reference/vscode/src/vs/editor/common/core/position.ts`, `reference/vscode/src/vs/editor/common/core/range.ts`, `reference/vscode/src/vs/editor/common/core/selection.ts`.
**Tests (port):** `reference/vscode/src/vs/editor/test/common/core/range.test.ts`.
**Tasks:**
1. `buffer.Position`, `Range`, `Selection` with VS Code's comparisons, contains, intersect, union and empty-range rules (zero-based, byte columns).
2. `core/vstest`: convert a VS Code (1-based line, UTF-16 column) to an Emetos position for a given text, and back; table tests over ASCII, Greek, CJK and astral-plane text.
**Tests:** the ported cases; the conversion tables.

### 1.1b The piece tree
**Goal:** a persistent piece tree with snapshots (SPEC §6.1).
**Read:** SPEC §6.1, §5.1.
**Reference:** `reference/vscode/src/vs/editor/common/model/pieceTreeTextBuffer/pieceTreeBase.ts`, `reference/vscode/src/vs/editor/common/model/pieceTreeTextBuffer/rbTreeBase.ts`.
**Tasks:**
1. Pieces over read-only chunks and an append-only add buffer, in an AVL tree with path copying: nodes are immutable once shared, an edit copies the path from the root, and there are no parent pointers. Each node keeps its subtree's byte length and line-feed count. Port only VS Code's piece logic (splitting a piece at an offset, inserting after a piece, deleting a range of pieces, appending to the last piece of the add buffer); the tree itself is Emetos's own.
2. Insert, delete and replace at byte offsets; `Len`, `Text(start, end)` by offsets; `Snapshot()` in O(1), safe from any goroutine.
**Tests:** a fuzz test of random edits against a plain string (content after every edit); AVL balance after every edit; snapshots unchanged by later edits; a race test reading a snapshot while editing.

### 1.2 Line index and builder
**Goal:** line-based access and building a buffer from a reader.
**Read:** SPEC §6.1.
**Reference:** `reference/vscode/src/vs/editor/common/model/pieceTreeTextBuffer/pieceTreeBase.ts` (line lookups), `reference/vscode/src/vs/editor/common/model/pieceTreeTextBuffer/pieceTreeTextBufferBuilder.ts`.
**Tests (port):** `reference/vscode/src/vs/editor/test/common/model/pieceTreeTextBuffer/pieceTreeTextBuffer.test.ts` (the cases without `\r`: the buffer holds only `\n`).
**Tasks:**
1. `LineCount`, `LineContent`, `LineLength`, `OffsetAt(Position)`, `PositionAt(offset)`, `Text(Range)`, chunk and line iterators; positions past a line's end clamp as VS Code's `validatePosition`.
2. A builder from an `io.Reader` in 64 KiB chunks that never splits a UTF-8 sequence (the text is already `\n`-only; SPEC §6.1).
3. Benchmarks on a 100,000-line buffer: `BenchmarkInsertTyping` (one byte at a time in the middle), `BenchmarkLineContent`, `BenchmarkPositionAt`.
**Tests:** the ported cases; the fuzz test of 1.1b extended to every line and position conversion.
**Done when:** the standard checks pass and the benchmark numbers are recorded in `IMPLEMENTED.md`, with `BenchmarkInsertTyping` under 5 µs per insert and `BenchmarkLineContent` under 2 µs per line.

### 1.3 Columns and boundaries
**Goal:** visual columns, grapheme, word and word-part boundaries.
**Read:** SPEC §6.2, §7.2.
**Reference:** `reference/vscode/src/vs/editor/common/core/cursorColumns.ts`, `reference/vscode/src/vs/editor/common/core/wordCharacterClassifier.ts`, `reference/vscode/src/vs/editor/common/core/wordHelper.ts`, `reference/vscode/src/vs/base/common/strings.ts` (`isFullWidthCharacter`, `isEmojiImprecise`).
**Tests (port):** the `CursorColumns` cases under `reference/vscode/src/vs/editor/test/common/` (search for `visibleColumnFromColumn`; there may be none, then write tables from `cursorColumns.ts`).
**Tasks:**
1. `core/edit` (imports `core/buffer`): visual column ↔ byte column exactly as SPEC §6.2; grapheme cluster boundaries (go-text's segmenter); word boundaries by `editor.wordSeparators`; word parts (camelCase, snake_case).
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
**Tests:** every M1 command through `SendKeyEvent`; chords and modifier-only keys; a key on a non-Latin layout (a Greek character with the physical key of `]` still runs `ctrl+]`, `editor.action.indentLines`); unbound printable keys reach text input.

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
**Done when:** every target marked 3.11 is measured headless, and its UI-thread, memory, idle and start numbers are met, or the user decided how to go on. The `--session` numbers (with the GPU) are a pending manual check and don't block the step.

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

### 4.6 The first languages: Go, JSON, YAML, Markdown
**Goal:** Go, JSON, JSON with Comments, YAML and Markdown bundled and tokenized as VS Code does (D13).
**Read:** SPEC §8.1, §15.1, §17.
**Reference:** `reference/vscode/extensions/{go,json,yaml,markdown-basics}/` (`package.json`, `syntaxes/`, `language-configuration.json`, `cgmanifest.json`), `reference/vscode/extensions/vscode-colorize-tests/test/colorize-fixtures/`, `reference/vscode/extensions/vscode-colorize-tests/test/colorize-results/`.
**Tasks:**
1. `update-assets.sh` copies the contributions, grammars and configurations of those extensions into `internal/core/language/assets/` (embedded), with licenses and `NOTICE` rows.
2. An oracle test: for each colorize fixture of these languages, tokenize and compare every token's text and scopes with `colorize-results/<fixture>.json`. A mismatch is fixed, or (for a documented regexp2 difference) listed in `internal/core/language/testdata/expected-differences.txt` with the reason. Markdown's embedded code blocks use only the languages bundled so far.
**Tests:** the oracle test; every bundled grammar compiles.

### 4.6b The first languages: Python, JavaScript, TypeScript
**Goal:** Python, JavaScript, JavaScript React, TypeScript and TypeScript React bundled and tokenized as VS Code does (D13).
**Read:** SPEC §8.1, §15.1, §17.
**Reference:** `reference/vscode/extensions/{python,javascript,typescript-basics}/`, the colorize fixtures and results.
**Tasks:** as 4.6 for these languages, with the same oracle test and expected-differences file; Markdown's code blocks gain them.
**Tests:** the oracle test for each.

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
**Tasks:** hidden rows in the line projection (layout, scrolling, vertical movement and hit testing follow); gutter controls (`editor.showFoldingControls`: always, mouseover, never); the `…` marker after a folded line, click to unfold; reveal rules of SPEC §8.7; folds in the editor view state; a `dev.openFile {"path","line","column"}` command.
**Tests:** projection tables with folds; movement over folds; goldens with folded regions and controls.
**Proof:** method B, `scripts/proof.sh docs/implemented-proof-images/4.20-folding.png --after 2s --commands '[["dev.openFile",{"path":"testdata/proof/4.20/sample.go"}],["editor.foldLevel1",null]]' --`: a Go file with its top-level functions folded and the controls visible.

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

### 6.4 Kit: tree
**Goal:** `Tree` with single selection and the keyboard (SPEC §10.2).
**Read:** SPEC §10.2; Glutter: `widgets/sliver_tree.go`.
**Reference:** `reference/vscode/src/vs/base/browser/ui/tree/`.
**Tasks:**
1. `Tree` on `TreeSliver`, knowing its limits: an unread folder needs a placeholder child to show a chevron; nodes can't be removed, so changes rebuild the node tree keeping the expansion state; set the row extent and `AnimationStyleNoAnimation`; node contents are small comparable values (lookups use `reflect.DeepEqual`); focus, selection and keyboard handling are Emetos's own; it lives in a `CustomScrollView`.
2. Keyboard navigation (arrows, Home/End, PageUp/Down, Left/Right), single selection, indent guides.
**Tests:** keyboard tables; rebuild keeps expansion.
**Proof:** method A, `go test ./internal/gui/kit -run TestProofTree -proof` → `6.4-tree.png`: an expanded tree with a selected row and indent guides.

### 6.4b Kit: list, multi-select, filter, drag and drop
**Goal:** `List` and the rest of the tree's behavior (SPEC §10.2).
**Read:** SPEC §10.2.
**Reference:** `reference/vscode/src/vs/base/browser/ui/list/`, `reference/vscode/src/vs/base/browser/ui/tree/`.
**Tasks:** `List` (virtualized, the tree's keyboard model); for both: multi-select (Ctrl/Shift), type-to-filter/highlight, drag and drop with drop feedback.
**Tests:** keyboard and selection tables; filtering; drag and drop.

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
**Tasks:** the grid in `core/editorgroup` (split right/down, move/copy editors, join, close, maximize, `workbench.editor.openSideBySideDirection`), focus by number (Ctrl+1..8), the grid view with `SplitView`s; grid state in/out; `dev.openFile` gains a `group` argument (a 1-based index of an existing group).
**Tests:** grid operation tables; focus.

### 6.10 Tab interactions
**Goal:** the rest of SPEC §3.2 "Tabs".
**Read:** SPEC §3.2.
**Reference:** `reference/vscode/src/vs/workbench/browser/parts/editor/multiEditorTabsControl.ts`, `reference/vscode/src/vs/workbench/browser/parts/editor/editorActions.ts`.
**Tasks:** drag to reorder, between groups and to split by dropping on an edge; overflow scrolling; the tab context menu and close actions (others, to the right, saved, all); copy path, reveal in explorer (wired in 6.14).
**Tests:** drag tables; close actions.
**Proof:** method B, `scripts/proof.sh docs/implemented-proof-images/6.10-groups.png --commands '<JSON>' --`, where `<JSON>` opens files from `testdata/proof/6.10/` with `dev.openFile`, creates the groups with `workbench.action.splitEditorRight` and `workbench.action.splitEditorDown` (two side by side, one below), opens more files into them with `dev.openFile`'s `group`, makes one dirty with `dev.type`, and leaves one as a preview; record the exact JSON in `IMPLEMENTED.md`. The image shows three groups with several tabs, one dirty and one preview.

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
**Tasks:** the item registry (left/right, priority, tooltip, command); Ln/Col (the visual column plus one) and selection count; indentation, encoding, line endings and language mode with their pickers (Change Language Mode with Configure File Association; Reopen with Encoding; Change End of Line Sequence; the indentation actions of 4.14 and 4.15); the notifications bell; problem counts; the chord message of 3.5.
**Tests:** widget tests for each item and picker.

---

## M7: Files and sessions

### 7.1 Saving
**Goal:** saving and reverting (SPEC §3.1 "Saving").
**Read:** SPEC §3.1, §6.3, §7.1; Glutter: `desktop/file_chooser.go`.
**Reference:** `reference/vscode/src/vs/workbench/services/textfile/common/textFileEditorModel.ts`, `reference/vscode/src/vs/workbench/contrib/codeEditor/browser/saveParticipants.ts`.
**Tasks:** Save, Save As (portal dialog on a goroutine; when it fails with `FileChooserUnavailable`, a quick input path prompt), Save All, Revert; untitled documents (Ctrl+N; Save As changes the URI from `untitled:` to the path, detects the language again, and updates the editor inputs); save participants (`files.trimTrailingWhitespace`, `files.insertFinalNewline`, `files.trimFinalNewlines`); "Save with Encoding" added to the encoding picker of 6.20; errors as notifications with Retry, Save As and Overwrite (SPEC §6.3).
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
**Goal:** profiles in the core and switching a window's profile (SPEC §10.12).
**Read:** SPEC §10.10, §10.12.
**Reference:** `reference/vscode/src/vs/platform/userDataProfile/common/userDataProfile.ts` (behavior only).
**Tasks:** profiles in `core/storage` (create empty or from the current, rename, delete, name rules, the last profile per folder); `--profile`; switching a window's profile reloads its settings, keybindings, snippets and theme in place.
**Tests:** switching changes the settings and keybindings in effect without closing editors; name validation; the last profile per folder.

### 8.6b Profiles UI
**Goal:** the commands and pickers for profiles (SPEC §3.2 "Profiles").
**Read:** SPEC §10.12.
**Tasks:** commands and pickers for create, rename, delete and switch, in the Manage menu; the profile name in the status bar when it isn't the default; export/import in the format of SPEC §10.12 through the portal dialog.
**Tests:** each command; export/import round trip with `FakeDesktop().AnswerFileChooser`.

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

### 9.9 Error routes audit
**Goal:** every error Tag of the code has a test that asserts its route, and no error is built in a helper (CLAUDE.md "Error handling", and `AI_GUIDE.md` of `github.com/rm4n0s/errors`, in the Go module cache: `go list -m -f '{{.Dir}}' github.com/rm4n0s/errors`).
**Read:** CLAUDE.md "Error handling"; `AI_GUIDE.md` of the errors package (R3, R4, R5 and section 4); every earlier step's "Tests" lines that name route tests.
**Tasks:**
1. Make the list of Tags: every first argument of `errors.New`, `errors.NewErr` and `errors.NewErrf` in non-test code of `cmd` and `internal` (including generators under `internal/.../gen`, which are exempt only when they cannot be run from a test: then say so in the report). Make the list of Tags that no test asserts: a test counts when it asserts the Tag in a route (`HasRoute("...->Func.Tag")`) or through `appErr.Tag`; a Tag that is only named in a comment or a constant does not count. Keep the script that makes both lists as `scripts/error-tags.sh` (prints the Tags and, with `--untested`, those without a test) so the check can be repeated; it must not need network and must run in a second or two.
2. For each Tag without a test, write the test that makes the failure happen and asserts the route from the entry function (`HasRoute("Entry.Func->Inner.Func.Tag")`, the Tag included) and, where the Tag carries metadata, the metadata. Make the failure happen with the seams that exist (injected file systems, fake clipboard, fake desktop, closed channels, cancelled contexts, unwritable folders in a temp dir, bad input); where a failure cannot be made to happen without changing production code, add the smallest seam (an option or an injectable function, never a global) and use it. Where it truly cannot be tested (an `os` call that cannot fail on a healthy system and has no seam worth adding), list the Tag in `docs/ERROR-TAGS-UNTESTED.md` with the reason; this file is the only allowed exception list and the step is "done" only when it holds nothing that a seam of a few lines would have made testable.
3. R4: no error is built in a helper. Find the functions whose body builds and returns an error through `errors.New`, `errors.NewErr` or `errors.NewErrf` and that are called from several places or only to wrap the construction (for example `errNoWindow`, `errCancelled`, `errInvalid`, `errInvalidRegex`, `invalid`, `preferenceInvalid`, `cssEmptyValueError`, `(*parser).fail`, `(*scanner).fail`; the scan must also look at helpers longer than six lines and at helpers that return `*errors.Error`), and inline the construction into the function that detects the failure, so the helper's name leaves the routes. Update every route assertion that names a helper. A function that detects a failure and builds the error itself is fine however small it is.
4. R5: no package-level `errors.New`/`NewErr`/`NewErrf` (a sentinel is `&errors.Error{Tag: ...}`); fix `errFailed` in `internal/gui/keybindingsui/rig_test.go` and any other.
5. Add to CLAUDE.md "Error handling" one bullet for R4 (construct in the failing function, never in a helper) and one for R5 (sentinels are struct literals) and a line telling to read `AI_GUIDE.md`; add `scripts/error-tags.sh --untested` (must print nothing) to the Definition of done in CLAUDE.md.
**Tests:** the new route tests; an archtest rule or a Go test that fails on a package-level `errors.New` and on a function that only builds and returns an error (so R4 and R5 stay true), with the testdata of the archtest style; `scripts/error-tags.sh --untested` prints nothing (except what `docs/ERROR-TAGS-UNTESTED.md` lists).

---

## M9b: One window, workspace tabs and the tray

### 9b.1 Workspaces in one window
**Goal:** one Glutter window holding every workspace as a tab, without the strip yet (SPEC §10.14, D17).
**Read:** SPEC §10.14, §10.10, §10.11, §10.12; the "Several windows as built" paragraph of §5 and step 7.8's entry in `docs/IMPLEMENTED.md`.
**Tasks:**
1. Rename `app.Window` to `app.Workspace` and `app.Windows` to `app.Workspaces` (a mechanical rename first, in its own commit-sized change, with every test passing), then make `Workspaces` hold *window tabs* (each with a kind: a workspace now, an extension tab from step 11.7, so the strip, the restore, the tray and the keys never assume a folder) and host them in the one implicit window: an `IndexedStack` of the tabs' workbenches under a `TickerMode` (only the active one ticks and paints), the focus and the key dispatcher of the active tab, the window title of the active tab. No `RegularWindowController` window is made any more; remove the multi-window code and its Glutter workarounds that no longer apply (update `docs/GLUTTER-ISSUES.md` and §20).
2. Opening, activating, moving and closing tabs (`Workspaces.Open/Activate/Move/Close`); closing a tab with the questions about unsaved changes or hot exit; the last tab leaves a No Folder tab; the quit flow over every tab; `workbench.action.newWindow` and `workbench.action.closeWindow` with their new titles.
3. State: `workspaceTabs`, `activeWorkspaceTab` and `windowGeometry` in `state.json` (additive), an old `openWindows` list read as tabs, `window.restoreWindows` read as in §10.14, the UI state file of each tab as before.
4. Profiles per tab (§10.12 as built moves from windows to tabs unchanged).
**Tests:** open, activate, move and close tabs; switching keeps each tab's editors, scroll positions, focus and view states; an inactive tab's watcher still applies an external change; closing a tab with unsaved changes (prompt, hot exit); the last tab leaves a No Folder tab; quit over several tabs; restore of every `window.restoreWindows` value; the migration of an old `state.json`; two tabs with two profiles; the existing tests of steps 7.5–8.6b rewritten from windows to tabs (list them).

### 9b.1b Glutter 1.7 and the end of the workarounds
**Goal:** Emetos on Glutter v1.7.0, with every workaround whose fix is in that release removed (CLAUDE.md "Glutter", `docs/GLUTTER-ISSUES.md`).
**Read:** `docs/GLUTTER-ISSUES.md` (every entry); Glutter v1.7.0's `docs/IMPLEMENTED.md` entries 27.1–27.10 and the APIs they name (in the Go module cache); every `// Glutter:` comment in `cmd` and `internal` (`grep -rn "// Glutter:"`).
**Tasks:**
1. `go get github.com/rm4n0s/glutter@v1.7.0` and `go mod tidy`; the build, the tests and every golden as before (a golden that changes is looked at and explained: a Glutter rendering change is accepted only when it is the release's intent).
2. Replace each workaround with the API of the fix and remove its `// Glutter:` comment: `kit.Hover` on `RawTooltipOpts.HoverDelayFunc`/`OnShown`/`OnHidden`/`Controller`/`DismissOnKey` (27.6); `SplitView` on `MouseRegionOpts.HoldCursorWhilePressed` (27.7); `kit.Tree` keeping its nodes with `SetChildren`/`InsertChild`/`RemoveChild`, lazy `HasChildren` and `NodeKey` (27.9); the list and tab drags on `DraggableOpts.Slop`, `CancelOnEscape` and `DragTargetOpts.RepeatMove` (27.8) where that keeps their behavior (the 4 px slop, Escape, scrolling and auto-expand while the pointer rests); the window title, activation, full screen and minimize through `widgets.WindowController`/`MaximizableWindowController`/`WindowControllerOf` and the tests through `Tester.ViewTitle` (27.4); the tests' unmount cleanup in `testutil.New` dropped in favor of `gluttertest.New`'s (27.2) and `ProcessOptions.WatchFiles` reconsidered (keep it only if a reason other than the leak remains); the `SaveDialog` test seam replaced by `FakeDesktop.DisableFileChooser()` (27.2); the `Scrollable` workaround (27.1); whatever of the first-window and placement workarounds step 9b.1 left (27.4, 27.5).
3. `scripts/perf.sh`: use the split startup log of 27.10 and the focus events of 27.3 (the idle-and-unfocused target becomes measurable headless); record the new cold start in `docs/PERFORMANCE.md`.
4. Remove each entry of `docs/GLUTTER-ISSUES.md` whose fix is now used (keep an entry only if the release does not cover all of it, rewritten to what is still missing); delete the file when no entry is left, and update SPEC §20, D8's note and every SPEC paragraph that describes a removed workaround.
**Tests:** every existing test passes on v1.7.0 (the kit, tree, list, tab and drag tests are the guard for the replaced workarounds); new tests where a new API changes behavior (a tooltip hidden by a key, the cursor held during a sash drag, a lazy tree node, Escape cancelling a drag, the title read through `Tester.ViewTitle`, a test of the save dialog without a file chooser through `DisableFileChooser`).

### 9b.2 The title bar and the workspace tab strip
**Goal:** Emetos's own title bar row with the menu, the workspace tabs and the window buttons (SPEC §10.14, §10.1).
**Read:** SPEC §10.14, §10.1; Glutter: `widgets/window_decoration.go`, `widgets/run_app.go` (`Decorations`), `ui/window_info.go` (`PerformWindowAction`).
**Tasks:** `Decorations: DecorationsApp` and `widgets.WindowDecoration` with the row as its content (35 px, the `titleBar.*` colors); the menu (`window.menuBarVisibility` `compact`, the new default, as a `≡` button; `classic`/`visible`/`toggle`/`hidden`), replacing the separate menu bar row of §10.1; the tab strip in `gui/workbench` (icon, name, disambiguated duplicate names, dirty dot, close button, tooltip with path and profile, active and inactive colors, widths from 120 to 240 px, horizontal scroll with the active tab in view, middle click, the `+` menu with its one entry **Workspace...** that opens the desktop portal's folder chooser and opens the folder as a new tab (step 11.7 adds the extension tab kinds), the context menu); drag on the empty strip moves the window, double click maximizes, right click opens the window menu; full screen and zen hide the row.
**Tests:** the strip's layout tables (widths, shrinking, scrolling, duplicate names); dirty dot follows the documents of the tab; close button and middle click; the context menu items; each `window.menuBarVisibility` value; window actions sent through a fake controller; goldens of the row (Dark Modern, Light Modern; one tab, three tabs with a dirty one and two of the same name, overflowing tabs, the compact and classic menus).
**Proof:** method B, `scripts/proof.sh docs/implemented-proof-images/9b.2-workspace-tabs.png --commands '[["dev.openFolderInNewTab",{"path":"testdata/proof/9b.2/notes"}],["dev.openFile",{"path":"testdata/proof/9b.2/notes/main.go"}]]' -- testdata/proof/9b.2/api`: the window with two workspace tabs, the second active with a file open (add `dev.openFolderInNewTab` in this step, SPEC §15.2).

### 9b.3 Launches open tabs
**Goal:** every launch and every Open Folder opens or focuses a tab (SPEC §10.14, §10.11).
**Read:** SPEC §10.14, §10.11, §10.12.
**Tasks:** `Workspaces.Open` with the placement rules of §10.14 for the first instance's command line, a hand-over over the socket, Open Folder, Open Recent (Ctrl forces a new tab), Close Folder, `-n`, `-r`, files, `--goto`, `--diff`, `--merge`, `--wait`, `--profile`; a launch shows a hidden window and brings it to the front; `emetos` with no argument shows the window; `window.openFoldersInNewWindow` read as "in a new tab". Extend `scripts/proof.sh` with `--launch '<args>'` (repeatable): after the first frame it runs `emetos <args>` again with the same isolated runtime folder, so the run is handed over to the first instance.
**Tests:** each placement rule over a real socket (two launches of two folders give one window with two tabs; the same folder twice focuses its tab; `-r` replaces the active tab with the questions about unsaved changes; an untouched No Folder tab is replaced; files go to the active tab; `--profile` rules; `--wait` returns when the files are closed); Open Folder and Open Recent from inside; a launch while hidden shows the window.
**Proof:** method B, `scripts/proof.sh docs/implemented-proof-images/9b.3-two-launches.png --launch 'testdata/proof/9b.3/notes' -- testdata/proof/9b.3/api`: one window with the tabs `api` and `notes` after two launches.

### 9b.4 The system tray and close to tray
**Goal:** the tray icon, its menu, and the close button hiding the window (SPEC §10.14, §17).
**Read:** SPEC §10.14, §17; Glutter: `desktop/tray.go`, `desktop/tray_menu.go`, `gluttertest/desktop.go` (the fake tray), `widgets/window_controller.go` (`SetHidden`).
**Tasks:**
1. The application icon (moved here from step 10.2): a small Go program draws it with Glutter's canvas, a test renders it at 16, 22, 24, 32, 48, 64, 128, 256 and 512 px into `assets/icons/` (and the tray's pixmaps through `TrayIconFromRGBA`); **ask the user** to look at it before going on.
2. `desktop.NewSystemTray` around the app with the icon, the tooltip, the click (show and bring to the front, or hide), and the menu of §10.14 (the tabs, Open Folder..., New Workspace Tab, Show/Hide Emetos, Quit), kept up to date when tabs open, close, rename or reorder.
3. `window.closeToTray` (application scope, default `true`); the close button hides the window (backups written first, no frames while hidden) when a tray is shown and the setting is on, and quits otherwise (`TrayUnavailable` makes the close button quit; it is logged once, no notification); Quit from the tray and Ctrl+Q run the quit flow; a quit with unsaved changes and `files.hotExit` off shows the window first.
**Tests:** with `FakeDesktop`: the tray registers, its title and tooltip, the menu labels in order and after tabs change, a click on the icon toggles the window, choosing a tab shows it, Quit quits (hot exit and prompts), the close button hides with the tray and quits without it or with the setting off, no frames are scheduled while hidden; the icon test (sizes, non-empty pixels).
**Manual checks:** the user sees the tray icon and its menu on their desktop and closes and reopens the window from it (`docs/MANUAL-CHECKS.md`; GNOME needs the AppIndicator extension).

### 9b.5 Switching and arranging tabs
**Goal:** the keys, the picker and the drag of the workspace tabs (SPEC §10.14).
**Read:** SPEC §10.14, §10.6.
**Tasks:** `emetos.workspaceTab.next`/`previous` (Ctrl+Alt+PageDown/PageUp), `open1`...`open9` (Ctrl+Alt+1...9), `switch` (the quick pick), `moveLeft`/`moveRight`, Close Others, Close to the Right; drag to reorder with the 4 px slop and Escape to cancel; the context keys `workspaceTabCount` and `activeWorkspaceTabHasFolder`; the commands in the palette and the View menu ("Workspace Tabs" submenu).
**Tests:** each command and key through the dispatcher; the picker's items, order, filtering and accept; drag and drop reorders and keeps the active tab; Escape cancels a drag; the order is restored after a restart.
**Proof:** method B, `scripts/proof.sh docs/implemented-proof-images/9b.5-switch-picker.png --commands '[["dev.openFolderInNewTab",{"path":"testdata/proof/9b.5/notes"}],["dev.openFolderInNewTab",{"path":"testdata/proof/9b.5/web"}],["emetos.workspaceTab.switch"]]' -- testdata/proof/9b.5/api`: three workspace tabs and the switch picker open.

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
2. The icon of step 9b.4 in the icon theme folders, a `dev.emetos.Emetos.desktop` file (`Exec=emetos %F`, matching the app id), and `scripts/install-desktop.sh` for `~/.local/share/applications` and the icon theme folders.
3. A coverage table in `docs/IMPLEMENTED.md`: every row of SPEC §3.1 and §3.2, the step(s) and tests that cover it, and its status; a `NOTICE` and `third_party/` audit (Codicons CC BY attribution).
4. **Ask the user** before tagging a version.
**Manual checks:** the user looks at the icon, and uses Emetos for their own work and reports problems; each problem becomes separate fix work after this step.

---

## M11: Extensions

### 11.1 The extension contract and its lifetime
**Goal:** compiled-in extensions activated per window tab, with their failures contained (SPEC §10.15, D18).
**Read:** SPEC §10.15, §10.14, §5.2, §5.3; CLAUDE.md "Error handling" and the errors package's `AI_GUIDE.md`.
**Tasks:** the public package `extension/` (`Factory`, `Extension`, `Manifest`, `API` with the parts this step needs: commands, keybindings, menus, settings, notifications, status bar items, documents read), `extensions/all.go` (empty list plus a test-only extension under `extensions/internal/testext`), activation events, one activation per scope with a scope-bound `API`, deactivation by `ctx`, the catching of errors and panics at every call into an extension (the box text, the notification, the log), `extensions.disabled`, the page "Extensions" with Enable/Disable, the archtest rules of §10.15 (with testdata like the existing rules). Add `extension/` and `extensions/` to SPEC §5.2/§5.3.
**Tests:** activation by each activation event; two scopes get two instances; closing a scope cancels the `ctx` and removes its contributions; a panic in `Activate`, in a command handler and in a status bar callback is caught (route-tested tags, the notification once, the extension inactive in that scope only); `extensions.disabled`; the page lists the manifests and states; the archtest rules catch a forbidden import.

### 11.2 Events
**Goal:** the event bus within a scope, request and reply, and the core events (SPEC §10.15 "Events").
**Read:** SPEC §10.15.
**Tasks:** `api.Events()`: `Publish`, `Subscribe` with patterns, `Request`/`Handle` with `EventNoHandler` and `EventTimeout`, topic ownership (an extension publishes and handles only its own and its declared topics), JSON copies of the values, delivery on the UI thread in order, per-frame batching of fast topics, the slow-handler log; the core events of §10.15 published by every workspace scope; `api.Documents().Read`.
**Tests:** delivery and order; patterns; request and reply, no handler, timeout, a handler's error and panic; ownership refused with a route-tested tag; values are copies (a change by the receiver does not reach the sender); typing 100 characters in one frame gives one batch; the core events for open, change, save, close, active editor and selection; no event crosses to another scope (no links yet).

### 11.3 Event sharing between window tabs
**Goal:** directional links, private workspaces, the prompt and the page (SPEC §10.15 "Event sharing").
**Read:** SPEC §10.15, §10.10.
**Tasks:** the store `event-sharing.json` in `storage.Paths` (atomic, 0600, versioned, read at start), delivery across scopes only along a link in its direction and for its topics, requests and replies along links, private workspaces (no events, no `Read` out), session links, the prompt in the receiving tab (Allow, This Session Only, Deny) for topics in the manifest's cross-tab list only, the page "Extensions › Event Sharing" with Remove and the private switches, `emetos.sharing.revoked`.
**Tests:** `notes → office` delivers notes' events to office and nothing back; a topic-limited link; removing a link stops delivery at once; a private workspace sends nothing and refuses `Read`; the prompt's three answers (the file written or not); a topic outside the manifest is never asked about; the file survives a restart; a corrupt file is ignored with a warning (route-tested); tests never touch the real state folder (`testutil.Isolate`).

### 11.4 Language packs move to `extensions/`
**Goal:** every bundled language becomes an extension, with no change of behavior (SPEC §10.15, §8.1, D13).
**Read:** SPEC §8.1, §10.15.
**Tasks:** an extension per language of §8.1 under `extensions/` (its grammar, language configuration and snippets in an `embed.FS`, registered through `language.Registry.AddExtension` from its `Activate` at `onStartup` (the registry must know every language before the first file opens), its `third_party/` licenses unchanged in `NOTICE`), the bundled themes and the Seti icon theme stay in the core; the core keeps plain text only; `scripts/update-assets.sh` writes into the new folders; D13 and §8.1 updated.
**Tests:** every existing language, highlighting and golden test passes unchanged (no golden changes); disabling `emetos.markdown` makes `.md` files plain text; the archtest rules hold for every pack.

### 11.5 Side bar views, panel views and contributions to the built-in parts
**Goal:** the workspace views of extensions outside the editor area (SPEC §10.15, §10.4, §10.13).
**Read:** SPEC §10.15, §10.4, §10.13.
**Tasks:** side bar view contributions (activity bar icon, view container, views with title actions and context keys, the box constraints, state per scope); panel view contributions (a tab of the bottom panel with its title actions and badge, moving and maximizing with the panel, state kept across panel tab switches and restored when the extension saves it, a "Toggle <Name>" command and a View menu entry); the built-in parts: `api.Output().Channel(name)` (an Output channel per extension, §10.13) and `api.Problems().Set(owner, uri, markers)` (markers owned by the extension), status bar items; `extensions/todo`: a side bar view "TODOs" listing the `TODO`/`FIXME` comments of the folder (from the file index, updated by the watcher) with click to open; `extensions/tasks`: a panel view "Tasks" that runs a command in the workspace folder (a list of commands in its settings, default `go test ./...`; started under the scope's `ctx`, stopped on close), streams its output, puts `file:line:col: message` lines into Problems and its log into an Output channel.
**Tests:** the side bar view's icon, title actions, state and constraints; the panel view's tab, badge, title actions, state and restore, moving with the panel; Output channel and Problems markers from an extension appear and go when the extension is disabled; the TODO list follows file changes; the tasks panel runs a fake command (a test binary or `go env` under the test's temp folder), streams it, fills Problems, stops on cancel and on scope close.

### 11.6 Editor tabs, editor title actions and file views
**Goal:** the extension contributions inside the editor area (SPEC §10.15, §10.3).
**Read:** SPEC §10.15, §10.3, §10.10.
**Tasks:** editor tab kinds (a tab with no file in the editor area, several instances, split/move/pin/close, restored from the state the extension saves, a dirty or busy mark, the editor-kind seam of `internal/gui/app/panes.go`); editor title actions (micro icons at the right end of each editor group's tab strip with an icon, a tooltip, a command and a `when` condition, ordered by extension and `order`, overflowing into a `...` menu); file views: the micro-tabs at the right end of the breadcrumbs row (or a 22 px strip when breadcrumbs are hidden) shown only for files with at least one view, Text always first, the choice per editor tab remembered and restored, `workbench.editor.defaultFileViews` (a map of glob to view ID, application scope), `workbench.action.cycleFileView`, edits from a view through the document API (undo, dirty, save, hot exit unchanged), the fall back to Text on failure; `extensions/markdown`: the preview file view (headings, paragraphs, emphasis, lists, block quotes, code blocks with the syntax colors of their language, tables, links opened with the `OpenURI` portal, local images) following edits live and the scroll of the Text view, and an editor title action "Open Preview to the Side" shown for Markdown only; `extensions/notes`: the pattern of an agent extension without a network: a side bar view listing the workspace's notes (kept in the extension's state), a note opened as an editor tab (several at once), and an editor title action "New Note" that opens a new note tab in that group.
**Tests:** editor tabs open, split, move, close and restore; several instances; the title actions appear by their `when`, overflow, and run their commands in the right group; micro-tabs appear only for matching files and disappear when the extension is disabled; switching keeps the document and its undo; the default view setting; restore of the chosen view; a failing view falls back to Text; the Markdown preview renders each element (goldens, Dark Modern and Light Modern) and follows edits; the notes flow (list → tab, New Note icon → tab, restore).
**Proof:** method B, `scripts/proof.sh docs/implemented-proof-images/11.6-editor-extensions.png --commands '[["dev.openFile",{"path":"testdata/proof/11.6/project/README.md"}],["workbench.action.cycleFileView"],["notes.newNote"]]' -- testdata/proof/11.6/project`: a Markdown file in its preview with its micro-tabs, a note tab beside it, the micro icons at the right of the tab strip and the notes view in the side bar.

### 11.7 Window tab extensions
**Goal:** extension tabs in the window's tab strip and the `+` menu entries (SPEC §10.15, §10.14).
**Read:** SPEC §10.15, §10.14.
**Tasks:** the window tab contribution (a tab kind with its icon, title and widget; its `+` menu entry; its state saved and restored; it is its own event scope; it is in the tray menu, the switch picker and the keys), the `+` menu listing "Workspace..." then each tab kind; `extensions/scratchpad`: a window tab "Scratchpad" (notes kept in the tab's state across restarts) with a feed "Recent edits" that subscribes to `emetos.document.changed` from other tabs, so it exercises the prompt and the links of 11.3 end to end.
**Tests:** the `+` menu entries appear and disappear with the extension; opening, restoring and closing a Scratchpad tab; its scope gets no events without a link and gets them after Allow; the tray menu and the picker list it.
**Proof:** method B, `scripts/proof.sh docs/implemented-proof-images/11.7-window-tab-extension.png --commands '[["dev.openWindowTab",{"kind":"emetos.scratchpad.tab"}],["dev.openPlusMenu"]]' -- testdata/proof/11.7/project`: a workspace tab and a Scratchpad tab, with the `+` menu open (add the two dev commands in this step, SPEC §15.2).

---

## M12: Language intelligence (area 3)

### 12.1 Plan area 3 with the user
**Goal:** a specification and steps for the LSP client and its UI, with `extensions/golang` and `gopls` as the first user (SPEC §3 area 3, D5, §10.15).
**Tasks:** write the area 3 sections of the SPEC (the LSP client, the providers API of `extension/`, completion, hover, signature help, definition and references, diagnostics, code actions, formatting, rename, the Go pack's server; the rest of the Go extension is M14, SPEC §10.17, whose steps use what these steps build) and the steps that build them into this file; **ask the user** to review them before any of them is built.

### 12.2 The JSON-RPC and LSP client core
**Goal:** a tested LSP client with no UI (SPEC §9.12 "The client").
**Read:** SPEC §9.12, D5, D6, D20; the LSP 3.17 specification (online, microsoft.github.io/language-server-protocol); VS Code's `vscode-languageclient` as a reference for behavior (in `reference/` if present, else online; do not copy).
**Tasks:** `internal/core/lsp`: the framing (`Content-Length`, a size limit per message), JSON-RPC requests/responses/notifications with ids, cancellation from `ctx`, the server-to-client requests of §9.12 as handlers, `initialize` with Emetos's client capabilities (only what area 3 builds), the position encoding negotiation and conversion (byte columns ⇄ UTF-16/UTF-8), document sync (open, incremental or full change, will save, did save, close) from the document model's change events with versions, `shutdown`/`exit`; the typed messages of area 3 (a hand-written subset of the protocol types, no code generator); `internal/core/lsp/lsptest`: the fake server program (scripted answers from JSON, traffic recorded, crash and stall on command) and a harness; add `core/lsp`, `core/lsp/lsptest` and `core/langfeatures` to SPEC §5.2.
**Tests:** framing (partial reads, several messages in one read, a wrong header, a message over the limit), request/response matching, cancellation sends `$/cancelRequest`, server requests answered, the initialize handshake, position conversion (table with ASCII, 2-, 3- and 4-byte characters, line ends), incremental and full sync against a fake server for a sequence of edits (the server's copy equals the document), shutdown, a server that dies mid-request (route-tested errors).

### 12.3 Language server contributions and their lifetime
**Goal:** servers declared by extensions, started per workspace tab, with their Output channel and status (SPEC §9.12 "Servers and their lifetime", §10.15).
**Read:** SPEC §9.12, §10.15 (and its "As built" notes), §10.13.
**Tasks:** `Contributes.LanguageServers` in `extension/`; the host starts a server per tab and id on the first document of its languages or at activation, in its own process group, restarts it after a crash (5 in 3 minutes, then a notification with Restart and Show Output), stops it with a grace period on tab close or disable; the Output channel with `<id>.trace.server`; the status bar item from `$/progress`; `window/showMessage` and `showMessageRequest` as notifications with their actions; `workspace/configuration` from the settings section; `workspace/didChangeWatchedFiles` from the watcher; `<extension id>.restartServer`; `core/langfeatures` (the provider registry, merging, LSP to Emetos conversions, `WorkspaceEdit` application) and the Go provider API of `extension/` (`api.Languages().Register...Provider`) with its containment; `extensions/golang` declares `gopls`.
**Tests:** with the fake server: start on first document, one per tab, settings served, a crash and the restarts and the final notification, stop on close (no process left), trace levels in the Output channel, progress in the status bar, show message requests answered by the user's choice; a Go provider and a server merged; a panic in a Go provider fails only that extension in that tab (route-tested); `WorkspaceEdit` with edits in open and closed files and file operations, one undo per document. A test with the real `gopls` (build tag `emetos_gopls`) starts it and opens a Go file.

### 12.4 Diagnostics
**Goal:** the server's problems in Problems and in the editor (SPEC §9.12 "Diagnostics").
**Read:** SPEC §9.12, §9.4, §10.13.
**Tasks:** `publishDiagnostics` and pull diagnostics to Problems markers owned by the server; squiggles by severity and tags (`unnecessary` faded, `deprecated` struck), the hover on a squiggle with source, code and link; F8/Shift+F8 through the editor; the overview ruler and minimap marks; markers kept per version and cleared on close as VS Code does.
**Tests:** markers appear and update by version, squiggle goldens (each severity and tag, Dark Modern and Light Modern), the hover's content, F8 order across files, clearing on close and server stop.
**Proof:** method B with the fake server or `gopls`: a Go file with an error squiggle, its hover, and the Problems panel.

### 12.5 Completion
**Goal:** the suggest widget (SPEC §9.12 "Completion").
**Read:** SPEC §9.12, §9.9, §10.6 (the fuzzy scorer).
**Tasks:** triggers (typing with `editor.quickSuggestions`, trigger characters, Ctrl+Space, re-filter while typing, incomplete lists re-requested), the widget (kind icons, label details, detail, the details pane with Markdown, sizes and placement as VS Code's, keyboard and mouse), filtering and sorting with VS Code's rules, accepting (Enter/Tab per setting, insert/replace ranges, snippets through `core/snippet`, `additionalTextEdits`, the item's command, commit characters), `completionItem/resolve`, word-based suggestions, user snippets as rows.
**Tests:** the scorer and sort table (ported cases from VS Code's `completionModel.test.ts`), each trigger, incomplete lists, accept with each edit kind and a snippet, additional edits in one undo step, resolve of the focused row only, commit characters, word-based fallback, widget goldens (list, details pane, Dark and Light).
**Proof:** method B: the suggest widget open on a Go file with the details pane.

### 12.6 Hover and signature help
**Goal:** the hover and the parameter hints (SPEC §9.12).
**Read:** SPEC §9.12.
**Tasks:** hover after the delay and with Ctrl+K Ctrl+I, sticky hover, Markdown with colored code, several providers stacked, the diagnostics part (12.4) in the same hover; signature help on triggers and Ctrl+Shift+Space with the active parameter, overload switching, re-trigger and close rules.
**Tests:** delay with the fake clock, sticky behavior, stacking, signature help triggers, active parameter highlighting, overloads, goldens of both widgets.

### 12.7 Navigation and symbols
**Goal:** go to definition and friends, the peek view, references, document highlights, outline, symbol pickers (SPEC §9.12 "Navigation", "Symbols").
**Read:** SPEC §9.12, §9.6, §9.7, §10.9.
**Tasks:** definition, declaration, type definition, implementation, references with their keys and menu entries; Ctrl+hover link and Ctrl+click; the peek view (an inline zone with the location list and an embedded editor) for several results and Peek Definition; navigation history entries; document highlights replacing word occurrences; the Outline view in the Explorer; breadcrumbs symbols and their picker; sticky scroll from symbols; Go to Symbol in Editor and in Workspace with their quick open prefixes.
**Tests:** one result jumps and records history; several open the peek view (golden), Enter opens, Escape closes; Ctrl+hover underline; highlights by kind; the outline tree follows the cursor; breadcrumbs and sticky scroll use symbols when present and fall back without them; both symbol pickers filter and open.
**Proof:** method B: the peek view of references on a Go identifier.

### 12.8 Code actions, formatting and rename
**Goal:** changes made by the server (SPEC §9.12 "Code actions", "Formatting", "Rename").
**Read:** SPEC §9.12, §10.11 (saving).
**Tasks:** the light bulb and the action list (Ctrl+., Quick Fix, Refactor, Source Action), applying edits and commands, `workspace/applyEdit` from the server; `editor.codeActionsOnSave`; Format Document/Selection, format on save (both modes), on type and on paste, `editor.defaultFormatter`, the save timeout; rename with `prepareRename`, the input box, apply and preview.
**Tests:** the light bulb appears for a diagnostic and opens the list; applying an action with a multi-file edit and a command; organize imports on save before formatting; formatting modes with one undo step each; the timeout path; rename refused by `prepareRename`, applied across files, previewed and partly accepted; goldens of the action list and the rename box.
**Proof:** method B: the code action list on a Go diagnostic.

### 12.9 Code lens, inlay hints, semantic tokens, folding and selection ranges
**Goal:** the remaining editor features of area 3 (SPEC §9.12).
**Read:** SPEC §9.12, §8.6, §9.2.
**Tasks:** code lenses (request, resolve on view, layout as a line above, click runs the command); inlay hints (the four modes, layout between characters without changing the text, hover and click); semantic tokens (full and delta, the legend, theme rules, drawn over TextMate colors, refresh); folding ranges from the server replacing the indentation provider; selection ranges for Expand/Shrink Selection.
**Tests:** lens layout and click; inlay hints do not change the text, cursor movement over them, the modes with Ctrl+Alt; semantic token decoding (delta edits) and colors (golden); server folding ranges used and the fallback; selection range expansion table; with the real `gopls` (build tag) hints and semantic tokens on a Go file.
**Proof:** method B with `gopls` installed (else the fake server): a Go file with inlay hints, semantic colors and a code lens.

---

## M13: The Agents extension

### 13.1 The `claude` adapter's protocol
**Goal:** a Go client of the `claude` program's stream-json mode, with no UI (SPEC §10.16).
**Read:** SPEC §10.16, §10.15; the Agent SDK documentation of the stream-json input and output and of the control requests (Anthropic's Python SDK, `claude_agent_sdk/_internal/query.py` and `transport/subprocess_cli.py`, is the reference of the wire shapes; read it online, do not copy it).
**Tasks:** `extensions/agents/claude`: find the program (`agents.claude.path`, else `PATH`); `AuthStatus(ctx)` from `claude auth status` (JSON, exit status); `Start(ctx, opts)` spawning the command line of §10.16 in the workspace folder in its own process group (killed with the scope's `ctx`); typed messages for `system/init`, `assistant` (text, thinking, `tool_use`), `user` (`tool_result`), `stream_event` (partial text), `result` (cost, turns, error) and unknown types kept as raw JSON; sending user messages; the control channel (`initialize`, `interrupt`, `set_permission_mode`, `set_model`, `rewind_files` out, `can_use_tool` in with `allow`/`updatedInput`/`deny`, request IDs, timeouts); capability detection from `system/init`; a fake `claude` (a test program built by the tests, replaying JSONL fixtures and answering control requests) and fixtures written from the documented shapes. Never `--bare`, never `CLAUDE_CONFIG_DIR`, never a token: a test checks the command line and the environment the adapter builds.
**Tests:** each message type parses (table of fixtures); partial text joins; a turn with a `can_use_tool` round trip; interrupt; a dead program and malformed lines give route-tested errors; the auth status cases (logged in, not, program missing); cancel kills the process group. A test against the real program carries the build tag `emetos_claude` and is skipped when `claude` is not installed; running it is a manual check (it uses the user's own account).

### 13.2 Sessions: the side bar view, the session tab and the title action
**Goal:** talk to the agent from the editor area (SPEC §10.16).
**Read:** SPEC §10.16, §10.15, §10.3.
**Tasks:** use the Markdown renderer of `extension/ui/markdown` (step 11.6); `extensions/agents`: the "Agents" side bar view (the session index in the extension's state: ID, name, time, the first message as title; resume, rename, delete from the list, New Session), the session editor tab (the conversation as in §10.16, streaming text, thinking collapsed, tool calls as rows with their results, the busy mark, the input box with `/` commands from the handshake and `@` file completion, Stop, the model and permission mode pickers), the editor title action "New Session (runs Claude Code)" with a generic agent icon, "Add Selection to Agent" (Alt+K, editor context menu), `agents.attachActiveFile`, Log in (the pseudo-terminal panel view of §10.16), the program-missing state, restore of open session tabs (resumed with `--resume` when the user sends the next message, not at start).
**Tests:** with the fake program: a new session streams into the tab; the side bar lists, renames, resumes and deletes; two session tabs at once; Stop; `/` and `@` completion; Add Selection inserts the reference; the not-logged-in and missing states; restore; goldens of the session tab (Dark Modern, Light Modern).
**Proof:** method B, `scripts/proof.sh docs/implemented-proof-images/13.2-agents.png --config testdata/proof/13.2/config --commands '[["agents.newSession"],["dev.agents.replay",{"fixture":"testdata/proof/13.2/session.jsonl"}]]' -- testdata/proof/13.2/project`: a session tab with a replayed conversation (the fake program, set by `agents.claude.path` in the fixture config), the side bar view and the micro icon (add the dev command in this step, SPEC §15.2).

### 13.3 Permissions, proposed edits, plans and checkpoints
**Goal:** the user decides what the agent may do, and sees every edit as a diff (SPEC §10.16, §9.11).
**Read:** SPEC §10.16, §9.11, §10.15.
**Tasks:** `api.Editors().OpenProposedChange` in `extension/` (the diff editor with the proposed text on the right, editable, Accept and Reject in its title, the answer and the final text returned; closing it is Reject); `can_use_tool` for `Edit`/`Write`/`NotebookEdit` through it (`updatedInput` when the user changed the text); the tool dialog in the session tab (the command or the URL shown, Allow once, Allow for this session with the request's suggestions, Deny with a message); `ExitPlanMode` (the plan as Markdown, Approve, Keep Planning); `AskUserQuestion`; "Rewind code to here" on user messages of sessions Emetos started (`rewind_files`; no capability names it, SPEC §10.16 "As built (step 13.1)"); the notification when a turn ends in a hidden tab; the status bar item; the Output channel "Agents".
**Tests:** with the fake program: an Edit request opens the diff, Accept answers `allow`, an edited Accept sends `updatedInput`, Reject and close answer `deny`; the Bash dialog's three answers; a plan approved and kept; a question answered; rewind sends the request, and a program that cannot restore shows its reason; the notification and status item.
**Proof:** method B, `scripts/proof.sh docs/implemented-proof-images/13.3-proposed-edit.png --config testdata/proof/13.3/config --commands '[["agents.newSession"],["dev.agents.replay",{"fixture":"testdata/proof/13.3/edit.jsonl"}]]' -- testdata/proof/13.3/project`: a proposed edit open in the diff editor beside the session tab.

---

## M14: The Go extension

### 14.1 gopls, the status bar and the tools
**Goal:** Go's language features through `gopls` (SPEC §10.17 and the area 3 sections written in step 12.1).
**Read:** SPEC §10.17, the area 3 sections, §10.15.
**Tasks:** `extensions/golang` (which declares `gopls` since step 12.3) registers the Go settings of `gopls` under `go.gopls` (12.3 serves the section unregistered), "Go: Restart Language Server" (the `emetos.golang.restartServer` command of 12.3 with Go's title), an `extension/` API to read a server's state for the status item (12.3 has it host-side only, `Scope.ServerStatus`), formatting and organize imports on save, the status bar item (Go version from `go env GOVERSION`, the state of `gopls`, its menu), "Go: Install/Update Tools" (a panel view with the `go install` output) and the notification for a missing tool, "Go: Show Go Environment".
**Tests:** with a fake server (the area 3 test server): start, restart, state in the status bar, settings passed; format and organize imports on save; the tools panel runs a fake `go`; missing-tool notification. A test against the real `gopls` carries the build tag `emetos_gopls` and is skipped when it is not installed.
**Proof:** method B with `gopls` installed (else a fake server and a note in the entry): a Go file with a hover, a completion list and the Go status item.

### 14.2 Go commands
**Goal:** the commands of VS Code's Go extension that `gopls` and the tools give (SPEC §10.17).
**Read:** SPEC §10.17.
**Tasks:** Add Tags to Struct Fields and Remove Tags (`gopls` `modify_tags` when the server has it, else editing the struct through the document API with a pure-Go tag editor), Fill Struct (the code action), Generate Unit Tests for Function (a table-driven test skeleton written by the extension into `<file>_test.go`), Toggle Test File, Run (`go run .` in a panel view).
**Tests:** tags added and removed (table of structs and options), fill struct through a fake server, the generated test skeleton (golden text), toggle between file and test file and creating the test file, run streams output.

### 14.3 Tests, lint and vet
**Goal:** run Go tests from the editor and see lint results (SPEC §10.17).
**Read:** SPEC §10.17, §10.13.
**Tasks:** the code lenses "run test | debug test" (debug shown disabled until area 6), Test Function at Cursor, Test File, Test Package, Rerun Last; `go test -json` parsed into the panel view "Go Tests" (tree of packages and tests, pass/fail/skip, output per test, durations) and failures into Problems; `go vet` and the lint tool on save into Problems (`go.vetOnSave`, `go.lintOnSave`, `go.lintTool`).
**Tests:** `go test -json` fixtures parse into the tree (pass, fail, skip, panic, build failure); lenses run the right `-run` pattern; Problems markers from a failure and from vet output; lint on save with a fake tool.
**Proof:** method B: a Go test file with its lenses and the "Go Tests" panel showing a passed and a failed test.

---

## M15: Remote Work (remote control of a workspace)

### 15.1 The protocol and the headless view
**Goal:** the message format of Remote Work and a second, off-screen view of a workspace (SPEC §10.18).
**Read:** SPEC §10.18, §10.15, §10.14.
**Tasks:** `extensions/remotework/protocol`: versioned, length-prefixed messages with a size limit on each (hello with version and token, frame tiles, frame confirm, keys with text, mouse, scroll, clipboard, notification and its action, resize, ping, disconnect with a reason), encode and decode with no allocation beyond the limit; the tile differ (64x64 tiles changed since the confirmed frame, deflate, a full refresh on request); `api.Workspace().NewHeadlessView(size)` in `extension/`, implemented in `internal/gui/app`: a second workbench of the same workspace with its own focus, cursor and scroll, rendered with Glutter's software renderer when it changes, input events delivered to it, resize, close.
**Tests:** round trip of every message (table); malformed, truncated and oversized input is refused with route-tested errors (seeded random inputs; a fuzz target run with `-parallel=1 -fuzztime=30s` and `GOMEMLIMIT=2GiB` as CLAUDE.md says); the differ sends only changed tiles and a full refresh; the headless view renders an edit made in the main view, takes typed keys into the document, and resizes (`gluttertest`).

### 15.2 Login over SSH and the Remote Work page
**Goal:** A lets listed keys in; B logs in with its key (SPEC §10.18).
**Read:** SPEC §10.18, §12.
**Tasks:** `go get golang.org/x/crypto@<latest>`; `extensions/internal/sshlogin` : the server on A (public-key only against the listed keys, the `emetos-remotework` channel and nothing else, rate limit, logging), the host key made and kept with mode 0600, the client on B (key file with the passphrase dialog, or `ssh-agent` from `SSH_AUTH_SOCK`), host key pinning in `remote-work-hosts.json` (first-use fingerprint dialog, changed key refused); the session token and the QUIC certificate fingerprint sent over the channel; `remote-work.json` (mode 0600) and the page "Remote Work" (per-workspace switch, port, idle timeout, keys added by pasting a line or choosing a `.pub` file, the host fingerprint shown, the security notes of §10.18); A listens only while a workspace has it on; the connected banner with Disconnect; notifications for logins and refused attempts.
**Tests:** login with a listed key, refusal of an unlisted key, of passwords, of other channel types and of shells; the agent path (an in-process agent); a wrong passphrase; first-use pinning and a changed host key; the rate limit; turning remote control off closes the listener and ends sessions; the state files' modes; all on loopback with ephemeral ports.

### 15.3 QUIC transport and the TCP fallback
**Goal:** the session's messages over QUIC on UDP, or SSH channels when UDP is blocked (SPEC §10.18).
**Read:** SPEC §10.18.
**Tasks:** `go get github.com/quic-go/quic-go@<latest>`; `extensions/remotework/transport`: A's QUIC listener on the same port (self-signed certificate made at start), B's dial that accepts only the fingerprint from the SSH channel, the token check, tiles as datagrams (dropped when stale, never queued), reliable streams for the rest, resume of a dropped QUIC connection with the token while SSH lives, the 3 s fallback to SSH channels with the same messages, the idle timeout, one interface for both transports.
**Tests:** on loopback: a session over QUIC; a wrong token and a wrong certificate are refused; datagrams arrive and stale ones are dropped; the fallback when UDP is blocked (a test that drops the UDP packets); resume after a dropped connection; the idle timeout ends the session; a lossy-link measurement (a UDP proxy in the test dropping 5% of packets with 50 ms of delay) reports the frame rate and the time to a full picture, recorded in the step's entry.

### 15.4 The remote window tab
**Goal:** use A's workspace from B (SPEC §10.18).
**Read:** SPEC §10.18, §10.14, §10.15.
**Tasks:** the window tab kind "Remote Work..." in the `+` menu (host, port, key file or agent; the tab titled `<host>: <workspace>` with a remote icon), the picture drawn from tiles at the tab's size (resize sends the new size), keys with their text, mouse and scroll sent, clipboard on copy and paste both ways, A's notifications shown on B with their actions run on A, reconnection after a drop, the disconnect reasons shown; A's side wires the headless view of 15.1 to the session; the tab's state saved (host, port, key path; never a passphrase or token).
**Tests:** two Emetos instances in one test process on loopback (or two `gluttertest` apps): B opens A's workspace, types into a file that changes on A, A's edit appears on B, copy and paste across, a notification's action runs on A, Disconnect on A ends B's tab with its reason, reconnection.
**Proof:** method B: two isolated Emetos instances started by the step's proof script on loopback, B's window showing A's workspace in a remote tab beside its own workspace tab.

### 15.5 Public keys by QR code
**Goal:** add an allowed key from the camera and show one's own key as a QR code (SPEC §10.18 "Public keys by QR code").
**Read:** SPEC §10.18, §10.19, §12; the Linux V4L2 API (`linux/videodev2.h`, online), gozxing's README.
**Tasks:** `go get github.com/makiuchi-d/gozxing@<latest>` (add it to `NOTICE` with its MIT license, and the Apache-2.0 license of ZXing that it ports, in `third_party/gozxing/`); `extensions/internal/keyqr`: the camera through V4L2 ioctls with `golang.org/x/sys/unix` (listing capture devices, choosing a format: MJPEG preferred, else YUYV; memory-mapped buffers; start and stop; everything under a `ctx`, released on cancel), frames to `image.Image`, decoding QR codes with gozxing off the UI thread, parsing the text with `ssh.ParseAuthorizedKey`, encoding a public key as a QR image; an image widget in `extension/ui` if there is none; the **Scan QR Code** dialog (live preview, camera picker, the confirmation with type, fingerprint and comment, the disabled button with the reason when there is no camera) on the Remote Work page; **Show My Public Key as QR Code** in the "Remote Work..." dialog (from the key file's `.pub` or the agent's key).
**Tests:** QR round trip for Ed25519, ECDSA and RSA 4096 keys (encode, then decode the image, also scaled, rotated by a few degrees, blurred and with noise); text that is not a key refused with its tag; the V4L2 layer against a fake device interface (format choice, YUYV conversion, buffer cycling, stop on cancel; no real camera in tests); the dialog with a fake camera that plays a sequence of frames (closes on the first valid key, ignores other QR codes, releases the camera on close); a test with the real camera carries the build tag `emetos_camera` and is a manual check (`docs/MANUAL-CHECKS.md`).
**Proof:** method B with a fake camera fed by a QR image of a test key (an `emetos_dev` dev command): the Scan QR Code dialog with the preview and the key's confirmation.

---

## M16: Core request topics, privileges and confinement

### 16.1 Core request topics, privileges and confinement
**Goal:** what lets an extension drive a workspace through requests, confined to it (SPEC §10.19 "Privileges", "Core request topics", "Confinement"). Steps 16.2 and 16.3 of the first plan built an extension on top of it; the user had it removed on 2026-10-29 (D32, step 23.1), and only this step stays.
**Read:** SPEC §10.19, §10.15 (events, ownership, containment, the "As built" notes), §10.11 (commands that act on windows and folders).
**Tasks:** the core request topics of §10.19 answered by every workspace scope (`emetos.commands.list`/`execute`, `emetos.files.list`/`stat`/`read`, `emetos.document.open`/`read`/`edit`/`save`/`close`, `emetos.editor.reveal`), with their payload types in `extension/`; `Manifest.Privileges` (`commands.execute`) granted by the host only to the extensions on its fixed allowlist (`emetos.terminal`; others refused with a problem on the Extensions page); `commands.execute` running core and extension commands in the requester's scope; the list of commands refused to clients (window tabs, quit, close window, open or replace a folder, new windows, profiles, user and profile settings) and its marking in `emetos.commands.list`.
**Tests:** each topic's payloads and errors (route-tested), file reads confined to the folder (`..`, absolute paths, symlinks out), an edit with a stale version refused, a privileged and an unprivileged extension (executing a command refused for the latter), every refused command class, a command run in one tab does not reach another.

---

## M17: Wrap-up

### 17.1 Update the README
**Goal:** `README.md` describes Emetos as it is after every other step (the user's request of 2026-10-26).
**Read:** `README.md`, `docs/IMPLEMENTED.md` (the entries after 10.2 and the coverage table), SPEC §1, §3, §10.15–§10.18, §16, §18.
**Tasks:** bring every section of the README up to date: the status and version (the areas and milestones now built, what is still planned), the feature lists (extensions and their seven kinds of contribution, event sharing, the bundled extensions such as TODOs, Tasks, the Markdown preview, notes, scratchpad, Agents, Go, Remote Work, and the language intelligence of M12), install and requirements (the `claude` program, `gopls` and the tools, the Remote Work port and keys, with the security notes of SPEC §10.18), where the new state files live (`event-sharing.json`, `remote-work.json`, `remote-work-hosts.json`), privacy (what talks to the network now: the agent's own program, remote control, language servers; still no telemetry), the command line, and fresh screenshots made with `scripts/screenshots.sh` (add the new ones: an extension view, a session of the Agents extension, a Go file with hovers or completion, a remote tab); every link and image resolves; the coverage table in `docs/IMPLEMENTED.md` gets the rows of the areas built since 10.2.
**Tests:** a check that every relative link and image of the README resolves (a test or a script run in the checks).
**Proof:** the README's screenshots, viewed.


---

## M17b: Bugs found on the desktop

On 2026-10-27 the user tried Emetos on their desktop and found it slow and broken ("it is very slow and freezes", "when I was adding a note the keyboard stopped working until I clicked again the input box"). A test with a real keyboard and mouse (a Wayland virtual keyboard in a nested sway window on their desktop) confirmed the bugs below; none of the tests caught them, because widget tests type through the fake platform and the proofs never typed. The user asked to fix these before anything else ("postpone the extensions I requested. Prioritize the bugs."). **Glutter is not upgraded in this milestone** (the user will say when): Glutter's bugs get an entry in `docs/GLUTTER-ISSUES.md` and a workaround in Emetos where one is possible.

What the desktop test found (screenshots and logs were in the session's scratch folder; the facts are here):
- **Every input box that starts empty with a placeholder takes only the first key** (Notes title and body, Quick Open, Find, settings search). `kit.InputBox.Build` wraps the text in a `Stack` with the placeholder only while the text is empty (`internal/gui/kit/inputbox.go`); the first key removes the `Stack`, so the `EditableText` is mounted again with a node that already has the focus, and Glutter's `EditableText` (like Flutter's) opens a text input connection only when the focus changes. The box still shows the focus; the keys are lost. A click in the box did not bring them back.
- **Keys typed right after Ctrl+P can go into the document**: the quick input focuses its box in a post-frame callback (`internal/gui/quickinput/host.go` `focusInputLater`), so the keys of a slow frame go to the editor.
- **The Markdown preview of a large document freezes the window and takes 2 GB**: `docs/IMPLEMENTED.md` (690 KB, about 150 images) made the resident memory go from 450 MB to 2.0–2.1 GB, frames took 1.2–1.65 s (almost all of it raster) and scrolling ran at 1 to 3 frames a second. The preview is one `Column` of every block in a scroll view (`extension/ui/markdown/view.go`), images are decoded at full size (`NewImageFile`), every change rebuilds the whole preview (`extensions/markdown/markdown.go` `Build`), and code blocks are highlighted (regexp2) on each build. A restored session with that preview made Emetos look frozen from its start.
- **Typing `)` types over a `)` the user typed**: `// z)`, Left, `)` gives `// z)` (VS Code: `// z))`), and `fmt.Sprintf(("x")` lost a parenthesis after a completion. SPEC §8.6.4 "Typing over" says `auto` types over only the closing characters the editor typed.
- **A completion that inserts a snippet with only `$0` never ends its session**: the gray tab stop grows over the following lines and the signature help stays open until Escape. SPEC §9.9 says the mode ends when the final tab stop is reached.
- **Glutter** (v1.9.1): (1) a panic `index out of range [0] with length 0` in `gestures/arena.go:309` (`resolveByDefault`, called from the posted `tryToResolveArena`, `internal/platform/linux/loop.go:301`): Glutter's `scheduleMicrotask` posts to the loop, so a new arena for the same pointer can exist when it runs, and the old state, now with no members, passes the `m.arenas[pointer]` check (Flutter asserts `_arenas[pointer] == state`); (2) the Vulkan renderer (`internal/gpu/explicit`, `frameBuilder.draw`) skips a draw only when its clip is empty, never when the shape is outside the clip, so every path of the scene is fingerprinted (`ui.Path.Fingerprint`, hashing all its points: 18% of the CPU) and masked on every frame, off screen too (85% of the raster time of the preview); (3) `wayland/keyboard.go:126` logs at error level the `no_keymap` format (0) that a compositor sends when the last keyboard goes, which is legal (keys work again when a keyboard comes back).
- **Idle**: with only the cursor blinking (2 frames a second), the Go heap grows by about 11 MB a second (2.5 to 5 MB per frame) until a collection; who allocates is not known yet.

Rules for this milestone: every step has display tests that type and click through the compositor (**real input**, SPEC §15.1, step 17b.1), each one seen failing before its fix (say so in `IMPLEMENTED.md`); the milestone is published only after the manager's own pass on the user's desktop (step 17b.5).

### 17b.1 Real input, and typing reaches every input box
**Goal:** text from a real keyboard reaches every input box of Emetos, and a harness proves it the way a keyboard does (SPEC §15.1 "Real input").
**Read:** SPEC §15.1, §10.2 (the input box of the kit), §10.6 (the quick input); Glutter's `widgets/editable_text_state.go` and `editable_text_input.go` (`openOrCloseInputConnectionIfNeeded`, `RequestKeyboard`, `handleSelectionChanged`), its Wayland keyboard and text input (`internal/platform/linux/wayland/keyboard.go`, `textinput*.go`); the protocols `virtual-keyboard-unstable-v1` and `wlr-virtual-pointer-unstable-v1`, online.
**Tasks:**
- **The harness** `internal/testenv/realinput` (standard library only, no cgo): a Wayland client that makes **one** virtual keyboard (a US xkb keymap in a sealed temporary file; one keyboard for the whole test, never removed while the app runs, because a compositor then sends `no_keymap`) and a virtual pointer; type text (shift for capitals and symbols), press combinations (`ctrl+p`, `Escape`, `F2`), move, click, double-click and scroll at window coordinates (the window's position from sway's IPC, `swaymsg -t get_tree`); a wait for the compositor's round trip after each key. A display-test helper (tag `emetos_display`, under `scripts/headless-wayland.sh`) starts a debug build of Emetos isolated (§15.1), waits for its first frame, sends real input, runs dev commands (§15.2) to read state, takes a screenshot (`grim`), and stops everything in `t.Cleanup`. `scripts/realinput.sh` runs the same against a nested sway window on the user's desktop for the manager's passes (`--desktop`), never on the user's own session directly.
- **The fix:** `kit.InputBox` builds the same tree whether the placeholder shows or not (the hint stays in the `Stack`, hidden when there is text), so its `EditableText` is never mounted again while it is typed in. Audit every widget of `internal/gui` and `extension/ui` that holds an `EditableText` or the editor's text input client under a condition that can change while it has the focus (grep the builds), and make each one stable. A click in a box that has the focus but no input connection opens one (`RequestKeyboard`). The quick input takes the focus when it opens, not after the next frame, so keys typed right after Ctrl+P go into it; the same for every input that a key opens (Find, Replace, rename, the explorer's new file, the settings search; audit the post-frame focus calls).
- A `gluttertest` guard: find the path of the fake platform that delivers text to the attached input client the way the Linux platform does (not `EnterText` on the controller), and write a kit test that types three characters into an empty box with a placeholder through it; it fails before the fix.
**Tests:** the kit test above; the audit's cases; display tests with real input that type `hello world` into: Quick Open, the command palette (after deleting `>`), Find and Replace, the Search view, the settings editor's search, the keybindings editor's search, an explorer rename and new file, a Notes title and body, and every other input box the audit lists; each checks the whole text arrived (through a dev command or its effect); Ctrl+P followed at once by `app.go` puts all of it in the quick input and nothing in the editor; a click in a focused box keeps typing working. Each was seen failing before the fix.
**Proof:** method C with real input: Quick Open with `app.go` typed (`17b.1-quickopen.png`) and a note with a whole title and body (`17b.1-note.png`), both through the harness.

### 17b.2 A fast Markdown preview
**Goal:** the preview of a large document opens at once, scrolls at the refresh rate and stays light (SPEC §14 "Markdown preview").
**Read:** SPEC §10.15 "As built (step 11.6)" (the renderer), §14; `extension/ui/markdown`, `extensions/markdown`; Glutter's lazy lists (`ListView` builders, slivers) and its image decoding and cache (`painting`, `NewImageFile`); VS Code's preview update delay in `reference/vscode/extensions/markdown-language-features`.
**Tasks:**
- **Virtualized:** the preview is a lazily built list of blocks (estimated heights, measured when laid out), not a `Column`; `Controller.ScrollToLine` and `TopLine` and the editor's scroll sync keep working; `Options.Hover` and `Compact` (small contents) may keep the column.
- **Images:** decoded off the UI thread at the size they are shown (box width × device pixel ratio), only for blocks near the viewport, cached by path, size and modification time within a byte budget with eviction; the alt text while loading; errors as today.
- **Updates:** a change of the document parses off the UI thread after VS Code's delay, and keeps the widgets of the blocks that did not change (keyed by their content), so a typed character does not rebuild the preview.
- **Highlighting:** code blocks are highlighted when built, cached per block.
- **Start:** a restored session with the preview of a large file draws its first frame without waiting for the preview.
- The rows of `docs/PERFORMANCE.md` for the preview (before and after, headless and with the GPU).
**Tests:** only the visible blocks are built (a count); `ScrollToLine`/`TopLine` across estimated and measured heights; the existing goldens (any change reviewed); the image cache (size, budget, eviction, errors); an edit rebuilds only its block; a benchmark with a generated 700 KB document with 150 images; a display test with real input: open the preview of that fixture, scroll it with the wheel, and check the frame stats (`GLUTTER_FRAME_STATS`) and the resident memory against §14 (headless numbers recorded; the GPU numbers come from the manager's desktop pass).
**Proof:** method B: the preview of the fixture scrolled to its middle (`17b.2-preview.png`).

### 17b.3 Brackets, snippets and signature help as in VS Code
**Goal:** typing over closing characters, the end of a snippet session and the signature help behave as SPEC §8.6.4 "Typing over", §9.9 "Session" and §9.12 (signature help) say.
**Read:** SPEC §8.6.4, §9.9, §9.12; `reference/vscode/src/vs/editor/common/cursor/cursorTypeEditOperations.ts` (`AutoClosingOvertypeOperation`, the auto-closed ranges), `reference/vscode/src/vs/editor/contrib/snippet/browser/snippetController2.ts` and `snippetSession.ts`, `reference/vscode/src/vs/editor/contrib/parameterHints/browser/parameterHintsModel.ts`.
**Tasks:** find why `auto` types over a closing character the user typed (the ranges of the auto-closed characters, when they are added and dropped) and match VS Code; a snippet whose only tab stop is the final one ends its session as soon as it is inserted (by a completion, `editor.action.insertSnippet` or Emmet), and the final tab stop does not grow with typing; the signature help closes when the cursor leaves the arguments (VS Code's retrigger on cursor moves and its close when the server has no signature) and on Escape.
**Tests:** the ported cases of `cursor.test.ts` (autoClosingOvertype), `snippetController2.test.ts` (the final tab stop) and `parameterHintsModel.test.ts` that need no TypeScript harness; real-input display tests: `// z)`, Left, `)` gives `// z))`; with a fake language server, accepting `Sprintf` (`Sprintf($0)`) and typing `("x")` gives `fmt.Sprintf(("x"))`, no tab stop is drawn after it, and the signature help is gone after Enter.

### 17b.4 The Glutter issues and the cost of an idle window
**Goal:** Glutter's bugs are written down with Emetos's workarounds, and an idle window allocates little (SPEC §14 "Idle").
**Read:** CLAUDE.md "Glutter"; Glutter's `gestures/arena.go`, `internal/gpu/explicit/frame.go`, `ui/path.go`, `internal/platform/linux/wayland/keyboard.go`; Flutter's `gestures/arena.dart` (`_resolveByDefault`).
**Tasks:**
- `docs/GLUTTER-ISSUES.md` entries for the three Glutter bugs of this milestone's list, each with what breaks, where, the fix Glutter should make (the arena: check that the arena is still `state` and has a member; the renderer: cull a draw by the bounds of its shape before building its mask, and cache a path's fingerprint; the keyboard: treat `no_keymap` as "no keyboard" at debug level), and the workaround. For the arena, find what Emetos does that triggers it (a recognizer disposed while its arena waits, a pointer id reused by a second click within one loop turn: make a `gluttertest` or real-input reproduction) and avoid it in Emetos, marked `// Glutter:`; if nothing in Emetos can avoid it, say so in the entry.
- The idle cost: profile an idle window (release build with `emetos_dev`, `EMETOS_PROFILE_DIR`, SIGUSR1 for the heap) with a 10,000-line Go file, and with the preview of 17b.2 open; find what each blink frame allocates; fix what is Emetos's (widgets rebuilt by the blink, paragraphs laid out again, per-frame slices); write down what is Glutter's.
**Tests:** a test that bounds what one idle blink frame allocates in the workbench with a file open (pumped with the fake clock, measured with `runtime.ReadMemStats` or `testing.AllocsPerRun`), at a budget set from the measurement after the fixes; the reproduction of the arena panic, passing with the workaround (or, if Emetos cannot avoid it, a test of Emetos's own part with the reason in the entry).

### 17b.5 A desktop pass
**Goal:** Emetos is used through everything it offers with real input, and what is broken is fixed or planned.
**Read:** SPEC §3 (the rows built), §15.1 "Real input"; `docs/IMPLEMENTED.md` (the coverage tables).
**Tasks:** display tests with real input (17b.1's harness) for a session as a user has it: open a folder; the explorer (expand, open with a click, new file and folder, rename, delete to the trash, undo, drag); edit a Go file (typing, auto-closing, completion and hover with a fake language server, and with `gopls` under the tag `emetos_gopls`), save, undo and redo; Quick Open, the command palette, Go to Line and Go to Symbol; Find and Replace in a file and in the Search view; the settings editor (search, change a setting by typing and with a check box), the keybindings editor (record a key), the theme picker; the diff editor and the merge editor; split groups and drag a tab; workspace tabs; the panel (Problems, Output); Notes, TODOs, Tasks; the Markdown preview; a second launch handing a file over. Each test checks its effect and takes a screenshot that is viewed. `docs/DESKTOP-PASS.md` lists every finding with its status. A bug of Emetos that is fixable in this step is fixed with a real-input regression test; a larger one is reported to the manager, who adds a step to this milestone; a Glutter one gets its `GLUTTER-ISSUES.md` entry.
**Done when:** every finding of `docs/DESKTOP-PASS.md` is fixed, or is a planned step or a Glutter issue. *(As built: the findings it could not fix in the step became step 17b.6, and the manager's pass on the user's desktop moved to the end of 17b.6, the last step of M17b.)*
### 17b.6 The findings of the desktop pass
**Goal:** the findings of `docs/DESKTOP-PASS.md` that 17b.5 reported (D4, D5, D6, D10, D11), and D12, are fixed, and the manager's pass on the user's desktop finds nothing left.
**Read:** `docs/DESKTOP-PASS.md`; SPEC §10.1 (the panel's title), §10.5, §10.6, §9.8, §15.1 (with its "As built (step 17b.5)"); VS Code's `reference/vscode/src/vs/workbench/browser/parts/compositeBar.ts` and `paneCompositeBar.ts` (the overflow of the panel's tabs), the welcome views (`viewsWelcome`, `reference/vscode/src/vs/workbench/browser/parts/views/viewPane.ts`), the hover's hide on a context menu (`reference/vscode/src/vs/editor/contrib/hover/browser/contentHoverController.ts`).
**Tasks:**
- **D5, keys typed fast** (the most important): a user who types fast, a held key repeating (25 and 50 keys a second), or keys that arrive in one batch must never lose a key or send it to the wrong place. Find why three keys 10 ms apart in the Find box leave the editor's cursor behind the match (the model alone is right at that speed), and why a letter typed in the frame after F2 starts the explorer's own find box; fix the cause (a key handled against a state that is half built), not the test's delay.
- **D4, the panel's tabs:** the view's actions (the filter box, the buttons) shrink first and never take more than the room left after the tabs; tabs that still do not fit go into a `...` button whose menu lists them, as VS Code's composite bar does; a tab's title is never cut mid-word.
- **D6:** the editor's hover hides when a context menu opens.
- **D10:** the empty Notes view shows a welcome text with a "New Note" button, as VS Code's welcome views.
- **D11:** a window tab's label is cut only when the strip has no room for it.
- **D12:** `TestAPictureThatWaitedForOnFrameAndARefreshAreHandedOver` (`internal/gui/app/headless_test.go`) fails about 3 times in 30 ("a picture was handed over after Close"), with or without cgo: find whether a Remote Work view really hands a picture to its client after `Close` (a race between the frame callback and `Close`) or the test is wrong, and fix the cause; the test passes 200 times in a row; the same for `TestTheCoreTopicsDriveARealWorkspace`, which fails about once in 20 runs under `-race`.
**Tests:** each finding gets a real-input regression test (D12: the test run 200 times), seen failing before its fix; for D5, the tests of 17b.1 and 17b.5 that type also run with a 10 ms key delay and with all keys in one batch (Find, F2 rename, new file, Quick Open, the command palette, settings search, Notes), and a held key repeats into the editor and into an input box without a lost or doubled character; for D4, the panel with Problems, Output, Tasks and the extensions' tabs with badges at 1280 and 1024 px wide, no tab text cut.
**Done when:** every finding of `docs/DESKTOP-PASS.md` is fixed, or is a planned step or a Glutter issue; then the manager does the pass on the user's desktop (moved here from 17b.5): a nested sway window there with `scripts/realinput.sh --desktop`, every screenshot viewed, the Markdown preview and an idle window measured with the GPU (§14), anything new added to `docs/DESKTOP-PASS.md` and fixed or planned; only then the step is merged and M17b is published.

---

## M17c: Remove the Agents extension

The user, on 2026-10-27: "Remove claude code extension. I don't want to read any conditions or accept any terms, it exhausts me mentally." (D26.) This replaces the earlier plan of this milestone (downloading Claude Code as VS Code's extension does), which was never started.

### 17c.1 Remove the Agents extension
**Goal:** Emetos has no Agents extension and nothing that runs or mentions the `claude` program, and a user whose last session had Agents tabs starts cleanly (SPEC §10.16, D26).
**Read:** SPEC §10.16 (the removal note), §10.15 (how a window restores tabs and views of extensions), §10.19, D26; `git grep -n -i -e agents -e claude` (every hit is in scope).
**Tasks:**
- Delete `extensions/agents` (with `claude`, `claudetest`, `internal/fakeclaude`, `testdata`) and its line in `extensions/all.go`; its settings, commands, keybinding (Alt+K), menu items, view container, dev commands (`dev.agents.*`), the build tag `emetos_claude` and its real test, and its parts of `scripts/` (`scripts/screenshots.sh` and any proof script).
- Move the pseudo-terminal code (`extensions/agents/pty.go` and its tests) to `extensions/internal/pty`, a package of its own (start a process in a new session with the terminal as its controlling terminal, read, write, resize, close the process group), with its tests; nothing uses it until M17d.
- Keep `api.Editors().OpenProposedChange` of `extension/` (any extension may use it) with its tests; remove what only the Agents extension used and nothing else needs (say what in the report).
- **Old state:** a restored window whose state has tabs of `agents.session`, the `agents.container` view or the `agents.login` panel drops them without an error dialog (one log line at info level); the extension's state files are left where they are. A test restores such a state.
- Documents: `README.md` (features, requirements, privacy, screenshots: regenerate with `scripts/screenshots.sh` and view them), `docs/IMPLEMENTED.md` (the coverage tables: the Agents rows marked removed in 17c.1), `docs/MANUAL-CHECKS.md` (the Agents items removed), `CLAUDE.md` (nothing about `claude` the program remains; the `openpgp` permission goes), `docs/DECISIONS-TO-REVIEW.md` (entries 97 to 102 marked withdrawn), SPEC (every remaining mention: §3, §10.19, §15.2 examples).
**Tests:** the restore test above; `git grep -n -i -e "agents\." -e "claude"` lists only history (the SPEC's removal note and D26, `docs/IMPLEMENTED.md` entries, `CLAUDE.md` as the name of this file, proof images) and is pasted in the step's entry; the standard checks.

---

## M17d: The terminal

The user, on 2026-10-27: "Put a new extension to run terminal and working with events ..." and on 2026-10-28: "make the terminal extension work like in vscode". VS Code's integrated terminal as an extension, with an emulator of its own (no xterm.js: the user, 2026-10-28, "dont use xterm.js !!!"), its terminals as events and requests that other extensions use, and what VS Code's terminal does by default: shell integration, IntelliSense, terminals in the editor area and persistent sessions (SPEC §10.25, D27). **Rule for every step of M17d:** nothing of xterm.js is used, read, fetched, ported or copied (code, tests or assets), and it is not added to `reference/`; VS Code's own code may be read for the behavior to match, but where it is built on xterm.js (its `browser/xterm/` folder, its xterm addons) only the visible behavior is matched.

### 17d.1 The emulator
**Goal:** Emetos's own terminal emulator with no UI, written in Go from the published descriptions of the terminal (SPEC §10.25 "The emulator"). **Nothing of xterm.js** is read, fetched, ported or copied, code or tests.
**Read:** SPEC §10.25, D27; ECMA-48 (5th edition); the DEC parser state machine at https://vt100.net/emu/dec_ansi_parser; the VT100, VT220 and VT510 manuals at https://vt100.net/docs/; "XTerm Control Sequences" (https://invisible-island.net/xterm/ctlseqs/ctlseqs.html); the `xterm-256color` entry of terminfo (`infocmp xterm-256color`); Unicode's East Asian Width and emoji data. Read them online; nothing is added to `reference/`.
**Tasks:** `extensions/terminal/emulator`: the parser, the input handler (the sequences of §10.25), the buffer with the scrollback and the alternate screen, reflow on resize, wide and combining characters, the replies to the program (device status and attributes) through a callback, the title, colors, working folder and hyperlinks a program sets, the key encoder (from a key, its modifiers and the modes to bytes) and the mouse encoder, serialization; bounded memory (the scrollback limit, a cap on a line's length and on an OSC string). Each sequence's handler names the document and section it follows (`// ctlseqs: CSI Ps J`, `// ECMA-48 8.3.39`). **Speed** (SPEC §10.25 "Speed", §14 "Terminal throughput" and "Terminal memory"): the ASCII fast path, allocation-free sequences, 8-byte cells with the shared style table, pages in a ring, dirty rows, and the emulator owned by one goroutine with a locked snapshot of the dirty rows for the UI.
**Tests:** Emetos's own, table-driven, from the documents: every sequence of §10.25 (with its defaults, its limits and what is ignored), the parser's states (including sequences split across reads, C1 controls and invalid UTF-8), the buffer (scroll regions, the alternate screen, wide characters at the right edge, combining marks), reflow, the key and mouse encoders against the tables of `ctlseqs`; a round trip: any screen serialized and fed to a new emulator gives the same cells (seeded random sequences); fuzzing of the parser (with the limits of CLAUDE.md "Memory"); recorded output of real programs (`vim`, `less`, `htop`, `tmux`, `git log --color`, made by the test's own pseudo-terminal when they are installed, skipped otherwise) drawn to the screen they show, checked by a golden of the cells; benchmarks of the throughput budgets of §14 (plain ASCII, frequent colors, CJK and emoji, scrolling in a region, cursor motion), each held to its budget by a test that fails below it (skipped under `-race`); the memory of 10,000 lines of scrollback; zero allocations per idle snapshot (`testing.AllocsPerRun`).

### 17d.2 The Terminal panel
**Goal:** terminals in the panel that a user works in as in VS Code (SPEC §10.25 "The process", "The UI", "Commands, keys and context keys").
**Read:** SPEC §10.25, §10.15 (panel views, commands, keybindings, settings, colors of extensions), §9.3, §14; VS Code's `reference/vscode/src/vs/workbench/contrib/terminal/` (the commands, settings and their defaults, `terminalConfiguration.ts`, `terminalColorRegistry.ts`, the default `commandsToSkipShell`, flow control constants).
**Tasks:** `extensions/terminal` (`emetos.terminal`): profiles and the shell, the environment, the working folder, the pseudo-terminal (`extensions/internal/pty`) with flow control, resize, close and kill; the widget (drawn as SPEC §10.25 "Speed" says: the glyph atlas and one `DrawAtlas` for the glyphs and one for the backgrounds and decorations, box-drawing and block characters drawn as shapes, a frame with no change reuses its picture, the cursor, colors and fonts, the scrollbar and the wheel, selection, copy and paste, the context menu, keys through the encoder and `commandsToSkipShell`, the input method, the bell), several terminals with the tabs list and splits; every command, key, context key, setting and color of §10.25 registered with VS Code's ids and defaults (find, links, `runSelectedText`, `runActiveFile`, `sendSequence`, `rename` and `selectDefaultShell` are 17d.3's); the refusal class `terminal` for clients of a hosted workspace.
**Tests:** the processes with a test shell (a small Go program built by the test that echoes, draws with sequences, uses the alternate screen and tracks the mouse) and with `/bin/sh`; flow control (a flood keeps frames under the budget of §14, measured with the fake clock); the widget's goldens (dark and light: a prompt, colors, a selection, the alternate screen, splits and the tabs list); **real input** (17b.1): Ctrl+\` opens a terminal, typing `echo hello` and Enter shows `hello`, Ctrl+Shift+C and V copy and paste, the wheel scrolls back, a second terminal and a split, Ctrl+P still opens Quick Open from a terminal, `exit` ends it; the budgets of §14 "Terminal output", "Terminal frame" and "Terminal typing" measured with real input in a release build (headless, and with the GPU in the manager's desktop pass), with `foot` (and Ghostty if installed) run on the same 100 MB file in the same headless compositor for comparison, all recorded in `docs/PERFORMANCE.md`; a budget missed because of Glutter gets its `docs/GLUTTER-ISSUES.md` entry and is reported to the manager.
**Proof:** method C with real input: a terminal with a colored listing and a split (`17d.2-terminal.png`).

### 17d.3 Find, links and the other commands
**Goal:** the rest of VS Code's terminal commands of SPEC §10.25 ("Find and links", the commands that 17d.2 left).
**Read:** SPEC §10.25, §9.8; `reference/vscode/src/vs/workbench/contrib/terminalContrib/find/`, `.../links/` (the web, file and word link detectors).
**Tasks:** find in the terminal over the scrollback (case, whole word, regular expression, highlights, next and previous); links (web, files with `:line:col` under the folder opened in an editor, OSC 8), Ctrl+click and the hover; `runSelectedText`, `runActiveFile`, `sendSequence`, `rename`, `selectDefaultShell`.
**Tests:** the link detectors (ported cases), find over wrapped lines and the scrollback, each command; real input: Ctrl+F finds a word in the output, Ctrl+click on `main.go:3:1` printed by the shell opens the file at that line, "Run Selected Text" from an editor.

### 17d.4 The terminal's events and requests
**Goal:** other extensions can watch a terminal's output and write to it (SPEC §10.25 "Events and requests", D27). The first plan had a client of a hosted workspace watch a host's terminals; that extension was removed in step 23.1 (D32) and what only it used went with it.
**Read:** SPEC §10.25, §10.19, §10.15 (events and requests).
**Tasks:** the events and requests of §10.25 in `extensions/terminal` (batched output with `seq`, the snapshot through serialization, `create` with a confined folder, `write`, `run`, `kill`); the README (the terminal); SPEC §10.25 "As built".
**Tests:** the topics and requests (fake API, table-driven, every tag's route); the snapshot plus events give the terminal's cells (seeded random output, with a gap).
**Proof:** none that stays (the proof image of this step showed the removed extension and was deleted in step 23.1).

### 17d.5 Shell integration
**Goal:** the shell tells the terminal where each command is, as in VS Code (SPEC §10.25 "Shell integration").
**Read:** SPEC §10.25; `reference/vscode/src/vs/workbench/contrib/terminal/common/scripts/` (the scripts, MIT), `reference/vscode/src/vs/platform/terminal/node/terminalEnvironment.ts` (`getShellIntegrationInjection`), `reference/vscode/src/vs/platform/terminal/common/capabilities/` (the command detection, the cwd detection), VS Code's documentation of shell integration and its sequences (https://code.visualstudio.com/docs/terminal/shell-integration; nothing of xterm.js or of the code VS Code builds on it), `.../terminalContrib/stickyScroll/`, `.../terminalContrib/quickFix/` (the built-in fixes), `.../terminalContrib/history/`.
**Tasks:** the scripts bundled through `scripts/update-assets.sh` (their license in `third_party/` and `NOTICE`); their injection into bash, zsh, fish and pwsh as VS Code does; OSC 633, 133 and 1337 read by the emulator into commands (prompt, command line, output, exit code) and the working folder, kept as markers that move with the scrollback and survive reflow; the decorations and their menu; the navigation and selection commands; sticky scroll; Run Recent Command and Go to Recent Directory with the history in the extension's state; the inherited folder for splits and new terminals; the quick fixes. The settings, commands and keys with VS Code's ids and defaults.
**Tests:** the injection (arguments and environment per shell, table-driven, ported from VS Code's `terminalEnvironment.test.ts`); the command detection with recorded output of each script (ported cases of the capability tests where they need no TypeScript harness); markers across scrollback trimming and reflow; each quick fix's matcher (ported cases); the commands; **real input** with the test's own `bash` (skipped when bash is missing) in an isolated `HOME`: `true`, `false` and Enter give a blue and a red decoration, Ctrl+Up scrolls to the first command, Ctrl+Alt+R runs `echo again` from the history, a long output keeps its command at the top (sticky scroll), `cd /tmp` and a split starts in `/tmp`.
**Proof:** method C with real input: a terminal with decorations of a passed and a failed command and sticky scroll (`17d.5-shell-integration.png`).

### 17d.6 Terminal IntelliSense
**Goal:** completions while a command line is typed, as VS Code's terminal suggest (SPEC §10.25 "Terminal IntelliSense").
**Read:** SPEC §10.25, §9.12 (the suggest widget); `reference/vscode/src/vs/workbench/contrib/terminalContrib/suggest/` (the widget in the terminal, the cursor tracking of the command line, `terminalSuggestConfiguration.ts`), `reference/vscode/extensions/terminal-suggest/` (the providers: commands in `PATH`, built-ins, files and folders, the completion specs).
**Tasks:** the prompt input model (what the user typed since the prompt, from shell integration, and where the cursor is in it); the providers (commands, built-ins per shell, files and folders, history, the specs converted to JSON by `scripts/update-assets.sh`, their license in `third_party/`); the suggest widget of the editor reused in the terminal; accepting writes the rest of the word to the shell; the settings and the trigger command.
**Tests:** the input model with recorded output (typing, Backspace, cursor keys, a wrapped line); each provider (table-driven, with a temporary `PATH` and folder); the conversion of the specs (a sample of specs round-trips); **real input** with bash: typing `gi` offers `git`, Tab completes it; `git che` offers `checkout`; `cat ` lists the files of the folder; Escape closes the list.
**Proof:** method C with real input: the suggest list over `git che` (`17d.6-suggest.png`).

### 17d.7 Terminals in the editor area, the tabs list and persistent sessions
**Goal:** the rest of VS Code's terminal (SPEC §10.25 "Terminals in the editor area", "The rest of the tabs list", "Persistent sessions").
**Read:** SPEC §10.25, §10.1 (editor tabs and drag and drop), §12 (the saved state); `reference/vscode/src/vs/workbench/contrib/terminal/browser/terminalEditor*.ts`, `terminalTabsList.ts`, `terminalIcons.ts`, `.../platform/terminal/common/terminalProcess.ts` and `terminalRecorder.ts` (what is saved), `.../terminal/browser/terminalProcessManager.ts` (the revive).
**Tasks:** the terminal editor (an editor input of the extension, its tab title and icon, moving between panel and editor and back with the same process and content, drag from the tabs list to the editor area, `defaultLocation`); icons and colors; drag to reorder; join, unsplit, kill all, new with a profile or a folder, size to content width; the tab's hover and the process name title; persistent sessions (save at exit with the serializer, restore at start before the first frame of the panel, the restored content and the new shell, the setting to turn it off).
**Tests:** moving a terminal keeps its process and content (the same pid, the same cells); the saved state and its restore (a goldens pair: before exit and after start); every command; **real input**: drag a terminal from the tabs list onto the editor area, type in it, move it back to the panel; quit Emetos with two terminals (one split) and start it again: both come back with their content and a working shell in the same folder; Change Color makes the tab red.
**Proof:** method C with real input: a terminal in an editor tab beside a file, and a restored terminal in the panel (`17d.7-terminal-editor.png`).

---

## M17e: What the user asked for on 2026-10-28

The user, on 2026-10-28, while M17d was being built: "Some steps for the future: remove extensions for Notes and TODO; add extension to find string in files like the search button in vscode; Right click on a method or variable should have two button "Find all references" and "Find all implementations"; remote work and ... are not clear on how one workspace can become host and how to disable it", and "also remove ScratchPad" (SPEC §10.26 to §10.29, D28 to D31). The milestone comes after M17d and before M18, and each step tests with real input (17b.1), as M17b did.

### 17e.1 Remove Notes, TODOs and Scratchpad
**Goal:** Emetos has none of the three extensions, and a user who had notes or Scratchpad text finds it in files (SPEC §10.26, D28).
**Read:** SPEC §10.26, §10.15 (the As built parts of 11.5 to 11.7, how a window restores tabs, views and window tabs), §10.16 and the 17c.1 entry of `docs/IMPLEMENTED.md` (the same kind of removal); `git grep -n -i -e notes -e todo -e scratchpad` (every hit about these extensions is in scope; TODO comments in code are not).
**Tasks:**
- Delete `extensions/notes`, `extensions/todo` and `extensions/scratchpad` and their lines in `extensions/all.go`, and their parts of `scripts/` (`scripts/screenshots.sh`, proof fixtures used only by them).
- Remove or retarget their uses elsewhere. Every test that used one of them as an example of the contract moves to `extensions/internal/testext` or to another extension, with no coverage lost. This covers:
  - the display tests of 17b.5 and 17b.6 (the Notes fast keys, the Notes welcome of D10);
  - `internal/core/command/clientpolicy_test.go`;
  - the window tab's tests;
  - goldens.
- Keep the user's text, as SPEC §10.26 "What the user wrote is kept" says:
  - the export to `$XDG_DATA_HOME/emetos/saved-notes/`, once, with its marker;
  - the notification with **Open Folder**.
  - The code reads the saved formats of the removed extensions (documented in §10.15's As built parts and the old code in git) and lives in `internal/gui/app`, with a comment that names D28.
- A restored window tab whose kind no extension declares is dropped, as the editor tabs of 17c.1 are, with one info log line.
- Update the docs:
  - README (features, screenshots: `todos-tasks.png` becomes a Tasks screenshot or goes);
  - SPEC: §3's package list, §10.26 As built, the milestone table;
  - the coverage rows of `docs/IMPLEMENTED.md`, marked removed;
  - `docs/MANUAL-CHECKS.md`;
  - `docs/DESKTOP-PASS.md` (D10 now notes the removal).
**Tests:**
- The export, table-driven over an isolated `XDG_*`:
  - notes with and without titles, two notes with one title, a Scratchpad with and without text;
  - nothing to save (nothing written, no notification);
  - a second start (nothing written again);
  - an unreadable state (one warning log and the window starts);
  - each error Tag's route.
- The window tab drop.
- **Real input:** a session saved with a note tab, a TODOs view and a Scratchpad tab, started again, gives the notification. Open Folder opens the folder with the files, and no dialog shows.
- `git grep` shows no use left.

### 17e.2 Search across files
**Goal:** VS Code's Search view: find and replace a string in the files of the workspace (SPEC §10.27, D29).
**Read:**
- SPEC §10.27, §14 "Search", §10.15, §9.11 (the diff editor), §10.5 (the Explorer's context menu);
- `reference/vscode/src/vs/workbench/contrib/search/` (`search.contribution.ts`, `searchView.ts`, `searchWidget.ts`, `searchActions*.ts`, `searchIcons.ts`, `replace*.ts`, `searchMessage.ts`);
- `reference/vscode/src/vs/workbench/services/search/` (the query, the ignore files, the excludes, `textSearchManager.ts`, the ripgrep arguments for what is matched);
- `internal/core/regex`, `internal/core/glob`, the file index of `internal/core/workspace`.
**Tasks:**
- `internal/core/search`: the engine of §10.27, with no UI.
- `extension/`: `api.Workspace().FindTextInFiles`, and the replace path, if the API has none that fits.
- `extensions/search`: the view, the commands, keys, context keys, menus, settings and colors of §10.27, with VS Code's ids and defaults, and Find in Folder... in the Explorer.
- README: a Search section.
- SPEC: §10.27 As built, and the §14 row measured.
**Tests:**
- The engine, table-driven:
  - literal, case, whole word, smart case, regular expressions (one line and several lines);
  - include and exclude globs;
  - `files.exclude` and `search.exclude`;
  - `.gitignore` cases (nested, negation, directories, anchored), ported where VS Code's search tests have them;
  - binary files, encodings, BOM, symlinks with a loop;
  - open editors with unsaved text, the limit, and cancellation (no goroutine left).
- Replace: preserve case (VS Code's cases), regex groups, one undo step per file, the confirmation.
- Goldens of the view (dark and light): the results as a list and as a tree, the replace box, the messages.
- **Speed:** a test holds the first results within 100 ms and the UI thread under 16 ms per frame while 20,000 generated files are searched. The time beside GNU `grep -rF` goes in `docs/PERFORMANCE.md`.
- **Real input:**
  - Ctrl+Shift+F, type a word: results show while typing;
  - F4 opens the next match;
  - Alt+C narrows the results;
  - Ctrl+Shift+H with a replacement, then Replace All and confirm: the files change, and Ctrl+Z in an open file undoes its part;
  - Find in Folder... from the Explorer.
**Proof:** method C with real input: the Search view with results and the replace box (`17e.2-search.png`).

### 17e.3 Find All References and Find All Implementations
**Goal:** a right click on a method or a variable offers Find All References and Find All Implementations, and their results fill a References view, as VS Code's references-view does (SPEC §10.28, D30).
**Read:** SPEC §10.28, §9.12 (the language client, the peek of references), §10.15; `reference/vscode/extensions/references-view/` (`package.json`, `src/references/`, `src/tree.ts`, `src/navigation.ts`, `src/highlights.ts`, `src/utils.ts`; MIT).
**Tasks:**
- `extension/`: `api.Languages().References` and `Implementations` (positions in, locations out, with the `ctx`).
- `extensions/references`: the commands, the context menu entries, the keys, the view, the history and the item actions of §10.28, with VS Code's ids.
- README: a line in the language features.
- SPEC: §10.28 As built.
**Tests:**
- The commands with a fake language server: results by file, the counts, the message, no result.
- The context menu shows the entries only with the provider, in VS Code's order.
- The history; F4 and Shift+F4; Dismiss and Copy; goldens of the view (dark and light).
- **Real input with `gopls`** (build tag `emetos_gopls`, skipped when gopls is missing): in a Go module of the test,
  - right click on a method → Find All References: the view lists the calls in two files, and a click opens one;
  - right click on an interface method → Find All Implementations lists the types that implement it;
  - Shift+Alt+F12 on a variable.
**Proof:** method C with real input: the context menu with the two entries, and the References view (`17e.3-references.png`).

### 17e.4 Hosting a workspace: one place to start and stop
**Goal:** a user can see at a glance whether a workspace is hosted by Remote Work, start hosting it, and stop it, from one status bar item (SPEC §10.29, D31).
**Read:** SPEC §10.29, §10.18 (all, with its As built parts), §10.7 (notifications and banners); `extensions/remotework`, `extensions/internal/pageui`.
**Tasks:**
- **First, find what is unclear.** Use Remote Work as a new user would, with real input, two Emetos processes on this machine and test keys (as the tests of 15.x). Take screenshots of each step of hosting and stopping. Write in `docs/DESKTOP-PASS.md` (new findings H1, H2...) what a user cannot find or understand.
- **Then build §10.29:**
  - the Sharing status item and its quick pick;
  - the commands;
  - the first-time guide with the addresses and the fingerprint;
  - the "Host this workspace" switch and its plain line;
  - Stop Hosting;
  - the banner for Remote Work;
  - "The host stopped sharing this workspace." on the client;
  - the README's "How to host a workspace" and "How to stop".
  - Fix every finding H*, or explain why it is not a bug.
**Tests:**
- The status item's states and tooltip, and the quick pick's entries for each state, table-driven: off; Remote Work on; with connected computers.
- Host and Stop, with the listener's state checked.
- The first-time guide (no key, a key, on).
- Goldens: the status item, the pick, the guide, the page, the tab's section, the banner, dark and light.
- **Real input:** from a fresh state,
  1. click the status item and choose Host with Remote Work, follow the guide (paste a test key), and turn it on;
  2. a second Emetos connects with that key;
  3. the item says "Hosting · 1";
  4. Stop Hosting from the item ends the client's session with the message.
**Proof:** method C with real input: the status item's menu while hosted, and the first-time guide (`17e.4-hosting.png`).

---

## M18: Containers (Docker and Podman)

**On hold until M17b, M17c and M17d are done** (the user, 2026-10-27: "postpone the extensions I requested. Prioritize the bugs." and "When you finish with the above requirements, then work on the extensions."; the removal of the Agents extension and the terminal were asked for the same day). M18 to M22 follow in order after them.

Requested by the user on 2026-10-27, with access to their Docker and Podman for tests behind the build tags `emetos_docker` and `emetos_podman` (CLAUDE.md "Testing notes"). VS Code's Container Tools (`microsoft/vscode-containers` v2.5.3, MIT, pinned in `scripts/reference-versions.txt`) may be ported: its texts, command ids, templates and design, each ported file saying so.

### 18.1 The engine client and the container test harness
**Goal:** one client for the Docker and Podman APIs, and safe tests against the real engines (SPEC §10.20 "The engine client", "Which engine").
**Read:** SPEC §10.20, D22; CLAUDE.md "Testing notes"; the Docker Engine API reference (v1.41+) and Podman's libpod API reference (v4+), online.
**Tasks:** `scripts/fetch-reference.sh` (the new pin) and `NOTICE`/`third_party/` for vscode-containers; `extensions/internal/containerapi`: the transport (HTTP over a Unix socket; `tcp://` with TLS), version negotiation, containers, images, networks, volumes, pods (libpod), events, logs (the multiplexed stream), exec, `system df`, errors with the engine's message, bounded bodies, every call under a `ctx`; finding the engine (settings, `DOCKER_HOST`/`CONTAINER_HOST`, Docker contexts, the default sockets, and for Podman its own `podman system service` on a private socket when none answers, stopped with Emetos); `containerapitest`: a fake engine server (both APIs) for the default tests; the real-engine harness (pinned images in one place, `emetos-test-*` names, the label `dev.emetos.test=1`, `127.0.0.1` ports, cleanup in `t.Cleanup`) and `scripts/container-test-cleanup.sh` (labelled things only).
**Tests:** every call against the fake (table-driven, with the engine's errors and route tests); the log stream demultiplexer (seeded random frames); events with cancel; the engine search order (isolated `HOME`, `XDG_*` and environment); the Podman service started and stopped (a fake `podman` on `PATH`); tagged `emetos_docker` and `emetos_podman`: run a labelled container of a pinned small image, logs, exec, inspect, events, stop, remove, against the real engines, and nothing labelled left afterwards.

### 18.2 The Docker extension: the explorer and its actions
**Goal:** see and manage Docker's containers, images, networks, volumes and contexts (SPEC §10.20 "The side bar view", "Container actions", "Image actions", "Safety").
**Read:** SPEC §10.20, §10.15; vscode-containers' tree views and commands.
**Tasks:** `extensions/internal/containerui` (shared with Podman): the side bar view and its sections (grouped containers, images by repository, networks, volumes), the filter, refresh from the event stream; `extensions/containers` (`emetos.containers`): the Contexts section and picker, container actions (start, stop, restart, pause, kill, remove, View Logs in a panel tab, Inspect, Open in Browser, Copy ID, Run Command, Explore Files), image actions (Run with its form, Pull, Tag, Remove, Inspect, History), networks and volumes (create, remove, inspect, prune), confirmations with counts and sizes, the missing-engine view, the events of §10.20, the first-run note about the Docker socket.
**Tests:** the views and actions against the fake engine in `gluttertest` (goldens dark and light: the explorer with every section, the logs tab, a confirmation, the missing engine); tagged `emetos_docker`: the explorer shows a labelled test container, start/stop/remove through the view, logs follow.
**Proof:** method B with `emetos_dev` pointing the extension at a fake engine (or at the real Docker with labelled test containers only): the explorer with containers grouped by Compose project, images and a logs tab.

### 18.3 Docker: build, Compose, run, push, scaffolding and language servers
**Goal:** the workflows around files (SPEC §10.20 "Image actions" Build, "Compose", "Scaffolding", "Language support", Open Shell).
**Read:** SPEC §10.20; vscode-containers' build, compose and scaffolding code and templates.
**Tasks:** Build Image (the CLI, streamed into a panel tab, errors into Problems); Compose Up, Up selected services, Down, Restart, Logs (explorer and editor context menus, the Compose group); Push and Run Interactive through the CLI; Open Shell in the user's terminal emulator; "Add Docker Files to Workspace" with the ported templates; the Dockerfile and Compose language servers declared when found on `PATH`.
**Tests:** command lines built for build, compose, push and shell (table-driven, a fake `docker` on `PATH`); build output parsing into Problems; the scaffolding (each template, no overwrite without confirmation); the language server declaration; tagged `emetos_docker`: build a tiny Dockerfile into a labelled image and remove it, `compose up`/`down` of a labelled one-service project.
**Proof:** method B: a build streamed into its panel tab, and the Compose group in the explorer.

### 18.4 The Podman extension
**Goal:** the same for Podman, plus pods, Kube and Quadlet (SPEC §10.20 "Podman only").
**Read:** SPEC §10.20, D22; Podman's libpod API and `podman kube`, `podman generate` and Quadlet documentation.
**Tasks:** `extensions/podman` (`emetos.podman`) on `containerapi` and `containerui`: the libpod API, its own service when no socket answers, the Pods section (containers grouped under their pod; create from selected containers, start, stop, restart, remove, inspect), Connections (rootless and rootful), Generate Kube YAML, Play Kube and Kube Down, Generate Quadlet (an untitled `.container` unit), build, push, Compose (`podman compose` or `podman-compose`) and Open Shell through the `podman` CLI; its own activity bar icon and events.
**Tests:** the libpod calls and the pod views against the fake engine; the Quadlet generator (table-driven from inspect JSON); the service start with a fake `podman`; goldens of the Podman view (dark and light); tagged `emetos_podman`: a labelled pod with a container, kube generate and play down, nothing labelled left.
**Proof:** method B: the Podman view with a pod and its containers, and a generated Kube YAML.

---

## M19: SQL databases

### 19.1 Connections, the keyring and the results grid
**Goal:** what the three database extensions share (SPEC §10.21 "Connections", §10.24, D24).
**Read:** SPEC §10.21–§10.24, D24; the Secret Service API (freedesktop.org), online.
**Tasks:** `go get github.com/godbus/dbus/v5` as a direct dependency (already brought in by Glutter); `extensions/internal/dbconn`: the connection model and `db-connections.json` (0600, user or workspace scope), the connection form (engine-specific fields given by each extension), passwords in the Secret Service keyring (store, look up, delete; locked keyrings unlocked through the service's prompt), in memory otherwise, `${env:…}`; the "Databases" side bar view whose roots the database extensions add, with connection states; `extensions/internal/resultgrid`: the virtualized grid (columns, resize, client sort, selection, copy as text and TSV, NULL, cut long values, hex binary), paging, export to CSV and JSON.
**Tests:** the connection file (modes, read policy, no password ever written); the keyring against a fake Secret Service on a private D-Bus connection (or an interface fake), and a manual check with the real keyring (`docs/MANUAL-CHECKS.md`); the form; the grid (goldens dark and light, 100,000 rows built lazily, copy and export formats).

### 19.2 SQL: drivers, dialects and the schema explorer
**Goal:** connect to the four engines and browse their schema (SPEC §10.21 "Drivers", "The explorer").
**Read:** SPEC §10.21, D23.
**Tasks:** `go get` the four drivers; `extensions/sql` (`emetos.sql`) with `extensions/sql/dialect` for PostgreSQL, MySQL/MariaDB, SQLite and SQL Server (catalog queries, quoting, cancel, read-only session); connect and disconnect with TLS and read-only; the explorer tree (databases, schemas, tables, views, functions and procedures, columns with keys, indexes), lazy and cached, the filter, Refresh; the context menus (New Query, Select Top 1000, Copy Name, Generate statements, Drop as text).
**Tests:** each dialect's catalog queries and generated statements (table-driven); SQLite end to end in the default tests (it is a file); the explorer in `gluttertest` (goldens); tagged `emetos_docker`: PostgreSQL, MySQL, MariaDB and SQL Server in labelled containers of pinned images, each with a fixture schema, connected and browsed.

### 19.3 SQL query editors and the results panel
**Goal:** run SQL and see the results (SPEC §10.21 "Query editors", "The results panel", "Safety").
**Read:** SPEC §10.21, §9.12 (completion providers).
**Tasks:** `.sql` editors bound to a connection and database (the status bar item and picker; bindings of workspace files kept in the workspace state); Run (selection or the statement under the cursor, split by each dialect's rules), Run All, Cancel; autocommit with Begin, Commit and Rollback and the open transaction shown; the results panel on `resultgrid` (result sets as tabs, row limit and Fetch More, rows affected, messages, errors with their position in the editor, history of 10 runs per editor, export); read-only enforcement and the destructive-statement confirmation; completion of keywords, schemas, tables and columns; the events of §10.21.
**Tests:** statement splitting for every dialect (strings, comments, dollar quotes, `DELIMITER`, `GO`; seeded random inputs); classification of reads and destructive statements; cancel; transactions; the panel (goldens dark and light: a result grid, rows affected, an error); end to end on SQLite in the default tests; tagged `emetos_docker`: the same on PostgreSQL, MySQL and MariaDB, including cancel of a long statement. **SQL Server is never started in a container** (its image needs `ACCEPT_EULA=Y`, which would accept Microsoft's licence terms on the user's behalf, and the user accepts no third-party terms): its splitting (`GO`), its classification and its execution are tested against fakes only (decision 320).
**Proof:** method B: the explorer of a SQLite fixture database, a query editor and its results in the panel.

---

## M20: MongoDB

### 20.1 Connections, the explorer and the playground language
**Goal:** connect to MongoDB, browse it, and parse playgrounds (SPEC §10.22 "Connections", "The explorer", "Playgrounds").
**Read:** SPEC §10.22, D23.
**Tasks:** `go get go.mongodb.org/mongo-driver/v2`; `extensions/mongodb` (`emetos.mongodb`): connections on `dbconn` (connection strings, the password moved to the keyring), the explorer (databases, collections with counts, indexes, the sampled schema), context menus; the `.mongodb` language (a TextMate grammar) and its parser in `extensions/mongodb/playground`: statements and relaxed JavaScript literals with the shell helpers, everything else refused with a position.
**Tests:** the parser (table-driven: every statement form and literal, errors with positions; seeded random inputs and a fuzz target run as CLAUDE.md says); the explorer against a fake (an interface over the driver); goldens of the explorer; tagged `emetos_docker`: a labelled `mongo` container of a pinned image with fixture data, connected and browsed.

### 20.2 Running playgrounds, results and editing documents
**Goal:** run playgrounds and see and edit documents (SPEC §10.22 "Playgrounds" Run, "Results", "Editing a document", "Safety").
**Read:** SPEC §10.22.
**Tasks:** Run and Run All with the connection and database in the status bar; the statements executed with the driver (find with its chain, aggregate, writes, commands, `show`), cursors fetched in pages; results in the shared panel (Tree, JSON and Table views, write results, errors with codes); a document opened as Extended JSON and saved with `replaceOne` after a confirmation of the changed fields; Delete Document; the destructive confirmations; read-only; the events of §10.22.
**Tests:** the translation of parsed statements to driver calls (table-driven against a fake); paging; the confirmations; the panel views (goldens dark and light); tagged `emetos_docker`: playground statements against the real server, editing and deleting a document.
**Proof:** method B: the MongoDB explorer, a playground and its results in Tree view.

---

## M21: Redis

### 21.1 Connections, the explorer and the key editor
**Goal:** browse keys and view and edit values (SPEC §10.23 "Connections", "The explorer", "The key editor").
**Read:** SPEC §10.23, D23.
**Tasks:** `go get github.com/redis/go-redis/v9`; `extensions/redis` (`emetos.redis`): connections on `dbconn` (URLs, ACL, TLS, database, separator, Sentinel and Cluster, read-only); the explorer (databases with key counts, the key tree by separator from `SCAN` in pages, the pattern filter, TTL); the key editor for strings, hashes, lists, sets, sorted sets, streams and ReJSON values, paged with the `*SCAN` commands and `LRANGE`/`XRANGE`, big strings cut; key actions (rename, TTL, persist, delete, new key).
**Tests:** the key tree from scanned keys (table-driven, separators, very many keys built lazily); each value type's editor against a fake (an interface over the client); goldens dark and light; tagged `emetos_docker`: a labelled `redis` container of a pinned image with fixture keys of every type, browsed and edited; Cluster scanning against a labelled small cluster if practical, otherwise a fake.

### 21.2 The console, Pub/Sub, Monitor and server info
**Goal:** run commands and watch the server (SPEC §10.23 "The console", "Safety").
**Read:** SPEC §10.23.
**Tasks:** the console panel tab and `.redis` files (a command line with history, `redis-cli` quoting, completion from `COMMAND DOCS`, replies formatted by type); Pub/Sub (Subscribe, PSubscribe, Publish) and Monitor tabs, bounded, with pause and clear; the server info tab (`INFO`, `SLOWLOG`, `CLIENT LIST`, `MEMORY STATS`); the dangerous-command confirmations and the read-only refusal (from `COMMAND INFO` flags); the events of §10.23.
**Tests:** the quoting parser (table-driven, seeded random inputs); reply formatting for RESP2 and RESP3 types; the confirmations and read-only refusal; the console and Pub/Sub tabs (goldens dark and light); tagged `emetos_docker`: commands, Pub/Sub and Monitor against the labelled server.
**Proof:** method B: the Redis explorer with a hash open in the key editor, and the console with replies.

---

## M22: Wrap-up of M18–M21

### 22.1 A container's database, and the refusal to clients
**Goal:** connect from a container to its database, and keep clients of a hosted workspace confined (SPEC §10.24, D25).
**Read:** SPEC §10.24, §10.19, D25.
**Tasks:** Connect to Database on containers of known database images in both container extensions (`emetos.containers.connectDatabase`, prefilled from the published port and the image's environment, never a password); each database extension opening its form from that event; the class `outside` in `command.ClientRefusal` with `containers.*`, `podman.*`, `sql.*`, `mongodb.*` and `redis.*`, and its marking in `emetos.commands.list`.
**Tests:** the prefill for every known image (table-driven from inspect JSON); the event to form flow between two extensions; the refusal class (every new command id refused to a client, the existing scan of risky commands extended); tagged `emetos_docker`: a labelled `postgres` container connected through Connect to Database.

### 22.2 Update the README
**Goal:** `README.md` describes the six new extensions.
**Read:** `README.md`, `docs/IMPLEMENTED.md` (the entries of M18–M22), SPEC §10.20–§10.24, §12.
**Tasks:** the status (M18–M22 built), the features and the bundled list (Containers, Podman, SQL, MongoDB, Redis), the requirements (Docker or Podman; nothing to install for the databases), the security notes (the Docker socket is power over the computer; passwords in the keyring), where things are (`db-connections.json`), privacy (the engines and databases the user configured); fresh screenshots with `scripts/screenshots.sh` (the Docker explorer with logs, a SQL query with results, a MongoDB playground, the Redis key editor) from fixtures, with fake engines or labelled test containers only; the coverage table rows; every link resolves (`TestREADMELinksResolve`).
**Tests:** `TestREADMELinksResolve`.
**Proof:** the README's screenshots, viewed.

---

## M23: Remove an extension the user no longer wanted

The user, on 2026-10-29, asked to remove the extension of M16 that controlled a workspace from another machine through its events (the one built by steps 16.2 and 16.3 and extended by 17d.4 and 17e.4), and then to remove every mention of it from every document, because it would scare users off (SPEC D32). After this milestone the repository does not name it anywhere; what other extensions use stays, described in neutral words. The check is `scripts/check-removed-name.sh` (it builds the word from its rot13 form, so the script does not hold it either).

### 23.1 Remove the event-control extension and every mention of it
**Goal:** Emetos has no event-control extension, nothing that hosts or joins such a session, and a user whose last session had its tabs starts cleanly; `scripts/check-removed-name.sh` prints nothing and exits 0 (SPEC D32). Remote Work, the terminal, the Sharing status item and the extensions that use the core request topics work as before.
**Read:** SPEC §10.19 (the core request topics, privileges and confinement), §10.15 (how a window restores tabs and views of extensions), §10.18 (Remote Work, which keeps the login, the keys, the QR codes and the Sharing item), §10.24 and D25 (the class `outside` of `command.ClientRefusal`), §10.25 (the terminal's events and requests); step 17c.1 (how the Agents extension was removed); `scripts/check-removed-name.sh` (every hit is in scope).
**Tasks:**
- Delete the extension's folder under `extensions/` (with its `client`, `client/clienttest`, `protocol` and `testdata`) and its line in `extensions/all.go`; its manifest, settings, commands, keybindings, menu items, views, panel and editor tabs, dev commands, state-file code and tests; its id constant in `extension/` and its entry in the privileges table of `internal/gui/exthost/manifest.go` (the privileges mechanism stays: the terminal has `commands.execute`); its scripts and their fixtures (`scripts/proof-*` for it, `testdata/proof/16.2`, `16.3` and `17d.4`, `testdata/screenshots/` for it) and its shot in `scripts/screenshots.sh` with its image in `docs/screenshots/`; its proof images in `docs/implemented-proof-images/` (every file whose name or picture is about it, and the proof image that shows the Sharing menu with its entries, made again).
- **Keep** what other extensions use, with its tests, renamed where a name or comment names the extension: the core request topics of step 16.1, `Manifest.Privileges` (`commands.execute`), `command.ClientRefusal` and its classes ("clients" are the clients of a hosted workspace), `extensions/internal/sshlogin`, `keyqr`, `keyform`, `loginui`, `sharing`, `statefile`, `pageui`, and the terminal's events and requests. Remove only what the extension alone used and nothing else needs, and say what in the report. Error Tags that named it are renamed (`CoreCommandRefused`, `CoreDocumentVersionStale`) and their routes tested again.
- **The Sharing status item** (`extensions/hosting`) shows and starts Remote Work only.
- **Old state:** a restored window whose state has the removed extension's tabs, views or panels drops them without an error dialog (one line at info level); its state files and keys on disk are left where they are (never delete the user's data). A test restores such a state; its ids are built from the rot13 form of the name at run time so that the repository stays free of it.
- **Every document, with no mention left:** `README.md` (features, requirements, security, privacy, screenshots, where things are), `CLAUDE.md`, `NOTICE`, `docs/SPEC.md` (§10.19 is the short section on the core request topics, privileges and confinement; D21 is withdrawn and D32 records this decision), `docs/IMPLEMENTATION.md` (M16 keeps 16.1 only), `docs/IMPLEMENTED.md`, `docs/DECISIONS-TO-REVIEW.md`, `docs/DESKTOP-PASS.md`, and anything else the script finds.
- Git history (commit messages, tags) is not rewritten by this step.
**Tests:** the restore test above; the Remote Work, terminal, Sharing item and client refusal tests pass (their cases about the removed extension are gone); `scripts/check-removed-name.sh` prints nothing and exits 0, and the step's entry says so; `TestREADMELinksResolve`; the standard checks.

## M24: Git and Source Control

Requested by the user on 2026-10-29: "I want a git extension like vscode", the full Git of VS Code, and no GitHub extension (D33; SPEC §10.30, D33–D37). It is VS Code's built-in `git` extension (MIT, `reference/vscode/extensions/git/`) on the Source Control parts of the workbench (`reference/vscode/src/vs/workbench/contrib/scm/`, `contrib/timeline/`, `contrib/multiDiffEditor/`, `services/decorations/`, `api/common/extHostSCM.ts`). Code ported from them says so with a `// Ported from VS Code: <path>` line. The extension is `extensions/gitscm` (`emetos.gitscm`, "Git"); `extensions/git` stays the language pack. No new Go module: Emetos runs the user's `git` program. Every step that needs a capability the extension contract lacks adds it to `extension/`, `internal/gui/exthost` and `extensions/internal/fakeapi` together and names it in its `IMPLEMENTED.md` entry.

**Tests of the milestone.** The Git extension is tested with the real `git` in isolated temporary repositories, by default and with no build tag (D36); `extensions/gitscm/gittest` (step 24.1) sets the environment so that nothing of the user's `~/.gitconfig`, credentials, SSH keys or repositories is touched (SPEC §10.30 "Tests"). A test that needs `git` calls `gittest.Require(t)`, which skips it when `git` is missing or older than 2.30. Real remotes are local bare repositories; there is no network and no `sshd`. **The user approved the CLAUDE.md change of step 24.1 (2026-10-30).** Manual checks against the user's own remotes, keys and credentials go to `docs/MANUAL-CHECKS.md` (CLAUDE.md "Working through the steps"). Proofs use the fixtures that `scripts/git-fixture.sh` makes (step 24.1), and, from step 24.5, set the dev knob `EMETOS_DEV_GIT_TRUST=<fixture dir>` in the environment of `scripts/proof.sh` (debug and `emetos_dev` builds only, SPEC §15.2) so that the trust question does not cover the image (24.5's own proof shows it): authors `Ada Example <ada@example.com>`, fixed dates, no real names, paths or addresses (RFC 5737 for any IP).

**Order and parallel work.** 24.1 and 24.2 touch different files and run in parallel; everything else runs in order on the shared files except where noted: 24.3 and 24.4 after 24.2 (the view, then its input); 24.5 after 24.1 and 24.3 (it also builds the trust question); 24.6 after 24.5 (its host half, the decorations service and the Explorer, may start with 24.3); 24.7 to 24.10 in order; 24.11 and 24.12 after 24.10; 24.13 and 24.14 after 24.10 (14 after 13); 24.15 after 24.9 and 24.13; 24.16 after 24.15; 24.17 (the multi-file diff editor) may start after 24.7 in parallel with 24.8–24.16; 24.18 (stash) after 24.17; 24.19 (worktrees) after 24.13 and 24.18; 24.20 (the Graph) and 24.21 (the Timeline) after 24.7, the Graph also after 24.17 (the Timeline may run in parallel with the Graph); 24.22 (blame) after 24.7; 24.23, then 24.25 (extensions and the terminal in the Remote Work view, added on 2026-11-01 at the user's request), then 24.24 last.

### 24.1 The Git client and the test harness
**Goal:** run `git` safely and portably, parse what it prints, and test it against the real program in isolated repositories (SPEC §10.30 "Running git", "Untrusted repositories", "Tests", D35–D37).
**Read:** SPEC §10.30, D35–D37, §12, §13, §15.1; CLAUDE.md "Testing notes" and "Don't"; `reference/vscode/extensions/git/src/git.ts`, `util.ts`, `operation.ts`, `cache.ts`, `test/git.test.ts`; Git's documentation of `status --porcelain`, `for-each-ref`, `blame --incremental`, `rev-parse`, `config` and the environment variables (`git help git`), online.
**Tasks:**
1. `extensions/gitscm/git` (plain Go, standard library and `rm4n0s/errors`, no UI): `Find` (the `git.path` candidates, then `PATH`; `git --version` parsed; the minimum 2.30; the tags `GitNotFound` and `GitTooOld` with the path and the version); `Runner.Run(ctx, Command)` with the environment of the SPEC (`GIT_TERMINAL_PROMPT=0`, `GIT_PAGER=cat`, `LC_MESSAGES=C`, `LANGUAGE=en`, `LC_ALL` removed, `EMETOS_GIT_COMMAND`, the askpass variables given by the caller) and the **background** flags and variables (`GIT_OPTIONAL_LOCKS=0`, `-c core.fsmonitor=false`, `-c core.pager=cat`, `--no-ext-diff`, `--no-textconv`), arguments as a list, paths after `--` and past 200 of them through `--pathspec-from-file=- --pathspec-file-nul`, a process group per command, cancellation by SIGTERM to the group and SIGKILL after 3 s, stdout as a stream (`RunStream`) or bounded (64 MiB, `GitOutputTooLarge`), stderr kept (last 2 KiB), no command ever through a shell; the queue of a repository (reads in parallel up to 4, writes one at a time in order, a read behind a write that changes what it reads, a coalesced refresh); the `Logger` hook (the Git Output channel's lines `> git status -z -uall [34ms]`, with the userinfo of URLs replaced by `***`); the classification of failures into the `GitErrorCodes` of the SPEC, each its own Tag created where the failure is seen, with the arguments, the exit status and the last error output as metadata.
2. The parsers, each streaming where the format is a stream: `status -z` (version 1 with `-uall`/`-uno`, renames with their origin, every `XY` code of VS Code's `Status`, ignored with `--ignored`), `log -z` with VS Code's `COMMIT_FORMAT`, `for-each-ref` (branches, remote branches and tags with the upstream, the track `[ahead 1, behind 2]`, the object, the commit date), `rev-parse` (top level, git dir, common dir, superproject, inside work tree, shallow, bare), `config --local --list -z`, `remote -v`, `stash list`, `blame --incremental`, `diff --name-status -z`, `show`/`cat-file` content, `check-ignore -z --stdin`, `ls-files -z`, `merge-base`, `rev-list --count`. The write commands (add, reset, commit, checkout, branch, merge, rebase, stash, fetch, pull, push, tag, remote, clone, init, config) are **added by the steps that use them**, each as a method of `Repo` with its own tests; this step has only `Init` and `Clone` for the harness.
3. `extensions/gitscm/gittest`: `New(t)` and `Require(t)` (the isolation of the SPEC: `HOME`, `XDG_CONFIG_HOME`, `GIT_CONFIG_NOSYSTEM`, `GIT_CONFIG_GLOBAL` and `GIT_CONFIG_SYSTEM` at empty files, `GIT_CEILING_DIRECTORIES`, `GIT_TERMINAL_PROMPT=0`, the identity and fixed dates, `init.defaultBranch=main`, `protocol.file.allow=always`, an empty `core.hooksPath`); `Trust(t, path)` (writes the repository's path into the isolated trust file of SPEC §10.30, so that a test does not meet the trust question unless it is about it; `New(t)` trusts the repositories it makes); `Repo` helpers (`Write`, `Commit`, `Branch`, `Merge`, `ConflictingBranches`, `Tag`, `Stash`, `Submodule`, a bare remote with `Clone`, `Hook`); `FakeGit` (the test binary acting as `git`, chosen with `EMETOS_FAKE_GIT=<script>`: missing, old, slow, failing, huge output, hangs until cancelled); `HTTPBackend` (a Go handler on `net/http/cgi` running `git http-backend` behind Basic authentication, on `127.0.0.1`); `cmd/gitfixture` and `scripts/git-fixture.sh <small|conflict|history|many> <dir>` (deterministic fixtures, all with `src/loader.go` of about 40 lines: `small` also has `src/loader_test.go`, `docs/notes.md` and `README.md` and changes in every group (a staged edit, an unstaged edit at lines 12 and 20 of `src/loader.go`, an untracked file, a deleted file, a renamed file); `conflict` is stopped in the middle of a merge with `src/loader.go` conflicted; `history` has 300 commits on a main branch and two feature branches with merges, tags, a bare `origin` it is cloned from that is 1 commit ahead and 2 behind, and `src/loader.go` changed by several authors; `history` also has a linked worktree `<dir>.worktrees/feature` on a feature branch; `many` has 100,000 changed files for the measurements).
4. CLAUDE.md "Testing notes" (approved by the user, 2026-10-30): add a paragraph that says plainly: the tests of the Git extension use the real `git` program, in temporary repositories created under `t.TempDir()`, with their own `HOME`, `XDG_CONFIG_HOME`, `GIT_CONFIG_GLOBAL`, `GIT_CONFIG_SYSTEM` and `GIT_CONFIG_NOSYSTEM` and a test author identity; they never read or write the user's git configuration, credentials, SSH keys or repositories; they use no network (a push, fetch or clone goes to a local bare repository or to the local test server on `127.0.0.1`); they run in the normal test runs (`go test ./...`, the race and cgo-free runs) with no build tag, calling `gittest.Require(t)` which skips them only when `git` is missing or older than 2.30; and `pgrep -a git` shows nothing left when a task is reported done.
**Tests:** every parser against recorded outputs (table-driven: renames, copies, conflicts in all seven forms, odd names with spaces, quotes, newlines and non-UTF-8 bytes, empty outputs, truncated streams, a stream cut at every byte); the environment of a command (a table of what is set and removed; the user's `LC_ALL` removed, `LC_CTYPE` kept); the background flags only on background commands; the version parsing and comparison (`2.30`, `2.43.0.windows.1`, `2.56.0.rc1`, `2.30.1 (Apple Git-130)`); the classification of every error message of the SPEC's list (a table of git's real messages, captured from a real git in the test where it can be provoked, otherwise fixed text); cancellation kills the whole group (a fake git that starts a child and sleeps); the queue (reads parallel, writes in order, a refresh coalesced, with a fake clock and `-race`); log masking of `https://user:secret@host/x`; the output bound; the pathspec file for 5,000 paths; real `git` for each parser (a repository built by `gittest`, parsed, compared with what the test knows); the harness itself (a test that runs `git config --global -l` and `git config --system -l` and finds nothing; `git rev-parse` in a temp folder inside the project finds no repository; no variable of the user's environment named `GIT_*` leaks); the fixtures are byte-for-byte reproducible (two runs, equal hashes). Route tests for every new Tag.
**Proof:** none.

### 24.2 The Source Control API and its model
**Goal:** the contract an extension uses to describe a source control, and the host's model of it, with no UI yet (SPEC §10.30 "The SCM API", D34).
**Read:** SPEC §10.30, §10.15 (the contract, "Lifetime", "Containment", the context keys and menus), §11.2; `reference/vscode/src/vs/workbench/api/common/extHostSCM.ts`, `reference/vscode/src/vs/workbench/contrib/scm/common/scm.ts`, `scmService.ts`, `contrib/scm/browser/activity.ts`, `menus.ts`, `contrib/scm/test/browser/activity.test.ts`, `reference/vscode/src/vscode-dts/vscode.d.ts` (`scm`, `SourceControl`, `SourceControlResourceGroup`, `SourceControlResourceState`).
**Tasks:** `extension/scm.go`: `SourceControls` (`api.SourceControl()`), `SourceControlSpec`, `SourceControl`, `SCMInput`, `Validation`, `ActionButton`, `StatusCommand`, `ResourceGroup`, `ResourceGroupSpec`, `ResourceState` with its decorations, `Command`; the menu id constants (`MenuSCMTitle`, `MenuSCMRepository`, `MenuSCMSourceControl`, `MenuSCMResourceGroupContext`, `MenuSCMResourceStateContext`, `MenuSCMResourceFolderContext`, `MenuSCMChangeTitle`, `MenuSCMHistoryTitle`, `MenuSCMHistoryItemContext`, `MenuSCMHistoryItemRefContext`, `MenuTimelineItemContext`, `MenuEditorContent`, `MenuEditorTitle`, `MenuDiffEditorGutterHunk`, `MenuDiffEditorGutterSelection`, `MenuCommandPalette` exists) with VS Code's ids as values, and the submenu type (`Contributes.Submenus`, VS Code's `submenus`: Commit, Changes, Pull/Push, Branch, Remote, Stash, Tags); `Contributes.ContextKeys` accepts dotted names (`git.state`); `internal/core/scm` (no Glutter): `Model` of sources, groups and resources with their order, `Sort` by path, name or status, `Tree` with compact folders, counts (`scm.countBadge`, `scm.providerCountBadge`), the active repository and the selection rules (`scm.repositories.selectionMode`), the context key values for the menus (`scmProvider`, `scmProviderContext`, `scmResourceGroup`, `scmResourceState`, `scm.providerCount`), and the argument lists the commands of the menus get (VS Code's: the source control, the group and the resource states of the selection); `internal/gui/exthost/scm.go`: binding to a scope (ownership: only the extension that made a source changes it; disposal with the activation; callbacks `Validate`, the commands and the providers run through the containment of §10.15; a source control per scope, a duplicate id fails with `ExtensionSCMDuplicate`); `extensions/internal/fakeapi`: the same in memory; the settings `scm.*` of the SPEC registered with VS Code's descriptions, scopes and defaults; the commands of the model that need no widget (`scm.setActiveProvider`, `workbench.scm.action.setSortBy*`, the view-mode toggles as state) registered. `internal/archtest` rule for `internal/core/scm` having no Glutter import is the existing one.
**Tests:** the sorting (path with folders first as VS Code's `ResourceTree`, name, status) and the tree with compact folders (table-driven, with Unicode names, equal names in different folders); 100,000 resources sorted and treed under a budget (bounded, built lazily); counts and badges; the selection rules; the menu arguments for one, many and mixed-group selections; the contract through `testext` (create a source control, groups and resources, update, dispose with the activation, a panic in `Validate` fails only that extension, a duplicate id, a foreign source control cannot be changed, calls after deactivation fail with `ExtensionDeactivated`); `CheckManifest` with dotted context keys and submenus (refuses a key under another extension's id); `fakeapi` conforms to the same contract tests as the host (one suite for both, as for the other APIs). Route tests for every new Tag.
**Proof:** none.

### 24.3 The Source Control view
**Goal:** see the repositories, the groups and the resources of a source control, and act on them (SPEC §10.30 "The Source Control view", §10.4).
**Read:** SPEC §10.30, §10.4, §10.2 (`kit.Tree`, lists, badges, menus), §10.5 (the Explorer's rows as the model); `reference/vscode/src/vs/workbench/contrib/scm/browser/scmViewPane.ts`, `scmRepositoryRenderer.ts`, `scmRepositoriesViewPane.ts`, `scmViewPaneContainer.ts`, `scmViewService.ts`, `scm.contribution.ts`, `activity.ts`, `util.ts`, `media/scm.css`, `reference/vscode/extensions/git/package.json` (`viewsWelcome`, `menus`).
**Tasks:** `internal/gui/views/scm`: the view container `workbench.view.scm` ("Source Control", the `source-control` Codicon, order 2, Ctrl+Shift+G, `workbench.view.scm` in the View menu) and its views **Repositories** (`workbench.scm.repositories`) and **Changes** (`workbench.scm`); the Changes view of the SPEC (the header with the name, the status commands, the count and the title actions of `MenuSCMTitle`; the groups as collapsible sections with their inline actions of `MenuSCMResourceGroupContext`; the resources as a virtualized list or tree with the file icon, the name, the folder, the inline actions of `MenuSCMResourceStateContext`, the letter and color of the resource's decoration, strike-through; compact folders; the view-mode and sort actions and commands; `scm.alwaysShowActions`; the context menus with multi-selection arguments; click runs the resource's command, double click keeps; Enter, Ctrl+Enter, Alt+Enter; drag a row to the editor area; `scm.autoReveal`; the progress bar of `SetBusy`; collapse and expand of the groups remembered per repository); **welcome content** as a generic contribution (`Contributes.ViewWelcome`: view, markdown contents with links and `[Button](command:...)`, `When`, `Group`, `Enablement`, VS Code's `viewsWelcome`), used by this view and available to the Explorer; the **activity bar badge** for view containers (`activityBarBadge.*`, `scm.countBadge`: the first badges on activity bar icons, SPEC §10.4), the status bar items for the active repository's `SetStatusBarCommands`, and the dev command `dev.scm.fake` (debug and `emetos_dev` builds, §15.2) that registers a fake source control with groups from a JSON fixture for the goldens and the proof. SPEC §10.4's "Not built: badges on the icons" is updated.
**Tests:** `gluttertest` on `fakeapi` and the host: the list and the tree (goldens dark and light: groups with letters and colors, compact folders, a selected and a hovered row with inline actions, many repositories, collapsed groups, the progress bar, the badge on the icon, the status bar items); keyboard navigation and the keys above; the menu items shown by their `when`; the arguments of a command run on one row and on a multi-selection; the welcome contents for each of the Git states (table of context key values to the text shown); `scm.alwaysShowActions`; remembered collapse state; **100,000 resources** in the view (the frame cost with the list scrolling stays under the budget of §14, measured with the frame stats); dragging a row opens the file.
**Proof:** method B: `scripts/proof.sh docs/implemented-proof-images/24.3-source-control-view.png --commands '[["dev.scm.fake", {"fixture": "testdata/proof/24.3/changes.json"}], ["workbench.view.scm"]]' -- testdata/proof/24.3/project`; the image shows the Source Control icon with its badge, the header with the branch item, the groups Merge Changes, Staged Changes and Changes with their files and letters in color, one row hovered with its actions.

### 24.4 The commit input and the action button
**Goal:** type a message and run the accept command or the action button's command (SPEC §10.30 "The input and the action button").
**Read:** SPEC §10.30, §10.2, §3.1 (the editor widget as a text box); `reference/vscode/src/vs/workbench/contrib/scm/browser/scmInput.ts`, `scm.contribution.ts` (the `scm.*` input commands), `reference/vscode/extensions/git/src/actionButton.ts`; the real-input rules of SPEC §15.1.
**Tasks:** the input in `internal/gui/views/scm`: a multi-line box (the editor widget with its decorations off, or a `kit` text area if it behaves the same: the plain-text editing keys, undo, selection, the clipboard, IME), the placeholder with the key (`Message (Ctrl+Enter to commit on "main")`), growth between `scm.inputMinLineCount` and `scm.inputMaxLineCount`, `scm.inputFontFamily` and `scm.inputFontSize`, the validation line in the `inputValidation.*` colors, `Validation` from the provider on each change, Ctrl+Enter (`scm.acceptInput`, the accept command), Escape (`scm.clearValidation`, then `scm.clearInput` when the validation is empty and the box is not), the message history of the session per repository (`scm.viewPreviousCommit`/`scm.viewNextCommit` with Up/Down at the first/last line, `scm.forceView...` with Alt), `SCMInput.Focus`, and the dev command `dev.scm.input {text}` (debug and `emetos_dev` builds, §15.2) that sets the input's text; the **action button** (primary command with its icon, the dropdown of `Secondary` groups as a menu, disabled state, description as the tooltip, `scm.showActionButton`, `scm.showInputActionButton`); `git.showCommitInput`-like visibility through `SetVisible`.
**Tests:** typing, selection, undo and paste in the box; the placeholder and its branch name; growth and its limits; validation levels (goldens dark and light: empty, a message, info, warning and error validation, the button enabled, disabled, with its dropdown open); the history keys (a table of key sequences to the resulting text, including the cursor-at-the-edge rules); Ctrl+Enter runs the accept command once and only with focus; Escape's two stages; the settings take effect while the view is open; **real input** (SPEC §15.1, `emetos_display`): click the box, type a message with spaces and non-ASCII letters, Ctrl+Enter runs the command (`TestRealInputSCMCommitBox`, seen failing first when a regression is made on purpose).
**Proof:** method B: `scripts/proof.sh docs/implemented-proof-images/24.4-commit-input.png --commands '[["dev.scm.fake", {"fixture": "testdata/proof/24.4/staged.json", "input": "Fix the cache key of the loader\n\nThe key ignored the scheme.", "dropdown": true}], ["workbench.view.scm"]]' -- testdata/proof/24.3/project`; the image shows the input with a two-line message and a warning about the subject length, the Commit button with its dropdown open.

### 24.5 The Git extension: repositories and status
**Goal:** the Git extension asks whether to trust a repository, then finds repositories and shows their state in the Source Control view, read only (SPEC §10.30 "Trusting a repository", "Repositories", "Running git", D37).
**Read:** SPEC §10.30 (Trusting a repository, Repositories, the context keys, Status bar, Performance), §10.15, §10.4; `reference/vscode/extensions/git/src/main.ts`, `model.ts`, `repository.ts`, `watch.ts`, `statusbar.ts`, `diagnostics.ts`, `repositoryCache.ts`, `util.ts`, `package.json` (`configuration`, `viewsWelcome`), `test/repositoryCache.test.ts`, `test/smoke.test.ts`.
**Tasks:** `extensions/gitscm` (`emetos.gitscm`, "Git", activation `onStartup`): the manifest with every setting of the SPEC registered (VS Code's keys, types, descriptions, scopes and defaults), the context keys, the commands that later steps implement declared in the palette with their `when` clauses (VS Code's `commandPalette` menu), the Git output channel and `git.showOutput`; `Workspace().Watch` (API, `internal/core/workspace` watcher support for any absolute folder with excludes, batches per frame, `exthost` binding, `fakeapi`) and `emetos.window.focusChanged` (core event); **the trust question (D37)**: `extensions/gitscm/trust` (the store `git-trust.json` under `$XDG_STATE_HOME/emetos/`, mode 0600, atomic writes, real-path keys with symbolic links resolved, trusted parents, the tags of an unusable file, the session-only Don't Trust), the dialog (Trust Repository, Trust All Repositories in <parent>, Don't Trust, with the list of programs the configuration names), Emetos's own reader of `.git/config` and `config.worktree` in `extensions/gitscm/git` (runs nothing; the keys of the SPEC), the guarantee that no `git` process has the repository as its working directory, `-C`, `GIT_DIR` or `GIT_WORK_TREE` before the answer (the runner refuses with `GitRepositoryNotTrusted` unless the repository is trusted, so a later step cannot forget), the commands `git.trustRepository` and `git.manageTrustedRepositories`, the context key `git.untrustedRepositoryCount`, the welcome content with the **Trust Repository** button, trust inherited by submodules and worktrees, and the dev knob `EMETOS_DEV_GIT_TRUST`; the model: discovery, which looks at the file system only (a `.git` folder or file, the `gitdir:` line), in the order of the SPEC (the folder, parent folders with the prompt and `git.openRepositoryInParentFolders`, subfolders by `git.autoRepositoryDetection`, open editors' files, submodules with `git.detectSubmodulesLimit`, `git.ignoredRepositories`), unsafe repositories, close and reopen; the repository: state (HEAD, upstream, ahead and behind, refs, remotes, stash count, the in-progress states by their files), status into the groups with `git.untrackedChanges`, `git.statusLimit` and its warning, `git.similarityThreshold`, optimistic-update hooks, refresh by the debounced watcher (500 ms), after operations, on save and on window focus, `git.autorefresh`; the source control, its groups and resources, its count (`git.countBadge`), the status bar commands (the branch item with VS Code's icon rules and the sync item, display only until 24.13 and 24.15), the progress (`SetBusy`) while a command runs, the welcome contents of the SPEC; `git.refresh`, `git.close`, `git.reopenClosedRepositories`, `git.closeOtherRepositories`, `git.openRepository`, `git.manageUnsafeRepositories`, `git.openRepositoriesInParentFolders`; the notification about the programs a repository's configuration can run (SPEC "Untrusted repositories"); the event `emetos.gitscm.repositoryChanged` and the request `emetos.gitscm.status`; `scripts/perf-git.sh` for the 100,000-file measurements (SPEC §14, recorded in `docs/PERFORMANCE.md`).
**Tests:** the trust question with the fake `git` that records every call: nothing is recorded for a repository before the answer, nothing after Don't Trust, the first call after Trust is the repository's own; the dialog's buttons and text (goldens dark and light: with and without listed programs); the answers kept (Trust is in the file after a restart, Don't Trust is not; a parent folder covers a repository below it and not a sibling with the same prefix; a symbolic link to a trusted path is the same repository; a missing, corrupt or future-version file is ignored with its Tag and the question is asked); a failing write keeps the answer for the session; Manage Trusted Repositories removes one and stops its processes; submodules and worktrees follow their parent; Clone and Initialize Repository (24.16) trust what they make; the config reader (a table of `.git/config` texts: includes ignored, quoting, `!` aliases, case-insensitive keys, a 1 MiB file bounded); the welcome content states; discovery in a table of repository layouts built with `gittest` (the folder is the root, is inside one, has a repository in a parent, has two in subfolders at depth 1 and 2, has a submodule, has a nested repository inside an ignored folder, `.git` as a file for a submodule and a worktree, a bare directory, a folder with no repository) under every `git.autoRepositoryDetection` and `git.openRepositoryInParentFolders` value; unsafe repositories with the fake git; status for every `XY` combination (a real repository in each state) into the right group under each `git.untrackedChanges`; renames; `git.statusLimit` with the warning once; ahead/behind and upstream from a local bare remote; the in-progress states from real merge, rebase and cherry-pick stops; the watcher (a burst of 1,000 file changes makes one status; changes under `.git/objects` make none; Emetos's own commands do not retrigger; a fake clock); refresh on focus; closing a repository stops its watcher and processes (nothing left in `pgrep -a git`); the welcome content for each state; idle (no process, no goroutine growth after 10 minutes of fake clock); the notification for a configuration that names programs (a table of keys); `-race`. A display test with the real extension and the real view over `gittest` repositories.
**Proof:** method B, two images. First `docs/implemented-proof-images/24.5-trust.png`: `scripts/proof.sh docs/implemented-proof-images/24.5-trust.png --commands '[["workbench.view.scm"], ["sleep", "1s"]]' -- <dir>` on the `small` fixture without the dev knob; it shows the trust dialog over the window with the Source Control view behind it (the welcome content with **Trust Repository**). Then make the fixture with `scripts/git-fixture.sh small <dir>` and run, with `EMETOS_DEV_GIT_TRUST=<dir>`, `scripts/proof.sh docs/implemented-proof-images/24.5-source-control.png --commands '[["workbench.view.scm"], ["sleep", "1s"]]' -- <dir>`; the image shows the real Git extension's view of the `small` fixture: the branch and sync items in the header and the status bar, the groups with files, letters and counts, and the badge.

### 24.6 File decorations
**Goal:** the Explorer, Open Editors, the editor tabs, the breadcrumbs and the Search results show the status of files in color and letters; Git provides them (SPEC §10.30 "File decorations and the Explorer").
**Read:** SPEC §10.30, §10.3 (tabs and `workbench.editor.decorations.*`), §10.5, §10.27 (`search.decorations.*`), §10.2; `reference/vscode/src/vs/workbench/services/decorations/browser/decorationsService.ts`, `services/decorations/test/browser/decorationsService.test.ts`, `reference/vscode/extensions/git/src/decorationProvider.ts`, `repository.ts` (`Resource`: letters, colors, tooltips, priorities), `test/decorationProvider.test.ts`, `package.json` (`colors`).
**Tasks:** `extension/decorations.go` (`api.Decorations()`, `FileDecorationProvider`, `FileDecoration`, the handle with `Fire`), the host binding and `fakeapi`; `internal/core/decorations` (the service: weights, tooltips joined with " • ", colors, badges joined with ", ", bubbling to parent folders with the folder's own mark, a deleted file not propagating, a cache by URI invalidated by `Fire`, bounded); the use in `internal/gui/views/explorer` (names in the color, the badge at the end of the row, the folder dot; `explorer.decorations.colors` and `explorer.decorations.badges`, true), Open Editors, the editor tabs (`workbench.editor.decorations.colors` exists; add `.badges`), the breadcrumbs, the Search results (`search.decorations.*` exist) and the Source Control view's rows; the colors `gitDecoration.*` (ten, VS Code's values for the four theme kinds) registered; the Git provider in `extensions/gitscm` (`git.decorations.enabled`, the letters `M A D R C U I T !`, the tooltips, the priorities, the ignored files asked in batches for visible rows, submodules, the staged colors), firing for the URIs whose status changed after each refresh.
**Tests:** the service (a port of `decorationsService.test.ts`'s cases that apply: weights, bubbling, change events, disposal; table-driven); the Git provider for every status (a table from `Resource`'s methods, then the real thing over a `gittest` repository); folder bubbling for a nested change and its removal when the file is committed; the views (goldens dark and light: the Explorer with a modified, an added, an untracked, an ignored, a deleted and a conflicted file, a folder with a changed child, tabs with colors and letters, the Search results); the settings turn colors and badges off; a provider that answers slowly is logged and does not freeze the Explorer; 100,000 decorated files do not slow the Explorer's frame (bounded cache).
**Proof:** method B on the `small` fixture: `scripts/proof.sh docs/implemented-proof-images/24.6-decorations.png --commands '[["workbench.view.explorer"], ["dev.openFile", {"path": "src/loader.go"}], ["sleep", "1s"]]' -- <dir>`; the image shows the Explorer with colored names and letters, a folder with the dot, and the tabs of two modified files.

### 24.7 Diffs of changes
**Goal:** open the changes of a file against HEAD, the index and the work tree, from the Source Control view and the editor (SPEC §10.30 "Diffs of changes").
**Read:** SPEC §10.30, §9.11 (the diff editor, virtual documents, `emetos-virtual:` as built, Compare with Saved), §7.1; `reference/vscode/extensions/git/src/repository.ts` (`ResourceCommandResolver`), `commands.ts` (`openChange`, `openFile`, `openHEADFile`, `openAllChanges`, `_openResource`), `fileSystemProvider.ts`, `uri.ts`, `package.json` (`editor/title` menus), `test/git.test.ts` (uri cases).
**Tasks:** `Documents().ProvideContent(ContentProvider)` and `Documents().OpenDiff(DiffSpec)` (API, host, `fakeapi`; the content is generated off the UI thread, read-only, never dirty, backed up or restored beyond the URI, §7.1; a diff whose provider is gone is dropped on restore); `extensions/gitscm`: the content provider for `emetos-virtual:/git/<repository id>/<ref>/<path>` (`HEAD`, the index, `~1` `~2` `~3`, a commit, with the language of the file, an empty text for a missing version, the 16 MiB limit); `ResourceCommandResolver` (which two URIs and which title for each status and group, VS Code's titles `a.go (Index)`, `(Working Tree)`, `(Deleted)`, `(Untracked)`...); `git.openChange`, `git.openFile`, `git.openFile2`, `git.openHEADFile`, `git.openAllChanges` (asks above 10 files), `git.openFile` for merge conflicts, the resource click command with `git.openDiffOnClick`, preview versus kept tabs, `git.closeDiffOnOperation`, `git.closeAllDiffEditors`, `git.closeAllUnmodifiedEditors`, `git.revealInExplorer`, `git.revealFileInOS.linux` ("Open Containing Folder"); the editor title items (`MenuEditorTitle`: Open File in diff editors, Open Changes for a file with changes) and the context keys `git.activeResourceHasUnstagedChanges`, `git.activeResourceHasStagedChanges`, `scmActiveResourceHasChanges`; the diff editor's titles and the `resourceScheme` context for virtual documents (`git`).
**Tests:** the resolver for every status and group (a table of expected left and right URIs and titles, ported from `ResourceCommandResolver`); the content provider with real git for HEAD, index, work tree and conflict stages, a deleted file, a new file, a binary file (refused as a text with the editor's notice), a file over the limit, a symlink; opening and re-opening keeps one tab, preview rules; `git.closeDiffOnOperation`; a restored session drops the diff of a repository that is gone silently; goldens of the diff editor with the Git titles (dark and light).
**Proof:** method B on `small`: `scripts/proof.sh docs/implemented-proof-images/24.7-open-changes.png --commands '[["workbench.view.scm"], ["git.openChange", {"path": "src/loader.go"}], ["sleep", "1s"]]' -- <dir>`; the image shows the Source Control view and the diff editor titled `loader.go (Working Tree)`.

### 24.8 Stage, unstage and discard
**Goal:** move changes between the work tree and the index, and throw them away safely (SPEC §10.30 "Staging, discarding and committing").
**Read:** SPEC §10.30, §6.3 (the trash), §10.7 (dialogs); `reference/vscode/extensions/git/src/commands.ts` (`stage`, `stageAll`, `unstage`, `clean`, `cleanAll`, `ignore`, `rename`, `delete`), `repository.ts` (`add`, `revert`, `clean`, `rm`, `optimistic`), `staging.ts`, `test/stackOperations.test.ts`.
**Tasks:** `Repo` methods `Add`, `Reset`, `Checkout` paths, `Clean`, `Rm`, `Mv`, `AddIntentToAdd`; the commands `git.stage`, `git.stageAll`, `git.stageAllTracked`, `git.stageAllUntracked`, `git.stageAllMerge`, `git.stageFile`, `git.unstage`, `git.unstageAll`, `git.unstageFile`, `git.clean`, `git.cleanAll`, `git.cleanAllTracked`, `git.cleanAllUntracked`, `git.ignore`, `git.rename`, `git.delete`; folder actions (`MenuSCMResourceFolderContext`), multi-selection, the resources of a group menu, `git.untrackedChanges` modes, the first commit's unstage (`rm --cached`), renames staged as pairs, the confirmations of the SPEC (counts, names, "This action is irreversible!", `git.confirmCommittedDelete`), the trash for untracked files (`git.discardUntrackedChangesToTrash`, §6.3) with an undo notification "Moved to Trash", clean open documents reloaded after a discard (the external-change path of §6.3, a dirty one asks as always), `git.optimisticUpdate`, the menus and their `when` of VS Code's `package.json` for `scm/resourceState/context`, `scm/resourceGroup/context` and `scm/resourceFolder/context`.
**Tests:** each command against a real repository, checking `git status` and the index afterwards (tables of the starting state: modified, deleted, untracked, renamed, intent-to-add, conflicted, a fresh repository before its first commit, a submodule, a path with spaces and non-ASCII, a symlink, 5,000 paths); the confirmations (Cancel changes nothing; the text for one and many); the trash; an open clean document reloads, a dirty one asks; the optimistic update is undone when git fails; failures with their Tags and routes; goldens of the discard dialog (dark and light).
**Proof:** method B on `small`: `scripts/proof.sh docs/implemented-proof-images/24.8-stage.png --commands '[["workbench.view.scm"], ["git.stage", {"path": "src/loader.go"}], ["git.clean", {"path": "docs/notes.md"}], ["sleep", "1s"]]' -- <dir>` (the second command's dialog is open when the image is made); the image shows a file moved to Staged Changes and the discard confirmation.

### 24.9 Passwords and editors: the helper
**Goal:** git asks for a password, a passphrase or an editor, and Emetos answers in its own window, storing nothing (SPEC §10.30 "Passwords and editors", D35).
**Read:** SPEC §10.30, §10.11 (`emetos --wait`), §12; `reference/vscode/extensions/git/src/askpass.ts`, `askpass-main.ts`, `askpassManager.ts`, `askpass.sh`, `ssh-askpass.sh`, `git-editor-main.ts`, `gitEditor.ts`, `git-editor.sh`, `ipc/ipcServer.ts`, `ipcClient.ts`, `test/askpassManager.test.ts`; `man gitcredentials`, `man ssh` (`SSH_ASKPASS_REQUIRE`).
**Tasks:** `extensions/gitscm/helper`: `Main(args, env, stdin, stdout) int` for `--git-askpass`, `--git-ssh-askpass` and `--git-editor` (read `EMETOS_GIT_SOCKET` and `EMETOS_GIT_TOKEN`, send one JSON request, print the reply, exit 0 or 1), the server (Unix socket in `$XDG_RUNTIME_DIR/emetos/git-<pid>/`, 0700 folder and 0600 socket, the token compared in constant time, `SO_PEERCRED` user id, one request per connection with a size bound and a deadline, stale folders of dead processes removed at the start, everything removed when the extension ends), the three scripts (no secret in them; the quoting of the binary's path tested with a path that has a space and a quote); `cmd/emetos` runs `helper.Main` before any other work for the three flags; the prompts through `InputBox` (password with dots) and `QuickPick` as in the SPEC, the prompt classified from git's and ssh's real texts (`Username for`, `Password for`, `Enter passphrase for key`, `Enter passphrase for`, the host-key question), cancel = exit 1 = `AuthenticationFailed` with **Retry**; the environment given to every command when `git.useIntegratedAskPass`; the editor flow (the file opened in an editor tab of the window tab, answer when the tab closes: 0 after a save, 1 otherwise), `COMMIT_EDITMSG` opened with the title actions `git.commitMessageAccept` (saves and closes) and `git.commitMessageDiscard` (closes without saving) as editor title actions, `git.useEditorAsCommitInput`; log masking checked end to end.
**Tests:** the helper protocol (table-driven: requests, replies, a wrong token, a stale socket, a request of 10 MiB, a peer that never answers, the folder and socket modes, two window tabs with two sockets); the scripts (`sh -n`, the quoting, no secret in the content); the prompt classifier against a table of real git and ssh prompts; **real git** against `HTTPBackend` that requires Basic credentials: a clone, a fetch and a push each ask for the username and the password through the real helper and a test responder, a wrong password fails with `AuthenticationFailed`, Escape fails and offers Retry, a `credential.helper=store` file inside the isolated `HOME` answers git first and the prompt never appears, the typed password is in no log line, no file and no notification text; ssh passphrase and host-key prompts are parsed from the real strings of `ssh` (no `sshd`); the editor with real `git commit` and `git rebase -i` (todo edited, tab closed after a save → success, closed without saving → git aborts); the amend and `useEditorAsCommitInput` flows; a display test with **real input** (`emetos_display`) typing a password into the prompt of a real `git fetch` against `HTTPBackend`; `pgrep -a` shows no helper left.
**Proof:** method B with `HTTPBackend` started by the dev command `dev.git.testserver` (debug and `emetos_dev` builds): `scripts/proof.sh docs/implemented-proof-images/24.9-password-prompt.png --commands '[["dev.git.testserver", {"user": "ada"}], ["git.fetch"], ["sleep", "1s"]]' -- <dir>`; the image shows the input box over the editor with the dots of a password, the prompt `Git: http://127.0.0.1:<port>` and the placeholder `Password for 'http://ada@127.0.0.1:<port>'`.

### 24.10 Commit
**Goal:** commit from the input and the action button, with the options VS Code has (SPEC §10.30 "Staging, discarding and committing").
**Read:** SPEC §10.30, §10.7; `reference/vscode/extensions/git/src/commands.ts` (`commit`, `_commit`, `commitEmpty`, `undoCommit`, `smartCommit`), `repository.ts` (`commit`, `getCommitTemplate`, input box handling), `actionButton.ts`, `postCommitCommands.ts`, `branchProtection.ts`, `diagnostics.ts`, `package.json` (the `git.commit` submenu, the `git.commit*` titles).
**Tasks:** `Repo.Commit` (message from the input, `--amend`, `-s`, `--no-verify`, `--allow-empty`, `-S` pass-through, `--verbose`, the template from `commit.template` and `git.restoreCommitTemplate`, `-c user.useConfigOnly=true`) and `Repo.Reset --soft`; the commands of the SPEC (`git.commit`, `commitStaged`, `commitAll`, `commitEmpty`, `commitAmend`, `commitSigned` and each `NoVerify` variant, `git.undoCommit`, `git.commitMessageAccept` and `Discard` use the same path); the input wired to the source control (accept command, placeholder with the branch, validation per `git.inputValidation*`, the message kept across failures, cleared on success, amend loading the last message, undo putting it back); smart commit and its question, `git.confirmEmptyCommits`, `git.promptToSaveFilesBeforeCommit` (`Documents().Dirty` and `SaveAll`, API), `git.requireGitUserConfig` with the guided setting of the name and e-mail (shows the exact `git config --global` commands and asks before running them), branch protection with its prompt and `git.branchProtectionPrompt`, `git.diagnosticsCommitHook.*` against the Problems model, `git.allowNoVerifyCommit` and its confirmation, `git.alwaysSignOff`, `git.enableCommitSigning`, `git.verboseCommit`; the **action button** (Commit, Commit (Amend), Continue, with the dropdown groups of VS Code, `git.showActionButton`, disabled while a command runs) and `git.postCommitCommand` (`push`/`sync` run after a commit; `git.rememberPostCommitCommand`); the continue-state placeholder text (merge, rebase and cherry-pick finish in 24.14).
**Tests:** each command against a real repository (the resulting commit's message, author, parents, signoff trailer, tree; amend; empty; staged-only versus all with smart commit; a pre-commit hook that passes, fails (its output reaches the error) and is skipped by no-verify; the identity missing and the guided fix, with the isolated global config; branch protection prompts; the diagnostics hook with markers of each severity; unsaved documents saved or not by `git.promptToSaveFilesBeforeCommit` in its three values); the input and button states in a table (state to text, enabled, command); a commit while another runs queues; the message survives a failed commit; goldens of the input with validation and the button states (dark and light); a display test with real input: type a message, Ctrl+Enter, the commit exists (`TestRealInputGitCommit`).
**Proof:** method B on `small` (a file staged): `scripts/proof.sh docs/implemented-proof-images/24.10-commit.png --commands '[["workbench.view.scm"], ["dev.scm.input", {"text": "Fix the cache key of the loader"}], ["scm.acceptInput"], ["sleep", "1s"]]' -- <dir>`; the image shows the view after the commit: the Staged Changes group gone, the input empty, the action button saying "Sync Changes" or "Publish Branch", and the branch item.

### 24.11 Quick diff: the marks
**Goal:** the editor's gutter, overview ruler and minimap show what changed against the index, and next/previous change move through them (SPEC §10.30 "Quick diff").
**Read:** SPEC §10.30, §9.4, §9.5, §9.11 (`core/diff`), §7.1 (decorations), §9.12 (format on save of modifications); `reference/vscode/src/vs/workbench/contrib/scm/browser/quickDiffDecorator.ts`, `quickDiffModel.ts`, `contrib/scm/common/quickDiff.ts`, `quickDiffService.ts`, `reference/vscode/extensions/git/src/quickDiffProvider.ts`, `package.json`; `reference/vscode/src/vs/editor/browser/widget/diffEditor` for the change shapes.
**Tasks:** `extension/quickdiff.go` (`QuickDiffProvider`, `SourceControl.AddQuickDiffProvider`, `RefreshQuickDiff`, `Documents().QuickDiffOriginal`), host binding, `fakeapi`; `internal/core/scm/quickdiff` (the model: the changes of a document against an original as VS Code's `IChange` — added, modified, deleted, with their line ranges — recomputed off the UI thread 250 ms after the last edit with `core/diff` (time limit 1 s, `scm.diffDecorationsIgnoreTrimWhitespace`), following edits meanwhile through decorations, the 2 MiB limit, the primary provider and the secondary ones, next and previous change with wrap); `internal/gui/editor`: the gutter stripes (`scm.diffDecorationsGutterWidth`, `Visibility`, `Pattern`), the deleted-lines triangle, the overview ruler marks, the minimap marks (`scm.diffDecorations` values), and the colors `editorGutter.*`, `editorOverviewRuler.added/modified/deletedForeground`, `minimapGutter.*`; the commands `workbench.action.editor.nextChange` and `previousChange` (Alt+F5, Shift+Alt+F5 with `editorTextFocus && !isInDiffEditor`, as VS Code, so the diff editor's own keys stay); the two Git providers (`Git Local Changes (Working Tree)`: the original is the index version, `Git Local Changes (Index)`: HEAD, with the rules of `quickDiffProvider.ts` for untracked, ignored, merge, symlink and `.git` files), refreshed after each operation and each save; **format on save of modifications** uses `Documents().QuickDiffOriginal` as its baseline when there is one (SPEC §9.12 text, decision 3, updated in this step).
**Tests:** the model (a table of an original and a modified text to the changes: an insertion, a deletion at the start, in the middle and at the end, a modification, adjacent changes, CRLF, whitespace-only with the setting, an empty original, an empty document, a file with a trailing newline difference); following edits and recomputing (seeded random edits check the marks against a fresh diff); the time limit; the provider rules with real git (every file kind); next/previous with wrap; the three places and their settings (goldens dark and light: stripes with each width, the pattern, the hover-only visibility, deleted triangles, the ruler and the minimap, a theme with other colors); format-on-save modifications with a provider and without (the edited ranges only); a 10,000-line file's recompute under the budget of §14 (benchmark).
**Proof:** method B on `small`: `scripts/proof.sh docs/implemented-proof-images/24.11-quick-diff.png --commands '[["dev.openFile", {"path": "src/loader.go", "line": 20}], ["sleep", "1s"]]' -- <dir>`; the image shows the editor with green, blue and red marks in the gutter, the ruler and the minimap.

### 24.12 Quick diff: the peek and the hunk actions
**Goal:** look at one change in place, and stage or revert it, or a selection (SPEC §10.30 "The peek", "Staging" by ranges).
**Read:** SPEC §10.30, §9.1 (view zones), §9.11 (the diff editor and its inline zones); `reference/vscode/src/vs/workbench/contrib/scm/browser/quickDiffWidget.ts`, `reference/vscode/extensions/git/src/staging.ts`, `commands.ts` (`stageChange`, `revertChange`, `unstageChange`, `stageSelectedRanges`, `unstageSelectedRanges`, `revertSelectedRanges`, `diffStageHunk`, `diffStageSelection`), `package.json` (`scm/change/title`, `editor/context`, `editor/title`, `diffEditor/gutter/*`, `keybindings`), `test/git.test.ts`.
**Tasks:** `internal/gui/quickdiffpeek`: the peek (a view zone under the change with a title bar of the provider's label, "Change 2 of 5", the actions of `MenuSCMChangeTitle`, previous, next and close; a small diff editor of the change's original and modified text; follows edits; the focus rules), opened by a click on a stripe (`scm.diffDecorationsGutterAction`) and by `editor.action.dirtydiff.next`/`previous` (Alt+F3, Shift+Alt+F3), closed by `closeQuickDiff` (Escape, Shift+Escape), `toggleQuickDiffWidgetFocus` (Ctrl+K F2); `extensions/gitscm/staging.go` (`applyLineChanges` ported from `staging.ts`: the new index text from the original text, the modified text and the selected changes) and the commands `git.stageChange`, `git.unstageChange`, `git.revertChange` (arguments `(uri, []LineChange, index)`), `git.stageSelectedRanges`, `git.unstageSelectedRanges`, `git.revertSelectedRanges` (with VS Code's keys Ctrl+K Ctrl+Alt+S, Ctrl+K Ctrl+N, Ctrl+K Ctrl+R and `when`), written to the index through `hash-object -w` and `update-index --cacheinfo` (never through the work tree), a revert as one undoable edit of the document; the diff editor's gutter hunk buttons and selection toolbar (`git.diff.stageHunk` "Stage Block", `git.diff.stageSelection`; `MenuDiffEditorGutterHunk`/`Selection` in `internal/gui/diffeditor`) and its editor title and context items for the right side of a working-tree diff.
**Tests:** `applyLineChanges` (a port of the cases of `staging.ts`'s behavior: every change kind, selections that split a change, the first and last lines, CRLF, no trailing newline, an empty file; checked against `git apply --cached` of the same patch in a real repository); the commands with real git (stage one change of three leaves the other two in the work tree and the file bytes unchanged; unstage; revert restores the original lines and is undone by Ctrl+Z; stale changes after an edit are refused with a message); the peek (goldens dark and light: modified, added, deleted, the title bar, the actions; navigation; it follows an edit; Escape; focus); the gutter buttons of the diff editor; keys and `when` clauses; a display test with real clicks on a stripe and on Stage Change.
**Proof:** method B on `small`: `scripts/proof.sh docs/implemented-proof-images/24.12-peek.png --commands '[["dev.openFile", {"path": "src/loader.go", "line": 20}], ["editor.action.dirtydiff.next"], ["sleep", "1s"]]' -- <dir>`; the image shows the peek under a change with "Stage Change", "Revert Change" and the arrows.

### 24.13 Branches and tags
**Goal:** switch, create, rename and delete branches and tags (SPEC §10.30 "Branches, tags, merge and the states in between").
**Read:** SPEC §10.30; `reference/vscode/extensions/git/src/commands.ts` (`checkout`, `branch`, `branchFrom`, `renameBranch`, `deleteBranch`, `deleteRemoteBranch`, `createTag`, `deleteTag`, `deleteRemoteTag`, `pushTags`), `repository.ts` (`checkout`, `branch`, `getRefs`, `Operation`s), `statusbar.ts`, `branchProtection.ts`, `package.json` (`git.checkout*`, `git.branch*`, `scm/*` menus).
**Tasks:** `QuickPickOptions.Title` and `QuickPickItem.Separator` (API, host, `fakeapi`); `Repo` methods `Checkout`, `Branch`, `RenameBranch`, `DeleteBranch`, `DeleteRemoteBranch`, `Tag`, `DeleteTag`, `DeleteRemoteTag`, `PushTag(s)`, `Refs`; the commands of the SPEC with VS Code's pickers (create-new entries, `git.checkoutType`, `git.branchSortOrder`, the details of `git.showReferenceDetails`, remote branches checked out as tracking branches, detached checkout, `git.branchPrefix`, `git.branchValidationRegex`, `git.branchWhitespaceChar`, `git.defaultBranchName`), the dirty work tree flow (**Stash & Checkout**, **Force Checkout**, `git.autoStash`), "not fully merged" with **Force Delete**, the status bar branch item and its click (`git.checkout`) with the running state, `git.createTag` (lightweight or annotated), the branch protection of 24.10 reading these refs; `git.pullBeforeCheckout` is read here and acts when 24.15 lands.
**Tests:** each command against real repositories (a table of the starting state and the picks): checkout a local, a remote (tracking created), a tag (detached), `-b`; creation from a ref; invalid names (`git.branchValidationRegex`, whitespace replaced); rename; delete merged, unmerged (force offered), current (refused), a remote branch against a bare remote; tags; the dirty checkout flows; the pickers (goldens dark and light: the checkout picker with separators and details); the status bar icon for each HEAD state (a branch, a protected branch, conflicts, staged, changes, a tag, a detached commit, a running checkout); failures and their Tags/routes.
**Proof:** method B on `history`: `scripts/proof.sh docs/implemented-proof-images/24.13-checkout.png --commands '[["git.checkout"], ["sleep", "1s"]]' -- <dir>`; the image shows the picker with its separators, branches with hashes and subjects, remote branches and tags, over the editor.

### 24.14 Merge, rebase, cherry-pick and conflicts
**Goal:** run the operations that stop on conflicts, see where they stand, and finish them with Emetos's merge editor (SPEC §10.30 "In-progress states", "Conflicts", §9.11).
**Read:** SPEC §10.30, §9.11 (the merge editor, Complete Merge, `git.openMergeEditor`); `reference/vscode/extensions/git/src/commands.ts` (`merge`, `mergeAbort`, `rebase`, `rebaseAbort`, `cherryPick`, `cherryPickAbort`, `acceptMerge`, `openMergeEditor`, `runGitMerge`, `stageAllMerge`), `repository.ts` (the merge group, `rebaseCommit`, `mergeInProgress`), `util.ts`, `package.json`.
**Tasks:** `Repo` methods `Merge`, `MergeAbort`, `Rebase`, `RebaseAbort`, `CherryPick`, `CherryPickAbort`, `Continue` (`merge --continue`, `rebase --continue`, `cherry-pick --continue`), `MergeFile`; the commands of the SPEC; detection of the states from `MERGE_HEAD`, `rebase-merge/`, `rebase-apply/`, `CHERRY_PICK_HEAD`, `REVERT_HEAD` (and `git.rebaseCommit` text "(Rebasing)"); the **Merge Changes** group and its resources (letter `!`, tooltips "Conflict: Both Modified"..., the seven forms), `git.stageAllMerge`, the click behavior (`git.mergeEditor`, default false: the text editor with the notice of §9.11; true: the merge editor), `git.runGitMerge` and `git.runGitMergeDiff3` (the three stages through `git merge-file -p`/`--diff3` into the document), the core event `emetos.mergeEditor.completed {uri}` published by the merge editor's Complete Merge (a change in `internal/gui/mergeeditor` and `gui/app`, SPEC §9.11 text updated) and `git.acceptMerge` (stage the file on that event when it is in the merge group); the commit while a state is in progress finishes it with the operation's message (the input's placeholder and the action button's "Continue" from 24.10), `rebase -i` through the editor helper of 24.9; the context key `git.mergeChanges` (the group's files) and `git.activeResourceHasMergeConflicts`; the editor content items (`MenuEditorContent`: Resolve in Merge Editor, Complete Merge).
**Tests:** real git scenarios (a table): merge with and without conflicts, fast-forward, abort; rebase of two commits with a conflict in the second, continue, abort, skip; `rebase -i` with the todo edited in the Emetos editor tab; cherry-pick with a conflict, continue, abort; every conflict form (both modified, both added, deleted by us, deleted by them, added by us, added by them, both deleted) shows in the merge group with the right letter and tooltip and opens the right thing; the merge editor completes a real conflicted file and the extension stages it, after which the group is empty and Continue finishes the merge commit; `git.runGitMerge` rewrites the markers to what `git merge-file` makes; states survive a restart of the extension (found again from the files); the view and status bar in each state (goldens dark and light).
**Proof:** method B on `conflict`: `scripts/proof.sh docs/implemented-proof-images/24.14-merge-changes.png --commands '[["workbench.view.scm"], ["dev.openFile", {"path": "src/loader.go"}], ["git.openMergeEditor"], ["sleep", "1s"]]' -- <dir>`; the image shows the Merge Changes group with `!` files, the branch item with the conflicts icon, the action button "Continue" and the merge editor over a conflicted file.

### 24.15 Remotes, fetch, pull, push, sync and autofetch
**Goal:** talk to remotes with the user's `git`, credentials and keys (SPEC §10.30 "Remotes, fetch, pull, push and sync").
**Read:** SPEC §10.30, §12 (privacy), D33, D35; `reference/vscode/extensions/git/src/commands.ts` (`fetch`, `fetchAll`, `pull`, `pullRebase`, `pullFrom`, `push`, `pushTo`, `pushForce`, `pushTags`, `sync`, `syncRebase`, `publish`, `addRemote`, `removeRemote`, `_push`, `handlePushError`), `repository.ts` (`fetch`, `pull`, `push`, `sync`, `pushForce`), `autofetch.ts`, `pushError.ts`, `statusbar.ts` (the sync item), `remoteSource.ts` for what is NOT ported, `package.json`.
**Tasks:** `Notifications().Progress` (API, host: a notification with a progress bar, cancellable, `Report(message, percent)`; `fakeapi`); `Repo` methods `Fetch`, `FetchAll`, `Pull`, `Push`, `PushForce` (`--force-with-lease`, `--force-if-includes`), `SetUpstream`, `AddRemote`, `RemoveRemote`, `Remotes`, with `--progress` parsing; the commands of the SPEC; the sync status bar item and its states (ahead/behind counts, publish when there is no upstream, spinning while it runs, `git.enableStatusBarSync`, `git.confirmSync`); the action button's Sync Changes and Publish Branch; `git.publish` to any remote (no service repository is created), `git.addRemote` URL validation; the push errors of VS Code (rejected → **Pull**, no upstream → **Publish Branch**, `git.allowForcePush` and `git.confirmForcePush`, protected-branch refusals as the remote sends them), `git.pullBeforeCheckout` (24.13's setting now acts), `git.fetchOnPull`, `git.pullTags`, `git.replaceTagsWhenPull`, `git.followTagsWhenSync`, `git.rebaseWhenSync`, `git.autoStash` for pulls, `git.pruneOnFetch`, `git.showPushSuccessNotification`, `git.showProgress`; **autofetch** (SPEC): off by default, a loop that waits for an idle repository and a focused window, the period, `all`, off after `AuthenticationFailed`, the one-time question after the first good remote operation (the answer kept in the profile's state, not in `settings.json`, unless Yes), no fetch before the answer.
**Tests:** real git against local bare remotes: fetch (new commits and tags, prune), pull (fast-forward, merge, rebase, conflicts, a dirty tree with `git.autoStash`), push (new branch with `-u`, rejected, force with lease succeeding and being refused when the remote moved, tags, follow tags), sync (pull then push, the confirm), publish, add and remove remotes, ahead/behind in the status after each; HTTP authentication through `HTTPBackend` and the helper (the 24.9 harness) for fetch and push, with a wrong password and a cancelled prompt; `ssh://` URLs with a fake `ssh` program in `GIT_SSH_COMMAND` that fails with a permission error (the message and Tag) and that asks `SSH_ASKPASS` for a passphrase (the prompt reaches the input box); progress parsing from real `--progress` output; autofetch with a fake clock (period, focus, an operation running, disabled by a failed authentication, `all`, the question once, "Ask Me Later", No) and **no process started** while it is off; the status bar and button states table; goldens of the sync item and the progress notification.
**Proof:** method B on a clone of a bare remote made by `scripts/git-fixture.sh history` (ahead 1, behind 2): `scripts/proof.sh docs/implemented-proof-images/24.15-sync.png --commands '[["workbench.view.scm"], ["sleep", "1s"]]' -- <dir>`; the image shows "Sync Changes 2↓ 1↑" on the action button and in the status bar, the repository header with the counts.

### 24.16 Clone, init and open repository
**Goal:** get a repository: clone, initialize, open (SPEC §10.30 "Clone, init, open").
**Read:** SPEC §10.30, §10.14 (workspace tabs), §10.10 (recent folders); `reference/vscode/extensions/git/src/cloneManager.ts`, `commands.ts` (`clone`, `cloneRepository`, `init`, `openRepository`), `repository.ts`, `test/cloneManager.test.ts`, `package.json` (`viewsWelcome` of the Explorer and the SCM view).
**Tasks:** `Repo.Clone` (`git clone --progress [--recursive]`, progress parsed into `Notifications().Progress`, cancel kills the group and removes the partly cloned folder), `git.clone`, `git.cloneRecursive` (URL input with validation, the parent folder through `FileChooser` starting at `git.defaultCloneDirectory`, an existing target refused), the question after cloning and `git.openAfterClone` (`prompt`, `always`, `alwaysNewWindow`, `whenNoFolderOpen`; opening is a workspace tab, §10.14, through `Workspace().OpenFolder`, API, host, `fakeapi`), `git.init` (the folder pick, `git init -b <git.defaultBranchName>`, then the repository opens), `git.openRepository`; the welcome contents with the **Initialize Repository** and **Clone Repository** buttons for a window with no repository and for the empty Explorer; the clone URL's secrets masked everywhere; a repository made by Clone or Initialize Repository is trusted at once (SPEC "Trusting a repository"), and a clone's submodules with it.
**Tests:** clone from local bare repositories (progress lines, recursive with a submodule, an existing target, a bad URL, cancel in the middle with a slow fake `git` leaves no folder), the URL validation table (https, ssh, scp-like, `file://`, paths, spaces); `openAfterClone` for every value; init with the default branch name (and `init.defaultBranch` unset); welcome states; a clone over `HTTPBackend` with authentication through the helper; goldens of the welcome content (dark and light).
**Proof:** method B on an empty folder: `scripts/proof.sh docs/implemented-proof-images/24.16-welcome.png --commands '[["workbench.view.scm"], ["sleep", "1s"]]' -- <empty dir>`; the image shows "The folder currently open doesn't have a Git repository" with the two buttons.

### 24.17 The multi-file diff editor
**Goal:** see the changes of many files in one scrolling editor: a group, a commit, a stash (SPEC §10.30 "Diffs of changes").
**Read:** SPEC §10.30, §9.11 (the diff editor, view zones), §10.3 (editor kinds); `reference/vscode/src/vs/workbench/contrib/multiDiffEditor/browser/multiDiffEditor.ts`, `multiDiffEditorInput.ts`, `actions.ts`, `reference/vscode/src/vs/editor/browser/widget/multiDiffEditor/` (`multiDiffEditorWidget.ts`, `multiDiffEditorWidgetImpl.ts`, `diffEditorItemTemplate.ts`, `workbenchUIElementFactory.ts`), `reference/vscode/extensions/git/src/commands.ts` (`viewChanges`, `viewStagedChanges`, `viewUntrackedChanges`, `viewCommit`).
**Tasks:** `Documents().OpenMultiDiff(MultiDiffSpec)` (API, host, `fakeapi`); `internal/gui/multidiff`: an editor kind of one scrolling column of file sections (a header: collapse chevron, file icon, name, folder, the status letter, the per-file actions from the resource's menu; the body: the diff editor of §9.11 sized to its content with its own scrolling off, laid out lazily — only the sections near the viewport are built — with the line numbers and the inline or side-by-side choice of the diff editor's settings), a collapse-all/expand-all toolbar, the file count in the tab title, keyboard navigation between files and changes, restore of the tab (the sources are re-asked from the provider), memory bounded for 2,000 files; the commands `git.viewChanges`, `git.viewStagedChanges`, `git.viewUntrackedChanges`, `git.viewCommit` (the commit's changes against its first parent; a root commit against the empty tree; merge commits against the first parent) and `git.openAllChanges` now opens the multi-file editor instead of many tabs; `Repo.CommitChanges(id)`.
**Tests:** the layout model (heights from the diff's rows, collapsed sections, lazy building with the viewport at the top, middle and end, scrolling to a file, 2,000 files in a bounded number of built widgets); goldens dark and light (several files, one collapsed, an added file, a deleted file, a binary file's notice, an image-free commit); the commit changes with real git (a normal, a root, a merge commit, a rename, a mode change); the per-file stage action; restoring a tab; a display test with real scrolling.
**Proof:** method B on `history`: `scripts/proof.sh docs/implemented-proof-images/24.17-multi-diff.png --commands '[["git.viewCommit", {"id": "HEAD"}], ["sleep", "1s"]]' -- <dir>`; the image shows the editor with the headers and diffs of three files of one commit.

### 24.18 Stash
**Goal:** put changes aside and bring them back (SPEC §10.30 "Stash").
**Read:** SPEC §10.30; `reference/vscode/extensions/git/src/commands.ts` (`stash`, `stashStaged`, `stashIncludeUntracked`, `stashPop`, `stashApply`, `stashDrop`, `stashDropAll`, `stashView`, `stashPopEditor`...), `repository.ts` (`createStash`, `popStash`, `applyStash`, `dropStash`, `getStashes`), `fileSystemProvider.ts`, `package.json` (`git.stash*`, `editor/title`).
**Tasks:** `Repo` methods `Stash`, `StashPop`, `StashApply`, `StashDrop`, `Stashes`, `StashChanges(index)`; the commands of the SPEC with their pickers (message input, the list of stashes with dates), `git.promptToSaveFilesBeforeStash`, `git.useCommitInputAsStashMessage`, `git.stashStaged` needing git 2.35 (the message), conflicts on pop/apply (`StashConflict`: the stash is kept, the merge group shows), `git.stashView` opening the multi-file diff of the stash (`git-stash` content including untracked files), the editor title actions on that tab (`git.stashApplyEditor`, `stashPopEditor`, `stashDropEditor`), confirmation of `git.stashDropAll`.
**Tests:** real git: stash with and without untracked, staged only (skipped with a message below 2.35), keeping the index, with a message; pop/apply of a stash onto a changed tree with and without conflict; drop and drop all with their confirmations; the pickers; view of a stash with untracked files; unsaved documents prompt in the three settings; goldens of the stash picker and the stash tab (dark and light).
**Proof:** method B on `small`: `scripts/proof.sh docs/implemented-proof-images/24.18-stash.png --commands '[["git.stash", {"message": "WIP loader cache"}], ["git.stashView"], ["sleep", "1s"]]' -- <dir>`; the image shows the stash's changes in the multi-file diff tab with Apply, Pop and Drop in its title.

### 24.19 Worktrees
**Goal:** create, open, delete and detect linked worktrees, and move changes between them (SPEC §10.30 "Worktrees").
**Read:** SPEC §10.30 (Worktrees, Repositories, Trusting a repository), §10.14 (workspace tabs); `reference/vscode/extensions/git/src/commands.ts` (`createWorktree`, `_createWorktree`, `getWorktreeCommitishAndBranch`, `getWorktreePath`, `deleteWorktree`, `deleteWorktreeFromPalette`, `openWorktreeInCurrentWindow`, `openWorktreeInNewWindow`, `migrateWorktreeChanges`, `openWorktreeMergeEditor`, `compareWithWorkspace`, `handleWorktreeAlreadyExists`, `handleWorktreeBranchAlreadyUsed`), `repository.ts` (`createWorktree`, `_setupWorktree`, `_symlinkWorktreeFolders`, `_copyWorktreeIncludeFiles`, `getWorktrees`, `getWorktreeDetails`, `deleteWorktree`, `migrateChanges`, `kind`), `model.ts` (`checkForWorktrees`, `detectWorktrees`), `worktreeInclude.ts`, `worktreeSymlink.ts`, `test/worktreeInclude.test.ts`, `test/worktreeSymlink.test.ts`, `package.json` (`git.worktrees` submenu, `scm/sourceControl`, `git.detectWorktrees*`, `git.worktree*`).
**Tasks:** `Repo` methods `Worktrees` (`git worktree list --porcelain`), `AddWorktree`, `RemoveWorktree`, `MigrateChanges` (through the stash of 24.18); the repository kind `worktree` and its Codicon, `scmProviderContext` (`repository`, `submodule`, `worktree`), detection by `git.detectWorktrees` and its limit with the warning; the commands `git.createWorktree`, `git.deleteWorktree`, `git.deleteWorktree2`, `git.openWorktree` and `git.openWorktreeInNewWindow` (the first replaces the active tab's folder, the second opens a new workspace tab: `Workspace().OpenFolder(path, newTab)` from 24.16), `git.migrateWorktreeChanges`, `git.openWorktreeMergeEditor`, `git.compareWithWorkspace`, the Worktrees submenu of the `...` menus and the `scm/sourceControl` items with VS Code's `when`; the pickers and messages of the SPEC (branch already used, path already exists, no worktrees, delete with changes and force, the worktree you are in); the remembered worktree root per repository in Emetos's state; `worktreeInclude.go` and `worktreeSymlink.go` ported (`sanitizeWorktreeIncludePatterns`, `resolveWorktreeIncludePaths`, `getWorktreeSymlinkFolderCandidates`, `filterWorktreeSymlinkFolders`, `createWorktreeSymlink`) with `git.worktreeIncludeFiles` and `git.worktreeSymlinkFolders`, run on a goroutine after creation with the notification of the counts; the settings `git.detectWorktrees` (false), `git.detectWorktreesLimit` (50), `git.worktreeIncludeFiles`, `git.worktreeSymlinkFolders`.
**Tests:** real git (`worktree add`, `list`, `remove`): create from a branch, a tag, HEAD and a new branch, a branch already checked out here and in another worktree (the two messages and their buttons), an existing path (the `-1` suffix), a path outside the parent, a prefix removed from the name; delete clean, with changes (force asked), the current one (refused), a locked one; detection under both settings and the limit with its warning, a worktree as the folder of the tab; the include and symlink logic (ports of `worktreeInclude.test.ts` and `worktreeSymlink.test.ts`: collapsed ignored directories, never over a tracked path, patterns with line breaks dropped, a source that contains the worktree, an existing target, a failing symlink counted); migration (a changed and an untracked file moved, overlapping files refused with the list, a conflict leading to the merge group and the "Worktree" side, the stash named as VS Code names it); Compare with Workspace; open in the current tab and in a new tab (`fakeapi` records the call, and a display test with the real window tabs); a worktree needs no second trust question; goldens of the pickers and the Repositories view with a worktree row (dark and light).
**Proof:** method B on `history` with a worktree made by the fixture script (`scripts/git-fixture.sh history <dir>` adds `<dir>.worktrees/feature`): `scripts/proof.sh docs/implemented-proof-images/24.19-worktrees.png --commands '[["workbench.view.scm"], ["git.createWorktree"], ["sleep", "1s"]]' -- <dir>` with `EMETOS_DEV_GIT_TRUST=<dir>:<dir>.worktrees`; the image shows the Repositories view with the main repository and a worktree row (the worktree icon) and the Create Worktree picker over the editor.

### 24.20 The Source Control Graph
**Goal:** the history of the repository as a graph, with refs, incoming and outgoing changes, and actions (SPEC §10.30 "The Source Control Graph").
**Read:** SPEC §10.30, §10.2; `reference/vscode/src/vs/workbench/contrib/scm/browser/scmHistory.ts`, `scmHistoryViewPane.ts`, `contrib/scm/common/history.ts`, `contrib/scm/test/browser/scmHistory.test.ts`, `reference/vscode/extensions/git/src/historyProvider.ts`, `package.json` (`scm/history/title`, `scm/historyItem/context`, `scm/historyItemRef/context`, `git.graph.*`).
**Tasks:** `extension/history.go` (`HistoryProvider`, `HistoryItem`, `HistoryItemRef`, `HistoryOptions`, `SourceControl.SetHistoryProvider`), host binding, `fakeapi`; `internal/core/scm/graph` (the lane assignment and colors of `scmHistory.ts`, page by page: `Layout(page, previousState)` keeps the earlier pages' lanes, no recomputation); `internal/gui/views/scmgraph`: the view `workbench.scm.history` ("Graph", shown with a history provider) with the rows (the graph column drawn with Glutter's canvas: circles, 2 px lines, merge curves; subject, author, date as "N days ago", ref badges by `scm.graph.badges`), the filter of the title (Auto, All, a pick of refs; the repository selector for several), the incoming and outgoing entries, paging by `scm.graph.pageSize` and `pageOnScroll`, the hover with details and statistics, the menus (`MenuSCMHistoryTitle`, `MenuSCMHistoryItemContext`, `MenuSCMHistoryItemRefContext`), click opening the commit's changes (24.17), the search box (message, author, hash), the colors `scmGraph.*`; `extensions/gitscm/history.go`: the provider (`git log` in pages with `--topo-order --decorate=full -z`, the refs with their categories, the current, remote and base refs (the merge base with the upstream), the statistics with `--shortstat`, the common ancestor), the commands `git.graph.checkout`, `.checkoutDetached`, `.deleteBranch`, `.deleteTag`, `.cherryPick`, `.compareRef`, `.compareWithRemote`, `.compareWithMergeBase`, `git.copyCommitId`, `git.copyCommitMessage`, `git.fetchRef`, `git.pullRef`, `git.pushRef`, `git.fetchAll`.
**Tests:** the layout (a port of `scmHistory.test.ts`: linear, merge, octopus, fork and join, parallel branches, colors, page boundaries, equal timestamps); the provider against real repositories (branching and merging histories made by `gittest`: refs, parents, the merge base, statistics, a page boundary inside a merge, `--grep` and `--author` filters, a repository with no commits); the view (goldens dark and light: a branching history with badges, the incoming and outgoing rows, the hover, the filter states); the actions run the right git commands; 300 and 100,000 commits (the performance row of §14: the first page within 300 ms, scrolling and paging smooth).
**Proof:** method B on `history`: `scripts/proof.sh docs/implemented-proof-images/24.20-graph.png --commands '[["workbench.view.scm"], ["sleep", "1s"]]' -- <dir>`; the image shows the Graph view with colored lanes, merges, branch, remote and tag badges and the incoming/outgoing rows.

### 24.21 The Timeline view
**Goal:** the history of the file in front (SPEC §10.30 "The Timeline view").
**Read:** SPEC §10.30, §10.5, §10.4; `reference/vscode/src/vs/workbench/contrib/timeline/browser/timelinePane.ts`, `timeline.contribution.ts`, `common/timeline.ts`, `timelineService.ts`, `test/`, `reference/vscode/extensions/git/src/timelineProvider.ts`, `test/timelineProvider.test.ts`, `package.json` (`timeline/item/context`, `git.timeline.*`).
**Tasks:** `extension/timeline.go` (`api.Timeline()`, `TimelineProvider`, `TimelineItem`, paging), host binding, `fakeapi`; `internal/gui/views/timeline`: the view `timeline` in the Explorer container (folded by default), following the active editor or pinned, the list with the label, description and relative date, paging (`timeline.pageSize` 50, `timeline.pageOnScroll` true), the loading and empty messages of VS Code, the item menu (`MenuTimelineItemContext`), the title actions (`timeline.refresh`, `timeline.toggleFollowActiveEditor`) and the source filter, `files.openTimeline`; `extensions/gitscm/timeline.go`: the provider (`git log --follow` pages for the file, `git.timeline.showUncommitted`, `git.timeline.showAuthor`, `git.timeline.date`), `git.timeline.openDiff` (the file at the commit against its parent), `git.timeline.viewCommit`, `selectForCompare`, `compareWithSelected` (the two versions of the file, title `a.go (abc1234) ↔ a.go (def5678)`), `copyCommitId`, `copyCommitMessage`, the context key `git.timeline.selectedForCompare`.
**Tests:** the provider against real repositories (renames followed, paging across the boundary, uncommitted and staged items, a file not tracked → no items, a merge commit, authored versus committed dates); the view (goldens dark and light: items, loading, empty, the pinned state; paging); the compare flow; follows the active editor and is pinned; refresh after a commit; the settings.
**Proof:** method B on `history`: `scripts/proof.sh docs/implemented-proof-images/24.21-timeline.png --commands '[["workbench.view.explorer"], ["dev.openFile", {"path": "src/loader.go"}], ["timeline.focus"], ["sleep", "1s"]]' -- <dir>`; the image shows the Explorer with the Timeline view open, listing the commits of the file.

### 24.22 Blame
**Goal:** who changed this line, in the editor and the status bar (SPEC §10.30 "Blame").
**Read:** SPEC §10.30, §9.12 (hovers), §9.4; `reference/vscode/extensions/git/src/blame.ts`, `hover.ts`, `package.json` (`git.blame.*`).
**Tasks:** `extension/editordecorations.go` (`api.EditorDecorations()`: `Set(key, uri, []LineAnnotation)`, `Clear`), the editor's after-line text layer (muted, theme color, one per annotation, does not shift the text, clipped at the viewport), hovers with `command:` links to the extension's own commands (§9.12's hover); `extensions/gitscm/blame.go`: `git blame --root --incremental` for the saved file (with `-w` by `git.blame.ignoreWhitespace`), edits mapped through the document's changes (lines changed since the save are "Uncommitted changes" with the user's name from `user.name`), a cache by file and HEAD, started 150 ms after the cursor stops and cancelled by the next move, the templates of `git.blame.editorDecoration.template` and `git.blame.statusBarItem.template` (`hash`, `hashShort`, `subject`, `authorName`, `authorEmail`, `authorDate`, `authorDateAgo`; unknown variables kept as written), the hover (commit details, **Open Commit** and **Copy Hash** links, `git.blame.editorDecoration.disableHover`), the status bar item and its click, `git.blame.toggleEditorDecoration`, `git.blame.toggleStatusBarItem`, the color `git.blame.editorDecorationForeground`; skipped for untracked, ignored and over-2-MiB files.
**Tests:** the blame parser and the mapping through edits (seeded random edits check that unchanged lines keep their commits); the templates (a table, with unknown variables, long subjects, non-ASCII); real git blame on a history with renames, a boundary commit, a file with uncommitted lines; the decoration layer (goldens dark and light: the text after a line, long lines, the hover with the links); the links run the commands and nothing else (a link to a command of another extension is ignored); toggles; cancellation (a fast sequence of cursor moves starts one blame); idle (nothing runs without a cursor move).
**Proof:** method B on `history`: `scripts/proof.sh docs/implemented-proof-images/24.22-blame.png --commands '[["git.blame.toggleEditorDecoration"], ["dev.openFile", {"path": "src/loader.go", "line": 12}], ["sleep", "1s"]]' -- <dir>`; the image shows the line with its muted annotation and the status bar item with the author.

### 24.23 Terminal authentication, cross-checks and the desktop pass
**Goal:** git typed in the terminal asks in Emetos too; the milestone's loose ends are closed (SPEC §10.30 "Passwords and editors" Terminals, §14, §15).
**Read:** SPEC §10.30, §10.25 (the Terminal extension), §10.18 (the headless view), §14, `docs/PERFORMANCE.md`; `reference/vscode/extensions/git/src/terminal.ts`, `askpass.ts` (`getTerminalEnv`).
**Tasks:** `TerminalEnvironment` (API: `Contribute(env, description)` with disposal; `fakeapi`; the Terminal extension reads it when it starts a shell and shows the contribution in its "environment changes" description as VS Code does), `git.terminalAuthentication` (true) and `git.terminalGitEditor` (false), `scm.openInIntegratedTerminal` and `scm.openInTerminal`; the Source Control view, the Graph, the Timeline and the quick diff in a window tab seen through Remote Work's headless view (§10.18) work, and **every Git command is available to a Remote Work client** with the host's credentials, keys and agent, as if at the host's keyboard, with no refusal (D38, the user's choice of 2026-10-30): a test drives fetch, push, sync and clone through the remote view against `HTTPBackend` and a local bare remote, the credential prompt appearing in the view (no change of the host expected beyond the test; if a general refusal list for remote clients exists by then, Git is not on it); the measurements of SPEC §14 for Git (`scripts/perf-git.sh` on the `many` fixture, the Graph on a generated 100,000-commit repository, the quick diff on a 10,000-line file, the idle check) recorded in `docs/PERFORMANCE.md` with the numbers and the machine; `docs/MANUAL-CHECKS.md` entries for what only the user's setup shows (a push over SSH with a passphrase-protected key and the user's agent to a remote of the user's choice; an HTTPS fetch that needs the user's credential helper; the whole milestone's screenshots on the real GPU with `scripts/realinput.sh --desktop`); the coverage table of the SPEC for area 5; `docs/ERROR-TAGS-UNTESTED.md` checked (`scripts/error-tags.sh --untested` prints nothing).
**Tests:** the terminal gets the variables (through `fakeapi` and the real Terminal extension in a display test: `git push` typed in a terminal against `HTTPBackend` raises the prompt in the window, the answer reaches the terminal's git; disabled by the setting); the editor variable only with `git.terminalGitEditor`; removal when the Git extension is turned off; the Remote Work view test; a full-milestone end-to-end display test (open the `small` fixture, edit a file, see the mark, stage it, commit with the input, see the Graph and the Timeline update, blame the line) with real input.
**Proof:** method B: `scripts/proof.sh docs/implemented-proof-images/24.23-terminal-auth.png ...` with a terminal running `git push` against `dev.git.testserver`; the image shows the terminal waiting and the password prompt of the window.

### 24.25 Extensions and the terminal in the Remote Work view
**Goal:** a client of a hosted workspace sees and uses what the host's extensions give the workbench, Git first, with prompts in the client's own view, and the host's terminals (SPEC §10.18 "Not in the view", §10.25, D38, D50, D51; decision 864 of step 24.23).
**Read:** SPEC §10.18 (the view's workspace, `initView`, `newViewWorkspace`), §10.19 (`command.ClientRefusal`), §10.15 (scopes, contributions), §10.25 (the Terminal extension and its panel), §10.30 (the Source Control view, the Graph, the Timeline, "Passwords and editors"), D38, D50, D51; `internal/gui/app/headless.go`, `internal/gui/exthost` (how a scope's contributions reach a window), `extensions/terminal`, decision 864 and the 24.23 entry of `docs/IMPLEMENTED.md`.
**Tasks:**
1. The view's shell gets the Source Control, Graph and Timeline panes and the file decorations, fed by the same models as the host's window (no second Git extension: the host's scope serves both).
2. The extensions' commands are registered in the view's command registry as forwards to the parent's scope, shown in the view's palette and menus with their `when` clauses evaluated on the view's context keys, and still checked by `command.ClientRefusal`; the extensions' status bar items are in the view's status bar.
3. **Prompts routed to the asker:** a command run from a view carries the view as its origin, and the quick picks, input boxes, dialogs, progress and notifications the extension raises while handling it (including the password box of Git's helper for an operation the client started) are shown in that view and answered there; a prompt with no origin (autofetch, a background refresh) stays on the host's window; two clients at once each get their own prompts.
4. **The terminal (D51):** the view has the Terminal panel showing the host's terminals live (the same terminal sessions, one emulator state, each view drawing it at its own size: the size of the terminal follows the view that typed last, as a shared tmux session does), and the client may type, open, split, rename and close terminals; the class `terminal` is removed from `command.ClientRefusal` and the SPEC text and tests that named it are updated.
**Tests:** through Remote Work's headless view with the real Git extension against `HTTPBackend` and a local bare remote: the client sees the changes, stages, commits, fetches, pushes, syncs and clones from the view, the credential prompt appears in the client's view (not the host's) and its answer reaches git; the host's window gets no prompt; a refused command (a database or container command) is still refused in the view; two views at once; a prompt raised by autofetch stays on the host; the terminal: the view shows a host terminal's output, typing in the view reaches the shell and the host's panel shows it, a terminal opened in the view appears on the host, resizing follows the last typist; goldens of the view with the Source Control view, the Graph and the Terminal panel; a display test with real input in the view (type in the terminal, push from the Source Control view).
**Proof:** method B: `scripts/proof.sh docs/implemented-proof-images/24.25-remote-view.png ...` with a dev command that opens a headless view of the fixture in a window tab (as step 15.x's proofs do); the image shows the client's view with the Source Control view, a terminal running `git push` and the password box in the view.

### 24.24 Update the README
**Goal:** `README.md` describes Git and Source Control.
**Read:** `README.md`, `docs/IMPLEMENTED.md` (the entries of M24), SPEC §10.30, §12, D33–D37.
**Tasks:** the status (M24 built), the features (the Source Control view, diffs, the quick diff, branches and tags, remotes, stash, the Graph, the Timeline, blame, file decorations), a section "Git" with how to use it and the settings that matter (`git.autofetch`, `git.enableSmartCommit`, `git.blame.*`, `git.path`), the requirements (`git` 2.30 or newer; nothing is downloaded), the security notes (hooks and configuration of a repository run programs when you commit; Emetos stores no credential, git's credential helpers and the SSH agent do; the helper program; the clean filters of `git status`; the `safe.directory` rule), privacy (only the remotes git contacts; autofetch off until you answer), what is not built (GitHub features, worktrees if still open, `scm.repositories.explorer`), where things are (`extensions/gitscm`, the Git output channel), fresh screenshots with `scripts/screenshots.sh` from the fixtures (the Source Control view with changes and the commit input, the diff with the quick diff marks and the peek, the Graph, the Timeline and blame), the coverage table rows for area 5, a line in the Terminal section that git asks for passwords in terminals too; every link resolves (`TestREADMELinksResolve`).
**Tests:** `TestREADMELinksResolve`.
**Proof:** the README's screenshots, viewed.

## M24b: Language servers of more languages

Requested by the user on 2026-11-01: what the Go extension has with `gopls`, for Rust, C/C++, Python, TypeScript/JavaScript, Java and C# (SPEC §10.34, D47–D49). F# (step 24b.8) and Kotlin (step 24b.9) were planned and dropped by the user on 2026-11-01 (SPEC D47); their step numbers are not reused. It runs **before M25**, because the debuggers and test providers of M25 and M26 build on these servers and on the Python interpreter and TypeScript version they choose. No new Go module; the LSP client and its UI are M12's. Emetos never installs a server (D47). **Tests of the milestone:** fake servers by default; the real servers under their own tags (SPEC §10.34 "Tests"), with the tools installed only into `/var/tmp/emetos-tools/` at versions pinned in `scripts/tool-versions.txt` (rust-analyzer's release binary, basedpyright and pylsp in a venv, typescript-language-server with `npm install --prefix`, jdtls's milestone archive; clangd and Java are the system's); csharp-ls is installed with `dotnet tool install --tool-path /var/tmp/emetos-tools/dotnet-tools` on the system's .NET SDK (10.0, installed by the user on 2026-11-01).

**Order.** 24b.1 first; 24b.2 to 24b.7 one after the other (they share `extensions/internal/langserver` and the asset script); 24b.10 last.

### 24b.1 What every pack shares
**Goal:** finding, starting and showing a language server the same way in every pack (SPEC §10.34 "What every pack gets").
**Read:** SPEC §10.34, §9.12, §10.17 (Go's status item and tools), `extensions/golang/status.go`, `tools.go`, `extensions/internal/langpack`.
**Tasks:** `extensions/internal/langserver` (the finder with the setting, `PATH` and the extra places, version checks, roots per project marker, settings as `initializationOptions` and `workspace/configuration` with restart on change, the status bar item and its menu, the missing-server notification with How to Install, Copy Install Command and Set Path..., the output channel and `trace.server`); `langpack` options to use it.
**Tests:** the finder (a table of settings, `PATH` and folders), roots (a table of trees), the notification once per tab, restart on a settings change, the status item's states, with a fake server over the fake API.
**Proof:** none beyond the tests (the packs show it); recorded in `IMPLEMENTED.md`.

### 24b.2 Rust: rust-analyzer
**Goal:** rust-analyzer in the Rust pack (SPEC §10.34 Rust).
**Read:** SPEC §10.34; rust-analyzer's manual and its VS Code extension's `package.json` (online).
**Tasks:** the server contribution, settings, the commands of §10.34 (Expand Macro and the syntax tree in read-only tabs), the run lenses through the Tasks extension, `experimental/serverStatus`.
**Tests:** over the fake API with a fake server; under `emetos_rustanalyzer`: a `testdata` crate (diagnostics from `cargo check`, completion, hover, definition, rename, Expand Macro).
**Proof:** method B on a Rust fixture: `scripts/proof.sh docs/implemented-proof-images/24b.2-rust.png --commands '[["dev.openFile", {"path": "src/main.rs", "line": 8}], ["editor.action.triggerSuggest"], ["sleep", "3s"]]' -- <dir>`; the image shows completion with types and the status item.

### 24b.3 C and C++: clangd
**Goal:** clangd in the C/C++ pack (SPEC §10.34 C and C++).
**Read:** SPEC §10.34; clangd's documentation and vscode-clangd's `package.json` (online).
**Tasks:** the server contribution, `compile_commands.json` detection and the `.clangd` stub offer, the commands (Switch Header/Source on Alt+O, the AST tab, inlay hints toggle), the file status.
**Tests:** fake server; under `emetos_clangd` (the system's clangd): a `testdata` CMake project with `compile_commands.json` (diagnostics, completion, switch header/source, rename).
**Proof:** method B on a C++ fixture: `scripts/proof.sh docs/implemented-proof-images/24b.3-cpp.png ...`; the image shows a diagnostic and inlay hints.

### 24b.4 Python: basedpyright, pyright or pylsp, and the interpreter
**Goal:** a Python server and the interpreter picker (SPEC §10.34 Python).
**Read:** SPEC §10.34, D49; basedpyright's and pyright's settings, python-lsp-server's README, VS Code Python's interpreter picker (`src/client/interpreter/`, online).
**Tasks:** `python.languageServer` and the three servers, the interpreter discovery and "Python: Select Interpreter" with the status item and the per-folder choice in the state, the settings, Organize Imports.
**Tests:** interpreter discovery (a table of folders, environment variables and fake `pyenv`), each server kind with a fake server; under `emetos_pyright` and `emetos_pylsp` (the venv): a `testdata` project with a `.venv` (diagnostics, completion from an installed package, hover, rename).
**Proof:** method B on a Python fixture: `scripts/proof.sh docs/implemented-proof-images/24b.4-python.png --commands '[["python.setInterpreter"], ["sleep", "2s"]]' -- <dir>`; the image shows the interpreter picker over an editor with diagnostics.

### 24b.5 TypeScript and JavaScript: typescript-language-server
**Goal:** a TypeScript server for both packs (SPEC §10.34 TypeScript and JavaScript).
**Read:** SPEC §10.34; typescript-language-server's README (its settings and commands), VS Code's `extensions/typescript-language-features/package.json` (in the pinned reference) for the setting and command names.
**Tasks:** the server contribution shared by the two packs, the TypeScript version (workspace and bundled) with its picker and status item, the preferences, the commands of §10.34, `tsconfig.json`/`jsconfig.json` projects.
**Tests:** fake server; under `emetos_tsls` (with `npm install --prefix /var/tmp/emetos-tools/node`): a `testdata` project with its own `node_modules/typescript` (diagnostics, completion, Organize Imports, Go to Source Definition, rename across files).
**Proof:** method B on a TypeScript fixture: `scripts/proof.sh docs/implemented-proof-images/24b.5-typescript.png ...`.

### 24b.6 Java: the pack and jdtls
**Goal:** Java with Eclipse JDT LS (SPEC §10.34 Java).
**Read:** SPEC §10.34, §8.1; eclipse-jdtls's README (how to start it), vscode-java's `package.json` and `src/javaServerStarter.ts` (online); VS Code's `extensions/java` (pinned reference).
**Tasks:** the `java` language pack (grammar, configuration, licenses, `scripts/update-assets.sh`, §8.1 updated), the jdtls start (launcher jar, `config_linux`, `-data` in the cache, the Java check), project import and its question, `jdt://` documents, the commands, the status from `language/status`.
**Tests:** the start command line (a table of jdtls folders and Java homes), fake server; under `emetos_jdtls` (jdtls in `/var/tmp/emetos-tools/`, the system's Java): a Maven and a Gradle `testdata` project (diagnostics, completion, definition into a JDK class, Organize Imports).
**Proof:** method B on a Java fixture: `scripts/proof.sh docs/implemented-proof-images/24b.6-java.png ...`.

### 24b.7 C#: the pack and csharp-ls
**Goal:** C# with csharp-ls (SPEC §10.34 C#).
**Read:** SPEC §10.34, D49; csharp-ls's README (online); VS Code's `extensions/csharp` (pinned reference).
**Tasks:** the `csharp` language pack, the server contribution, the solution picker, metadata documents, the commands.
**Tests:** fake server; under `emetos_csharpls` (csharp-ls from the milestone's tool path): a `testdata` solution.
**Proof:** method B on a C# fixture: `scripts/proof.sh docs/implemented-proof-images/24b.7-csharp.png ...`; the image shows completion from csharp-ls.

### 24b.10 The desktop pass and the README
**Goal:** the milestone's loose ends and the README (SPEC §10.34).
**Read:** SPEC §10.34, §14, `README.md`, the M24b entries of `docs/IMPLEMENTED.md`.
**Tasks:** the coverage table of the SPEC for area 3; measurements (each real server's time to the first diagnostics on its `testdata` project; typing latency with each server running) in `docs/PERFORMANCE.md`; `docs/MANUAL-CHECKS.md` entries (the real GPU pass); the README's "Languages" section (a table of each language, its server and how to install it, the settings that matter); `scripts/error-tags.sh --untested` prints nothing.
**Tests:** `TestREADMELinksResolve`.
**Proof:** the README's screenshots, viewed.

## M25: Run and Debug

Requested by the user on 2026-11-01 (from a list of what VS Code users would miss: "I would like 1 and 2"), with debuggers for Go, Python, C/C++/Rust and JavaScript/TypeScript (SPEC §10.31, D39–D41, D43, D46). It is VS Code's debug workbench (MIT, `reference/vscode/src/vs/workbench/contrib/debug/`) over the Debug Adapter Protocol; code ported from it says so with a `// Ported from VS Code: <path>` line. No new Go module. Every step that needs a capability the extension contract lacks adds it to `extension/`, `internal/gui/exthost` and `extensions/internal/fakeapi` together and names it in its `IMPLEMENTED.md` entry.

**Tests of the milestone.** By default everything is tested against `internal/core/dap/daptest`, a scripted fake adapter (step 25.1). The real debuggers are tested under build tags of their own (D43) and skipped when missing. **The tools are never installed into the user's environment:** a step that needs one for its tagged tests or its proof builds or installs it into `/var/tmp/emetos-tools/` with a pinned version (`GOBIN=/var/tmp/emetos-tools/bin go install github.com/go-delve/delve/cmd/dlv@<pinned>`; `python3 -m venv /var/tmp/emetos-tools/venv` and `pip install debugpy==<pinned> pytest==<pinned>` into it; js-debug's pinned release unpacked into `/var/tmp/emetos-tools/js-debug`), records the versions in `scripts/tool-versions.txt`, and points the fixture's settings at them (`go.alternateTools`, `python.defaultInterpreterPath`, `debug.javascript.adapterPath`). gdb 14 or newer and `lldb-dap` are the system's (the user installed `lldb` on 2026-11-01). Programs started by tests and proofs run under `timeout -k` in their own process group and are gone when the step ends (`pgrep -a`).

**Order.** 25.1 to 25.6 in order (each builds on the one before); 25.7 to 25.10 (the four debuggers) after 25.6, one after the other because they share the contract; 25.11 after 25.7; 25.12 and 25.13 last.

### 25.1 The DAP client and the fake adapter
**Goal:** speak DAP to a debug adapter process or server, and have a scriptable fake adapter for every later test (SPEC §10.31 "The session", "Tests").
**Read:** SPEC §10.31, D39, D43, §13, §15.1; the DAP specification (online, microsoft.github.io/debug-adapter-protocol); `reference/vscode/src/vs/workbench/contrib/debug/common/debugProtocol.d.ts`, `debugger.ts`, `abstractDebugAdapter.ts`, `node/debugAdapter.ts` (`StreamDebugAdapter`, `SocketDebugAdapter`, `ExecutableDebugAdapter`).
**Tasks:** `internal/core/dap`: the protocol types ported from `debugProtocol.d.ts` (every request, response and event of DAP 1.65 that §10.31 uses; the rest as raw JSON); the framing (`Content-Length` headers, partial reads, a 64 MiB message limit with the tag `DapMessageTooLarge`); `Client` with `Request(ctx, command, args) (Response, error)` (sequence numbers, a cancelled ctx sends `cancel` when the adapter supports it and stops waiting), the event stream, reverse requests answered through a handler (`runInTerminal`, `startDebugging`), `Close`; the transports: a process (stdin/stdout, stderr to the log, its own process group, SIGTERM then SIGKILL after 3 s), TCP on 127.0.0.1, a Unix socket, and "start a program, read the port it prints, connect"; failures each with their own tag (`DapAdapterStartFailed`, `DapAdapterExited`, `DapResponseFailed` with the adapter's message, `DapTimeout`, `DapProtocolInvalid`). `internal/core/dap/daptest`: a Go program built by `TestMain` that reads a script (JSON lines: expect a request, answer it, send events, delay, crash, print garbage) so a test describes a whole session.
**Tests:** framing (split headers, several messages in one read, a message across reads, garbage, the limit); the client with daptest over each transport (requests and responses in any order, events between them, reverse requests, cancel, the adapter exiting mid-request, a slow answer with a timeout); every tag's route; a 10,000-event burst is read without growing memory (bounded queue, back-pressure). No UI.
**Proof:** none (no UI); the test output in the `IMPLEMENTED.md` entry.

### 25.2 The debug model and launch configurations
**Goal:** sessions, threads, frames, variables and breakpoints as data, and `launch.json` read and resolved (SPEC §10.31 "Launch configurations", "The session", "State").
**Read:** SPEC §10.31, D41, §10.10, §11.1; `reference/vscode/src/vs/workbench/contrib/debug/common/debugModel.ts`, `debugSession.ts` (in `browser/`), `breakpoints`, `debugStorage.ts`, `browser/debugConfigurationManager.ts`, `common/debugSchemas.ts`, `services/configurationResolver/` (the variables).
**Tasks:** `internal/core/debug`: the model (sessions and their states, threads, stack frames loaded on demand, scopes, variables loaded lazily and paged, watch expressions, the REPL history, breakpoints of the kinds line (condition, hit condition, log message, triggered by), inline (column), function and exception filters; their verification and the adapter's moves; breakpoints follow the edits of their document through `core/decorations`); the session driver (VS Code's sequence of §10.31 over `core/dap`, restart, stop, disconnect, several sessions, child sessions, compounds); `launch.json` (`<folder>/.emetos/launch.json` and the `launch` setting, JSONC, `version`, `configurations`, `compounds`, `inputs`), the variables of §10.31 (`${input:*}` and `${command:*}` through callbacks the window gives), dynamic configurations through callbacks; the state of §10.10 (breakpoints, watches, exception choices, the selected configuration, activation) saved and restored.
**Tests:** a table of launch.json files (every field, comments, errors with their positions, compounds, inputs) and of every variable; the driver against daptest scripts (the sequence, breakpoints set before `configurationDone`, verification and moves, stop and continue, threads, a child session, restart, stop of an attach, an adapter that dies); breakpoints following edits (seeded random edits); state round trip.
**Proof:** none (no UI).

### 25.3 The debug API and the debuggers' contributions
**Goal:** extensions contribute debuggers and drive sessions (SPEC §10.31 "The extension contract").
**Read:** SPEC §10.31, §10.15; `reference/vscode/src/vscode-dts/vscode.d.ts` (the `debug` namespace, `DebugAdapterDescriptorFactory`, `DebugConfigurationProvider`, `DebugAdapterTracker`), `reference/vscode/src/vs/workbench/api/common/extHostDebugService.ts`, `contrib/debug/common/debugger.ts` (the `contributes.debuggers` schema).
**Tasks:** `extension/debug.go` (`api.Debug()` and the types of §10.31, `Contributes.Debuggers`, `Contributes.Breakpoints`, checked by `CheckManifest`); the host (`internal/gui/exthost`): adapter descriptors started per session, configuration providers called in VS Code's order, trackers, custom requests and events, `StartDebugging`, sessions ended when the extension is turned off; `launch.json` checked against the debuggers' attribute schemas into Problems (owner `launch.json`), and completion of `type`, `request` and attributes in `launch.json` (the JSON language's completion from the schemas, as settings.json has); `fakeapi`; debugging from a Remote Work client (D46): the debug commands are not refused, and the client's view (step 24.25) carries the Run and Debug view, the toolbar, the Debug Console and the editor's breakpoints and debug decorations.
**Tests:** the manifest rules; each API call through the host with daptest as a contributed adapter (executable, server, executable-server); the provider order and substitution; trackers see every message; turning the extension off ends its sessions; the Problems of a bad launch.json; the refusal (and its test that every refused id exists).
**Proof:** none (no UI).

### 25.4 The Run and Debug view, the toolbar and the status bar
**Goal:** start, watch and drive sessions from the workbench (SPEC §10.31 "The Run and Debug view", "The debug toolbar and the status bar", "Commands and keys", "Settings").
**Read:** SPEC §10.31, §10.4, §10.1; `reference/vscode/src/vs/workbench/contrib/debug/browser/debugViewlet.ts`, `welcomeView.ts`, `variablesView.ts`, `watchExpressionsView.ts`, `callStackView.ts`, `breakpointsView.ts`, `loadedScriptsView.ts`, `debugToolBar.ts`, `debugStatus.ts`, `statusbarColorProvider.ts`, `debug.contribution.ts`, `debugCommands.ts`, `debugActionViewItems.ts`.
**Tasks:** `internal/gui/views/debug`: the view container and its five views with their menus, the welcome, the configuration dropdown and start button, inline value editing (Set Value), Add to Watch, Copy Value, Copy as Expression, Copy Call Stack, Restart Frame, the Breakpoints view with exception filters and function breakpoints; the toolbar in its three locations; the status bar color and the launch item; the commands, keys, context keys and the Run menu; the settings of §10.31 that these parts read.
**Tests:** goldens (dark, light) of the view before a session (welcome, with configurations), stopped with variables expanded and changed values, the call stack with several threads and a child session, the breakpoints view, the toolbar floating and docked, the status bar while debugging; widget tests of every command and key against daptest sessions; the view states follow the model.
**Proof:** method B with daptest as the adapter of a fixture's `launch.json` (a dev debugger contributed only in debug builds, `dev.debug.fake`, SPEC §15.2): `scripts/proof.sh docs/implemented-proof-images/25.4-run-and-debug.png --commands '[["workbench.view.debug"], ["workbench.action.debug.start"], ["sleep", "2s"]]' -- <dir>`; the image shows the view stopped at a breakpoint with variables, the call stack, the toolbar and the orange status bar.

### 25.5 Breakpoints and the debugger in the editor
**Goal:** set breakpoints and see where the program stopped, in the editor (SPEC §10.31 "The editor").
**Read:** SPEC §10.31, §9.1, §9.4; `reference/vscode/src/vs/workbench/contrib/debug/browser/breakpointEditorContribution.ts`, `breakpointWidget.ts`, `callStackEditorContribution.ts`, `debugEditorContribution.ts`, `debugHover.ts`, `exceptionWidget.ts`, `debugIcons.ts`, `common/debugUtils.ts`.
**Tasks:** `internal/gui/editor`: the breakpoint glyphs and their hover hint, the context menu, the breakpoint widget (a view zone with its kinds), inline breakpoints and their candidates (`debug.showInlineBreakpointCandidates`), the stopped and focused frame lines, the debug hover (evaluate, tree), inline values (`debug.inlineValues`), the exception widget, Run to Cursor, Jump to Cursor, the overview ruler marks; the colors and icons of §10.31.
**Tests:** goldens of each glyph kind (verified, unverified, disabled, conditional, log, function), the widget in each kind, the stopped and focused lines, the hover with a tree, inline values, the exception widget; widget tests of F9, Shift+F9, a click in the glyph margin, the widget's Enter and Escape, breakpoints moving with edits; a display test with real input (F9, F5 with daptest, the hover, F10).
**Proof:** method B with the dev fake debugger: `scripts/proof.sh docs/implemented-proof-images/25.5-editor.png --commands '[["dev.openFile", {"path": "src/loader.go", "line": 14}], ["editor.debug.action.toggleBreakpoint"], ["workbench.action.debug.start"], ["sleep", "2s"]]' -- <dir>`; the image shows the breakpoint, the stopped line, inline values and the exception widget or hover.

### 25.6 The Debug Console
**Goal:** read the program's output and evaluate in the stopped program (SPEC §10.31 "The Debug Console").
**Read:** SPEC §10.31, §10.13; `reference/vscode/src/vs/workbench/contrib/debug/browser/repl.ts`, `replViewer.ts`, `replFilter.ts`, `debugANSIHandling.ts`, `linkDetector.ts`, `common/replModel.ts`.
**Tasks:** the panel view with the output tree (categories and their colors, groups, object values, links, collapsed identical lines), the ANSI renderer (`debugANSIHandling.ts` ported; a reusable read-only ANSI text widget in `gui/kit` that the Testing views use later), the filter, the input with history, completions and multi-line, the `debug.console.*` settings and `debug.internalConsoleOptions`.
**Tests:** the ANSI parser (a table of SGR sequences, 256 and true colors, resets, links); goldens of the console with mixed output, groups, an object tree and the filter; widget tests of evaluate, history, completion (daptest answers `completions`), Clear Console; 100,000 lines of output stay within the frame budget (§14).
**Proof:** method B with the dev fake debugger: `scripts/proof.sh docs/implemented-proof-images/25.6-debug-console.png --commands '[["workbench.action.debug.start"], ["workbench.debug.action.focusRepl"], ["sleep", "2s"]]' -- <dir>`; the image shows colored output, a group, an object tree and an evaluated expression.

### 25.7 Go: dlv dap
**Goal:** debug Go programs and tests (SPEC §10.31 "The debuggers" Go, §10.17).
**Read:** SPEC §10.31, §10.17, D40; vscode-go's `package.json` (the `go` debugger's attributes), `src/goDebugConfiguration.ts`, `src/goDebugFactory.ts`, `docs/debugging.md` (online or `reference/` when `scripts/fetch-reference.sh` has it); Delve's `Documentation/api/dap/README.md` (online).
**Tasks:** `extensions/golang`: the `go` debugger contribution (attributes, initial configurations, snippets), the adapter descriptor (`dlv dap` as an executable-server), the configuration provider (defaults, `program` from the active file, `${command:pickProcess}` with a process picker, `envFile`), the "debug test" lens and `emetos.golang.debugTestAtCursor` (mode `test`, `-test.run`), the missing-`dlv` notification with Install; `scripts/tool-versions.txt` and the tool install of the milestone's rule for the tagged tests.
**Tests:** the provider and descriptor over the fake API (a table of configurations to the resolved ones and the dlv command lines); under `emetos_dlv`: a `testdata` program run to a breakpoint, variables, a goroutine list, step over, a test debugged from the lens, attach to a running process, the end.
**Proof:** method B on a Go fixture with the pinned dlv: `scripts/proof.sh docs/implemented-proof-images/25.7-go.png --commands '[["dev.openFile", {"path": "main.go", "line": 12}], ["editor.debug.action.toggleBreakpoint"], ["workbench.action.debug.start"], ["sleep", "4s"]]' -- <dir>`; the image shows a real Go program stopped with its variables.

### 25.8 Python: debugpy
**Goal:** debug Python programs and modules (SPEC §10.31 "The debuggers" Python).
**Read:** SPEC §10.31, D40; vscode-python-debugger's `package.json` and `src/extension/debugger/configuration/` (online); debugpy's documentation of its adapter (online).
**Tasks:** `extensions/python`: the `debugpy` debugger (and `python` as its old name), the adapter (`<interpreter> -m debugpy.adapter`), the configuration provider ("Python Debugger: Current File", module, attach), the interpreter setting, the missing-`debugpy` notification whose Install runs `pip` in a terminal.
**Tests:** the provider over the fake API; under `emetos_debugpy` (the milestone's venv): a `testdata` script to a breakpoint, variables, an exception stop with the exception widget's data, `justMyCode`, attach by port.
**Proof:** method B on a Python fixture with the venv's interpreter: `scripts/proof.sh docs/implemented-proof-images/25.8-python.png ...`; the image shows a Python program stopped at a breakpoint.

### 25.9 C, C++ and Rust: gdb and lldb-dap
**Goal:** debug native programs (SPEC §10.31 "The debuggers" C, C++ and Rust).
**Read:** SPEC §10.31, D40; gdb's manual chapter "Debugger Adapter Protocol" (online); LLVM's `lldb/tools/lldb-dap/README.md` and its `package.json` (online); rust-analyzer's debug configurations (online) for the Rust targets.
**Tasks:** `extensions/cpp` and `extensions/rust`: the `gdb` and `lldb-dap` debuggers (the gdb version check, 14 or newer, `GdbTooOld`), their attributes and configurations; the Rust provider (targets from `cargo metadata`, `cargo build` or `cargo test --no-run --message-format=json` before the launch, the executable it names); a missing debugger's notification.
**Tests:** providers over the fake API (a table of `cargo` outputs to configurations); under `emetos_gdb`: a C program and a Rust program built in the test (`cc`, `cargo`) run to a breakpoint, locals, step, the end; under `emetos_lldbdap` the same with the system's lldb-dap.
**Proof:** method B on a Rust fixture with gdb: `scripts/proof.sh docs/implemented-proof-images/25.9-rust.png ...`; the image shows a Rust program stopped with its locals.

### 25.10 JavaScript and TypeScript: js-debug
**Goal:** debug Node.js programs (SPEC §10.31 "The debuggers" JavaScript and TypeScript).
**Read:** SPEC §10.31, D40, D4; vscode-js-debug's `README.md`, `OPTIONS.md`, the `dapDebugServer` of its releases (online).
**Tasks:** `extensions/javascript` and `extensions/typescript`: the `pwa-node` and `node` debuggers, js-debug started as an executable-server with the user's `node`, child sessions through `startDebugging` (25.3), the configuration provider (the `scripts` of `package.json`, the current file), `debug.javascript.adapterPath`, "JavaScript Debugger: Download" (the pinned release, its SHA-256, unpacked into Emetos's data folder; only on the user's click; the tag `JsDebugChecksumMismatch`), the missing-`node` and missing-js-debug notifications.
**Tests:** the provider and the download (against a local `httptest` server serving a fake archive: checksum good and bad, a broken archive) over the fake API; under `emetos_jsdebug` (js-debug from `/var/tmp/emetos-tools/js-debug`): a Node script and a TypeScript file with a source map to a breakpoint, variables, a child process session.
**Proof:** method B on a Node fixture: `scripts/proof.sh docs/implemented-proof-images/25.10-node.png ...`; the image shows a TypeScript file stopped with its variables and the child session in the call stack.

### 25.11 Tasks before and after, and the rest of the settings
**Goal:** `preLaunchTask` and `postDebugTask`, and every remaining setting of §10.31 (SPEC §10.31 "Launch configurations", "Settings").
**Read:** SPEC §10.31, §10.15 (the Tasks extension), `extensions/tasks`; `reference/vscode/src/vs/workbench/contrib/debug/browser/debugTaskRunner.ts`.
**Tasks:** running tasks of `extensions/tasks` before and after a session with `debug.onTaskErrors`'s answers (a task-run request between extensions, added to the contract if missing), `debug.saveBeforeStart`, `debug.openDebug`, `debug.openExplorerOnEnd`, `debug.focusEditorOnBreak`, `debug.focusWindowOnBreak`, `debug.confirmOnExit` (the quit question of §10.3 while sessions run), `debug.hideLauncherWhileDebugging`, `debug.terminal.clearBeforeReusing`.
**Tests:** each setting (a table), a failing pre-launch task with each answer, the quit question.
**Proof:** none beyond the tests (no new UI); recorded in `IMPLEMENTED.md`.

### 25.12 Cross-checks, the desktop pass and performance
**Goal:** the milestone's loose ends (SPEC §10.31, §14, §15).
**Read:** SPEC §10.31, §14, §15, `docs/PERFORMANCE.md`, `docs/MANUAL-CHECKS.md`.
**Tasks:** the coverage table of the SPEC for area 6; a Remote Work test of D46 (a client sets a breakpoint, starts a session of the Go fixture, steps and stops it, and the host sees the same session); measurements (stepping 100 times in the Go fixture with dlv: the time from F10 to the painted line; the Variables view with 10,000 children; the console with 100,000 lines) in `docs/PERFORMANCE.md`; an end-to-end display test with real input (open the Go fixture, set a breakpoint, F5, hover, watch, step, Debug Console, stop); `docs/MANUAL-CHECKS.md` entries for what only the user's machine shows (debugging on the real GPU with `scripts/realinput.sh --desktop`); `scripts/error-tags.sh --untested` prints nothing.
**Tests:** the end-to-end display test; the Remote Work refusal test.
**Proof:** the end-to-end test's screenshot, `docs/implemented-proof-images/25.12-desktop-pass.png`.

### 25.13 Update the README
**Goal:** `README.md` describes Run and Debug.
**Read:** `README.md`, the M25 entries of `docs/IMPLEMENTED.md`, SPEC §10.31, D39–D41.
**Tasks:** the status, a "Run and Debug" section (how to start, `launch.json` in `.emetos/`, the four debuggers and how to get each: `dlv` through "Go: Install/Update Tools", `debugpy` with pip, gdb 14+ or lldb-dap, js-debug's Download), what is not built, fresh screenshots with `scripts/screenshots.sh`; every link resolves.
**Tests:** `TestREADMELinksResolve`.
**Proof:** the README's screenshots, viewed.

## M26: Testing

Requested by the user on 2026-11-01 with M25 (SPEC §10.32, D42, D43, D46). It is VS Code's Testing workbench (MIT, `reference/vscode/src/vs/workbench/contrib/testing/`) on an API like `vscode.tests`, with providers in the language extensions. No new Go module. The contract, the host and `fakeapi` change together, as in M25.

**Tests of the milestone.** Fake controllers and fake runners by default; the Go provider also with the real `go` in the default run (as step 14.3); the other real runners under `emetos_pytest`, `emetos_cargo`, `emetos_node`, `emetos_ctest` (D43), with tools installed only into `/var/tmp/emetos-tools/` as in M25 (pytest and coverage into the M25 venv; Jest and Vitest with `npm install --prefix /var/tmp/emetos-tools/node` at pinned versions, never in a fixture's own folder that is committed). Debug profiles need M25.

**Order.** 26.1 to 26.4 in order; 26.5 to 26.9 (the providers) after 26.4, one after the other; 26.10 last.

### 26.1 The Testing API and model
**Goal:** extensions describe tests and report runs; the core keeps the results (SPEC §10.32 "The contract", "The model").
**Read:** SPEC §10.32, D42; `reference/vscode/src/vscode-dts/vscode.d.ts` (the `tests` namespace), `reference/vscode/src/vs/workbench/api/common/extHostTesting.ts`, `contrib/testing/common/testService.ts`, `testResult.ts`, `testResultService.ts`, `testingStates.ts`, `testProfileService.ts`, `testCoverage.ts`, `testingContinuousRunService.ts`, `testResultStorage.ts`.
**Tasks:** `extension/testing.go`; `internal/core/testing` (the item tree by controller, profiles, run requests, results with VS Code's state precedence and durations, the last 16 runs saved with the state of §10.10 with output trimmed, continuous runs, coverage data and its sums); the host and `fakeapi`; the `testing.` commands from a Remote Work client (D46): not refused, and the client's view carries the Testing view, the glyphs and Test Results.
**Tests:** a table of item trees and results to states (precedence, parents, durations); run requests with include and exclude; continuous runs re-triggered; coverage sums; the stored results round trip; the API through the host with a fake controller.
**Proof:** none (no UI).

### 26.2 The Test Explorer
**Goal:** see and run the tests (SPEC §10.32 "The Testing view container", "Commands and keys", "Settings").
**Read:** SPEC §10.32; `reference/vscode/src/vs/workbench/contrib/testing/browser/testingExplorerView.ts`, `testingExplorerFilter.ts`, `testingViewPaneContainer.ts`, `testExplorerActions.ts`, `testing.contribution.ts`, `icons.ts`, `theme.ts`, `testingProgressUiService.ts`.
**Tasks:** `internal/gui/views/testing`: the view container, the Test Explorer tree with icons, durations, the title bar, the filter and its terms, sort and view modes, item actions, the summary and the badge; the commands and keys (the Ctrl+; chords), context keys and settings of §10.32 that it reads.
**Tests:** goldens (dark, light) of the explorer with mixed states, running, filtered, as a list; widget tests of every command and key against a fake controller; the filter terms (a table).
**Proof:** method B with a dev fake controller (`dev.testing.fake`, debug builds only): `scripts/proof.sh docs/implemented-proof-images/26.2-test-explorer.png --commands '[["workbench.view.testing.focus"], ["testing.runAll"], ["sleep", "2s"]]' -- <dir>`; the image shows passed, failed and skipped tests with durations and the summary.

### 26.3 The editor, the peek and Test Results
**Goal:** run tests from the editor and read their failures (SPEC §10.32 "The editor", Test Results).
**Read:** SPEC §10.32, §9.11; `reference/vscode/src/vs/workbench/contrib/testing/browser/testingDecorations.ts`, `testingOutputPeek.ts`, `testResultsView/`, `testingOutputTerminalService.ts`.
**Tasks:** the gutter glyphs and their menu, the failure messages at their lines, the peek (message, expected/actual in a small diff editor, stack trace, output, next and previous), the Test Results panel view with the ANSI text widget of 25.6, `testing.automaticallyOpenPeekView`, `testing.openTesting`, `testing.followRunningTest`, `testing.alwaysRevealTestOnStateChange`, `testing.saveBeforeTest`.
**Tests:** goldens of the glyphs in each state, an inline message, the peek with a diff and with a stack, Test Results; widget tests of a gutter click with each `defaultGutterClickAction`, Alt+F8 in the peek; a display test with real input (click a glyph, see the peek, rerun the failed).
**Proof:** method B with the dev fake controller: `scripts/proof.sh docs/implemented-proof-images/26.3-peek.png ...`; the image shows the gutter glyphs and the peek of a failure with its expected/actual diff.

### 26.4 Coverage
**Goal:** see what the tests ran (SPEC §10.32 "Test Coverage", coverage in the editor and the Explorer).
**Read:** SPEC §10.32; `reference/vscode/src/vs/workbench/contrib/testing/browser/testCoverageView.ts`, `codeCoverageDecorations.ts`, `testCoverageBars.ts`, `common/testCoverageService.ts`.
**Tasks:** the Test Coverage view with bars and sorting, the editor's coverage (gutter and inline, branch counts, the toggle and its toolbar), the Explorer's percentages (a decoration provider, §10.30's decorations service), the coverage settings.
**Tests:** goldens of the view, the editor with covered, uncovered and partial lines, the Explorer; the bar thresholds and the displayed percent (a table).
**Proof:** method B with the dev fake controller's coverage profile: `scripts/proof.sh docs/implemented-proof-images/26.4-coverage.png ...`; the image shows the coverage view and an editor with green and red lines.

### 26.5 Go tests in the Testing view
**Goal:** Go's tests through the Testing API, the "Go Tests" panel replaced (SPEC §10.32 "The providers" Go, §10.17, D42).
**Read:** SPEC §10.32, §10.17 (step 14.3's as-built text), `extensions/golang/testrun.go`, `testjson.go`, `testfuncs.go`, `testpanel.go`; vscode-go's `src/goTest/` (online) for the item ids and the subtest rules.
**Tasks:** the `go` controller (items from `go/parser`, refreshed on changes; run through the `go test -json` runner; subtests added at run time; Debug through dlv mode `test`, M25; Coverage with `-coverprofile`), the lenses and the `emetos.golang.test*` commands through the controller, `testpanel.go` and its view removed (its goldens and tests replaced), SPEC §10.17 updated.
**Tests:** discovery (a table of test files to items), runs against the fake `go` fixtures of 14.3 and the real `go` on `testdata` packages (pass, fail, skip, subtests, a build error, a panic, cancel), coverage from a real profile, debug under `emetos_dlv`.
**Proof:** method B on the Go fixture: `scripts/proof.sh docs/implemented-proof-images/26.5-go-tests.png ...`; the image shows Go tests with a failure peeked and coverage in the editor.

### 26.6 Python tests: pytest and unittest
**Goal:** Python's tests (SPEC §10.32 "The providers" Python).
**Read:** SPEC §10.32; vscode-python's `src/client/testing/` and `python_files/vscode_pytest/` (online) for what the helper reports.
**Tasks:** the `python` controller, the helper script (a pytest plugin and a unittest runner printing JSON lines, shipped with the extension and written to Emetos's cache), discovery, runs, Debug through debugpy, Coverage through coverage.py when present, the `python.testing.*` settings.
**Tests:** the helper's JSON over fake runners; under `emetos_pytest` with the venv: discovery and runs of a `testdata` project (parametrized tests, fixtures failing, unittest classes, coverage).
**Proof:** method B on a Python fixture with the venv: `scripts/proof.sh docs/implemented-proof-images/26.6-python-tests.png ...`.

### 26.7 Rust tests: cargo test
**Goal:** Rust's tests (SPEC §10.32 "The providers" Rust).
**Read:** SPEC §10.32; libtest's output format (online), `cargo test --help`.
**Tasks:** the `cargo` controller (discovery per target with `--list --format terse`, runs with `--exact`, the libtest output parser, Debug through gdb/lldb-dap on the binary of `--no-run --message-format=json`).
**Tests:** the parser (a table of libtest outputs: ok, FAILED with the failures section, ignored, should_panic, a panic in a thread, doc tests); under `emetos_cargo`: a `testdata` crate.
**Proof:** method B on a Rust fixture: `scripts/proof.sh docs/implemented-proof-images/26.7-rust-tests.png ...`.

### 26.8 JavaScript and TypeScript tests: Node, Jest, Vitest
**Goal:** JavaScript's and TypeScript's tests (SPEC §10.32 "The providers" JavaScript and TypeScript).
**Read:** SPEC §10.32; Node's `node:test` reporters, Jest's `--json` result format, Vitest's `json` reporter and `vitest list` (online).
**Tasks:** the `node-test`, `jest` and `vitest` controllers (detection from `package.json` and config files, the files and the static scan of `describe`/`it`/`test`, runs through the JSON and TAP reporters, Debug through js-debug, Coverage from Istanbul's `coverage-final.json`).
**Tests:** the static scan (a table of files), the reporters' parsers on fixtures; under `emetos_node`: a `testdata` project with each runner (installed into `/var/tmp/emetos-tools/node`).
**Proof:** method B on a TypeScript fixture with Vitest: `scripts/proof.sh docs/implemented-proof-images/26.8-js-tests.png ...`.

### 26.9 C and C++ tests: CTest
**Goal:** CMake projects' tests (SPEC §10.32 "The providers" C and C++).
**Read:** SPEC §10.32; `ctest --help`, CTest's `--show-only=json-v1` format and JUnit output (online).
**Tasks:** the `ctest` controller (the build folder setting, discovery, runs, the JUnit reader, Debug through gdb/lldb-dap on the test's command).
**Tests:** the JSON and JUnit readers on fixtures; under `emetos_ctest`: a `testdata` CMake project configured and built in the test.
**Proof:** method B on a C++ fixture: `scripts/proof.sh docs/implemented-proof-images/26.9-ctest.png ...`.

### 26.10 Cross-checks, the desktop pass and the README
**Goal:** the milestone's loose ends and the README (SPEC §10.32, §14, §15).
**Read:** SPEC §10.32, §14, `README.md`, the M26 entries of `docs/IMPLEMENTED.md`.
**Tasks:** the coverage table of the SPEC for the testing UI; a Remote Work test of D46 (a client runs a test of the Go fixture and both the client and the host see its result); measurements (a Go module with 5,000 tests: discovery, a full run's UI cost, the explorer's frame time) in `docs/PERFORMANCE.md`; an end-to-end display test with real input; `docs/MANUAL-CHECKS.md` entries for the real GPU pass; the README's "Testing" section with each provider and its requirements, and fresh screenshots; `scripts/error-tags.sh --untested` prints nothing.
**Tests:** the end-to-end display test; `TestREADMELinksResolve`.
**Proof:** the end-to-end test's screenshot and the README's screenshots, viewed.

## M27: HTTP Client

Requested by the user on 2026-11-01: "I would like also an extension like postman" (SPEC §10.33, D44–D46), with collections in readable files of the project, no scripts, and Emetos's own format only. The extension is `extensions/httpclient` (`emetos.httpclient`, "HTTP Client"). Go's standard library only; no new Go module.

**Tests of the milestone.** Every request goes to an `httptest` server on 127.0.0.1 (plain or TLS) or a fake authorization server; **no test reaches the network**. Secrets use the fake keyring of the database extensions; the real keyring only under `emetos_keyring`.

**Order.** 27.1 to 27.9 in order; 27.10 last.

### 27.1 The format, the store and variables
**Goal:** read and write collections, folders, requests, bodies and environments, and resolve variables (SPEC §10.33 "The format", "Variables").
**Read:** SPEC §10.33, D44, D45, §11.1 (`core/jsonc` edits), §10.10.
**Tasks:** `extensions/httpclient/format` (the types of version 1, reading with comments, writing through `core/jsonc` edits that keep comments and unknown keys, new files in the fixed key order, safe file names, `order`, newer versions read-only, errors with their tags and positions, paths outside the folder refused); `extensions/httpclient/store` (the tree of a workspace's collections, watched for changes on disk, create, rename, move, duplicate, delete to the trash with the body files); variables (the resolution order, current values in the state, globals, dynamic variables, `httpClient.allowedProcessEnv`, the keyring for secrets).
**Tests:** round trips (a table of files with comments and unknown keys edited and saved), every key, safe names and clashes, newer and broken files, store operations and external changes, the resolution order (a table), dynamic variables, secrets never in a file.
**Proof:** none (no UI).

### 27.2 The engine
**Goal:** send requests and capture everything about the response (SPEC §10.33 "The engine").
**Read:** SPEC §10.33; RFC 9110, RFC 7616 (digest), Go's `net/http/httptrace`, `cookiejar`, `mime/multipart`.
**Tasks:** `extensions/httpclient/engine` (building the request from the model and variables, every body kind, basic, bearer, API key and digest auth, the timeout, redirects with the chain kept, TLS options and client certificates, the proxy, a cookie jar per environment kept in the state, gzip and deflate, timings, the 50 MiB memory limit and the cache file beyond it, cancel), each failure with its own tag.
**Tests:** against `httptest` servers: each body kind (the server checks what it got), each auth (digest's challenge), redirects followed and not, the limit, a timeout, cancel, TLS with a bad certificate and with verification off, client certificates, cookies across requests and environments, gzip, a 200 MiB body streamed to the cache with bounded memory, timings present.
**Proof:** none (no UI).

### 27.3 The side bar
**Goal:** see and organize collections, environments and history (SPEC §10.33 "The side bar").
**Read:** SPEC §10.33, §10.4, §10.15 (side bar views).
**Tasks:** the view container and its three views with their menus and drag and drop, the environment status bar item and picker, the environment editor tab, history (state, limit, masking, Open, Save to Collection, Clear).
**Tests:** goldens (dark, light) of each view; widget tests of every action, drag and drop, history masking (a table).
**Proof:** method B on a fixture with a collection under `.emetos/http/`: `scripts/proof.sh docs/implemented-proof-images/27.3-side-bar.png --commands '[["emetos.httpclient.focus"], ["sleep", "1s"]]' -- <dir>`; the image shows collections with method badges, the environments and the history.

### 27.4 The request tab
**Goal:** build and send a request in a form (SPEC §10.33 "The request tab").
**Read:** SPEC §10.33, §10.3, §10.15 (window tabs and file views).
**Tasks:** the request editor tab (opened from the side bar and for `.request.jsonc` files, "Open as Text"), the method dropdown, the URL box with variable highlighting and hover, Params and URL in step, the Headers, Body (each kind, with Emetos editors for raw kinds), Auth, Settings tabs, Send and Cancel, Save, dirty state and the close question.
**Tests:** goldens of the tab with each body kind and auth type; widget tests of Params/URL sync (a table), bulk edit, Send through the engine against `httptest`, save writing the files (checked on disk), the close question.
**Proof:** method B: `scripts/proof.sh docs/implemented-proof-images/27.4-request.png ...` with a local `httptest`-like dev server (`dev.httpclient.echo`, debug builds only); the image shows a POST with a JSON body ready to send.

### 27.5 The response pane
**Goal:** read the response every way (SPEC §10.33 "The response pane").
**Read:** SPEC §10.33.
**Tasks:** the status line and timing hover, Body (Pretty with the language from the content type, Raw, image Preview, paged large bodies), Headers, Cookies, Timeline (redirects, TLS, timings), Save Response to File, Copy Body, Open in New Editor Tab, the layout setting.
**Tests:** goldens of each tab for JSON, XML, an image, an error status, a redirect chain; a large body paged within the frame budget.
**Proof:** method B with the dev echo server: `scripts/proof.sh docs/implemented-proof-images/27.5-response.png ...`; the image shows a sent request with a pretty JSON response and its status, time and size.

### 27.6 Extraction and checks
**Goal:** carry values between requests and check responses without scripts (SPEC §10.33 "Extraction and checks").
**Read:** SPEC §10.33, D44.
**Tasks:** the JSONPath subset, extraction into current values, checks and their results, the Extract and Checks tabs, the Checks tab of the response.
**Tests:** JSONPath (a table), each extraction source and scope, each check operator (a table), results shown.
**Proof:** method B with the dev echo server: `scripts/proof.sh docs/implemented-proof-images/27.6-checks.png ...`; the image shows passed and failed checks.

### 27.7 The collection runner
**Goal:** run a collection or folder in order (SPEC §10.33 "The collection runner").
**Read:** SPEC §10.33.
**Tasks:** the runner tab (choices, iterations, delay, stop on failure, CSV and JSON data files), the results table, Cancel, Save Results.
**Tests:** runs against `httptest` with values carried by extraction, iterations with data files (a table), stop on failure, cancel; goldens of the runner before, during and after.
**Proof:** method B with the dev echo server: `scripts/proof.sh docs/implemented-proof-images/27.7-runner.png ...`.

### 27.8 OAuth 2.0
**Goal:** get and refresh tokens (SPEC §10.33 "OAuth 2.0").
**Read:** SPEC §10.33, D45; RFC 6749, RFC 7636 (PKCE), RFC 8252 (loopback redirect).
**Tasks:** the client credentials and authorization code with PKCE grants, the loopback redirect server living only while it waits, the browser through `Workspace().OpenURI`, tokens in the keyring with refresh, Get New Access Token and Clear Token.
**Tests:** against a fake authorization server and a fake opener that follows the redirect: both grants, a refresh, an expired refresh token, a state mismatch, a denied consent, the loopback server closed afterwards.
**Proof:** method B with the fake authorization server (dev only): `scripts/proof.sh docs/implemented-proof-images/27.8-oauth.png ...`; the image shows the Auth tab with a token.

### 27.9 Remote Work, the desktop pass and performance
**Goal:** the milestone's cross-checks (SPEC §10.33 "Privacy and safety", §14, §15).
**Read:** SPEC §10.33, §10.18, §10.24, §14, `docs/PERFORMANCE.md`.
**Tasks:** the refusal with the class `outside` (D46) and its test; measurements (a collection of 1,000 requests in the side bar, a 50 MiB JSON response's Pretty view, the runner over 500 requests) in `docs/PERFORMANCE.md`; an end-to-end display test with real input (make a collection, send, extract a token, run the collection); `docs/MANUAL-CHECKS.md` entries (the real keyring; an OAuth login with a provider of the user's choice; the real GPU pass); `scripts/error-tags.sh --untested` prints nothing.
**Tests:** the end-to-end display test; the refusal test.
**Proof:** the end-to-end test's screenshot, `docs/implemented-proof-images/27.9-desktop-pass.png`.

### 27.10 Update the README
**Goal:** `README.md` describes the HTTP Client.
**Read:** `README.md`, the M27 entries of `docs/IMPLEMENTED.md`, SPEC §10.33, D44–D46.
**Tasks:** the status, an "HTTP Client" section (collections in `.emetos/http/` shared through git, the format in short, environments and secrets in the keyring, extraction and checks, the runner, OAuth 2.0, what is not built and why: no scripts, no import or export), fresh screenshots; every link resolves.
**Tests:** `TestREADMELinksResolve`.
**Proof:** the README's screenshots, viewed.

---

## M28: A terminal as a window tab

Requested by the user on 2026-11-05 ("being able to create terminal as panel and tab", clarified to mean a tab in the window's own tab strip beside the workspaces, SPEC §10.14, not an editor tab: step 17d.7 already built that one). The terminal already has a panel (step 17d.2) and an editor tab (17d.7); this milestone adds the third place VS Code does not have: a window tab of its own, using the window tab extension mechanism of step 11.7 (`Contributes.WindowTabKinds`, `api.WindowTabs().Provide`), the way `extensions/remotework` already uses it for its own tab.

### 28.1 The terminal's window tab
**Goal:** the "+" menu of the window tab strip has a **Terminal** entry that opens a single terminal, with the extension's usual pane (selection, find, shell integration, IntelliSense, persistence), as a tab beside the workspaces (SPEC §10.25 "A terminal as a window tab").
**Read:** SPEC §10.25 (the new bullet, and "As built (step 17d.2)" for the pane and "As built (step 17d.7)" for persistence), §10.14, §10.15 "Window tab extensions" and "As built (step 11.7)"; `extensions/remotework/remotework.go` (the two-scope `Activate`, `Contributes.WindowTabKinds`) as the precedent; `extensions/terminal/pane.go`, `session.go`, `persist.go` (what to reuse); `extension/windowtabs.go`.
**Tasks:**
- The manifest declares `Contributes.WindowTabKinds: []extension.WindowTabKindSpec{{ID: "emetos.terminal.tab", Title: "Terminal", Icon: "terminal"}}` (default `PlusMenuLabel`, no `...`: choosing it opens a terminal at once, nothing more to ask, as Scratchpad's entry did before it was removed).
- `Activate` runs in two scopes, as Remote Work's does: the existing one, per workspace (unchanged), and `extension.ScopeWindowTab` for a tab of the kind, where it calls `api.WindowTabs().Provide("emetos.terminal.tab", extension.WindowTabKind{Create: ...})`.
- `Create` makes one `Terminal` (`session.go`) with no workspace: the shell and profile from `terminal.integrated.defaultProfile.linux`/`profiles.linux` read through the window tab's own `Settings` API (which resolves as in a workspace, with no folder layer); the working folder `terminal.integrated.cwd` when it is absolute, else the home folder; the environment as the panel's terminals get it, without `api.TerminalEnvironment()` contributions tied to a folder (Git's `GIT_ASKPASS`, for instance, still applies: it does not need a folder). `WindowTabContent.Build` wraps `pane.go`'s `paneView` (reused as it is: no `terminals`/`entry` structures around it, a small new type owns the one `*Terminal` and the `paneEnv` instead), with no tabs list and no split view around it (one terminal; opening the kind again from the "+" menu makes another window tab).
- Links: the file-link detector finds nothing (no workspace folder, as for a No Folder workspace tab's terminal); web and OSC 8 links still work.
- Persistence: `terminal.integrated.enablePersistentSessions` (true by default) serializes the terminal (`emulator.Serialize`) into `WindowTabContent.SaveState`, the same bytes the panel and editor-tab terminals save; `Create`'s `state` argument revives it (a new shell in the same folder, the saved lines, the restored-session line), reusing `persist.go`'s functions. The process ends when the window tab closes or Emetos quits, like the other kinds of terminal.
- Closing: `WindowTab.Close()` kills the process at once (`SIGHUP`, then `SIGKILL` after 5 seconds, as every terminal); no confirmation (window tabs have no close veto, like an editor tab's terminal, D226 — nothing new here, just the same limitation in a third place).
- Context keys, the key dispatcher (`terminalFocus`, encoding keys to the pty, `commandsToSkipShell`) and the pointer behave as in the panel's pane; `CaptureKeys` is not used (a terminal window tab is local, not another computer's screen).
**Tests:** the manifest (the kind declared, `ExtensionWindowTabIDForeign`/`Duplicate`/`Invalid` not triggered); the "+" menu lists **Terminal** and opening it makes a tab with a running shell (`dev.openPlusMenu`, `dev.openWindowTab {"kind": "emetos.terminal.tab"}`); typing and output work in it (the fake API, as the panel's tests do); it has no file link under a fixture folder's absolute path (the home folder is used instead); it is dropped from `emetos.terminal.list`; it survives a restart with its scrollback (`persistentSessionScrollback`) when the setting is on, and does not when it is off; closing it ends the process with no question; a second **Terminal** entry opens a second, independent window tab; `TestEverythingTheStepAddsIsDeclared`-style check (as 17d.7's own) that nothing of the manifest is missed.
**Proof:** method B: `scripts/proof.sh docs/implemented-proof-images/28.1-terminal-window-tab.png --commands '[["dev.openWindowTab",{"kind":"emetos.terminal.tab"}],["dev.openPlusMenu"]]' -- testdata/proof/28.1/project`; the image shows a workspace tab and a Terminal window tab beside it, with the "+" menu open showing the **Terminal** entry.

## M29: A "+" button in the editor tab strip

Requested by the user on 2026-11-05: a terminal in the editor area (step 17d.7) was reachable only from the Command Palette, so they asked for "a "+" button in editor tab to select "new file" and "new terminal"", plus the Terminal menu entry and the panel dropdown entry for it.

### 29.1 The new-editor button and the terminal's editor-area entries
**Goal:** every editor group's tab strip ends with a **+** whose menu has **New Text File** and **New Terminal**, each opening in that group; the Terminal menu and the panel's profile dropdown also offer **New Terminal in Editor Area** (SPEC §10.3 "The new-editor button", §10.25 "Opening a terminal in the editor area").
**Read:** SPEC §10.3 (the tab strip, "The new-editor button", the context menu of a tab for how menus run with a context), §10.15 "Editor title action" and the menu constants, §10.25 "Terminals in the editor area" and "Opening a terminal in the editor area"; `internal/gui/kit/tabs.go`, `internal/gui/app/group.go`, `exttitle.go`, `tabmenu.go` (how a menu opens from the strip and runs rows with `{"groupId", ...}`); `internal/core/command/menus_default.go`; `extension/api.go` (menu ids), `extension/editors.go` (`GroupOfArgs`, `EditorOpenOptions.Group`); `extensions/terminal/terminal.go`, `tabcmds.go` (`createEditor`), `editor.go`, `windowtab.go`.
**Tasks:**
- `extension.MenuEditorTabsNew = "EditorTabsNew"` with a doc comment; the host shows the items extensions contribute to it (`Contributes.Menus`) like the other menus (when-clauses, groups, order).
- The core contributes **New Text File** (`workbench.action.files.newUntitledFile`, group `1_new`) to the menu in `menus_default.go`; `newUntitledFile` honors `{"groupId": n}` (the untitled editor opens in that group; no argument is the active group, as today).
- `kit.Tabs` gets an option for a trailing widget after the last tab that stays visible when the tabs overflow (at the right end of the scrolling area, left of the title actions); the group passes the **+** button there (Codicon `add`, tooltip "New...", title-action size and colors, hover and focus like a title action). Shown with `showTabs` `multiple` and `single`, and in an empty group; not with `none`.
- A click (or Enter/Space) opens the `EditorTabsNew` menu under the button, rows run with `{"groupId": n}` through `workbench.MenuSource` as the tab context menu does; Escape or a click outside closes it and the keyboard returns to the group.
- Terminal: `Contributes.Menus` gains `{Menu: MenuEditorTabsNew, Command: CmdCreateEditor, Title: "New Terminal", Group: "2_terminal"}` and `{Menu: MenuTerminal, Command: CmdCreateEditor, Title: "New Terminal in Editor Area", Group: "1_create", Order: 2}` (Split Terminal moves to order 3); `createEditor` reads `extension.GroupOfArgs(args)` (also when `groupId` sits next to `profileName`/`cwd`/`name`) and opens the tab in that group; `newWithProfile`'s quick pick ends with a separator and **New Terminal in Editor Area** (the default profile, in the active group).
- The window tab terminal is unaffected (`CmdCreateEditor` is already in `windowTabExcluded`).
**Tests:** the menu id and its rows (core row first, the terminal's after a separator; when-clauses hide a row); a click on **+** in each of two groups opens the menu, New Text File makes `Untitled-1` in the clicked group with the keyboard, New Terminal opens a terminal tab in the clicked group (`dev.*` commands and the widget tests over the fake platform); the button is there with `showTabs` `multiple`, `single` and in an empty group, not with `none`; it stays visible when 30 tabs overflow; Escape closes the menu; `createEditor` with `{"groupId": 2}` and with `{"groupId": 2, "profileName": "sh"}`; the Terminal menu row and the quick pick's last item; goldens of the strip with the button (dark and light) and with the menu open.
**Proof:** method B: `scripts/proof.sh docs/implemented-proof-images/29.1-new-editor-button.png --after 3s --commands '[["dev.openFile",{"path":"testdata/proof/29.1/project/main.go"}],["dev.openMenu",{"id":"EditorTabsNew"}]]' -- testdata/proof/29.1/project`; the image shows a text tab, the **+** after it and its menu open with New Text File and New Terminal. A second image, `scripts/proof.sh docs/implemented-proof-images/29.1-terminal-in-editor.png --after 4s --commands '[["dev.openFile",{"path":"testdata/proof/29.1/project/main.go"}],["workbench.action.splitEditorRight"],["workbench.action.createTerminalEditor",{"groupId":2}]]' -- testdata/proof/29.1/project`, shows a text file and a terminal tab side by side in two groups.

## M30: The Emetos Tutor

Requested by the user on 2026-11-05 ("What if we can have a tutorial mode like in vim?"): a `vimtutor` for Emetos, SPEC §10.35.

### 30.1 Opening the tutor
**Goal:** `emetos --tutor` and "Help: Emetos Tutor" make a fresh practice copy of the embedded lesson folder and open it as a new workspace tab with `TUTOR.md`, with `{{key:<command>}}` placeholders replaced by the user's own keys (SPEC §10.35 "Opening it", "The practice copy", "Your own keys", "Errors").
**Read:** SPEC §10.35, §10.10 (recently opened), §10.11 (the command line and a running instance), §10.14 (workspace tabs); `cmd/emetos/args.go` and `main.go` (how `--install-launcher` was added); `internal/gui/app/workspaces.go` (`HandleLaunch`, `openFrom`), `launch.go` (the command line of the first instance), `state.go` (the recent list), `statusbar.go` (`statusKeyLabel`).
**Tasks:**
- `assets/tutor/` with a first `TUTOR.md` (lesson 1 only; 30.2 writes the rest) and `assets.Tutor() fs.FS` with `assets.TutorRoot` (`go:embed`).
- `internal/core/tutor`: `Placeholders` (the commands the lessons name), `Expansions` (the label of each command's key, or how to run it from the Command Palette), `Folder` (the folder of the copy: `tutor`, or `tutor-2`... while a tab has the earlier ones open), `Copy` (empties the folder and writes the lessons with the placeholders replaced, off the UI thread), `IsPractice`. Error tags `TutorCopyFailed`, `TutorNoFreeFolder`, `TutorLessonsUnreadable`, each with a route test.
- `ipc.OpenArgs.Tutor`; `cmd/emetos`: `--tutor` (usage line; `ArgsConflict` with files, folders, `--goto`, `--diff`, `--merge` or `--wait`).
- `gui/app`: the command `emetos.tutor.open` ("Emetos Tutor", category Help) and the first row of the Help menu (group `1_welcome`), which makes the Help menu show in the menu bar; a `--tutor` launch, to a running Emetos or to the first instance, does the same; the practice folder and its files stay out of Open Recent.
**Tests:** the core package (a fresh copy, a second copy starts clean, a tab in use gets `tutor-2`, bound and unbound commands, a folder that cannot be written, every route); every placeholder of the embedded lessons names a command; the arguments (`--tutor`, with `--profile`, with a folder: `ArgsConflict`); the command opens a tab of the practice folder with `TUTOR.md`, keeps it out of Open Recent, and a second one goes to `tutor-2`; a `--tutor` launch, and one whose copy fails (status 1); the menu bar now has Help (the goldens that show the menu bar are regenerated and looked at).
**Proof:** method B: `scripts/proof.sh docs/implemented-proof-images/30.1-tutor.png --after 4s -- --tutor`; the image shows the tutor workspace tab with `TUTOR.md` open at its first lesson and the user's keys in its text.

### 30.2 The lessons
**Goal:** the eleven lessons of SPEC §10.35 "The lessons", with their practice files.
**Read:** SPEC §10.35; the default keybindings (`internal/core/keybinding/defaults.go`) and the keybindings of the Search and Terminal extensions.
**Tasks:**
- Write `TUTOR.md` (lessons 1 to 11, each a `##` heading, a short explanation and `--->` / `ok>` exercise pairs, keys only through `{{key:...}}`) and the practice files `practice/animals.yaml` (YAML: comments with `#` and no language server), `practice/fruits.txt` and `practice/hidden/needle.txt`.
- The check that every placeholder is a command with a default key moves to `cmd/emetos`, which sees the core's keybindings and those of the compiled-in extensions (Search's Ctrl+Shift+F and the Terminal's Ctrl+\` are the extensions').
- The README gets a "Learn Emetos: the tutor" section (how to open it, what it covers), and its Status lists M30 to M32.
**Tests:** the lessons' structure (eleven `##` lessons in order, every `--->` line followed by an `ok>` line or a block of them, every placeholder a command with a default key, the practice files the lessons name exist); a real-input test (`emetos_display`) that opens the tutor and does three exercises with real keys (delete a line in lesson 2, Ctrl+D twice in lesson 4, Ctrl+P to `fruits.txt` in lesson 6) and checks the text.
**Proof:** method B: `scripts/proof.sh docs/implemented-proof-images/30.2-tutor-lesson.png --after 3s -- --tutor`; the image shows the start of the lessons (the introduction, lessons 1 and 2 with their exercise lines) with the user's keys in the text. (The dev commands of `--commands` run in the first tab, not in the tutor's, so the proof cannot scroll the tutor to a later lesson; the real-input test does the exercises of lessons 2, 4 and 6.)

## M31: Extensions on and off in the Settings editor

Requested by the user on 2026-11-05 ("the option to disable/enable extensions from settings"). `extensions.disabled` already exists, but the Settings editor shows it only as "Edit in settings.json".

### 31.1 The Installed group of the Settings editor
**Goal:** the Settings editor's Extensions group starts with a group **Installed** holding an "Enabled" check box per extension that reads and writes `extensions.disabled` (SPEC §10.15 "Turning extensions on and off in the Settings editor").
**Read:** SPEC §10.15 (`extensions.disabled`, the Extensions page), §11 "The settings editor as built" (groups, rows, committing, modified, search filters); `internal/core/settings/{layout,uimodel,search}.go`, `internal/gui/settingsui/editor.go`, `internal/gui/app/settingspane.go` and `extensions.go` (`setExtensionEnabled`).
**Tasks:**
- `core/settings`: `ExtensionInfo`, `UIModel.SetExtensions`, `ExtensionRow`, `ExtensionSettingRow` (a check box `Row` "<Name>: Enabled" whose key is `@ext:<id>`), `Category.Extensions`, the group `extensions/installed` first under Extensions (User tab only), and `SetExtensionEnabled`, the one write of `extensions.disabled` (sorted; an empty list removes it) that the Extensions page now calls too; `SetValueFor` and `ResetFor` send the rows' keys to it.
- Search: the rows match by name, id and description; `@ext:<ids>` is supported (it lists those rows; the settings do not record their extension, so none is shown with them); `@modified` lists the disabled ones; `@id:` and `@lang:` list none.
- `settingsui`: the rows of `Category.Extensions` before the settings of the group; Copy Setting as JSON of a row gives `extensions.disabled`. The app gives the model the extensions of the Extensions page.
**Tests:** the model (rows follow `extensions.disabled`, the group first under Extensions, search by name, description, id, `@ext:`, `@modified`, none on the Workspace tab, writes keep other ids and stay sorted, Reset Setting, the error `SettingExtensionValueInvalid`); the editor (a click turns an extension off and on, the modified mark, Reset Setting, Copy Setting ID gives `@ext:<id>`, a change of the file shows at once, the Workspace tab has none); goldens of the group (dark and light, one extension off); the Extensions page still writes the same list.
**Proof:** method B: `scripts/proof.sh docs/implemented-proof-images/31.1-extensions-settings.png --config testdata/proof/31.1/config --after 3s --commands '[["workbench.action.openSettings","@ext:emetos.gitscm,emetos.golang,emetos.redis,emetos.search,emetos.terminal,emetos.httpclient"]]' -- testdata/proof/31.1/project` (the fixture's `settings.json` turns Redis off); the image shows the Installed group with its check boxes, Redis unchecked with the modified mark.

## M32: The mascot

Requested by the user on 2026-11-05 ("for mascot create a cat that throws up the hairball that used as icon, and put it on readme").

### 32.1 The mascot in the README
**Goal:** a mascot picture, a teal cat coughing up the hairball of the icon, centered at the top of the README (SPEC §16 "The mascot").
**Read:** SPEC §16 "Desktop entry and icon" and "The mascot"; `internal/gui/kit/appicon.go` (the icon's colors and `paintAppIconBall`).
**Tasks:**
- Render the icon's hairball alone at 1024 px with `paintAppIconBall` (a throwaway test, not committed) and trim it into `docs/mascot/hairball.png`.
- Draw the cat in `docs/mascot/mascot.svg` in the icon's colors, the hairball coming out of its open mouth, transparent background; render `docs/mascot/mascot.png` with `rsvg-convert -w 1200`.
- The README shows `mascot.png` centered under the title, with an alt text.
**Tests:** none (a picture for the documents); the picture is looked at on a white and on a dark background.
**Proof:** `docs/mascot/mascot.png` itself, linked from the step's `IMPLEMENTED.md` entry.

## M33: A missing tool is told once

Requested by the user on 2026-11-11 ("if something that is used by extension is missing, make a notification but don't annoy the user every time he opens the application").

### 33.1 Notices of missing tools are shown once, not at every start
**Goal:** a notification that a program an extension needs is not installed (gopls, a language server, the lint tool, git) is shown once and not again at the next start of Emetos, until the program was found and went away; a notice that answers a command of the user is shown every time (SPEC §10.34 "A missing tool is told once", decision 1912).
**Read:** SPEC §10.34 (`api.State()`, decision 960), §9.12 (the host's notification of a missing program), §10.17 "The missing tool", §10.19 "Finding it"; `extension/state.go`, `internal/core/storage/extensionstate.go`, `internal/gui/exthost/{state,servers}.go`, `extensions/internal/langserver/controller.go`, `extensions/golang/{tools,status,diagnostics}.go`, `extensions/gitscm/activate.go`.
**Tasks:**
- `storage`: `GlobalExtensionFolder` (`*`), accepted by `SetExtensionValue`, `ExtensionValue` and the cleaning of a state file.
- `extension`: `API.GlobalState()`, `FirstTime`, `FirstTimeSaying`, `Forget`; `exthost` implements the global state over the scope's `StateGet`/`StateSet`; `fakeapi` has `GlobalState`, `SetGlobalState`, `GlobalStateKeys`.
- The Go extension, the controller of the language packs, the Git extension and the host's own notification follow the rule of the spec; Restart Server from a status item's menu and a lint run started by hand count as asked for.
- `app`: the editor a restore put in front is published as `emetos.editor.activeChanged` (`initGroup`), so that the status item of the language shows what the quiet notification no longer says.
**Tests:** `app` (the event after a restore, with and without a file of the command line); `extension` (first time, another message, forgetting, a state that keeps nothing); `storage` (the global folder apart from the folders, kept across a start, cleaned); `exthost` (the global state apart from the folder's; the missing program is not told in a second tab, and is forgotten when it runs); `golang`, `langserver`, `gitscm` (a later session is quiet, what the user asked for is answered, found and gone is told once more).
**Proof:** the tests, and a run of the editor on this Mac with its own state folder and gopls hidden from `PATH`: the first start shows the notification, the second does not and the status bar says "gopls not found"; with gopls in `PATH` the remembered notice is gone from `state.json`, and without it again it is told once more. The step's `IMPLEMENTED.md` entry gives the result.

## M34: What the extensions need from the system

Requested by the user on 2026-11-13 ("I want emetos to have a way to tell me what my system has or missing for the extensions to work"); they chose the Extensions page and a Check System page, covering programs and services (SPEC §10.36). Turning extensions off already exists (step 31.1 and the Extensions page).

### 34.1 Requirements in the manifest, and their checks
**Goal:** an extension declares what it needs from the system with a check of each, the host runs the checks off the UI thread and keeps the results, and the language packs' servers are declared by themselves (SPEC §10.36 "Requirements are declared", "Alternatives are one requirement", "Checking").
**Read:** SPEC §10.36, §10.15 (the manifest, `CheckManifest`), §10.34 "What every pack gets"; `extension/{extension,manifest}.go`, `extensions/internal/langserver/{langserver,find,install,version,controller}.go`, `internal/gui/exthost/host.go`, `extensions/internal/fakeapi`.
**Tasks:**
- `extension`: `Requirement`, `RequirementKind`, `RequirementEnv`, `RequirementResult`, `RequirementState`, `Install`, `InstallCommand`, the family constants (with `FamilyMacOS`), `OSFamily(osRelease)` and `SystemFamily()`; `Manifest.Requirements`; `CheckManifest` checks them (`ExtensionRequirementInvalid`, `ExtensionRequirementDuplicate`). `langserver` keeps `Install`, `InstallCommand`, `Family` and the constants as aliases of these.
- `langserver`: `Config.Requirements()` gives one requirement for the server (one per `Program` when there is no `Select`; one that follows `Select` when there is), whose `Check` is `Find` and the version check the launcher runs, with the `Purpose`, install commands and path setting of the program; `langpack.Spec` puts them in the manifest.
- `exthost.Requirements`: made per window with the host's infos and a reader of the settings of the active tab (also for keys nobody registered), checks on demand, four at a time, 10 s each, a panic caught (`ExtensionRequirementPanicked`), results kept per folder, `Rows()`, `CheckAgain()`, `SetFolder`, re-checking one requirement when its `PathSetting` changes, `OnChange`.
**Tests:** `extension` (the manifest checks, each tag's route; `OSFamily` on the os-release of Fedora, Ubuntu, Arch, openSUSE, an unknown system; `SystemFamily` on the running system); `langserver` (the requirement of rust-analyzer found in `PATH`, through its path setting, through a folder after `PATH`, missing, too old, the Python server following `python.languageServer`); `exthost` (on demand only, the limit of four, the timeout, a panic, the cache per folder, Check Again, a path setting that changes re-checks that requirement only, the results of a turned-off extension, the processes of a cancelled check end). Fake programs in a temporary `PATH`; nothing real is looked for.
**Proof:** the tests; the step's `IMPLEMENTED.md` entry lists the requirement of every pack as `Config.Requirements()` gives it.

### 34.2 The requirements of the language extensions beyond their servers
**Goal:** the Go, Python, JavaScript, TypeScript, Rust, C/C++, Java and C# extensions declare their interpreters, runtimes, tools and debuggers, each checked by the code the extension uses when it runs them (SPEC §10.36 "What is declared").
**Read:** SPEC §10.36, §10.17, §10.31 "The debuggers", §10.32, §10.34; `extensions/golang/{tools,env,run,diagnostics}.go`, `extensions/python/{interpreter,interpreters,debug}.go`, `extensions/javascript/jsdebug.go`, `extensions/typescript/version.go`, `extensions/internal/nativedebug/adapter.go`, `extensions/rust/debug.go`, `extensions/cpp/testctl.go`, `extensions/java/server.go`, `extensions/csharp/dotnet.go`.
**Tasks:**
- Go: `go` (required), gopls (required, the extension's own finder: `go.goplsPath`, `go.alternateTools`, `GOBIN`, `GOPATH/bin`), `dlv` (optional, debugging), the program of `go.lintTool` (optional, linting); `InstallAction` `emetos.golang.installTools` for the three tools.
- Python: the interpreter the extension selects (required), `debugpy` in it (optional, debugging), `pytest` in it (optional, testing; unittest needs nothing).
- JavaScript and TypeScript: `node` (required: typescript-language-server, the debugger and the tests run on it), js-debug (optional, debugging; `InstallAction` "JavaScript Debugger: Download"), the `typescript` package the extension's version finder finds.
- Rust: `cargo` (required: tests and runs), the native debugger (optional: gdb 14 or newer, else lldb-dap). C and C++: the native debugger (optional), `cmake` and `ctest` (optional: tests).
- Java: `java` 21 or newer, which jdtls runs on (required). C#: the .NET SDK, `dotnet` (required).
- Where the extension's finder is not reachable from the requirement's `Check` without a running activation, move it into a function of the package that both call; behavior when the feature is used does not change.
**Tests:** each requirement found, missing and unusable (too old, a path setting that names nothing) with fake programs in a temporary `PATH` and fake interpreters (`python -c "import debugpy"` answered by a script); the manifests pass `CheckManifest`.

### 34.3 The requirements of git, the containers, the keyring, the camera, the ssh-agent and the terminal
**Goal:** the extensions that need services and devices declare them (SPEC §10.36 "What is declared").
**Read:** SPEC §10.36, §10.18, §10.20, §10.21–§10.24, §10.25, §10.30, §10.33; `extensions/gitscm/git/find.go`, `extensions/internal/containerapi` (`Find`, `Negotiate`), `extensions/internal/containerui/cli.go`, `extensions/internal/dbconn/{keyring,secretservice}.go`, `extensions/internal/keyqr/v4l2*.go`, `extensions/internal/sshlogin/agent.go`, `extensions/terminal/profile.go`.
**Tasks:**
- Git: git 2.30 or newer through `git.path` (required; `git.Find`).
- Containers (Docker) and Podman: the engine (required, a service: `Find` and `/_ping` of the endpoint, the version in the result) and the CLI (required for logs, exec, compose).
- The databases hub (SQL, MongoDB, Redis share it) and the HTTP Client: the Secret Service keyring (optional, service: the name `org.freedesktop.secrets` is owned or activatable on the session bus; on macOS the login keychain), with the purpose "keeps passwords out of the files".
- Remote Work: a camera (optional, device: a V4L2 capture device among `/dev/video*`; none on macOS yet, `RequirementUnknown` with that said) and the ssh-agent (optional, service: `SSH_AUTH_SOCK` answers).
- The terminal: the shell of the default profile (required).
- A check never changes anything: it pulls no image, unlocks no keyring (no prompt), opens no camera stream beyond the capability query, and lists none of the user's containers.
**Tests:** each found, missing and unusable: a fake engine API server on a Unix socket under `t.TempDir()` (answers, does not answer, too old), a private session bus from `dbconn/secrettest` (owned, not owned), the fake camera driver of `keyqr`, a fake agent socket, a git older than 2.30 (a fake program); nothing touches the user's Docker, Podman, keyring or camera.

### 34.4 The requirements on the Extensions page
**Goal:** the Extensions page shows each extension's requirements with their state and the buttons that help meet them (SPEC §10.36 "On the Extensions page").
**Read:** SPEC §10.36, §10.15 "The page Extensions"; `internal/gui/extensionsui/page.go`, `internal/gui/app/extensionspane.go`, `extensions.go`; the Settings editor's command `workbench.action.openSettings` with a query.
**Tasks:**
- `extensionsui`: `Options.Requirements` (the rows of `exthost.Requirements`), a line per requirement under the state line with its icon, name, "optional", path and version or detail; the buttons Copy Install Command, Install, Set Path..., Instructions; the header's Check System and Check Again; "Checking..." while the results come.
- `app`: the window's `exthost.Requirements` (made when a page first needs it, its folder following the active workspace tab), the clipboard, `OpenURI`, the Settings editor on a key, the commands of `InstallAction`.
**Tests:** the page (a found, a missing required, a missing optional, an unusable and an unknown requirement; each button does what it says; a turned-off extension shows its requirements; the results arrive while the page is open); goldens of the page in dark and light with each state.
**Proof:** method B: `scripts/proof.sh docs/implemented-proof-images/34.4-extensions-requirements.png --config testdata/proof/34.4/config --after 4s --commands '[["workbench.extensions.action.showInstalledExtensions"]]' -- testdata/proof/34.4/project` (the fixture's `settings.json` points `go.goplsPath` and `git.path` at files that do not exist); the image shows the Go extension's requirements with gopls "cannot be used" and its buttons.

### 34.5 The Check System page and the README
**Goal:** "Help: Check System" opens a page with every requirement of the enabled extensions, grouped by state, with Check Again and Copy Report; the README tells about both places (SPEC §10.36 "The Check System page").
**Read:** SPEC §10.36, §10.15; `internal/core/editorgroup` (the inputs of the Extensions and Event Sharing pages), `internal/gui/app/extensionspane.go` (`openSingletonPage`), `internal/core/command/{builtin,menus_default}.go`, `README.md` ("Extensions").
**Tasks:**
- `editorgroup.SystemCheckInput` (`emetos.systemCheck`, restored; `SystemCheckInputInvalid`), the command `emetos.checkSystem` ("Help: Check System") in the palette and in the Help menu (`5_tools`), and the Extensions page's Check System button.
- `extensionsui.SystemCheckPage`: the system line, the folder, the summary, the three groups, the line of turned-off extensions, Check Again, Copy Report (the Markdown of the spec).
- README: in "Extensions", how to see what the system lacks (the two pages, Copy Report) and how to turn an extension off (the Extensions page, the Settings editor), with the proof image.
**Tests:** the page (the groups and their order, the summary, the turned-off line, Copy Report's Markdown exactly, Check Again, the command and the menu item, restore with the window); `TestREADMELinksResolve`; goldens dark and light.
**Proof:** method B: `scripts/proof.sh docs/implemented-proof-images/34.5-check-system.png --config testdata/proof/34.5/config --after 4s --commands '[["emetos.checkSystem"]]' -- testdata/proof/34.5/project`; the image shows the System Check page with its groups, a missing requirement first.

## M35: An untrusted Git repository in the status bar

Requested by the user on 2026-11-13 ("if a project was not trusted before, then I want on emetos to say under the editor that the project was not trusted (or something like that) and the user press on it to show the dialog if he wants the project to be trusted").

### 35.1 The Not Trusted item of the status bar
**Goal:** while the window tab has a Git repository that is not trusted, the status bar says so, and a click asks the trust question (SPEC §10.30 "Trusting a repository", "The status bar says it").
**Read:** SPEC §10.30 ("Trusting a repository", "Status bar", the welcome content), §10.15 (`api.StatusBar()`); `extensions/gitscm/{trustflow,commands,statusbar,model,repository,welcome}.go`, `extension/api.go` (`StatusBar`, `StatusItemSpec`).
**Tasks:**
- `extensions/gitscm`: the item `git.untrusted` (left, the priority of the branch item; `$(workspace-untrusted) Not Trusted`, the count with several; the warning colors; the tooltip with the paths; the command `git.trustRepository`), updated wherever `git.untrustedRepositoryCount` is (a repository opened, trusted, declined, closed, untrusted from Manage Trusted Repositories, `git.enabled` changed, git missing).
- `git.trustRepository` from the item asks the question even after Don't Trust in this session (it is what the user asked for), as the welcome content's button does.
**Tests:** `gitscm` over `fakeapi` with `gittest` repositories: no item for a trusted repository, the item for an unanswered one and after Don't Trust, the count and the tooltip with two, a click shows the dialog and Trust removes the item and starts git, Manage Trusted Repositories bringing it back, none with `git.enabled` false or git missing; the item's spec (text, icon, colors, command).
**Proof:** method B. A repository cannot be committed under `testdata`, so `testdata/proof/35.1/make-repo.sh <dir>` makes one (with a test identity and its own `HOME` and `GIT_CONFIG_GLOBAL`), and the step adds the dev command `dev.dialog.answer {"button"}`, which presses a button of the dialog that shows (the plan first named it `dev.git.answerTrust`; an extension cannot close a dialog the host shows, so the window's dev command does it): `scripts/proof.sh docs/implemented-proof-images/35.1-untrusted-status.png --after 4s --commands '[["sleep","2s"],["dev.dialog.answer",{"button":"Don\u0027t Trust"}]]' -- <dir>`; the image shows the item "Not Trusted" in the status bar.

## M36: Images, videos, PDFs and EPUBs

Requested by the user on 2026-11-13 ("I want it to be able to open images, videos, pdfs and epubs"); for video and PDF they asked for what also works on macOS and Windows and left the choice to Emetos, and chose a reflowed EPUB reader (SPEC §10.37, D52–D55).

### 36.1 File editors
**Goal:** an extension can be the editor of a file: files that match its globs open in it, "Reopen Editor With..." switches between it and the text editor, and `workbench.editorAssociations` chooses (SPEC §10.37 "File editors", D52).
**Read:** SPEC §10.37, §10.15 ("As built (step 11.6)": editor tabs and file views), §6.3 (the binary notice), §10.3 (preview tabs, the tab context menu), §10.10 (restore); `extension/{editors,manifest}.go`, `internal/core/editorgroup/{exttabinput,textinput}.go`, `internal/gui/exthost/editors.go`, `internal/gui/app/{exttabs,fileviews,gridview,workspacemodel}.go`, `internal/core/command/menus_default.go`.
**Reference:** `reference/vscode/src/vs/workbench/services/editor/common/editorResolverService.ts` (priorities, `editorAssociations`), `reference/vscode/src/vs/workbench/contrib/customEditor/common/contributedCustomEditors.ts`, `reference/vscode/src/vs/workbench/browser/parts/editor/editorCommands.ts` (`reopenWithEditor`).
**Tasks:**
- `extension`: `FileEditorSpec`, `FileEditorPriority` (`FileEditorDefault`, `FileEditorOption`), `Contributes.FileEditors`, the manifest checks, `FileEditors`, `FileEditorProvider`, `FileEditorFile`, `FileEditorChange`, `FileEditorTab`, `FileEditorContent`, `API.FileEditors()`; `fakeapi` implements them; the activation event `onFileEditor:`.
- `exthost`: the file editors of the active extensions, `Scope.FileEditorsFor(path)` (with `workbench.editorAssociations`), `Open` contained like the other callbacks (`ExtensionFileEditorPanicked`), the status items of the active tab.
- `editorgroup.FileEditorInput` (`emetos.fileEditor`, serialized, a resource, never dirty, a `Copier`); the window opens matching files in it (from the Explorer, Quick Open, the command line, links, restore), `workbench.action.reopenWithEditor` with its pick and "Configure default editor for '*.ext'...", the tab menu item, Open With... on the binary notice, the fallback to text.
- Register `workbench.editorAssociations` with VS Code's description.
**Tests:** `extension` (each manifest check and its route); `exthost` (matching, associations, priorities, a disabled extension, a panic); `app` with a test extension (a matching file opens in the file editor, preview replacement, Reopen With to text and back, Configure default editor writes the setting, a binary file's Open With..., restore, split, the extension turned off falls back to text, a change on disk reaches `OnChange`, status items follow the active tab); goldens of the pick and the notice with Open With... in dark and light.

### 36.2 Image Preview
**Goal:** images open in a preview like VS Code's, with zoom and the size, file size and zoom in the status bar (SPEC §10.37 "Image Preview").
**Read:** SPEC §10.37, §10.15; Glutter's `ui/codec.go`, `widgets/image.go`, `svg`; `extension/ui`.
**Reference:** `reference/vscode/extensions/media-preview/` (`package.json`, `src/imagePreview/*.ts`, `media/imagePreview.js`, `media/imagePreview.css`).
**Tasks:**
- `extensions/mediapreview`: the extension (`emetos.mediapreview`, "Media Preview"), the file editor `emetos.mediapreview.image` for every format of the spec but AVIF and HEIC (they come in 36.4), decoding off the UI thread with the limits, the TIFF and ICO readers, SVG through Glutter's `svg`, animated GIF.
- The view: fit, zoom levels, click and Alt+click, Ctrl+wheel and pinch, scroll and drag, pixelated at 3x and more, the checkerboard, reload and deleted file; the commands `imagePreview.zoomIn`, `zoomOut`, `copyImage` with VS Code's titles, keys and when-clauses; the three status bar entries and the zoom pick.
**Tests:** decoding each format from small files the test writes (PNG, JPEG, GIF with three frames, BMP, WebP from a fixture, TIFF, ICO with PNG and BMP entries, SVG), the limits, zoom steps and the anchor of a click, fit, the status texts, reload keeping the zoom, a deleted file, Copy; goldens (a transparent PNG fitted, zoomed to 5x pixelated, an SVG, the status bar) in dark and light.
**Proof:** method B: `scripts/proof.sh docs/implemented-proof-images/36.2-image-preview.png --config testdata/proof/36.2/config --after 3s -- testdata/proof/36.2/project testdata/proof/36.2/project/emetos.png` (the project holds a copy of the app icon); the image shows the icon in the preview on the checkerboard and the three status bar entries.

### 36.3 FFmpeg
**Goal:** Emetos finds FFmpeg, probes a video, and decodes its frames and sound through pipes, with processes that end with their context; Media Preview declares it as a requirement (SPEC §10.37 "Video", D53).
**Read:** SPEC §10.37, §10.36; `extensions/internal/langserver/{find,version}.go` (`CheckProgram`), `extension/requirements.go`; Glutter's `audio` package (`System`, `Device`, `Stream`).
**Tasks:**
- `extensions/internal/ffmpeg`: `Find` (the setting, `PATH`, the Homebrew folders; `ffprobe` beside `ffmpeg`), the version check (4.4+), `Probe` (the JSON of ffprobe into `Info`: duration, streams, size, rate, codecs), `Frames` (a reader of RGBA frames at a size and rate from a time) and `Sound` (f32le, 48 kHz, stereo, from a time), each process in its own process group, ended with the `ctx`, its stderr kept (the last 4 KiB) for the errors, with the codec named when FFmpeg cannot decode it.
- The requirement FFmpeg of `emetos.mediapreview` with the spec's install commands and `mediaPreview.ffmpegPath` (registered).
**Tests:** a fake `ffmpeg` and `ffprobe` (the test binary run again under another name by `TestMain`, writing frames and samples it computes) in a temporary `PATH`: found, through the setting, in a Homebrew folder of the test, too old, missing; probe of a video with and without sound, a damaged one; frames and sound with the right sizes and counts; a cancelled `ctx` ends the processes (no child left, checked by PID); an unknown codec. With the build tag `emetos_ffmpeg` the same against the real FFmpeg on a video the test makes with `-f lavfi -i testsrc`.

### 36.4 The video player
**Goal:** videos play in their tab with sound, seeking and the controls of the spec; AVIF and HEIC images open through FFmpeg (SPEC §10.37 "Video", "Image Preview").
**Read:** SPEC §10.37, §10.18 (the view of a client); `extensions/mediapreview`, `extensions/internal/ffmpeg`; Glutter's `audio`.
**Reference:** `reference/vscode/extensions/media-preview/src/videoPreview.ts` and `media/videoPreview.js` (the settings).
**Tasks:**
- The file editor `emetos.mediapreview.video`: the first frame on open, Play/Pause, the clock (the sound's played position, else the monotonic clock), frame dropping, the bounded queues, seeking, Mute, volume, Loop, the keys, `mediaPreview.video.autoPlay` and `mediaPreview.video.loop` (registered with VS Code's descriptions), pause when hidden, the state saved with the tab, muted in a client's view, the messages of a missing FFmpeg and of a codec it cannot decode.
- AVIF, HEIC and HEIF in the image preview through one frame of FFmpeg.
**Tests:** with the fake FFmpeg and a fake audio sink (a `Stream` read by the test at a set pace): the frames shown follow the sound's position, late frames dropped, pause holds the frame and the processes, a seek restarts both at the time, loop, autoplay, the end, mute and volume reach the stream, a hidden tab pauses, closing ends the processes, no FFmpeg, an undecodable codec; AVIF through the fake; goldens of the player (first frame, playing with the bar, the codec message) in dark and light. `docs/MANUAL-CHECKS.md`: playing a real video with sound on this machine (`scripts/isolated.sh -- go run -tags debug_mode ./cmd/emetos <a video>`), listened to by the user.
**Proof:** method B: `testdata/proof/36.4/make-video.sh <dir>` makes a 10 s test video with FFmpeg's `testsrc` and a sine; `scripts/proof.sh docs/implemented-proof-images/36.4-video.png --after 4s --commands '[["emetos.mediapreview.video.play"]]' -- <dir>/test.mp4`; the image shows the test pattern playing with the bar.

### 36.5 The PDF viewer
**Goal:** PDFs open with their pages drawn by Poppler, scrolled continuously, with zoom, page navigation and passwords (SPEC §10.37 "PDF", D54).
**Read:** SPEC §10.37, §10.36; `extensions/internal/langserver/{find,version}.go`.
**Tasks:**
- `extensions/pdf/poppler`: finding the programs (`pdf.popplerPath`, `PATH`, the Homebrew folders; version 22.02+), `Info` (pages and sizes), `Render` (one page at a dpi to an image), the password, processes in their own groups ended with the `ctx`, the errors of a damaged or locked file.
- The extension `emetos.pdf` with the requirement Poppler, the file editor `emetos.pdf.viewer`: the layout of the pages, drawing what is in view (two at once), the 128 MiB cache, scaled pictures while a zoom is drawn, the toolbar's page box, Previous/Next, zoom (Fit Width, Fit Page, the levels), the keys, the password box, reload, the state saved with the tab. `pdf.popplerPath` registered.
**Tests:** fake Poppler programs (as 36.3's fakes) for found/missing/too old, info, render, a password asked and given to every program, a wrong one, a damaged file, cancellation ending the processes; the view: only the pages in view are drawn, the cache bound, zoom keeps the page in view, the page box, reload keeping the page; goldens (Fit Width with a page drawn, the password box) in dark and light. With the build tag `emetos_poppler` the client against the real Poppler on a PDF the test writes (a one-page PDF written by hand in the test).
**Proof:** method B: `scripts/proof.sh docs/implemented-proof-images/36.5-pdf.png --after 4s -- testdata/proof/36.5/project/sample.pdf` (a small PDF of several pages made for the proof with text and a drawing, committed with its source); the image shows the first pages with the toolbar.

### 36.6 Outline, Find and Copy Page Text in the PDF viewer
**Goal:** the PDF viewer has its outline, finding text with highlights, and copying a page's text (SPEC §10.37 "PDF").
**Read:** SPEC §10.37; `extensions/pdf`; the editor's find widget (`internal/gui/editor/find*.go`) for its look.
**Tasks:**
- `poppler.Outline` (`pdftohtml -xml -i -q -stdout`), `poppler.Words` (`pdftotext -bbox-layout`, page by page, in the background), `poppler.PageText` (`pdftotext -layout`).
- The Outline panel and its tree, the find widget (Ctrl+F, Enter, Shift+Enter, the count, match case, Escape), the highlights in the theme's find colors, matches across words of a line, Copy Page Text.
**Tests:** parsing the outline XML (nested, none, odd entities) and the bbox XHTML from fixtures; matching across words and lines, case; the view: the outline goes to a page, find moves between matches across pages and scrolls to them, highlights drawn on the right boxes at two zooms; goldens (the outline panel, a page with highlights) in dark and light.
**Proof:** method B over the sample of 36.5: `scripts/proof.sh docs/implemented-proof-images/36.6-pdf-find.png --after 4s --commands '[["emetos.pdf.toggleOutline"],["emetos.pdf.find","drawing"]]' -- testdata/proof/36.5/project/sample.pdf`; the image shows the outline and a highlighted match.

### 36.7 The EPUB book model
**Goal:** a book is read into its metadata, table of contents and the blocks of its chapters, safely (SPEC §10.37 "EPUB", D55).
**Read:** SPEC §10.37; the EPUB 3.3 specification (https://www.w3.org/TR/epub-33/: the container, the package document, the navigation document) and EPUB 2's NCX.
**Tasks:**
- `extensions/epub/book`: `Open(ctx, path)` (the limits, path cleaning, the DRM check), `Book` (title, authors, language, `Spine`, `TOC`), `Chapter(i)` parsing XHTML into `Block`s and `Inline` runs as the spec lists, anchors (`id`s) for links, images as zip paths; no UI.
**Tests:** books written by the test with `archive/zip`: EPUB 3 with a nav, EPUB 2 with an NCX, both, nested TOC, each block and inline kind, `hidden` and `display:none`, centered text, footnotes, entities and unclosed HTML tags, an image inside `svg`, a link to another chapter's anchor, a remote image left out; refused: too many entries, an entry too large, a zip bomb ratio, a path with `..`, DRM, no container, a broken package document (each with its Tag and route).

### 36.8 The EPUB reader
**Goal:** books open in a reflowed reader with their contents, font size, chapters and the reading position remembered (SPEC §10.37 "EPUB").
**Read:** SPEC §10.37, §10.34 "A missing tool is told once" (`api.GlobalState()`, for the position); `extensions/epub/book`, `extension/ui/markdown` (how it renders blocks and images).
**Tasks:**
- The extension `emetos.epub` and the file editor `emetos.epub.reader`: the column, `epub.fontFamily` and `epub.fontSize` (registered), the bar (Contents, the titles, A− and A+, Previous and Next), the Contents panel, links inside the book and to the web, images, the keys, the position saved with the tab and in the global state by path.
**Tests:** over `fakeapi` and `uitest`: a chapter rendered with each block kind, Next/Previous and the keys, the contents go to an anchor, A+ writes the setting and reflows, a link to another chapter, a web link through `OpenURI`, the position restored in a new tab of the same book, a refused book's message; goldens (a chapter with a heading, text and an image; the contents panel) in dark and light.
**Proof:** method B: `scripts/proof.sh docs/implemented-proof-images/36.8-epub.png --after 3s --commands '[["emetos.epub.toggleContents"]]' -- testdata/proof/36.8/project/sample.epub` (a small book of public-domain text made by `testdata/proof/36.8/make-epub.go` and committed); the image shows a chapter with the contents panel.

### 36.9 The README and the desktop pass
**Goal:** the README tells what Emetos opens and what it needs for it; the viewers are checked together on this machine (SPEC §10.37).
**Read:** SPEC §10.37, §10.36; `README.md`.
**Tasks:**
- README: a section "Images, videos, PDFs and EPUBs" (the formats, FFmpeg and Poppler and how to install them on Linux and macOS, Reopen Editor With and `workbench.editorAssociations`), with the proof images of 36.2, 36.4, 36.5 and 36.8.
- A real-input display test that opens an image, a video, the PDF and the book from the Explorer in one window and closes them (no process left).
**Tests:** `TestREADMELinksResolve`; the display test (`emetos_display`, with the real FFmpeg and Poppler; skipped when they are missing).
