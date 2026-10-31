package workspace

import (
	"slices"
	"testing"
)

func TestComparers(t *testing.T) {
	names := []string{"b.txt", "B.txt", "a.txt", "A.txt", "file10.txt", "file2.txt", "file02.txt", ".hidden", "Zeta", "é.txt", "e.txt", "main.go", "Makefile", "README", "readme.md"}
	tests := []struct {
		name string
		cmp  NameComparer
		want []string
	}{
		{"names default mixes cases and sorts numbers as numbers", CompareFileNamesDefault,
			[]string{".hidden", "a.txt", "A.txt", "b.txt", "B.txt", "e.txt", "é.txt", "file2.txt", "file02.txt", "file10.txt", "main.go", "Makefile", "README", "readme.md", "Zeta"}},
		{"names upper groups upper case first", CompareFileNamesUpper,
			[]string{".hidden", "A.txt", "B.txt", "Makefile", "README", "Zeta", "a.txt", "b.txt", "e.txt", "é.txt", "file2.txt", "file02.txt", "file10.txt", "main.go", "readme.md"}},
		{"names unicode is by code point", CompareFileNamesUnicode,
			[]string{".hidden", "A.txt", "B.txt", "Makefile", "README", "Zeta", "a.txt", "b.txt", "e.txt", "file02.txt", "file10.txt", "file2.txt", "main.go", "readme.md", "é.txt"}},
		{"extensions default groups by extension", CompareFileExtensionsDefault,
			[]string{".hidden", "Makefile", "README", "Zeta", "main.go", "readme.md", "a.txt", "A.txt", "b.txt", "B.txt", "e.txt", "é.txt", "file2.txt", "file02.txt", "file10.txt"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := slices.Clone(names)
			slices.SortFunc(got, tc.cmp)
			if !slices.Equal(got, tc.want) {
				t.Errorf("sorted\n got %q\nwant %q", got, tc.want)
			}
		})
	}
}

func TestCompareFileNamesLowerFirst(t *testing.T) {
	got := []string{"Zeta", "alpha", "Beta", "gamma"}
	slices.SortFunc(got, CompareFileNamesLower)
	if want := []string{"alpha", "gamma", "Beta", "Zeta"}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	slices.SortFunc(got, CompareFileNamesUpper)
	if want := []string{"Beta", "Zeta", "alpha", "gamma"}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCompareFileExtensionsUnicodeAndDotfiles(t *testing.T) {
	tests := []struct {
		a, b string
		want int // sign
	}{
		{"a.TXT", "b.txt", -1}, // the extension is compared in lower case, then the names
		{"b.go", "a.txt", -1},
		{".gitignore", "a.go", -1}, // a dotfile has no extension
		{"x.go", "x.go", 0},
	}
	for _, tc := range tests {
		got := CompareFileExtensionsUnicode(tc.a, tc.b)
		if sign(got) != tc.want {
			t.Errorf("CompareFileExtensionsUnicode(%q, %q) = %d, want sign %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestExtractExtension(t *testing.T) {
	tests := []struct{ name, want string }{
		{"a.txt", "txt"}, {"a.b.c", "c"}, {".gitignore", ""}, {"Makefile", ""}, {"", ""}, {"a.", ""}, {".a.b", ""},
	}
	for _, tc := range tests {
		if got := extractExtension(tc.name); got != tc.want {
			t.Errorf("extractExtension(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}

// TestNumbersWorthZeroSortFirst keeps the order of Intl.Collator for numbers whose value is zero, which the numeric collation of
// x/text puts after the others when text follows them.
func TestNumbersWorthZeroSortFirst(t *testing.T) {
	tests := []struct {
		name  string
		cmp   NameComparer
		names []string
		want  []string
	}{
		{"two digits", CompareFileNamesDefault,
			[]string{"file11.txt", "file10.txt", "file01.txt", "file00.txt"},
			[]string{"file00.txt", "file01.txt", "file10.txt", "file11.txt"}},
		{"one digit", CompareFileNamesDefault,
			[]string{"file1.txt", "file0.txt", "file9.txt"},
			[]string{"file0.txt", "file1.txt", "file9.txt"}},
		{"zeros of different lengths keep the shorter first", CompareFileNamesDefault,
			[]string{"x000y", "x1y", "x00y", "x0y"},
			[]string{"x0y", "x00y", "x000y", "x1y"}},
		{"a zero number before a name without one", CompareFileNamesDefault,
			[]string{"v1-0-final", "v1-0-beta", "v0-9", "v1-10"},
			[]string{"v0-9", "v1-0-beta", "v1-0-final", "v1-10"}},
		{"nines carry", CompareFileNamesDefault,
			[]string{"a10b", "a9b", "a0b", "a99b", "a100b"},
			[]string{"a0b", "a9b", "a10b", "a99b", "a100b"}},
		{"upper first", CompareFileNamesUpper,
			[]string{"b1.txt", "B0.txt", "a0.txt", "A1.txt", "A0.txt"},
			[]string{"A0.txt", "A1.txt", "B0.txt", "a0.txt", "b1.txt"}},
		{"by extension", CompareFileExtensionsDefault,
			[]string{"f1.t0x", "f0.t1x", "f0.t0x"},
			[]string{"f0.t0x", "f1.t0x", "f0.t1x"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := slices.Clone(tc.names)
			slices.SortFunc(got, tc.cmp)
			if !slices.Equal(got, tc.want) {
				t.Errorf("sorted\n got %q\nwant %q", got, tc.want)
			}
		})
	}
}

func TestIncrementNumbers(t *testing.T) {
	for in, want := range map[string]string{
		"": "", "abc": "abc", "0": "1", "00": "1", "009": "10", "file99.txt": "file100.txt", "a0b19c": "a1b20c", "é0": "é1",
	} {
		if got := incrementNumbers(in); got != want {
			t.Errorf("incrementNumbers(%q) = %q, want %q", in, got, want)
		}
	}
}
