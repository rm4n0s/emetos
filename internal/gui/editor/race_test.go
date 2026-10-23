//go:build race

package editor

// raceEnabled is true when the tests run under the race detector, which makes the regular
// expressions of the tokenizer about ten times slower: a test that counts on the budget of the
// text layer to tokenize some lines does not.
const raceEnabled = true
