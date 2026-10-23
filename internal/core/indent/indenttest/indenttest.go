// Package indenttest holds what the tests of the indentation need: the language configurations of
// VS Code's tests of indentation (JavaScript, Ruby, PHP, Go, C++, HTML, Visual Basic, LaTeX and
// Lua) and a text with the tokens a test gives it. It is only for tests.
package indenttest

import (
	"strings"

	"github.com/rm4n0s/emetos/internal/core/brackets"
	"github.com/rm4n0s/emetos/internal/core/indent"
	"github.com/rm4n0s/emetos/internal/core/language"
	"github.com/rm4n0s/emetos/internal/core/regex"
	"github.com/rm4n0s/emetos/internal/core/textmate"
	"github.com/rm4n0s/emetos/internal/core/tokens"
)

// Tok is where a token of a line starts and its standard type, as VS Code's tests give them.
type Tok struct {
	Start int
	Type  textmate.TokenType
}

// Source gives the tokens of a document from a table; a line that is not in the table has none.
// It is an edit.TokenSource.
type Source struct {
	// Code is the code of the language of the document.
	Code int
	// Content is the text of a line, to find where its last token ends.
	Content func(line int) string
	// Lines holds the tokens of the lines (zero-based) that have some.
	Lines map[int][]Tok
	// Cheap, when it is not nil, says which lines (zero-based) are cheap to tokenize; all are
	// when it is nil.
	Cheap func(line int) bool
}

// LineTokens implements edit.TokenSource.
func (s Source) LineTokens(line int) []tokens.Token {
	spec := s.Lines[line]
	if len(spec) == 0 {
		return nil
	}
	size := len(s.Content(line))
	out := make([]tokens.Token, len(spec))
	for i, tk := range spec {
		end := size
		if i+1 < len(spec) {
			end = spec[i+1].Start
		}
		out[i] = tokens.Token{End: int32(min(end, size)), Language: uint16(s.Code), Type: tk.Type}
	}
	return out
}

// Language implements edit.TokenSource.
func (s Source) Language() int { return s.Code }

// CheapToTokenize implements edit.TokenSource.
func (s Source) CheapToTokenize(line int) bool { return s.Cheap == nil || s.Cheap(line) }

// Text is a document of fixed lines and tokens, an indent.Model.
type Text struct {
	Source
	Text []string
}

// NewText makes a Text of the lines with the tokens in the table.
func NewText(code int, lines []string, toks map[int][]Tok) *Text {
	t := &Text{Text: lines}
	t.Source = Source{Code: code, Content: func(l int) string { return t.Text[l] }, Lines: toks}
	return t
}

// LineCount implements indent.Model.
func (t *Text) LineCount() int { return len(t.Text) }

// LineContent implements indent.Model.
func (t *Text) LineContent(line int) string { return t.Text[line] }

// Languages is a table of languages by code. A code that is not in it has no rules.
type Languages map[int]*indent.Language

// Language implements indent.Languages.
func (l Languages) Language(code int) *indent.Language {
	if lang, ok := l[code]; ok {
		return lang
	}
	return &indent.Language{}
}

// The codes of the languages of the tests.
const (
	TypeScript = iota + 1
	Ruby
	PHP
	Go
	CPP
	HTML
	VB
	Latex
	Lua
)

// re compiles a regular expression of a language configuration; § stands for a backtick, which a
// Go raw string cannot hold.
func re(pattern string, flags ...string) *regex.Regexp {
	opts := regex.Options{Syntax: regex.ECMAScript}
	if len(flags) > 0 && strings.Contains(flags[0], "i") {
		opts.IgnoreCase = true
	}
	r, err := regex.Compile(strings.ReplaceAll(pattern, "§", "`"), opts)
	if err != nil {
		panic("indenttest: " + pattern + ": " + err.Error())
	}
	return r
}

func pairs(texts ...string) []language.Pair {
	out := make([]language.Pair, 0, len(texts)/2)
	for i := 0; i+1 < len(texts); i += 2 {
		out = append(out, language.Pair{Open: texts[i], Close: texts[i+1]})
	}
	return out
}

func make1(code int, c *language.Configuration) *indent.Language {
	return indent.NewLanguage(c, brackets.FromConfiguration(code, c))
}

var standardBrackets = pairs("{", "}", "[", "]", "(", ")")

// All is the table of the languages of the tests.
func All() Languages {
	return Languages{
		TypeScript: typeScript(), Ruby: ruby(), PHP: php(), Go: golang(), CPP: cpp(), HTML: html(),
		VB: vb(), Latex: latex(), Lua: lua(),
	}
}

func typeScript() *indent.Language {
	return make1(TypeScript, TypeScriptConfiguration())
}

// TypeScriptConfiguration is the language configuration of TypeScript in the tests.
func TypeScriptConfiguration() *language.Configuration {
	return &language.Configuration{
		Brackets: pairs("${", "}", "{", "}", "[", "]", "(", ")"),
		AutoClosingPairs: []language.AutoClosingPair{
			{Pair: language.Pair{Open: "{", Close: "}"}}, {Pair: language.Pair{Open: "[", Close: "]"}},
			{Pair: language.Pair{Open: "(", Close: ")"}},
			{Pair: language.Pair{Open: "'", Close: "'"}, NotIn: []string{"string", "comment"}},
			{Pair: language.Pair{Open: `"`, Close: `"`}, NotIn: []string{"string"}},
			{Pair: language.Pair{Open: "`", Close: "`"}, NotIn: []string{"string", "comment"}},
			{Pair: language.Pair{Open: "/**", Close: " */"}, NotIn: []string{"string"}},
		},
		IndentationRules: &language.IndentationRules{
			DecreaseIndentPattern: re(`^((?!.*?\/\*).*\*\/)?\s*[\}\]\)].*$`),
			IncreaseIndentPattern: re(`^((?!\/\/).)*(\{([^}"'§]*|(\t|[ ])*\/\/.*)|\([^)"'§]*|\[[^\]"'§]*)$`),
			UnIndentedLinePattern: re(`^(\t|[ ])*[ ]\*[^/]*\*\/\s*$|^(\t|[ ])*[ ]\*\/\s*$|^(\t|[ ])*[ ]\*([ ]([^\*]|\*(?!\/))*)?$`),
			IndentNextLinePattern: re(`^((.*=>\s*)|((.*[^\w]+|\s*)(if|while|for)\s*\(.*\)\s*))$`),
		},
		OnEnterRules: []language.OnEnterRule{
			{ // e.g. /** | */
				BeforeText: re(`^\s*\/\*\*(?!\/)([^\*]|\*(?!\/))*$`), AfterText: re(`^\s*\*\/$`),
				Action: language.EnterAction{Indent: language.IndentOutdent, AppendText: " * "},
			},
			{ // e.g. /** ...|
				BeforeText: re(`^\s*\/\*\*(?!\/)([^\*]|\*(?!\/))*$`),
				Action:     language.EnterAction{Indent: language.IndentNone, AppendText: " * "},
			},
			{ // e.g.  * ...|
				BeforeText:       re(`^(\t|[ ])*[ ]\*([ ]([^\*]|\*(?!\/))*)?$`),
				PreviousLineText: re(`(?=^(\s*(\/\*\*|\*)).*)(?=(?!(\s*\*\/)))`),
				Action:           language.EnterAction{Indent: language.IndentNone, AppendText: "* "},
			},
			{ // e.g.  */|
				BeforeText: re(`^(\t|[ ])*[ ]\*\/\s*$`),
				Action:     language.EnterAction{Indent: language.IndentNone, RemoveText: 1},
			},
			{ // e.g.  *-----*/|
				BeforeText: re(`^(\t|[ ])*[ ]\*[^/]*\*\/\s*$`),
				Action:     language.EnterAction{Indent: language.IndentNone, RemoveText: 1},
			},
			{
				BeforeText: re(`^\s*(\bcase\s.+:|\bdefault:)$`), AfterText: re(`^(?!\s*(\bcase\b|\bdefault\b))`),
				Action: language.EnterAction{Indent: language.IndentIndent},
			},
			{
				PreviousLineText: re(`^\s*(((else ?)?if|for|while)\s*\(.*\)\s*|else\s*)$`),
				BeforeText:       re(`^\s+([^{i\s]|i(?!f\b))`),
				Action:           language.EnterAction{Indent: language.IndentOutdentOne},
			},
			{ // Indent when pressing enter from inside ()
				BeforeText: re(`^.*\([^\)]*$`), AfterText: re(`^\s*\).*$`),
				Action: language.EnterAction{Indent: language.IndentOutdent, AppendText: "\t"},
			},
			{ // Indent when pressing enter from inside {}
				BeforeText: re(`^.*\{[^\}]*$`), AfterText: re(`^\s*\}.*$`),
				Action: language.EnterAction{Indent: language.IndentOutdent, AppendText: "\t"},
			},
			{ // Indent when pressing enter from inside []
				BeforeText: re(`^.*\[[^\]]*$`), AfterText: re(`^\s*\].*$`),
				Action: language.EnterAction{Indent: language.IndentOutdent, AppendText: "\t"},
			},
		},
	}
}

func ruby() *indent.Language {
	c := &language.Configuration{
		Brackets: standardBrackets,
		IndentationRules: &language.IndentationRules{
			DecreaseIndentPattern: re(`^\s*([}\]]([,)]?\s*(#|$)|\.[a-zA-Z_]\w*\b)|(end|rescue|ensure|else|elsif)\b|(in|when)\s)`),
			IncreaseIndentPattern: re(`^\s*((begin|class|(private|protected)\s+def|def|else|elsif|ensure|for|if|module|rescue|unless|until|when|in|while|case)|([^#]*\sdo\b)|([^#]*=\s*(case|if|unless)))\b([^#\{;]|("|'|\/).*\4)*(#.*)?$`),
		},
	}
	return make1(Ruby, c)
}

func php() *indent.Language {
	c := &language.Configuration{
		Brackets: standardBrackets,
		IndentationRules: &language.IndentationRules{
			IncreaseIndentPattern: re(`({(?!.*}).*|\(|\[|((else(\s)?)?if|else|for(each)?|while|switch|case).*:)\s*((\/[/*].*|)?$|\?>)`),
			DecreaseIndentPattern: re(`^(.*\*\/)?\s*((\})|(\)+[;,])|(\]\)*[;,])|\b(else:)|\b((end(if|for(each)?|while|switch));))`),
		},
		OnEnterRules: []language.OnEnterRule{
			{
				BeforeText: re(`^\s*\/\*\*(?!\/)([^\*]|\*(?!\/))*$`), AfterText: re(`^\s*\*\/$`),
				Action: language.EnterAction{Indent: language.IndentOutdent, AppendText: " * "},
			},
			{
				BeforeText: re(`^\s*\/\*\*(?!\/)([^\*]|\*(?!\/))*$`),
				Action:     language.EnterAction{Indent: language.IndentNone, AppendText: " * "},
			},
			{
				BeforeText: re(`^(\t|(\ \ ))*\ \*(\ ([^\*]|\*(?!\/))*)?$`),
				Action:     language.EnterAction{Indent: language.IndentNone, AppendText: "* "},
			},
			{
				BeforeText: re(`^(\t|(\ \ ))*\ \*\/\s*$`),
				Action:     language.EnterAction{Indent: language.IndentNone, RemoveText: 1},
			},
			{
				BeforeText: re(`^(\t|(\ \ ))*\ \*[^/]*\*\/\s*$`),
				Action:     language.EnterAction{Indent: language.IndentNone, RemoveText: 1},
			},
			{
				BeforeText:       re(`^\s+([^{i\s]|i(?!f\b))`),
				PreviousLineText: re(`^\s*(((else ?)?if|for(each)?|while)\s*\(.*\)\s*|else\s*)$`),
				Action:           language.EnterAction{Indent: language.IndentOutdentOne},
			},
		},
	}
	return make1(PHP, c)
}

func golang() *indent.Language {
	c := &language.Configuration{
		Brackets: standardBrackets,
		IndentationRules: &language.IndentationRules{
			DecreaseIndentPattern: re(`^\s*(\bcase\b.*:|\bdefault\b:|}[)}]*[),]?|\)[,]?)$`),
			IncreaseIndentPattern: re(`^.*(\bcase\b.*:|\bdefault\b:|(\b(func|if|else|switch|select|for|struct)\b.*)?{[^}"'§]*|\([^)"'§]*)$`),
		},
	}
	return make1(Go, c)
}

func cpp() *indent.Language {
	c := &language.Configuration{
		Brackets: standardBrackets,
		OnEnterRules: []language.OnEnterRule{{
			PreviousLineText: re(`^\s*(((else ?)?if|for|while)\s*\(.*\)\s*|else\s*)$`),
			BeforeText:       re(`^\s+([^{i\s]|i(?!f\b))`),
			Action:           language.EnterAction{Indent: language.IndentOutdentOne},
		}},
	}
	return make1(CPP, c)
}

func html() *indent.Language {
	c := &language.Configuration{
		Brackets: pairs("<!--", "-->", "{", "}", "(", ")"),
		IndentationRules: &language.IndentationRules{
			DecreaseIndentPattern: re(`^\s*(<\/(?!html)[-_\.A-Za-z0-9]+\b[^>]*>|-->|\})`),
			IncreaseIndentPattern: re(`<(?!\?|(?:area|base|br|col|frame|hr|html|img|input|keygen|link|menuitem|meta|param|source|track|wbr)\b|[^>]*\/>)([-_\.A-Za-z0-9]+)(?=\s|>)\b[^>]*>(?!.*<\/\1>)|<!--(?!.*-->)|\{[^}"']*$`),
		},
		OnEnterRules: []language.OnEnterRule{
			{
				BeforeText: re(`<(?!(?:area|base|br|col|embed|hr|img|input|keygen|link|menuitem|meta|param|source|track|wbr))([_:\w][_:\w\-.\d]*)(?:(?:[^'"/>]|"[^"]*"|'[^']*')*?(?!\/)>)[^<]*$`, "i"),
				AfterText:  re(`^<\/([_:\w][_:\w\-.\d]*)\s*>`, "i"),
				Action:     language.EnterAction{Indent: language.IndentOutdent},
			},
			{
				BeforeText: re(`<(?!(?:area|base|br|col|embed|hr|img|input|keygen|link|menuitem|meta|param|source|track|wbr))([_:\w][_:\w\-.\d]*)(?:(?:[^'"/>]|"[^"]*"|'[^']*')*?(?!\/)>)[^<]*$`, "i"),
				Action:     language.EnterAction{Indent: language.IndentIndent},
			},
		},
	}
	return make1(HTML, c)
}

func vb() *indent.Language {
	c := &language.Configuration{
		Brackets: standardBrackets,
		IndentationRules: &language.IndentationRules{
			DecreaseIndentPattern: re(`^\s*((End\s+(If|Sub|Function|Class|Module|Enum|Structure|Interface|Namespace|With|Select|Try|While|For|Property|Get|Set|SyncLock|Using|AddHandler|RaiseEvent|RemoveHandler|Event|Operator))|Else|ElseIf|Case|Catch|Finally|Loop|Next|Wend|Until)\b`, "i"),
			IncreaseIndentPattern: re(`^\s*((If|ElseIf).*Then(?!.*End\s+If)\s*(('|REM).*)?|(Else|While|For|Do|Select\s+Case|Case|Sub|Function|Class|Module|Enum|Structure|Interface|Namespace|With|Try|Catch|Finally|SyncLock|Using|Property|Get|Set|AddHandler|RaiseEvent|RemoveHandler|Event|Operator)\b(?!.*\bEnd\s+(If|Sub|Function|Class|Module|Enum|Structure|Interface|Namespace|With|Select|Try|While|For|Property|Get|Set|SyncLock|Using|AddHandler|RaiseEvent|RemoveHandler|Event|Operator)\b).*(('|REM).*)?)$`, "i"),
		},
		OnEnterRules: []language.OnEnterRule{{
			BeforeText: re(`^\s*((End\s+(If|Sub|Function|Class|Module|Enum|Structure|Interface|Namespace|With|Select|Try|While|For|Property|Get|Set|SyncLock|Using|AddHandler|RaiseEvent|RemoveHandler|Event|Operator))|Loop|Next|Wend|Until)\b.*$`, "i"),
			Action:     language.EnterAction{Indent: language.IndentNone},
		}},
	}
	return make1(VB, c)
}

func latex() *indent.Language {
	c := &language.Configuration{
		Brackets: append(pairs("{", "}", "[", "]", "(", ")", "[", ")", "(", "]"), latexBrackets()...),
		AutoClosingPairs: []language.AutoClosingPair{
			{Pair: language.Pair{Open: `\left(`, Close: `\right)`}}, {Pair: language.Pair{Open: `\left[`, Close: `\right]`}},
			{Pair: language.Pair{Open: `\left\{`, Close: `\right\}`}},
			{Pair: language.Pair{Open: `\(`, Close: `\)`}}, {Pair: language.Pair{Open: `\[`, Close: `\]`}},
			{Pair: language.Pair{Open: `\{`, Close: `\}`}},
			{Pair: language.Pair{Open: "{", Close: "}"}}, {Pair: language.Pair{Open: "[", Close: "]"}},
			{Pair: language.Pair{Open: "(", Close: ")"}}, {Pair: language.Pair{Open: "`", Close: "'"}},
		},
		IndentationRules: &language.IndentationRules{
			DecreaseIndentPattern: re(`^\s*\\end{(?!document)`),
			IncreaseIndentPattern: re(`\\begin{(?!document)([^}]*)}(?!.*\\end{\1})`),
		},
	}
	return make1(Latex, c)
}

// latexBrackets are the \left, \right and \big bracket pairs of LaTeX, as VS Code's tests list them.
func latexBrackets() []language.Pair {
	var out []language.Pair
	add := func(open, close string) { out = append(out, language.Pair{Open: open, Close: close}) }
	for _, p := range [][2]string{
		{`\left(`, `\right)`}, {`\left(`, `\right.`}, {`\left.`, `\right)`}, {`\left[`, `\right]`}, {`\left[`, `\right.`},
		{`\left.`, `\right]`}, {`\left\{`, `\right\}`}, {`\left\{`, `\right.`}, {`\left.`, `\right\}`}, {`\left<`, `\right>`},
	} {
		add(p[0], p[1])
	}
	for _, size := range []string{`\bigl`, `\Bigl`, `\biggl`, `\Biggl`} {
		bigr := strings.Replace(size, "l", "r", 1)
		add(size+"(", bigr+")")
		add(size+"[", bigr+"]")
		add(size+`\{`, bigr+`\}`)
	}
	add(`\langle`, `\rangle`)
	add(`\lvert`, `\rvert`)
	add(`\lVert`, `\rVert`)
	add(`\left|`, `\right|`)
	add(`\left\vert`, `\right\vert`)
	add(`\left\|`, `\right\|`)
	add(`\left\Vert`, `\right\Vert`)
	add(`\left\langle`, `\right\rangle`)
	add(`\left\lvert`, `\right\rvert`)
	add(`\left\lVert`, `\right\rVert`)
	for _, size := range []string{`\bigl`, `\Bigl`, `\biggl`, `\Biggl`} {
		bigr := strings.Replace(size, "l", "r", 1)
		add(size+`\langle`, bigr+`\rangle`)
		add(size+"|", bigr+"|")
		add(size+`\lvert`, bigr+`\rvert`)
		add(size+`\vert`, bigr+`\vert`)
		add(size+`\|`, bigr+`\|`)
		add(size+`\lVert`, bigr+`\rVert`)
		add(size+`\Vert`, bigr+`\Vert`)
	}
	return out
}

func lua() *indent.Language {
	c := &language.Configuration{
		Brackets: standardBrackets,
		IndentationRules: &language.IndentationRules{
			DecreaseIndentPattern: re(`^\s*((\b(elseif|else|end|until)\b)|(\})|(\)))`),
			IncreaseIndentPattern: re(`^((?!(\-\-)).)*((\b(else|function|then|do|repeat)\b((?!\b(end|until)\b).)*)|(\{\s*))$`),
		},
	}
	return make1(Lua, c)
}
