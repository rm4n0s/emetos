// Package language holds the language registry, language configurations and the bundled grammar assets (SPEC §8.1).
//
// A Registry is filled from VS Code extensions: the languages and grammars that the package.json
// of an extension contributes. It tells the language of a file (Detect, with the user's
// files.associations first), gives the language configuration of a language (Configuration) and
// the tokenizer of a language (NewGrammar), and it is the textmate.Registry that grammars find
// each other in.
package language
