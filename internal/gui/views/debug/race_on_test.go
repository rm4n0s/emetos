//go:build race

package debug

// raceEnabled is true when the test binary runs under the race detector, which slows a frame about ten times.
const raceEnabled = true
