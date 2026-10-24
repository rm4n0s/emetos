package workspace

import (
	"regexp"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

// Ported from VS Code: src/vs/base/common/comparers.ts
//
// VS Code compares names with Intl.Collator of the runtime's locale. Emetos uses the root
// collation of golang.org/x/text (CLDR root, the order of Intl.Collator with no locale), whatever
// the locale of the user is. JavaScript compares strings by UTF-16 code units where Go compares
// bytes (code points); the two differ only for characters outside the Basic Multilingual Plane.

// collator is a collator that can be used from several goroutines.
type collator struct {
	mu sync.Mutex
	c  *collate.Collator
}

func newCollator(opts ...collate.Option) *collator {
	return &collator{c: collate.New(language.Und, opts...)}
}

func (c *collator) compare(a, b string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.c.CompareString(a, b)
}

var (
	// collatorNumeric has numeric sorting ("file2" before "file10"), and tells upper case from
	// lower case and accents.
	collatorNumeric = newCollator(collate.Numeric)
	// collatorNumericCaseInsensitive has numeric sorting and sensitivity to accents but not case.
	collatorNumericCaseInsensitive = newCollator(collate.Numeric, collate.IgnoreCase)
)

// NameComparer compares two file names: negative when a goes first, zero when they are the same
// and positive when b goes first.
type NameComparer func(a, b string) int

// compareAndDisambiguateByLength compares with a collator; in a numeric comparison "foo1" and
// "foo01" are equal, so the shorter name goes first.
func compareAndDisambiguateByLength(c *collator, one, other string) int {
	if r := c.compare(one, other); r != 0 {
		return r
	}
	if len(one) != len(other) {
		if len(one) < len(other) {
			return -1
		}
		return 1
	}
	return 0
}

// CompareFileNamesDefault compares full names without grouping by case.
func CompareFileNamesDefault(one, other string) int {
	return compareAndDisambiguateByLength(collatorNumeric, one, other)
}

// CompareFileNamesUpper compares full names, grouping the names that start with an upper case
// letter before the others.
func CompareFileNamesUpper(one, other string) int {
	if r := compareCaseUpperFirst(one, other); r != 0 {
		return r
	}
	return compareAndDisambiguateByLength(collatorNumeric, one, other)
}

// CompareFileNamesLower compares full names, grouping the names that start with a lower case
// letter before the others.
func CompareFileNamesLower(one, other string) int {
	if r := compareCaseLowerFirst(one, other); r != 0 {
		return r
	}
	return compareAndDisambiguateByLength(collatorNumeric, one, other)
}

// CompareFileNamesUnicode compares full names by their code points.
func CompareFileNamesUnicode(one, other string) int { return strings.Compare(one, other) }

// CompareFileExtensionsDefault compares names by extension, then by full name; upper and lower
// case are mixed.
func CompareFileExtensionsDefault(one, other string) int {
	if r := compareAndDisambiguateByLength(collatorNumericCaseInsensitive, extractExtension(one), extractExtension(other)); r != 0 {
		return r
	}
	return compareAndDisambiguateByLength(collatorNumeric, one, other)
}

// CompareFileExtensionsUpper compares names by extension, then by case (upper case first), then
// by full name.
func CompareFileExtensionsUpper(one, other string) int {
	if r := compareAndDisambiguateByLength(collatorNumericCaseInsensitive, extractExtension(one), extractExtension(other)); r != 0 {
		return r
	}
	if r := compareCaseUpperFirst(one, other); r != 0 {
		return r
	}
	return compareAndDisambiguateByLength(collatorNumeric, one, other)
}

// CompareFileExtensionsLower compares names by extension, then by case (lower case first), then
// by full name.
func CompareFileExtensionsLower(one, other string) int {
	if r := compareAndDisambiguateByLength(collatorNumericCaseInsensitive, extractExtension(one), extractExtension(other)); r != 0 {
		return r
	}
	if r := compareCaseLowerFirst(one, other); r != 0 {
		return r
	}
	return compareAndDisambiguateByLength(collatorNumeric, one, other)
}

// CompareFileExtensionsUnicode compares names by the extension in lower case, by code points,
// then by full name.
func CompareFileExtensionsUnicode(one, other string) int {
	oneExt, otherExt := strings.ToLower(extractExtension(one)), strings.ToLower(extractExtension(other))
	if oneExt != otherExt {
		return strings.Compare(oneExt, otherExt)
	}
	return strings.Compare(one, other)
}

var fileNameMatch = regexp.MustCompile(`^(.*?)(\.([^.]*))?$`)

// extractExtension is the extension of a name; a dotfile has none (".gitignore" is a name).
func extractExtension(name string) string {
	if name == "" {
		return ""
	}
	m := fileNameMatch.FindStringSubmatch(name)
	if m == nil || m[1] == "" || strings.HasPrefix(m[1], ".") {
		return ""
	}
	return m[3]
}

func firstRune(s string) rune {
	r, _ := utf8.DecodeRuneInString(s)
	return r
}

// startsWithLower tells whether the string starts with a letter that has an upper case form.
func startsWithLower(s string) bool {
	r := firstRune(s)
	return s != "" && unicode.ToUpper(r) != r
}

// startsWithUpper tells whether the string starts with a letter that has a lower case form.
func startsWithUpper(s string) bool {
	r := firstRune(s)
	return s != "" && unicode.ToLower(r) != r
}

func compareCaseLowerFirst(one, other string) int {
	if startsWithLower(one) && startsWithUpper(other) {
		return -1
	}
	if startsWithUpper(one) && startsWithLower(other) {
		return 1
	}
	return 0
}

func compareCaseUpperFirst(one, other string) int {
	if startsWithUpper(one) && startsWithLower(other) {
		return -1
	}
	if startsWithLower(one) && startsWithUpper(other) {
		return 1
	}
	return 0
}
