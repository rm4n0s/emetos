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

- Only the blocks within 250 px of the viewport exist as widgets (`Stats.Built`); a jump chooses its window from estimates and positions it from an anchor block, so it costs one window (a `SliverList` of Glutter 1.9.1 laid out every block on the way; Glutter 1.9.2 has `EstimateItemOffsets` for that, and the preview keeps its own list). A step of the wheel builds nothing that is built already: the widgets of the window are kept by block key.
- The images are decoded at the size they are shown at, off the UI thread, two at a time, and held within 64 MB with the least recently used dropped first: the 2.1 GB were 150 full-size decodes (and their textures) held for as long as their widgets were mounted.
- The text is parsed off the UI thread when it is over 16 KB (11 ms for 700 KB: the parse was never the cost, the 3224 mounted blocks were); a typed character shows 300 ms later, as in VS Code, and builds the widgets of its block only.
- The highlighting of a code block is on the UI thread, when its block is built: the frame that builds a whole window of new blocks (a jump) takes 40 ms headless with two or three Go blocks in it. Moving it off the thread needs a thread-safe `Languages().Tokenize` in the extension API.
- The raster time headless is llvmpipe drawing the whole window (13 ms a frame whatever the preview holds); with the Vulkan renderer of Glutter 1.9.1 every shape that is built was drawn, also off screen, which is why the margin is 250 px (Glutter 1.9.2 skips what is outside the clip; the margin stays at Flutter's default).

### After Glutter 1.9.2 (headless)

The same document (`docs/IMPLEMENTED.md` of the branch, 714 KB, with its images), the same tests and benchmarks, on a machine that was running another agent's tests at the same time. Glutter 1.9.2 skips the shapes outside the clip before it makes their mask (its step 29.13) and `EstimateItemOffsets` makes a jump into a long `SliverList` cost one window (29.20); Emetos's own list of the preview is kept, with the same 250 px margin.

| Metric | After 17b.2 (Glutter 1.9.1) | Glutter 1.9.2 |
|---|---|---|
| The command to the first blocks | 157 to 259 ms | 141 ms |
| Resident memory over the file open as text | +116 to +129 MB | +113 to +119 MB (446 to 447 MB as text, 561 to 566 MB with the preview) |
| Blocks built | 7 to 11 of 3224 | 10 of 3224 |
| UI thread per frame while scrolling and jumping | 0.7 to 1.0 ms on average, the worst 40 to 69 ms | 0.86 ms on average, the worst 45 ms (480 frames) |
| Renderer per frame while scrolling (software) | 12.6 to 18.0 ms on average, 29 to 119 ms at worst, 83 to 264 of 417 to 529 frames over 16 ms | 9.2 ms on average, 24.5 ms at worst, 36 of 480 frames over 16 ms (the first of two runs, with the machine busier, had many frames at 17 to 20 ms) |
| First frame with the big preview as the first view | the same as with a small one | 4.0 s since `Run` with the small preview, 4.1 s with the big one |

| Benchmark (100 runs) | After 17b.2 | Glutter 1.9.2 |
|---|---|---|
| `BenchmarkPreviewParse` | 8.8 ms, 152,000 allocations | 9.2 ms, 152,361 allocations |
| `BenchmarkPreviewFirstFrame` | 16.2 ms, then 13.6 ms | 14.5 ms, then 11.2 ms (26.3 ms in all) |
| `BenchmarkPreviewJump` | 1.6 ms, 3.9 MB, 7,700 allocations | 1.0 ms, 3.6 MB, 7,757 allocations |
| `BenchmarkPreviewWheelStep` | 58 µs, 24 KB | 35 µs, 24 KB |

## Idle allocation (step 17b.4, headless)

The cursor blinks twice a second and nothing else happens (SPEC §14 "Idle allocation"). The desktop test of 2026-10-27 saw the Go heap grow by about 11 MB a second (2.5 to 5 MB a frame) until a collection. Method: `go build -tags 'glutter_release emetos_dev'`, run isolated (`scripts/headless-wayland.sh -- scripts/isolated.sh -- <binary> <file>`, `GLUTTER_RENDERER=gles`, `GLUTTER_FRAME_STATS=1`, `EMETOS_PROFILE_DIR`, `GODEBUG=memprofilerate=4096`), 12 s to settle, `SIGUSR1` (a garbage collection, the `allocs` and `heap` profiles, `memstats`), 24 s of idle with a CPU profile, `SIGUSR1` again; the frames are the sum of the `fps` of the statistics lines between the two dumps; the bytes and objects are the cumulative `allocs` profile of the second dump minus the first, of `internal/gui/app.Run` (the UI thread, without the profiler's own work: `go tool pprof -sample_index=alloc_space -base allocs-1.pprof -ignore='runtime/pprof|compress/' -top -cum`). The window is 1280 x 800, one editor group, the editor focused. The files: a generated 10,000-line Go file (454 KB; the lines of `scripts/perf.sh`'s `gen`) and `docs/IMPLEMENTED.md` (703 KB, 150 images) in the editor with its preview to the side (`emetos.markdown.openPreviewToSide`).

| Run (24 s of idle) | Frames | The UI thread a frame, all | of it, not the renderer | Rate | Live heap between collections |
|---|---|---|---|---|---|
| 10,000-line Go file, before | 45 | 1.94 MB, 7,900 allocations | 17.0 KB, 283 allocations | 3.6 MB/s | 34 to 64 MB |
| 10,000-line Go file, after | 47 | 1.87 MB, 7,200 allocations | 10.5 KB, 132 allocations | 3.7 MB/s | 34 to 62 MB |
| Markdown file with its preview, before | 48 | 8.44 MB, 30,100 allocations | 24.3 KB, 277 allocations | 16.8 MB/s | 80 to 147 MB |
| Markdown file with its preview, after | 48 | 8.54 MB, 30,600 allocations | 13.1 KB, 157 allocations | 17.1 MB/s | 82 to 147 MB |

Who allocates: under `drawScene` of the GLES renderer (Vulkan: `explicit`), which is Glutter's: 99% of the first column. For the Go file: `gpu.flattener.emit` 40%, `text/layout.paragraph.paintGlyphs` 12% (cumulative with `textdraw.Expand`: 49%), `gles.shapeContext.paintWith` 9%, `gles.frame.add` 6%, `ui.Matrix4.Clone` 6%, `gpu.PaintAccess` 4%, `gpu.ShapeOf` 1%. `gpu.Flatten` walks and expands every picture of the scene on every frame, also inside the layers its raster cache holds and then drops (fixed in Glutter 1.9.2, step 29.15: see "After Glutter 1.9.2" below); a blink adds one picture of three operations to the scene (checked by walking the scenes in a test), so the cost is the glyphs on screen, paid for nothing. Emetos's part before the step: the layer of the cursors (a repaint boundary of its own, so a blink repaints only it) read the text of every visible row, 57 allocations and 3.4 KB a frame, to look for control characters (`editor.renderControlCharacters` is on by default, as in VS Code), then the style of the text (fingerprint string), the geometry, the blink timers; now a cache keeps the control characters of the rows (`controlCache`, read again when the document, `editor.stopRenderingLineAfter` or the text of a row changes). What is left of the non-renderer part is the framework's scene builder, which adds every retained layer to the scene again, the blink timers (Glutter's `NewTimer`), the platform's events and the frame statistics.

The test with the fake platform (no renderer, so Emetos's part and the framework's alone), `TestIdleBlinkFrameAllocationStaysInBudget`, 10,000-line Go file, highlighted, editor focused: before 8.0 KB and 147 allocations a frame, after 4.6 KB and 92 (4.8 KB and 94 under `-race`); the budget is 6 KB and 120. The CPU profile of 20 of those seconds: 1.7% of a core (the profiler's sampling of allocations included): the statistics of the headless frames say the build takes 0.15 to 0.3 ms and the raster (llvmpipe) 12 to 90 ms.

### After Glutter 1.9.2 (headless)

Glutter's step 29.15 makes the flattener skip the layers the raster cache holds, expand only the rows of a paragraph that reach the clip, and copy less. The same method as above (the 10,000-line Go file, and `docs/IMPLEMENTED.md`, 714 KB, with its preview to the side; a release build with `emetos_dev`, a headless sway of 1280 x 800 at scale 1, GLES on llvmpipe, 24 s of idle after 12 s of settling, `GODEBUG=memprofilerate=4096`), one run each:

| Run (24 s of idle) | Frames | The UI thread a frame, all | of it, not the renderer | Rate | Live heap between collections |
|---|---|---|---|---|---|
| 10,000-line Go file, Glutter 1.9.1 (after 17b.4) | 47 | 1.87 MB, 7,200 allocations | 10.5 KB, 132 allocations | 3.7 MB/s | 34 to 62 MB |
| 10,000-line Go file, **Glutter 1.9.2** | 56 | 164 KB, 2,119 allocations | 11.3 KB, 199 allocations | 0.38 MB/s | 47 to 57 MB |
| Markdown file with its preview, Glutter 1.9.1 (after 17b.4) | 48 | 8.54 MB, 30,600 allocations | 13.1 KB, 157 allocations | 17.1 MB/s | 82 to 147 MB |
| Markdown file with its preview, **Glutter 1.9.2** | 56 | 549 KB, 5,947 allocations | 11.5 KB, 191 allocations | 1.28 MB/s | 98 to 128 MB |

The renderer's part fell 11 times with the Go file and 16 times with the preview; Emetos's own part is where it was (the same figures within the noise of a sampled profile). What is left in the renderer is the glyphs of the visible rows (`paragraph.paintGlyphs`: 50% of the bytes with the preview, 36% with the Go file), the arguments of the GL calls (`vertexAttribPointer`, 33% of both) and the paints; it still grows with the text on screen, but not with the text of the document. `TestIdleBlinkFrameAllocationStaysInBudget` is unchanged.

## Desktop pass (step 17b.6)

The manager's pass on the user's desktop, 2026-10-28: Radeon 8060S (RADV STRIX_HALO), Vulkan (`GLUTTER_RENDERER=auto`), in the session of the caller for `scripts/perf.sh`, in a nested sway for the display tests (`scripts/realinput.sh --desktop`).

### The GPU numbers of SPEC §14

`scripts/perf.sh --session -n 3 -s 15 --no-go --no-huge` (a release build with `emetos_dev`; the 200 MB open and the Go benchmarks were left out of this run). Targets are SPEC §14's and are unchanged.

| Metric (SPEC §14) | Target | Measured | |
|---|---|---|---|
| Typing in a 10,000-line file: build time of the frames (average, worst of 136 frames) | < 4 ms | 2.33 ms, 6.96 ms (renderer 2.31 ms on average, 4.63 ms worst; 0 frames over 16 ms) | met |
| Scrolling a 100,000-line file: frames | the refresh rate, none over 8 ms | 268 frames; build 1.48 ms on average, **8.24 ms worst**; renderer 2.18 ms on average, 4.75 ms worst; 0 over 16 ms | **missed** (average met, the worst frame is 0.24 ms over) |
| Opening a 10 MB file: first frame | < 300 ms | **410 ms** from the start of the process (150 ms more than the empty window; 43 ms of it before the window is made); build of the first frame 10.06 ms | **missed** |
| Cold start, no folder, to the first frame (median of 3), both shader caches warm | < 400 ms | 260 ms (Glutter's counter, same run: 350 ms); Emetos before the window is made: 27 ms; build of its first frame 10.5 ms | met |
| Cold start, the driver's cache warm and Glutter's empty | < 400 ms | 270 ms (the counter: 330 ms); before the window 19 ms; first build 9.27 ms | met |
| Cold start, both caches empty (the first start on a machine) | < 400 ms | 560 ms (the counter: 370 ms); before the window 25 ms; first build 8.70 ms | missed, a first start only |
| Cold start with the workbench and the session restored (four tabs, three files in each; median of 3), both caches warm | < 400 ms | **570 ms**; Emetos before the window is made 54 ms; build of its first frame 38.2 ms; resident memory 410 MB | **missed** |
| Idle, focused, cursor blinking (15 s) | < 1% of a core | 0.47% with the renderer threads, 0.47% without them (30 frames; the counter: 0.00%) | met |
| Idle, without the focus (15 s, another window has it) | no frames at all | 0 frames, 0.00% of a core | met |
| Resident memory, one 10,000-line file | the counter + 50 MB | 184 MB (peak 206 MB; 91 MB anonymous, Go 88 MB of it); Glutter's counter 135 MB (47 MB anonymous, Go 44 MB): +49 MB | met |

The parts of the first draw of the last start (the editor with no folder), in ms:

| Part of the first draw | Both caches empty | The driver's warm | Both warm |
|---|---|---|---|
| Renderers tried that failed | 0 | 0 | 0 |
| GPU context or device | 160.8 | 180.6 | 175.0 |
| Shaders (4 built, 0 from the cache in each) | 305.0 | 0.3 | 0.1 |
| The frame itself | 13.6 | 4.4 | 4.0 |
| First draw, all of it | 479.0 | 185.0 | 179.0 |
| From the start of Run to the first frame | 542 | 261 | 270 |

Missed, to decide (`docs/MANUAL-CHECKS.md` 3.11 and 10.1): the 10 MB open (410 ms against 300 ms), the restored start (570 ms against 400 ms), the worst frame of the scroll (8.24 ms against 8 ms). The decisions proposed in "Proposed decisions for the targets still missed" above wait for the 200 MB open and the Go benchmarks, which this run left out.

### The big preview with the GPU, and what D13 was

`TestRealInputBigPreviewOpensAndScrolls` (the 700 KB document with 150 images, the preview to the side, 30 notches of the wheel and a jump by the scroll bar) with the GPU and headless, debug and release builds. "Scrolling and jumping" is measured from the window of frame statistics after the open (D13 of `docs/DESKTOP-PASS.md`: before, it also counted the first frame of the editor's window, which is why the test failed 4 of 4 on the desktop).

| Run | Frames | UI thread per frame | Renderer per frame | Over 16 ms | Resident over the file as text |
|---|---|---|---|---|---|
| Desktop, Vulkan, debug build (2 runs) | 471 | 0.78 to 0.89 ms on average, 11.1 to 11.6 ms worst | 2.4 to 2.6 ms, 5.4 to 6.3 ms worst | 0 | +42 to +43 MB |
| Desktop, Vulkan, release build (2 runs) | 471 | 0.53 to 0.55 ms, 4.4 to 5.2 ms worst | 2.8 ms, 5.7 to 6.0 ms worst | 0 | +48 MB |
| Headless, llvmpipe, debug build (3 runs) | 473 to 475 | 0.69 to 0.76 ms, 8.9 to 12.0 ms worst | 9.2 to 9.4 ms, 20.8 to 24.9 ms worst | 28 to 42 | +112 to +124 MB |
| Headless, llvmpipe, release build (2 runs) | 472 to 475 | 0.48 to 0.49 ms, 3.4 to 3.8 ms worst | 8.6 to 9.0 ms, 20.9 to 21.7 ms worst | 18 to 29 | +112 to +131 MB |

The worst frame of the scroll was 40 to 69 ms (after 17b.2) and 45 ms (after Glutter 1.9.2) in the notes above. That was the frame that opens the preview (it fell in the same one-second window as the scroll), not the jump: the jump's worst frame is 9 to 12 ms in the debug build and 3.4 to 5.2 ms in the release build.

The three costs of the start and the open, which the test used to mix:

| Cost | Debug build (what the display tests run) | Release build | Whose |
|---|---|---|---|
| UI thread, first frame of the editor's window (the workbench with the 700 KB file open as text) | 100 to 114 ms (GPU and headless alike) | 10 to 20 ms (one run 48 ms) | Glutter's debug checks (`AssertCurrent` was 63 of about 150 ms in a profile); gone since Glutter 1.9.3, see "After Glutter 1.9.3" below |
| UI thread, the frame that builds the first blocks of the preview | 42 to 47 ms (also on the second and third open in one process: 47 and 49 ms, so no first use) | 8 to 9 ms (under 16 ms: not a slow frame on the GPU) | the same; Emetos's Markdown code in the profile is 4 ms |
| Renderer, the first frame of the editor's window | llvmpipe: 4.0 to 5.2 s with no Mesa cache (the second frame, the first with text, 0.65 to 0.8 s more); GPU: 865 to 900 ms | llvmpipe, Mesa cache warm: 0.29 s; GPU, either cache warm: 570 to 620 ms, both warm: 546 to 598 ms | the driver's first compile; Glutter's device creation |

After Glutter 1.9.3 (2026-10-28, `scripts/realinput.sh -- ./cmd/emetos -run BigPreview -count=3 -v` without the race detector, headless llvmpipe, debug build; Glutter 1.9.3 made `UIThread.IsCurrent` a read of the goroutine's record instead of a parse of `runtime.Stack`, and 1.9.4 kept its speed): the UI thread of the first frame of the editor's window is 10.0 to 12.5 ms (it was 100 to 114 ms), and the frame that builds the first blocks of the preview is 8.3, 8.7 and 9.1 ms (it was 42 to 55 ms), the same as the release build's 8 to 9 ms. The scrolling frames are unchanged (0.48 ms on average, 3.2 ms worst). The renderer's frames and the first start (4.3 to 5.1 s since `Run`) are the driver's and are not changed. So the debug build's checks are no longer a cost to account for, and `previewFirstFrameBudget` is 20 ms (twice the worst of the three runs, rounded), where it was 100 ms.

The first open of a preview in a process is slower than that (2026-10-30, 24 headless runs on a machine shared with other work): the frame that builds the first blocks is 17 to 43 ms, of which the layout of the ten blocks is 17 to 20 ms the first time and 2 ms every time after (in the first layout the heading of the first block took 10 ms and the code block 5 ms, 0.3 ms later; the window itself, with the 1.7 ms of the highlighting of its code block, is 3 ms). That is Glutter loading the faces the editor had not used, not the work of the preview, and it cannot leave the UI thread. `TestRealInputBigPreviewOpensAndScrolls` therefore holds the first open to `previewColdFrameBudget` (100 ms) and the open of the same preview after closing it to `previewFirstFrameBudget` (20 ms; 7 to 17 ms in 10 runs). SPEC §14 has no number for the frame of an open, only the first frame within 300 ms (150 to 290 ms, both opens) and no scrolling frame over 16 ms.

The renderer's first frame, with `GLUTTER_FRAME_STATS=1` (the `startup` lines of the log), on the GPU: GPU context or device 571 ms (590 ms warm), shaders 287 ms with no cache and 0.1 ms with the pipeline cache Glutter keeps (6 pipelines; `pipeline_cache_bytes=156340`), the frame itself 7 to 14 ms. A warm cache makes only the shaders go away: on a second start the first draw is about 600 ms in the nested sway, of which the device is all but 10 ms; in the user's own session the device took 160 to 180 ms (the table above). The caches are either one: Mesa's (`MESA_SHADER_CACHE_DIR`, `$XDG_CACHE_HOME/mesa_shader_cache` by default) or Glutter's (`$XDG_CACHE_HOME/glutter`) was enough on the GPU. With llvmpipe only Mesa's counts (4.06 s with Glutter's warm and Mesa's empty, 0.29 s the other way round; one run each, both warm gave 1.8 s in a run on a loaded machine). The preview makes no pipeline and no shader of its own: its open and its images cost no frame over 16 ms of the renderer on the GPU, and the three opens of one process cost the same. Glutter's part of the cold start is the device creation (it is not cached and is the same on a second start); the shaders are the driver's compile, which Glutter's pipeline cache already avoids, so there is no `GLUTTER-ISSUES.md` entry for the renderer.

`display.Start` gives every run a fresh `XDG_CACHE_HOME`, so every display test is a first start for the driver: the start of the window costs 4 to 5 s on llvmpipe in each test, and the numbers of "first frame" in `TestRealInputBigPreviewStartDoesNotWaitForThePreview` (4.0 s since `Run` in the notes above) are that. They are not the preview's.

## Terminal emulator (step 17d.1)

The emulator alone (`extensions/terminal/emulator`, no UI), on the machine above, on 2026-10-28, with the machine otherwise used by the developer's session (single numbers move by 10% to 20%; the cursor motion by more). The budgets are those of SPEC §14 "Terminal throughput" and "Terminal memory"; `TestThroughputBudgets` fails when the best of five runs of 8 MiB is under them (it is skipped under `-race` and `-short`), and `TestScrollbackMemory` and `TestSnapshotAllocations` hold the memory and the allocations.

Method: a terminal of 160x45 cells with 10,000 lines of scrollback is fed 8 MiB (made by a seeded generator) in chunks of 64 KiB, as the pseudo-terminal's reads arrive; MB/s counts 10^6 input bytes, escape sequences included. Repeat with `go test ./extensions/terminal/emulator -run XXX -bench . -benchmem -benchtime 20x -count 3` (the numbers below are the medians of three runs) and `go test ./extensions/terminal/emulator -run 'Budgets|ScrollbackMemory' -v`.

| Input | Budget | Measured (median) | Allocations per 8 MiB |
|---|---|---|---|
| Plain ASCII: text lines of 20 to 140 characters, CR LF | 400 MB/s | 790 MB/s (751 to 829) | 8 (the 64 KiB-page growth of the first pass) |
| A color change every 8 characters: 256-color, 16-color with bold, and true color from a theme of 64 colors, a reset at the end of each line | 100 MB/s | 228 MB/s (223 to 232) | 9 |
| UTF-8 of CJK, emoji and some Latin, 100 columns a line | 150 MB/s | 356 MB/s (354 to 357) | 8 |
| Scrolling in a region (`CSI 2;44 r`, then line feeds at its bottom) | none; floor of the test 200 MB/s | 306 MB/s | 0 |
| Cursor motion: `CSI r;c H`, `C`, `B`, `D`, `A` with one character each | none; floor 60 MB/s | 186 MB/s (144 to 208) | 0 |
| The worst case of the style table: a new true color every 8 characters, never repeated | none; floor 40 MB/s | 101 MB/s (86 to 103) | 2,420 (the compactions of the table) |

| Memory and allocation | Budget | Measured |
|---|---|---|
| 10,000 lines of scrollback at 160 columns (heap after the collection, 11,000 lines written) | 16 MB | 13.4 MB |
| Snapshot of a terminal with nothing changed | 0 allocations | 0 allocations, 11 ns |
| One write that changes a row, then a snapshot (reused `Frame`) | 0 allocations | 0 allocations |
| A write of 64 KiB that scrolls the whole screen, then a snapshot (all 45 rows copied, 57 KB) | no budget | 77 microseconds, 0 allocations |

What made the numbers: the ASCII run is found by a scan and written to the cells in one loop (the first version, which tested each byte in the writing loop, did 320 to 420 MB/s); a new line takes the oldest line of the ring and clears it with `clear` (a loop that stored a cell value was 15% of the time); the pen's style is looked up in the table once, at the first character after an SGR; the parameters of a sequence are in a fixed array. The worst case of the style table is bounded on purpose: compaction (a scan of the 1.6 million cells of the scrollback and a new map) runs at most once per 65,535 refused styles, because a table full of live styles rebuilt every 8,192 refused styles took 16 MB/s; between compactions a new 24-bit color is rounded to the 6x6x6 cube. `REP` and the other sequences that ask for much work with few bytes are held to the size of the screen (`TestAmplification`: 64 KiB of `CSI 99999999 b` takes 90 ms).

The flow of the whole terminal (the process, the UI thread, the glyph atlas and `cat` of a 100 MB file against `foot`) was measured in step 17d.2, below.

## Terminal panel (step 17d.2)

The whole terminal, with real input through the compositor, in a **release build** (`-tags glutter_release,emetos_dev`), on the machine above, on 2026-10-28, headless (sway at 1920x1080, Mesa llvmpipe, the software renderer: the raster numbers are not the GPU's). The terminal is the panel maximized with the side bar hidden, 231 x 52 cells (larger than the 160 x 45 of the budgets, so every frame holds 1.6 times the cells). Command: `scripts/terminal-perf.sh` (it builds the release binary, runs `TestPerfTerminal*` of `cmd/emetos` with `scripts/realinput.sh`, and prints the lines that start with `PERF`); `scripts/terminal-perf.sh --desktop` runs the same in a nested sway window on the desktop with the GPU, which is the manager's run. The shells are bash with `--norc --noprofile` in a home folder of the run. `foot` is run in the same compositor with the same file (`--window-size-chars` 160x45 and 231x52, the size confirmed with `stty size`). **Ghostty is not installed on this machine**, so there is no number for it. Three runs; each cell is the three values.

| Budget (SPEC §14) | Target | Emetos | foot |
|---|---|---|---|
| Terminal output: `cat` of a 100 MB file (the shell's clock, `date` before and after) | under 1.5 s | **661, 623, 655 ms** at 231 x 52 | 651, 466, 660 ms at 160 x 45; 575, 479, 493 ms at 231 x 52 |
| Terminal output: frames while it runs | no frame (UI thread and raster) over 16 ms | 82 to 84 frames, **0 over 16 ms**; UI thread 0.33 to 0.39 ms on average, 1.5 to 2.4 ms at worst; software raster 7.1 to 7.3 ms on average, 13.7 to 14.5 ms at worst | |
| Terminal frame: a stream of full screens of 160 x 45 cells that all change (6,000 screens, 50 MB, in 525 to 537 ms) | UI thread 3 ms or less, raster 4 ms or less with the GPU | UI thread **0.52 to 0.56 ms** on average, 1.55 to 1.97 ms at worst, 0 frames over 16 ms; software raster 7.6 to 7.8 ms on average, 13.8 to 14.4 ms at worst (the 4 ms is for the GPU: the desktop run decides) | |
| Terminal typing: 300 characters typed at 10 ms | the UI thread's part of the echo under 2 ms; the echo in the first frame after the shell's output | UI thread **0.35 ms** on average, 1.1 to 1.7 ms at worst; the last character is on screen 63 to 65 ms after the typing ended (the harness polls every 50 ms) | |

The Go benchmarks of the UI thread alone, without the renderer (`go test ./extensions/terminal -run xxx -bench 'FullScreen|EchoFrame' -benchmem`, the fake platform): a frame of 160 x 45 cells that all changed 0.29 ms (475 KB, 56 allocations), the frame of one typed character 0.11 ms (183 KB, 86 allocations). An idle terminal paints nothing (`TestAPaneThatDoesNotChangeDrawsNothingMore`: no paint in 40 frames).

What made the numbers, and what to keep in mind:

- The cost of a frame does not depend on the text: the dirty rows are copied under one short lock, the styles are resolved once per run, and two `DrawAtlas` calls draw the glyphs and the backgrounds. The software renderer's 7 ms is the whole window drawn by llvmpipe (Glutter's raster cache keeps the picture of what did not change).
- The flow control is what keeps `cat` of 100 MB bounded: the reader pauses above 100,000 unparsed bytes, so the parser (about 700 MB/s alone, §14 "Terminal throughput") is the pace and the memory stays flat; Emetos takes 1.0 to 1.4 times foot's time at the same size. The time is the pipe of the pseudo-terminal, the emulator and the 64 KiB reads, not the drawing (the UI thread idles most of the time).
- The first frames after the panel is maximized or the font changes take 0.3 to 0.7 s of raster each on llvmpipe (new pipelines and a new atlas image); they are outside the numbers above (the test waits for them) and Glutter's (`docs/PERFORMANCE.md` "Desktop pass").
- The measurement of the "frames while it runs" is Glutter's once-a-second statistics (`GLUTTER_FRAME_STATS=1`), so a window can hold a second of idle frames before or after the run; the table has the windows of the run and the one that follows it.

## Search (step 17e.2)

The engine alone (`internal/core/search`, no UI), on the machine above, on 2026-10-28, run alone (`scripts/heavy.sh --alone`), warm file cache, one goroutine lists the folders and a worker per CPU (at most 8) reads and matches. The tree is made once per test binary by `speed_test.go` (a seeded generator, removed when the binary ends): 20,000 files of about 25 KB (500 MB in all) in 200 folders (`dNNN/src/`), made of lines of random words, with 1,000 of the files (one in twenty) holding the word `quokka` once; the search runs with the ignore files and the default `files.exclude` and `search.exclude` on, and symbolic links followed. The budget is that of SPEC section 14 "Search": the first results within 100 ms. `TestSpeedFirstResults` fails above it (ten times that under `-race`), and writes the times with `-v`; the same word is searched with GNU `grep -rF` (and `-rFi`) over the same tree, in the same test, for the comparison.

| What | Budget | Measured |
|---|---|---|
| First result, case sensitive literal | 100 ms | 0.78 ms |
| First result, ignoring case | 100 ms | 0.59 ms |
| The whole search (20,000 files, 500 MB, 1,000 files with a match), case sensitive | none | 57 ms |
| The whole search, ignoring case | none | 50 ms |
| `grep -rF` over the same tree (GNU grep, 1 thread) | none | 310 ms |
| `grep -rFi` over the same tree | none | 344 ms |

The same tree, measured while other builds of the repository ran on the machine: the whole search took 94 to 158 ms and `grep` 344 to 361 ms, so the ratio is about the same (5 to 6 times faster than `grep`, which has one thread).

The Go benchmark (`go test ./internal/core/search -run xxx -bench .`, 5 iterations):

| Benchmark | Time per search | Allocations |
|---|---|---|
| A literal over 20,000 files | 53.6 ms | 169,405 (16 MB) |
| A literal ignoring case over 20,000 files | 55.0 ms | 169,381 (16 MB) |
| A regular expression (`qu+okk?a\b`) over one folder of 100 files | 20.3 ms | 66,998 (4.7 MB) |
| A multiline regular expression (`quokka \w+\n`) over the same folder | 10.4 ms | 67,048 (9.9 MB) |

What made the numbers: a literal is found with `bytes.Index` (ignoring case: the vectorized `IndexByte` of both cases of the first byte, then a fold-compare of the rest), so most files cost one read and one scan; a file is read whole and a literal is searched in all of it (no line splitting), and the line of a match is found by a locator that only moves forward; the walk lists on its own goroutine, so the workers start on the first file at once.

The UI thread (`extensions/search/frames_test.go`, under the fake platform): with 20,000 matches in 2,000 files streaming into the view, the longest turn of the UI thread (the take of a batch of files, or the frame that shows it) was 10.7 ms, under the 16 ms of the budget; the results reach the UI thread at most every 30 ms (decision 158).

## Result grid (step 19.1)

`extensions/internal/resultgrid`, under the fake platform of `gluttertest` (software renderer, no GPU), on the machine above (AMD RYZEN AI MAX+ 395), on 2026-10-29, run alone (`scripts/heavy.sh --alone`), with a result of 100,000 rows of 8 columns (numbers, text, a NULL now and then, a date, binary data) read lazily by a `FuncSource` (the rows are made when the grid asks for them). The grid builds only the rows in view and a few around them (a sliver list with one row height), so a scroll does not depend on the size of the result. `budget_test.go` times the UI-thread work by the CPU clock of its OS thread (`CLOCK_THREAD_CPUTIME_ID`), runs a case several times, takes the best run and scales the budget by how much slower a piece of reference work was than on a quiet machine (the way `extensions/search/frames_test.go` does); under `-race` the budgets are ten times as large.

| What | Budget | Measured (budget test) | Go benchmark (`-bench . -benchtime 1s`) |
|---|---|---|---|
| Longest frame of 200 wheel scrolls through 100,000 rows | 16 ms | 1.58 ms | 0.70 ms per frame (5 rows built per frame, 5,500 allocations) |
| First frame of a result of 100,000 rows | 100 ms | 18.7 ms | 18.8 ms |
| Sort 100,000 rows by a column (client side) | 1 s | 26 ms | 19.2 ms |
| Copy all 100,000 rows as TSV | 2 s | 17 ms | 23.5 ms |
| Export 100,000 rows as CSV | none | | 15.8 ms |
| Export 100,000 rows as JSON | none | | 85.4 ms |

The rows built per frame (5 of 100,000) is also asserted by a test (`TestScrollingBuildsOnlyTheRowsInView`: at most 8 times the rows that fit in the view), so a change that lays out the rows off screen fails it on every machine, without a clock. The first frame is the sizing of the columns from the first rows plus the first build; the sort is the precomputation of one key per row and a stable sort; the export and the copy read a snapshot (`Model.Export`) and write row by row.

## Pending

`scripts/perf.sh --session` with the GPU was run for the table of step 10.1 on 2026-10-28 by the manager (see "Desktop pass (step 17b.6)"), except the 200 MB open and the Go benchmarks. What is left is in `docs/MANUAL-CHECKS.md` (3.11 and 10.1): those two, and the decision on the targets still missed.

## Git: a repository with 100,000 changed files (step 24.5)

`scripts/heavy.sh --alone -- scripts/perf-git.sh` builds the `many` fixture (`scripts/git-fixture.sh many`, 100,000 files, 7.1 s, the real `git` in an isolated environment), runs the Git extension on it against the fake API (`go test -tags emetos_perf ./extensions/gitscm -run TestPerfManyChangedFiles`, no window; `git.statusLimit` raised to 1,000,000, `git.untrackedChanges` separate) and the Changes view's budget test (`TestHugeChanges`, 100,000 resources, the processor time of the UI thread). Same machine as above, nothing else running (the script waits for both heavy slots).

| Measurement | Value | Target (SPEC §14) |
|---|---|---|
| First status, activation to the groups handed to the host | 686 ms | |
| First status: `git status` and the parse of its stream | 531 ms | recorded, not a target |
| First status: grouping and sorting, off the UI thread | 46 ms | within 150 ms |
| First status: hand-over to the host, on the UI thread | 10 ms | |
| Second status (git's caches warm): `git status` and the parse | 529 ms | recorded |
| Second status: grouping and sorting, off the UI thread | 48 ms | within 150 ms |
| Second status: hand-over on the UI thread | 29 ms | |
| Statuses for a burst of 5,000 file changes | 1 | 1 |
| Changes view, first frame after the update (processor time of the UI thread) | 66 ms (the model's update took 214 ms off the UI thread) | within 100 ms |
| Changes view, longest of 60 frames of a steady scroll | 9.5 ms (0 over 16 ms) | no frame over 16 ms |
| Changes view, longest frame after a jump | 10.9 ms | |

The grouping used to run on the UI thread (decision 490 of `docs/DECISIONS-TO-REVIEW.md`); it runs on the goroutine of the command now. Not measured here: the same run through the window of the app (the proof of the step runs the real extension and view on a repository with six changes; the 100,000-file budget of the view is `TestHugeChanges`). The idle check (no timers and no growth of goroutines in ten minutes of the fake clock with a repository open) is `TestIdleDoesNothing`.

## File decorations: 100,000 files (step 24.6)

`go test ./internal/core/decorations -run Huge -v` (the service alone) and `go test ./internal/gui/views/explorer -run 'Huge|CPU' -v` (the Explorer with 100,000 files and the service) through `scripts/heavy.sh --alone`. Times are the processor time of the thread that does the work (`CLOCK_THREAD_CPU`), as in step 24.3.

| Measurement | Value | Target |
|---|---|---|
| `Fire` of 100,000 files (a Git status that changed all of them), on the UI thread, in 50 chunks of 2,000 URIs | 229 ms in all, about 8 ms a chunk | each chunk within one frame, the whole within 500 ms |
| First `Get` of the root with the bubbled marks of 100,000 files | 45 ms | recorded |
| One row lookup (`Get` of a cached file) | 2.8 us | recorded |
| One file changes, then the root is asked again | 150 us | recorded |
| Explorer, median frame of a steady scroll, decorated / plain | 59.3 ms / 56.9 ms | within 10% of the plain Explorer (the 100,000-file frame itself is the budget of 24.3) |
| Explorer, first frame after the last chunk | 58 ms | recorded |

The cache is bounded at 250,000 entries; a provider that answers in more than 1 ms is logged and suspended after 20 slow answers (decisions 502 and 503 of `docs/DECISIONS-TO-REVIEW.md`).

## Quick diff of a 10,000-line file (step 24.11)

`scripts/heavy.sh --alone -- go test -tags glutter_release ./internal/gui/editor -run xxx -bench 'TypeCharacter10kLines$|TypeCharacter10kLinesWithQuickDiff|SetQuickDiffMarks' -benchmem` and `scripts/heavy.sh --alone -- go test ./internal/core/scm/quickdiff -run xxx -bench . -benchmem`. Same machine as above, nothing else running.

| Measurement | Value | Target |
|---|---|---|
| Diff of a 10,000-line file with 200 scattered edits against its original (`core/diff.ComputeLines` and the changes), off the UI thread | 3.5 ms | within 20 ms |
| The same through the model (snapshot, diff, changes), off the UI thread | 4.8 ms | marks within 50 ms of the end of the 250 ms pause |
| `SetQuickDiffMarks` with 200 marks in a 10,000-line file, on the UI thread | 0.30 ms | within one frame |
| One typed character in a 10,000-line file, without / with 200 marks | 0.24 ms / 0.37 ms | within one frame (16.6 ms) |

The marks are decorations of the document, so a typed character shifts them with the other decorations; the extra 0.13 ms is that and the repaint of the stripe. The original of a file is read once and kept by the host until the provider says it changed.

## Git: the cross-checks of the milestone (step 24.23)

The measurements of SPEC §14 for Git, repeated on the finished milestone, each through `scripts/heavy.sh --alone` with nothing else of this run going on. **Machine: AMD Ryzen 9 6900HX with Radeon Graphics (16 threads), 61 GB, Linux 7.2 (Fedora 44), Go 1.27.3, headless** (the machine of the other sections of this file is a Ryzen AI Max+ 395: the figures below are not comparable with theirs, and the GPU was not used; the numbers with the GPU are the user's, `docs/MANUAL-CHECKS.md` 24.23). The commit hash is the one of the step.

**Source Control with 100,000 changed files** (`scripts/perf-git.sh`, the `many` fixture built in 12.8 s):

| Measurement | Value | Target (SPEC §14) |
|---|---|---|
| First status, activation to the groups handed to the host | 726 ms | |
| First status: `git status` and the parse of its stream | 516 ms | recorded, not a target |
| First status: grouping and sorting, off the UI thread | 40 ms | within 150 ms: met |
| First status: hand-over on the UI thread | 31 ms | |
| Second status: `git status` and the parse / grouping / hand-over | 514 ms / 40 ms / 26 ms | within 150 ms: met |
| Statuses for a burst of 5,000 file changes | 1 | 1: met |
| Changes view, first frame after the update (processor time of the UI thread) | 76.9 ms (the update of the model took 199 ms off the UI thread) | within 100 ms: met |
| Changes view, longest of 60 frames of a steady scroll | 9.5 ms (0 over 16 ms) | no frame over 16 ms: met |
| Changes view, longest frame after a jump | 11.4 ms | |

**The Graph on a generated repository of 100,000 commits** (`go test -tags emetos_perf ./extensions/gitscm -run TestPerfLongHistory`, the repository built in 8.7 s, and the view tests `TestAVeryLongHistoryIsReadPageByPageWithTheCostOfAPage` and `TestScrollingALongGraphIsSmooth`):

| Measurement | Value | Target |
|---|---|---|
| First page of 50 commits, with git's commit-graph file | 5.3 ms | the first rows within 300 ms: met |
| First page without a commit-graph file | 476 ms | the same target: **not met without the file** (git's own walk of 100,000 commits; Git writes the file by itself on `git gc` and with `fetch.writeCommitGraph`; known since step 24.20, decision 853) |
| The page at commit 1,000 / 50,000 / 99,900 | 4.9 ms / 25.8 ms / 46.9 ms | |
| Search by message over the 100,000 commits | 1.14 s | recorded, off the UI thread |
| Refs, common ancestor of two references | 3.1 ms, 43 ms | |
| The view, longest of 60 frames of a steady scroll over the rows | 12.5 ms (0 over 16 ms) | no frame over 16 ms: met |
| The view, longest frame after a jump | 19.6 ms | recorded (a jump builds a whole screen of new rows; it is not a steady scroll) |

**Quick diff of a 10,000-line file** (the commands of step 24.11 again): the diff of a file with 200 scattered edits against its original 3.5 ms (`BenchmarkRecompute10000Lines`), through the model 4.8 ms (`BenchmarkModelCompute10000Lines`), both within the 20 ms and 50 ms of §14; `SetQuickDiffMarks` with 200 marks 0.30 ms; a typed character in a 10,000-line file 0.23 ms without and 0.36 ms with 200 marks (the target of §14 for typing is 4 ms).

**Idle**: `TestIdleDoesNothing` (a repository with a change open, ten minutes of the fake clock: no status run, no timer pending, no goroutine more) passes; autofetch is off until the user answers. The window of the app was not measured for CPU in this step: the idle rows of §14 (focused 1% with the GPU, the frames of an unfocused window) are `scripts/perf.sh --session` on the user's machine.

Nothing here is over its target except the first page of a history that has no commit-graph file, which was known and is git's time, not Emetos's.

## Language servers (step 24b.10)

The real servers of milestone M24b, in the real editor: the time from the start of the process to the first diagnostics of each server on its project of `testdata/`, and the frames of the UI thread while text is typed with the server running. **Machine: AMD Ryzen 9 6900HX with Radeon Graphics (16 threads), 61 GB, Linux 7.2.13 (Fedora 44), Go 1.27.3, sway 1.11 headless (pixman), Mesa llvmpipe as the GLES renderer, Node 22.23.1, OpenJDK 25.0.4.1, .NET SDK 10.0.113, clangd 22.1.8** (the machine of the Git section above, not the Ryzen AI Max+ 395 of the first sections: the figures are not comparable with theirs). The GPU was not used and the NVIDIA card was kept out; the numbers with the GPU are the user's pass (`docs/MANUAL-CHECKS.md` 24b.10). The servers are the pins of `scripts/tool-versions.txt` in `/var/tmp/emetos-tools/` (rust-analyzer 2026-10-26, basedpyright 1.40.2, Pyright 1.1.414, python-lsp-server 1.15.0, typescript-language-server 6.0.2 with TypeScript 6.0.3, jdtls 1.63.0, csharp-ls 0.28.0) and the system's clangd.

**Method.** `TestPerfLangServers` (`cmd/emetos/langservers_perf_display_test.go`, tag `emetos_display`; it is not in the default run of `scripts/realinput.sh`) starts the editor in the headless compositor through `internal/testenv/display`, with a copy of the project, a `settings.json` that names the server, `GLUTTER_FRAME_STATS=1`, and the Problems panel opened by a dev command at the first frame. The binary is the release build with the dev commands (`go build -tags glutter_release,emetos_dev`), named by `EMETOS_DISPLAY_BINARY`. Each server is a case, and each case is a fresh editor (a new temporary `HOME` and `XDG_*`, so a server starts cold: no workspace of jdtls, no restored solution of csharp-ls, no cache). Everything ran through `scripts/heavy.sh --alone`, the whole test three times in one run (`-count=3`), and the table gives the median of the three:

```sh
go build -tags glutter_release,emetos_dev -o /var/tmp/emetos-perf/emetos ./cmd/emetos
EMETOS_DISPLAY_BINARY=/var/tmp/emetos-perf/emetos EMETOS_REALINPUT_RACE=0 scripts/heavy.sh --alone -- \
  scripts/realinput.sh -- -tags emetos_display ./cmd/emetos -run TestPerfLangServers -v -count=3
```

- **First diagnostics**: the time, from before `display.Start` spawns the process, until a text of the first diagnostic's message is drawn in the Problems panel (polled every 50 ms through `dev.texts`; the project's error: the type mismatch of `testdata/<language>`, the undeclared name in C++, the undefined name in Python). The first frame of the window is at 0.7 to 0.8 s in every run, which is Emetos and Glutter's own start and part of each figure; the server's own share is the figure minus that. The Rust fixture has no error, so the case writes the text of the real-server test of the pack (`let wrong: i32 = "text";`); the first diagnostic is rust-analyzer's own ("expected i32, found &'static str", from the analysis of the crate); `cargo check` follows with rustc's "mismatched types". Python runs on the system's `python3` with no `.venv`.
- **Typing**: after the diagnostics, the cursor goes to the end of the file and 14 comment lines of 44 characters each ("// the quick brown fox jumps over the lazy dog") are typed with real key events (10 ms between keys, Return after each line), 644 to 658 keys in about 7 s. The figures are Glutter's frame statistics over the windows of that time: the average and the worst time of the UI thread (build, layout and paint: SPEC §14's "UI-thread time of the frame that shows it"; the target is under 4 ms for the Go file with the GPU) and the renderer's, which here is llvmpipe's (23 to 25 ms a frame: every frame is over 16 ms headless, which says nothing about the GPU). The **baseline** is the same typing in the same file with no server running: Python with `python.languageServer` `none`, TypeScript and C++ with a server path that does not exist. The grammar of the language decides most of the frame (below), so each server is compared with the baseline of its own language.

| Server | First diagnostics (3 runs) | Median | UI thread while typing, average | worst frame | No-server baseline, average / worst |
|---|---|---|---|---|---|
| rust-analyzer | 1.52 s, 1.25 s, 1.23 s | **1.25 s** | 2.24 ms | 7.9 ms | (no Rust baseline run) |
| clangd | 1.17 s, 1.29 s, 1.15 s | **1.17 s** | 3.78 ms | 7.2 ms | C++: 3.73 ms / 8.6 ms |
| basedpyright | 1.94 s, 1.99 s, 2.17 s | **1.99 s** | 2.09 ms | 6.2 ms | Python: 1.82 ms / 4.2 ms |
| Pyright | 2.02 s, 2.16 s, 2.11 s | **2.11 s** | 1.98 ms | 5.0 ms | Python: 1.82 ms / 4.2 ms |
| python-lsp-server | 1.68 s, 1.68 s, 1.58 s | **1.68 s** | 1.93 ms | 5.0 ms | Python: 1.82 ms / 4.2 ms |
| typescript-language-server | 2.97 s, 2.75 s, 3.00 s | **2.97 s** | 2.49 ms | 5.8 ms | TypeScript: 2.28 ms / 4.5 ms |
| jdtls | 6.18 s, 6.05 s, 6.14 s | **6.14 s** | 2.35 ms | 5.7 ms | (no Java baseline run) |
| csharp-ls | 10.09 s, 9.81 s, 9.82 s | **9.82 s** | 2.19 ms | 5.1 ms | (no C# baseline run) |

(The first-diagnostics times include the 0.7 to 0.8 s before the first frame. jdtls: the Eclipse compiler's diagnostics for a project with no build file; with a Maven or Gradle project its import needs the network and is not measured. csharp-ls: the load of a one-project solution with the SDK's reference pack already on disk and no package source. Rust, C++, Python and TypeScript projects are one to three files each, so these are the floor of each server, not the time of a big project.)

**What the figures say.**
- **No server adds to the typing frame.** Every server's average UI-thread time is within 0.2 ms of the baseline of its language (clangd 3.78 against 3.73, TypeScript 2.49 against 2.28, the three Python servers 1.93 to 2.09 against 1.82), and the worst frames (5 to 8 ms) are the same as without a server. The requests a typed character causes (`didChange`, the refreshed diagnostics, semantic tokens, inlay hints, code lenses) are answered off the UI thread and cost it a fraction of a millisecond. All the figures are under the 4 ms of SPEC §14 for the average frame; the worst frame of the Rust and C++ runs is over 7 ms (C++ also without the server: 8.6 ms), the others 5 to 6 ms.
- **The time to the first problems is the server's own start.** The small servers (clangd, rust-analyzer, Pyright, basedpyright, pylsp, typescript-language-server) answer within 1.2 to 3 s of the process start, 0.5 to 2.2 s after the window's first frame; jdtls takes 6 s (a JVM and the OSGi bundles of Eclipse, its workspace made at that start) and csharp-ls 10 s (the runtime, MSBuild's load of the solution and Roslyn's first compilation).
- **A long line costs more than the server.** A first version of the measurement typed the same 620 characters as **one** line, and the UI thread took 11.0, 10.7 and 11.3 ms on average (59.7, 42.3 and 83.6 ms at worst) with typescript-language-server, and 5.2 to 5.3 ms with clangd, against 2.6 to 3.1 ms for the others. A CPU profile of the typing (`EMETOS_LANGPERF_PROFILE`, `SIGUSR2`, `go tool pprof`) put 62% of the samples of the TypeScript run in `textmate.(*Grammar).TokenizeLine` over `regexp2` and under 3% in the language server's code; the same typing with the server turned off was as slow. The tokenizer reads the whole line again for each character typed, so a line of several hundred characters costs a few milliseconds a key for the TypeScript and C++ grammars, whatever server runs; lines of 44 characters, which is what a person types, do not. This belongs to the highlighter and not to the language servers; it is a decision to review (DECISIONS-TO-REVIEW 1087), not changed in this step.

The figures of one run each of the long-line version, from `/var/tmp/emetos-perf/run1.log`, are kept here because they found the cost: clangd 5.35 / 5.16 / 5.28 ms (9.7 / 10.8 / 9.9 ms worst), basedpyright 3.05 / 3.07 / 3.06, Pyright 2.95 / 2.95 / 2.92, python-lsp-server 2.84 / 2.86 / 2.86, jdtls 2.90 / 2.88 / 2.95, csharp-ls 2.77 / 2.77 / 2.81, no server (Python) 2.64 / 2.71 / 2.66.

## Run and Debug (step 25.12)

The measurements of SPEC §14 for Run and Debug, taken on the finished milestone, each through `scripts/heavy.sh --alone` with nothing else of this run going on. **Machine: the one of the sections above (Linux 7.2.13, Fedora 44, Go 1.27.3, sway 1.11 headless with pixman, Mesa llvmpipe as the GLES renderer), the GPU not used and the NVIDIA card kept out** (the numbers with the GPU are the user's pass, `docs/MANUAL-CHECKS.md` 25.12). The debugger is the pinned dlv v1.27.2 of `/var/tmp/emetos-tools`. The two views are measured as the UI thread's processor time of build, layout and paint with the fake platform (no renderer), as the Git views are (`threadCPU`: the work of the thread, which a busy machine does not stretch); stepping is measured in the real editor.

**Stepping 100 times with dlv** (`TestPerfDebugStepping`, `cmd/emetos/debug_perf_display_test.go`, tag `emetos_display`, run with a release build with the dev commands; not in the default run of `scripts/realinput.sh`). A Go program with a loop of 1,000 iterations (`total += i * 2` and `total %= 7919`) is debugged with F5; F10 is pressed 100 times through the virtual keyboard; the time of a step is taken from before the key is sent to the first poll in which the line of the top frame in the Call Stack differs from the one before (the badge of the frame, which the model sets in the same pass as the stopped line of the editor), polled every 2 ms. The harness costs inside each figure: **a question to the window (the texts it draws) takes 40.6 ms**, so the figures have a resolution of about 40 ms and are upper bounds; a key press takes 0.09 ms. The figures below are of the run after the fixes of "What the figures say" (the harness took the breakpoint's badge in the Breakpoints view for the top frame while the thread ran, and the first run was measured so); the run before them: median 161 ms, 12 presses repeated.

```sh
go build -tags glutter_release,emetos_dev -o steprelease.out ./cmd/emetos
EMETOS_DISPLAY_BINARY=$PWD/steprelease.out EMETOS_REALINPUT_RACE=0 scripts/heavy.sh --alone -- \
  scripts/realinput.sh -- -tags emetos_display ./cmd/emetos -run TestPerfDebugStepping -v -count=1
go test -tags emetos_dlv ./extensions/golang -run TestDlvStepTime -v -count=1   # dlv alone, over its DAP connection
```

| Measurement | min | median | average | p90 | max |
|---|---|---|---|---|---|
| F10 to the new line in the Call Stack, 100 steps, the editor with dlv, headless | 119 ms | **235 ms** | 230 ms | 313 ms | 387 ms |
| dlv alone: `next` to `stopped` (the same loop, 100 steps, no editor) | 2.3 ms | 2.6 ms | 2.8 ms | 3.1 ms | 17.9 ms |
By move (median, count): line 5 to 6: 246 ms (33), 6 to 7: 244 ms (34), 7 to 5: 211 ms (33). No press of F10 was ignored in 100.

By move (median, count): line 5 to 6: 140 ms (24), 6 to 5: 128 ms (21), 6 to 7: 354 ms (28), 7 to 5: 254 ms (3), 7 to 6: 186 ms (24). The frame statistics of the run (`GLUTTER_FRAME_STATS=1`) while stepping: the UI thread's build 2.4 ms on average and 10 ms at most in the last window, the renderer (llvmpipe) 27 ms on average and 72 ms at most, 17 of 25 frames over 16 ms: every frame of a headless run is late, which says nothing about the GPU.

**The Variables view with a variable of 10,000 children** (`TestPerfVariablesTenThousandChildren`, `internal/gui/app/debugperf_test.go`, tag `emetos_perf`; the adapter answers the expansion of `order` with 10,000 variables in one reply, which is the worst case for the view: with paging the view asks for chunks of 100):

```sh
scripts/heavy.sh --alone -- go test -tags 'emetos_perf glutter_release' ./internal/gui/app -run TestPerfVariables -v -count=1
```

| Measurement | Value |
|---|---|
| The expansion to the model with the 10,000 rows (the request, the reply, the wrapping), wall time | 50 ms |
| The first frame after the rows (UI thread) | 2.8 ms |
| 60 frames of a steady scroll: average / longest | 2.6 ms / 4.7 ms (0 over 16 ms) |
| A frame with nothing changed | 1.7 ms |
| Collapsing it again | 2.3 ms |

**The Debug Console with 100,000 lines** (`TestPerfConsoleHundredThousandLines`, `internal/gui/views/debug/perf_test.go`, tag `emetos_perf`; lines of 78 characters with an ANSI color; `TestConsoleHundredThousandLinesStayInTheFrameBudget` is the budget in the normal run):

```sh
scripts/heavy.sh --alone -- go test -tags 'emetos_perf glutter_release' ./internal/gui/views/debug -run TestPerfConsole -v -count=1
```

| Measurement | Value |
|---|---|
| The 100,000 lines into the model (`AppendToRepl`, wall time with the hand-over to the UI thread) | 184 ms, 27 MB of heap |
| The first frame after them (UI thread) | 6.7 ms |
| 60 frames of a steady scroll up / down: average, longest | 3.6 ms, 9.8 ms / 3.0 ms, 5.8 ms (0 over 16 ms) |
| 20 frames while 1,000 lines arrive in each: the longest | 4.0 ms |
| The filter `line 5` over the 100,000 lines (40,951 match) | UI thread: **30 ms** in all and **5.8 ms** the longest frame (was 561 ms in one frame); the rows are there 622 ms after the key (300 ms of debounce, the match on a goroutine, the swap) |

**What the figures say.**
- **dlv is not the cost of a step.** dlv answers `next` in 2.6 ms and the stack, scopes and variables of the new line 0.8 ms later. The 161 ms median of the editor is the editor's own work plus the harness's 40 ms of polling plus a software renderer that needs 27 ms for a frame (and 354 ms for the move from line 6 to 7, the one in which the value of `total` changes and the Variables view and the inline values change with it); on the GPU the renderer's part is a few milliseconds. The number to hold is the user's, with the GPU (`docs/MANUAL-CHECKS.md` 25.12, "does each key move the line at once"), and the target of §14 is set on it, not on this one (decision 1505).
- **A key pressed 60 ms after the line moved was ignored 12 times in 100: a harness artifact, and under it a real delay (fixed on `fix/debugger-issues`, decision 1505).** The model was right: `debugState` is `stopped` from the moment the thread is marked stopped, before its frames are loaded, so no frame paints a new line before the state says `stopped` (`TestF10IsHonouredWhenTheLineOfTheStepIsShown`, 100 steps, and the same with dlv under `emetos_dlv`). What the harness saw as "the line moved" was, in the ignored cases, the line badge of the breakpoint in the Breakpoints view, which the poll took for the top frame while the thread was running and its stack empty; the press that followed met a running thread, as it should. Tracing the run showed the real delay behind it: the rest of a stop's stack is loaded 420 ms after the stop (VS Code's delay), and Emetos did that wait on the session's queue, so the stop of a step pressed within 420 ms of the last one waited for it (line and editor showed up to 420 ms late, `rawUpdate` of the new stop 100 to 200 ms after its stopped event), and the editor opened the source of a stop only after that wait. The wait is now off the queue (`loadRestOfStack`), `EventStopped` comes with the first frame, and a thread that ran again is not loaded. The harness takes only badges above the Breakpoints view now: 0 presses repeated in 100.
- **The views are far inside their budget.** 10,000 children cost the UI thread 2.8 ms for the first frame and 2.6 ms on average while scrolling, the console with 100,000 lines 3 to 3.6 ms, 0 frames over 16 ms in either.
- **The console's filter was the one thing over a budget, and is not now.** Each change of the filter text ran the fuzzy match of VS Code's `matchesFuzzy` over every line on the UI thread: 561 ms for 100,000 lines. A console of 1,000 elements or more now waits 300 ms for the next key (VS Code's filter boxes wait 300 ms too), matches the lines on a goroutine and swaps the rows in on the UI thread, dropping the result of an older text (a generation number), and the answers are kept so that new rows do not match the old lines again (decision 1506). Measured as above: 30 ms of the UI thread in all, 5.8 ms the longest frame, `TestConsoleFilterOfHundredThousandLinesStaysInTheFrameBudget` keeps every frame within 16 ms.

## Testing (step 26.10)

The measurements of SPEC §14 for Testing, taken on the finished milestone, each through `scripts/heavy.sh --alone` with nothing else of this run going on. **Machine: the one of the sections above (Linux 7.2.13, Fedora 44, Go 1.27.3, the GPU not used and the NVIDIA card kept out; nothing here draws through a renderer), and the user's pass with the real GPU is `docs/MANUAL-CHECKS.md` 26.10.** The test is `TestPerfTestingFiveThousandGoTests` (and `TestPerfTestingFullRunWithTestResultsClosed`), `internal/gui/app/testingperf_test.go`, tag `emetos_perf`: a Go module that the test writes into a temporary folder, with **5 packages of 10 files of 100 tests (5,000 tests, 5,055 items with the files and packages)**, each a four-line test, one in 250 failing (`t.Errorf`, 20 failures), in a window with the Go extension and **the real `go`** (a build cache of the test's own, no network; `GOFLAGS=` and `GOTOOLCHAIN=local`). The views are measured as the UI thread's processor time of build, layout and paint with the fake platform (`threadCPU`, as the Debug and Git views are), a frame at a time from `FlushPosted` and `Pump`, 60 frames a second while a run goes on.

```sh
scripts/heavy.sh --alone -- go test -tags 'emetos_perf glutter_release' ./internal/gui/app -run TestPerfTesting -v -count=1
```

| Measurement | Value |
|---|---|
| Discovery when the Go extension starts: from the activation to the tree of 5,055 items (the window's file list read, 50 files parsed with `go/parser`) | 49 ms (58 ms in the first run) |
| Refresh Tests (every `_test.go` read again, unchanged files keep their items): wall time, and the UI thread meanwhile | 17 to 21 ms; 5 frames of 24 µs on average |
| The Test Explorer as a **list of 5,000 rows**: the first frame after the change | 0.9 ms |
| 60 frames of a steady scroll: average / longest | 4.0 ms / 6.8 ms (0 over 16 ms) |
| A frame with nothing changed | 0.3 ms on average |
| The Test Explorer as a **tree with everything expanded (5,055 rows)**: the first frame after the change | 0.3 ms |
| 60 frames of a steady scroll: average / longest | 3.9 ms / 7.7 ms (0 over 16 ms) |
| **A full run of the 5,000 tests, Test Results closed** (`testing.openTesting` `neverOpen`; the tree collapsed to the packages): wall time (build cache warm) | 4.6 s |
| The UI thread, 267 frames while the results stream in: in all / average / longest / frames over 16 ms | 178 ms / 0.67 ms / 119 ms / 2 |
| **The same run with Test Results open** (the default: it opens at the first failure and shows the run's output, 250,039 bytes as a list of lines built when in view; before the fix of decision 1725 the output was one paragraph) | 4.7 s wall time |
| The UI thread, 266 frames: in all / average / longest / over 16 ms | 210 ms / 0.79 ms / 140 ms / 2 (before: 3.98 s / 14.7 ms / **3.91 s (one frame)** / 2) |
| A second full run, the Explorer as a list of 5,000 rows, Test Results open | 0.5 s (the tests are cached); the UI thread 366 ms in 4 frames, the longest 180 ms (before: 4.4 s in all, the longest frame **4.2 s**) |

The result of each run is 5,010 passed and 45 failed nodes (the 20 failing tests and the files and packages above them).

**What the figures say.**
- **The tree and the Explorer are far inside their budgets.** Finding 5,000 tests takes 50 ms, and 5,000 rows scroll at 4 ms a frame (0 frames over 16 ms) as a list or a tree: the rows are virtual. During a run (Test Results closed) the window costs the UI thread 0.67 ms a frame on average (the summary line, the badge, the rows that change), with 2 frames over 16 ms and the longest 119 ms; which work that frame holds was not traced (the 5,000 queued states arrive at the start of a run, and the model of the 5,055 items is made again for them).
- **Test Results was the one thing over a budget, and is fixed (decision 1725):** it laid out the whole 250 KB output of the run as one paragraph (`kit.AnsiText` inside a scroll view), and Glutter's text layout (`paragraph.cut`, 65% of the processor time of the whole test) took 3.9 to 4.2 s on the UI thread in one frame at the end of the run. The output is now a `ListView` of lines (`kit.AnsiLines` splits the parsed text, the style is carried from line to line, one `kit.AnsiText` per line is built when it is in view), so the run costs the UI thread 210 ms in all with the pane open against 181 ms with it closed; the longest frame (140 ms and 125 ms) is the same with the pane closed, so it is not the pane. A widget test keeps 100,000 lines of output inside the frame budget (`TestResultsOutputOfHundredThousandLinesStaysInTheFrameBudget`).

## HTTP Client (step 27.9)

The measurements of SPEC §14 for the HTTP client, each through `scripts/heavy.sh --alone` with nothing else of this run going on. **Machine: the one of the sections above (Linux 7.2.13, Fedora 44, Go 1.27.3, the GPU not used and the NVIDIA card kept out; nothing here draws through a renderer), and the user's pass with the real GPU is `docs/MANUAL-CHECKS.md` 27.9.** The tests are `TestPerfCollectionOfAThousandRequests`, `TestPerfFiftyMiBJSONResponse` and `TestPerfRunnerOverFiveHundredRequests`, `extensions/httpclient/perf_test.go`, tag `emetos_perf`. They run the extension over the fake extension API with the widgets in a `gluttertest` window (the extension's own build, layout and paint with the fake platform, not the whole window with its shell), the test goroutine being the UI thread locked to its OS thread; the figures are that thread's processor time for each frame, as in the other view measurements. Every request goes to a server of the test on 127.0.0.1; the 50 MiB body is written by the server in 64 KiB chunks and is never held whole by the test.

```sh
scripts/heavy.sh --alone -- go test -tags 'emetos_perf glutter_release' ./extensions/httpclient -run TestPerf -v -count=1
```

**A collection of 1,000 requests in the side bar** (one collection of 10 folders of 100 requests, 1,002 files with their folders' files):

| Measurement | Value |
|---|---|
| From the start of the extension to the whole tree (the files read and parsed in the background, polled every 2 ms) | 33 ms |
| Making the rows again from the tree of the store (`SetItems` of 1,000 requests; what a change of a file costs the UI thread): average / longest of 5 | 0.34 ms / 0.97 ms |
| Open: the view in a new window, its first frames (includes making the test window) | 39 ms |
| Expand: one folder of 100 requests after the other (10 frames): average / longest | 1.7 ms / 4.9 ms |
| All folders open (1,000 request rows): the first frame | 0.96 ms |
| 60 frames of a steady scroll: average / longest (frames over 16 ms) | 3.3 ms / 5.6 ms (0) |
| A frame with nothing changed | 0.29 ms on average (median 50 µs) |
| A rebuild of the rows and the frame that draws it, everything open: average / longest of 5 | 2.7 ms / 4.8 ms |

**A 50 MiB JSON response** (an array of objects, 52,428,736 bytes, `httpClient.maxResponseMiB` 50 as shipped). Pretty is limited to 2 MiB (`maxPretty`, decision 1821), so what the user gets is the Raw view in pages of 64 KiB (800 pages) with the note "The body is 50.0 MB: too large to format here, shown as it is in pages.":

| Measurement | Value |
|---|---|
| Sent and read (loopback, the body held in memory: it is within the limit) | 222 ms of wall time, off the UI thread; the UI thread's callbacks meanwhile: 186, the longest 0.22 ms |
| What the pane builds when the response arrives (`buildDisplay`: the first 512 bytes and the page count) | 6 µs |
| Heap in use before / after the response (the garbage not yet collected and the test server's work included) | 80 MiB / 211 MiB (before the fix of decision 1910: 82 MiB / 320 MiB) |
| Heap after a collection with the response held (the test server holds one chunk of 64 KiB) | 128 MiB: 0.96 bodies above the 80 MiB before (before the fix: about 2 bodies, the buffer having doubled) |
| The first page shown (first frames in a new window, includes making the test window) | 23 ms |
| Page 1 with nothing changed: average / longest of 20 frames | 28 µs / 0.21 ms |
| 100 page turns (Next page: the tap, the read of one page and its frame): average / longest (frames over 16 ms) | 2.4 ms / 4.9 ms (0) |
| 60 frames of a scroll inside a page: average / longest | 90 µs / 0.88 ms |
| Last page | 2.7 ms |
| For comparison, **a 2 MiB JSON response, the largest that has Pretty**: sent, read and displayed (255,332 lines of indented text) | 237 ms of wall time, off the UI thread |
| Its first frames (includes the test window); 60 frames of a steady scroll: average / longest | 29 ms; 79 µs / 0.4 ms |

**The runner over 500 requests** (a collection of 500 requests, each with an extraction and two checks, against a local server that answers each with a small JSON; the runner tab open on its table; frames at 60 a second):

| Measurement | Value |
|---|---|
| The run: wall time, requests served, results | 200 ms, 500, 500 |
| The UI thread while the results stream in, 13 frames: in all / average / longest / frames over 16 ms / over 100 ms | 68 ms / 5.2 ms / 11.1 ms / **0** / 0 (before the fix of decision 1911: 9 frames, 211 ms / 23.5 ms / 28.3 ms / 9 / 0) |
| The same frames by wall time (whatever the thread was doing) | average 5.9 ms, longest 12.4 ms |
| After the run, a frame with nothing changed: average / longest of 10 | 0.21 ms / 0.98 ms (median 11 µs) |
| After the run, 60 frames of a steady scroll through the 500 results: average / longest (over 16 ms) | 2.8 ms / 5.9 ms (0) |

**What the figures say.**
- **No frame of any of the three is over 100 ms, and no UI-thread freeze was found.** The side bar's 1,000 rows are virtual: a scroll costs 3.3 ms a frame, and a change on disk makes the rows again in under a millisecond.
- **The runner's frames are within the 16 ms budget (13 frames, the longest 11.1 ms; they were 9 frames of 23 to 28 ms).** The cost was not the results: each one refreshed the tab, and the left column built and laid out a row for each of the 500 requests every time (about 26 ms a frame whether one result or 55 came in). That list is now built lazily, only the rows in view (decision 1911); the default-run test `TestFiveHundredStreamedResultsStayInTheFrameBudget` keeps it so.
- **A 50 MiB body is not a freeze, and it is not formatted:** the user gets the Raw pages at once, a page turn costs 2.4 ms. Formatting stops at 2 MiB on purpose (a JSON of 2 MiB is 255,332 lines of Pretty text, made off the UI thread in 237 ms in all). The body is held once (decision 1910): after a collection the heap is 0.96 bodies above its start (it was about 2: the engine's buffer doubled); the 211 MiB right after the response (320 MiB before) still has the garbage of the read and of the test server, which the next collection takes.
- **Not measured:** the whole window with its shell (the side bar's container, the tab strip): these are the extension's views and tabs in a test window; and the real GPU (`docs/MANUAL-CHECKS.md` 27.9).
