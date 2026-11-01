//go:build !race

package scmgraph

// raceEnabled is true when the test binary runs under the race detector (see race_on_test.go).
const raceEnabled = false
