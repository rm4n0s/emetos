# Performance

Measurements of the targets of SPEC §14. The method and the numbers are here; `scripts/perf.sh` repeats all of them (the app-level ones, the Go benchmarks of the UI thread alone, the fuzzy file search and the loading of the grammars) and prints them as Markdown tables.

## Machine

AMD Ryzen AI Max+ 395 (32 threads), 30 GB, Linux 7.3 (Fedora 44), Go 1.27.3, sway 1.11 (headless, pixman) for the compositor, Mesa llvmpipe (LLVM 22.1.8) as the software GLES renderer. The machine has a GPU (Radeon 8060S), which the headless runs don't use: SPEC §14 says the numbers with the GPU, in the user's session, decide, and the headless ones are recorded too. The machine is shared with the user's desktop (its swap was full and other programs ran during the runs), so single numbers move by 5% to 10% from run to run; the tables below are the medians of several runs where the script takes several.

## Method

- **UI-thread time of a frame** (the build/layout/paint work of the framework thread, which does not depend on the renderer): Go benchmarks in `internal/gui/editor/bench_test.go`, with the fake platform (no rasterization):
  `go test -tags glutter_release ./internal/gui/editor -run xxx -bench . -benchmem`
- **The app**: `scripts/perf.sh [--session] [-n runs] [-s seconds] [--no-go] [--no-huge] [--profile dir]` builds with `-tags glutter_release,emetos_dev`, generates in a temporary folder a 10,000-line Go-like file, a 100,000-line file, a 10 MB file, a 200 MB file (4.6 million lines) and four folders of 30 files, runs each measurement in a fresh app isolated from the user's configuration (`scripts/isolated.sh`) with `GLUTTER_FRAME_STATS=1` (Glutter logs once a second the frames drawn, the average and worst build and raster times and the frames over 16 ms; the first presented frame is logged with the time since the process started and since `Run` began), and drives the editor with the dev commands `dev.type` (types a text, one character per frame), `dev.scroll` (scrolls by rows, one step per frame), `dev.openFolderInNewTab` and `dev.openFile`. Glutter's `counter` example is built and measured the same way as the baseline of a window with nothing in it. The idle CPU is read from `/proc/<pid>/stat` over the measuring time, the memory from `VmRSS` and `smaps_rollup` (the anonymous part is the Go heap and the driver's), and the Go runtime's from the `go_sys_mb` of Glutter's log.
- **What counts as a frame of typing or scrolling**: the dev commands begin 3 s after the first frame is drawn and the measuring time 2 s after it is presented, so that the first frame (which builds the whole window) and Glutter's first statistics line are out of it. The run is taken three times and the one with the median average build time is reported (one run is at the mercy of what else the machine does).
- **The parts of the start**: "Emetos before the window is made" is the time from the start of the process to the start of `Run` (the Go runtime, the package initializers, the process, and every workspace of the plan: the tabs restored, the files of the command line, their documents read), which is `since_process_start - since_run` of Glutter's first-frame line; "build of its first frame" is the build time (build, layout and paint on the framework thread, not the raster) of the first frame, which Glutter logs as a slow frame. The rest of the start is Glutter's renderer: the table of the parts of the first draw below comes from its startup lines.
- **The renderer**: headless, `scripts/perf.sh` and `scripts/proof.sh` set `GLUTTER_RENDERER=gles` unless the environment sets it (Glutter's `auto` tries Vulkan first and gives it up after about 600 ms where the compositor presents no GPU buffers; `GLUTTER_RENDERER=auto scripts/perf.sh` measures that start). With `--session` nothing is set and the user's GPU decides. The results of step 10.1 below were made before this, with `auto`.
- **The cold start** is measured in three states of the shader caches (Glutter 1.7 keeps GLES program binaries in `$XDG_CACHE_HOME/glutter/shaders`, and Mesa has its own cache): both empty, Mesa's warm and Glutter's empty, and both warm. `scripts/isolated.sh` gives every run a fresh `XDG_CACHE_HOME`, so `scripts/perf.sh` gives the runs that need a warm cache of Glutter's a cache folder of its own work folder (`PERF_CACHE`) and moves Mesa's the same way (`MESA_SHADER_CACHE_DIR`); a first start of each app fills them before the warm runs are measured. **The start with the session restored** runs the editor once with dev commands that open four folders as tabs with three files each and ends it with `SIGTERM`, in the same isolated folders, then measures the next start with nothing on the command line (three times, both caches warm).
- **The idle CPU without the focus** is measured headless too: Glutter 1.7 takes the focus of a window from the active window of the compositor, so a second window (the counter) started next to the editor takes it.
- **The fuzzy search and the grammars** are Go benchmarks that `scripts/perf.sh` runs and summarizes (`internal/core/fuzzy`, `internal/core/workspace`, `internal/core/language`). The 200 MB file is opened in the app, not in a Go benchmark, by the script (`BenchmarkOpen200MB` is still the UI thread alone: `GOMEMLIMIT=3GiB go test -tags glutter_release ./internal/gui/editor -run xxx -bench Open200MB -benchtime 1x -benchmem`, alone, it needs 1.2 GB).
- **Profiling**: a build with the `emetos_dev` tag profiles itself (`cmd/emetos/profile_dev.go`, SPEC §15.2): `scripts/perf.sh --profile <dir>` writes, for each run of the editor, a CPU profile of its measuring time and the heap profile after it to `<dir>/<run>/` and copies the binary to `<dir>/emetos`: `go tool pprof -top -cum <dir>/emetos <dir>/scrolling-1/cpu-1.pprof`. By hand: set `EMETOS_PROFILE_DIR`, send `SIGUSR2` to start and again to end a CPU profile, `SIGUSR1` for the heap; `EMETOS_PROFILE_START=cpu` profiles the start. To see where the resident memory goes: `awk` over `/proc/<pid>/smaps` by mapping, the heap profile (`-sample_index=inuse_space`) and `memstats-N.txt`; a build with a `println` in Glutter's `Collection.load` (a copy of the module through `go build -modfile`) tells which font files are parsed.
- Headless by default: `scripts/headless-wayland.sh` with sway at 1920x1080, scale 1.25, windows tiled over the output. With `--session`, in the caller's Wayland session with its GPU.

## Results of step 10.1 (headless)

"Before" is the commit before the step (`c9ccdb6`) and "after" the step's branch, both measured with the same script (`scripts/perf.sh -n 5 -s 10`) a few minutes apart on the same machine. The step found and fixed four things that were Emetos's own (below); the targets still missed are missed for reasons that are not Emetos's or that the headless run cannot judge, and each has a proposed decision.

| Metric | Target (SPEC §14) | Before | After | |
|---|---|---|---|---|
| Typing in a 10,000-line file: UI-thread time of the frame | < 4 ms | build 2.37 ms on average, 4.73 ms worst of 145 frames (the Go benchmark: 0.15 ms) | 2.50 ms, 4.50 ms of 137 frames (0.17 ms); in the final run 3.03 ms, 7.72 ms of 135 frames (0.14 ms) | **met** on average and in the benchmark; the worst frames are the busy machine (their raster is 25 ms, against 15 ms in a quiet run) |
| Scrolling a 100,000-line file | no UI-thread frame over 8 ms | build 8.33 ms on average, 25.7 ms worst of 272 frames | 1.53 ms, 5.56 ms worst of 271 frames (the final run: 1.70 ms, 6.76 ms of 268) | **met** in the app (fix 2). The Go benchmark of a page of lines nobody has tokenized, 7.2 to 8.3 ms in these runs, was at the line: fix 5 brings it to 4.7 ms |
| Opening a 10 MB file: first frame with text | < 300 ms | 1360 ms from the start of the process, 410 ms more than the empty window; build of the first frame 206 ms | 1050 ms, 110 ms more than the empty window; build 11.7 ms (the final run: 1060 ms, 140 ms more, build 14.7 ms) | **met** as the time over the empty window (fix 1); the absolute time is the start's (proposal 1). Emetos's share: 27 ms to read the file before the window, 12 ms to build the frame (the UI thread alone, the Go benchmark: 24 ms) |
| Opening a 200 MB file in large file mode: first frame with text | < 3 s | 1270 ms (320 ms more than the empty window, all of it reading the file before the window); build 12 ms; resident 1434 MB | 1280 ms (340 ms more, 355 ms reading); build 9.8 ms; resident 1437 MB (1250 ms, 375 ms reading in the final run) | **met** (the file is read at about 600 MB/s before the window; the 1.4 GB resident is 7 times the file, step 5.12 measured 1.15 GB allocated: not a target) |
| Cold start, no folder, both shader caches warm | < 400 ms | 950 ms (the counter 910 ms); Emetos before the window 14 ms, build of its first frame 10.7 ms | 940 ms (the counter 950 ms); 17 ms, 11.3 ms (the final run: 920 ms and 900 ms; 12 ms, 9.2 ms) | **not met headless**, and none of it is Emetos's: proposal 1 |
| Cold start, Mesa's cache warm, Glutter's empty | < 400 ms | 920 ms (the counter 950 ms) | 950 ms (the counter 940 ms); the final run 870 ms (880 ms) | not met headless (same) |
| Cold start, both caches empty (the first start on a machine) | < 400 ms | 4280 ms (the counter 2410 ms) | 4390 ms (the counter 2450 ms); the final run 4720 ms (2550 ms) | not met headless; 3.3 s of it is llvmpipe compiling the frame; the GPU's driver is another story |
| Cold start with the workbench and four restored tabs, both caches warm | < 400 ms | 1220 ms; Emetos before the window 29 ms, build 37 ms; resident 725 MB | 1110 ms; 34 ms, 42 ms; resident 406 MB (the final run: 1100 ms; 32 ms, 45 ms) | not met headless (same); Emetos's share is 76 to 77 ms |
| Idle, window focused, cursor blinking | < 1% of a core | 1.4% (31 frames in 10 s by Glutter's count) | 1.3% (21 frames); the final run 1.0% (20 frames) | not met headless; none of it is Emetos's (fix 3 took the extra frame out; proposal 4) |
| Idle, window without focus | no frames at all | 0 frames, 0.00% | 0 frames, 0.00% | **met** (headless: the focus follows the active window of the compositor) |
| Resident memory, one 10,000-line file | the counter + 50 MB | 738 MB; the counter 280 MB (+458 MB); anonymous 589 MB, 425 MB of it the Go runtime's | 418 MB; the counter 280 MB (+138 MB); anonymous 269 MB, 92 MB the Go runtime's (the final run: 420 MB, +140 MB; Go 88 MB against the counter's 40 MB) | **not met**, 90 MB over (fix 1 took 320 MB away): proposal 3 |
| Fuzzy file search over 100,000 paths, per keystroke, off the UI thread | < 30 ms | 9.0 to 24.4 ms the scorer; 9.4 to 25.3 ms the quick open on one goroutine, 1.6 to 7.8 ms on all | 9.3 to 19.8 ms; 12.5 to 24.2 ms; 2.3 to 8.6 ms (the final run: 9.2 to 22.3 ms; 12.2 to 28.9 ms; 2.6 to 9.9 ms) | **met** (sections "Fuzzy scorer" and "Quick open" below) |

Not a target of §14, measured because the step asks (the grammars and the session):

| What | Measured |
|---|---|
| Making the registry of the bundled languages, at the start, before the window | 1.0 ms (`BenchmarkBundled`) |
| The first file of a language: the grammar on the UI thread, then the first line, which makes the rules and compiles the patterns | Go 2.3 + 2.2 ms, Python 2.1 + 2.2 ms, TypeScript 5.2 + 10.0 ms, HTML 0 + 17.1 ms, Markdown 1.6 + 73 ms (`BenchmarkFirstLine`, the final run; two runs before it were 3 to 8% apart). Before fix 4 the second part ran on the UI thread of the first frame of the first file of a language; now it runs on a goroutine, started before the window is made |
| The start with four tabs restored over the start with none | +170 ms headless: +17 ms before the window (five workspaces made, their documents read), +31 ms to build the frame (explorer, three editors, the side bar of the tab), the rest the renderer drawing a fuller first frame |

The parts of the first draw (last start of each state, headless, Vulkan tried first; Glutter 1.7 logs them with `GLUTTER_FRAME_STATS=1`):

| Part of the first draw (ms) | Both caches empty | Mesa's warm | Both warm |
|---|---|---|---|
| Renderers tried that failed (Vulkan finds no presentable GPU on the headless compositor) | 588 | 615 | 621 |
| GPU context | 139 | 153 | 170 |
| Shaders | 220 (3 built) | 61 (3 built) | 57 (3 from Glutter's cache) |
| The frame itself | 3267 | 38 | 36 |
| First draw, all of it | 4214 | 869 | 885 |

What this says: the 400 ms target is out of reach headless for a reason that is not Emetos's: 560 to 620 ms of every start is the Vulkan renderer being tried and given up (`VulkanSwapchainFailed`) before the GLES renderer is used, and Glutter's own counter starts as slowly as the editor in the same runs (950 ms against 940 ms). Emetos's share of the start, from the start of the process to the first frame being built, is 12 to 17 ms + 9 to 11 ms with no folder and 32 to 34 ms + 42 to 45 ms with four tabs restored: 3% to 8% of the start. Mesa's own cache is what separates the 4.4 s of the first start from the 0.9 s of the next (the llvmpipe compile of the frame, 3267 ms against 36 ms). Whether `auto` should try GLES first where Vulkan presents to no output was Glutter's decision, and its owner decided to keep Vulkan first (Glutter's SPEC §9.6); with `GLUTTER_RENDERER=gles` Glutter measured 320 ms against 960 ms, and so did Emetos after Glutter 1.8 ("Results after Glutter 1.8" below). The figure that decides is the GPU's, `scripts/perf.sh --session`.

### What step 10.1 found

1. **The tab loaded every font of the system (Emetos's workaround for Glutter; the memory and the 10 MB open).** Glutter looks for a face with a glyph for each character, in the order of the style's font files, and when no font has one it loads and parses all of them before the system's fallback. A tab is in no font, so the first row with a tab (every Go file) made Glutter parse the 62 files fontconfig lists for `monospace` on this machine: 116 MB on disk, **170 MB of live Go heap, 300 MB more resident memory** (849 MB against 525 MB with the same file) and, at the first frame of a file, 160 to 240 ms of build time (the 10 MB file's first frame: 206 ms before, 12 ms after). Found with the heap profile (100 MB in `opentype.(*Loader).findTableBuffer` under `styleFonts.ResolveFace`) and a copy of Glutter that printed the font files it parsed. Fix at the time: `fonts.ControlsFont`, a 288-byte TrueType font with an empty glyph for U+0009, loaded with the icon fonts and put first in the families of a row that has a tab (`textStyle.families`); widths and heights were unchanged (`TestTheFontOfTheTabChangesNoMeasure`, now `TestATabIsATabStopWide`, and every golden of the editor was unchanged). The same waste would come from any character that no font has. **Glutter 1.8 (28.16) fixed this itself and the workaround is gone** ("Results after Glutter 1.8" below).
2. **Every slice of the tokenizer made the next frame pair the whole document's brackets (scrolling).** `brackets.Index.Invalidate` forgot the lines whose tokens arrived and the next paint pairing (`ensureLocked`, 10 ms for 100,000 lines; a CPU profile of `dev.scroll` put 72% of the UI thread's Emetos time in it) started from scratch, for each of the tokenizer's slices: while a big file is tokenized, every frame cost 10 ms more. Tokens usually leave a line's brackets as they were (they only take away the ones in strings and comments), so `Invalidate` now only notes the lines (from the tokenizer's goroutine, without reading the document), and the next question finds their brackets again on the UI thread and keeps the pairs when they are the same (`TestNewTokensKeepThePairsWhenTheBracketsAreTheSame`, `TestInvalidateReadsNothingFromTheSource`, `TestNotedLinesFollowAnEdit`). Scrolling: build 8.33 ms on average and 25.7 ms worst before, 1.53 ms and 5.56 ms after.
3. **A line of the log drew a frame of the whole window (the idle frames).** The Output panel follows the end of its channel with a frame it asks for, also while the panel is closed. Every line of the log, which with `GLUTTER_FRAME_STATS=1` is the statistics line each second, drew a frame: 3 frames a second for a blinking cursor that asks for 2 (a `ScheduleFrame` trace shows one frame at each 500 ms and one 10 ms after each statistics line). It is not what a user sees, a closed panel asking for frames after a log line is, so the panel now asks only when it is shown (`Visible` of the view; `TestAHiddenPanelAsksForNoFrames`, `TestIdleWorkbenchDrawsOnlyTheBlink`). The idle numbers of the earlier steps (30 frames in 10 s) counted that frame; now 20 to 21.
4. **The rules of a big grammar were made on the UI thread (the open of the first file of a language).** `Registry.NewGrammarWith` is 0 to 6 ms; the first line makes the rules and compiles the patterns: 2 ms for Go and Python, 11 for TypeScript, 21 for HTML, 75 for Markdown, in the first frame of the first file of that language. `Process.WarmGrammars` now makes them on a goroutine, for the languages of the files the window opens with, while the renderer starts (`Grammar.Warm`), and `Tokenizer.Ensure` does not wait for another goroutine that is making them (`Grammar.Compiling`): the lines are tokenized by the background loop when the rules are there (`TestEnsureDoesNotWaitForTheRulesOfTheGrammar`, `TestStartWarmsTheGrammarOfTheFilesOfTheCommandLine`).
5. **The budget for the tokens was per request, not per frame (the first view of unseen lines).** The frame asked for tokens in the layout of the text and again in its paint (and the minimap in its own), each with a fresh budget, so the frame's tokenization was the sum of them: 8 ms with the budget of 6 ms (a CPU profile of the benchmark: 270 to 370 ms of 50 frames in `Tokenizer.Ensure` under `ensureTokens`, 7 ms a frame), and 8 ms still when the constant was lowered to 4 ms. Now the budget is the frame's (`takeFrameBudget`: 4 ms for the layout, the paint and the minimap together, a frame ending with a posted function); the benchmark takes 4.7 ms (5 runs: 4.63 to 4.74 ms). Test: `TestAFrameTokenizesWithinOneBudget`.

What was looked at and is not waste: the cursor blink frame (the CPU profile of 10 s of idle: 200 ms in all, 10 ms of it Emetos's, the rest the renderer's rasterization of the window twice a second); the typing frame (2.5 ms in the app against 0.17 ms in the Go benchmark: the status bar, the minimap, the gutter and, after each edit, the pairing of the 10,000 lines' brackets, 0.7 ms; pairing only from the edited line is the optimization left in the note of step 4.12, still not needed for the target); the start of the process (package initializers 3 ms, the process and its registry 2 to 20 ms: no heavy work that could be lazy before the first frame, except fix 4).

### Proposed decisions for the targets still missed

For you to decide and log; none is applied.

1. **Cold start and the 10 MB open, headless.** Keep the 400 ms and 300 ms targets for the GPU session (`scripts/perf.sh --session`) and add the measure that is Emetos's: "Emetos's share of the start (before the window is made + the build of the first frame) under 100 ms", which is 21 to 28 ms with no folder and 76 to 77 ms with four tabs restored; and for the 10 MB open, the time over the empty window's first frame (110 ms, target 300 ms). Headless the start is Glutter's (Vulkan tried first for 560 to 620 ms, the counter starts as slowly): if the session's start is over 400 ms too, the follow-up is a Glutter issue about the renderer's start, not work in Emetos.
2. **First view of lines nobody has tokenized** (decided; fixed, see "What step 10.1 found", fix 5).
3. **Resident memory, counter + 50 MB (418 MB against 280 MB headless, 138 MB over).** A full editor holds what a one-widget window does not: of the 138 MB, about 50 MB are the Go runtime (88 to 92 MB in the editor, 40 to 46 MB in the counter: a heap of 70 MB against 27 MB; about 55 MB of the editor's is live: the UI and editor fonts parsed by go-text 12 MB, Glutter's font tables 8 MB, the packages' tables and compiled expressions 10 MB, the document's tokens, brackets and buffer 8 MB, widgets and caches the rest; the other third is the collector's headroom), 12 MB are pages of the larger binary (70 MB, the counter's is 49 MB), a region of 33 MB of anonymous memory that the counter does not have is not identified, and the rest (the driver's heap, the same 165 to 185 MB in both) is equal. Propose the target "the counter + 150 MB" (about 430 MB headless; with the GPU the driver's share shrinks for both and the difference stays). Lowering the collector's target (`debug.SetGCPercent(50)`) would save about 25 MB (measured with `GOGC=50`: 488 to 462 MB) for twice the collections while scrolling; not done, since the target as proposed holds without it.
4. **Idle CPU under 1% of a core, headless (1.3%).** The editor asks for exactly two frames a second (the cursor), 0.2 ms of UI thread each; the 1.3% is the software rasterization of the whole window twice a second (14 to 16 ms each) and the compositor's work, which a GPU does elsewhere. Propose to judge the target by the session's number and, headless, by "UI frames at most 2 a second with the cursor blinking and none without the focus" (the test `TestIdleWorkbenchDrawsOnlyTheBlink` pins it).

### Other notes

- The earlier idle figures (1.4% with 31 frames, 0.5% to 0.8% in the first session of step 3.11) and the fix 3 above: the statistics of `GLUTTER_FRAME_STATS=1` were drawing one frame a second of their own; the idle runs of this table are with the fix.
- The old `scripts/perf.sh` took the frames of a whole run, the first frame included, for the typing and scrolling rows ("worst 31 ms" of the earlier tables was the first frame, which builds the whole window), and its wait for the first frame could be late by seconds (`grep -q` in a pipeline under `pipefail`). Both are fixed; the numbers of the earlier tables of this file are not comparable.
- A single frame of `dev.type` or `dev.scroll` runs at 20 to 60 frames a second headless, because the raster of the software renderer takes 14 to 18 ms.

Notes of earlier steps:

- The memory target was "< 200 MB", chosen before the Glutter baseline was known. An empty Glutter window with software GL is about 280 MB headless (its `counter` example), so the target became the baseline plus 50 MB in step 3.11; proposal 3 above revises it again with the numbers of this step.
- Step 4.9 (highlighting), Go benchmarks with `-tags glutter_release`: typing a character in a 10,000-line highlighted file takes 0.6 ms per frame (the tokenizer re-tokenizes from the edited line and stops when a line's end state is unchanged). Tokenizing Go costs about 160 µs per line (`BenchmarkTokenizeGoLines`) after the scanner result cache in `core/regex`. Scrolling a page of lines nobody has tokenized yet, in a 100,000-line file, took 7 to 8 ms of UI-thread time before step 10.1 (the budget was 6 ms for each of the layout, the paint and the minimap) and takes 4.7 ms now (`tokenBudget`, 4 ms for the whole frame); lines it does not finish are tokenized by the background loop and painted plain until the next frame. Lines already tokenized scroll as before. Do not run `BenchmarkScrollPage100kLinesHighlighted` with a large `-benchtime`: every iteration tokenizes new lines and the retained tokens grow with it (use `-benchtime 50x`).
- Step 4.12 (bracket pairs), Go benchmarks with `-tags glutter_release`, on a synthetic file of 100,000 lines and 600,000 brackets: the first pairing takes 49 ms, the first question after an edit 18 ms (30 ns for each bracket: the lines the edit touched are read again and all the brackets are paired in one pass), and the brackets of a screen of 50 lines 5 µs once the pairs are made. A file of 10,000 lines with 60,000 brackets costs about 2 ms after an edit. The pairing is lazy, so it costs nothing while nobody asks (large file mode has no index). New tokens that leave the brackets of their lines as they were no longer make the pairs again (step 10.1, fix 2). Pairing only from the first edited line, and stopping when the open pairs are the same as before, would make an edit cost the edit and not the file; it is left for when a measurement of the editor needs it.
- Step 5.12 (large files): `BenchmarkOpen200MB` writes a file of 200 MB (about 2.1 million Go-like lines) and measures `fileio.Read`, `document.FromRead`, the view model and the first frame: 361 ms (582 MB/s), 1.15 GB allocated. A line longer than `editor.stopRenderingLineAfter` is laid out up to that many bytes only; `TestAHugeSingleLineStaysResponsive` shows and moves in a single line of 5 MB (with word wrap on) in a fraction of a second.

Go benchmarks (UI thread only, one frame of work; `scripts/perf.sh` runs them with `-benchtime 30x`):

| Benchmark | Time per frame | Allocations |
|---|---|---|
| Typing a character in a 10,000-line file (edit, layout of the changed line, paint) | 0.14 ms | 1,044 |
| Scrolling a page in a 100,000-line file | 0.56 ms | 3,874 |
| Opening a 10 MB file (read, buffer, first layout) | 24.5 ms (420 MB/s) | 157,634 |
| Opening a 200 MB file in large file mode (read, buffer, first layout; step 5.12) | 361 ms (582 MB/s), 1.15 GB allocated | 202,552 |

## Results after Glutter 1.8 (the workarounds removed, headless)

Glutter 1.8 fixes three things that Emetos worked around (its steps 28.16 to 28.18): the font fallback no longer parses a font to learn that it lacks a character, and control characters go to the first face; `TreeSliverNode.SetExpanded` lets a tree keep its nodes; `WindowDecoration` handles the window gestures in the title only. Emetos removed `fonts.ControlsFont` (and its family `emetos-controls`), made `kit.Tree` keep its nodes and put the workspace tabs in `Leading`. Glutter's `auto` renderer still tries Vulkan first, and its owner decided to keep that (its SPEC §9.6: a headless run that cares sets `GLUTTER_RENDERER=gles`), so the headless runs of `scripts/proof.sh` and `scripts/perf.sh` now set `GLUTTER_RENDERER=gles` (the environment's value wins; `scripts/perf.sh --session` sets nothing, and the GPU decides there).

"Before" is `main` with Glutter 1.8.0 in `go.mod` (the error boundary of 1.8.1 does not touch these rows) and none of the changes (the font workaround in, the tree remade at every change, Vulkan tried first); "after" is the branch with the changes and `GLUTTER_RENDERER=gles`; both with `scripts/perf.sh --no-go` a few minutes apart on the machine above (5 starts for the medians, the shaders caches as in the first results).

| Metric (SPEC §14) | Before | After |
|---|---|---|
| Resident memory, one 10,000-line file (the font workaround out) | 423 MB (peak 431; anonymous 269, the Go runtime 92); the counter 285 MB | **422 MB** (peak 431; anonymous 269, the Go runtime 93); the counter 278 MB |
| The same file on Glutter 1.7 with no workaround (step 10.1) | 849 MB | not measured again: the fix of 28.16 is what the 422 MB shows |
| Opening a 10 MB file: first frame, from the start of the process | 1000 ms (80 ms over the empty window); build of the first frame 11.2 ms | 470 ms (130 ms over the empty window, the window itself 340 ms); build 13.5 ms |
| Opening a 202 MB file in large file mode: first frame | 1190 ms; resident 1441 MB | 740 ms; resident 1441 MB |
| Cold start, no folder, both shader caches warm (median of 5) | 920 ms (the counter 910 ms) | **340 ms** (the counter 330 ms); Emetos before the window 15 ms, first build 11.1 ms |
| Cold start, Mesa's cache warm, Glutter's empty | 940 ms (the counter 920 ms) | 340 ms (the counter 330 ms) |
| Cold start, both caches empty | 4470 ms (the counter 2540 ms) | 3890 ms (the counter 1880 ms) |
| Cold start with four restored tabs, both caches warm (median of 3) | 1180 ms; resident 612 MB | 580 ms; resident 658 MB |
| Typing in a 10,000-line file: build of the frames | 2.72 ms average, 5.56 ms worst of 138 | 2.60 ms, 7.02 ms of 137 |
| Scrolling a 100,000-line file: build of the frames | 1.60 ms average, 6.11 ms worst of 269 | 1.64 ms, 8.95 ms of 269 |
| Idle, focused, cursor blinking | 1.20% of a core (22 frames) | 1.00% (21 frames) |
| Renderers tried that failed (Vulkan), last start of each cache state | 616, 618, 596 ms | none |

What this says:

- **The memory holds without the workaround.** 422 MB against 423 MB: Glutter 1.8 parses one font for the first row with a tab, as `ControlsFont` made it do, and the 62 files of fontconfig's `monospace` are not loaded (the 849 MB of Glutter 1.7 without it). The 10 MB file's first build is the same as before within the noise of the machine (11.2 and 13.5 ms; the 10.1 runs were 11.7 and 14.7 ms).
- **The cold start is 340 ms headless, under the target of 400 ms,** and the counter of Glutter starts as fast (330 ms): all of the 580 ms that the Vulkan try cost went (the first draw is 258 ms, of which 156 to 170 ms is the GLES context and 55 ms the shaders). It says nothing of the start with a GPU, which is the session's number (`scripts/perf.sh --session`, `docs/MANUAL-CHECKS.md`), where the try costs nothing.
- The restored session's resident memory (612 and 658 MB) varies by 50 MB or more between runs of the same code (10.1 measured 406 and 725 MB for it): the sample is taken a few seconds after the first frame, while the tokenizer and the file system's tree reads are still running, and the faster start now takes it earlier in their work. It is not one of the targets.
- **A second run of the final branch (Glutter 1.8.1, which adds the error boundary only)** gave the same: 423 MB resident (peak 429; the counter 282), cold start 350 ms (counter 320) and 340 ms with the driver's cache warm, 4200 ms with both empty, 10 MB open 480 ms, 202 MB open 690 ms, four restored tabs 600 ms and 643 MB, idle 1.20% (22 frames).
- The tree's kept nodes and the title bar have no number of their own: the typing, scrolling and idle rows are what they were (the differences are those of two runs on a shared machine).

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

Re-measured by `scripts/perf.sh` in step 10.1 (`BenchmarkSearch100k`, 20 operations): 9.3 to 19.8 ms. Result: **met**, 9 to 19 ms for the scoring and sorting of a keystroke (target 30 ms), 9 to 17 ms for the scoring alone; every case is also under 30 ms with the cache. The scoring is nearly allocation free after the first call (the allocations are the matches of the items that match). A `Scorer` is used by one goroutine; the search of step 6.18 splits the paths over several scorers (see "Quick open (step 6.18)"). `TestScoring100kPathsWithinTarget` checks a bound of 600 ms (twenty times the target, so that a busy machine and the race detector, which is four times slower, do not make it flaky); it is skipped with `-short`.

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

Re-measured by `scripts/perf.sh` in step 10.1 (`BenchmarkQuickOpen100k`, 3 operations): 12.5 to 24.2 ms on one goroutine, 2.3 to 8.6 ms on all of them. Result: **met**. A keystroke takes 2 ms to 8 ms with the goroutines and 9 ms to 27 ms with one (target 30 ms), so the target holds even on a machine with one core, though the 27 ms of "view model" is close. The search runs on goroutines, never on the UI thread; the UI thread only takes the finished rows (`Picker.SetItems`, 512 rows at most). `TestQuickOpen100kWithinTarget` checks a bound of 600 ms (twenty times the target, so that a busy machine and the race detector do not make it flaky); it is skipped with `-short`.

Listing: at this rate 100,000 files take about 0.1 s (an extrapolation, not a measurement: the benchmark does not write 100,000 files), so the first rows of a search are there at once (the search runs again while the walk goes, at most ten times a second) and the list is complete within a fraction of a second for a repository of that size on a warm file cache. A cold disk is the file system's cost, not the walk's.

What made it fast, and what to keep in mind:

- A score decides the order whenever two scores differ, so the sort only needs the paths whose score is at least the score of the last place (512 of up to 60,000 matches): `Search100k` of step 6.16 sorted every match with a cache for them. The matches of the paths that are kept are made once, at the end.
- Every goroutine scores into its own slice of the matches (the place and the score), and the slices are joined in the order of the list, so the result does not depend on the number of goroutines (`TestSearchPathsIsDeterministicAndCapped`).
- The `e` query allocates the most (225,000 allocations): every path that matches makes the slices of its matches during the scan. A scorer that only returns the score would halve it; not needed for the target.
- The walk keeps one string for each file. The ignore rules are checked for every entry (`glob.Ignore.Check`: a few regular expressions of the files that apply); the benchmark has two patterns and one ignore file, so a tree with dozens of ignore files in the path of every file costs more.

## Markdown preview of a big document (step 17b.2, headless)

The document is `extension/ui/uitest/mdfixture`: 700 KB, 3224 blocks, 150 images of 960 x 540 (the size of `docs/IMPLEMENTED.md` in October 2026), generated in a temporary folder. The app numbers come from the display tests `TestRealInputBigPreview*` of `cmd/emetos` (`scripts/realinput.sh`: a headless sway of 1280 x 800, Mesa llvmpipe, a debug build with `GLUTTER_FRAME_STATS=1`, real wheel and pointer events; the editor shows the file as text, then the preview opens in the group on its side, 466 x 686 px). The memory is `VmRSS` of the editor. The UI-thread numbers are Go benchmarks with the fake platform. The machine was busy with other programs during every run. The GPU numbers (the row of SPEC §14 is decided by them) wait for the manager's pass on the desktop.

| Metric | Target (SPEC §14) | Before (`main`, step 17b.1) | After |
|---|---|---|---|
| The command to the first frame with blocks | the first frame of the preview within 300 ms | 9.1 to 9.6 s until the editor answered again (one frame of 3.2 s of UI thread, then 1 s of raster per frame) | 157 to 259 ms until the first blocks were built (the parse is 11 ms off the UI thread; the first frame is drawn before it) |
| Start with the big preview as the first view (`workbench.editor.defaultFileViews`, as a restored session) | a first frame that does not wait for the preview | not measured (the first frame builds all 3224 blocks: seconds) | the same first frame as with a preview of a small file (4.86 s against 5.38 s since `Run` under load, the headless start of the window itself, 25 to 500 ms apart in the runs) |
| Resident memory over the file open as text | at most 200 MB more | +2090 to +2290 MB (453 MB to 2.5 to 2.7 GB) | +116 to +129 MB (449 to 582 MB) |
| Blocks built | the visible ones | 3224 | 7 to 11 of 3224 |
| UI thread per frame while scrolling (30 notches of 3, a jump by the scroll bar) | no frame over 16 ms | 38 frames: 2.9 ms on average, 12.8 ms at worst (the first frame: 3.2 s) | 417 to 529 frames: 0.7 to 1.0 ms on average; the worst frame is 40 to 69 ms, the one that builds a window after the jump (it highlights the code blocks of the window with the Go grammar) |
| Renderer per frame while scrolling (software, no GPU) | no frame over 16 ms with the GPU | 786 ms on average, 1.37 s at worst, every frame over 16 ms | 12.6 to 18.0 ms on average, 29 to 119 ms at worst; 83 to 264 of 417 to 529 frames over 16 ms (the load of the machine decides) (the full window is drawn by llvmpipe at 13 ms a frame: the GPU decides) |

Go benchmarks (`go test -tags glutter_release ./extension/ui/markdown -run xxx -bench Preview -benchmem`, the UI thread alone, 700 KB, 150 images):

| Benchmark | Measures | Result (100 runs) |
|---|---|---|
| `BenchmarkPreviewParse` | the parse and the block keys, off the UI thread | 8.8 ms (82 MB/s), 152,000 allocations |
| `BenchmarkPreviewFirstFrame` | the first frame (no blocks yet, it does not wait for the parse), then the parse and the frame with the first blocks | 16.2 ms (the harness's window and theme included), then 13.6 ms |
| `BenchmarkPreviewJump` | a jump to a place never shown: the window is chosen from estimates, built and laid out (no highlighting) | 1.6 ms, 3.9 MB, 7,700 allocations |
| `BenchmarkPreviewWheelStep` | a step of the wheel (12 px) | 58 µs, 24 KB |


What made it fast, and what to keep in mind:

- Only the blocks within 250 px of the viewport exist as widgets (`Stats.Built`); a jump chooses its window from estimates and positions it from an anchor block, so it costs one window (a `SliverList` would lay out every block on the way: `docs/GLUTTER-ISSUES.md`). A step of the wheel builds nothing that is built already: the widgets of the window are kept by block key.
- The images are decoded at the size they are shown at, off the UI thread, two at a time, and held within 64 MB with the least recently used dropped first: the 2.1 GB were 150 full-size decodes (and their textures) held for as long as their widgets were mounted.
- The text is parsed off the UI thread when it is over 16 KB (11 ms for 700 KB: the parse was never the cost, the 3224 mounted blocks were); a typed character shows 300 ms later, as in VS Code, and builds the widgets of its block only.
- The highlighting of a code block is on the UI thread, when its block is built: the frame that builds a whole window of new blocks (a jump) takes 40 ms headless with two or three Go blocks in it. Moving it off the thread needs a thread-safe `Languages().Tokenize` in the extension API.
- The raster time headless is llvmpipe drawing the whole window (13 ms a frame whatever the preview holds); with the Vulkan renderer every shape that is built is drawn, also off screen, which is why the margin is 250 px.

## Pending

`scripts/perf.sh --session` with the GPU for every row of the table of step 10.1, in `docs/MANUAL-CHECKS.md` (3.11 and 10.1). The decisions proposed above for the targets still missed wait for its numbers and for yours.
