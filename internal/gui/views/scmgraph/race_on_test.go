//go:build race

package scmgraph

// raceEnabled is true when the test binary runs under the race detector, which slows a frame about ten times.
const raceEnabled = true
