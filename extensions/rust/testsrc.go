package rust

import (
	"os"
	"path/filepath"
	"strings"
)

// Where the tests are (SPEC §10.32 "As built (step 26.7)"): libtest lists the names of the tests, not their places, so the places are
// read from the source of the target with a light scan: the attributes `#[test]` (and the attributes of the crates that wrap it, such as
// `#[tokio::test]`) in front of a function, the modules that nest them and the files modules name (`mod tests;`). A test that a macro
// generates, or a module with a `#[path]`, has no place and is shown without one.

// srcPlace is where a test or a module is.
type srcPlace struct {
	// File is the absolute path and Line the line of `fn` or `mod`, counted from 1.
	File string
	Line int
}

// srcIndex are the places of the tests and the modules of one target, by their names: "tests::adds", "tests" and "tests::nested".
type srcIndex struct {
	tests, mods map[string]srcPlace
}

const (
	maxScanFiles = 400
	maxScanDepth = 24
	maxScanBytes = 4 << 20
)

// scanTarget reads the crate whose root is the file and returns the places in it.
func scanTarget(root string) srcIndex {
	idx := srcIndex{tests: map[string]srcPlace{}, mods: map[string]srcPlace{}}
	s := &scanner{idx: idx, seen: map[string]bool{}}
	s.file(root, nil, filepath.Dir(root), 0)
	return idx
}

// scanner holds the state of a scan.
type scanner struct {
	idx   srcIndex
	seen  map[string]bool
	files int
}

// tokenKind is the kind of a token of the light lexer.
type tokenKind int

const (
	tokIdent tokenKind = iota
	tokPunct
	tokString
)

// token is a word, a punctuation mark or a string literal of the source.
type token struct {
	kind tokenKind
	text string
	line int
}

// lex cuts Rust source into identifiers, single punctuation characters and string literals. Comments, character literals and the
// contents of strings are dropped; a lifetime is a punctuation mark followed by an identifier.
func lex(src string) []token {
	var out []token
	line := 1
	i := 0
	n := len(src)
	isIdentStart := func(c byte) bool { return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80 }
	isIdent := func(c byte) bool { return isIdentStart(c) || c >= '0' && c <= '9' }
	for i < n {
		c := src[i]
		switch {
		case c == '\n':
			line++
			i++
		case c == ' ' || c == '\t' || c == '\r':
			i++
		case c == '/' && i+1 < n && src[i+1] == '/':
			for i < n && src[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < n && src[i+1] == '*':
			depth := 1
			i += 2
			for i < n && depth > 0 {
				switch {
				case src[i] == '\n':
					line++
					i++
				case src[i] == '/' && i+1 < n && src[i+1] == '*':
					depth++
					i += 2
				case src[i] == '*' && i+1 < n && src[i+1] == '/':
					depth--
					i += 2
				default:
					i++
				}
			}
		case c == '"':
			start := line
			i++
			for i < n && src[i] != '"' {
				if src[i] == '\\' && i+1 < n {
					if src[i+1] == '\n' {
						line++
					}
					i++
				}
				if src[i] == '\n' {
					line++
				}
				i++
			}
			i++
			out = append(out, token{kind: tokString, line: start})
		case (c == 'r' || c == 'b') && rawStringAt(src, i):
			start := line
			j := i
			for j < n && (src[j] == 'r' || src[j] == 'b') {
				j++
			}
			hashes := 0
			for j < n && src[j] == '#' {
				hashes++
				j++
			}
			j++ // the quote
			end := "\"" + strings.Repeat("#", hashes)
			k := strings.Index(src[j:], end)
			if k < 0 {
				k = n - j
			}
			line += strings.Count(src[j:j+k], "\n")
			i = j + k + len(end)
			out = append(out, token{kind: tokString, line: start})
		case c == '\'':
			// a character literal ('a', '\n', '\u{1F600}') or a lifetime ('a)
			if i+2 < n && src[i+1] == '\\' {
				j := i + 2
				for j < n && src[j] != '\'' && src[j] != '\n' {
					j++
				}
				i = j + 1
			} else if i+2 < n && src[i+2] == '\'' {
				i += 3
			} else if i+1 < n && src[i+1] >= 0x80 {
				j := i + 1
				for j < n && src[j] != '\'' && src[j] != '\n' {
					j++
				}
				i = j + 1
			} else {
				out = append(out, token{kind: tokPunct, text: "'", line: line})
				i++
			}
		case isIdentStart(c):
			j := i
			for j < n && isIdent(src[j]) {
				j++
			}
			out = append(out, token{kind: tokIdent, text: src[i:j], line: line})
			i = j
		case c >= '0' && c <= '9':
			j := i
			for j < n && (isIdent(src[j]) || src[j] == '.' && j+1 < n && src[j+1] >= '0' && src[j+1] <= '9') {
				j++
			}
			i = j
		default:
			out = append(out, token{kind: tokPunct, text: string(c), line: line})
			i++
		}
	}
	return out
}

// rawStringAt tells whether src[i:] starts a raw or byte string: r"..", r#".."#, b"..", br"..".
func rawStringAt(src string, i int) bool {
	j := i
	for j < len(src) && (src[j] == 'r' || src[j] == 'b') && j-i < 2 {
		j++
	}
	if j == i {
		return false
	}
	for j < len(src) && src[j] == '#' {
		j++
	}
	return j < len(src) && src[j] == '"' && (strings.ContainsRune(src[i:j], 'r') || src[j-1] != '#')
}

// testAttrs are the last segments of the attribute paths that make a function a test.
var testAttrs = map[string]bool{"test": true, "rstest": true}

// resetWords are the keywords of items that end the attributes waiting for a function.
var resetWords = map[string]bool{"mod": true, "struct": true, "enum": true, "impl": true, "trait": true, "type": true, "use": true, "static": true,
	"union": true, "macro_rules": true}

// file scans one source file. mods is the path of the modules above it, dir the folder its `mod x;` files are in.
func (s *scanner) file(path string, mods []string, dir string, depth int) {
	if depth > maxScanDepth || s.files >= maxScanFiles || s.seen[path] {
		return
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() > maxScanBytes {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	s.seen[path] = true
	s.files++
	toks := lex(string(data))
	type frame struct {
		name  string
		depth int
		dir   string
	}
	var stack []frame
	level := 0
	cur := func() ([]string, string) {
		m, d := append([]string(nil), mods...), dir
		for _, f := range stack {
			m = append(m, f.name)
			d = f.dir
		}
		return m, d
	}
	join := func(m []string, name string) string {
		return strings.Join(append(append([]string(nil), m...), name), "::")
	}
	pending := false
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		switch {
		case t.kind == tokPunct && t.text == "#":
			j := i + 1
			if j < len(toks) && toks[j].text == "!" {
				j++
			}
			if j >= len(toks) || toks[j].text != "[" {
				continue
			}
			// the attribute's path: idents joined by ::
			k := j + 1
			last := ""
			for k < len(toks) && (toks[k].kind == tokIdent || toks[k].text == ":") {
				if toks[k].kind == tokIdent {
					last = toks[k].text
				}
				k++
			}
			if testAttrs[last] && toks[i+1].text != "!" {
				pending = true
			}
			// skip to the closing bracket
			bd := 0
			for ; k < len(toks); k++ {
				if toks[k].text == "[" {
					bd++
				} else if toks[k].text == "]" {
					if bd == 0 {
						break
					}
					bd--
				}
			}
			i = k
		case t.kind == tokPunct && t.text == "{":
			level++
			pending = false
		case t.kind == tokPunct && t.text == "}":
			level--
			pending = false
			if len(stack) > 0 && stack[len(stack)-1].depth == level {
				stack = stack[:len(stack)-1]
			}
		case t.kind == tokPunct && t.text == ";":
			pending = false
		case t.kind == tokIdent && t.text == "mod" && i+1 < len(toks) && toks[i+1].kind == tokIdent:
			name := toks[i+1].text
			m, d := cur()
			pending = false
			if i+2 < len(toks) && toks[i+2].text == "{" {
				s.idx.mods[join(m, name)] = srcPlace{File: path, Line: t.line}
				stack = append(stack, frame{name: name, depth: level, dir: filepath.Join(d, name)})
				level++
				i += 2
			} else if i+2 < len(toks) && toks[i+2].text == ";" {
				s.idx.mods[join(m, name)] = srcPlace{File: filepath.Join(d, name+".rs"), Line: 1}
				child := filepath.Join(d, name+".rs")
				childDir := filepath.Join(d, name)
				if _, err := os.Stat(child); err != nil {
					child = filepath.Join(d, name, "mod.rs")
					childDir = filepath.Join(d, name)
					if _, err := os.Stat(child); err == nil {
						s.idx.mods[join(m, name)] = srcPlace{File: child, Line: 1}
					}
				}
				s.file(child, append(m, name), childDir, depth+1)
				i += 2
			}
		case t.kind == tokIdent && t.text == "fn" && i+1 < len(toks) && toks[i+1].kind == tokIdent:
			if pending && (len(stack) == 0 && level == 0 || len(stack) > 0 && stack[len(stack)-1].depth+1 == level) {
				m, _ := cur()
				s.idx.tests[join(m, toks[i+1].text)] = srcPlace{File: path, Line: t.line}
			}
			pending = false
		case t.kind == tokIdent && resetWords[t.text]:
			pending = false
		}
	}
}
