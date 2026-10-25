// Package language holds the language registry and language configurations (SPEC §8.1). The registry has plain text and
// nothing else of its own: the bundled languages are the language packs under extensions/ (SPEC §10.15), which the
// extension host adds and takes away.
//
// A Registry is filled from VS Code extensions: the languages and grammars that the package.json
// of an extension contributes. It tells the language of a file (Detect, with the user's
// files.associations first), gives the language configuration of a language (Configuration) and
// the tokenizer of a language (NewGrammar), and it is the textmate.Registry that grammars find
// each other in. Extensions are added with AddExtension (for good) and AddNamedExtension (RemoveExtension takes them
// away again); Version counts the changes.
package language
