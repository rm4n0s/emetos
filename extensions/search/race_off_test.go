//go:build !race

package search

// raceEnabled is true when the test binary runs under the race detector, which slows the work several times.
const raceEnabled = false
