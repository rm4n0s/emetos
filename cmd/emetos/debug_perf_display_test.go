//go:build emetos_display

package main

import (
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/testenv/display"
	"github.com/rm4n0s/emetos/internal/testenv/realinput"
)

// The time of a step (step 25.12, SPEC §14, docs/PERFORMANCE.md "Run and Debug"): a Go program with a loop is debugged with the pinned dlv, and
// F10 is pressed 100 times with real input. The time of a step is taken from the moment the key is sent to the first poll of the window in
// which the line of the top stack frame (the badge with the line of the Call Stack, which the model sets in the same pass as the stopped line of the
// editor) is not the one of before; the poll asks the window every few milliseconds what texts it draws. dlv's own time is part of it.
//
//	go build -tags emetos_dev -o steprelease.out ./cmd/emetos
//	EMETOS_DISPLAY_BINARY=$PWD/steprelease.out EMETOS_REALINPUT_RACE=0 scripts/heavy.sh --alone -- scripts/realinput.sh -- -tags emetos_display ./cmd/emetos -run TestPerfDebugStepping -v -count=1

const steppingProgram = `package main

func main() {
	total := 0
	for i := 0; i < 1000; i++ {
		total += i * 2
		total %= 7919
	}
	println(total)
}
`

// frameBadge matches the position of a stack frame in the Call Stack.
var frameBadge = regexp.MustCompile(`^\d+(:\d+)?$`)

// topFrame is the position the Call Stack shows for the first frame of the stopped thread, or "".
func topFrame(a *display.App) string {
	best, bestY := "", 1e9
	texts := a.Texts()
	// the Breakpoints view is under the Call Stack and has the line of its breakpoint in the same kind of badge: while the thread runs
	// the stack is empty, and that badge would be taken for the top frame (it made presses look ignored, step 25.12's finding)
	limit := 1e9
	for _, tx := range texts {
		if strings.EqualFold(tx.Text, "Breakpoints") && tx.X < 420 {
			limit = tx.Y
		}
	}
	for _, tx := range texts {
		if frameBadge.MatchString(tx.Text) && tx.X > 150 && tx.X < 420 && tx.Y > 430 && tx.Y < limit && tx.Y < bestY {
			best, bestY = tx.Text, tx.Y
		}
	}
	return best
}

func TestPerfDebugStepping(t *testing.T) {
	opts := goDebugProject(t)
	opts.Project["main.go"] = steppingProgram
	opts.KeyDelay = realinput.NoKeyDelay
	opts.Env = append(opts.Env, "GLUTTER_FRAME_STATS=1")
	a := display.Start(t, opts)
	a.Settle()

	a.Click(editorColumnX(8), editorLine(6))
	a.Press("f9")
	a.Press("f5")
	var at string
	a.EventuallyFor(90*time.Second, "the stop at the breakpoint", func() (bool, string) {
		at = topFrame(a)
		return at != "", a.Dump()
	})
	time.Sleep(time.Second) // the rest of the stack and the variables are loaded after the stop

	// what the harness itself costs: a key that does nothing (F7), and a question to the window
	const probes = 20
	pressStart := time.Now()
	for range probes {
		a.Press("f7")
	}
	press := time.Since(pressStart) / probes
	pollStart := time.Now()
	for range probes {
		topFrame(a)
	}
	poll := time.Since(pollStart) / probes

	const steps = 100
	var times []time.Duration
	var trace []string
	retries := 0
	byMove := map[string][]time.Duration{}
	for i := range steps {
		before := at
		var took time.Duration
		for attempt := 0; ; attempt++ {
			start := time.Now()
			a.Press("f10")
			deadline := start.Add(3 * time.Second)
			for time.Now().Before(deadline) {
				at = topFrame(a)
				if at != "" && at != before {
					took = time.Since(start)
					break
				}
				time.Sleep(2 * time.Millisecond)
			}
			if took > 0 {
				break
			}
			// a key that finds the session not stopped yet does nothing: it is pressed again, and the step counts from that press
			retries++
			if attempt == 4 {
				t.Fatalf("step %d: the top frame stays at %q after 5 presses\n%s", i+1, before, a.Dump())
			}
		}
		times = append(times, took)
		byMove[before+">"+at] = append(byMove[before+">"+at], took)
		trace = append(trace, at)
		time.Sleep(60 * time.Millisecond) // the views finish loading before the next key
	}
	sorted := slices.Clone(times)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	var sum time.Duration
	for _, d := range times {
		sum += d
	}
	pct := func(p float64) time.Duration { return sorted[min(len(sorted)-1, int(p*float64(len(sorted))))] }
	t.Logf("%d steps of F10 in a loop with dlv: min %v, median %v, average %v, p90 %v, p99 %v, max %v", steps, sorted[0], pct(0.5), sum/steps, pct(0.9), pct(0.99), sorted[len(sorted)-1])
	t.Logf("the harness itself: a key press of the virtual keyboard takes %v and a question to the window (what texts it draws) %v; both are inside each figure above", press, poll)
	t.Logf("the median of each move (line>line): %s", medians(byMove))
	t.Logf("%d presses of F10 found the session not stopped yet and were repeated", retries)
	t.Logf("the first lines it went through: %s", strings.Join(trace[:12], " "))
	for _, s := range a.SlowFrames() {
		t.Logf("a slow frame: %+v", s)
	}
	if w := a.FrameWindows(); len(w) > 0 {
		t.Logf("frame statistics windows: %d (last: %+v)", len(w), w[len(w)-1])
	}
}

// medians are the median times of each move from one line to another, in the order of the lines.
func medians(byMove map[string][]time.Duration) string {
	var keys []string
	for k := range byMove {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []string
	for _, k := range keys {
		list := slices.Clone(byMove[k])
		sort.Slice(list, func(i, j int) bool { return list[i] < list[j] })
		out = append(out, k+": "+list[len(list)/2].Round(time.Millisecond).String()+" ("+strconv.Itoa(len(list))+")")
	}
	return strings.Join(out, ", ")
}
