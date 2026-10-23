//go:build race

package language

import "time"

// raceEnabled is true when the tests run under the race detector, which makes the regular
// expressions about ten times slower: wall-clock assertions are skipped and the time limit of a
// search is longer.
const raceEnabled = true

// testScanTimeout is the time limit of a search in the tests.
const testScanTimeout = 30 * time.Second
