package format

import (
	"strconv"
	"strings"
	"unicode"
)

// MaxNameLength is the most bytes of a safe name.
const MaxNameLength = 100

// SafeName makes a name fit for a file name (SPEC §10.33): lowercase, letters and digits and "_" kept, everything else (spaces,
// slashes, dots, punctuation) becomes "-", runs of "-" become one and none is left at the ends. A name with nothing left is
// "untitled". The result is never "." or "..", has no separator and is at most MaxNameLength bytes.
func SafeName(name string) string {
	var b strings.Builder
	dash := true // no dash at the start
	for _, r := range strings.ToLower(name) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_':
			if b.Len()+len(string(r)) > MaxNameLength {
				return strings.TrimRight(b.String(), "-")
			}
			b.WriteRune(r)
			dash = false
		case !dash:
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.TrimRight(b.String(), "-")
	if out == "" {
		return "untitled"
	}
	return out
}

// UniqueName returns base, or base-2, base-3, ... for the first of them that taken does not report as taken.
func UniqueName(base string, taken func(name string) bool) string {
	if !taken(base) {
		return base
	}
	for n := 2; ; n++ {
		name := base + "-" + strconv.Itoa(n)
		if !taken(name) {
			return name
		}
	}
}

// RequestFileName is the file name of the request whose stem is stem.
func RequestFileName(stem string) string { return stem + RequestSuffix }

// RequestStem is the stem of a request file name, and whether the name is one.
func RequestStem(fileName string) (string, bool) {
	stem, ok := strings.CutSuffix(fileName, RequestSuffix)
	return stem, ok && stem != ""
}

// EnvironmentFileName is the file name of the environment whose stem is stem.
func EnvironmentFileName(stem string) string { return stem + EnvironmentSuffix }

// BodyExtension is the extension of the body file of a body type, with its dot; it is empty for a type that has no body file.
func BodyExtension(bodyType string) string {
	switch bodyType {
	case BodyJSON:
		return ".json"
	case BodyXML:
		return ".xml"
	case BodyText:
		return ".txt"
	case BodyHTML:
		return ".html"
	case BodyJavaScript:
		return ".js"
	case BodyGraphQL:
		return ".graphql"
	}
	return ""
}

// BodyFileName is the name of the body file of the request with the stem, or "" for a type with no body file.
func BodyFileName(stem, bodyType string) string {
	ext := BodyExtension(bodyType)
	if ext == "" {
		return ""
	}
	return stem + ".body" + ext
}

// VariablesFileName is the name of the file of the GraphQL variables of the request with the stem.
func VariablesFileName(stem string) string { return stem + ".variables.json" }

// ValidSiblingName reports whether name can be the name of a body file: a plain file name, no folders.
func ValidSiblingName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, "/\\\x00") && len(name) <= 255
}
