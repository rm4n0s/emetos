// Package testpeek is the peek of a failed test (SPEC §10.32 "The editor", step 26.3): a zone under the failing line of an editor with the
// message, the expected and actual text as a small diff editor, the stack trace, the output, and next and previous failure. It knows
// nothing of the test model: the window gives it the failures of the file as Subjects and makes the small diff editor.
package testpeek
