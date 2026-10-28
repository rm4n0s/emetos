//go:build race

package emulator

// raceEnabled is true when the tests run under the race detector, which makes the emulator several times slower: the
// throughput budgets are not checked then.
const raceEnabled = true
