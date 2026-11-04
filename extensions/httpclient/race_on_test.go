//go:build race

package httpclient

// raceEnabled is true when the test binary runs under the race detector, which slows the code about ten times.
const raceEnabled = true
