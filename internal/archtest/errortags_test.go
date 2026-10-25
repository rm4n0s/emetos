package archtest

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/rm4n0s/errors"
)

const errorTagsFixture = "testdata/error_tags"

func TestErrorTags(t *testing.T) {
	sites, bad, err := ErrorTags(errorTagsFixture)
	if err != nil {
		t.Fatalf("ErrorTags: %v", err)
	}
	var got []string
	for _, s := range sites {
		got = append(got, s.Tag+" "+s.File+" "+s.Func)
	}
	want := []string{
		"ConstRoute internal/core/thing/thing.go Do",
		"ExtensionCallFailed extension/api.go Call",
		"HelperRoute internal/core/thing/thing.go Do",
		"LiteralRoute internal/core/thing/thing.go Do",
		"MethodTag internal/core/thing/thing.go Thing.Method",
		"NeverNamed internal/core/thing/thing.go Do",
		"NeverNamed internal/core/thing/thing.go Do",
		"OnlyAFake internal/core/thing/thing.go Do",
		"OnlyInComment internal/core/thing/thing.go Do",
		"OnlyNamed internal/gui/other/other.go Fail",
		"SentinelTag internal/core/thing/thing.go Do",
		"TableRoute internal/core/thing/thing.go Do",
		"TableTag internal/core/thing/thing.go Do",
		"ToolFailed cmd/tool/main.go main",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tags = %q\nwant %q", got, want)
	}
	if len(bad) != 1 || bad[0].File != "extensions/demo/demo.go" || bad[0].Rule != RuleNonLiteralTag || bad[0].Detail != "errors.New(tag, ...)" {
		t.Errorf("violations = %+v, want the tag of extensions/demo/demo.go", bad)
	}
}

func TestUntestedErrorTags(t *testing.T) {
	untested, bad, err := UntestedErrorTags(errorTagsFixture)
	if err != nil {
		t.Fatalf("UntestedErrorTags: %v", err)
	}
	var got []string
	for _, s := range untested {
		got = append(got, s.Tag)
	}
	sort.Strings(got)
	// Not in the list: LiteralRoute (a literal route), TableRoute (a table of routes), TableTag (a table of
	// tags compared through .Tag), SentinelTag (errors.Is), ConstRoute (a constant), HelperRoute (a helper that
	// builds the route from its arguments). In the list: a Tag named only in a comment, one made only by a
	// fake error of a test, one named in a folder whose tests assert nothing, and the Tags no test names.
	want := []string{"ExtensionCallFailed", "MethodTag", "NeverNamed", "NeverNamed", "OnlyAFake", "OnlyInComment", "OnlyNamed", "ToolFailed"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("untested = %q, want %q", got, want)
	}
	if len(bad) != 1 {
		t.Errorf("violations = %+v, want one", bad)
	}
}

func TestReadUntestedList(t *testing.T) {
	dir := t.TempDir()
	write := func(text string) string {
		path := filepath.Join(dir, "list.md")
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	tests := []struct {
		name string
		file string
		want map[string]string
	}{
		{"the fixture", errorTagsFixture + "/docs/ERROR-TAGS-UNTESTED.md", map[string]string{"OnlyNamed": "a fixture of the archtest tests"}},
		{"a file that is not there", filepath.Join(dir, "missing.md"), map[string]string{}},
		{"bullets and prose", write("# Title\n\nSome `prose` here.\n- `A`: first\n* `B`: second\n- not a tag: x\n- `C`\n"),
			map[string]string{"A": "first", "B": "second", "C": ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ReadUntestedList(tt.file)
			if err != nil {
				t.Fatalf("ReadUntestedList: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("list = %v, want %v", got, tt.want)
			}
		})
	}
	t.Run("a file that cannot be read", func(t *testing.T) {
		_, err := ReadUntestedList(dir) // a folder
		appErr, ok := errors.FromError(err)
		if !ok {
			t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
		}
		if !appErr.HasRoute("ReadUntestedList.ErrorTagsExceptionsReadFailed") || appErr.Metadata["path"] != dir {
			t.Errorf("unexpected failure path: %s, metadata %v", appErr.Route(), appErr.Metadata)
		}
	})
}

func TestErrorTagsErrors(t *testing.T) {
	tests := []struct {
		name  string
		run   func(root string) error
		route string
	}{
		{"ErrorTags", func(root string) error { _, _, err := ErrorTags(root); return err }, "ErrorTags->walkGo"},
		{"UntestedErrorTags", func(root string) error { _, _, err := UntestedErrorTags(root); return err }, "UntestedErrorTags->ErrorTags->walkGo"},
	}
	for _, tt := range tests {
		for _, root := range []struct{ name, path, tag string }{
			{"missing root", "testdata/does_not_exist", "ArchWalkFailed"},
			{"file that does not parse", brokenTree(t), "ArchParseFailed"},
		} {
			t.Run(tt.name+" "+root.name, func(t *testing.T) {
				appErr, ok := errors.FromError(tt.run(root.path))
				if !ok {
					t.Fatalf("expected *errors.Error")
				}
				if appErr.Tag != root.tag || !appErr.HasRoute(tt.route) || !appErr.HasRoute("walkGo.func1."+root.tag) {
					t.Errorf("Tag = %q, route = %s", appErr.Tag, appErr.Route())
				}
			})
		}
	}
}

// TestEveryErrorTagHasARouteTest is CLAUDE.md "Every error Tag has a route test": every Tag of the code is
// asserted by a test (UntestedErrorTags says what counts), except those docs/ERROR-TAGS-UNTESTED.md lists, and
// no Tag is built from a variable. scripts/error-tags.sh --untested is the same check on the command line.
func TestEveryErrorTagHasARouteTest(t *testing.T) {
	untested, bad, err := UntestedErrorTags("../..")
	if err != nil {
		t.Fatalf("UntestedErrorTags: %v", err)
	}
	for _, v := range bad {
		t.Errorf("%s:%d: %s: %s", v.File, v.Line, v.Rule, v.Detail)
	}
	excepted, err := ReadUntestedList("../../docs/ERROR-TAGS-UNTESTED.md")
	if err != nil {
		t.Fatalf("ReadUntestedList: %v", err)
	}
	seen := map[string]bool{}
	for _, s := range untested {
		seen[s.Tag] = true
		if _, ok := excepted[s.Tag]; !ok {
			t.Errorf("%s:%d: the Tag %s (%s) has no test that asserts its route", s.File, s.Line, s.Tag, s.Func)
		}
	}
	for tag := range excepted {
		if !seen[tag] {
			t.Errorf("docs/ERROR-TAGS-UNTESTED.md lists %s, which has a test or no longer exists: remove it", tag)
		}
	}
}
