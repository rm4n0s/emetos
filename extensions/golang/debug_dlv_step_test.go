//go:build emetos_dlv

package golang

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension"
)

// The time of dlv's own step (step 25.12, docs/PERFORMANCE.md "Run and Debug"): the same loop as the display measurement of the whole
// editor (cmd/emetos TestPerfDebugStepping), stepped 100 times over the DAP connection with no editor in between. The first figure is the time from
// the request `next` to the `stopped` event, the second adds what a client asks after a stop (the stack, the scopes and the variables).
//
//	go test -tags emetos_dlv ./extensions/golang -run TestDlvStepTime -v -count=1
func TestDlvStepTime(t *testing.T) {
	r := dlvRig(t)
	prog := t.TempDir()
	const source = `package main

func main() {
	total := 0
	for i := 0; i < 1000; i++ {
		total += i * 2
		total %= 7919
	}
	println(total)
}
`
	for name, text := range map[string]string{"main.go": source, "go.mod": "module example.com/loop\n\ngo 1.21\n"} {
		if err := os.WriteFile(filepath.Join(prog, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	d, c := r.session(extension.DebugConfiguration{"name": "Launch", "type": "go", "request": "launch", "mode": "debug", "program": prog})
	if msg := d.start("launch", c, filepath.Join(prog, "main.go"), 6); msg != "" {
		t.Fatalf("launch failed: %s", msg)
	}
	_, thread := d.stopped()
	var step, whole []time.Duration
	for range 100 {
		start := time.Now()
		d.call("next", map[string]any{"threadId": thread})
		if reason, _ := d.stopped(); reason != "step" {
			t.Fatalf("stopped for %q", reason)
		}
		step = append(step, time.Since(start))
		_, _, frame := d.top(thread)
		d.locals(frame)
		whole = append(whole, time.Since(start))
	}
	report := func(name string, list []time.Duration) {
		s := slices.Clone(list)
		sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
		var sum time.Duration
		for _, v := range s {
			sum += v
		}
		t.Logf("%s: min %v, median %v, average %v, p90 %v, max %v", name, s[0], s[len(s)/2], sum/time.Duration(len(s)), s[len(s)*9/10], s[len(s)-1])
	}
	report("next to stopped", step)
	report("next to the variables of the new line", whole)
	d.finish(true)
}
