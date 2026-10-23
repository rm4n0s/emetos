//go:build !race

package language

import "time"

// raceEnabled is true when the tests run under the race detector (see race_test.go).
const raceEnabled = false

// testScanTimeout is the time limit of a search in the tests: the default of the scanner.
const testScanTimeout time.Duration = 0
