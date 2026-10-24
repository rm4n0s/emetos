// Package fuzzy matches and scores strings fuzzily (SPEC §10.6).
//
// It is a port of VS Code's fuzzy scorer (src/vs/base/common/fuzzyScorer.ts) and of the parts of
// src/vs/base/common/filters.ts that the scorer uses. The scores are the numbers of the
// reference, so the ordering of results is VS Code's.
//
// # Positions
//
// VS Code works with UTF-16 code units. This package works with runes: every position and
// every Match is an offset in runes of the string (not bytes, not UTF-16 units), which is what
// a label widget needs to draw highlights. For text in the Basic Multilingual Plane the numbers
// are the same as VS Code's. Strings are lower-cased rune by rune with unicode.ToLower, so a
// string and its lower-case form always have the same length.
//
// # Concurrency
//
// A Scorer owns scratch buffers (the tables of the dynamic programs and rune slices) and is
// not safe for concurrent use: every goroutine holds its own. A Cache is not safe for
// concurrent use either. Results hold no reference to the scratch buffers and can be kept.
package fuzzy
