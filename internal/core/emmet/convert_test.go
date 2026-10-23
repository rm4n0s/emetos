package emmet

import "testing"

// Ported from Emmet: packages/abbreviation/test/convert.ts

type convertCase struct {
	abbr string
	opts ParserOptions
	want string
}

func runConvertCases(t *testing.T, cases []convertCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.abbr, func(t *testing.T) {
			tree, err := ParseAbbreviation(tc.abbr, tc.opts)
			if err != nil {
				t.Fatalf("%q: %v", tc.abbr, err)
			}
			if got := stringifyNodes(tree.Children); got != tc.want {
				t.Errorf("%q\n got %s\nwant %s", tc.abbr, got, tc.want)
			}
		})
	}
}

func TestConvertBasic(t *testing.T) {
	runConvertCases(t, []convertCase{
		{`input[value="text$"]*2`, ParserOptions{}, `<input*2@0 value="text1"></input><input*2@1 value="text2"></input>`},
		{`ul>li.item$*3`, ParserOptions{}, `<ul><li*3@0 class="item1"></li><li*3@1 class="item2"></li><li*3@2 class="item3"></li></ul>`},
		{`ul>li.item$*`, ParserOptions{Text: TextLines("foo$", "bar$")}, `<ul><li*2@0 class="item1">foo$</li><li*2@1 class="item2">bar$</li></ul>`},
		{`ul>li[class=$#]{item $}*`, ParserOptions{Text: TextLines("foo$", "bar$")}, `<ul><li*2@0 class="foo$">item 1</li><li*2@1 class="bar$">item 2</li></ul>`},
		{`ul>li.item$*`, ParserOptions{}, `<ul><li*1@0 class="item1"></li></ul>`},
		{`ul>li.item$*`, ParserOptions{Text: TextLines("foo.bar", "hello.world")}, `<ul><li*2@0 class="item1">foo.bar</li><li*2@1 class="item2">hello.world</li></ul>`},
		{`p{hi}`, ParserOptions{Text: TextLines("hello")}, `<p>hihello</p>`},
		{`p*{hi}`, ParserOptions{Text: TextLines("1", "2")}, `<p*2@0>hi1</p><p*2@1>hi2</p>`},
		{`div>p+p{hi}`, ParserOptions{Text: TextLines("hello")}, `<div><p></p><p>hihello</p></div>`},
		{`html[lang=${lang}]`, ParserOptions{}, `<html lang="lang"></html>`},
		{`html.one.two`, ParserOptions{}, `<html class="one", class="two"></html>`},
		{`html.one[two=three]`, ParserOptions{}, `<html class="one", two="three"></html>`},
		{`div{[}+a{}`, ParserOptions{}, `<div>[</div><a></a>`},
	})
}

func TestConvertUnroll(t *testing.T) {
	runConvertCases(t, []convertCase{
		{`a>(b>c)+d`, ParserOptions{}, `<a><b><c></c></b><d></d></a>`},
		{`(a>b)+(c>d)`, ParserOptions{}, `<a><b></b></a><c><d></d></c>`},
		{`a>((b>c)(d>e))f`, ParserOptions{}, `<a><b><c></c></b><d><e></e></d><f></f></a>`},
		{`a>((((b>c))))+d`, ParserOptions{}, `<a><b><c></c></b><d></d></a>`},
		{`a>(((b>c))*4)+d`, ParserOptions{}, `<a><b*4@0><c></c></b><b*4@1><c></c></b><b*4@2><c></c></b><b*4@3><c></c></b><d></d></a>`},
		{`(div>dl>(dt+dd)*2)`, ParserOptions{}, `<div><dl><dt*2@0></dt><dd*2@0></dd><dt*2@1></dt><dd*2@1></dd></dl></div>`},
		{`a*2>b*3`, ParserOptions{}, `<a*2@0><b*3@0></b><b*3@1></b><b*3@2></b></a><a*2@1><b*3@0></b><b*3@1></b><b*3@2></b></a>`},
		{`a>(b+c)*2`, ParserOptions{}, `<a><b*2@0></b><c*2@0></c><b*2@1></b><c*2@1></c></a>`},
		{`a>(b+c)*2+(d+e)*2`, ParserOptions{}, `<a><b*2@0></b><c*2@0></c><b*2@1></b><c*2@1></c><d*2@0></d><e*2@0></e><d*2@1></d><e*2@1></e></a>`},
		// the <div> is a sibling of {foo}
		{`p>{foo}>div`, ParserOptions{}, `<p><?>foo</?><div></div></p>`},
		{`p>{foo ${0}}>div`, ParserOptions{}, `<p><?>foo ${0}<div></div></?></p>`},
	})
}

func TestConvertLimitUnroll(t *testing.T) {
	runConvertCases(t, []convertCase{
		{`a*10`, ParserOptions{MaxRepeat: 5}, `<a*10@0></a><a*10@1></a><a*10@2></a><a*10@3></a><a*10@4></a>`},
		{`a*10`, ParserOptions{}, `<a*10@0></a><a*10@1></a><a*10@2></a><a*10@3></a><a*10@4></a><a*10@5></a><a*10@6></a><a*10@7></a><a*10@8></a><a*10@9></a>`},
		{`a*3>b*3`, ParserOptions{MaxRepeat: 5}, `<a*3@0><b*3@0></b><b*3@1></b><b*3@2></b></a><a*3@1><b*3@0></b></a>`},
	})
}

func TestConvertParentRepeater(t *testing.T) {
	runConvertCases(t, []convertCase{
		{`a$*2>b$*3/`, ParserOptions{}, `<a1*2@0><b1*3@0 /><b2*3@1 /><b3*3@2 /></a1><a2*2@1><b1*3@0 /><b2*3@1 /><b3*3@2 /></a2>`},
		{`a$*2>b$@^*3/`, ParserOptions{}, `<a1*2@0><b1*3@0 /><b2*3@1 /><b3*3@2 /></a1><a2*2@1><b4*3@0 /><b5*3@1 /><b6*3@2 /></a2>`},
	})
}

func TestConvertHref(t *testing.T) {
	href := func(text string) ParserOptions { return ParserOptions{Href: true, Text: TextString(text)} }
	runConvertCases(t, []convertCase{
		{`a`, href("https://www.google.it"), `<a href="https://www.google.it">https://www.google.it</a>`},
		{`a`, href("www.google.it"), `<a href="http://www.google.it">www.google.it</a>`},
		{`a`, href("google.it"), `<a href="">google.it</a>`},
		{`a`, href("test here"), `<a href="">test here</a>`},
		{`a`, href("test@domain.com"), `<a href="mailto:test@domain.com">test@domain.com</a>`},
		{`a`, href("test here test@domain.com"), `<a href="">test here test@domain.com</a>`},
		{`a`, href("test here www.domain.com"), `<a href="">test here www.domain.com</a>`},

		{`a[href=]`, href("https://www.google.it"), `<a href="https://www.google.it">https://www.google.it</a>`},
		{`a[href=]`, href("www.google.it"), `<a href="http://www.google.it">www.google.it</a>`},
		{`a[href=]`, href("google.it"), `<a href="">google.it</a>`},
		{`a[href=]`, href("test here"), `<a href="">test here</a>`},
		{`a[href=]`, href("test@domain.com"), `<a href="mailto:test@domain.com">test@domain.com</a>`},
		{`a[href=]`, href("test here test@domain.com"), `<a href="">test here test@domain.com</a>`},
		{`a[href=]`, href("test here www.domain.com"), `<a href="">test here www.domain.com</a>`},
		{`a[class=here]`, href("test@domain.com"), `<a class="here", href="mailto:test@domain.com">test@domain.com</a>`},
		{`a.here`, href("www.domain.com"), `<a class="here", href="http://www.domain.com">www.domain.com</a>`},
		{`a[class=here]`, href("test here test@domain.com"), `<a class="here", href="">test here test@domain.com</a>`},
		{`a.here`, href("test here www.domain.com"), `<a class="here", href="">test here www.domain.com</a>`},

		{`a[href="www.google.it"]`, ParserOptions{Href: false, Text: TextString("test")}, `<a href="www.google.it">test</a>`},
		{`a[href="www.example.com"]`, href("www.google.it"), `<a href="www.example.com">www.google.it</a>`},
	})
}

func TestConvertWrapBasic(t *testing.T) {
	runConvertCases(t, []convertCase{
		{`p`, ParserOptions{Text: TextString("test")}, `<p>test</p>`},
		{`p`, ParserOptions{Text: TextLines("test")}, `<p>test</p>`},
		{`p`, ParserOptions{Text: TextLines("test1", "test2")}, "<p>test1\ntest2</p>"},
		{`p`, ParserOptions{Text: TextLines("test1", "", "test2")}, "<p>test1\n\ntest2</p>"},
		{`p*`, ParserOptions{Text: TextLines("test1", "test2")}, `<p*2@0>test1</p><p*2@1>test2</p>`},
		{`p*`, ParserOptions{Text: TextLines("test1", "", "test2")}, `<p*2@0>test1</p><p*2@1>test2</p>`},
	})
}

// Not in Emmet's tests: the limits on the amounts.
func TestConvertLimits(t *testing.T) {
	count := func(abbr string, opts ParserOptions) int {
		tree, err := ParseAbbreviation(abbr, opts)
		if err != nil {
			t.Fatalf("%q: %v", abbr, err)
		}
		return len(tree.Children)
	}
	cases := []struct {
		name string
		abbr string
		opts ParserOptions
		want int
	}{
		{"the default limit", "a*999999999999", ParserOptions{}, DefaultMaxRepeat},
		{"a limit above the hard one", "a*999999999999", ParserOptions{MaxRepeat: 1 << 40}, HardMaxRepeat},
		{"a number too large for an int", "a*99999999999999999999999999999", ParserOptions{MaxRepeat: 7}, 7},
		{"the limit is for all the repeaters together", "a*100>b*100", ParserOptions{MaxRepeat: 10}, 1},
		{"a zero count is one", "a*0", ParserOptions{}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := count(tc.abbr, tc.opts); got != tc.want {
				t.Errorf("%q: %d top level nodes, want %d", tc.abbr, got, tc.want)
			}
		})
	}
}
