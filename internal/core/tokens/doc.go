// Package tokens keeps the tokenization state of documents and runs the background tokenizer (SPEC §8.3).
//
// A Tokenizer holds, for every line of a document, its tokens and the grammar state at its end. Edits invalidate
// from the first edited line and re-tokenization stops at the first line whose end state is unchanged. The visible
// lines are tokenized synchronously within a budget (Ensure); the rest in slices by Run, on snapshots of the document,
// dropping a result when the document has changed since.
package tokens
