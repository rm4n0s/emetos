package language

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/rm4n0s/errors"
)

func TestParseConfigurationReadsEveryField(t *testing.T) {
	c, problems := ParseConfiguration("test", []byte(`{
		// a comment
		"comments": {"lineComment": {"comment": "//", "noIndent": true}, "blockComment": ["/*", "*/"]},
		"brackets": [["{", "}"], ["[", "]"]],
		"autoClosingPairs": [["(", ")"], {"open": "'", "close": "'", "notIn": ["string", "comment"]}, {"open": "<", "close": ">", "beforeText": "\\s$"}],
		"surroundingPairs": [["(", ")"], {"open": "\"", "close": "\""}],
		"colorizedBracketPairs": [["(", ")"]],
		"autoCloseBefore": ";:.,=}])> \n\t",
		"wordPattern": "(-?\\d*\\.\\d\\w*)|([^\\s]+)",
		"indentationRules": {
			"increaseIndentPattern": "\\{\\s*$", "decreaseIndentPattern": {"pattern": "^\\s*\\}", "flags": "i"},
			"indentNextLinePattern": "^if\\b", "unIndentedLinePattern": "^//"},
		"folding": {"offSide": true, "markers": {"start": "^\\s*//#region", "end": "^\\s*//#endregion"}},
		"onEnterRules": [
			{"beforeText": "^\\s*/\\*\\*", "afterText": "^\\s*\\*/$", "previousLineText": "x", "action": {"indent": "indentOutdent", "appendText": " * "}},
			{"beforeText": "a", "action": {"indent": "outdent", "removeText": 1}},
			{"beforeText": "b", "action": {"indent": "indent"}},
			{"beforeText": "c", "action": {"indent": "none"}},
		],
	}`))
	if len(problems) != 0 {
		t.Fatalf("problems: %v", problems)
	}
	if c.Comments.LineComment != "//" || !c.Comments.LineCommentNoIndent || *c.Comments.BlockComment != (Pair{"/*", "*/"}) {
		t.Errorf("comments: %+v", c.Comments)
	}
	if !reflect.DeepEqual(c.Brackets, []Pair{{"{", "}"}, {"[", "]"}}) {
		t.Errorf("brackets: %v", c.Brackets)
	}
	if len(c.AutoClosingPairs) != 3 || c.AutoClosingPairs[1].NotIn[1] != "comment" || c.AutoClosingPairs[2].BeforeText == nil {
		t.Errorf("auto-closing pairs: %+v", c.AutoClosingPairs)
	}
	if !reflect.DeepEqual(c.SurroundingPairs, []Pair{{"(", ")"}, {"\"", "\""}}) || !reflect.DeepEqual(c.ColorizedBracketPairs, []Pair{{"(", ")"}}) {
		t.Errorf("surrounding %v, colorized %v", c.SurroundingPairs, c.ColorizedBracketPairs)
	}
	if c.AutoCloseBefore == nil || *c.AutoCloseBefore != ";:.,=}])> \n\t" {
		t.Errorf("autoCloseBefore: %v", c.AutoCloseBefore)
	}
	if c.WordPattern == nil || c.IndentationRules == nil || c.Folding == nil || len(c.OnEnterRules) != 4 {
		t.Fatalf("missing fields: %+v", c)
	}
	rules := c.IndentationRules
	if rules.IndentNextLinePattern == nil || rules.UnIndentedLinePattern == nil {
		t.Errorf("rules: %+v", rules)
	}
	if _, ok, _ := rules.DecreaseIndentPattern.Find([]rune("  }"), 0); !ok {
		t.Error("the decrease pattern matches")
	}
	if !c.Folding.OffSide || !c.Folding.HasOffSide || c.Folding.MarkersStart == nil || c.Folding.MarkersEnd == nil {
		t.Errorf("folding: %+v", c.Folding)
	}
	first := c.OnEnterRules[0]
	if first.AfterText == nil || first.PreviousLineText == nil || first.Action != (EnterAction{Indent: IndentOutdent, AppendText: " * "}) {
		t.Errorf("first rule: %+v", first)
	}
	if c.OnEnterRules[1].Action != (EnterAction{Indent: IndentOutdentOne, RemoveText: 1}) || c.OnEnterRules[2].Action.Indent != IndentIndent || c.OnEnterRules[3].Action.Indent != IndentNone {
		t.Errorf("actions: %+v", c.OnEnterRules)
	}
}

func TestParseConfigurationProblems(t *testing.T) {
	tests := []struct {
		name string
		json string
		tag  string
		// field is the field named in the problem, when the tag is ConfigurationFieldInvalid
		field string
	}{
		{"syntax error", `{"comments": }`, "ConfigurationSyntaxError", ""},
		{"not an object", `[]`, "ConfigurationNotObject", ""},
		{"comments not an object", `{"comments": 3}`, "ConfigurationFieldInvalid", "comments"},
		{"line comment of the wrong type", `{"comments": {"lineComment": 3}}`, "ConfigurationFieldInvalid", "comments.lineComment"},
		{"line comment object without comment", `{"comments": {"lineComment": {"noIndent": true}}}`, "ConfigurationFieldInvalid", "comments.lineComment.comment"},
		{"block comment of the wrong shape", `{"comments": {"blockComment": ["/*"]}}`, "ConfigurationFieldInvalid", "comments.blockComment"},
		{"brackets not an array", `{"brackets": 1}`, "ConfigurationFieldInvalid", "brackets"},
		{"a bracket pair of the wrong shape", `{"brackets": [["(", ")"], ["["]]}`, "ConfigurationFieldInvalid", "brackets[1]"},
		{"colorized pairs not an array", `{"colorizedBracketPairs": {}}`, "ConfigurationFieldInvalid", "colorizedBracketPairs"},
		{"auto closing pairs not an array", `{"autoClosingPairs": "x"}`, "ConfigurationFieldInvalid", "autoClosingPairs"},
		{"an auto closing pair of the wrong shape", `{"autoClosingPairs": [["("]]}`, "ConfigurationFieldInvalid", "autoClosingPairs[0]"},
		{"an auto closing pair that is a number", `{"autoClosingPairs": [4]}`, "ConfigurationFieldInvalid", "autoClosingPairs[0]"},
		{"an auto closing pair without open", `{"autoClosingPairs": [{"close": ")"}]}`, "ConfigurationFieldInvalid", "autoClosingPairs[0].open"},
		{"an auto closing pair without close", `{"autoClosingPairs": [{"open": "("}]}`, "ConfigurationFieldInvalid", "autoClosingPairs[0].close"},
		{"notIn of the wrong type", `{"autoClosingPairs": [{"open": "(", "close": ")", "notIn": [1]}]}`, "ConfigurationFieldInvalid", "autoClosingPairs[0].notIn"},
		{"beforeText that does not compile", `{"autoClosingPairs": [{"open": "(", "close": ")", "beforeText": "(["}]}`, "ConfigurationRegexInvalid", ""},
		{"surrounding pairs not an array", `{"surroundingPairs": 1}`, "ConfigurationFieldInvalid", "surroundingPairs"},
		{"a surrounding pair of the wrong shape", `{"surroundingPairs": [1]}`, "ConfigurationFieldInvalid", "surroundingPairs[0]"},
		{"a surrounding pair without open", `{"surroundingPairs": [{"close": ")"}]}`, "ConfigurationFieldInvalid", "surroundingPairs[0].open"},
		{"a surrounding pair without close", `{"surroundingPairs": [{"open": "("}]}`, "ConfigurationFieldInvalid", "surroundingPairs[0].close"},
		{"on enter rules not an array", `{"onEnterRules": 1}`, "ConfigurationFieldInvalid", "onEnterRules"},
		{"an on enter rule that is not an object", `{"onEnterRules": [1]}`, "ConfigurationFieldInvalid", "onEnterRules[0]"},
		{"an on enter rule without action", `{"onEnterRules": [{"beforeText": "a"}]}`, "ConfigurationFieldInvalid", "onEnterRules[0].action"},
		{"an on enter action of an unknown kind", `{"onEnterRules": [{"beforeText": "a", "action": {"indent": "sideways"}}]}`, "ConfigurationFieldInvalid", "onEnterRules[0].action.indent"},
		{"appendText of the wrong type", `{"onEnterRules": [{"beforeText": "a", "action": {"indent": "none", "appendText": 3}}]}`, "ConfigurationFieldInvalid", "onEnterRules[0].action.appendText"},
		{"removeText of the wrong type", `{"onEnterRules": [{"beforeText": "a", "action": {"indent": "none", "removeText": "x"}}]}`, "ConfigurationFieldInvalid", "onEnterRules[0].action.removeText"},
		{"an on enter rule without beforeText", `{"onEnterRules": [{"action": {"indent": "none"}}]}`, "ConfigurationFieldInvalid", "onEnterRules[0].beforeText"},
		{"afterText that does not compile", `{"onEnterRules": [{"beforeText": "a", "afterText": "([", "action": {"indent": "none"}}]}`, "ConfigurationRegexInvalid", ""},
		{"previousLineText that does not compile", `{"onEnterRules": [{"beforeText": "a", "previousLineText": "([", "action": {"indent": "none"}}]}`, "ConfigurationRegexInvalid", ""},
		{"indentationRules not an object", `{"indentationRules": 1}`, "ConfigurationFieldInvalid", "indentationRules"},
		{"indentationRules without the increase pattern", `{"indentationRules": {"decreaseIndentPattern": "a"}}`, "ConfigurationFieldInvalid", "indentationRules.increaseIndentPattern"},
		{"indentationRules without the decrease pattern", `{"indentationRules": {"increaseIndentPattern": "a"}}`, "ConfigurationFieldInvalid", "indentationRules.decreaseIndentPattern"},
		{"folding not an object", `{"folding": 1}`, "ConfigurationFieldInvalid", "folding"},
		{"a pattern object without pattern", `{"wordPattern": {"flags": "i"}}`, "ConfigurationFieldInvalid", "wordPattern.pattern"},
		{"flags of the wrong type", `{"wordPattern": {"pattern": "a", "flags": 1}}`, "ConfigurationFieldInvalid", "wordPattern.flags"},
		{"a pattern that is a number", `{"wordPattern": 3}`, "ConfigurationFieldInvalid", "wordPattern"},
		{"flags that do not exist", `{"wordPattern": {"pattern": "a", "flags": "z"}}`, "ConfigurationRegexInvalid", ""},
		{"a word pattern that does not compile", `{"wordPattern": "(["}`, "ConfigurationRegexInvalid", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, problems := ParseConfiguration("test", []byte(tc.json))
			var found bool
			for _, p := range problems {
				appErr, ok := errors.FromError(p)
				if !ok {
					t.Fatalf("problem %v is not an *errors.Error", p)
				}
				if appErr.Tag != tc.tag {
					continue
				}
				if tc.field == "" || appErr.Metadata["field"] == tc.field {
					found = true
				}
			}
			if !found {
				t.Errorf("want %s %s in %v", tc.tag, tc.field, problems)
			}
		})
	}
	// An empty file is no configuration and no problem.
	if c, problems := ParseConfiguration("test", []byte("  ")); len(problems) != 0 || c == nil {
		t.Errorf("empty: %v", problems)
	}
	// Regular expression flags that only matter to JavaScript are accepted.
	if c, problems := ParseConfiguration("test", []byte(`{"wordPattern": {"pattern": "\\w+", "flags": "gimsuy"}}`)); len(problems) != 0 || c.WordPattern == nil {
		t.Errorf("flags: %v", problems)
	}
}

func TestConfigurationsAreMerged(t *testing.T) {
	a, _ := ParseConfiguration("x", []byte(`{"comments": {"lineComment": "#"}, "brackets": [["(", ")"]], "autoCloseBefore": "a"}`))
	b, _ := ParseConfiguration("x", []byte(`{"comments": {"lineComment": "//"}, "wordPattern": "\\w+", "surroundingPairs": [["[", "]"]], "folding": {"offSide": true}}`))
	m := a.Merge(b)
	if m.Comments.LineComment != "//" || len(m.Brackets) != 1 || *m.AutoCloseBefore != "a" || m.WordPattern == nil || m.SurroundingPairs == nil || m.Folding == nil {
		t.Errorf("merged: %+v", m)
	}
	if a.Comments.LineComment != "#" {
		t.Error("merging changed the first configuration")
	}
	all, _ := ParseConfiguration("x", []byte(`{"autoClosingPairs": [["(", ")"]], "colorizedBracketPairs": [], "indentationRules": {"increaseIndentPattern": "a", "decreaseIndentPattern": "b"}, "onEnterRules": [{"beforeText": "a", "action": {"indent": "none"}}]}`))
	m = (&Configuration{}).Merge(all)
	if m.AutoClosingPairs == nil || m.ColorizedBracketPairs == nil || m.IndentationRules == nil || m.OnEnterRules == nil {
		t.Errorf("merged: %+v", m)
	}
}

func TestTheRegistryGivesTheMergedConfiguration(t *testing.T) {
	r := NewRegistry()
	files := map[string]string{
		"a.json":   `{"comments": {"lineComment": "#"}, "brackets": [["(", ")"]]}`,
		"b.json":   `{"comments": {"lineComment": "//"}}`,
		"bad.json": `{"comments": 1}`,
	}
	if err := r.AddExtension(ext(`{"contributes": {"languages": [{"id": "x", "configuration": "./a.json"}, {"id": "y", "configuration": "./missing.json"}, {"id": "z", "configuration": "./bad.json"}]}}`, files)); err != nil {
		t.Fatal(err)
	}
	if err := r.AddExtension(ext(`{"contributes": {"languages": [{"id": "x", "configuration": "./b.json"}]}}`, files)); err != nil {
		t.Fatal(err)
	}
	c := r.Configuration("x")
	if c.Comments.LineComment != "//" || len(c.Brackets) != 1 {
		t.Errorf("x: %+v", c.Comments)
	}
	if r.Configuration("x") != c {
		t.Error("the configuration is made once")
	}
	if c := r.Configuration("nope"); c == nil || c.Comments != nil {
		t.Errorf("a language without a configuration has an empty one: %+v", c)
	}
	r.Configuration("y")
	r.Configuration("z")
	tags := map[string]bool{}
	for _, p := range r.Problems() {
		tag, _ := errorsFrom(p)
		tags[tag] = true
	}
	if !tags["ConfigurationFileUnreadable"] || !tags["ConfigurationFieldInvalid"] {
		t.Errorf("problems: %v", r.Problems())
	}
	for _, p := range r.Problems() {
		if tag, _ := errorsFrom(p); tag == "ConfigurationFileUnreadable" {
			wantTag(t, p, "Registry.Configuration.ConfigurationFileUnreadable")
		}
	}
}

// referenceExtensions is the folder of the VS Code extensions of the reference.
func referenceExtensions(t *testing.T) string {
	t.Helper()
	dir := filepath.Join("..", "..", "..", "reference", "vscode", "extensions")
	if _, err := os.Stat(dir); err != nil {
		t.Skip("reference/ is missing: run scripts/fetch-reference.sh")
	}
	return dir
}

func TestEveryConfigurationFileOfVSCodeLoads(t *testing.T) {
	dir := referenceExtensions(t)
	files, err := filepath.Glob(filepath.Join(dir, "*", "*language-configuration.json"))
	if err != nil {
		t.Fatal(err)
	}
	more, _ := filepath.Glob(filepath.Join(dir, "..", "..", "vscode-go", "extension", "languages", "*.json"))
	files = append(files, more...)
	if len(files) < 40 {
		t.Fatalf("only %d configuration files found", len(files))
	}
	var withRules, withWord, withOnEnter int
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		c, problems := ParseConfiguration(filepath.Base(filepath.Dir(f)), data)
		for _, p := range problems {
			t.Errorf("%s: %v", f, p)
		}
		if c.IndentationRules != nil {
			withRules++
		}
		if c.WordPattern != nil {
			withWord++
		}
		if len(c.OnEnterRules) > 0 {
			withOnEnter++
		}
	}
	t.Logf("%d files; %d with indentation rules, %d with a word pattern, %d with on-enter rules", len(files), withRules, withWord, withOnEnter)
	if withRules == 0 || withWord == 0 || withOnEnter == 0 {
		t.Error("the sample must have indentation rules, word patterns and on-enter rules")
	}
}

func TestTheConfigurationOfGo(t *testing.T) {
	dir := referenceExtensions(t)
	data, err := os.ReadFile(filepath.Join(dir, "go", "language-configuration.json"))
	if err != nil {
		t.Fatal(err)
	}
	c, problems := ParseConfiguration("go", data)
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	if c.Comments.LineComment != "//" || c.Comments.BlockComment.Open != "/*" || len(c.Brackets) != 3 || c.IndentationRules == nil || c.Folding == nil || c.Folding.MarkersStart == nil {
		t.Errorf("go: %+v", c)
	}
	if _, ok, _ := c.IndentationRules.IncreaseIndentPattern.Find([]rune("func main() {"), 0); !ok {
		t.Error("the increase pattern matches a line that opens a block")
	}
	if _, ok, _ := c.Folding.MarkersStart.Find([]rune("// #region x"), 0); !ok {
		t.Error("the folding start marker matches a region comment")
	}
}
