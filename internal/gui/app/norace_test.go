//go:build !race

package app

// raceEnabled is true when the tests run under the race detector (see race_test.go).
const raceEnabled = false
