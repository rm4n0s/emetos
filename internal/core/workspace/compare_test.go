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
