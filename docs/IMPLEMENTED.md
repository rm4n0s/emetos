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
