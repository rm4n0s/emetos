package settings

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/jsonc"
)

func TestDefaultSettingsJSON(t *testing.T) {
	reg := DefaultRegistry()
	text := DefaultSettingsJSON(reg)
	if text != DefaultSettingsJSON(reg) {
		t.Error("the document changes from one call to the next")
	}
	if strings.Contains(text, "\r") || !strings.HasSuffix(text, "}\n") || !strings.HasPrefix(text, "{\n") {
		t.Errorf("the shape: %q ... %q", text[:10], text[len(text)-10:])
	}
	// valid JSONC, strictly: no trailing comma
	value, errs := jsonc.Parse(text, jsonc.ParseOptions{DisallowTrailingComma: true})
	if len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	root, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("the document is %T", value)
	}
	tree, _ := jsonc.ParseTree(text, jsonc.ParseOptions{})
	if got, want := len(tree.Children), len(reg.All()); got != want {
		t.Errorf("%d properties, %d settings", got, want)
	}
	// every setting once, with its default
	seen := map[string]int{}
	for _, child := range tree.Children {
		seen[child.Children[0].Value.(string)]++
	}
	for _, s := range reg.All() {
		if seen[s.Key] != 1 {
			t.Errorf("%s is written %d times", s.Key, seen[s.Key])
			continue
		}
		data, err := json.Marshal(s.Default)
		if err != nil {
			t.Fatal(err)
		}
		var want any
		if err := json.Unmarshal(data, &want); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(root[s.Key], want) {
			t.Errorf("%s = %#v, the default is %#v", s.Key, root[s.Key], want)
		}
	}
	// the settings come in the order of the groups of the settings editor
	order := map[string]int{}
	n := 0
	for _, c := range BuildCategories(DefaultTOC(), reg.All()) {
		if c.ID == CommonlyUsedID {
			continue
		}
		c.Walk(func(c *Category) {
			for _, k := range c.Keys {
				order[k] = n
				n++
			}
		})
	}
	last := -1
	for _, child := range tree.Children {
		key := child.Children[0].Value.(string)
		if order[key] < last {
			t.Errorf("%s is out of order", key)
		}
		last = order[key]
	}
}

func TestDefaultSettingsJSONComments(t *testing.T) {
	reg := NewRegistry()
	reg.MustRegister(
		Setting{Key: "editor.fontSize", Types: []Type{Number}, Default: 14, Description: "Controls the font size in pixels.", Minimum: Float(6), Maximum: Float(100)},
		Setting{Key: "editor.fontFamily", Types: []Type{String}, Default: "Droid Sans Mono", Description: "Controls the font family."},
		Setting{Key: "editor.long", Types: []Type{Boolean}, Default: true, Description: "word " + strings.Repeat("alpha beta gamma delta ", 12) + "\nsecond line"},
		Setting{Key: "files.autoSave", Types: []Type{String}, Default: "off", Enum: []any{"off", "afterDelay"}, EnumDescriptions: []string{"Never.", "After a delay."}, Description: "Controls auto save."},
		Setting{Key: "files.exclude", Types: []Type{Object}, Default: map[string]any{"**/.git": true, "**/a": false}, Description: "Globs."},
		Setting{Key: "files.list", Types: []Type{Array}, Default: []any{"a", 1.5}, Description: "A list."},
		Setting{Key: "workbench.iconTheme", Types: []Type{String, Null}, Default: nil, Description: "Theme."},
		Setting{Key: "window.old", Types: []Type{Boolean}, Default: false, Description: "Old.", Deprecated: "Use window.new."},
	)
	text := DefaultSettingsJSON(reg)
	for _, want := range []string{
		"\t// Text Editor\n",
		"\t// Text Editor > Font\n",
		"\t// Controls the font size in pixels.\n\t// Minimum: 6\n\t// Maximum: 100\n\t\"editor.fontSize\": 14,\n",
		"\t// Controls the font family.\n\t\"editor.fontFamily\": \"Droid Sans Mono\",\n",
		"\t// Controls auto save.\n\t// Allowed values: \"off\", \"afterDelay\"\n\t//   \"off\": Never.\n\t//   \"afterDelay\": After a delay.\n\t\"files.autoSave\": \"off\",\n",
		"\t\"files.exclude\": {\n\t\t\"**/.git\": true,\n\t\t\"**/a\": false\n\t},\n",
		"\t\"files.list\": [\n\t\t\"a\",\n\t\t1.5\n\t],\n",
		"\t\"workbench.iconTheme\": null,\n",
		"\t// Deprecated: Use window.new.\n\t\"window.old\": false\n}\n",
		"\t// second line\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the document lacks %q:\n%s", want, text)
		}
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "\t// ") && len(strings.ReplaceAll(line, "\t", "    ")) > defaultSettingsWrap+3 {
			t.Errorf("the comment line is too long (%d): %q", len(line), line)
		}
	}
	if _, errs := jsonc.Parse(text, jsonc.ParseOptions{DisallowTrailingComma: true}); len(errs) > 0 {
		t.Errorf("parse errors %v", errs)
	}
	// an empty registry is an empty object
	if got := DefaultSettingsJSON(NewRegistry()); got != "{\n}\n" {
		t.Errorf("empty registry: %q", got)
	}
}

func TestWrapWords(t *testing.T) {
	for _, c := range []struct {
		in    string
		width int
		want  []string
	}{
		{"", 10, []string{""}},
		{"short", 10, []string{"short"}},
		{"aaa bbb ccc", 7, []string{"aaa bbb", "ccc"}},
		{"aaaaaaaaaaaa b", 5, []string{"aaaaaaaaaaaa", "b"}},
		{"  indented words here", 14, []string{"  indented", "  words here"}},
	} {
		if got := wrapWords(c.in, c.width); !reflect.DeepEqual(got, c.want) {
			t.Errorf("wrapWords(%q, %d) = %q, want %q", c.in, c.width, got, c.want)
		}
	}
}
