package fuzzy

import (
	"strings"
	"testing"
)

// Tests ported from VS Code: src/vs/base/test/common/filters.test.ts
//
// Only the tests of fuzzyScore are ported (the word scorer behind FuzzyScore). The file has
// 41 tests; 26 are ported here, with VS Code's expected strings: "fuzzyScore, #23215", "#23332",
// "#23190", "#23581", "#23458", "#23746", "fuzzyScore", "fuzzyScore (first match can be weak)",
// "fuzzyScore, many matches", "Freeze when fjfj -> jfjf", "fuzzyScore, issue #26423", "Fuzzy
// IntelliSense matching vs Haxe metadata completion, #26995", "Separator only match should not
// be weak #79558", "Cannot set property '1' of undefined, #26511", "Vscode 1.12 no longer obeys
// 'sortText' ...  #26096", "patternPos isn't working correctly #79815", "topScore -
// fuzzyScore", "Unexpected suggestion scoring, #28791", "HTML closing tag proposal filtered out
// #38880", "List highlight filter ... #66923", "Autocompletion is matched against truncated
// filterText ... #74133", "\"Go to Symbol\" with the exact method name ... #84787", "Wrong
// highlight after emoji #113404", "IntelliSense completion not correctly highlighting text in
// front of cursor #115250" (its first assert; the others use anyScore), "configurable full match
// boost" and "Unexpected suggest highlighting ignores whole word match ... #147423".
// Not ported (15): the 11 tests of filters that are not part of the package (or, prefix, camel
// case, contiguous and sub string, words), "fuzzyScoreGraceful", "Suggestion is not highlighted
// #85826" and "anyScore should not require a strong first match" (they test the graceful and
// anyScore variants, which the scorer does not use; the other two asserts of #115250 use
// anyScore too), and the skipped test of #187055.

type wordCase struct {
	pattern, word, decorated string // decorated "" is no match
	patternPos, wordPos      int
	weak                     bool
}

// decorate marks the matches in the word with a ^ before each rune.
func decorate(word string, matches []Match) string {
	runes := []rune(word)
	var sb strings.Builder
	pos := 0
	for _, m := range matches {
		sb.WriteString(string(runes[pos:m.Start]))
		for _, r := range runes[m.Start:m.End] {
			sb.WriteRune('^')
			sb.WriteRune(r)
		}
		pos = m.End
	}
	sb.WriteString(string(runes[pos:]))
	return sb.String()
}

func TestFuzzyScoreWords(t *testing.T) {
	long := strings.Repeat("a", 300)
	tests := []wordCase{
		// fuzzyScore, #23215
		{pattern: "tit", word: "win.tit", decorated: "win.^t^i^t"},
		{pattern: "title", word: "win.title", decorated: "win.^t^i^t^l^e"},
		{pattern: "WordCla", word: "WordCharacterClassifier", decorated: "^W^o^r^dCharacter^C^l^assifier"},
		{pattern: "WordCCla", word: "WordCharacterClassifier", decorated: "^W^o^r^d^Character^C^l^assifier"},
		// fuzzyScore, #23332
		{pattern: "dete", word: `"editor.quickSuggestionsDelay"`},
		// fuzzyScore, #23190
		{pattern: `c:\do`, word: `& 'C:\Documents and Settings'`, decorated: `& '^C^:^\^D^ocuments and Settings'`},
		{pattern: `c:\do`, word: `& 'c:\Documents and Settings'`, decorated: `& '^c^:^\^D^ocuments and Settings'`},
		// fuzzyScore, #23581
		{pattern: "close", word: "css.lint.importStatement", decorated: "^css.^lint.imp^ort^Stat^ement"},
		{pattern: "close", word: "css.colorDecorators.enable", decorated: "^css.co^l^orDecorator^s.^enable"},
		{pattern: "close", word: "workbench.quickOpen.closeOnFocusOut", decorated: "workbench.quickOpen.^c^l^o^s^eOnFocusOut"},
		// fuzzyScore, #23458
		{pattern: "highlight", word: "editorHoverHighlight", decorated: "editorHover^H^i^g^h^l^i^g^h^t"},
		{pattern: "hhighlight", word: "editorHoverHighlight", decorated: "editor^Hover^H^i^g^h^l^i^g^h^t"},
		{pattern: "dhhighlight", word: "editorHoverHighlight"},
		// fuzzyScore, #23746
		{pattern: "-moz", word: "-moz-foo", decorated: "^-^m^o^z-foo"},
		{pattern: "moz", word: "-moz-foo", decorated: "-^m^o^z-foo"},
		{pattern: "moz", word: "-moz-animation", decorated: "-^m^o^z-animation"},
		{pattern: "moza", word: "-moz-animation", decorated: "-^m^o^z-^animation"},
		// fuzzyScore
		{pattern: "ab", word: "abA", decorated: "^a^bA"},
		{pattern: "ccm", word: "cacmelCase", decorated: "^ca^c^melCase"},
		{pattern: "bti", word: "the_black_knight"},
		{pattern: "ccm", word: "camelCase"},
		{pattern: "cmcm", word: "camelCase"},
		{pattern: "BK", word: "the_black_knight", decorated: "the_^black_^knight"},
		{pattern: "KeyboardLayout=", word: "KeyboardLayout"},
		{pattern: "LLL", word: "SVisualLoggerLogsList", decorated: "SVisual^Logger^Logs^List"},
		{pattern: "LLLL", word: "SVilLoLosLi"},
		{pattern: "LLLL", word: "SVisualLoggerLogsList"},
		{pattern: "TEdit", word: "TextEdit", decorated: "^Text^E^d^i^t"},
		{pattern: "TEdit", word: "TextEditor", decorated: "^Text^E^d^i^tor"},
		{pattern: "TEdit", word: "Textedit", decorated: "^Text^e^d^i^t"},
		{pattern: "TEdit", word: "text_edit", decorated: "^text_^e^d^i^t"},
		{pattern: "TEditDit", word: "TextEditorDecorationType", decorated: "^Text^E^d^i^tor^Decorat^ion^Type"},
		{pattern: "TEdit", word: "TextEditorDecorationType", decorated: "^Text^E^d^i^torDecorationType"},
		{pattern: "Tedit", word: "TextEdit", decorated: "^Text^E^d^i^t"},
		{pattern: "ba", word: "?AB?"},
		{pattern: "bkn", word: "the_black_knight", decorated: "the_^black_^k^night"},
		{pattern: "bt", word: "the_black_knight", decorated: "the_^black_knigh^t"},
		{pattern: "ccm", word: "camelCasecm", decorated: "^camel^Casec^m"},
		{pattern: "fdm", word: "findModel", decorated: "^fin^d^Model"},
		{pattern: "fob", word: "foobar", decorated: "^f^oo^bar"},
		{pattern: "fobz", word: "foobar"},
		{pattern: "foobar", word: "foobar", decorated: "^f^o^o^b^a^r"},
		{pattern: "form", word: "editor.formatOnSave", decorated: "editor.^f^o^r^matOnSave"},
		{pattern: "g p", word: "Git: Pull", decorated: "^Git:^ ^Pull"},
		{pattern: "gip", word: "Git: Pull", decorated: "^G^it: ^Pull"},
		{pattern: "gp", word: "Git: Pull", decorated: "^Git: ^Pull"},
		{pattern: "gp", word: "Git_Git_Pull", decorated: "^Git_Git_^Pull"},
		{pattern: "is", word: "ImportStatement", decorated: "^Import^Statement"},
		{pattern: "is", word: "isValid", decorated: "^i^sValid"},
		{pattern: "lowrd", word: "lowWord", decorated: "^l^o^wWo^r^d"},
		{pattern: "myvable", word: "myvariable", decorated: "^m^y^v^aria^b^l^e"},
		{pattern: "no", word: ""},
		{pattern: "no", word: "match"},
		{pattern: "ob", word: "foobar"},
		{pattern: "sl", word: "SVisualLoggerLogsList", decorated: "^SVisual^LoggerLogsList"},
		{pattern: "sllll", word: "SVisualLoggerLogsList", decorated: "^SVisua^l^Logger^Logs^List"},
		{pattern: "Three", word: "HTMLHRElement"},
		{pattern: "Three", word: "Three", decorated: "^T^h^r^e^e"},
		{pattern: "fo", word: "barfoo"},
		{pattern: "fo", word: "bar_foo", decorated: "bar_^f^oo"},
		{pattern: "fo", word: "bar_Foo", decorated: "bar_^F^oo"},
		{pattern: "fo", word: "bar foo", decorated: "bar ^f^oo"},
		{pattern: "fo", word: "bar.foo", decorated: "bar.^f^oo"},
		{pattern: "fo", word: "bar/foo", decorated: "bar/^f^oo"},
		{pattern: "fo", word: `bar\foo`, decorated: `bar\^f^oo`},
		// fuzzyScore (first match can be weak)
		{pattern: "Three", word: "HTMLHRElement", decorated: "H^TML^H^R^El^ement", weak: true},
		{pattern: "tor", word: "constructor", decorated: "construc^t^o^r", weak: true},
		{pattern: "ur", word: "constructor", decorated: "constr^ucto^r", weak: true},
		// fuzzyScore, many matches (only the first 128 runes are looked at)
		{pattern: "aaaaaa", word: long, decorated: "^a^a^a^a^a^a" + long[6:]},
		// Freeze when fjfj -> jfjf
		{pattern: "jfjfj", word: "fjfjfjfjfjfjfjfjfjfjfj"},
		{pattern: "jfjfjfjfjfjfjfjfjfj", word: "fjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfj"},
		{pattern: "jfjfjfjfjfjfjfjfjfjjfjfjfjfjfjfjfjfjfjjfjfjfjfjfjfjfjfjfjjfjfjfjfjfjfjfjfjfjjfjfjfjfjfjfjfjfjfjjfjfjfjfjfjfjfjfjfj",
			word: "fjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfj"},
		{pattern: "jfjfjfjfjfjfjfjfjfj", word: "fJfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfj",
			decorated: "f^J^f^j^f^j^f^j^f^j^f^j^f^j^f^j^f^j^f^jfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfj"}, // strong match
		{pattern: "jfjfjfjfjfjfjfjfjfj", word: "fjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfj",
			decorated: "f^j^f^j^f^j^f^j^f^j^f^j^f^j^f^j^f^j^f^jfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfjfj", weak: true}, // any match
		// fuzzyScore, issue #26423
		{pattern: "baba", word: "abababab"},
		{pattern: "fsfsfs", word: "dsafdsafdsafdsafdsafdsafdsafasdfdsa"},
		{pattern: "fsfsfsfsfsfsfsf", word: "dsafdsafdsafdsafdsafdsafdsafasdfdsafdsafdsafdsafdsfdsafdsfdfdfasdnfdsajfndsjnafjndsajlknfdsa"},
		// Fuzzy IntelliSense matching vs Haxe metadata completion, #26995
		{pattern: "f", word: ":Foo", decorated: ":^Foo"},
		{pattern: "f", word: ":foo", decorated: ":^foo"},
		// Separator only match should not be weak #79558
		{pattern: ".", word: "foo.bar", decorated: "foo^.bar"},
		// Vscode 1.12 no longer obeys 'sortText' in completion items (from language server), #26096
		{pattern: "  ", word: "  group", patternPos: 2},
		{pattern: "  g", word: "  group", decorated: "  ^group", patternPos: 2},
		{pattern: "g", word: "  group", decorated: "  ^group"},
		{pattern: "g g", word: "  groupGroup"},
		{pattern: "g g", word: "  group Group", decorated: "  ^group^ ^Group"},
		{pattern: " g g", word: "  group Group", decorated: "  ^group^ ^Group", patternPos: 1},
		{pattern: "zz", word: "zzGroup", decorated: "^z^zGroup"},
		{pattern: "zzg", word: "zzGroup", decorated: "^z^z^Group"},
		{pattern: "g", word: "zzGroup", decorated: "zz^Group"},
		// patternPos isn't working correctly #79815
		{pattern: "p", word: "prop", decorated: "^prop"},
		{pattern: ":p", word: "prop", decorated: "^prop", patternPos: 1},
		{pattern: ":p", word: "prop", patternPos: 2},
		{pattern: ":p", word: "proP", decorated: "pro^P", patternPos: 1, wordPos: 1},
		{pattern: ":p", word: "aprop", decorated: "a^prop", patternPos: 1, weak: true},
		{pattern: ":p", word: "aprop", patternPos: 1},
		// List highlight filter: Not all characters from match are highlighterd #66923
		{pattern: "foo", word: "barbarbarbarbarbarbarbarbarbarbarbarbarbarbarbar_foo", decorated: "barbarbarbarbarbarbarbarbarbarbarbarbarbarbarbar_^f^o^o"},
		// Autocompletion is matched against truncated filterText to 54 characters #74133
		{pattern: "foo", word: "ffffffffffffffffffffffffffffbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbar_foo",
			decorated: "ffffffffffffffffffffffffffffbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbar_^f^o^o"},
		{pattern: "Aoo", word: "Affffffffffffffffffffffffffffbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbar_foo",
			decorated: "^Affffffffffffffffffffffffffffbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbar_f^o^o"},
		{pattern: "foo", word: "Gffffffffffffffffffffffffffffbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbarbar_foo"},
		// Wrong highlight after emoji #113404
		{pattern: "di", word: `✨div classname=""></div>`, decorated: `✨^d^iv classname=""></div>`},
		{pattern: "di", word: `adiv classname=""></div>`, decorated: `adiv classname=""></^d^iv>`},
		// IntelliSense completion not correctly highlighting text in front of cursor #115250
		{pattern: "lo", word: "log", decorated: "^l^og"},
		// Unexpected suggest highlighting ignores whole word match in favor of matching first letter #147423
		{pattern: "i", word: "machine/{id}", decorated: "machine/{^id}"},
		{pattern: "ok", word: "obobobf{ok}/user", decorated: "^obobobf{o^k}/user"},
		// HTML closing tag proposal filtered out #38880
		{pattern: "\t\t<", word: "\t\t</body>", decorated: "^\t^\t^</body>"},
		{pattern: "\t\t<", word: "\t\t</body>", decorated: "\t\t^</body>", patternPos: 2},
		{pattern: "\t<", word: "\t</body>", decorated: "\t^</body>", patternPos: 1},
	}
	s := NewScorer()
	for _, tt := range tests {
		name := tt.pattern + " in " + tt.word
		if len(name) > 60 {
			name = name[:60]
		}
		t.Run(name, func(t *testing.T) {
			r, ok := s.FuzzyScore(tt.pattern, strings.ToLower(tt.pattern), tt.patternPos, tt.word, strings.ToLower(tt.word), tt.wordPos,
				FuzzyScoreOptions{FirstMatchCanBeWeak: tt.weak, BoostFullMatch: true})
			if ok != (tt.decorated != "") {
				t.Fatalf("matched = %v, want %v", ok, tt.decorated != "")
			}
			if !ok {
				return
			}
			if got := decorate(tt.word, r.Matches()); got != tt.decorated {
				t.Errorf("matches %q, want %q", got, tt.decorated)
			}
		})
	}
}

func TestFuzzyScoreTopScore(t *testing.T) {
	// test: topScore - fuzzyScore, Unexpected suggestion scoring #28791 and the top score of
	// "fuzzyScore (first match can be weak)" (tor)
	tests := []struct {
		pattern string
		want    int
		words   []string
	}{
		{"tor", 2, []string{"constructor", "Thor", "cTor"}},
		{"cons", 2, []string{"ArrayBufferConstructor", "Console", "console"}},
		{"Foo", 1, []string{"foo", "Foo", "foo"}},
		{"onMess", 1, []string{"onmessage", "onMessage", "onThisMegaEscape"}}, // #24904
		{"CC", 1, []string{"camelCase", "CamelCase"}},
		{"cC", 0, []string{"camelCase", "CamelCase"}},
		{"p", 4, []string{"parse", "posix", "pafdsa", "path", "p"}},
		{"pa", 0, []string{"parse", "pafdsa", "path"}},
		{"log", 3, []string{"HTMLOptGroupElement", "ScrollLogicalPosition", "SVGFEMorphologyElement", "log", "logger"}}, // #14583
		{"e", 2, []string{"AbstractWorker", "ActiveXObject", "else"}},
		{"workbench.sideb", 1, []string{"workbench.editor.defaultSideBySideLayout", "workbench.sideBar.location"}}, // #14446
		{"editor.r", 2, []string{"diffEditor.renderSideBySide", "editor.overviewRulerlanes", "editor.renderControlCharacter", "editor.renderWhitespace"}},
		{"-mo", 1, []string{"-ms-ime-mode", "-moz-columns"}},
		{"convertModelPosition", 0, []string{"convertModelPositionToViewPosition", "convertViewToModelPosition"}},
		{"is", 0, []string{"isValidViewletId", "import statement"}},
		{"title", 1, []string{"files.trimTrailingWhitespace", "window.title"}},
		{"const", 1, []string{"constructor", "const", "cuOnstrul"}},
		{"_lines", 1, []string{"_lineStarts", "_lines"}}, // #28791
		{"_lines", 1, []string{"_lineS", "_lines"}},
		{"_lineS", 0, []string{"_lineS", "_lines"}},
	}
	s := NewScorer()
	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			topScore, topIdx := -(100 * 10), 0
			for i, w := range tt.words {
				// the options of assertTopScore: the defaults of VS Code
				m, ok := s.FuzzyScore(tt.pattern, strings.ToLower(tt.pattern), 0, w, strings.ToLower(w), 0, DefaultFuzzyScoreOptions)
				if ok && m.Score > topScore {
					topScore, topIdx = m.Score, i
				}
			}
			if topIdx != tt.want {
				t.Errorf("%s -> actual=%s <> expected=%s", tt.pattern, tt.words[topIdx], tt.words[tt.want])
			}
		})
	}
}

func TestFuzzyScoreLongInputsAreBounded(t *testing.T) {
	// test: Cannot set property '1' of undefined, #26511 (must not explode), and the limit of 128
	word := strings.Repeat("a", 122)
	pattern := strings.Repeat("a", 119)
	if _, ok := FuzzyScore(pattern, pattern, 0, word, word, 0, DefaultFuzzyScoreOptions); !ok {
		t.Error("no match")
	}
	huge := strings.Repeat("ab", 50000)
	r, ok := FuzzyScore("ab", "ab", 0, huge, huge, 0, DefaultFuzzyScoreOptions)
	if !ok || len(r.Positions) != 2 {
		t.Errorf("huge word: %+v %v", r, ok)
	}
}

func TestFuzzyScoreWordsOther(t *testing.T) {
	s := NewScorer()
	weak := FuzzyScoreOptions{FirstMatchCanBeWeak: true, BoostFullMatch: true}

	// test: "Go to Symbol" with the exact method name doesn't work as expected #84787
	if _, ok := s.FuzzyScore(":get", ":get", 1, "get", "get", 0, weak); !ok {
		t.Error("#84787: no match")
	}

	// test: configurable full match boost
	prefix, a, b := "create", "createModelServices", "create"
	for _, wordPrefix := range []string{"", "$(symbol-function) "} {
		wa, wb := wordPrefix+a, wordPrefix+b
		start := len([]rune(wordPrefix))
		aBoost, okA := s.FuzzyScore(prefix, prefix, 0, wa, strings.ToLower(wa), start, weak)
		bBoost, okB := s.FuzzyScore(prefix, prefix, 0, wb, strings.ToLower(wb), start, weak)
		if !okA || !okB || aBoost.Score >= bBoost.Score {
			t.Errorf("boost with prefix %q: a=%+v b=%+v", wordPrefix, aBoost, bBoost)
		}
	}
	noBoost := FuzzyScoreOptions{FirstMatchCanBeWeak: true}
	aScore, okA := s.FuzzyScore(prefix, prefix, 0, a, strings.ToLower(a), 0, noBoost)
	bScore, okB := s.FuzzyScore(prefix, prefix, 0, b, strings.ToLower(b), 0, noBoost)
	if !okA || !okB || aScore.Score != bScore.Score {
		t.Errorf("without boost: a=%+v b=%+v", aScore, bScore)
	}
}
