#!/usr/bin/env bash
# Measures the performance targets of SPEC §14 that need the running app: typing and
# scrolling frames, opening a 10 MB file, the cold start, the idle CPU (focused and not) and the
# resident memory. The benchmarks of the UI thread alone are Go benchmarks (docs/PERFORMANCE.md).
#
# Usage: scripts/perf.sh [--session] [-n runs] [-s seconds]
#
#   --session    run in the Wayland session of the caller, with its GPU, instead of a headless
#                compositor with a software renderer. The numbers of the targets are those of
#                this mode (SPEC §14); the headless ones are recorded but do not decide
#   -n runs      starts of the editor to take the median cold start of (default 5)
#   -s seconds   how long the idle CPU is measured (default 10)
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
# Needs sway for the headless mode (scripts/headless-wayland.sh). The output is a Markdown table.
set -euo pipefail

cd "$(dirname "$0")/.."
root="$(pwd)"

mode=main
session=0
runs=5
seconds=10
while [[ $# -gt 0 ]]; do
  case "$1" in
    --session) session=1; shift ;;
    -n) runs="$2"; shift 2 ;;
    -s) seconds="$2"; shift 2 ;;
    --one) mode=one; shift; break ;;
    --inner) mode=inner; shift ;;
    *) echo "usage: $0 [--session] [-n runs] [-s seconds]" >&2; exit 64 ;;
  esac
done

# ---------------------------------------------------------------------------------------------
# --one: one measured run, inside scripts/isolated.sh. The arguments:
#   <out dir> <name> <binary> <file or -> <dev commands or -> <settle s> <measure s> <other binary or ->
# It starts the app, waits for its first frame and the settling, optionally starts another window
# to take the focus away, samples the process for the measuring time, and leaves the log of
# the app, and the samples, in the out dir.
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

  "$bin" "${args[@]}" >"$out/$name.stderr" 2>&1 &
  pid=$!
  trap 'kill "$pid" ${opid:-} 2>/dev/null || true; wait 2>/dev/null || true' EXIT
  for _ in $(seq 1 600); do
    # Glutter's counter has no log file of ours: its lines are on its stderr.
    if { logs; cat "$out/$name.stderr"; } | grep -q "first frame presented"; then break; fi
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
  t0="$(cpu_ticks "$pid")"
  a0="$(app_ticks "$pid")"
  sleep "$measure"
  t1="$(cpu_ticks "$pid")"
  a1="$(app_ticks "$pid")"
  hwm_kb="$(awk '/^VmHWM:/ { print $2 }' "/proc/$pid/status")"
  { logs; cat "$out/$name.stderr"; } >"$out/$name.log"
  # the log lines of the measuring time only
  { logs | tail -n +$((lmark + 1)); tail -n +$((smark + 1)) "$out/$name.stderr"; } >"$out/$name.during"
  echo "rss_kb=$rss_kb hwm_kb=$hwm_kb ticks=$((t1 - t0)) app_ticks=$((a1 - a0)) seconds=$measure" >"$out/$name.sample"
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

  export PERF_WORK="$work" PERF_RUNS="$runs" PERF_SECONDS="$seconds" PERF_SESSION="$session"
  if [[ $session -eq 1 ]]; then
    exec "$0" --inner
  fi
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

# frames_summary reads the "frames" lines of a log after the first line number given and
# prints: frames, the average build, the worst build, the average raster, the worst raster and
# the number of frames over 16 ms.
frames_summary() {
  { grep -hE "( msg=frames | INFO frames )" "$out/$1.log" || true; } | awk '
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
  cold=() base=()
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
    PERF_CACHE="$glutter" MESA_SHADER_CACHE_DIR="$mesa" measure "base-$mode-$i" "$work/bin/counter" - - 0 0 -
    base+=("$(first_frame_ms "base-$mode-$i")")
  done
  cold_ms="$(median "${cold[@]}")"
  base_ms="$(median "${base[@]}")"
  cp "$out/cold-$mode-$runs.log" "$out/part-$mode.log"
}
for mode in empty driver warm; do
  cold_start "$mode"
  eval "cold_$mode=$cold_ms base_$mode=$base_ms"
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

# Typing: 120 characters, one per frame, in the 10,000-line file.
text="$(printf 'x%.0s' $(seq 1 120))"
measure typing "$work/bin/emetos" "$work/data/go10k.go" "[[\"sleep\",\"500ms\"],[\"dev.type\",\"$text\"]]" 1 20 -
read -r t_frames t_build t_build_max t_raster t_raster_max t_slow <<<"$(frames_summary typing)"

# Scrolling: 200 frames of 30 rows in the 100,000-line file.
measure scrolling "$work/bin/emetos" "$work/data/lines100k.go" '[["sleep","500ms"],["dev.scroll",{"rows":30,"frames":200}]]' 1 20 -
read -r s_frames s_build s_build_max s_raster s_raster_max s_slow <<<"$(frames_summary scrolling)"

# Opening the 10 MB file.
measure big "$work/bin/emetos" "$work/data/big.go" - 1 1 -
big_ms="$(first_frame_ms big)"
big_build="$(duration_ms "$(field "$(grep -hE '( msg="slow frame"| INFO slow frame )' "$out/big.log" | head -1)" build)")"

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
cat <<TABLE
Renderer: ${renderer} ($([[ $session -eq 1 ]] && echo "the session of the caller" || echo "headless: software rendering, no GPU"))

| Metric (SPEC §14) | Target | Measured |
|---|---|---|
| Typing in a 10,000-line file: build time of the frames (average, worst of $t_frames frames) | < 4 ms | ${t_build} ms, ${t_build_max} ms (raster ${t_raster} ms average, ${t_raster_max} ms worst; ${t_slow} frames over 16 ms) |
| Scrolling a 100,000-line file: frames | the refresh rate, none over 8 ms | ${s_frames} frames; build ${s_build} ms average, ${s_build_max} ms worst; raster ${s_raster} ms average, ${s_raster_max} ms worst; ${s_slow} frames over 16 ms |
| Opening a 10 MB file: first frame | < 300 ms | ${big_ms} ms from the start of the process (${extra} ms more than the empty window); build of the first frame ${big_build} ms |
| Cold start, no folder, to the first frame (median of $runs, from the start of the process), both shader caches warm | < 400 ms | ${cold_warm} ms (Glutter's counter, same run: ${base_warm} ms) |
| Cold start, the driver's cache warm and Glutter's empty | < 400 ms | ${cold_driver} ms (the counter: ${base_driver} ms) |
| Cold start, both caches empty (the first start on a machine) | < 400 ms | ${cold_empty} ms (the counter: ${base_empty} ms) |
| Idle, focused, cursor blinking ($seconds s) | < 1% of a core | ${idle_cpu}% of a core with the renderer threads, ${idle_app}% without them (${idle_frames} frames; the counter: ${base_cpu}%) |
| Idle, without the focus ($seconds s, another window has it) | no frames at all | ${unf} |
| Resident memory, one 10,000-line file | < 200 MB (see the note) | $((idle_rss / 1024)) MB (peak $((idle_hwm / 1024)) MB); Glutter's counter: $((base_rss / 1024)) MB |
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
