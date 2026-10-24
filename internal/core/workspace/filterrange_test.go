package workspace

import "testing"

// The cases of extractRangeFromFilter's tests in VS Code (extractRange.test.ts) and a few more of
// the formats the quick open accepts. Lines and columns count from 1.
func TestExtractRangeFromFilter(t *testing.T) {
	const base = "/some/path/file.txt"
	type want struct {
		pattern string
		rng     FilterRange
	}
	r := func(l, c, el, ec int) FilterRange { return FilterRange{l, c, el, ec} }
	tests := []struct {
		name   string
		filter string
		unless []string
		want   *want
	}{
		{"empty", "", nil, nil},
		{"a path", "/some/path", nil, nil},
		{"a file", base, nil, nil},
		{"space before the position", base + " (19,20)", nil, &want{base, r(19, 20, 19, 20)}},
		{"line range", base + ":20-40", nil, &want{base, r(20, 1, 40, 1)}},
		{"line and column range", base + ":20:3-40:5", nil, &want{base, r(20, 3, 40, 5)}},
		{"end column defaults to the start of the line", base + ":20:3-40", nil, &want{base, r(20, 3, 40, 1)}},
		{"mixed separators", base + "#20#3-40,5", nil, &want{base, r(20, 3, 40, 5)}},
		{"parentheses", base + "(20,3-40,5)", nil, &want{base, r(20, 3, 40, 5)}},
		{"a dangling range", base + ":20-", nil, &want{base, r(20, 1, 20, 1)}},
		{"a hyphen in the name", "/some/path/my-file.txt", nil, nil},
		{"a hyphen and a digit", "/some/path/file-2.txt", nil, nil},
		{"colon without a line is the start of the file", "main.go:", nil, &want{"main.go", r(1, 1, 1, 1)}},
		{"hash without a line", "main.go#", nil, &want{"main.go", r(1, 1, 1, 1)}},
		{"line only", "main.go:12", nil, &want{"main.go", r(12, 1, 12, 1)}},
		{"line and column", "main.go:12:5", nil, &want{"main.go", r(12, 5, 12, 5)}},
		{"a trailing colon after the position", "main.go:12:5:", nil, &want{"main.go", r(12, 5, 12, 5)}},
		{"hash", "main.go#12", nil, &want{"main.go", r(12, 1, 12, 1)}},
		{"parenthesis line", "main.go(12)", nil, &want{"main.go", r(12, 1, 12, 1)}},
		{"the word line", "main.go:line 12", nil, &want{"main.go", r(12, 1, 12, 1)}},
		{"trailing white space", "main.go:12  ", nil, &want{"main.go", r(12, 1, 12, 1)}},
		{"a path with a colon", "a:b/main.go:7", nil, &want{"a:b/main.go", r(7, 1, 7, 1)}},
		{"no digits after the file", "main.go:abc", nil, nil},
		{"two words keep the first intact", "server util:3", nil, &want{"server util", r(3, 1, 3, 1)}},
		{"symbol after the unless", "/some/path/file.txt@alphasymbol", []string{"@"}, nil},
		{"unless as first char", "@/some/path/file.txt (19,20)", []string{"@"}, nil},
		{"unless as last char", "/some/path/file.txt (19,20)@", []string{"@"}, nil},
		{"unless before the comma", "/some/@path/file.txt (19,20)", []string{"@"}, &want{"/some/@path/file.txt", r(19, 20, 19, 20)}},
		{"unless before the colon", "/some/@path/file.txt:19:20", []string{"@"}, &want{"/some/@path/file.txt", r(19, 20, 19, 20)}},
		{"unless before the hash", "/some/@path/file.txt#19", []string{"@"}, &want{"/some/@path/file.txt", r(19, 1, 19, 1)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pattern, rng, ok := ExtractRangeFromFilter(tc.filter, tc.unless...)
			if tc.want == nil {
				if ok {
					t.Fatalf("got %q %+v, want no position", pattern, rng)
				}
				return
			}
			if !ok || pattern != tc.want.pattern || rng != tc.want.rng {
				t.Errorf("got %q %+v %v, want %q %+v", pattern, rng, ok, tc.want.pattern, tc.want.rng)
			}
		})
	}
	// the separators of the basic test of VS Code
	for _, lineSep := range []string{":", "#", "(", ":line "} {
		for _, colSep := range []string{":", "#", ","} {
			for _, c := range []struct {
				suffix string
				l, col int
			}{{"20", 20, 1}, {"20" + colSep, 20, 1}, {"20" + colSep + "3", 20, 3}} {
				pattern, rng, ok := ExtractRangeFromFilter(base + lineSep + c.suffix)
				if !ok || pattern != base || rng.StartLine != c.l || rng.StartColumn != c.col {
					t.Errorf("%q: got %q %+v %v", base+lineSep+c.suffix, pattern, rng, ok)
				}
			}
		}
	}
}
