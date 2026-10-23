// Package brackets finds bracket pairs, matches them and numbers their nesting for colorization (SPEC §8.5).
//
// An Index reads the brackets of each line of a document from its tokens (a bracket in a comment, a
// string or a regular expression is text), pairs them as VS Code's bracket pair parser does, and answers
// the questions of the editor: the pairs and brackets in a range with their nesting levels, the bracket
// that matches the one at a position, the pair around a position, and the closing brackets that close
// nothing. Languages come from their configurations ("brackets" and "colorizedBracketPairs").
//
// Edits and new tokens invalidate the lines they touch; the pairs are made again, lazily, when the next
// question comes.
package brackets
