package archtest

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
)

func TestCheck(t *testing.T) {
	type want struct {
		file string
		line int
		rule Rule
		det  string
	}
	tests := []struct {
		name    string
		fixture string
		want    []want
	}{
		{"clean tree", "clean", nil},
		{"core imports glutter", "core_imports_glutter", []want{
			{"internal/core/buffer/bad.go", 3, RuleCoreImportsUI, "github.com/rm4n0s/glutter/widgets"},
		}},
		{"core imports gui", "core_imports_gui", []want{
			{"internal/core/edit/bad.go", 3, RuleCoreImportsUI, "github.com/rm4n0s/emetos/internal/gui/kit"},
		}},
		{"import C", "import_c", []want{
			{"internal/gui/app/bad.go", 4, RuleImportC, "C"},
		}},
		{"regexp2 outside regex", "regexp2_outside", []want{
			{"internal/core/find/bad.go", 3, RuleRegexp2Outside, "github.com/dlclark/regexp2"},
		}},
		{"standard errors", "std_errors", []want{
			{"internal/core/fileio/bad.go", 3, RuleStdErrors, "errors"},
		}},
		{"fmt.Errorf with and without an alias", "fmt_errorf", []want{
			{"internal/core/fileio/bad.go", 8, RuleFmtErrorf, "fmt.Errorf"},
			{"internal/core/fileio/bad.go", 9, RuleFmtErrorf, "f.Errorf"},
		}},
		{"a local value called fmt", "fmt_errorf_shadow", nil},
		{"package-level errors, in code and in tests", "package_level_error", []want{
			{"internal/core/fileio/bad.go", 9, RulePackageLevelError, "errors.New"},
			{"internal/core/fileio/bad.go", 12, RulePackageLevelError, "errors.NewErr"},
			{"internal/core/fileio/bad.go", 13, RulePackageLevelError, "errors.NewErrf"},
			{"internal/core/fileio/bad_test.go", 5, RulePackageLevelError, "e.New"},
		}},
		{"functions that only build an error", "error_helper", []want{
			{"internal/core/emmet/helper.go", 11, RuleErrorHelper, "fail"},
			{"internal/core/emmet/helper.go", 16, RuleErrorHelper, "errNoWindow"},
			{"internal/core/emmet/helper.go", 21, RuleErrorHelper, "wrap"},
			{"internal/core/emmet/helper.go", 64, RuleErrorHelper, "fail"},
		}},
		{"errors of Emetos wrapped again", "project_error_rewrapped", []want{
			{"internal/gui/app/bad.go", 16, RuleProjectErrorRewrapped, "fileio.Read"},
			{"internal/gui/app/bad.go", 23, RuleProjectErrorRewrapped, "fileio.Write"},
			{"internal/gui/app/bad.go", 31, RuleProjectErrorRewrapped, "plistpkg.Parse"},
			{"internal/gui/app/bad.go", 45, RuleProjectErrorRewrapped, "fileio.Read"},
		}},
		{"tags that are not string literals", "non_literal_tag", []want{
			{"internal/core/regex/alias.go", 6, RuleNonLiteralTag, "xerr.NewErrf(t, ...)"},
			{"internal/core/regex/bad.go", 11, RuleNonLiteralTag, "errors.New(tag, ...)"},
			{"internal/core/regex/bad.go", 18, RuleNonLiteralTag, "errors.NewErr(failTag, ...)"},
			{"internal/core/regex/bad.go", 23, RuleNonLiteralTag, "errors.New(tagConst, ...)"},
		}},
		{"the terminal emulator imports a UI, a package of Emetos or another module", "emulator_imports_ui", []want{
			{"extensions/terminal/emulator/bad.go", 6, RuleEmulatorImportsUI, "github.com/rm4n0s/emetos/extension"},
			{"extensions/terminal/emulator/bad.go", 7, RuleEmulatorImportsUI, "github.com/rm4n0s/emetos/extensions/internal/pty"},
			{"extensions/terminal/emulator/bad.go", 8, RuleEmulatorImportsUI, "github.com/rm4n0s/glutter/widgets"},
			{"extensions/terminal/emulator/bad.go", 9, RuleEmulatorImportsUI, "golang.org/x/sys/unix"},
			{"extensions/terminal/emulator/bad_test.go", 5, RuleEmulatorImportsUI, "github.com/rm4n0s/emetos/internal/gui/kit"},
			{"extensions/terminal/emulator/bad_test.go", 5, RuleExtensionImportsInternal, "github.com/rm4n0s/emetos/internal/gui/kit"},
		}},
		{"an extension imports the editor", "extension_imports_internal", []want{
			{"extensions/golang/bad.go", 5, RuleExtensionImportsInternal, "github.com/rm4n0s/emetos/internal/core/buffer"},
			{"extensions/golang/bad.go", 6, RuleExtensionImportsInternal, "github.com/rm4n0s/emetos/internal/gui/kit"},
			{"extensions/golang/cmd.go", 3, RuleExtensionImportsInternal, "github.com/rm4n0s/emetos/cmd/emetos"},
		}},
		{"an extension imports another extension, but not its own subpackages, extensions/internal or the terminal emulator", "extension_imports_extension", []want{
			{"extensions/golang/bad.go", 7, RuleExtensionImportsExtension, "github.com/rm4n0s/emetos/extensions/markdown"},
		}},
		{"an extension imports a module nobody allowed", "extension_forbidden_dependency", []want{
			{"extensions/golang/bad.go", 7, RuleExtensionImportsForbidden, "github.com/rm4n0s/glutter/internal/engine"},
			{"extensions/golang/bad.go", 9, RuleExtensionImportsForbidden, "github.com/someone/lib"},
			{"extensions/golang/bad.go", 11, RuleExtensionImportsForbidden, "gopkg.in/yaml.v3"},
		}},
		{"the contract and extension/ui import the editor", "extension_contract_imports_internal", []want{
			{"extension/bad.go", 4, RuleExtensionContractImportsEmetos, "github.com/rm4n0s/emetos/internal/core/buffer"},
			{"extension/bad.go", 5, RuleExtensionContractImportsEmetos, "github.com/rm4n0s/emetos/internal/gui/kit"},
			{"extension/ui/bad.go", 4, RuleExtensionUIImportsInternal, "github.com/rm4n0s/emetos/internal/core/buffer"},
			{"extension/ui/bad.go", 5, RuleExtensionUIImportsInternal, "github.com/rm4n0s/emetos/internal/gui/testutil"},
			{"extension/ui/bad.go", 6, RuleExtensionUIImportsInternal, "github.com/rm4n0s/emetos/internal/gui/workbench"},
			{"extension/ui/uitest/bad.go", 3, RuleExtensionUIImportsInternal, "github.com/rm4n0s/emetos/internal/gui/app"},
		}},
		{"the editor imports the extensions", "extensions_imported_by_internal", []want{
			{"internal/core/language/bad_test.go", 3, RuleExtensionsImportedByInternal, "github.com/rm4n0s/emetos/extensions/golang"},
			{"internal/gui/app/bad.go", 4, RuleExtensionsImportedByInternal, "github.com/rm4n0s/emetos/extensions"},
			{"internal/gui/app/bad.go", 5, RuleExtensionsImportedByInternal, "github.com/rm4n0s/emetos/extensions/golang"},
		}},
		{"reference and testdata folders are skipped", "skipped_dirs", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Check("testdata/" + tt.fixture)
			if err != nil {
				t.Fatalf("Check: %v", err)
			}
			var gotWant []want
			for _, v := range got {
				gotWant = append(gotWant, want{v.File, v.Line, v.Rule, v.Detail})
			}
			if !reflect.DeepEqual(gotWant, tt.want) {
				t.Errorf("violations = %+v, want %+v", gotWant, tt.want)
			}
		})
	}
}

// TestRepository is the boundary test of the real repository: the rules of SPEC
// §5.3 hold for every package.
func TestRepository(t *testing.T) {
	got, err := Check("../..")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	for _, v := range got {
		t.Errorf("%s:%d: %s: %s", v.File, v.Line, v.Rule, v.Detail)
	}
}

func TestCheckErrors(t *testing.T) {
	tests := []struct {
		name  string
		root  string
		route string
		tag   string
	}{
		{"missing root", "testdata/does_not_exist", "Check->walkGo", "ArchWalkFailed"},
		{"file that does not parse", brokenTree(t), "Check->walkGo", "ArchParseFailed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Check(tt.root)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if appErr.Tag != tt.tag {
				t.Errorf("Tag = %q, want %q", appErr.Tag, tt.tag)
			}
			if !appErr.HasRoute(tt.route) || !appErr.HasRoute(tt.tag) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
}

// brokenTree makes a tree with a Go file that doesn't parse. It is made at test
// time, because gofmt would report a committed one.
func brokenTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "internal", "core", "buffer")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "broken.go"), []byte("package buffer\n\nfunc broken( {\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// TestAPackageOfTheBridgeMayUseTheKit pins what step 11.6 relies on: extension/ui/markdown, a subpackage of extension/ui,
// imports the contract and the kit like extension/ui does, and nothing else of the editor.
func TestAPackageOfTheBridgeMayUseTheKit(t *testing.T) {
	root := filepath.Join("testdata", "clean")
	got, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range got {
		if strings.HasPrefix(v.File, "extension/ui/markdown/") {
			t.Errorf("the bridge's subpackage breaks a rule: %+v", v)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "extension", "ui", "markdown", "ok.go")); err != nil {
		t.Errorf("the fixture is missing: %v", err)
	}
}
