// Package regex compiles Oniguruma-style regular expressions for grammars, and ECMAScript-mode
// expressions for find (SPEC §8.2).
//
// regexp2 reads the syntax of .NET, which has lookbehind and backreferences as Oniguruma does but
// differs in many other places. Compile parses an Oniguruma pattern into a tree and writes it
// out in regexp2's syntax, so that a rule of a grammar means what it means in vscode-textmate.
// Searches work on runes; the scanner of step 4.2 maps the offsets to bytes.
package regex
