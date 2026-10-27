//go:build race

package app

// raceEnabled is true when the tests run under the race detector, which makes Glutter's software renderer, a loop over the
// pixels of every picture, about a hundred times slower: a test that draws many pictures of 1280x800 is skipped.
const raceEnabled = true
