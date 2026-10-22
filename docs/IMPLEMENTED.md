# Implemented steps

This is the log of finished steps from `docs/IMPLEMENTATION.md`, newest at the bottom. An agent adds an entry on the step's branch, as part of the step's commit (`docs/IMPLEMENTATION.md`, rules 12–14). The next step to do is the first step in `docs/IMPLEMENTATION.md` that has no entry here.

## Entry format

Copy this block, fill it in, and keep every heading. Write "none" where something doesn't apply.

```markdown
## <id> <step title>

- **Date:** YYYY-MM-DD
- **Branch:** feat/step-<id>-<slug>
- **Summary:** 2–4 sentences on what now exists and works.
- **Main files:** the packages or files created or changed.
- **Tests:** what was added; the number of VS Code test cases ported and skipped (and from which files).
- **Checks:** gofmt ✅ · go vet ✅ · golangci-lint ✅ · go test -race ✅ · CGO_ENABLED=0 build ✅ · CGO_ENABLED=0 test ✅ · display tests ✅/none
- **Proof:**
  ![<id> <name>](implemented-proof-images/<id>-<name>.png)
  One sentence describing what the image shows, as you verified it by viewing it.
- **Manual checks:** the user's answers to the step's manual checks, "pending, see docs/MANUAL-CHECKS.md", or "none".
- **Glutter issues:** entries added to docs/GLUTTER-ISSUES.md, or "none".
- **Deviations and follow-ups:** differences from SPEC or VS Code, and TODOs left for later steps.
```

---

<!-- Entries start below this line. -->

## 0.1 Module and package layout

- **Date:** 2026-10-23
- **Branch:** feat/step-0.1-module-and-package-layout
- **Summary:** The repository has all 44 packages of SPEC §5.2 that the plan fills, each with a `doc.go` that states its purpose and SPEC section, and an empty `cmd/emetos` main. `.golangci.yml` (standard linters plus `revive`'s `exported` and `package-comments`, `reference` and `testdata` excluded) and `internal/archtest` enforce SPEC §5.3: the core never imports Glutter or the GUI, no `net/http`/`net/rpc`/`crypto/tls`, `net` only in `internal/core/ipc`, no `import "C"`, `regexp2` only in `internal/core/regex`, no standard `errors`, no `fmt.Errorf`. `NOTICE` and `third_party/README.md` list the assets and the step that adds each.
- **Main files:** `internal/*/doc.go` (44), `cmd/emetos/main.go`, `.golangci.yml`, `internal/archtest/{archtest.go,archtest_test.go,testdata/}`, `NOTICE`, `third_party/README.md`, `go.mod`, `go.sum`.
- **Tests:** `internal/archtest`: 10 table cases over fixture trees (a clean tree, and one violating tree for each of the 7 rules, a shadowed `fmt` name, and skipped `reference`/`testdata` folders), `TestRepository` (the real repository has no violation), and 2 route tests (`Check.ArchWalkFailed` for a missing root, `Check.ArchParseFailed` for a file that does not parse). No VS Code tests ported (none apply).
- **Checks:** gofmt ✅ · go vet ✅ · golangci-lint ✅ (and it flags an undocumented exported function, checked by hand) · go test -race ✅ · CGO_ENABLED=0 build ✅ · CGO_ENABLED=0 test ✅ · display tests none
- **Proof:** none
- **Manual checks:** none
- **Glutter issues:** none
- **Deviations and follow-ups:** `go mod tidy` removed the `glutter` and `regexp2` requirements because nothing imports them yet; step 0.4 adds `glutter@v1.4.0` and step 4.1 adds `regexp2`. Only the exact package `net` is restricted to `internal/core/ipc`; `net/url`, `net/netip` and the other sub-packages are allowed (they open no connections). The file that does not parse is written to a temporary folder by the test, not committed, because `gofmt -l` recurses into `testdata` and would report it. The `internal/core/ipc` rule will need `net` allowed for the language-server packages when area 3 is planned.

## 0.2 Reference sources and asset scripts

- **Date:** 2026-10-23
- **Branch:** feat/step-0.2-reference-sources-and-asset-scripts
- **Summary:** `scripts/fetch-reference.sh` reads the pins in `scripts/reference-versions.txt` and builds `reference/` (git-ignored): sparse, shallow checkouts of VS Code 1.143.0 (`src/vs`, `extensions`), vscode-textmate, Emmet, vscode-go and the `@vscode/codicons` npm package. A first fetch takes about 23 s and 460 MB (VS Code is 392 MB); a second run takes 16 ms. `reference/go.mod` makes it a separate Go module, so `./...` never sees it. `scripts/check-references.sh` finds 288 `reference/...` paths in the docs and confirms they all exist; `scripts/update-assets.sh` has the copy helpers and no assets yet.
- **Main files:** `scripts/fetch-reference.sh`, `scripts/check-references.sh`, `scripts/update-assets.sh`, `.gitignore`.
- **Tests:** none for Go (the step has none). By hand: a second fetch changes nothing; `check-references.sh` fails with exit 1 and lists the 3 missing paths when a bogus plain path, a `{a,b}` list with one bad name and a `*` glob with no match are added to `docs/SPEC.md` (then reverted); both scripts fail cleanly when `reference/` is missing; `git check-ignore` confirms the ignore; `go list ./...` shows no `reference` package.
- **Checks:** gofmt ✅ · go vet ✅ · golangci-lint ✅ · go test -race ✅ · CGO_ENABLED=0 build ✅ · CGO_ENABLED=0 test ✅ · display tests none
- **Proof:** none
- **Manual checks:** none
- **Glutter issues:** none
- **Deviations and follow-ups:** `shellcheck` is not installed, so the scripts were not linted by it (`sudo dnf install ShellCheck` if wanted). The npm tarball is not checked against its published hash; the pin is the version and URL. In cone mode, git also checks out the files in VS Code's root (`AGENTS.md`, `package.json`...); they are reference data only and nothing reads them. The Rules say to run `scripts/fetch-reference.sh` when `reference/` is missing, so a fresh clone needs about 460 MB and network access once.

## 0.3 Paths and logs

- **Date:** 2026-10-23
- **Branch:** feat/step-0.3-paths-and-logs
- **Summary:** `core/storage` computes every folder from the XDG variables (`Paths`: config, state, cache and data homes, the runtime folder; profile-aware `ConfigDir`/`StateDir`, `CacheDir`, `LogsDir`, `BackupsDir`, `TrashDir`, `SocketPath`), ignoring empty or relative variables as the XDG specification says. `storagetest.Isolate(t)` points `HOME` and all five XDG variables at fresh folders (the runtime folder under a short `/tmp` path, since socket paths are limited to about 100 bytes) and restores them after the test. `core/logs` writes a `slog` logger to `<logs>/<YYYY-MM-DD>/main.log` (folders 0700, files 0600), moves to the new day's file when the clock's date changes, keeps the last 7 days (today included), and mirrors every line into the `Emetos` channel of an in-memory `Store` (last 10,000 lines per channel, subscribers told of each change) that the Output panel will read.
- **Main files:** `internal/core/storage/{paths.go,paths_test.go}`, `internal/core/storage/storagetest/`, `internal/core/logs/{logs.go,store.go,logs_test.go,store_test.go}`.
- **Tests:** 31 table or scenario cases. Storage: paths with every variable, defaults from `HOME`, relative variables ignored, cleaning, `ResolvePaths` from the real environment; profile folders and 7 unsafe profile names on both methods; `Isolate` sets and restores the environment and removes its folders. Logs: file and channel contents, level filtering, append across runs, file modes, rotation at midnight, 5 retention cases, retention leaving non-day entries alone, retention on rotation, a rotation failure keeping the old file, close behavior, 800 concurrent log lines under `-race`; store: order, copies, trimming (6 sizes), subscribers (order, unsubscribe, no lock held), 4,000 concurrent appends. Route tests for all 8 tags: `HomeDirUnknown`, `ProfilePathUnsafe`, `LogDirEmpty`, `LogDirCreateFailed`, `LogFileOpenFailed`, `LogRetentionFailed` (both a failed read and a failed removal), `LogFileClosed`, `LogFileCloseFailed`. Coverage 92%, 83%, 99%. No VS Code tests ported (none apply).
- **Checks:** gofmt ✅ · go vet ✅ · golangci-lint ✅ · go test -race ✅ (also `-count=10`) · CGO_ENABLED=0 build ✅ · CGO_ENABLED=0 test ✅ · display tests none
- **Proof:** none
- **Manual checks:** none
- **Glutter issues:** none
- **Deviations and follow-ups:** `Isolate` also sets `HOME` (the step says only the XDG variables), so code that asks for the home folder can't reach the real one. Logs are 0700/0600, not the usual 0755/0644, because they hold file paths (SPEC G2). `Paths.RuntimeDir` is empty when `XDG_RUNTIME_DIR` is unset: the XDG specification has no default, and step 7.7 decides what the socket does then. A rotation failure keeps logging into the old day's file and puts a `log: rotating the log failed: ...` line in the channel. Profile names are only checked to be a single folder name (`ProfilePathUnsafe`); the naming rules of SPEC §10.12 come in step 8.6. A subscriber of the `Store` must not log through the `Logs` it watches (documented on `Subscribe`).

## 0.4 First window and proof scripts

- **Date:** 2026-10-23
- **Branch:** feat/step-0.4-first-window-and-proof-scripts
- **Summary:** `go run ./cmd/emetos` opens a 1280×800 Emetos window (`dev.emetos.Emetos`) showing the `code` Codicon and "Emetos" on Dark Modern's `#1F1F1F`, with Codicons and Seti loaded through `RunAppOpts.Fonts`; `--version`, `--help` and `--log <level>` work, the log goes to `core/logs`, and cancelling the context (Ctrl+C) exits the app. Glutter v1.4.0 is now a dependency. `scripts/isolated.sh` runs any command with `HOME` and the XDG folders in a throwaway folder (Go's caches and the Wayland socket stay reachable), and `scripts/proof.sh` builds a debug binary and captures a screenshot in a headless sway, isolated, in 7 s (method B; `--output` is method C). `scripts/headless-wayland.sh` and `scripts/screenshot-headless.sh` are adapted from Glutter's. `internal/gui/kit/fonts` embeds the two fonts, exports `Codicon`/`Seti`, `Sources()` and `CodiconIcon`, and a generator turns `codicon.csv` into 630 `Codicon…` constants.
- **Main files:** `cmd/emetos/main.go`, `internal/gui/app/app.go`, `internal/gui/kit/fonts/{fonts.go,codicon_gen.go,internal/gen/main.go}` (+ the embedded `codicon.ttf`, `codicon.csv`, `seti.woff`), `scripts/{isolated.sh,proof.sh,headless-wayland.sh,screenshot-headless.sh,update-assets.sh}`, `third_party/{codicons,seti,glutter}/`, `NOTICE`, `go.mod`, `go.sum`.
- **Tests:** fonts: both fonts load into a test engine and resolve as loaded fonts (including the WOFF), `CodiconIcon`, an icon builds and lays out without framework errors; generator: name conversion (6), generation, 10 error inputs, the collision tag, 3 `run` errors, and a check that the committed `codicon_gen.go` is what the CSV generates and has a constant for each of the 630 rows; app: the home shows the icon and the name, cancelling the context asks to exit once, a cancelled context opens nothing; command line: 8 argument cases, 5 error cases (routes through `parseArgs`), 5 `run` cases, a broken stdout and stderr, the version string; scripts (in `internal/archtest`): `isolated.sh` hides the user's folders and removes its own, keeps Go's caches, copies `--config`, passes the exit status on, rejects 5 usage mistakes, keeps a compositor socket reachable; `proof.sh` rejects 6 usage mistakes. No VS Code tests ported (none apply). Route tests for the tags `ArgsInvalid`, `LogLevelInvalid` and the 6 `Codicon…` tags.
- **Checks:** gofmt ✅ · go vet ✅ · golangci-lint ✅ · go test -race ✅ · CGO_ENABLED=0 build ✅ (Glutter without cgo) · CGO_ENABLED=0 test ✅ · display tests none
- **Proof:**
  ![0.4 first window](implemented-proof-images/0.4-first-window.png)
  The dark window with the `</>` Codicon in grey and "Emetos" in the desktop's sans-serif font centered below it, as I saw it by viewing the image (`scripts/proof.sh docs/implemented-proof-images/0.4-first-window.png`; generated twice, byte-identical).
- **Manual checks:** none
- **Glutter issues:** none
- **Deviations and follow-ups:** the Seti WOFF loads in Glutter as it is, so no `scripts/woff2ttf` converter is needed. The tests of the scripts are in `internal/archtest` (it holds the repository's rules) instead of a new package, since SPEC §5.2 has none for them. `isolated.sh` exports `GOCACHE`, `GOMODCACHE` and `GOPATH` and links the compositor socket, as the plan's review asked. `proof.sh` waits 1 s before capturing when `--commands` is given. `app.Run` takes a `ctx` that Glutter's `RunApp` does not: cancelling it posts `ExitApplication` from the first page's state. The window text uses `UIFont()` from the desktop and falls back to `sans-serif`; the headless sway has no desktop font settings, so the proof shows the system's default sans. `cmd/emetos` rejects positional arguments (`ArgsInvalid`) until step 3.1 adds `<file>`. When run normally the log goes to the real `$XDG_STATE_HOME/emetos/logs`; proofs and tests redirect it. The `MESA-EGL: failed to get driver name` lines in headless runs are Mesa's, about the missing GPU of the headless compositor. `shellcheck` is not installed, so the scripts were not linted by it.
