# CLAUDE.md

Guidance for Claude Code when working in this repository (Go, hosted on GitHub).

## Project
Emetos is a code editor in the spirit of Visual Studio Code, written in Go. Its user interface is built on [Glutter](https://github.com/rm4n0s/glutter), a cgo-free GUI toolkit for Go with Flutter's framework API (Wayland/X11, Vulkan with an OpenGL ES 3.0 fallback). Emetos targets the Linux desktop only. Language features come from language servers (LSP) and debugging from debug adapters (DAP), so the editor itself stays language-agnostic.

**`docs/SPEC.md` is the source of truth.** Read the sections that cover a task before starting it. If the code and the spec disagree, or a task needs a decision the spec doesn't make, ask instead of guessing. When a decision changes, update the spec on the same branch.

## Commands
- Build: `go build ./...`. Emetos is cgo-free; `CGO_ENABLED=0 go build ./...` must always work
- Test all: `go test -race ./...` (the race detector needs cgo, so this one needs a C compiler)
- Test the cgo-free path: `CGO_ENABLED=0 go test ./...`
- Test one package: `go test ./internal/core/buffer -run TestName -v`
- Tests that need a real display/GPU: `go test -race -tags emetos_display ./...`
- Update golden images: `go test ./... -run Golden -update-goldens`, then review every changed PNG
- Vet: `go vet ./...`
- Lint: `golangci-lint run`
- Format: `gofmt -w .` (or `goimports -w .`)
- Generate: `go generate ./...`
- Run the editor: `go run ./cmd/emetos` (debug build; it uses your real configuration)
- Run the editor isolated from your configuration: `scripts/isolated.sh [--config <fixture dir>] -- go run ./cmd/emetos <args>`
- Make a proof screenshot: `scripts/proof.sh <out.png> [--config <dir>] [--after <duration>] [--commands '<json>'] -- <emetos args>` (docs/IMPLEMENTATION.md "Proof methods")
- Release build: `go build -tags glutter_release ./cmd/emetos`
- Fetch the pinned VS Code references into `reference/`: `scripts/fetch-reference.sh`
- After changing dependencies: `go mod tidy`

## Definition of done
Before reporting a task as finished, run `gofmt`, `go vet ./...`, `golangci-lint run` and `go test -race ./...`, and fix anything they report. Say so explicitly if any step was skipped or failed. Also:
- `CGO_ENABLED=0 go build ./...` and `CGO_ENABLED=0 go test ./...` pass (proves the editor stays cgo-free)
- Every new exported identifier has a doc comment
- Every changed golden image has been looked at, not just regenerated

## Layout
- `cmd/emetos/` is the editor's `main` package
- `internal/core/...` is the editor core (text buffer, documents, editing, languages, settings, keybindings, workspace, storage). It never imports Glutter or `internal/gui`, so it is tested without a UI
- `internal/gui/...` is the UI on Glutter: the widget kit, the editor widget, the workbench, views and dialogs. It imports the core, never the other way round
- `internal/archtest/` checks the package boundaries and forbidden imports (SPEC §5.3)
- The UI is built on Glutter's `widgets` layer; no Material widgets (SPEC D9)
- `third_party/` holds the licenses of bundled assets (grammars, themes, icon fonts); `NOTICE` lists them
- `reference/` (git-ignored) holds the VS Code sources (and vscode-textmate, Emmet, vscode-go, Codicons) used for porting and for copying assets, at the versions pinned in `scripts/reference-versions.txt` (committed). `scripts/fetch-reference.sh` creates it. Never import or commit it; bundled assets are copied out of it by `scripts/update-assets.sh`
- Code ported from VS Code (MIT) says so with a `// Ported from VS Code: <path>` line
- `docs/SPEC.md` is the specification; `docs/IMPLEMENTATION.md` is the step-by-step plan; `docs/IMPLEMENTED.md` logs finished steps; `docs/implemented-proof-images/` holds each step's proof screenshots
- `scripts/` holds helper scripts
- Tests live next to the code (`foo_test.go`); golden images live in `testdata/goldens/` of their package
- The exact package list is defined in `docs/SPEC.md`

## Go conventions
- Never ignore a returned error; handle it or return it
- Pass `context.Context` as the first parameter of anything that does I/O (files, processes, language servers, debug adapters, network). Name it `ctx`. Name a Glutter `BuildContext` `bc`
- Tests are table-driven
- No global mutable state and no singletons. Package-level read-only values are fine and are never reassigned
- Prefer the standard library. Allowed dependencies: `github.com/rm4n0s/glutter` (v1.4.0 or newer), `github.com/rm4n0s/errors`, `github.com/dlclark/regexp2` (v1.12.0 or newer, only in `internal/core/regex`), and what Glutter brings in (`golang.org/x/text`, `golang.org/x/image`, `golang.org/x/sys`, `github.com/go-text/typesetting`). A step that first imports one of them adds it with `go get <module>@<version>`. Ask before adding any other
- No network access: never import `net/http`; `net` only in `internal/core/ipc` (SPEC §12)
- **No cgo**: never write `import "C"`. If a C library is ever needed, ask first
- Only Glutter's UI thread touches widgets and render objects. Other goroutines (file watchers, language servers, search) hand work over through `SetState` or the binding's `Post`
- Long-running work (search, indexing, language-server requests) never blocks the UI thread, and takes a `ctx` so it can be cancelled

## Glutter
- Follow Glutter's API conventions: `NewX(required..., opts ...XOpts) *X` constructors, options structs, `foundation.Opt[T]` for nullable options. Emetos's own widgets follow the same conventions
- When Glutter is missing something or has a bug, work around it in Emetos without stopping, and mark the workaround with a `// Glutter:` comment that says what it works around and what Glutter should provide instead
- Keep a list of these workarounds in `docs/GLUTTER-ISSUES.md` (what is missing or broken, where the workaround is, and what the fix in Glutter would be), and mention new entries in the report at the end of the task, so they can be fixed in Glutter later. Create the file with the first entry; remove an entry when the fix is in the Glutter version Emetos uses, and delete the file when no entry is left
- Read Glutter's source in the Go module cache when its behavior is unclear; its `docs/SPEC.md` describes its design

## Error handling
Use `github.com/rm4n0s/errors` for all errors. Never use `fmt.Errorf`, and never import the standard library `errors` package: this package re-exports `errors.Is`, `errors.As` and `errors.Unwrap`, so a single import covers everything.

**Creating errors**
- New failure with a fixed message: `errors.New("Tag", "message", "key", value, ...)`
  - Tag: short PascalCase name of the failure, e.g. `FileOpenFailed`
  - Trailing arguments are key/value metadata pairs, e.g. `"path", path`
- New failure with a formatted message: `errors.NewErrf("Tag", "format %s", arg)`
- Wrapping an error that is NOT already an `*errors.Error` (stdlib, third-party, Glutter errors that are not from this package): `errors.NewErr("Tag", err)`. Add metadata with one `.SetMetadata("key", value, "key2", value2)` call (a second call replaces the metadata of the first)
- Every `New`, `NewErr` and `NewErrf` call captures the stack at that point

**Propagating errors**
- If the error already came from this package, return it untouched: `return err`. Re-wrapping it would replace the Tag and the stack and break route tests
- Functions return the plain `error` interface, never `*errors.Error` (avoids the non-nil-interface-holding-nil-pointer bug)
- `errors.FromError(err)` is a plain type assertion. It fails if something else wrapped the error, so don't wrap these errors with anything

**Inspecting errors**
- `appErr, ok := errors.FromError(err)`, then use `appErr.Tag`, `appErr.Metadata`, `appErr.Message`
- Use `errors.Is` / `errors.As` / `errors.Unwrap` for the underlying cause stored in `OriginalErr`

**Testing errors**
- Every new failure case gets its own unique Tag and a test that asserts the route, not just that an error occurred:
  ```go
  appErr, ok := errors.FromError(err)
  if !ok {
      t.Fatalf("expected *errors.Error, got %T", err)
  }
  if !appErr.HasRoute("Workspace.Open->Buffer.Load.FileOpenFailed") {
      t.Errorf("unexpected failure path: %s", appErr.Route())
  }
  ```
- A route lists the call chain from outermost to innermost (`Type.Method` or `Func`, joined by `->`) and ends with the Tag. `HasRoute` matches by substring
- Log errors with `appErr.ToJson()` so they can be replayed in a test via `errors.FromJson`

## Git and GitHub
- Never commit or push directly to `main`; work on a branch named `feat/...` or `fix/...`
- **Exception:** if `docs/IMPLEMENTATION.md` exists, follow its Rules section for the steps it lists. Each step is done on its own branch, one at a time, and a finished step (all four Definition-of-done checks passing) is published to `main` by a fast-forward merge and push, without a PR, after its entry is added to `docs/IMPLEMENTED.md` on that branch. Never commit work for a step directly on `main`. Everything not covered by that file (other changes, fixes outside the listed steps) still follows the rule above
- Commit messages: imperative mood, e.g. "Add retry to client"
- PRs: short description, link the issue (`Closes #123`), keep them small
- No `.github/` directory and no GitHub Actions workflows: never add them. The four Definition-of-done checks are run locally

## Working through the steps
- Don't stop to ask for confirmation, and don't end a turn with a question such as "Should I start the next step?", until every step in `docs/IMPLEMENTATION.md` is finished (through the last one). After a step is published, report it briefly and start the next step in the same turn
- The only reasons to stop are the ones `docs/IMPLEMENTATION.md` names: its rule 8 (an ambiguous step, a wrong spec, a missing tool, a new dependency, `sudo`, a check that keeps failing), a failed push (rule 13), and a step that says **ask the user**. Then stop, ask, and continue with the same step after the answer
- **Manual checks** don't stop the work: add them to `docs/MANUAL-CHECKS.md` (create it if needed) with the step id and the exact command for the user, write "pending, see docs/MANUAL-CHECKS.md" in the step's `IMPLEMENTED.md` entry, and go on. When the user answers, record the answer in that entry and remove the item from the list (delete the file when it is empty)
- A request such as "publish to main" that is already satisfied gets a one-line answer, then work continues with the next step

## Don't
- Don't edit generated files (`*_gen.go`); regenerate them with `go generate ./...`
- Don't commit secrets or `.env` files
- Don't let a Glutter type appear in the editor core's packages
- Don't leave processes running: every editor instance, compositor, language server or debug adapter started for a test, a proof or a check is stopped when it is no longer needed (run it under `timeout -k`, in its own process group), and `pgrep -a` shows nothing left before a task is reported done
- Never look for, list, open or read files outside this project folder (no home-directory databases, configs, logs or other projects). If something outside it is needed, ask the user to provide it. Exceptions: dependency source in the Go module cache (including Glutter's), and temporary folders your own tests and scripts create
- Never let a test, proof or check use the user's real Emetos configuration, state, cache, trash or socket: Go tests isolate the `XDG_*` folders (the helper of `internal/core/storage`), and scripts run the app through `scripts/isolated.sh` or `scripts/proof.sh` (SPEC §15.1)

## Testing notes
- The editor core is tested with plain Go tests; no UI needed
- Widget tests use `gluttertest` (`PumpWidget`, `Pump`, `Tap`, finders) with a fake clock and the fake platform; no display needed
- Golden tests use `gluttertest`'s `MatchesGolden`, which renders through Glutter's software renderer, so they are deterministic and need no GPU
- Tests that open real windows carry the `emetos_display` build tag and are skipped by the default test run; proofs and display tests run in a headless compositor, never on the user's desktop unless a step says so
- UI tests use the test fonts (Go Mono, Codicons, Seti) through `internal/gui/testutil`, never system fonts
- Tests that start language servers or debug adapters use fakes by default; tests against real servers (e.g. `gopls`) carry a build tag of their own and are skipped when the server is not installed
- Every error Tag has a route test
