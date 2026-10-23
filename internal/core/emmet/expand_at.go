package emmet

import (
	"regexp"
	"slices"
	"strings"
)

// Ported from VS Code: extensions/emmet/src/abbreviationActions.ts (expandEmmetAbbreviation and
// expandAbbr, the part that does not need the document or the editor).
//
// The checks of validity (IsAbbreviationValid) and the filters (|c, |bem, |t) are what VS Code
// takes from its helper library, @vscode/emmet-helper, whose source is not in reference/; they are
// written here from its documented behavior (SPEC section 9.10.2).

// ExpandRequest is what ExpandAt needs to expand the abbreviation before a cursor, or in a
// selection. Everything but the text comes from the settings and the editor.
type ExpandRequest struct {
	// Syntax of Emmet to expand in: the result of SyntaxFor.
	Syntax string
	// TextBefore is the text of the line from its start up to the cursor.
	TextBefore string
	// Selection, if not empty, is the selected text: the abbreviation is the text itself, and
	// TextBefore is not read.
	Selection string

	// Variables are the setting emmet.variables (see VariablesFromSettings).
	Variables map[string]string
	// Profiles are the setting emmet.syntaxProfiles (markup only).
	Profiles map[string]any
	// Preferences are the setting emmet.preferences (the stylesheet ones are read, see
	// Config.ApplyPreferences). Nothing is asked for them in Emetos by default.
	Preferences map[string]any

	// Indent is the string of one level of indentation of the output (default a tab, which a
	// snippet insertion turns into the indentation of the document).
	Indent string
	// Newline is the line break of the output (default "\n").
	Newline string
	// BaseIndent is added to every line of the output but the first. Leave it empty when the text
	// is inserted as a snippet: the insertion indents its lines by itself.
	BaseIndent string
}

// ExpandResult is the expansion of an abbreviation.
type ExpandResult struct {
	// Start and End are the byte offsets of the text to replace in TextBefore (in Selection when
	// it is given).
	Start, End int
	// Abbreviation is the abbreviation that was expanded, without its filters.
	Abbreviation string
	// Text is the expansion, written as a snippet: the tab stops are `${1:name}`, `${1}` and
	// `${0}`, and a `\` and a `$` of the text have a backslash before them.
	Text string
	// Syntax is the syntax that was used.
	Syntax string
}

var htmlOpenTagRe = regexp.MustCompile(`<(\w+)$`)

// ExpandAt expands the abbreviation before the cursor, or the selected text. It gives false (and no
// error) when there is nothing to expand: the text before the cursor ends with white space or is
// empty, no abbreviation is found, or what is found is not valid for the syntax (so that the
// caller can do what the key would do otherwise). It gives an error when the abbreviation is
// valid but cannot be expanded (the tags of the errors of the parser and of the configuration).
//
// For html, text like `<div` before the cursor is expanded as `div`, taking the `<`. A stylesheet
// abbreviation that ends with `:` is not expanded.
//
// As the cursor is assumed to be where an abbreviation may be (in markup for the syntaxes of
// markup, in a rule for stylesheets), nothing of the document is read (SPEC section 9.10.2).
func ExpandAt(req ExpandRequest) (ExpandResult, bool, error) {
	if req.Syntax == "" {
		return ExpandResult{}, false, nil
	}
	stylesheet := IsStylesheetSyntax(req.Syntax)

	var abbr, filter string
	start, end := 0, 0
	if req.Selection != "" {
		a, f, ok := extractFromText(req.Selection, stylesheet)
		if !ok {
			return ExpandResult{}, false, nil
		}
		abbr, filter, start, end = a, f, 0, len(req.Selection)
	} else {
		before := req.TextBefore
		if before == "" || before[len(before)-1] == ' ' || before[len(before)-1] == '\t' {
			// A space before the cursor: the key is a key for a space, not for an abbreviation
			return ExpandResult{}, false, nil
		}
		end = len(before)
		if m := htmlOpenTagRe.FindStringSubmatchIndex(before); req.Syntax == "html" && m != nil {
			// <div is expanded as div, not as <<div></div>
			abbr, start = before[m[2]:m[3]], m[0]
		} else {
			pos, f := getFilters(before, len(before))
			typ := TypeMarkup
			if stylesheet {
				typ = TypeStylesheet
			}
			res, ok := ExtractAbbreviation(before, pos, ExtractOptions{Type: typ})
			if !ok {
				return ExpandResult{}, false, nil
			}
			abbr, filter, start = res.Abbreviation, f, res.Location
		}
	}

	if !IsAbbreviationValid(req.Syntax, abbr) {
		return ExpandResult{}, false, nil
	}
	if stylesheet && strings.HasSuffix(abbr, ":") {
		// An abbreviation that ends with a colon is a selector like `a:`, or a property
		// being typed (vscode issue 1623)
		return ExpandResult{}, false, nil
	}

	cfg, err := req.config(filter)
	if err != nil {
		return ExpandResult{}, false, err
	}
	text, err := Expand(abbr, cfg)
	if err != nil {
		return ExpandResult{}, false, err
	}
	return ExpandResult{Start: start, End: end, Abbreviation: abbr, Text: text, Syntax: req.Syntax}, true, nil
}

// config makes the configuration of the request: the one of the syntax, the settings, the output
// for a snippet, and the filters.
func (req ExpandRequest) config(filter string) (Config, error) {
	cfg, err := NewConfig(req.Syntax)
	if err != nil {
		return Config{}, err
	}
	indent, newline := req.Indent, req.Newline
	if indent == "" {
		indent = "\t"
	}
	if newline == "" {
		newline = "\n"
	}
	cfg.SetOutput(indent, newline, req.BaseIndent)
	cfg.SetVariables(req.Variables)
	if cfg.Type == TypeStylesheet {
		cfg.Options.StylesheetFuzzySearchMinScore = DefaultFuzzySearchMinScore
	} else if err := cfg.ApplyProfiles(req.Profiles); err != nil {
		return Config{}, err
	}
	if err := cfg.ApplyPreferences(req.Preferences); err != nil {
		return Config{}, err
	}
	for _, f := range strings.Split(filter, ",") {
		switch f {
		case "bem":
			cfg.Options.BEMEnabled = true
		case "c":
			cfg.Options.CommentEnabled = true
		}
	}
	cfg.Options.Field = SnippetField
	cfg.Options.Text = SnippetText
	return cfg, nil
}

// extractFromText finds the abbreviation in a text that is all of it (a selection): it is the text
// with its filters, as the helper's extractAbbreviationFromText.
func extractFromText(text string, stylesheet bool) (abbr, filter string, ok bool) {
	pos, filter := getFilters(text, len(text))
	typ := TypeMarkup
	if stylesheet {
		typ = TypeStylesheet
	}
	res, found := ExtractAbbreviation(text, pos, ExtractOptions{Type: typ})
	if !found || res.Location != 0 {
		return "", "", false
	}
	return res.Abbreviation, filter, true
}

// maxFilters is how many filters (|bem, |c, |t) follow an abbreviation.
const maxFilters = 3

// getFilters finds the filters at the end of the text (up to pos): `ul>li|c|bem`. It gives the
// place where the abbreviation ends, and the names of the filters separated by commas, in their order.
func getFilters(text string, pos int) (int, string) {
	filter := ""
	for range maxFilters {
		matched := ""
		for _, name := range []string{"bem", "c", "t"} {
			if strings.HasSuffix(text[:pos], "|"+name) {
				matched = name
				break
			}
		}
		if matched == "" {
			break
		}
		pos -= len(matched) + 1
		if filter == "" {
			filter = matched
		} else {
			filter = matched + "," + filter
		}
	}
	return pos, filter
}

// The words of HTML that stand for a tag: an abbreviation of a stylesheet that starts with one of
// them before a # is a selector (`div#main`), not a color.
var commonlyUsedTags = []string{
	"a", "abbr", "address", "area", "article", "aside", "audio", "b", "base", "bdi", "bdo", "blockquote", "body", "br",
	"button", "canvas", "caption", "cite", "code", "col", "colgroup", "data", "datalist", "dd", "del", "details", "dfn",
	"dialog", "div", "dl", "dt", "em", "embed", "fieldset", "figcaption", "figure", "footer", "form", "h1", "h2", "h3",
	"h4", "h5", "h6", "head", "header", "hgroup", "hr", "html", "i", "iframe", "img", "input", "ins", "kbd", "label",
	"legend", "li", "link", "main", "map", "mark", "menu", "meta", "meter", "nav", "noscript", "object", "ol", "optgroup",
	"option", "output", "p", "param", "picture", "pre", "progress", "q", "rp", "rt", "ruby", "s", "samp", "script",
	"section", "select", "slot", "small", "source", "span", "strong", "style", "sub", "summary", "sup", "table", "tbody",
	"td", "template", "textarea", "tfoot", "th", "thead", "time", "title", "tr", "track", "u", "ul", "var", "video",
	"wbr", "lorem",
}

var (
	hexColorRe           = regexp.MustCompile(`^#[\da-fA-F]{1,6}$`)
	cssAbbreviationRe    = regexp.MustCompile(`^-?[a-zA-Z!@#]`)
	htmlAbbrStartRe      = regexp.MustCompile(`^[a-zA-Z!(\[#.{]`)
	htmlAbbrEndRe        = regexp.MustCompile(`[a-zA-Z\d)\]}#.*$!/]$`)
	numbersAndDotsOnlyRe = regexp.MustCompile(`^[\d.]+$`)
)

// IsAbbreviationValid tells if the text may be an abbreviation of the syntax (an editor must not
// try to expand every word before the cursor):
//   - a stylesheet abbreviation starts with a letter, `-`, `!`, `@` or `#`; with a `#` it is a hex
//     color (`#fc0`) or a property with a color (`c#fc0`), not a selector of a tag (`div#main`)
//   - a markup abbreviation starts with a letter, `!`, `(`, `[`, `#`, `.` or `{`, and ends with a
//     letter, a digit or one of `) ] } # . * $ ! /`; one that starts with `!` is only `!`s
//   - a markup abbreviation of digits and dots only (`.5`, `1.5`) is a number, not a class
func IsAbbreviationValid(syntax, abbreviation string) bool {
	if abbreviation == "" {
		return false
	}
	if IsStylesheetSyntax(syntax) {
		if strings.Contains(abbreviation, "#") {
			if strings.HasPrefix(abbreviation, "#") {
				return hexColorRe.MatchString(abbreviation)
			}
			if slices.Contains(commonlyUsedTags, abbreviation[:strings.Index(abbreviation, "#")]) {
				return false
			}
		}
		return cssAbbreviationRe.MatchString(abbreviation)
	}

	if strings.HasPrefix(abbreviation, "!") {
		return strings.Trim(abbreviation, "!") == ""
	}
	if numbersAndDotsOnlyRe.MatchString(abbreviation) && strings.ContainsAny(abbreviation, "0123456789") {
		return false
	}
	return htmlAbbrStartRe.MatchString(abbreviation) && htmlAbbrEndRe.MatchString(abbreviation)
}
