package golang

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// marks cut the places out of a source: § is a cursor, ‹ and › are the ends of a selection.
func marks(t *testing.T, src string) (text string, start, end int) {
	t.Helper()
	start, end = -1, -1
	var sb strings.Builder
	for _, r := range src {
		switch r {
		case '§':
			start, end = sb.Len(), sb.Len()
		case '‹':
			start = sb.Len()
		case '›':
			end = sb.Len()
		default:
			sb.WriteRune(r)
		}
	}
	if start < 0 || end < 0 {
		t.Fatalf("the source has no cursor or selection:\n%s", src)
	}
	return sb.String(), start, end
}

// tagCase is a case of testdata/tags/cases.json: a struct, a modification of its tags in the arguments of gopls' command
// gopls.modify_tags (Modification, Add, AddOptions, Remove, RemoveOptions, Clear, ClearOptions, Overwrite, SkipUnexportedFields,
// Transform, ValueFormat), and the text that comes out. The test of the pure Go editor here and the test of the real gopls (build tag
// emetos_gopls, internal/gui/exthost/gopls_test.go) read the same file: they must give the same text.
type tagCase struct {
	Name string         `json:"name"`
	Src  string         `json:"src"`
	Args map[string]any `json:"args"`
	Want string         `json:"want"`
	// GoplsDiffers, when it is not empty, says how gopls behaves differently for the case; the test of the real gopls then expects
	// the text GoplsWant, or an error that contains GoplsError, instead of Want.
	GoplsDiffers string `json:"goplsDiffers,omitempty"`
	GoplsWant    string `json:"goplsWant,omitempty"`
	GoplsError   string `json:"goplsError,omitempty"`
}

// mod is the modification the arguments of gopls say.
func (c tagCase) mod(t *testing.T) tagMod {
	t.Helper()
	str := func(k string) string { s, _ := c.Args[k].(string); return s }
	flag := func(k string) bool { b, _ := c.Args[k].(bool); return b }
	add, err1 := parseOptions(str("AddOptions"))
	remove, err2 := parseOptions(str("RemoveOptions"))
	if err1 != nil || err2 != nil {
		t.Fatal(err1, err2)
	}
	return tagMod{
		Add: splitList(str("Add")), AddOptions: add, Remove: splitList(str("Remove")), RemoveOptions: remove,
		Overwrite: flag("Overwrite"), SkipUnexported: flag("SkipUnexportedFields"), Transform: str("Transform"), ValueFormat: str("ValueFormat"),
		Clear: flag("Clear"), ClearOptions: flag("ClearOptions"),
	}
}

// loadTagCases reads the cases.
func loadTagCases(t *testing.T) []tagCase {
	t.Helper()
	data, err := os.ReadFile("testdata/tags/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []tagCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 20 {
		t.Fatalf("%d cases", len(cases))
	}
	return cases
}

func TestModifyTags(t *testing.T) {
	for _, tt := range loadTagCases(t) {
		t.Run(tt.Name, func(t *testing.T) {
			src, start, end := marks(t, tt.Src)
			got, err := modifyTags(src, start, end, tt.mod(t))
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.Want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tt.Want)
			}
		})
	}
}

func TestModifyTagsFailures(t *testing.T) {
	const ok = "package p\n\ntype T struct {\n\tA int§\n}\n"
	tests := []struct {
		name  string
		src   string
		mod   tagMod
		route string
	}{
		{"the cursor is not in a struct", "package p\n\nfunc f() {§}\n", tagMod{Add: []string{"json"}}, "modifyTags.TagsNoStruct"},
		{"the source has a syntax error", "package p\n\ntype T struct {\n\tA int§\n", tagMod{Add: []string{"json"}}, "modifyTags.TagsSourceInvalid"},
		{"a tag that cannot be read", "package p\n\ntype T struct {\n\tA int `json`§\n}\n", tagMod{Add: []string{"xml"}}, "modifyTags.TagInvalid"},
		{"a tag whose value has no quote", "package p\n\ntype T struct {\n\tA int `json:a`§\n}\n", tagMod{Add: []string{"xml"}}, "modifyTags.TagInvalid"},
		{"a tag with no key", "package p\n\ntype T struct {\n\tA int `:\"a\"`§\n}\n", tagMod{Add: []string{"xml"}}, "modifyTags.TagInvalid"},
		{"a value that is not closed", "package p\n\ntype T struct {\n\tA int `json:\"a`§\n}\n", tagMod{Add: []string{"xml"}}, "modifyTags.TagInvalid"},
		{"nothing to do", ok, tagMod{}, "tagMod.validate.TagsRequestInvalid"},
		{"a transform that does not exist", ok, tagMod{Add: []string{"json"}, Transform: "shouting"}, "tagMod.validate.TagsRequestInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src, start, end := marks(t, tt.src)
			_, err := modifyTags(src, start, end, tt.mod)
			appErr, isErr := errors.FromError(err)
			if !isErr {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tt.route) {
				t.Errorf("unexpected failure path: %s (want ...%s)", appErr.Route(), tt.route)
			}
		})
	}
	// a range outside the text
	_, err := modifyTags("package p\n", 5, 99, tagMod{Add: []string{"json"}})
	if appErr, isErr := errors.FromError(err); !isErr || !appErr.HasRoute("modifyTags.TagsRequestInvalid") {
		t.Errorf("a range outside the text: %v", err)
	}
}

func TestParseOptions(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		want  map[string][]string
		route string
	}{
		{"none", "", map[string][]string{}, ""},
		{"one", "json=omitempty", map[string][]string{"json": {"omitempty"}}, ""},
		{"several for a tag and a tag more", "json=omitempty,json=string,xml=attr", map[string][]string{"json": {"omitempty", "string"}, "xml": {"attr"}}, ""},
		{"no equal sign", "json", nil, "parseOptions.TagsRequestInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseOptions(tt.text)
			if tt.route != "" {
				appErr, isErr := errors.FromError(err)
				if !isErr || !appErr.HasRoute(tt.route) {
					t.Fatalf("error = %v, want route %s", err, tt.route)
				}
				return
			}
			if err != nil || len(got) != len(tt.want) {
				t.Fatalf("= %v, %v", got, err)
			}
			for k, v := range tt.want {
				if !slices.Equal(got[k], v) {
					t.Errorf("%s = %v, want %v", k, got[k], v)
				}
			}
		})
	}
}

func TestSplitList(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil}, {"json", []string{"json"}}, {" json , xml ,,", []string{"json", "xml"}}, {"json:name,xml", []string{"json:name", "xml"}},
	}
	for _, tt := range tests {
		if got := splitList(tt.in); !slices.Equal(got, tt.want) {
			t.Errorf("splitList(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSplitWords(t *testing.T) {
	// the examples of fatih/camelcase
	tests := []struct {
		in   string
		want []string
	}{
		{"", []string{""}},
		{"lowercase", []string{"lowercase"}},
		{"Class", []string{"Class"}},
		{"MyClass", []string{"My", "Class"}},
		{"MyC", []string{"My", "C"}},
		{"HTML", []string{"HTML"}},
		{"PDFLoader", []string{"PDF", "Loader"}},
		{"AString", []string{"A", "String"}},
		{"SimpleXMLParser", []string{"Simple", "XML", "Parser"}},
		{"vimRPCPlugin", []string{"vim", "RPC", "Plugin"}},
		{"GL11Version", []string{"GL", "11", "Version"}},
		{"99Bottles", []string{"99", "Bottles"}},
		{"May5", []string{"May", "5"}},
		{"BFG9000", []string{"BFG", "9000"}},
		{"BöseÜberraschung", []string{"Böse", "Überraschung"}},
		{"Two  spaces", []string{"Two", "  ", "spaces"}},
		{"BadUTF8\xe2\xe2\xa1", []string{"BadUTF8\xe2\xe2\xa1"}},
	}
	for _, tt := range tests {
		if got := splitWords(tt.in); !slices.Equal(got, tt.want) {
			t.Errorf("splitWords(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestTextEdits(t *testing.T) {
	tests := []struct {
		name     string
		old, new string
		want     []extension.RangeEdit
	}{
		{"the same", "a\nb\n", "a\nb\n", nil},
		{"one line", "ab\ncd\nef\n", "ab\ncXd\nef\n", []extension.RangeEdit{{Range: rg(1, 1, 1, 1), Text: "X"}}},
		{"two lines, the part that changed", "x int\ny string\n", "x int `a`\ny string `b`\n", []extension.RangeEdit{
			{Range: rg(0, 5, 0, 5), Text: " `a`"}, {Range: rg(1, 8, 1, 8), Text: " `b`"}}},
		{"a character is not cut", "é\n", "ê\n", []extension.RangeEdit{{Range: rg(0, 0, 0, 2), Text: "ê"}}},
		{"more lines", "a\nb\nc\n", "a\nb\nX\nY\nc\n", []extension.RangeEdit{{Range: rg(2, 0, 2, 0), Text: "X\nY\n"}}},
		{"fewer lines", "a\nb\nc\n", "a\nc\n", []extension.RangeEdit{{Range: rg(1, 0, 2, 0), Text: ""}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := textEdits(tt.old, tt.new)
			if !slices.Equal(got, tt.want) {
				t.Errorf("textEdits = %+v, want %+v", got, tt.want)
			}
			if tt.want != nil {
				if applied := applyEdits(t, tt.old, got); applied != tt.new {
					t.Errorf("the edits make %q, want %q", applied, tt.new)
				}
			}
		})
	}
}

func rg(l1, c1, l2, c2 int) extension.Range {
	return extension.Range{Start: extension.Position{Line: l1, Col: c1}, End: extension.Position{Line: l2, Col: c2}}
}

// applyEdits applies edits, whose ranges are in the text.
func applyEdits(t *testing.T, text string, edits []extension.RangeEdit) string {
	t.Helper()
	return applyRangeEdits(text, edits)
}

func TestOffsetsAndPositions(t *testing.T) {
	text := "ab\ncé\n\nz"
	tests := []struct {
		offset int
		pos    extension.Position
	}{{0, pos(0, 0)}, {2, pos(0, 2)}, {3, pos(1, 0)}, {6, pos(1, 3)}, {7, pos(2, 0)}, {8, pos(3, 0)}, {9, pos(3, 1)}}
	for _, tt := range tests {
		if got := positionAt(text, tt.offset); got != tt.pos {
			t.Errorf("positionAt(%d) = %+v, want %+v", tt.offset, got, tt.pos)
		}
		if got := offsetOf(text, tt.pos); got != tt.offset {
			t.Errorf("offsetOf(%+v) = %d, want %d", tt.pos, got, tt.offset)
		}
	}
	// a position outside the text is moved into it
	if got := offsetOf(text, pos(9, 0)); got != len(text) {
		t.Errorf("a line past the end = %d", got)
	}
	if got := offsetOf(text, pos(0, 99)); got != 2 {
		t.Errorf("a column past the end of the line = %d", got)
	}
	if got := offsetOf(text, pos(0, -3)); got != 0 {
		t.Errorf("a negative column = %d", got)
	}
}

func pos(line, col int) extension.Position { return extension.Position{Line: line, Col: col} }
