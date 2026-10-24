# Performance

Measurements of the targets of SPEC §14. The method and the numbers are here; `scripts/perf.sh` repeats the app-level ones and the Go benchmarks repeat the UI-thread ones.

## Machine

AMD Ryzen AI Max+ 395 (32 threads), 30 GB, Linux 7.3 (Fedora 44), Go 1.27.3, sway 1.11 (headless, pixman) for the compositor, Mesa llvmpipe (LLVM 22.1.8) as the software GLES renderer. The machine has a GPU (Radeon 8060S), which the headless runs don't use: SPEC §14 says the numbers with the GPU, in the user's session, decide, and the headless ones are recorded too.

## Method

- **UI-thread time of a frame** (the build/layout/paint work of the framework thread, which does not depend on the renderer): Go benchmarks in `internal/gui/editor/bench_test.go`, with the fake platform (no rasterization):
  `go test -tags glutter_release ./internal/gui/editor -run xxx -bench . -benchmem`
- **The app**: `scripts/perf.sh [--session] [-n runs] [-s seconds]` builds with `-tags glutter_release,emetos_dev`, generates a 10,000-line Go-like file, a 100,000-line file and a 10 MB file in a temporary folder, runs each measurement in a fresh app isolated from the user's configuration (`scripts/isolated.sh`) with `GLUTTER_FRAME_STATS=1` (Glutter logs once a second the frames drawn, the average and worst build and raster times and the frames over 16 ms; the first presented frame is logged with the time since the process started), and drives the editor with the dev commands `dev.type` (types a text, one character per frame) and `dev.scroll` (scrolls by rows, one step per frame). Glutter's `counter` example is built and measured the same way as the baseline of a window with nothing in it. The idle CPU is read from `/proc/<pid>/stat` over the measuring time, the memory from `VmRSS`. The Mesa shader cache is kept across the runs (the isolated home would empty it each time), after a first run that fills it.
- Headless by default: `scripts/headless-wayland.sh` with sway at 1920x1080, scale 1.25, windows tiled over the output. With `--session`, in the caller's Wayland session with its GPU.

## Results

Go benchmarks (UI thread only, one frame of work):

| Benchmark | Time per frame | Allocations |
|---|---|---|
| Typing a character in a 10,000-line file (edit, layout of the changed line, paint) | 0.08 ms | 754 |
| Scrolling a page in a 100,000-line file | 0.45 ms | 3,857 |
| Opening a 10 MB file (read, buffer, first layout) | 23.8 ms (442 MB/s) | 149,862 |
| Opening a 200 MB file in large file mode (read, buffer, first layout; step 5.12) | 361 ms (582 MB/s), 1.15 GB allocated | 202,552 |

App, headless (software rendering), `scripts/perf.sh` with 5 starts and 10 s of idle:

| Metric | Target (SPEC §14) | Measured | |
|---|---|---|---|
| Typing in a 10,000-line file: UI-thread time of the frame | < 4 ms | build 0.28 ms on average, 0.90 ms worst of 165 frames | met |
| Scrolling a 100,000-line file | no UI-thread frame over 8 ms | build 0.56 ms on average, 2.2 ms worst of 297 frames | met (UI thread) |
| Opening a 10 MB file: first frame with text | < 300 ms | 890 ms from the start of the process, 30 ms more than the empty window (the first frame builds in 3.5 ms) | met in Emetos's share; the total is the start below |
| Opening a 200 MB file in large file mode: first frame with text | < 3 s | 361 ms for the read, the document and the first layout on the UI thread (`BenchmarkOpen200MB`: 2.1 million lines, no tokenization, folding, minimap or wrapping); the process start comes on top as in the row above | met in Emetos's share |
| Cold start, no folder | < 400 ms | 860 ms (median of 5); Glutter's counter, same run: 940 ms | **not met headless**: the first draw of the software renderer is 0.85 s (`docs/GLUTTER-ISSUES.md`) |
| Idle, window focused, cursor blinking | < 1% of a core | 0.5% to 0.8% (3 runs, 2 frames a second) | met |
| Idle, window without focus | no frames at all | not measurable headless (no keyboard focus events); the logic is tested with the fake platform | pending `--session` |
| Resident memory, one 10,000-line file | counter + 50 MB (revised, SPEC §14) | 271 MB; Glutter's counter: 287 MB | met |

Notes:

- The raster times of the headless runs (14 ms on average, 800 ms for the first frame) are the software renderer's and say nothing about Emetos; the few frames over 16 ms in these runs are raster time too (the UI-thread worst case is under 3 ms).
- In three early runs of the idle measurement 20 to 30 threads of llvmpipe used up to 30% of a core while the app drew two frames a second; the later runs, with a longer settling time before the measurement, show 0.5%. In one run split by thread, the threads that are not llvmpipe's used 0.4% of a core. Treat the headless idle figure as indicative and take the one from `--session`.
- The memory target was "< 200 MB", chosen before the Glutter baseline was known. An empty Glutter window with software GL is 287 MB, so the target now is the baseline plus 50 MB. The memory was not split further than this: the Go heap of Emetos with the 10,000-line file is 9 to 16 MB (`go_heap_mb` in Glutter's log), so the rest is outside it (graphics stack, fonts, buffers).
- Step 4.9 (highlighting), Go benchmarks with `-tags glutter_release`: typing a character in a 10,000-line highlighted file takes 0.62 ms per frame (the tokenizer re-tokenizes from the edited line and stops when a line's end state is unchanged). Tokenizing Go costs about 160 µs per line (`BenchmarkTokenizeGoLines`) after the scanner result cache in `core/regex`. Scrolling a page of lines nobody has tokenized yet, in a 100,000-line file, takes 8.2 ms of UI-thread time, 6 ms of which is the synchronous tokenization budget (`tokenBudget`); lines it does not finish are tokenized by the background loop and painted plain until the next frame. That is above the 8 ms target for the first view of unseen lines only; lines already tokenized scroll as before. Do not run `BenchmarkScrollPage100kLinesHighlighted` with a large `-benchtime`: every iteration tokenizes new lines and the retained tokens grow with it (use `-benchtime 50x`).
- Step 4.12 (bracket pairs), Go benchmarks with `-tags glutter_release`, on a synthetic file of 100,000 lines and 600,000 brackets: the first pairing takes 49 ms, the first question after an edit 18 ms (30 ns for each bracket: the lines the edit touched are read again and all the brackets are paired in one pass), and the brackets of a screen of 50 lines 5 µs once the pairs are made. A file of 10,000 lines with 60,000 brackets costs about 2 ms after an edit. The pairing is lazy, so it costs nothing while nobody asks (large file mode has no index). Pairing only from the first edited line, and stopping when the open pairs are the same as before, would make an edit cost the edit and not the file; it is left for when a measurement of the editor needs it.

- Step 5.12 (large files): `BenchmarkOpen200MB` writes a file of 200 MB (about 2.1 million Go-like lines) and measures `fileio.Read`, `document.FromRead`, the view model and the first frame. It needs about 1.2 GB, so it runs alone and once: `GOMEMLIMIT=3GiB go test -tags glutter_release ./internal/gui/editor -run xxx -bench Open200MB -benchtime 1x -benchmem`. A line longer than `editor.stopRenderingLineAfter` is laid out up to that many bytes only; `TestAHugeSingleLineStaysResponsive` shows and moves in a single line of 5 MB (with word wrap on) in a fraction of a second.

## Fuzzy scorer (step 6.16)

Target (SPEC §14): fuzzy file search over 100,000 paths, per keystroke, under 30 ms, off the UI thread. The scorer is `internal/core/fuzzy`; the benchmarks are in `internal/core/fuzzy/bench_test.go` and score 100,000 generated paths (seeded, 2 to 6 folders and a file name each, 4.2 MB of strings, 42 bytes a path on average: `tests/workbench/fixtures/language/electron-node/theme.go` is the kind of thing). One operation is one keystroke. Run:

`GOMEMLIMIT=2GiB go test ./internal/core/fuzzy -run '^$' -bench . -benchmem -benchtime=20x`

Machine of this run: `go1.27.3 linux/amd64`, `cpu: AMD RYZEN AI MAX+ 395 w/ Radeon 8060S` (the benchmark's own line; one goroutine, 32 threads idle), sway not involved. Times are per operation (the 100,000 paths against one query), measured with 20 operations.

| Benchmark | Query | Time | Allocations | Matches |
|---|---|---|---|---|
| `Search100k` (score without a cache, sort the matches with a cache for them: the quick open) | `edtvw` | 18.8 ms | 1.4 MB, 23,401 | 1,556 |
| | `gui/exp/bo` | 11.9 ms | 51 KB, 859 | 56 |
| | `view model` | 19.5 ms | 1.4 MB, 48,007 | 486 |
| | `zqxjk` (no match) | 10.1 ms | 64 B, 2 | 0 |
| `ScoreItems100k` (score all with a new cache, as VS Code does) | `edtvw` | 26.9 ms | 3.1 MB, 12,669 | 1,556 |
| | `gui/exp/bo` | 16.7 ms | 2.4 MB, 710 | 56 |
| | `view model` | 23.0 ms | 3.5 MB, 42,271 | 486 |
| | `zqxjk` | 14.2 ms | 2.4 MB, 259 | 0 |
| `ScoreItems100kNoCache` (the scoring alone) | `edtvw` | 13.3 ms | 0.6 MB, 10,853 | 1,556 |
| | `gui/exp/bo` | 11.7 ms | 22 KB, 395 | 56 |
| | `view model` | 16.8 ms | 1.1 MB, 41,530 | 486 |
| | `zqxjk` | 8.9 ms | 0, 0 | 0 |

Result: **met**, 9 to 19 ms for the scoring and sorting of a keystroke (target 30 ms), 9 to 17 ms for the scoring alone; every case is also under 30 ms with the cache. The scoring is nearly allocation free after the first call (the allocations are the matches of the items that match). A `Scorer` is used by one goroutine; the search of step 6.18 splits the paths over several scorers (see "Quick open (step 6.18)"). `TestScoring100kPathsWithinTarget` checks a bound of 600 ms (twenty times the target, so that a busy machine and the race detector, which is four times slower, do not make it flaky); it is skipped with `-short`.

What made it fast, and what to keep in mind:

- A query that is not a subsequence of the label or of the path cannot score. VS Code fills the dynamic programming matrix first and gets 0; `fuzzy` checks the subsequence on the bytes of ASCII strings before it makes runes (`mayContain`), and on the runes before it fills the matrix (`isSubsequence`). The results are the same (`TestQuickRejectKeepsResults`); a search where most paths do not match costs about 90 ns a path.
- The matrix, the tables and the rune slices belong to the `Scorer` and are reused. A matrix over 65,536 cells is not kept.
- The cache (`fuzzy.Cache`) is for sorting: scoring all 100,000 items into a cache costs a map insert for each (5 ms to 14 ms for 100,000, 16 bytes for an item that does not match). The quick open scores without a cache and gives the sort a cache for the matches only (`Search100k`).

## Quick open (step 6.18)

Target (SPEC §14): fuzzy file search over 100,000 paths, per keystroke, under 30 ms, off the UI thread. The search is `workspace.QuickOpen` (`internal/core/workspace/search.go`, `quickopen.go`): it takes the list of the files from the `FileIndex`, scores every path with the scorer of step 6.16 on up to eight goroutines (a `fuzzy.Scorer` each, chunks of at least 8,192 paths, the context looked at every 256), keeps the paths whose score is at least the score of the 512th, sorts those with VS Code's comparison and makes the rows (the matches to highlight). The benchmarks are in `internal/core/workspace/index_bench_test.go`: `BenchmarkQuickOpen100k` is one keystroke over an index of 100,000 generated paths in memory (the generator of `core/fuzzy`'s benchmarks: seeded, 2 to 6 folders and a file name), end to end, with one goroutine (`Workers: 1`) and with all of them (`parallel`); `BenchmarkIndexWalk` lists a tree on disk (`t.TempDir()`, about 5,000 files in folders of 25, three levels deep, with a `.gitignore` that hides `*.log` and `build/` and an excluded `node_modules`: 4,401 files are listed). Run:

`GOMEMLIMIT=2GiB go test ./internal/core/workspace -run '^$' -bench . -benchmem -benchtime=3x`

Machine of this run: `go1.27.3 linux/amd64`, `cpu: AMD RYZEN AI MAX+ 395 w/ Radeon 8060S` (the benchmark's own line; 32 threads), no display involved. Times are per operation.

| Benchmark | Query | One goroutine | All goroutines | Rows | Allocations (all goroutines) |
|---|---|---|---|---|---|
| `QuickOpen100k` | `edtvw` | 22.0 ms | 4.7 ms | 512 | 1.1 MB, 15,531 |
| | `gui/exp/bo` | 18.7 ms | 2.7 ms | 56 | 191 KB, 1,138 |
| | `view model` | 26.5 ms | 5.3 ms | 486 | 1.7 MB, 48,907 |
| | `e` (matches most paths) | 24.1 ms | 7.6 ms | 512 | 8.2 MB, 225,293 |
| | `zqxjk` (no match) | 9.4 ms | 1.9 ms | 0 | 2 KB, 26 |

| Benchmark | Time | Files | Rate |
|---|---|---|---|
| `IndexWalk` (4,401 files, 200 folders, ignore files) | 4.4 ms to 5.5 ms | 4,401 | 800,000 to 1,000,000 files/s |

Result: **met**. A keystroke takes 2 ms to 8 ms with the goroutines and 9 ms to 27 ms with one (target 30 ms), so the target holds even on a machine with one core, though the 27 ms of "view model" is close. The search runs on goroutines, never on the UI thread; the UI thread only takes the finished rows (`Picker.SetItems`, 512 rows at most). `TestQuickOpen100kWithinTarget` checks a bound of 600 ms (twenty times the target, so that a busy machine and the race detector do not make it flaky); it is skipped with `-short`.

Listing: at this rate 100,000 files take about 0.1 s (an extrapolation, not a measurement: the benchmark does not write 100,000 files), so the first rows of a search are there at once (the search runs again while the walk goes, at most ten times a second) and the list is complete within a fraction of a second for a repository of that size on a warm file cache. A cold disk is the file system's cost, not the walk's.

What made it fast, and what to keep in mind:

- A score decides the order whenever two scores differ, so the sort only needs the paths whose score is at least the score of the last place (512 of up to 60,000 matches): `Search100k` of step 6.16 sorted every match with a cache for them. The matches of the paths that are kept are made once, at the end.
- Every goroutine scores into its own slice of the matches (the place and the score), and the slices are joined in the order of the list, so the result does not depend on the number of goroutines (`TestSearchPathsIsDeterministicAndCapped`).
- The `e` query allocates the most (225,000 allocations): every path that matches makes the slices of its matches during the scan. A scorer that only returns the score would halve it; not needed for the target.
- The walk keeps one string for each file. The ignore rules are checked for every entry (`glob.Ignore.Check`: a few regular expressions of the files that apply); the benchmark has two patterns and one ignore file, so a tree with dozens of ignore files in the path of every file costs more.

## Pending

`scripts/perf.sh --session` with the GPU for every row above, in `docs/MANUAL-CHECKS.md`.
