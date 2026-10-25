package markdown

import (
	"math/rand"
	"strings"
	"testing"
	"time"
)

// inlineCase is a source that parses to one paragraph, and the dump of its inlines.
type inlineCase = struct{ name, src, want string }

func runInlineCases(t *testing.T, cases []inlineCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bs := parse(tc.src)
			if len(bs) != 1 {
				t.Fatalf("src %q: got %d blocks (%s), want 1 paragraph", tc.src, len(bs), dumpBlocks(bs))
			}
			p, ok := bs[0].(Paragraph)
			if !ok {
				t.Fatalf("src %q: got %s, want a paragraph", tc.src, dumpBlocks(bs))
			}
			if got := dumpInlines(p.Inlines); got != tc.want {
				t.Errorf("src %q\n got: %s\nwant: %s", tc.src, got, tc.want)
			}
		})
	}
}

func TestEmphasis(t *testing.T) {
	runInlineCases(t, []inlineCase{
		{"em star", "*foo bar*", `(em "foo bar")`},
		{"star followed by space", "a * foo bar*", `"a * foo bar*"`},
		{"star between quotes", "a*\"foo\"*", `"a*\"foo\"*"`},
		{"spaced star pair", "a * b *", `"a * b *"`},
		{"intraword star", "foo*bar*", `"foo" (em "bar")`},
		{"digits star", "5*6*78", `"5" (em "6") "78"`},
		{"em underscore", "_foo bar_", `(em "foo bar")`},
		{"underscore followed by space", "_ foo bar_", `"_ foo bar_"`},
		{"underscore between quotes", "a_\"foo\"_", `"a_\"foo\"_"`},
		{"intraword underscore", "foo_bar_", `"foo_bar_"`},
		{"digits underscore", "5_6_78", `"5_6_78"`},
		{"cyrillic underscore", "пристаням_стремятся_", `"пристаням_стремятся_"`},
		{"quotes underscore", "aa_\"bb\"_cc", `"aa_\"bb\"_cc"`},
		{"underscore after punctuation", "foo-_(bar)_", `"foo-" (em "(bar)")`},
		{"mismatched delimiters", "_foo*", `"_foo*"`},
		{"closer preceded by space", "*foo bar *", `"*foo bar *"`},
		{"closer on next line", "*foo bar\n*", `"*foo bar" (sb) "*"`},
		{"opener before punctuation", "*(*foo)", `"*(*foo)"`},
		{"nested punctuation", "*(*foo*)*", `(em "(" (em "foo") ")")`},
		{"closer followed by text", "*foo*bar", `(em "foo") "bar"`},
		{"underscore closer preceded by space", "_foo bar _", `"_foo bar _"`},
		{"underscore opener before punctuation", "_(_foo)", `"_(_foo)"`},
		{"nested underscores", "_(_foo_)_", `(em "(" (em "foo") ")")`},
		{"underscore closer followed by text", "_foo_bar", `"_foo_bar"`},
		{"underscore cyrillic", "_пристаням_стремятся", `"_пристаням_стремятся"`},
		{"underscore inside underscore", "_foo_bar_baz_", `(em "foo_bar_baz")`},
		{"underscore then period", "_(bar)_.", `(em "(bar)") "."`},
		{"strong star", "**foo bar**", `(strong "foo bar")`},
		{"strong opener followed by space", "** foo bar**", `"** foo bar**"`},
		{"strong between quotes", "a**\"foo\"**", `"a**\"foo\"**"`},
		{"strong intraword", "foo**bar**", `"foo" (strong "bar")`},
		{"strong underscore", "__foo bar__", `(strong "foo bar")`},
		{"strong underscore opener followed by space", "__ foo bar__", `"__ foo bar__"`},
		{"strong intraword underscore", "foo__bar__", `"foo__bar__"`},
		{"strong digits underscore", "5__6__78", `"5__6__78"`},
		{"strong nested in strong", "__foo, __bar__, baz__", `(strong "foo, " (strong "bar") ", baz")`},
		{"strong after punctuation", "foo-__(bar)__", `"foo-" (strong "(bar)")`},
		{"strong closer preceded by space", "**foo bar **", `"**foo bar **"`},
		{"strong opener before punctuation", "**(**foo)", `"**(**foo)"`},
		{"em with strong inside parens", "*(**foo**)*", `(em "(" (strong "foo") ")")`},
		{"strong with em across lines", "**Gomphocarpus (*Gomphocarpus physocarpus*, syn.\n*Asclepias physocarpa*)**", `(strong "Gomphocarpus (" (em "Gomphocarpus physocarpus") ", syn." (sb) (em "Asclepias physocarpa") ")")`},
		{"strong around quotes", "**foo \"*bar*\" foo**", `(strong "foo \"" (em "bar") "\" foo")`},
		{"strong then text", "**foo**bar", `(strong "foo") "bar"`},
		{"underscore strong closer preceded by space", "__foo bar __", `"__foo bar __"`},
		{"underscore strong in parens", "_(__foo__)_", `(em "(" (strong "foo") ")")`},
		{"underscore strong followed by text", "__foo__bar", `"__foo__bar"`},
		{"underscore strong nested", "__foo__bar__baz__", `(strong "foo__bar__baz")`},
		{"underscore strong then period", "__(bar)__.", `(strong "(bar)") "."`},
		{"em with link", "*foo [bar](/url)*", `(em "foo " (a "/url" "bar"))`},
		{"em across lines", "*foo\nbar*", `(em "foo" (sb) "bar")`},
		{"em with strong", "_foo __bar__ baz_", `(em "foo " (strong "bar") " baz")`},
		{"em in em", "_foo _bar_ baz_", `(em "foo " (em "bar") " baz")`},
		{"em with underscores", "__foo_ bar_", `(em (em "foo") " bar")`},
		{"em star in em", "*foo *bar**", `(em "foo " (em "bar"))`},
		{"em with strong star", "*foo **bar** baz*", `(em "foo " (strong "bar") " baz")`},
		{"em intraword strong", "*foo**bar**baz*", `(em "foo" (strong "bar") "baz")`},
		{"multiple of three", "*foo**bar*", `(em "foo**bar")`},
		{"strong inside em", "***foo** bar*", `(em (strong "foo") " bar")`},
		{"strong at the end of em", "*foo **bar***", `(em "foo " (strong "bar"))`},
		{"intraword strong at the end", "*foo**bar***", `(em "foo" (strong "bar"))`},
		{"triple intraword", "foo***bar***baz", `"foo" (em (strong "bar")) "baz"`},
		{"many stars", "foo******bar*********baz", `"foo" (strong (strong (strong "bar"))) "***baz"`},
		{"complex nesting", "*foo **bar *baz* bim** bop*", `(em "foo " (strong "bar " (em "baz") " bim") " bop")`},
		{"em with link and em", "*foo [*bar*](/url)*", `(em "foo " (a "/url" (em "bar")))`},
		{"empty em", "** is not an empty emphasis", `"** is not an empty emphasis"`},
		{"empty strong", "**** is not an empty strong emphasis", `"**** is not an empty strong emphasis"`},
		{"odd match star", "**foo*bar**", `(strong "foo*bar")`},
		{"snake case", "snake_case_word", `"snake_case_word"`},
		{"snake case in text", "use snake_case_word and __init__.py", `"use snake_case_word and " (strong "init") ".py"`},
		{"em and code", "*foo`*`", `"*foo" (code "*")`},
		{"escaped stars", "\\*not emphasized*", `"*not emphasized*"`},
		{"strike", "~~del~~", `(del "del")`},
		{"strike with strong", "~~a **b** c~~", `(del "a " (strong "b") " c")`},
		{"strike intraword", "a~~b~~c", `"a" (del "b") "c"`},
		{"single tilde is text", "~single~", `"~single~"`},
		{"three tildes are text", "x ~~~three~~~", `"x ~~~three~~~"`},
		{"strike opener followed by space", "a ~~ b ~~", `"a ~~ b ~~"`},
		{"strike unclosed", "~~open", `"~~open"`},
		{"strike in em", "*a ~~b~~ c*", `(em "a " (del "b") " c")`},
		{"em in strike", "~~a *b* c~~", `(del "a " (em "b") " c")`},
		{"unmatched stars stay", "**a*", `"*" (em "a")`},
	})
}

// "* a *" is a list item, not a paragraph; check it separately to keep the table homogeneous.
func TestStarSpaceIsAList(t *testing.T) {
	got := dumpBlocks(parse("* a *"))
	if want := `(ul tight (li (p "a *")))`; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestCodeSpans(t *testing.T) {
	runInlineCases(t, []inlineCase{
		{"simple", "`foo`", `(code "foo")`},
		{"double", "`` foo ` bar ``", `(code "foo ` + "`" + ` bar")`},
		{"spaces stripped once", "` `` `", "(code \"``\")"},
		{"only one pair of spaces", "`  ``  `", "(code \" `` \")"},
		{"leading space only", "` a`", `(code " a")`},
		{"all spaces", "` `", `(code " ")`},
		{"line endings become spaces", "`foo   bar \nbaz`", `(code "foo   bar  baz")`},
		{"backslash is literal", "`foo\\`bar`", "(code \"foo\\\\\") \"bar`\""},
		{"longer fence", "``foo`bar``", "(code \"foo`bar\")"},
		{"different length", "`foo``bar``", "\"`foo\" (code \"bar\")"},
		{"unclosed", "`foo", "\"`foo\""},
		{"unclosed double", "``foo`", "\"``foo`\""},
		{"in emphasis", "*a `b*` c*", "(em \"a \" (code \"b*\") \" c\")"},
		{"not a link", "[not a `link](/foo`)", "\"[not a \" (code \"link](/foo\") \")\""},
		{"html in code", "`<a href=\"`\">`", "(code \"<a href=\\\"\") \"\\\">`\""},
		{"entities not decoded", "`&amp;`", "(code \"&amp;\")"},
	})
}

func TestLinks(t *testing.T) {
	runInlineCases(t, []inlineCase{
		{"with title", "[link](/uri \"title\")", `(a "/uri" (title "title") "link")`},
		{"without title", "[link](/uri)", `(a "/uri" "link")`},
		{"empty destination", "[link]()", `(a "" "link")`},
		{"angle empty destination", "[link](<>)", `(a "" "link")`},
		{"space in destination", "[link](/my uri)", `"[link](/my uri)"`},
		{"angle destination with space", "[link](</my uri>)", `(a "/my uri" "link")`},
		{"newline in angle destination", "[link](foo\nbar)", `"[link](foo" (sb) "bar)"`},
		{"angle with paren", "[a](<b)c>)", `(a "b)c" "a")`},
		{"angle unclosed", "[link](<foo(and(bar)>)", `(a "foo(and(bar)" "link")`},
		{"escaped parens", "[link](foo\\(and\\(bar\\))", `(a "foo(and(bar)" "link")`},
		{"balanced parens", "[link](foo(and(bar)))", `(a "foo(and(bar))" "link")`},
		{"unbalanced parens", "[link](foo(and(bar))", `"[link](foo(and(bar))"`},
		{"quoted title only", "[link](\"title\")", `(a "\"title\"" "link")`},
		{"destination and title across lines", "[link](   /uri\n  \"title\"  )", `(a "/uri" (title "title") "link")`},
		{"single quote title", "[link](/url 'title')", `(a "/url" (title "title") "link")`},
		{"paren title", "[link](/url (title))", `(a "/url" (title "title") "link")`},
		{"title with escaped quotes", "[link](/url \"title \\\"&quot;\")", `(a "/url" (title "title \"\"") "link")`},
		{"two titles", "[link](/url \"title\" \"and title\")", `"[link](/url \"title\" \"and title\")"`},
		{"escapes in destination and title", "[link](/url\\bar\\*baz \"foo\\\"bar\\baz\")", `(a "/url\\bar*baz" (title "foo\"bar\\baz") "link")`},
		{"entity in destination", "[link](foo%20b&auml;)", `(a "foo%20bä" "link")`},
		{"title needs a space", "[link](/url\"title\")", `(a "/url\"title\"" "link")`},
		{"emphasis in text", "[link *foo **bar** `#`*](/uri)", `(a "/uri" "link " (em "foo " (strong "bar") " " (code "#")))`},
		{"image in text", "[![moon](moon.jpg)](/uri)", `(a "/uri" (img "moon.jpg" "moon"))`},
		{"links do not nest", "[foo [bar](/uri)](/uri)", `"[foo " (a "/uri" "bar") "](/uri)"`},
		{"links do not nest deeper", "[foo *[bar [baz](/uri)](/uri)*](/uri)", `"[foo " (em "[bar " (a "/uri" "baz") "](/uri)") "](/uri)"`},
		{"image may contain a link", "![[[foo](uri1)](uri2)](uri3)", `(img "uri3" "[foo](uri2)")`},
		{"star outside link", "*[foo*](/uri)", `"*" (a "/uri" "foo*")`},
		{"star inside link", "[foo *bar](baz*)", `(a "baz*" "foo *bar")`},
		{"brackets bind tighter than emphasis", "*foo [bar* baz]", `(em "foo [bar") " baz]"`},
		{"code binds tighter", "[foo`](/uri)`", "\"[foo\" (code \"](/uri)\")"},
		{"link text with escaped bracket", "[foo\\]](/u)", `(a "/u" "foo]")`},
		{"unmatched brackets", "[foo", `"[foo"`},
		{"unmatched closer", "foo]", `"foo]"`},
		{"empty text", "[](/u)", `(a "/u")`},
		{"space before paren", "[link] (/uri)", `"[link] (/uri)"`},
	})
	for _, tc := range []struct{ name, src, want string }{
		{"fragment", "[link](#fragment)", `(a "#fragment" "link")`},
		{"url", "[link](http://example.com?foo=3#frag)", `(a "http://example.com?foo=3#frag" "link")`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := dumpBlocks(parse(tc.src)); got != "(p "+tc.want+")" {
				t.Errorf("got %s, want (p %s)", got, tc.want)
			}
		})
	}
}

func TestReferenceLinks(t *testing.T) {
	runCases(t, []blockCase{
		{"full", "[foo][bar]\n\n[bar]: /url \"title\"", `(p (a "/url" (title "title") "foo"))`},
		{"full with markup", "[link [foo [bar]]][ref]\n\n[ref]: /uri", `(p (a "/uri" "link [foo [bar]]"))`},
		{"full with emphasis", "[*foo* bar][Ref]\n\n[ref]: /uri", `(p (a "/uri" (em "foo") " bar"))`},
		{"full across lines", "[foo]\n[bar]\n\n[bar]: /url", `(p "[foo]" (sb) (a "/url" "bar"))`},
		{"collapsed", "[foo][]\n\n[foo]: /url \"title\"", `(p (a "/url" (title "title") "foo"))`},
		{"collapsed with emphasis", "[*foo* bar][]\n\n[*foo* bar]: /url \"title\"", `(p (a "/url" (title "title") (em "foo") " bar"))`},
		{"shortcut", "[foo]\n\n[foo]: /url \"title\"", `(p (a "/url" (title "title") "foo"))`},
		{"shortcut with emphasis", "[*foo* bar]\n\n[*foo* bar]: /url \"title\"", `(p (a "/url" (title "title") (em "foo") " bar"))`},
		{"shortcut followed by text", "[foo] bar\n\n[foo]: /url", `(p (a "/url" "foo") " bar")`},
		{"shortcut followed by brackets", "[foo][bar][baz]\n\n[baz]: /url", `(p "[foo]" (a "/url" "bar"))`},
		{"shortcut then full", "[foo][bar][baz]\n\n[baz]: /url1\n[bar]: /url2", `(p (a "/url2" "foo") (a "/url1" "baz"))`},
		{"undefined", "[foo]", `(p "[foo]")`},
		{"undefined full", "[foo][bar]\n\n[baz]: /url", `(p "[foo][bar]")`},
		{"escaped bracket", "\\[foo]\n\n[foo]: /url", `(p "[foo]")`},
		{"case insensitive", "[ФОО]\n\n[фоо]: /url", `(p (a "/url" "ФОО"))`},
		{"whitespace collapsed", "[Foo\n  bar]: /url\n\n[Baz][Foo bar]", `(p (a "/url" "Baz"))`},
		{"inline wins over reference", "[foo](/inline)\n\n[foo]: /ref", `(p (a "/inline" "foo"))`},
		{"in list item", "- [foo]\n\n[foo]: /url", `(ul tight (li (p (a "/url" "foo"))))`},
		{"image reference", "![foo *bar*]\n\n[foo *bar*]: train.jpg \"train & tracks\"", `(p (img "train.jpg" "foo bar" (title "train & tracks")))`},
		{"image full reference", "![foo][bar]\n\n[bar]: /url", `(p (img "/url" "foo"))`},
		{"image collapsed", "![foo][]\n\n[foo]: /url \"title\"", `(p (img "/url" "foo" (title "title")))`},
	})
}

func TestImages(t *testing.T) {
	runInlineCases(t, []inlineCase{
		{"with title", "![foo](/url \"title\")", `(img "/url" "foo" (title "title"))`},
		{"alt with markup", "![foo *bar*](train.jpg)", `(img "train.jpg" "foo bar")`},
		{"nested image alt", "![foo ![bar](/url)](/url2)", `(img "/url2" "foo bar")`},
		{"nested link alt", "![foo [bar](/url)](/url2)", `(img "/url2" "foo bar")`},
		{"empty alt", "![](/url)", `(img "/url" "")`},
		{"empty destination", "![foo]()", `(img "" "foo")`},
		{"angle destination", "![foo](<url>)", `(img "url" "foo")`},
		{"bang without bracket", "wow! [a](/b)", `"wow! " (a "/b" "a")`},
		{"bang before text", "hi! there", `"hi! there"`},
		{"escaped bang", "\\![a](/b)", `"!" (a "/b" "a")`},
		{"image in text", "a ![i](/p) b", `"a " (img "/p" "i") " b"`},
		{"alt with code", "![a `b` c](/p)", `(img "/p" "a b c")`},
	})
}

func TestAutolinks(t *testing.T) {
	runInlineCases(t, []inlineCase{
		{"http", "<http://foo.bar.baz>", `(a "http://foo.bar.baz" "http://foo.bar.baz")`},
		{"query", "<http://foo.bar.baz/test?q=hello&id=22&boolean>", `(a "http://foo.bar.baz/test?q=hello&id=22&boolean" "http://foo.bar.baz/test?q=hello&id=22&boolean")`},
		{"irc", "<irc://foo.bar:2233/baz>", `(a "irc://foo.bar:2233/baz" "irc://foo.bar:2233/baz")`},
		{"upper case", "<MAILTO:FOO@BAR.BAZ>", `(a "MAILTO:FOO@BAR.BAZ" "MAILTO:FOO@BAR.BAZ")`},
		{"scheme with plus", "<a+b+c:d>", `(a "a+b+c:d" "a+b+c:d")`},
		{"scheme without slashes", "<made-up-scheme://foo,bar>", `(a "made-up-scheme://foo,bar" "made-up-scheme://foo,bar")`},
		{"relative is not", "<http://../>", `(a "http://../" "http://../")`},
		{"email", "<foo@bar.example.com>", `(a "mailto:foo@bar.example.com" "foo@bar.example.com")`},
		{"email with plus", "<foo+special@Bar.baz-bar0.com>", `(a "mailto:foo+special@Bar.baz-bar0.com" "foo+special@Bar.baz-bar0.com")`},
		{"space inside", "<http://foo.bar/baz bim>", `"<http://foo.bar/baz bim>"`},
		{"one letter scheme", "<m:abc>", `"<m:abc>"`},
		{"no scheme", "<foo.bar.baz>", `"<foo.bar.baz>"`},
		{"empty", "<>", `"<>"`},
		{"spaces around", "< http://foo.bar >", `"< " (a "http://foo.bar" "http://foo.bar") " >"`},
		{"backslash email", "<foo\\+@bar.example.com>", `"<foo+@bar.example.com>"`},
		{"bare http", "see http://example.com/a_b, ok", `"see " (a "http://example.com/a_b" "http://example.com/a_b") ", ok"`},
		{"bare https", "https://example.com", `(a "https://example.com" "https://example.com")`},
		{"bare www", "www.example.com", `(a "http://www.example.com" "www.example.com")`},
		{"bare www with path", "Visit www.commonmark.org/help for more information.", `"Visit " (a "http://www.commonmark.org/help" "www.commonmark.org/help") " for more information."`},
		{"bare trailing period", "Visit www.commonmark.org.", `"Visit " (a "http://www.commonmark.org" "www.commonmark.org") "."`},
		{"bare in parens", "(see https://x.org/y)", `"(see " (a "https://x.org/y" "https://x.org/y") ")"`},
		{"bare keeps balanced parens", "https://x.org/a(b)", `(a "https://x.org/a(b)" "https://x.org/a(b)")`},
		{"bare trailing entity", "www.google.com/search?q=commonmark&hl=en&amp;", `(a "http://www.google.com/search?q=commonmark&hl=en" "www.google.com/search?q=commonmark&hl=en") "&"`},
		{"bare in emphasis", "*https://x.org*", `(em (a "https://x.org" "https://x.org"))`},
		{"bare after word is text", "foowww.example.com", `"foowww.example.com"`},
		{"bare scheme only", "http:// alone", `"http:// alone"`},
		{"bare inside link text", "[http://x.com](http://y.com)", `(a "http://y.com" "http://x.com")`},
		{"bare uppercase", "HTTP://EXAMPLE.COM", `(a "HTTP://EXAMPLE.COM" "HTTP://EXAMPLE.COM")`},
		{"bare ends at angle", "http://x.com<b>", `(a "http://x.com" "http://x.com") (raw "<b>")`},
	})
}

func TestEscapesAndEntities(t *testing.T) {
	runInlineCases(t, []inlineCase{
		{"escapable", "\\!\\\"\\#\\$\\%\\&\\'\\(\\)\\*\\+\\,\\-\\.\\/\\:\\;\\<\\=\\>\\?\\@\\[\\\\\\]\\^\\_\\`\\{\\|\\}\\~", `"!\"#$%&'()*+,-./:;<=>?@[\\]^_` + "`" + `{|}~"`},
		{"not escapable", "\\→\\A\\a\\ \\3\\φ\\«", `"\\→\\A\\a\\ \\3\\φ\\«"`},
		{"escaped markup", "\\*not emphasized*\n\\<br/> not a tag\n\\[not a link](/foo)\n\\`not code`", "\"*not emphasized*\" (sb) \"<br/> not a tag\" (sb) \"[not a link](/foo)\" (sb) \"`not code`\""},
		{"escaped backslash", "\\\\*emphasis*", `"\\" (em "emphasis")`},
		{"trailing backslash", "foo\\", `"foo\\"`},
		{"named", "&nbsp; &amp; &copy; &AElig; &Dcaron;\n&frac34; &HilbertSpace; &DifferentialD;\n&ClockwiseContourIntegral; &ngE;", "\"\\u00a0 & © Æ Ď\" (sb) \"¾ ℋ ⅆ\" (sb) \"∲ ≧̸\""},
		{"decimal", "&#35; &#1234; &#992; &#0;", "\"# Ӓ Ϡ \ufffd\""},
		{"hex", "&#X22; &#XD06; &#xcab;", "\"\\\" ആ ಫ\""},
		{"invalid", "&nbsp &x; &#; &#x;\n&#87654321;\n&#abcdef0;\n&ThisIsNotDefined; &hi?;", `"&nbsp &x; &#; &#x;" (sb) "&#87654321;" (sb) "&#abcdef0;" (sb) "&ThisIsNotDefined; &hi?;"`},
		{"no semicolon", "&copy", `"&copy"`},
		{"legacy prefix is not a reference", "&notit; &notin;", "\"&notit; ∉\""},
		{"escaped ampersand", "&amp;copy;", `"&copy;"`},
		{"entities do not make markup", "&#42;foo&#42;", `"*foo*"`},
		{"entity text", "&lt;b&gt;", `"<b>"`},
		{"surrogate", "&#xD800;", "\"\ufffd\""},
		{"lone ampersand", "AT&T & co", `"AT&T & co"`},
	})
}

func TestBreaks(t *testing.T) {
	runInlineCases(t, []inlineCase{
		{"two spaces", "foo  \nbaz", `"foo" (br) "baz"`},
		{"backslash", "foo\\\nbaz", `"foo" (br) "baz"`},
		{"many spaces", "foo       \nbaz", `"foo" (br) "baz"`},
		{"leading spaces on next line", "foo  \n     bar", `"foo" (br) "bar"`},
		{"backslash leading spaces", "foo\\\n     bar", `"foo" (br) "bar"`},
		{"in emphasis", "*foo  \nbar*", `(em "foo" (br) "bar")`},
		{"in emphasis backslash", "*foo\\\nbar*", `(em "foo" (br) "bar")`},
		{"code span", "`code  \nspan`", `(code "code   span")`},
		{"code span backslash", "`code\\\nspan`", "(code \"code\\\\ span\")"},
		{"html backslash", "<a href=\"foo\\\nbar\">", `(raw "<a href=\"foo\\\nbar\">")`},
		{"soft", "foo\nbaz", `"foo" (sb) "baz"`},
		{"soft with trailing space", "foo \nbaz", `"foo" (sb) "baz"`},
		{"soft with leading space", "foo\n baz", `"foo" (sb) "baz"`},
		{"tab before newline is soft", "foo\t\nbar", "\"foo\\t\" (sb) \"bar\""},
		{"trailing spaces trimmed", "foo  ", `"foo"`},
		{"trailing backslash", "foo\\", `"foo\\"`},
		{"hard break at heading end", "foo  \n", `"foo"`},
	})
}

func TestInlineHTML(t *testing.T) {
	runInlineCases(t, []inlineCase{
		{"open tags", "<a><bab><c2c>", `(raw "<a>") (raw "<bab>") (raw "<c2c>")`},
		{"self closing", "<a/><b2/>", `(raw "<a/>") (raw "<b2/>")`},
		{"whitespace in tags", "<a  /><b2\ndata=\"foo\" >", `(raw "<a  />") (raw "<b2\ndata=\"foo\" >")`},
		{"attributes", "<a foo=\"bar\" bam = 'baz <em>\"</em>'\n_boolean zoop:33=zoop:33 />", `(raw "<a foo=\"bar\" bam = 'baz <em>\"</em>'\n_boolean zoop:33=zoop:33 />")`},
		{"custom element", "Foo <responsive-image src=\"foo.jpg\" />", `"Foo " (raw "<responsive-image src=\"foo.jpg\" />")`},
		{"illegal tag name", "<33> <__>", `"<33> <__>"`},
		{"illegal attribute name", "<a h*#ref=\"hi\">", `"<a h*#ref=\"hi\">"`},
		{"illegal attribute value", "<a href=\"hi'> <a href=hi'>", `"<a href=\"hi'> <a href=hi'>"`},
		{"illegal whitespace", "< a><\nfoo><bar/ >\n<foo bar=baz\nbim!bop />", `"< a><" (sb) "foo><bar/ >" (sb) "<foo bar=baz" (sb) "bim!bop />"`},
		{"missing whitespace", "<a href='bar'title=title>", `"<a href='bar'title=title>"`},
		{"closing tags", "</a></foo >", `(raw "</a>") (raw "</foo >")`},
		{"closing tag with attribute", "</a href=\"foo\">", `"</a href=\"foo\">"`},
		{"comment", "foo <!-- this is a\ncomment - with hyphen -->", `"foo " (raw "<!-- this is a\ncomment - with hyphen -->")`},
		{"processing instruction", "foo <?php echo $a; ?>", `"foo " (raw "<?php echo $a; ?>")`},
		{"declaration", "foo <!ELEMENT br EMPTY>", `"foo " (raw "<!ELEMENT br EMPTY>")`},
		{"cdata", "foo <![CDATA[>&<]]>", `"foo " (raw "<![CDATA[>&<]]>")`},
		{"entity kept in attribute", "foo <a href=\"&ouml;\">", `"foo " (raw "<a href=\"&ouml;\">")`},
		{"backslash kept in attribute", "foo <a href=\"\\*\">", `"foo " (raw "<a href=\"\\*\">")`},
		{"unclosed comment", "a <!-- b", `"a <!-- b"`},
		{"markup inside tag is not parsed", "<a title=\"*x*\">*y*", `(raw "<a title=\"*x*\">") (em "y")`},
	})
}

func TestPlainText(t *testing.T) {
	cases := []struct {
		name string
		in   []Inline
		want string
	}{
		{"nil", nil, ""},
		{"text and emphasis", []Inline{Text{S: "a "}, Emph{Children: []Inline{Text{S: "b"}}}, Strong{Children: []Inline{Text{S: "c"}}}, Strike{Children: []Inline{Text{S: "d"}}}}, "a bcd"},
		{"code and link", []Inline{Code{S: "x"}, Text{S: " "}, Link{Dest: "/u", Children: []Inline{Text{S: "l"}}}}, "x l"},
		{"image", []Inline{Image{Dest: "/u", Alt: "alt"}}, "alt"},
		{"breaks and html", []Inline{Text{S: "a"}, SoftBreak{}, Text{S: "b"}, HardBreak{}, HTMLInline{S: "<br>"}, Text{S: "c"}}, "a b\nc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := PlainText(tc.in); got != tc.want {
				t.Errorf("PlainText = %q, want %q", got, tc.want)
			}
		})
	}
	bs := parse("# a *b* `c` [d](/e) ![f](/g)")
	h, ok := bs[0].(Heading)
	if !ok {
		t.Fatalf("got %s", dumpBlocks(bs))
	}
	if got := PlainText(h.Inlines); got != "a b c d f" {
		t.Errorf("PlainText of a parsed heading = %q", got)
	}
}

func TestTextIsMergedAndNotEmpty(t *testing.T) {
	srcs := []string{"a\\*b\\_c &amp; d &lt; e", "a * b * c _ d", "x[y]z", "1 < 2 > 0 & ![]"}
	for _, src := range srcs {
		bs := parse(src)
		p := bs[0].(Paragraph)
		for i, in := range p.Inlines {
			txt, ok := in.(Text)
			if ok && txt.S == "" {
				t.Errorf("%q: empty text at %d", src, i)
			}
			if _, next := in.(Text); next && i > 0 {
				if _, prev := p.Inlines[i-1].(Text); prev {
					t.Errorf("%q: adjacent texts at %d: %s", src, i, dumpInlines(p.Inlines))
				}
			}
		}
	}
}

// --- robustness and bounds ---

func TestRobustnessRandomMarkdownPunctuation(t *testing.T) {
	alphabet := []string{
		"*", "_", "~", "`", "[", "]", "(", ")", "!", "<", ">", "&", ";", "#", "-", "+", "=", "|", ":", "\\", "\"", "'",
		"\n", "\n\n", " ", "  ", "\t", "    ", "> ", "- ", "1. ", "1) ", "```", "~~~", "---", "***", "a", "b", "foo", "0",
		"http://x.y", "www.a.b", "<div>", "</div>", "<!--", "-->", "[x]: /u", "| a |", "|---|", "\r\n", "\r", "\x00", "é", "→",
		"&amp;", "&#35;", "[ ]", "[x]",
	}
	rng := rand.New(rand.NewSource(20260101))
	start := time.Now()
	for i := 0; i < 2000; i++ {
		var sb strings.Builder
		n := rng.Intn(201)
		for sb.Len() < n {
			sb.WriteString(alphabet[rng.Intn(len(alphabet))])
		}
		src := sb.String()
		bs := checkedParse(t, src)
		checkSpans(t, src, bs)
	}
	if d := time.Since(start); d > 20*time.Second {
		t.Errorf("2000 small inputs took %s", d)
	}
}

func TestRobustnessRandomBytes(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for i := 0; i < 500; i++ {
		b := make([]byte, rng.Intn(300))
		for j := range b {
			b[j] = byte(rng.Intn(256))
		}
		checkedParse(t, string(b))
	}
}

// checkedParse calls parse (not Parse, which would hide a panic) and returns the blocks.
func checkedParse(t *testing.T, src string) (bs []Block) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("parse(%q) panicked: %v", src, r)
		}
	}()
	return parse(src)
}

// checkSpans verifies that the spans of the blocks are inside the source and not inverted.
func checkSpans(t *testing.T, src string, bs []Block) {
	t.Helper()
	n := len(splitLines(src))
	var walk func(bs []Block, lo, hi int)
	walk = func(bs []Block, lo, hi int) {
		prevEnd := lo
		for _, b := range bs {
			s := b.Span()
			if s.StartLine < prevEnd || s.EndLine <= s.StartLine || s.EndLine > hi {
				t.Fatalf("src %q: bad span %+v of %T (previous end %d, limit %d)", src, s, b, prevEnd, hi)
			}
			prevEnd = s.EndLine
			switch v := b.(type) {
			case Quote:
				walk(v.Blocks, s.StartLine, s.EndLine)
			case List:
				itemEnd := s.StartLine
				for _, it := range v.Items {
					if it.Pos.StartLine < itemEnd || it.Pos.EndLine <= it.Pos.StartLine || it.Pos.EndLine > s.EndLine {
						t.Fatalf("src %q: bad item span %+v in %+v", src, it.Pos, s)
					}
					itemEnd = it.Pos.EndLine
					walk(it.Blocks, it.Pos.StartLine, it.Pos.EndLine)
				}
			}
		}
	}
	walk(bs, 0, n)
}

func TestLargeInputsAreFast(t *testing.T) {
	line := "Some *emphasis*, a `code span`, a [link](http://example.com/a(b)) and **strong** text with snake_case.\n"
	cases := []struct {
		name string
		src  string
	}{
		{"20k lines of paragraph", strings.Repeat(line, 20000)},
		{"20k paragraphs", strings.Repeat("para *x* one\n\n", 20000)},
		{"20k list items", strings.Repeat("- item *x* [l](/u)\n", 20000)},
		{"20k nested list lines", strings.Repeat("  - a\n", 20000)},
		{"20k quote lines", strings.Repeat("> quoted `x`\n", 20000)},
		{"20k table rows", "| a | b |\n|---|---|\n" + strings.Repeat("| 1 | *2* |\n", 20000)},
		{"20k code lines", "```\n" + strings.Repeat("code line\n", 20000) + "```\n"},
		{"20k refs", strings.Repeat("[a]: /x\n", 20000) + "[a]"},
		{"5k nested emphasis delimiters", strings.Repeat("*a ", 5000) + strings.Repeat("a* ", 5000)},
		{"5k nested emphasis underscores", strings.Repeat("_a ", 5000) + strings.Repeat("a_ ", 5000)},
		{"100k stars", strings.Repeat("*", 100000)},
		{"50k star pairs", strings.Repeat("**a*", 50000)},
		{"mixed delimiters", strings.Repeat("*_~~a", 20000)},
		{"20k open brackets", strings.Repeat("[", 20000)},
		{"20k bracket pairs", strings.Repeat("[a]", 20000)},
		{"20k links", strings.Repeat("[a](b) ", 20000)},
		{"20k open parens in links", strings.Repeat("[a](b(", 20000)},
		{"20k unclosed titles", strings.Repeat("[a](b \"c ", 20000)},
		{"20k unclosed backticks of growing length", func() string {
			var sb strings.Builder
			for i := 1; i < 600; i++ {
				sb.WriteString(strings.Repeat("`", i))
				sb.WriteString("a ")
			}
			return sb.String()
		}()},
		{"50k backticks single", strings.Repeat("` ", 50000)},
		{"20k unclosed comments", strings.Repeat("<!-- ", 20000)},
		{"20k unclosed tags", strings.Repeat("<a b='", 20000)},
		{"20k unclosed tags with spaces", strings.Repeat("<a b=\"x\" c=\"", 5000)},
		{"deep quotes", strings.Repeat("> ", 20000) + "x"},
		{"deep lists", strings.Repeat("- ", 20000) + "x"},
		{"many bare urls", strings.Repeat("www.a.com/x) ", 20000)},
		{"long url trailing parens", "www.a.com/" + strings.Repeat(")", 100000)},
		{"long backslash run", strings.Repeat("\\", 100000)},
		{"entities", strings.Repeat("&amp;&#35;&bogus;", 20000)},
		{"setext candidates", strings.Repeat("a\n", 20000) + "---\n"},
		{"pipes", strings.Repeat("| a ", 50000) + "\n" + strings.Repeat("|---", 50000)},
		{"tabs", strings.Repeat("\t", 100000)},
		{"html lines", strings.Repeat("<div>\n", 20000)},
		{"fences", strings.Repeat("```\n", 20000)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Now()
			bs := checkedParse(t, tc.src)
			if d := time.Since(start); d > 3*time.Second {
				t.Errorf("parsing %d bytes took %s", len(tc.src), d)
			}
			_ = bs
		})
	}
}

func TestLargeDocumentStructure(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 5000; i++ {
		sb.WriteString("# Heading\n\nText with *emphasis* and `code`.\n\n- a\n- b\n\n")
	}
	bs := checkedParse(t, sb.String())
	if len(bs) != 5000*3 {
		t.Errorf("got %d blocks, want %d", len(bs), 5000*3)
	}
	checkSpans(t, sb.String(), bs)
}

func TestParseNeverPanics(t *testing.T) {
	for _, src := range []string{"", "\x00", "\xff\xfe", "[", "![", "]", "<", "`", "&", "&#", "&#x", "\\", "> ", "- ", "1.", "|", "|-", "~~", "\t", "\r", "[\x00]: /x", "<a", "<!--", "www.", "http://"} {
		checkedParse(t, src)
		_ = Parse(src)
	}
}
