// Package markdown renders Markdown for the extensions of Emetos (SPEC §10.15 "As built (step 11.6)"): a parser that
// turns a text into a tree of blocks (headings, paragraphs, lists, block quotes, code blocks, tables, rules) and inlines
// (emphasis, code, links, images), and a widget that draws the tree with the theme of the window. The Markdown preview
// of extensions/markdown and the other extensions that show Markdown use it, because extensions never import
// each other.
//
// The parser has no dependencies; the widget imports the kit through the same rule as package ui.
package markdown
