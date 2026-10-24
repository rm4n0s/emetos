package workspace

import (
	"path"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// IncrementalNaming is explorer.incrementalNaming: how a pasted or duplicated item that finds
// its name taken gets a new one.
type IncrementalNaming string

const (
	// NamingSimple appends "copy" and then a number: "a copy.txt", "a copy 2.txt" (the default).
	NamingSimple IncrementalNaming = "simple"
	// NamingSmart adds a number to the name, or raises the one it has: "a.1.txt", "a.2.txt".
	NamingSmart IncrementalNaming = "smart"
	// NamingDisabled keeps the name: the destination is overwritten (after a confirmation).
	NamingDisabled IncrementalNaming = "disabled"
)

// maxSmallInteger is VS Code's Constants.MAX_SAFE_SMALL_INTEGER (1 << 30).
const maxSmallInteger = 1 << 30

var (
	copySuffixRe = regexp.MustCompile(`^(.+ copy)( \d+)?$`)
	// the patterns of the smart naming, in the order VS Code tries them
	suffixFileRe       = regexp.MustCompile(`(.*[.\-_])(\d+)(\..*)$`)
	prefixFileRe       = regexp.MustCompile(`(\d+)([.\-_].*)(\..*)$`)
	prefixFileNoNameRe = regexp.MustCompile(`(\d+)(\..*)$`)
	trailingNumberRe   = regexp.MustCompile(`(\d+)$`)
	leadingNumberRe    = regexp.MustCompile(`^(\d+)(.*)$`)
)

// IncrementFileName gives the next candidate name for an item whose name is taken. With NamingSimple,
// "name.txt" becomes "name copy.txt", then "name copy 2.txt" and so on; with NamingSmart a number is
// added to the name or raised ("file.1.txt", "file.2.txt", "1.txt", "2.txt", "folder1"). Any other
// value of naming is NamingSimple.
//
// Ported from VS Code: src/vs/workbench/contrib/files/browser/fileActions.ts (incrementFileName)
func IncrementFileName(name string, isFolder bool, naming IncrementalNaming) string {
	if naming != NamingSmart {
		return incrementSimple(name, isFolder)
	}
	return incrementSmart(name, isFolder)
}

// jsExtname is Node's path.posix.extname: the last dot of the name and what follows it, unless the
// dot starts the name.
func jsExtname(name string) string {
	i := strings.LastIndex(name, ".")
	if i <= 0 || name == ".." {
		return ""
	}
	return name[i:]
}

func incrementSimple(name string, isFolder bool) string {
	prefix, ext := name, ""
	if !isFolder {
		ext = jsExtname(name)
		if ext != "" && ext != name {
			prefix = strings.TrimSuffix(name, ext)
		}
	}
	// name copy 5(.txt) => name copy 6(.txt); name copy(.txt) => name copy 2(.txt)
	if m := copySuffixRe.FindStringSubmatch(prefix); m != nil {
		g1, g2 := m[1], m[2]
		number := 1
		if g2 != "" {
			number = parseSmallInt(strings.TrimSpace(g2))
		}
		switch {
		case number == 0:
			return g1 + ext
		case number < maxSmallInteger:
			return g1 + " " + strconv.Itoa(number+1) + ext
		}
		return g1 + g2 + " copy" + ext
	}
	// name(.txt) => name copy(.txt)
	return prefix + " copy" + ext
}

// parseSmallInt reads a run of digits; one too long for an int counts as the largest.
func parseSmallInt(digits string) int {
	n, err := strconv.Atoi(digits)
	if err != nil {
		return maxSmallInteger
	}
	return n
}

// bump is the next number, as wide as the digits it replaces; the second result is false when the
// number is too large to raise.
func bump(digits string) (string, bool) {
	n := parseSmallInt(digits)
	if n >= maxSmallInteger {
		return "", false
	}
	s := strconv.Itoa(n + 1)
	for len(s) < len(digits) {
		s = "0" + s
	}
	return s, true
}

func incrementSmart(name string, isFolder bool) string {
	// file.1.txt => file.2.txt
	if !isFolder {
		if m := suffixFileRe.FindStringSubmatchIndex(name); m != nil {
			g1, g2, g3 := name[m[2]:m[3]], name[m[4]:m[5]], name[m[6]:m[7]]
			if next, ok := bump(g2); ok {
				return name[:m[0]] + g1 + next + g3
			}
			return name[:m[0]] + g1 + g2 + ".1" + g3
		}
		// 1.file.txt => 2.file.txt
		if m := prefixFileRe.FindStringSubmatchIndex(name); m != nil {
			g1, g2, g3 := name[m[2]:m[3]], name[m[4]:m[5]], name[m[6]:m[7]]
			if next, ok := bump(g1); ok {
				return name[:m[0]] + next + g2 + g3
			}
			return name[:m[0]] + g1 + g2 + ".1" + g3
		}
		// 1.txt => 2.txt
		if m := prefixFileNoNameRe.FindStringSubmatchIndex(name); m != nil {
			g1, g2 := name[m[2]:m[3]], name[m[4]:m[5]]
			if next, ok := bump(g1); ok {
				return name[:m[0]] + next + g2
			}
			return name[:m[0]] + g1 + ".1" + g2
		}
	}
	lastDot := strings.LastIndex(name, ".")
	// file.txt => file.1.txt
	if !isFolder && lastDot >= 0 {
		return name[:lastDot] + ".1" + name[lastDot:]
	}
	// 123 => 124; file => file1; file1 => file2
	if !isFolder && lastDot == -1 {
		if m := trailingNumberRe.FindStringSubmatchIndex(name); m != nil {
			digits := name[m[2]:m[3]]
			if next, ok := bump(digits); ok {
				return name[:m[0]] + next
			}
			return name[:m[0]] + digits + ".1"
		}
		return name + "1"
	}
	// folder.1 => folder.2
	if isFolder {
		if m := trailingNumberRe.FindStringSubmatchIndex(name); m != nil {
			digits := name[m[2]:m[3]]
			if next, ok := bump(digits); ok {
				return name[:m[0]] + next
			}
			return name[:m[0]] + digits + ".1"
		}
		// 1.folder => 2.folder
		if m := leadingNumberRe.FindStringSubmatch(name); m != nil {
			if next, ok := bump(m[1]); ok {
				return next + m[2]
			}
			return m[1] + m[2] + ".1"
		}
	}
	// file/folder => file.1/folder.1
	return name + ".1"
}

// Severity says how bad a problem with a name is.
type Severity int

const (
	// SeverityWarning lets the name be used.
	SeverityWarning Severity = iota + 1
	// SeverityError refuses the name.
	SeverityError
)

// NameProblem is a message about a name typed for a new or renamed item. The Content marks the
// parts VS Code shows in bold with ** (the name).
type NameProblem struct {
	Content  string
	Severity Severity
}

// MaxNameBytes is the longest name a file system takes (255 bytes).
const MaxNameBytes = 255

// ValidateFileName checks a name typed into the inline input of the Explorer. current is the name the
// item has now ("" for a new item); exists tells whether the folder the item is in has an item of a
// name. It returns nil for a name that can be used. A name with slashes names a path below the folder
// ("a/b/c.txt"), and each part is checked; a part with a trailing or leading space is a warning.
//
// Ported from VS Code: src/vs/workbench/contrib/files/browser/fileActions.ts (validateFileName) and
// src/vs/base/common/extpath.ts (isValidBasename, for Linux), with the 255 limit in bytes, which is the
// limit of the file systems, where VS Code counts UTF-16 units
func ValidateFileName(name, current string, exists func(name string) bool) *NameProblem {
	name = wellFormedName(name)
	if name == "" || strings.TrimSpace(name) == "" {
		return &NameProblem{Content: "A file or folder name must be provided.", Severity: SeverityError}
	}
	if name[0] == '/' || name[0] == '\\' {
		return &NameProblem{Content: "A file or folder name cannot start with a slash.", Severity: SeverityError}
	}
	parts := nameParts(name)
	if name != current && exists != nil && exists(name) {
		return &NameProblem{
			Content:  "A file or folder **" + name + "** already exists at this location. Please choose a different name.",
			Severity: SeverityError,
		}
	}
	for _, part := range parts {
		if !validBasename(part) {
			escaped := strings.ReplaceAll(name, "*", `\*`)
			return &NameProblem{
				Content:  "The name **" + trimLongName(escaped) + "** is not valid as a file or folder name. Please choose a different name.",
				Severity: SeverityError,
			}
		}
	}
	for _, part := range parts {
		if part != strings.TrimFunc(part, unicode.IsSpace) {
			return &NameProblem{Content: "Leading or trailing whitespace detected in file or folder name.", Severity: SeverityWarning}
		}
	}
	return nil
}

// wellFormedName trims tabs and the trailing slashes of a name.
func wellFormedName(name string) string {
	name = strings.Trim(name, "\t")
	name = strings.TrimRight(name, "/")
	return strings.TrimRight(name, "\\")
}

// nameParts splits a name at its slashes and backslashes, leaving out the empty parts.
func nameParts(name string) []string {
	return strings.FieldsFunc(name, func(r rune) bool { return r == '/' || r == '\\' })
}

func validBasename(name string) bool {
	switch {
	case name == "" || strings.TrimSpace(name) == "":
		return false
	case strings.ContainsAny(name, "/\x00"):
		return false
	case name == "." || name == "..":
		return false
	case len(name) > MaxNameBytes:
		return false
	}
	return true
}

func trimLongName(name string) string {
	if len(name) > MaxNameBytes {
		return name[:MaxNameBytes] + "..."
	}
	return name
}

// CleanNewName turns what was typed into the relative path of the item to create: tabs and trailing
// slashes are dropped and the path is cleaned. endsInSlash tells whether the name ended in a slash, which
// makes the item a folder (VS Code does the same).
func CleanNewName(typed string) (rel string, endsInSlash bool) {
	endsInSlash = strings.HasSuffix(typed, "/")
	rel = wellFormedName(typed)
	return path.Clean(rel), endsInSlash
}
