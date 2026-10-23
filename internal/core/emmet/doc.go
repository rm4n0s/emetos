// Package emmet expands Emmet abbreviations (SPEC §9.10).
//
// The package is a port of Emmet (https://github.com/emmetio/emmet, MIT): the abbreviation
// tokenizer and parser (Tokenize, ParseTokens), the conversion of the parsed abbreviation into
// a tree of nodes (Convert, ParseAbbreviation), the markup transformations (implicit tags,
// snippets, attributes, Lorem ipsum, BEM, labels, XSL) and the formatters for HTML, XML, XSL,
// JSX and the indent based HAML, Pug and Slim (Expand, Parse, Stringify). It is plain Go with
// no dependency on the UI. Offsets are byte offsets: every character with a meaning in the
// abbreviation language is ASCII, and the other bytes are ordinary text.
//
// The stylesheet (CSS) abbreviations are a port of Emmet's css-abbreviation package and of its
// stylesheet module (TokenizeCSS, ParseCSS, ParseStylesheet, StringifyStylesheet,
// ExpandStylesheet): property names are matched fuzzily against css.json. ExtractAbbreviation finds
// the abbreviation before the cursor, and ExpandAt, SyntaxFor and the validity checks are the
// part of the Emmet extension of VS Code that does not depend on the editor (SPEC section 9.10.2).
package emmet
