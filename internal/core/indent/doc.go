// Package indent holds indentation detection, indentation rules and on-enter rules (SPEC §8.6).
// Guess finds how a text is indented: with tabs or with spaces, and how many spaces make an
// indent. An Indenter applies the indentation rules and the on-enter rules of languages, with the
// tokens of the text: the indentation of a new line (EnterAction, IndentForEnter), of a line that a
// typed character makes match decreaseIndentPattern (IndentActionForType), of lines that are
// reindented (ReindentEdits) or pasted (PasteEdits). Its functions follow VS Code's autoIndent.ts
// and enterAction.ts, and keep their one-based line numbers inside; the public ones take
// zero-based lines and byte columns.
package indent
