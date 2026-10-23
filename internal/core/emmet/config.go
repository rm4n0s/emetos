package emmet

import "github.com/rm4n0s/errors"

// Ported from Emmet: src/config.ts (the markup part; step 5.11 adds the stylesheet options)

// StringCase is how a name is written: upper case, lower case, or as it is ("").
type StringCase string

// The cases of names.
const (
	CaseKeep  StringCase = ""
	CaseLower StringCase = "lower"
	CaseUpper StringCase = "upper"
)

// QuoteStyle is the quote that surrounds the value of an attribute.
type QuoteStyle string

// The quotes of attribute values.
const (
	QuoteDouble QuoteStyle = "double"
	QuoteSingle QuoteStyle = "single"
)

// SelfClosingStyle is how an element with no content is closed.
type SelfClosingStyle string

// The self-closing styles.
const (
	// SelfClosingHTML is `<br>`.
	SelfClosingHTML SelfClosingStyle = "html"
	// SelfClosingXML is `<br/>`.
	SelfClosingXML SelfClosingStyle = "xml"
	// SelfClosingXHTML is `<br />`.
	SelfClosingXHTML SelfClosingStyle = "xhtml"
)

// FieldFunc makes the text of a field (a tab stop) in the output, from its index and placeholder,
// the byte offset in the output so far and the line and column (in bytes) there.
type FieldFunc func(index int, placeholder string, offset, line, column int) string

// TextFunc processes a piece of text before it goes into the output (an editor may escape
// characters there). It gets the same position as FieldFunc.
type TextFunc func(text string, offset, line, column int) string

// Options are the options of the output, named as Emmet's ("output.indent" is OutputIndent...).
type Options struct {
	// InlineElements are the names of the inline-level elements.
	InlineElements []string

	// Indent is the string of one level of indentation.
	Indent string
	// BaseIndent is added to the start of every line the output has but the first (the
	// indentation of the line the abbreviation is on).
	BaseIndent string
	// Newline is the string of a line break.
	Newline string
	// TagCase is the case of the names of the elements.
	TagCase StringCase
	// AttributeCase is the case of the names of the attributes.
	AttributeCase StringCase
	// AttributeQuotes is the quote of the attribute values.
	AttributeQuotes QuoteStyle
	// Format turns on the indentation and the line breaks.
	Format bool
	// FormatLeafNode puts the inside of an element with no content on a line of its own.
	FormatLeafNode bool
	// FormatSkip are the names of the elements whose children are not indented.
	FormatSkip []string
	// FormatForce are the names of the elements that always have their inside on lines of its own.
	FormatForce []string
	// InlineBreak is the number of inline siblings in a row that makes each of them go on a
	// line of its own: 0 never breaks them, 1 breaks them as block elements.
	InlineBreak int
	// CompactBoolean writes `<div hidden>` and not `<div hidden="hidden">`.
	CompactBoolean bool
	// BooleanAttributes are the names of the boolean attributes.
	BooleanAttributes []string
	// ReverseAttributes puts the attributes of the abbreviation before those of the snippet it
	// uses, and keeps the values of the snippet.
	ReverseAttributes bool
	// SelfClosingStyle is the style of elements with no closing tag.
	SelfClosingStyle SelfClosingStyle
	// Field makes the fields of the output; the default writes the placeholder.
	Field FieldFunc
	// Text processes the text of the output; the default leaves it as it is.
	Text TextFunc

	// MarkupHref fills the href of a link that wraps a URL or an e-mail address.
	MarkupHref bool
	// MarkupAttributes renames attributes in the output ("class" -> "className"); a key that ends with
	// `*` is for the attributes written more than once ('..foo').
	MarkupAttributes map[string]string
	// MarkupValuePrefix are prefixes of attribute values, which then are written in object
	// notation (`styles.foo`); the keys are as in MarkupAttributes.
	MarkupValuePrefix map[string]string

	// CommentEnabled puts comments before and after the elements with the attributes of
	// CommentTrigger.
	CommentEnabled bool
	// CommentTrigger are the names of the attributes that trigger a comment.
	CommentTrigger []string
	// CommentBefore is the template of the comment before the opening tag.
	CommentBefore string
	// CommentAfter is the template of the comment after the closing tag, like `\n<!-- /[#ID][.CLASS] -->`.
	CommentAfter string

	// BEMEnabled turns on the BEM notation of the class names.
	BEMEnabled bool
	// BEMElement is the string between a block and its element.
	BEMElement string
	// BEMModifier is the string before a modifier.
	BEMModifier string

	// JSXEnabled turns on the JSX mode (components, expressions as attribute values).
	JSXEnabled bool
}

// Context is the element an abbreviation is expanded inside of.
type Context struct {
	// Name is the name of the element.
	Name string
	// Attributes are its attributes, by name (only the class is read, for BEM).
	Attributes map[string]string
}

// Config is everything an expansion depends on. NewConfig makes the configuration of a syntax;
// the fields can be changed afterwards.
type Config struct {
	// Syntax of the document: html, xml, xsl, jsx, pug, slim, haml, vue, svelte or another (which is
	// output as html).
	Syntax string
	// Variables are the values of the ${name} variables.
	Variables map[string]string
	// Snippets are the abbreviations that a name stands for.
	Snippets map[string]string
	// Options of the output.
	Options Options
	// Context is the element the abbreviation is in, if known.
	Context *Context
	// Text is the text that the abbreviation wraps; nil is none.
	Text *Text
	// MaxRepeat limits the elements an abbreviation makes (ParserOptions.MaxRepeat); zero is DefaultMaxRepeat.
	MaxRepeat int
	// Rand gives random numbers in [0, 1) for the Lorem ipsum generator; nil uses math/rand/v2.
	Rand func() float64
	// Warn, if set, is told of what is left out without failing (a snippet that does not parse).
	Warn func(message string, err error)
}

// DefaultOptions gives the default options of Emmet.
func DefaultOptions() Options {
	return Options{
		InlineElements: []string{
			"a", "abbr", "acronym", "applet", "b", "basefont", "bdo",
			"big", "br", "button", "cite", "code", "del", "dfn", "em", "font", "i",
			"iframe", "img", "input", "ins", "kbd", "label", "map", "object", "q",
			"s", "samp", "select", "small", "span", "strike", "strong", "sub", "sup",
			"textarea", "tt", "u", "var",
		},
		Indent:          "\t",
		Newline:         "\n",
		AttributeQuotes: QuoteDouble,
		Format:          true,
		FormatSkip:      []string{"html"},
		FormatForce:     []string{"body"},
		InlineBreak:     3,
		BooleanAttributes: []string{
			"contenteditable", "seamless", "async", "autofocus",
			"autoplay", "checked", "controls", "defer", "disabled", "formnovalidate",
			"hidden", "ismap", "loop", "multiple", "muted", "novalidate", "readonly",
			"required", "reversed", "selected", "typemustmatch",
		},
		SelfClosingStyle: SelfClosingHTML,
		Field:            func(_ int, placeholder string, _, _, _ int) string { return placeholder },
		Text:             func(text string, _, _, _ int) string { return text },
		MarkupHref:       true,
		CommentTrigger:   []string{"id", "class"},
		CommentAfter:     "\n<!-- /[#ID][.CLASS] -->",
		BEMElement:       "__",
		BEMModifier:      "_",
	}
}

// SnippetField writes a field as TextMate and VS Code do: `${1}` or `${1:placeholder}`.
func SnippetField(index int, placeholder string, _, _, _ int) string {
	if placeholder != "" {
		return "${" + itoa(index) + ":" + placeholder + "}"
	}
	return "${" + itoa(index) + "}"
}

// NewConfig makes the configuration of a syntax: the default options, variables and snippets of
// Emmet, and what the syntax changes (the snippets of XSL and Pug, the self-closing style of XML,
// the class names of JSX and Vue).
func NewConfig(syntax string) (Config, error) {
	if syntax == "" {
		syntax = "html"
	}
	variables, err := readStringMap("assets/variables.json")
	if err != nil {
		return Config{}, err
	}
	raw, err := readStringMap("assets/html.json")
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		Syntax:    syntax,
		Variables: variables,
		Snippets:  splitSnippetKeys(raw),
		Options:   DefaultOptions(),
	}

	switch syntax {
	case "xhtml":
		cfg.Options.SelfClosingStyle = SelfClosingXHTML
	case "xml":
		cfg.Options.SelfClosingStyle = SelfClosingXML
	case "xsl":
		cfg.Options.SelfClosingStyle = SelfClosingXML
		extra, err := readStringMap("assets/xsl.json")
		if err != nil {
			return Config{}, err
		}
		for k, v := range splitSnippetKeys(extra) {
			cfg.Snippets[k] = v
		}
	case "jsx":
		cfg.Options.JSXEnabled = true
		cfg.Options.MarkupAttributes = map[string]string{"class": "className", "class*": "styleName", "for": "htmlFor"}
		cfg.Options.MarkupValuePrefix = map[string]string{"class*": "styles"}
	case "vue":
		cfg.Options.MarkupAttributes = map[string]string{"class*": ":class"}
	case "svelte":
		cfg.Options.JSXEnabled = true
	case "pug":
		extra, err := readStringMap("assets/pug.json")
		if err != nil {
			return Config{}, err
		}
		for k, v := range splitSnippetKeys(extra) {
			cfg.Snippets[k] = v
		}
	}
	return cfg, nil
}

// SetOutput sets how the output is indented and where lines break: the string of one level of
// indentation, the line break, and the indentation added to every line but the first. The
// variables ${indentation} and ${newline} take the same values.
func (c *Config) SetOutput(indent, newline, baseIndent string) {
	c.Options.Indent = indent
	c.Options.Newline = newline
	c.Options.BaseIndent = baseIndent
	if c.Variables == nil {
		c.Variables = map[string]string{}
	}
	c.Variables["indentation"] = indent
	c.Variables["newline"] = newline
}

// SetVariables adds variables (the setting emmet.variables) over those of the configuration.
func (c *Config) SetVariables(vars map[string]string) {
	if c.Variables == nil {
		c.Variables = map[string]string{}
	}
	for k, v := range vars {
		c.Variables[k] = v
	}
}

// SetSnippets adds snippets over those of the configuration. Unlike Emmet's own, the names are
// not split at `|`.
func (c *Config) SetSnippets(snippets map[string]string) {
	if c.Snippets == nil {
		c.Snippets = map[string]string{}
	}
	for k, v := range snippets {
		c.Snippets[k] = v
	}
}

func (c *Config) warn(message string, err error) {
	if c.Warn != nil {
		c.Warn(message, err)
	}
}

// VariablesFromSettings reads the setting emmet.variables, an object of strings.
func VariablesFromSettings(value any) (map[string]string, error) {
	if value == nil {
		return nil, nil
	}
	obj, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("VariablesInvalid", "emmet.variables must be an object", "got", value)
	}
	result := make(map[string]string, len(obj))
	for k, v := range obj {
		s, ok := v.(string)
		if !ok {
			return nil, errors.New("VariableValueInvalid", "the values of emmet.variables must be strings", "name", k, "got", v)
		}
		result[k] = s
	}
	return result, nil
}
