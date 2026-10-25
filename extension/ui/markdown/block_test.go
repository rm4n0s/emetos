package markdown

import (
	"strings"
	"testing"
)

type blockCase = struct{ name, src, want string }

func TestHeadings(t *testing.T) {
	runCases(t, []blockCase{
		{"atx", "# Title", `(h1 "Title")`},
		{"atx levels", "# a\n## b\n### c\n#### d\n##### e\n###### f", `(h1 "a") (h2 "b") (h3 "c") (h4 "d") (h5 "e") (h6 "f")`},
		{"atx seven is text", "####### foo", `(p "####### foo")`},
		{"atx needs space", "#5 bolt\n\n#hashtag", `(p "#5 bolt") (p "#hashtag")`},
		{"atx escaped hash", "\\## foo", `(p "## foo")`},
		{"atx inline", "# foo *bar* \\*baz\\*", `(h1 "foo " (em "bar") " *baz*")`},
		{"atx leading and trailing space", "#                  foo                     ", `(h1 "foo")`},
		{"atx indented up to three", "   # foo", `(h1 "foo")`},
		{"atx indented four is code", "    # foo", `(indent "# foo")`},
		{"atx closing sequence", "## foo ##\n  ###   bar    ###", `(h2 "foo") (h3 "bar")`},
		{"atx closing longer", "# foo ##################################\n##### foo ##", `(h1 "foo") (h5 "foo")`},
		{"atx closing with trailing spaces", "### foo ###     ", `(h3 "foo")`},
		{"atx closing not preceded by space", "### foo ### b\n\n# foo#", `(h3 "foo ### b") (h1 "foo#")`},
		{"atx escaped closing", "### foo \\###\n## foo #\\##\n# foo \\#", `(h3 "foo ###") (h2 "foo ###") (h1 "foo #")`},
		{"atx between content", "****\n## foo\n****", `(hr) (h2 "foo") (hr)`},
		{"atx interrupts paragraph", "Foo bar\n# baz\nBar foo", `(p "Foo bar") (h1 "baz") (p "Bar foo")`},
		{"atx empty", "## \n#\n### ###", `(h2) (h1) (h3)`},
		{"setext h1", "Foo *bar*\n=========", `(h1 "Foo " (em "bar"))`},
		{"setext h2", "Foo *bar*\n---------", `(h2 "Foo " (em "bar"))`},
		{"setext multiline", "Foo *bar\nbaz*\n====", `(h1 "Foo " (em "bar" (sb) "baz"))`},
		{"setext any length", "Foo\n-------------------------\n\nFoo\n=", `(h2 "Foo") (h1 "Foo")`},
		{"setext indented", "   Foo\n---\n\n  Foo\n-----\n\n  Foo\n  ===", `(h2 "Foo") (h2 "Foo") (h1 "Foo")`},
		{"setext underline indented four is text", "Foo\n    ---", `(p "Foo" (sb) "---")`},
		{"setext trailing spaces", "Foo\n= =\n\nFoo\n--- -", `(p "Foo" (sb) "= =") (p "Foo") (hr)`},
		{"setext underline with trailing space", "Foo  \n-----", `(h2 "Foo")`},
		{"setext not lazy", "> foo\nbar\n===", `(quote (p "foo" (sb) "bar" (sb) "==="))`},
		{"setext after quote is hr", "> foo\n---", `(quote (p "foo")) (hr)`},
		{"setext vs list", "- foo\n-----", `(ul tight (li (p "foo"))) (hr)`},
		{"setext vs hr after blank", "Foo\n\n---", `(p "Foo") (hr)`},
		{"setext two paragraph lines", "Foo\nBar\n---", `(h2 "Foo" (sb) "Bar")`},
		{"setext then text", "---\nFoo\n---\nBar\n---\nBaz", `(hr) (h2 "Foo") (h2 "Bar") (p "Baz")`},
		{"setext without content is hr", "\n====", `(p "====")`},
		{"setext hr pair", "---\n---", `(hr) (hr)`},
	})
}

func TestRulesAndParagraphs(t *testing.T) {
	runCases(t, []blockCase{
		{"rules", "***\n---\n___", `(hr) (hr) (hr)`},
		{"plus is not a rule", "+++", `(p "+++")`},
		{"equals is not a rule", "===", `(p "===")`},
		{"two is not enough", "--\n**\n__", `(p "--" (sb) "**" (sb) "__")`},
		{"rule indented", " ***\n  ***\n   ***", `(hr) (hr) (hr)`},
		{"rule indented four", "    ***", `(indent "***")`},
		{"rule long", "_____________________________________", `(hr)`},
		{"rule spaced", " - - -", `(hr)`},
		{"rule spaced mix", " **  * ** * ** * **", `(hr)`},
		{"rule trailing spaces", "- - - -    ", `(hr)`},
		{"rule not with other chars", "_ _ _ _ a\n\na------\n\n---a---", `(p "_ _ _ _ a") (p "a------") (p "---a---")`},
		{"rule mixed chars", " *-*", `(p (em "-"))`},
		{"rule between paragraphs", "Foo\n***\nbar", `(p "Foo") (hr) (p "bar")`},
		{"rule between lists", "- foo\n***\n- bar", `(ul tight (li (p "foo"))) (hr) (ul tight (li (p "bar")))`},
		{"rule in list item", "* Foo\n* * *\n* Bar", `(ul tight (li (p "Foo"))) (hr) (ul tight (li (p "Bar")))`},
		{"paragraphs", "aaa\n\nbbb", `(p "aaa") (p "bbb")`},
		{"paragraph lines", "aaa\nbbb\n\nccc\nddd", `(p "aaa" (sb) "bbb") (p "ccc" (sb) "ddd")`},
		{"multiple blank lines", "aaa\n\n\nbbb", `(p "aaa") (p "bbb")`},
		{"leading spaces skipped", "  aaa\n bbb", `(p "aaa" (sb) "bbb")`},
		{"continuation indented", "aaa\n             bbb\n                                       ccc", `(p "aaa" (sb) "bbb" (sb) "ccc")`},
		{"first line indented three", "   aaa\nbbb", `(p "aaa" (sb) "bbb")`},
		{"trailing spaces", "aaa     \nbbb     ", `(p "aaa" (br) "bbb")`},
		{"empty", "", ``},
		{"blank only", "  \n\n \t\n", ``},
		{"blank lines around", "  \n\naaa\n  \n\n# aaa\n\n  ", `(p "aaa") (h1 "aaa")`},
	})
}

func TestCodeBlocks(t *testing.T) {
	runCases(t, []blockCase{
		{"indented", "    a simple\n      indented code block", `(indent "a simple\n  indented code block")`},
		{"indented is not a list continuation", "  - foo\n\n    bar", `(ul loose (li (p "foo") (p "bar")))`},
		{"indented in list", "1.  foo\n\n    - bar", `(ol 1 loose (li (p "foo") (ul tight (li (p "bar")))))`},
		{"indented html is text", "    <a/>\n    *hi*\n\n    - one", `(indent "<a/>\n*hi*\n\n- one")`},
		{"indented chunks", "    chunk1\n\n    chunk2\n  \n \n \n    chunk3", `(indent "chunk1\n\nchunk2\n\n\n\nchunk3")`},
		{"indented with extra spaces", "    chunk1\n      \n      chunk2", `(indent "chunk1\n  \n  chunk2")`},
		{"indented cannot interrupt paragraph", "Foo\n    bar", `(p "Foo" (sb) "bar")`},
		{"indented then paragraph", "    foo\nbar", `(indent "foo") (p "bar")`},
		{"indented between headings", "# Heading\n    foo\nHeading\n------\n    foo\n----", `(h1 "Heading") (indent "foo") (h2 "Heading") (indent "foo") (hr)`},
		{"indented first line extra", "        foo\n    bar", `(indent "    foo\nbar")`},
		{"indented after blank lines", "\n    \n    foo\n    ", `(indent "foo")`},
		{"indented trailing spaces kept", "    foo  ", `(indent "foo  ")`},
		{"indented tab", "\tfoo\tbaz\t\tbim", `(indent "foo\tbaz\t\tbim")`},
		{"indented spaces then tab", "  \tfoo\tbaz\t\tbim", `(indent "foo\tbaz\t\tbim")`},
		{"indented four spaces then tab", "    a\ta\n    ὐ\ta", `(indent "a\ta\nὐ\ta")`},
		{"tab in list item", "  - foo\n\n\tbar", `(ul loose (li (p "foo") (p "bar")))`},
		{"two tabs in list item", "- foo\n\n\t\tbar", `(ul loose (li (p "foo") (indent "  bar")))`},
		{"tab after quote marker", ">\t\tfoo", `(quote (indent "  foo"))`},
		{"tab after list marker", "-\t\tfoo", `(ul tight (li (indent "  foo")))`},
		{"fenced", "```\n<\n >\n```", `(fence "" "<\n >")`},
		{"fenced tildes", "~~~\n<\n >\n~~~", `(fence "" "<\n >")`},
		{"fenced two backticks is code span", "``\nfoo\n``", `(p (code "foo"))`},
		{"fenced different chars", "```\naaa\n~~~\n```", `(fence "" "aaa\n~~~")`},
		{"fenced tilde different", "~~~\naaa\n```\n~~~", "(fence \"\" \"aaa\\n```\")"},
		{"fenced longer closing", "````\naaa\n```\n``````", "(fence \"\" \"aaa\\n```\")"},
		{"fenced tildes longer", "~~~~\naaa\n~~~\n~~~~", `(fence "" "aaa\n~~~")`},
		{"fenced unclosed", "```", `(fence "" "")`},
		{"fenced unclosed content", "`````\n\n```\naaa", "(fence \"\" \"\\n```\\naaa\")"},
		{"fenced in quote unclosed", "> ```\n> aaa\n\nbbb", `(quote (fence "" "aaa")) (p "bbb")`},
		{"fenced blank lines", "```\n\n  \n```", `(fence "" "\n  ")`},
		{"fenced empty", "```\n```", `(fence "" "")`},
		{"fenced indented opening", " ```\n aaa\naaa\n```", `(fence "" "aaa\naaa")`},
		{"fenced indented two", "  ```\naaa\n  aaa\naaa\n  ```", `(fence "" "aaa\naaa\naaa")`},
		{"fenced indented three", "   ```\n   aaa\n    aaa\n  aaa\n   ```", `(fence "" "aaa\n aaa\naaa")`},
		{"fenced indented four is code", "    ```\n    aaa\n    ```", "(indent \"```\\naaa\\n```\")"},
		{"fenced closing indented", "```\naaa\n  ```", `(fence "" "aaa")`},
		{"fenced opening indented closing", "   ```\naaa\n  ```", `(fence "" "aaa")`},
		{"fenced closing indented four", "```\naaa\n    ```", "(fence \"\" \"aaa\\n    ```\")"},
		{"fenced closing with text", "```\naaa\n``` aaa", "(fence \"\" \"aaa\\n``` aaa\")"},
		{"fenced inline backticks", "``` ```\naaa", `(p (code " ") (sb) "aaa")`},
		{"fenced tildes with backticks in info", "~~~~~~\naaa\n~~~ ~~", `(fence "" "aaa\n~~~ ~~")`},
		{"fenced interrupts paragraph", "foo\n```\nbar\n```\nbaz", `(p "foo") (fence "" "bar") (p "baz")`},
		{"fenced between headings", "foo\n---\n~~~\nbar\n~~~\n# baz", `(h2 "foo") (fence "" "bar") (h1 "baz")`},
		{"fenced info", "```ruby\ndef foo(x)\n  return 3\nend\n```", `(fence "ruby" "def foo(x)\n  return 3\nend")`},
		{"fenced info extra words", "~~~~    ruby startline=3 $%@#$\ndef foo(x)\n  return 3\nend\n~~~~~~~", `(fence "ruby" "def foo(x)\n  return 3\nend")`},
		{"fenced info braces", "````;\n````", `(fence ";" "")`},
		{"fenced backtick info with backtick", "``` aa ```\nfoo", `(p (code "aa") (sb) "foo")`},
		{"fenced tilde info with backticks", "~~~ aa ``` ~~~\nfoo\n~~~", `(fence "aa" "foo")`},
		{"fenced closing no info", "```\n``` aaa\n```", "(fence \"\" \"``` aaa\")"},
		{"fenced info escapes", "``` f\\*o&amp;o\nx\n```", `(fence "f*o&o" "x")`},
		{"fenced in list", "- a\n  ```\n  b\n\n\n  ```\n- c", `(ul tight (li (p "a") (fence "" "b\n\n")) (li (p "c")))`},
		{"fenced crlf", "```go\r\nx := 1\r\n```\r\n", `(fence "go" "x := 1")`},
	})
}

func TestQuotes(t *testing.T) {
	runCases(t, []blockCase{
		{"simple", "> # Foo\n> bar\n> baz", `(quote (h1 "Foo") (p "bar" (sb) "baz"))`},
		{"no space after marker", "># Foo\n>bar\n> baz", `(quote (h1 "Foo") (p "bar" (sb) "baz"))`},
		{"indented marker", "   > # Foo\n   > bar\n > baz", `(quote (h1 "Foo") (p "bar" (sb) "baz"))`},
		{"indented four is code", "    > # Foo\n    > bar\n    > baz", `(indent "> # Foo\n> bar\n> baz")`},
		{"lazy", "> # Foo\n> bar\nbaz", `(quote (h1 "Foo") (p "bar" (sb) "baz"))`},
		{"lazy mixed", "> bar\nbaz\n> foo", `(quote (p "bar" (sb) "baz" (sb) "foo"))`},
		{"hr is not lazy", "> foo\n---", `(quote (p "foo")) (hr)`},
		{"list is not lazy", "> - foo\n- bar", `(quote (ul tight (li (p "foo")))) (ul tight (li (p "bar")))`},
		{"code is not lazy", ">     foo\n    bar", `(quote (indent "foo")) (indent "bar")`},
		{"fence is not lazy", "> ```\nfoo\n```", `(quote (fence "" "")) (p "foo") (fence "" "")`},
		{"lazy not indented code", "> foo\n    - bar", `(quote (p "foo" (sb) "- bar"))`},
		{"empty", ">", `(quote)`},
		{"empty lines", ">\n>  \n> ", `(quote)`},
		{"leading blank lines", ">\n> foo\n>  ", `(quote (p "foo"))`},
		{"separated by blank", "> foo\n\n> bar", `(quote (p "foo")) (quote (p "bar"))`},
		{"two paragraphs", "> foo\n>\n> bar", `(quote (p "foo") (p "bar"))`},
		{"consecutive", "> foo\n> bar", `(quote (p "foo" (sb) "bar"))`},
		{"interrupts paragraph", "foo\n> bar", `(p "foo") (quote (p "bar"))`},
		{"around blocks", "> aaa\n***\n> bbb", `(quote (p "aaa")) (hr) (quote (p "bbb"))`},
		{"blank ends lazy", "> bar\n\nbaz", `(quote (p "bar")) (p "baz")`},
		{"nested", "> > > foo\nbar", `(quote (quote (quote (p "foo" (sb) "bar"))))`},
		{"nested partly lazy", ">>> foo\n> bar\n>>baz", `(quote (quote (quote (p "foo" (sb) "bar" (sb) "baz"))))`},
		{"code in quote", ">     code\n\n>    not code", `(quote (indent "code")) (quote (p "not code"))`},
		{"list in quote", "> - a\n> - b", `(quote (ul tight (li (p "a")) (li (p "b"))))`},
		{"quote in list", "- > a\n  > b", `(ul tight (li (quote (p "a" (sb) "b"))))`},
		{"quote with fence", "> ```go\n> x\n> ```", `(quote (fence "go" "x"))`},
	})
}

func TestLists(t *testing.T) {
	runCases(t, []blockCase{
		{"bullets", "- foo\n- bar\n- baz", `(ul tight (li (p "foo")) (li (p "bar")) (li (p "baz")))`},
		{"changing bullet starts a list", "- foo\n- bar\n+ baz", `(ul tight (li (p "foo")) (li (p "bar"))) (ul tight (li (p "baz")))`},
		{"star plus", "* a\n+ b\n- c", `(ul tight (li (p "a"))) (ul tight (li (p "b"))) (ul tight (li (p "c")))`},
		{"ordered", "1. foo\n2. bar\n3) baz", `(ol 1 tight (li (p "foo")) (li (p "bar"))) (ol 3 tight (li (p "baz")))`},
		{"ordered start", "123. a\n124. b", `(ol 123 tight (li (p "a")) (li (p "b")))`},
		{"ordered start zero", "0. ok", `(ol 0 tight (li (p "ok")))`},
		{"ordered ten digits is text", "1234567890. not ok", `(p "1234567890. not ok")`},
		{"ordered nine digits", "999999999. ok", `(ol 999999999 tight (li (p "ok")))`},
		{"ordered start 1 interrupts paragraph", "The number of windows in my house is\n14.  The number of doors is 6.", `(p "The number of windows in my house is" (sb) "14.  The number of doors is 6.")`},
		{"ordered one interrupts paragraph", "The number of windows in my house is\n1.  The number of doors is 6.", `(p "The number of windows in my house is") (ol 1 tight (li (p "The number of doors is 6.")))`},
		{"bullet interrupts paragraph", "Foo\n- bar\n- baz", `(p "Foo") (ul tight (li (p "bar")) (li (p "baz")))`},
		{"empty item cannot interrupt paragraph", "foo\n*\n\nfoo\n1.", `(p "foo" (sb) "*") (p "foo" (sb) "1.")`},
		{"marker without space is text", "-foo\n\n1.foo", `(p "-foo") (p "1.foo")`},
		{"loose", "- foo\n\n- bar", `(ul loose (li (p "foo")) (li (p "bar")))`},
		{"loose later items", "- a\n- b\n\n- c", `(ul loose (li (p "a")) (li (p "b")) (li (p "c")))`},
		{"loose inside item", "- a\n- b\n\n  c\n- d", `(ul loose (li (p "a")) (li (p "b") (p "c")) (li (p "d")))`},
		{"loose with empty item", "* a\n*\n\n* c", `(ul loose (li (p "a")) (li) (li (p "c")))`},
		{"tight with trailing blank lines", "- a\n- b\n\n", `(ul tight (li (p "a")) (li (p "b")))`},
		{"two lists separated by comment-like paragraph", "- foo\n- bar\n\nbaz", `(ul tight (li (p "foo")) (li (p "bar"))) (p "baz")`},
		{"nested tight", "- a\n  - b\n  - c\n- d", `(ul tight (li (p "a") (ul tight (li (p "b")) (li (p "c")))) (li (p "d")))`},
		{"nested inner loose outer tight", "- a\n  - b\n\n    c\n- d", `(ul tight (li (p "a") (ul loose (li (p "b") (p "c")))) (li (p "d")))`},
		{"nested outer loose inner tight", "- a\n  - b\n  - c\n\n- d\n  - e\n  - f", `(ul loose (li (p "a") (ul tight (li (p "b")) (li (p "c")))) (li (p "d") (ul tight (li (p "e")) (li (p "f")))))`},
		{"nested ordered", "1. a\n   1. b\n   2. c\n2. d", `(ol 1 tight (li (p "a") (ol 1 tight (li (p "b")) (li (p "c")))) (li (p "d")))`},
		{"nested by two spaces", "- a\n - b\n  - c\n   - d\n    - e", `(ul tight (li (p "a")) (li (p "b")) (li (p "c")) (li (p "d" (sb) "- e")))`},
		{"deeper nesting", "- a\n  - b\n    - c\n      - d", `(ul tight (li (p "a") (ul tight (li (p "b") (ul tight (li (p "c") (ul tight (li (p "d")))))))))`},
		{"sibling after nested", "- a\n    - b\n- c", `(ul tight (li (p "a") (ul tight (li (p "b")))) (li (p "c")))`},
		{"item with blocks", "1.  A paragraph\n    with two lines.\n\n        indented code\n\n    > A block quote.", `(ol 1 loose (li (p "A paragraph" (sb) "with two lines.") (indent "indented code") (quote (p "A block quote."))))`},
		{"item content indentation", "- one\n\n two", `(ul tight (li (p "one"))) (p "two")`},
		{"item content indentation ok", "- one\n\n  two", `(ul loose (li (p "one") (p "two")))`},
		{"item wide marker", "-    one\n\n     two", `(ul loose (li (p "one") (p "two")))`},
		{"item code after wide spaces", "-     code", `(ul tight (li (indent "code")))`},
		{"item blank first line", "-\n  foo\n-\n  ```\n  bar\n  ```\n-\n      baz", `(ul tight (li (p "foo")) (li (fence "" "bar")) (li (indent "baz")))`},
		{"item empty then paragraph", "-\n\n  foo", `(ul tight (li)) (p "foo")`},
		{"item empty in middle", "- foo\n-\n- bar", `(ul tight (li (p "foo")) (li) (li (p "bar")))`},
		{"item empty with spaces", "- foo\n-   \n- bar", `(ul tight (li (p "foo")) (li) (li (p "bar")))`},
		{"item two blank lines end list", "- foo\n- bar\n\n\n- baz", `(ul loose (li (p "foo")) (li (p "bar")) (li (p "baz")))`},
		{"item indent variations", "1. a\n\n  2. b\n\n   3. c", `(ol 1 loose (li (p "a")) (li (p "b")) (li (p "c")))`},
		{"item indent four becomes code", "1. a\n\n  2. b\n\n    3. c", `(ol 1 loose (li (p "a")) (li (p "b"))) (indent "3. c")`},
		{"item fence", "- a\n  ```\n  b\n  ```", `(ul tight (li (p "a") (fence "" "b")))`},
		{"item lazy", "- a\nb\n- c", `(ul tight (li (p "a" (sb) "b")) (li (p "c")))`},
		{"item with heading", "- # Foo\n- Bar\n  ---\n  baz", `(ul tight (li (h1 "Foo")) (li (h2 "Bar") (p "baz")))`},
		{"item start of ordered keeps the number", "10) foo\n    - bar", `(ol 10 tight (li (p "foo") (ul tight (li (p "bar")))))`},
		{"different delimiters", "1. a\n2) b", `(ol 1 tight (li (p "a"))) (ol 2 tight (li (p "b")))`},
		{"list in list in quote", "> 1. a\n>    - b", `(quote (ol 1 tight (li (p "a") (ul tight (li (p "b"))))))`},
	})
	// The two bounded cases only check that the nesting stops: no panic, and a tree of limited depth.
	for _, src := range []string{strings.Repeat(">", 200) + " x", strings.Repeat("- ", 200) + "x"} {
		if d := blockDepth(parse(src)); d > maxDepth+2 {
			t.Errorf("nesting depth %d is not bounded", d)
		}
	}
}

func blockDepth(bs []Block) int {
	best := 0
	for _, b := range bs {
		d := 1
		switch v := b.(type) {
		case Quote:
			d += blockDepth(v.Blocks)
		case List:
			m := 0
			for _, it := range v.Items {
				m = max(m, blockDepth(it.Blocks))
			}
			d += m
		}
		best = max(best, d)
	}
	return best
}

func TestTaskItems(t *testing.T) {
	runCases(t, []blockCase{
		{"tasks", "- [ ] todo\n- [x] done\n- [X] Done", `(ul tight (li [ ] (p "todo")) (li [x] (p "done")) (li [x] (p "Done")))`},
		{"no space after marker", "- [x]no", `(ul tight (li (p "[x]no")))`},
		{"other letter", "- [y] no", `(ul tight (li (p "[y] no")))`},
		{"ordered task", "1. [x] a\n2. [ ] b", `(ol 1 tight (li [x] (p "a")) (li [ ] (p "b")))`},
		{"task with inline", "- [ ] a *b* `c`", `(ul tight (li [ ] (p "a " (em "b") " " (code "c"))))`},
		{"task with more lines", "- [ ] a\n  b", `(ul tight (li [ ] (p "a" (sb) "b")))`},
		{"task with child list", "- [x] a\n  - [ ] b", `(ul tight (li [x] (p "a") (ul tight (li [ ] (p "b")))))`},
		{"not first block", "- a\n\n  [ ] b", `(ul loose (li (p "a") (p "[ ] b")))`},
		{"empty task is text", "- [ ]", `(ul tight (li (p "[ ]")))`},
		{"mixed", "- [ ] a\n- b", `(ul tight (li [ ] (p "a")) (li (p "b")))`},
		{"link ref is no task", "- [x]: /u\n\n[x]", `(ul tight (li)) (p (a "/u" "x"))`},
	})
}

func TestHTMLBlocks(t *testing.T) {
	runCases(t, []blockCase{
		{"div", "<div>\n*foo*\n</div>", `(html "<div>\n*foo*\n</div>")`},
		{"div ends at blank", "<div>\n\nfoo", `(html "<div>") (p "foo")`},
		{"details", "<details>\n<summary>x</summary>\n\nbody\n</details>", `(html "<details>\n<summary>x</summary>") (p "body") (html "</details>")`},
		{"indented up to three", "  <div>\n    foo", `(html "  <div>\n    foo")`},
		{"indented four is code", "    <div>\n    foo", `(indent "<div>\nfoo")`},
		{"table tag", "<table><tr><td>\n<pre>\n**Hello**,\n\n_world_.\n</pre>\n</td></tr></table>", `(html "<table><tr><td>\n<pre>\n**Hello**,") (p (em "world") "." (sb) (raw "</pre>")) (html "</td></tr></table>")`},
		{"comment", "<!-- a\n\nb -->\nc", `(html "<!-- a\n\nb -->") (p "c")`},
		{"comment one line", "<!-- x -->\nfoo", `(html "<!-- x -->") (p "foo")`},
		{"pre keeps blank lines", "<pre>\na\n\nb</pre>\nc", `(html "<pre>\na\n\nb</pre>") (p "c")`},
		{"script", "<script>\nfoo\n\nbar\n</script>\nokay", `(html "<script>\nfoo\n\nbar\n</script>") (p "okay")`},
		{"style", "<style type=\"text/css\">\nh1 {color:red;}\n\np {color:blue;}\n</style>\nokay", `(html "<style type=\"text/css\">\nh1 {color:red;}\n\np {color:blue;}\n</style>") (p "okay")`},
		{"processing instruction", "<?php\n\n  echo '>';\n\n?>\nokay", `(html "<?php\n\n  echo '>';\n\n?>") (p "okay")`},
		{"declaration", "<!DOCTYPE html>", `(html "<!DOCTYPE html>")`},
		{"cdata", "<![CDATA[\nfunction matchwo(a,b)\n{\n  return a < b;\n}\n]]>\nokay", `(html "<![CDATA[\nfunction matchwo(a,b)\n{\n  return a < b;\n}\n]]>") (p "okay")`},
		{"hr tag", "<hr/>\nfoo", `(html "<hr/>\nfoo")`},
		{"closing tag kind 6", "</div>\n*foo*", `(html "</div>\n*foo*")`},
		{"complete tag kind 7", "<a href=\"foo\">\n*bar*\n</a>", `(html "<a href=\"foo\">\n*bar*\n</a>")`},
		{"kind 7 close tag", "</ins>\n*bar*", `(html "</ins>\n*bar*")`},
		{"kind 7 needs whole line", "<del>*foo*</del>", `(p (raw "<del>") (em "foo") (raw "</del>"))`},
		{"inline tag line", "<span>foo</span>", `(p (raw "<span>") "foo" (raw "</span>"))`},
		{"kind 7 cannot interrupt paragraph", "Foo\n<a href=\"bar\">\nbaz", `(p "Foo" (sb) (raw "<a href=\"bar\">") (sb) "baz")`},
		{"kind 6 interrupts paragraph", "Foo\n<div>\nbar\n</div>", `(p "Foo") (html "<div>\nbar\n</div>")`},
		{"kind 6 can end paragraph and keep text", "<div>\nbar\n</div>\n*foo*", `(html "<div>\nbar\n</div>\n*foo*")`},
		{"not a tag", "<33>\n\n<__>", `(p "<33>") (p "<__>")`},
		{"in list item", "- <div>\n  x\n\n  y", `(ul loose (li (html "<div>\nx") (p "y")))`},
		{"in quote", "> <div>\n> x", `(quote (html "<div>\nx"))`},
		{"comment closing on first line", "<!-- x --> text\nnext", `(html "<!-- x --> text") (p "next")`},
	})
}

func TestLinkReferenceDefinitions(t *testing.T) {
	runCases(t, []blockCase{
		{"basic", "[foo]: /url \"title\"\n\n[foo]", `(p (a "/url" (title "title") "foo"))`},
		{"spaces and newlines", "   [foo]: \n      /url  \n           'the title'  \n\n[foo]", `(p (a "/url" (title "the title") "foo"))`},
		{"title on the next line", "[foo]: /url\n\"title\"\n\n[foo]", `(p (a "/url" (title "title") "foo"))`},
		{"no title", "[foo]: /url\n\n[foo]", `(p (a "/url" "foo"))`},
		{"text after title is not a definition", "[foo]: /url \"title\" ok", `(p "[foo]: /url \"title\" ok")`},
		{"title on next line then text", "[foo]: /url\n\"title\" ok", `(p "\"title\" ok")`},
		{"definition then paragraph", "[foo]: /url\nbar", `(p "bar")`},
		{"definition indented four is code", "    [foo]: /url \"title\"\n\n[foo]", `(indent "[foo]: /url \"title\"") (p "[foo]")`},
		{"in fence is code", "```\n[foo]: /url\n```\n\n[foo]", `(fence "" "[foo]: /url") (p "[foo]")`},
		{"cannot interrupt paragraph", "Foo\n[bar]: /baz\n\n[bar]", `(p "Foo" (sb) "[bar]: /baz") (p "[bar]")`},
		{"angle destination", "[foo]: <>\n\n[foo]", `(p (a "" "foo"))`},
		{"angle with spaces", "[foo]: </my url>\n\n[foo]", `(p (a "/my url" "foo"))`},
		{"missing destination", "[foo]:\n\n[foo]", `(p "[foo]:") (p "[foo]")`},
		{"case insensitive", "[FOO]: /url\n\n[Foo]", `(p (a "/url" "Foo"))`},
		{"unicode case", "[ΑΓΩ]: /φου\n\n[αγω]", `(p (a "/φου" "αγω"))`},
		{"whitespace collapsed", "[Foo\n  bar]: /url\n\n[Baz][Foo bar]", `(p (a "/url" "Baz"))`},
		{"first wins", "[foo]: /url1\n[foo]: /url2\n\n[foo]", `(p (a "/url1" "foo"))`},
		{"defined after use", "[foo]\n\n[foo]: /url", `(p (a "/url" "foo"))`},
		{"several in a row", "[foo]: /foo-url \"foo\"\n[bar]: /bar-url\n  \"bar\"\n[baz]: /baz-url\n\n[foo],\n[bar],\n[baz]", `(p (a "/foo-url" (title "foo") "foo") "," (sb) (a "/bar-url" (title "bar") "bar") "," (sb) (a "/baz-url" "baz"))`},
		{"in quote", "[foo]\n\n> [foo]: /url", `(p (a "/url" "foo")) (quote)`},
		{"only definitions", "[foo]: /url", ``},
		{"definition with setext underline is hr", "[foo]: /url\n===\n[foo]", `(p "===" (sb) (a "/url" "foo"))`},
		{"title with parens", "[foo]: /url (the title)\n\n[foo]", `(p (a "/url" (title "the title") "foo"))`},
		{"escaped destination", "[foo]: /url\\bar\\*baz \"foo\\\"bar\\baz\"\n\n[foo]", `(p (a "/url\\bar*baz" (title "foo\"bar\\baz") "foo"))`},
		{"empty label is no definition", "[]: /url\n\n[]", `(p "[]: /url") (p "[]")`},
		{"blank label is no definition", "[ ]: /url", `(p "[ ]: /url")`},
	})
}

func TestTables(t *testing.T) {
	runCases(t, []blockCase{
		{"basic", "| a | b |\n|---|---|\n| 1 | 2 |", `(table "--" (head (c "a") (c "b")) (row (c "1") (c "2")))`},
		{"no outer pipes", "a | b\n--|--\n1 | 2", `(table "--" (head (c "a") (c "b")) (row (c "1") (c "2")))`},
		{"alignment", "a | b | c | d\n:-- | :-: | --: | ---\n1|2|3|4", `(table "lcr-" (head (c "a") (c "b") (c "c") (c "d")) (row (c "1") (c "2") (c "3") (c "4")))`},
		{"alignment with outer pipes", "| a | b |\n| :---: | ---: |\n| 1 | 2 |", `(table "cr" (head (c "a") (c "b")) (row (c "1") (c "2")))`},
		{"escaped pipe", "| a |\n|---|\n| x \\| y |", `(table "-" (head (c "a")) (row (c "x | y")))`},
		{"escaped pipe in code", "| a |\n|---|\n| `x \\| y` |", `(table "-" (head (c "a")) (row (c (code "x | y"))))`},
		{"escaped backslash before pipe", "| a | b |\n|---|---|\n| x \\\\| y |", `(table "--" (head (c "a") (c "b")) (row (c "x \\") (c "y")))`},
		{"ragged rows padded and truncated", "| a | b |\n|---|---|\n| 1 |\n| 1 | 2 | 3 |", `(table "--" (head (c "a") (c "b")) (row (c "1") (c)) (row (c "1") (c "2")))`},
		{"header count mismatch is a paragraph", "| a | b |\n|---|\n| 1 |", `(p "| a | b |" (sb) "|---|" (sb) "| 1 |")`},
		{"after paragraph text", "intro\n| a |\n|---|\n| 1 |", `(p "intro") (table "-" (head (c "a")) (row (c "1")))`},
		{"inline content", "| `x` | *y* | [l](/u) |\n|---|---|---|\n| **b** | ~~s~~ | ![i](/p) |", `(table "---" (head (c (code "x")) (c (em "y")) (c (a "/u" "l"))) (row (c (strong "b")) (c (del "s")) (c (img "/p" "i"))))`},
		{"header only", "| a | b |\n|---|---|", `(table "--" (head (c "a") (c "b")))`},
		{"ended by blank line", "| a |\n|---|\n| 1 |\n\npara", `(table "-" (head (c "a")) (row (c "1"))) (p "para")`},
		{"ended by quote", "| a |\n|---|\n| 1 |\n> q", `(table "-" (head (c "a")) (row (c "1"))) (quote (p "q"))`},
		{"row without pipe", "| a | b |\n|---|---|\nplain", `(table "--" (head (c "a") (c "b")) (row (c "plain") (c)))`},
		{"no delimiter row", "| a | b |\n| 1 | 2 |", `(p "| a | b |" (sb) "| 1 | 2 |")`},
		{"delimiter with spaces", "a | b\n :--- | ---: \n1 | 2", `(table "lr" (head (c "a") (c "b")) (row (c "1") (c "2")))`},
		{"dash only delimiter is setext", "a\n---", `(h2 "a")`},
		{"empty cells", "| a | b |\n|---|---|\n|  | 2 |", `(table "--" (head (c "a") (c "b")) (row (c) (c "2")))`},
		{"table in list item", "- | a |\n  |---|\n  | 1 |", `(ul tight (li (table "-" (head (c "a")) (row (c "1")))))`},
		{"table in quote", "> | a |\n> |---|\n> | 1 |", `(quote (table "-" (head (c "a")) (row (c "1"))))`},
		{"delimiter with letters is no table", "| a |\n|-x-|", `(p "| a |" (sb) "|-x-|")`},
	})
}

func TestLineEndings(t *testing.T) {
	runCases(t, []blockCase{
		{"crlf", "a\r\n\r\n# b\r\n", `(p "a") (h1 "b")`},
		{"lone cr", "a\r\rb", `(p "a") (p "b")`},
		{"mixed", "a\r\nb\nc\rd", `(p "a" (sb) "b" (sb) "c" (sb) "d")`},
		{"nul", "a\x00b", "(p \"a\ufffdb\")"},
		{"no trailing newline", "a", `(p "a")`},
	})
}

func TestBlockSpans(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"paragraph and heading", "a\n\n# b\n", "p[0,1] h1[2,3]"},
		{"crlf", "a\r\n\r\n# b\r\n", "p[0,1] h1[2,3]"},
		{"lone cr", "a\r\r# b\r", "p[0,1] h1[2,3]"},
		{"setext covers both lines", "x\n===\ny", "h1[0,2] p[2,3]"},
		{"multi-line paragraph", "a\nb\nc\n\nd", "p[0,3] p[4,5]"},
		{"trailing blank lines", "a\n\n\n", "p[0,1]"},
		{"fenced code", "```go\nx\n```\nafter", "code[0,3] p[3,4]"},
		{"unclosed fence", "text\n```\nx\ny", "p[0,1] code[1,4]"},
		{"indented code", "    code\n\nx", "code[0,1] p[2,3]"},
		{"indented code with blank", "    a\n\n    b\n\nx", "code[0,3] p[4,5]"},
		{"rule", "---\nfoo", "hr[0,1] p[1,2]"},
		{"list", "- a\n- b\n\npara", "list[0,2]{li[0,1]{p[0,1]} li[1,2]{p[1,2]}} p[3,4]"},
		{"list item with blocks", "1. a\n\n   b\n2. c", "list[0,4]{li[0,3]{p[0,1] p[2,3]} li[3,4]{p[3,4]}}"},
		{"list item with fence", "- a\n  ```\n  c\n  ```\n- b", "list[0,5]{li[0,4]{p[0,1] code[1,4]} li[4,5]{p[4,5]}}"},
		{"nested list", "- a\n  - b\n  - c\n- d", "list[0,4]{li[0,3]{p[0,1] list[1,3]{li[1,2]{p[1,2]} li[2,3]{p[2,3]}}} li[3,4]{p[3,4]}}"},
		{"empty item", "- a\n-\n- b", "list[0,3]{li[0,1]{p[0,1]} li[1,2]{} li[2,3]{p[2,3]}}"},
		{"quote", "> a\n> b\n\nc", "quote[0,2]{p[0,2]} p[3,4]"},
		{"quote with lazy line", "> a\nlazy", "quote[0,2]{p[0,2]}"},
		{"quote after text", "x\n\n> a\n> # h", "p[0,1] quote[2,4]{p[2,3] h1[3,4]}"},
		{"empty quote", ">\n\nx", "quote[0,1]{} p[2,3]"},
		{"quote in list", "- > a\n  > b\n- c", "list[0,3]{li[0,2]{quote[0,2]{p[0,2]}} li[2,3]{p[2,3]}}"},
		{"list in quote", "> - a\n>   b\n> - c", "quote[0,3]{list[0,3]{li[0,2]{p[0,2]} li[2,3]{p[2,3]}}}"},
		{"table", "t\n| a |\n|---|\n| 1 |\n\nx", "p[0,1] table[1,4] p[5,6]"},
		{"reference definition shifts the paragraph", "[a]: /x\ntext", "p[1,2]"},
		{"only a definition", "[a]: /x\n\nz", "p[2,3]"},
		{"html block", "<div>\n\nx", "html[0,1] p[2,3]"},
		{"html comment spanning blank", "<!-- a\n\nb -->\nc", "html[0,3] p[3,4]"},
		{"heading in list", "- # h\n- x", "list[0,2]{li[0,1]{h1[0,1]} li[1,2]{p[1,2]}}"},
		{"item after blank lines", "- a\n\n\n- b", "list[0,4]{li[0,1]{p[0,1]} li[3,4]{p[3,4]}}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := dumpSpans(parse(tc.src))
			if got != tc.want {
				t.Errorf("src %q\n got: %s\nwant: %s", tc.src, got, tc.want)
			}
		})
	}
}

func TestSpanLineCountMatchesSource(t *testing.T) {
	cases := []struct {
		src   string
		lines int
	}{
		{"a\nb\nc", 3},
		{"a\nb\nc\n", 3},
		{"a\r\nb\r\nc\r\n", 3},
		{"a\n\n\n", 3},
		{"\n\n\na", 4},
	}
	for _, tc := range cases {
		if got := len(splitLines(tc.src)); got != tc.lines {
			t.Errorf("%q: %d lines, want %d", tc.src, got, tc.lines)
		}
	}
}

func TestParseExportedAPI(t *testing.T) {
	bs := Parse("a\n\n# b\n")
	if len(bs) != 2 {
		t.Fatalf("got %d blocks, want 2", len(bs))
	}
	if p, ok := bs[0].(Paragraph); !ok || p.Pos != (Span{0, 1}) {
		t.Errorf("block 0 = %#v, want Paragraph{0,1}", bs[0])
	}
	if h, ok := bs[1].(Heading); !ok || h.Level != 1 || h.Pos != (Span{2, 3}) {
		t.Errorf("block 1 = %#v, want Heading{2,3}", bs[1])
	}
	if got := Parse(""); len(got) != 0 {
		t.Errorf("Parse(\"\") = %#v, want no blocks", got)
	}
}

func TestFallbackBlocks(t *testing.T) {
	got := dumpBlocks(fallbackBlocks("a *b*\n\n  \nc"))
	if want := `(p "a *b*") (p "c")`; got != want {
		t.Errorf("fallbackBlocks = %s, want %s", got, want)
	}
	if s := dumpSpans(fallbackBlocks("a\n\nc")); s != "p[0,1] p[2,3]" {
		t.Errorf("spans = %s", s)
	}
}
