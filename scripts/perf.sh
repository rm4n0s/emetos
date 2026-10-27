#!/usr/bin/env bash
# Measures every performance target of SPEC §14: the ones that need the running app (typing and
# scrolling frames, opening a 10 MB and a 200 MB file, the cold start with the workbench and with a
# restored session, the idle CPU focused and not, the resident memory) and, with Go benchmarks, the
# ones of the UI thread alone, the fuzzy file search and the loading of the grammars
# (docs/PERFORMANCE.md).
#
# Usage: scripts/perf.sh [--session] [-n runs] [-s seconds] [--no-go] [--no-huge] [--profile dir]
#
#   --session    run in the Wayland session of the caller, with its GPU, instead of a headless
#                compositor with a software renderer. The numbers of the targets are those of
#                this mode (SPEC §14); the headless ones are recorded but do not decide
#   -n runs      starts of the editor to take the median cold start of (default 5)
#   -s seconds   how long the idle CPU is measured (default 10)
#   --no-go      do not run the Go benchmarks (the table has no rows for them)
#   --no-huge    do not make and open the 200 MB file (it needs 1.5 GB of memory in the editor)
#   --profile d  write a CPU profile of the measuring time, and the heap profile after it, of the
#                runs of the editor to d/<run>/ (cpu-1.pprof, heap-1.pprof, allocs-1.pprof; the runs are
#                named idle, typing-1..3, scrolling-1..3, big, huge, restore-1..3...): the
#                dev build can profile itself (cmd/emetos/profile_dev.go); the binary that made
#                them is copied to d/emetos, so that `go tool pprof -top -cum d/emetos
#                d/scrolling-1/cpu-1.pprof` reads them
#   PERF_KEEP=dir  in the environment, copies the logs of every run (<run>.log, .during, .sample) to dir
#
# It builds the editor with -tags glutter_release,emetos_dev (a release build whose dev commands
# can drive it), generates the files to open in a temporary folder, and runs each measurement in
# an isolated app (scripts/isolated.sh: nothing of the user's configuration is read), with
# GLUTTER_FRAME_STATS=1, which makes the app log its frame times once a second. The editor
# is driven with dev commands (dev.type, dev.scroll). Glutter's counter example is built and
# measured the same way, as the baseline of the start and the memory of a window.
#
# The cold start is measured in three states of the shader caches (Glutter 1.7 keeps GLES program
# binaries in $XDG_CACHE_HOME/glutter/shaders, and the driver, Mesa, has its own cache): both
# empty (a machine that never ran the app), the driver's warm and Glutter's empty, and both warm
# (the second start of a user). isolated.sh gives every run a fresh XDG_CACHE_HOME, so the runs
# that need a warm cache of Glutter's are given a folder of the temporary work folder instead
# (PERF_CACHE; the user's cache is never touched), and the Mesa cache is moved the same way
# (MESA_SHADER_CACHE_DIR). The parts of the first draw come from the log of GLUTTER_FRAME_STATS=1.
#
# The idle CPU without the focus is measured headless too: a second window (Glutter's counter)
# is started, the compositor activates it, and the editor's window loses the focus (Glutter 1.7
# takes the view focus from the active window of the compositor).
#
# Headless runs set GLUTTER_RENDERER=gles (unless the environment sets it): Vulkan is tried first by `auto` and costs 600 ms
# where the compositor presents no GPU buffers. A --session run sets nothing.
#
# Needs sway for the headless mode (scripts/headless-wayland.sh). The output is a Markdown table.
set -euo pipefail

cd "$(dirname "$0")/.."
root="$(pwd)"

mode=main
session=0
runs=5
seconds=10
go_bench=1
huge=1
profile=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --session) session=1; shift ;;
    -n) runs="$2"; shift 2 ;;
    -s) seconds="$2"; shift 2 ;;
    --no-go) go_bench=0; shift ;;
    --no-huge) huge=0; shift ;;
    --profile) profile="$(mkdir -p "$2" && cd "$2" && pwd)"; shift 2 ;;
    --one) mode=one; shift; break ;;
    --inner) mode=inner; shift ;;
    *) echo "usage: $0 [--session] [-n runs] [-s seconds] [--no-go] [--no-huge] [--profile dir]" >&2; exit 64 ;;
  esac
done

# ---------------------------------------------------------------------------------------------
# --one: one measured run, inside scripts/isolated.sh. The arguments:
#   <out dir> <name> <binary> <file or -> <dev commands or -> <settle s> <measure s> <other binary or ->
# It starts the app, waits for its first frame and the settling, optionally starts another window
# to take the focus away, samples the process for the measuring time, and leaves the log of
# the app, and the samples, in the out dir. With PERF_SETUP (dev commands) the app is run once
# before, with those commands, and ended with SIGTERM, so that the measured run restores what they
# left (tabs, editors); the log of that run is dropped. With PERF_PROFILE the measuring time is
# profiled (the dev build only).
# ---------------------------------------------------------------------------------------------
if [[ $mode == one ]]; then
  out="$1" name="$2" bin="$3" file="$4" commands="$5" settle="$6" measure="$7" other="$8"
  export GLUTTER_FRAME_STATS=1
  # a cache folder of the work folder where a warm cache of Glutter's is wanted
  [[ -n "${PERF_CACHE:-}" ]] && export XDG_CACHE_HOME="$PERF_CACHE"
  args=()
  [[ "$file" != "-" ]] && args+=("$file")
  [[ "$commands" != "-" ]] && export EMETOS_DEV_COMMANDS="$commands"
  logs() { cat "$XDG_STATE_HOME"/emetos/logs/*/main.log 2>/dev/null || true; }
  cpu_ticks() { awk '{ sub(/^.*\) /, ""); print $12 + $13 }' "/proc/$1/stat"; }
  # app_ticks is the CPU of the threads that are not the software renderer's (llvmpipe-N): on a
  # GPU the drawing is not done by the CPU, so what is left is the cost of the app itself.
  app_ticks() {
    local t sum=0
    for t in /proc/"$1"/task/*; do
      case "$(cat "$t/comm" 2>/dev/null)" in llvmpipe*) continue ;; esac
      sum=$((sum + $(awk '{ sub(/^.*\) /, ""); print $12 + $13 }' "$t/stat" 2>/dev/null || echo 0)))
    done
    echo "$sum"
  }

  if [[ -n "${PERF_SETUP:-}" ]]; then
    EMETOS_DEV_COMMANDS="$PERF_SETUP" "$bin" >"$out/$name.setup.stderr" 2>&1 &
    spid=$!
    sleep "${PERF_SETUP_SECONDS:-10}"
    kill -TERM "$spid" 2>/dev/null || true
    wait "$spid" 2>/dev/null || true
    rm -rf "$XDG_STATE_HOME"/emetos/logs
  fi
  profiling=0
  if [[ -n "${PERF_PROFILE:-}" && "$(basename "$bin")" == emetos ]]; then
    profiling=1
    export EMETOS_PROFILE_DIR="$PERF_PROFILE/$name"
  fi
  "$bin" "${args[@]}" >"$out/$name.stderr" 2>&1 &
  pid=$!
  trap 'kill "$pid" ${opid:-} 2>/dev/null || true; wait 2>/dev/null || true' EXIT
  for _ in $(seq 1 600); do
    # Glutter's counter has no log file of ours: its lines are on its stderr. (Not `| grep -q`: with
    # pipefail, a grep that stops reading at the first match fails the pipeline when cat is still
    # writing, and the loop then goes on for seconds.)
    if [[ "$(logs; cat "$out/$name.stderr")" == *"first frame presented"* ]]; then break; fi
    kill -0 "$pid" 2>/dev/null || { cat "$out/$name.stderr" >&2; echo "$name stopped before its first frame" >&2; exit 1; }
    sleep 0.1
  done
  sleep "$settle"
  if [[ "$other" != "-" ]]; then
    "$other" >"$out/$name.other.stderr" 2>&1 &
    opid=$!
    sleep 2 # the other window takes the focus
  fi
  lmark="$(logs | wc -l)"
  smark="$(wc -l <"$out/$name.stderr")"
  rss_kb="$(awk '/^VmRSS:/ { print $2 }' "/proc/$pid/status")"
  anon_kb="$(awk '/^Anonymous:/ { print $2 }' "/proc/$pid/smaps_rollup")"
  [[ $profiling -eq 1 ]] && kill -USR2 "$pid"
  t0="$(cpu_ticks "$pid")"
  a0="$(app_ticks "$pid")"
  sleep "$measure"
  t1="$(cpu_ticks "$pid")"
  a1="$(app_ticks "$pid")"
  if [[ $profiling -eq 1 ]]; then
    kill -USR2 "$pid"
    sleep 1
    kill -USR1 "$pid" # the heap profile (after a garbage collection)
    sleep 2
  fi
  hwm_kb="$(awk '/^VmHWM:/ { print $2 }' "/proc/$pid/status")"
  { logs; cat "$out/$name.stderr"; } >"$out/$name.log"
  # the log lines of the measuring time only
  { logs | tail -n +$((lmark + 1)); tail -n +$((smark + 1)) "$out/$name.stderr"; } >"$out/$name.during"
  gosys_mb="$({ logs; cat "$out/$name.stderr"; } | sed -n 's/.* go_sys_mb=\([0-9]*\).*/\1/p' | tail -1)"
  echo "rss_kb=$rss_kb hwm_kb=$hwm_kb anon_kb=$anon_kb gosys_mb=${gosys_mb:-0} ticks=$((t1 - t0)) app_ticks=$((a1 - a0)) seconds=$measure" >"$out/$name.sample"
  exit 0
fi

# ---------------------------------------------------------------------------------------------
# main: build, generate, and re-run inside the compositor.
# ---------------------------------------------------------------------------------------------
if [[ $mode == main ]]; then
  work="$(mktemp -d "${TMPDIR:-/tmp}/emetos-perf.XXXXXX")"
  trap 'rm -rf "$work"' EXIT
  mkdir -p "$work/bin" "$work/data" "$work/out" "$work/mesa"
  echo "building (-tags glutter_release,emetos_dev)..." >&2
  go build -tags glutter_release,emetos_dev -o "$work/bin/emetos" ./cmd/emetos
  go build -tags glutter_release -o "$work/bin/counter" github.com/rm4n0s/glutter/examples/counter

  # Go-looking code: indented statements of about 50 columns.
  gen() { # gen <lines> <file>
    awk -v n="$1" 'BEGIN { for (i = 0; i < n; i++) { m = i % 4
      if (m == 0) printf "func handler%d(ctx context.Context, request *Request) error {\n", i
      else if (m == 1) printf "\tresult, err := service.Process(ctx, request.Items[%d])\n", i % 64
      else if (m == 2) print "\tif err != nil { return fmt.Errorf(\"processing: %w\", err) }"
      else print "}" } }' >"$2"
  }
  gen 10000 "$work/data/go10k.go"
  gen 100000 "$work/data/lines100k.go"
  gen 200000 "$work/data/big.go"
  while [[ $(stat -c %s "$work/data/big.go") -lt 10485760 ]]; do gen 10000 "$work/data/more.go"; cat "$work/data/more.go" >>"$work/data/big.go"; done
  rm -f "$work/data/more.go"
  if [[ $huge -eq 1 ]]; then
    # more than 200 MiB (4.6 million lines of about 46 bytes): the large file of SPEC §14
    gen 4600000 "$work/data/huge.go"
  fi
  # four folders of 30 files of 300 lines each, for the session that is restored: four tabs, three
  # files open in each
  for w in 1 2 3 4; do
    mkdir -p "$work/data/ws$w/pkg"
    for f in $(seq 1 30); do
      awk -v n=300 -v f="$f" 'BEGIN { print "package pkg\n"; for (i = 0; i < n / 4; i++) {
        printf "func handler%d_%d(ctx context.Context, request *Request) error {\n", f, i
        printf "\tresult, err := service.Process(ctx, request.Items[%d])\n", i % 64
        print "\tif err != nil { return fmt.Errorf(\"processing: %w\", err) }"
        print "\treturn nil\n}" } }' >"$work/data/ws$w/pkg/file$f.go"
    done
  done

  # The Go benchmarks: the UI thread alone, the fuzzy file search, the grammars. They need no display.
  : >"$work/gobench.txt"
  if [[ $go_bench -eq 1 ]]; then
    echo "running the Go benchmarks..." >&2
    gobench() { GOMEMLIMIT=3GiB go test "$@" -run '^$' -benchmem 2>&1 | grep '^Benchmark' >>"$work/gobench.txt" || true; }
    gobench -tags glutter_release ./internal/gui/editor -bench 'TypeCharacter10kLines$|ScrollPage100kLines$|Open10MB$' -benchtime 30x
    # every operation tokenizes lines nobody has: a long -benchtime grows the tokens kept
    gobench -tags glutter_release ./internal/gui/editor -bench 'TypeCharacter10kLinesHighlighted$|ScrollPage100kLinesHighlighted$' -benchtime 50x
    # the Markdown preview of the 700 KB document with 150 images (step 17b.2)
    gobench -tags glutter_release ./extension/ui/markdown -bench 'Preview' -benchtime 50x
    gobench ./internal/core/fuzzy -bench 'Search100k$' -benchtime 20x
    gobench ./internal/core/workspace -bench 'QuickOpen100k$' -benchtime 3x
    gobench ./internal/core/language -bench 'Bundled$|FirstLine' -benchtime 10x
  fi

  [[ -n "$profile" ]] && cp "$work/bin/emetos" "$profile/emetos"
  export PERF_WORK="$work" PERF_RUNS="$runs" PERF_SECONDS="$seconds" PERF_SESSION="$session" PERF_HUGE="$huge" PERF_GO="$go_bench" PERF_PROFILE="$profile"
  if [[ $session -eq 1 ]]; then
    exec "$0" --inner
  fi
  # The headless compositor presents no GPU buffers, and Glutter's `auto` tries Vulkan first and gives it up after about
  # 600 ms (Glutter SPEC §9.6: the owner accepted that cost); a headless run asks for GLES, for the editor and for the
  # counter. The environment's GLUTTER_RENDERER wins (GLUTTER_RENDERER=auto measures the old start). With --session
  # the user's GPU decides and nothing is set.
  export GLUTTER_RENDERER="${GLUTTER_RENDERER:-gles}"
  export HEADLESS_SIZE="${HEADLESS_SIZE:-1920x1080}" HEADLESS_SCALE="${HEADLESS_SCALE:-1.25}" HEADLESS_FLOATING=0
  # The trap of this shell removes the work folder when the compositor run ends.
  scripts/headless-wayland.sh -- "$0" --inner
  exit $?
fi

# ---------------------------------------------------------------------------------------------
# inner: the measurements, in the compositor (or the session).
# ---------------------------------------------------------------------------------------------
work="$PERF_WORK" runs="$PERF_RUNS" seconds="$PERF_SECONDS" session="$PERF_SESSION"
out="$work/out"
export MESA_SHADER_CACHE_MAX_SIZE=1G
caches="$work/caches"

# measure runs one isolated measured run; the same arguments as --one.
measure() {
  scripts/isolated.sh -- "$root/scripts/perf.sh" --one "$out" "$@"
}

# field extracts key=value from a log line.
field() { sed -n "s/.* $2=\\([^ ]*\\).*/\\1/p" <<<"$1" | head -1; }

# duration_ms converts a Go duration such as 1.5s, 120ms or 85.2µs to milliseconds.
duration_ms() {
  awk -v d="$1" 'BEGIN {
    if (d ~ /µs$/) { sub(/µs$/, "", d); v = d / 1000 }
    else if (d ~ /ms$/) { sub(/ms$/, "", d); v = d + 0 }
    else if (d ~ /s$/) { sub(/s$/, "", d); v = d * 1000 }
    else v = d
    printf "%.2f", v }'
}

median() { printf '%s\n' "$@" | sort -n | awk '{ v[NR] = $1 } END { print v[int((NR + 1) / 2)] }'; }

first_frame_ms() { duration_ms "$(field "$(grep -h "first frame presented" "$out/$1.log" | head -1)" since_process_start)"; }

# pre_window_ms is what the process did before Glutter began to run the app, from the start of the
# process (the runtime, the package initializers, the process and its workspaces: the tabs restored,
# the files given) in milliseconds: the time of the first frame from the start of the process less
# the time from the start of Run.
pre_window_ms() {
  local line
  line="$(grep -h "first frame presented" "$out/$1.log" | head -1)"
  awk -v a="$(duration_ms "$(field "$line" since_process_start)")" -v b="$(duration_ms "$(field "$line" since_run)")" 'BEGIN { printf "%.0f", a - b }'
}

# first_build_ms is the build time (build, layout and paint of the framework thread, not the raster)
# of the first frame, which Glutter logs as a slow frame when it takes over 16 ms with the raster; "-" when
# the first frame is not logged.
first_build_ms() {
  local line
  line="$(grep -hE '( msg="slow frame"| INFO slow frame )' "$out/$1.log" | sed -n 1p)"
  if [[ -z "$line" ]]; then echo "-"; else duration_ms "$(field "$line" build)"; fi
}

# fmt0 rounds a number of milliseconds to a whole number.
fmt0() { awk -v v="$1" 'BEGIN { if (v ~ /^[0-9.]+$/) printf "%.0f", v; else printf "%s", v }'; }

# bench_range prints the shortest and the longest ns/op, in milliseconds, of the benchmarks of the
# Go benchmark output that match a regular expression ("- -" when none ran).
bench_range() {
  awk -v pat="$1" '$1 ~ pat { for (i = 2; i <= NF; i++) if ($i == "ns/op") { v = $(i - 1) / 1e6; if (!n || v < min) min = v; if (v > max) max = v; n++ } }
    END { if (n) printf "%.2f %.2f", min, max; else printf "- -" }' "$work/gobench.txt"
}
# bench_metric prints a custom metric (grammar-ms, line-ms) of the benchmark that matches.
bench_metric() {
  awk -v pat="$1" -v unit="$2" '$1 ~ pat { for (i = 2; i <= NF; i++) if ($i == unit) { printf "%.1f", $(i - 1); found = 1 } } END { if (!found) printf "-" }' "$work/gobench.txt"
}
# bench_cell prints "min to max" (or one number) of a benchmark pattern, for a cell of a table.
bench_cell() {
  local lo hi
  read -r lo hi <<<"$(bench_range "$1")"
  if [[ "$lo" == "-" ]]; then echo "not run"; elif [[ "$lo" == "$hi" ]]; then echo "$lo ms"; else echo "$lo to $hi ms"; fi
}

# frames_summary reads the "frames" lines of the measuring time of a run (the log lines after the
# settling: the start, whose first frame builds the whole window, is not in them) and prints: frames,
# the average build, the worst build, the average raster, the worst raster and the number of frames
# over 16 ms.
frames_summary() {
  { grep -hE "( msg=frames | INFO frames )" "$out/$1.during" || true; } | awk '
    function ms(d) { if (d ~ /µs$/) { sub(/µs$/, "", d); return d / 1000 } if (d ~ /ms$/) { sub(/ms$/, "", d); return d + 0 } sub(/s$/, "", d); return d * 1000 }
    { for (i = 1; i <= NF; i++) { split($i, kv, "="); v[kv[1]] = kv[2] }
      f = v["fps"]; frames += f; b += ms(v["build_avg"]) * f; r += ms(v["raster_avg"]) * f; slow += v["over_16ms"]
      if (ms(v["build_max"]) > bm) bm = ms(v["build_max"])
      if (ms(v["raster_max"]) > rm) rm = ms(v["raster_max"]) }
    END { if (frames) printf "%d %.3f %.3f %.2f %.2f %d", frames, b / frames, bm, r / frames, rm, slow; else printf "0 0 0 0 0 0" }'
}

# stage_ms is the sum of the "took" of the log lines of a startup stage of a measured run, in
# milliseconds (0 when there is none; several renderers can be tried and fail).
stage_ms() { # stage_ms <name> <stage>
  local total=0 line
  while IFS= read -r line; do
    [[ -n "$line" ]] && total="$(awk -v a="$total" -v b="$(duration_ms "$(field "$line" took)")" 'BEGIN { printf "%.1f", a + b }')"
  done < <(grep -h -e "stage=$2 " -e "stage=\"$2\" " "$out/$1.log" 2>/dev/null | sort -u)
  echo "$total"
}
# shader_counts says how many programs the last start built and took from the cache.
shader_counts() {
  local line
  line="$(grep -h "stage=shaders " "$out/$1.log" 2>/dev/null | head -1)"
  echo "$(field "$line" built) built, $(field "$line" from_cache) from the cache"
}

# cold_start <mode> measures the editor with no folder and no file, and the counter as the baseline,
# $runs times, and sets cold_ms and base_ms to the medians. The mode says what the caches hold:
# "empty" has a new cache of Glutter's and of the driver for every start, "driver" the driver's
# warm and Glutter's new for every start, "warm" both warm (a first start of each app fills them
# before anything is measured). The log of the last editor start is kept as part-<mode>.log.
cold_start() {
  local mode="$1" i app glutter mesa
  cold=() base=() pre=() bld=()
  glutter="$caches/glutter-$mode"
  mesa="$caches/mesa-$mode"
  if [[ $mode != empty ]]; then
    for app in emetos counter; do
      PERF_CACHE="$glutter" MESA_SHADER_CACHE_DIR="$mesa" measure "prime-$mode-$app" "$work/bin/$app" - - 1 0 - >/dev/null
    done
  fi
  for i in $(seq 1 "$runs"); do
    case $mode in
      empty) glutter="$caches/glutter-empty-$i"; mesa="$caches/mesa-empty-$i" ;;
      driver) glutter="$caches/glutter-driver-$i" ;;
    esac
    PERF_CACHE="$glutter" MESA_SHADER_CACHE_DIR="$mesa" measure "cold-$mode-$i" "$work/bin/emetos" - - 0 0 -
    cold+=("$(first_frame_ms "cold-$mode-$i")")
    pre+=("$(pre_window_ms "cold-$mode-$i")")
    bld+=("$(first_build_ms "cold-$mode-$i")")
    PERF_CACHE="$glutter" MESA_SHADER_CACHE_DIR="$mesa" measure "base-$mode-$i" "$work/bin/counter" - - 0 0 -
    base+=("$(first_frame_ms "base-$mode-$i")")
  done
  cold_ms="$(median "${cold[@]}")"
  base_ms="$(median "${base[@]}")"
  pre_ms="$(median "${pre[@]}")"
  bld_ms="$(median "${bld[@]}")"
  cp "$out/cold-$mode-$runs.log" "$out/part-$mode.log"
}
for mode in empty driver warm; do
  cold_start "$mode"
  eval "cold_$mode=$cold_ms base_$mode=$base_ms pre_$mode=$pre_ms bld_$mode=$bld_ms"
done
# the driver's cache for everything that follows: the one the warm runs filled
export MESA_SHADER_CACHE_DIR="$caches/mesa-warm" PERF_CACHE="$caches/glutter-warm"

# Idle: the editor on a 10,000-line file, focused (the cursor blinks) and with another window in
# front. The memory is read at the same time.
measure idle "$work/bin/emetos" "$work/data/go10k.go" - 5 "$seconds" -
# The window loses the focus when another one takes it: the counter is started next to it, the
# compositor activates the newest window, and the editor's view gets the unfocused event.
measure unfocused "$work/bin/emetos" "$work/data/go10k.go" - 5 "$seconds" "$work/bin/counter"
measure idlebase "$work/bin/counter" - - 5 "$seconds" -
cpu() { # cpu <name> <key>: percent of one core, from the ticks (1/100 s) of the sample
  local t
  t="$(sed -n "s/.*$2=\([0-9]*\).*/\1/p" "$out/$1.sample")"
  awk -v t="$t" -v s="$seconds" 'BEGIN { printf "%.2f", t / s }'
}
rss() { sed -n "s/.*$2=\\([0-9]*\\).*/\\1/p" "$out/$1.sample"; }
frames_during() { # frames_during <name>: frames drawn in the measuring time
  { grep -E "( msg=frames | INFO frames )" "$out/$1.during" || true; } | awk '{ for (i = 1; i <= NF; i++) { split($i, kv, "="); if (kv[1] == "fps") f += kv[2] } } END { print f + 0 }'
}
idle_cpu="$(cpu idle ticks)"; idle_app="$(cpu idle app_ticks)"; idle_frames="$(frames_during idle)"
unf="$(frames_during unfocused) frames, $(cpu unfocused ticks)% ($(cpu unfocused app_ticks)% without the renderer threads) of a core"
base_cpu="$(cpu idlebase ticks)"
idle_rss="$(rss idle rss_kb)"; idle_hwm="$(rss idle hwm_kb)"; base_rss="$(rss idlebase rss_kb)"
idle_anon="$(rss idle anon_kb)"; base_anon="$(rss idlebase anon_kb)"; idle_gosys="$(rss idle gosys_mb)"; base_gosys="$(rss idlebase gosys_mb)"

# frame_runs <name> <file> <dev commands> <settle> <measure> runs the editor $frame_repeats times with
# the dev commands and prints the frame summary of the run with the median average build time (one
# run is at the mercy of whatever else the machine does).
frame_repeats=3
frame_runs() {
  local name="$1" i sums=()
  for i in $(seq 1 "$frame_repeats"); do
    measure "$name-$i" "$work/bin/emetos" "$2" "$3" "$4" "$5" - >/dev/null
    sums+=("$(frames_summary "$name-$i")")
  done
  printf '%s\n' "${sums[@]}" | sort -k2 -n | awk '{ v[NR] = $0 } END { print v[int((NR + 1) / 2)] }'
}

# Typing: 120 characters, one per frame, in the 10,000-line file. The commands run 3 s after the first
# frame is drawn (headless, the first frame is presented about 0.9 s after it is drawn) and the measuring
# time begins 2 s after it is presented, so that the first frame, which builds the whole window, is not in
# the frames that count (Glutter logs its statistics in windows of a second).
text="$(printf 'x%.0s' $(seq 1 120))"
read -r t_frames t_build t_build_max t_raster t_raster_max t_slow <<<"$(frame_runs typing "$work/data/go10k.go" "[[\"sleep\",\"3s\"],[\"dev.type\",\"$text\"]]" 2 8)"

# Scrolling: 200 frames of 30 rows in the 100,000-line file.
read -r s_frames s_build s_build_max s_raster s_raster_max s_slow <<<"$(frame_runs scrolling "$work/data/lines100k.go" '[["sleep","3s"],["dev.scroll",{"rows":30,"frames":200}]]' 2 10)"

# Opening the 10 MB file.
measure big "$work/bin/emetos" "$work/data/big.go" - 1 1 -
big_ms="$(first_frame_ms big)"
big_pre="$(pre_window_ms big)"
big_build="$(first_build_ms big)"

# Opening the 200 MB file (large file mode); the memory is read when the window has settled.
if [[ "$PERF_HUGE" -eq 1 ]]; then
  measure huge "$work/bin/emetos" "$work/data/huge.go" - 3 1 -
  huge_ms="$(first_frame_ms huge)"
  huge_pre="$(pre_window_ms huge)"
  huge_build="$(first_build_ms huge)"
  huge_rss="$(rss huge rss_kb)"
  huge_mb="$(awk -v b="$(stat -c %s "$work/data/huge.go")" 'BEGIN { printf "%.0f", b / 1048576 }')"
fi

# The session restored: a first run opens four folders as tabs with three files in each and is ended;
# the measured run is the next start, with nothing on the command line. Taken $restore_runs times.
restore_runs=$(( runs < 3 ? runs : 3 ))
setup='['
for w in 1 2 3 4; do
  setup+="[\"dev.openFolderInNewTab\",{\"path\":\"$work/data/ws$w\"}],[\"sleep\",\"300ms\"],"
  for f in 1 2 3; do setup+="[\"dev.openFile\",{\"path\":\"$work/data/ws$w/pkg/file$f.go\"}],[\"sleep\",\"200ms\"],"; done
done
setup+='["sleep","1s"]]'
rst=() rpre=() rbld=() rrss=()
for i in $(seq 1 "$restore_runs"); do
  PERF_SETUP="$setup" measure "restore-$i" "$work/bin/emetos" - - 3 1 -
  rst+=("$(first_frame_ms "restore-$i")")
  rpre+=("$(pre_window_ms "restore-$i")")
  rbld+=("$(first_build_ms "restore-$i")")
  rrss+=("$(( $(rss "restore-$i" rss_kb) / 1024 ))")
done
restore_ms="$(median "${rst[@]}")"; restore_pre="$(median "${rpre[@]}")"; restore_build="$(median "${rbld[@]}")"; restore_rss="$(median "${rrss[@]}")"

[[ -n "${PERF_KEEP:-}" ]] && cp -r "$out/." "$PERF_KEEP/"
renderer="$(grep -h "first frame presented" "$out/idle.log" | head -1 | sed -n 's/.* renderer="\([^"]*\)".*/\1/p')"
extra="$(awk -v a="$big_ms" -v b="$cold_warm" 'BEGIN { printf "%.0f", a - b }')"
part() { # part <mode> <stage>: a part of the first draw of the last start of a mode, in milliseconds
  cp "$out/part-$1.log" "$out/cold-part.log"
  case "$2" in
    tried) stage_ms cold-part "renderer tried" ;;
    context) stage_ms cold-part "GPU context or device" ;;
    shaders) stage_ms cold-part shaders ;;
    frame) stage_ms cold-part "frame without the above" ;;
    draw) stage_ms cold-part "first draw (renderer, GL context, shaders, first frame)" ;;
  esac
}
shaders_of() { cp "$out/part-$1.log" "$out/cold-part.log"; shader_counts cold-part; }
since_run() { duration_ms "$(field "$(grep -h "first frame presented" "$out/part-$1.log" | head -1)" since_run)"; }
huge_row=""
if [[ "$PERF_HUGE" -eq 1 ]]; then
  huge_extra="$(awk -v a="$huge_ms" -v b="$cold_warm" 'BEGIN { printf "%.0f", a - b }')"
  huge_row="| Opening a ${huge_mb} MB file in large file mode: first frame | < 3 s | ${huge_ms} ms from the start of the process (${huge_extra} ms more than the empty window; $(fmt0 "$huge_pre") ms of it before the window is made, reading the file; build of the first frame ${huge_build} ms); resident memory $((huge_rss / 1024)) MB |
"
fi
bench_rows=""
if [[ "$PERF_GO" -eq 1 ]]; then
  bench_rows="| UI thread alone (Go benchmarks, the fake platform): typing a character in a 10,000-line file | < 4 ms | $(bench_cell '^BenchmarkTypeCharacter10kLines-'); highlighted: $(bench_cell '^BenchmarkTypeCharacter10kLinesHighlighted-') |
| UI thread alone: scrolling a page of a 100,000-line file | none over 8 ms | $(bench_cell '^BenchmarkScrollPage100kLines-'); highlighted, lines nobody tokenized: $(bench_cell '^BenchmarkScrollPage100kLinesHighlighted-') |
| UI thread alone: opening a 10 MB file (read, buffer, first layout) | < 300 ms | $(bench_cell '^BenchmarkOpen10MB-') |
| UI thread alone: the Markdown preview of a 700 KB document with 150 images (step 17b.2) | no frame over 16 ms | first frame $(bench_metric '^BenchmarkPreviewFirstFrame-' first-frame-ms) ms, then parse and first blocks $(bench_metric '^BenchmarkPreviewFirstFrame-' parse-and-blocks-ms) ms; a jump to a place never shown $(bench_cell '^BenchmarkPreviewJump-'); a step of the wheel $(bench_cell '^BenchmarkPreviewWheelStep-'); the parse, off the UI thread, $(bench_cell '^BenchmarkPreviewParse-') |
| Fuzzy file search over 100,000 paths, per keystroke, off the UI thread | < 30 ms | the scorer alone and sorted: $(bench_cell '^BenchmarkSearch100k/'); the quick open end to end on one goroutine: $(bench_cell '^BenchmarkQuickOpen100k/one_goroutine/'), on all of them: $(bench_cell '^BenchmarkQuickOpen100k/parallel/') |
| Making the registry of the languages (at the start, before the window) | part of the cold start | $(bench_cell '^BenchmarkBundled-') |
| The first file of a language: the grammar (UI thread) and the first line (compiles the patterns) | part of the open of a file | Go $(bench_metric '^BenchmarkFirstLine/go-' grammar-ms) + $(bench_metric '^BenchmarkFirstLine/go-' line-ms) ms, Python $(bench_metric '^BenchmarkFirstLine/python-' grammar-ms) + $(bench_metric '^BenchmarkFirstLine/python-' line-ms) ms, TypeScript $(bench_metric '^BenchmarkFirstLine/typescript-' grammar-ms) + $(bench_metric '^BenchmarkFirstLine/typescript-' line-ms) ms, HTML $(bench_metric '^BenchmarkFirstLine/html-' grammar-ms) + $(bench_metric '^BenchmarkFirstLine/html-' line-ms) ms, Markdown $(bench_metric '^BenchmarkFirstLine/markdown-' grammar-ms) + $(bench_metric '^BenchmarkFirstLine/markdown-' line-ms) ms |
"
fi
cat <<TABLE
Renderer: ${renderer} ($([[ $session -eq 1 ]] && echo "the session of the caller" || echo "headless: software rendering, no GPU"))

| Metric (SPEC §14) | Target | Measured |
|---|---|---|
| Typing in a 10,000-line file: build time of the frames (average, worst of $t_frames frames) | < 4 ms | ${t_build} ms, ${t_build_max} ms (raster ${t_raster} ms average, ${t_raster_max} ms worst; ${t_slow} frames over 16 ms) |
| Scrolling a 100,000-line file: frames | the refresh rate, none over 8 ms | ${s_frames} frames; build ${s_build} ms average, ${s_build_max} ms worst; raster ${s_raster} ms average, ${s_raster_max} ms worst; ${s_slow} frames over 16 ms |
| Opening a 10 MB file: first frame | < 300 ms | ${big_ms} ms from the start of the process (${extra} ms more than the empty window; $(fmt0 "$big_pre") ms of it before the window is made); build of the first frame ${big_build} ms |
${huge_row}| Cold start, no folder, to the first frame (median of $runs, from the start of the process), both shader caches warm | < 400 ms | ${cold_warm} ms (Glutter's counter, same run: ${base_warm} ms); Emetos before the window is made: ${pre_warm} ms; build of its first frame: ${bld_warm} ms |
| Cold start, the driver's cache warm and Glutter's empty | < 400 ms | ${cold_driver} ms (the counter: ${base_driver} ms); before the window: ${pre_driver} ms; first build ${bld_driver} ms |
| Cold start, both caches empty (the first start on a machine) | < 400 ms | ${cold_empty} ms (the counter: ${base_empty} ms); before the window: ${pre_empty} ms; first build ${bld_empty} ms |
| Cold start with the workbench and the session restored (four tabs, three files in each; median of $restore_runs), both caches warm | < 400 ms | ${restore_ms} ms; Emetos before the window is made: ${restore_pre} ms; build of its first frame: ${restore_build} ms; resident memory ${restore_rss} MB |
| Idle, focused, cursor blinking ($seconds s) | < 1% of a core | ${idle_cpu}% of a core with the renderer threads, ${idle_app}% without them (${idle_frames} frames; the counter: ${base_cpu}%) |
| Idle, without the focus ($seconds s, another window has it) | no frames at all | ${unf} |
| Resident memory, one 10,000-line file | the counter + 50 MB | $((idle_rss / 1024)) MB (peak $((idle_hwm / 1024)) MB; $((idle_anon / 1024)) MB anonymous, the Go runtime has $idle_gosys MB of it); Glutter's counter: $((base_rss / 1024)) MB ($((base_anon / 1024)) MB anonymous, Go $base_gosys MB) |
${bench_rows}
TABLE

cat <<PARTS

| Part of the first draw of the last start (ms) | Both caches empty | The driver's warm | Both warm |
|---|---|---|---|
| Renderers tried that failed (Vulkan on a compositor that presents no GPU buffers) | $(part empty tried) | $(part driver tried) | $(part warm tried) |
| GPU context or device | $(part empty context) | $(part driver context) | $(part warm context) |
| Shaders ($(shaders_of empty); $(shaders_of driver); $(shaders_of warm)) | $(part empty shaders) | $(part driver shaders) | $(part warm shaders) |
| The frame itself | $(part empty frame) | $(part driver frame) | $(part warm frame) |
| First draw, all of it | $(part empty draw) | $(part driver draw) | $(part warm draw) |
| From the start of Run to the first frame | $(since_run empty) | $(since_run driver) | $(since_run warm) |
PARTS
