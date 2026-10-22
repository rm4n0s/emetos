package archtest

import (
	"os"
	"path/filepath"
	"reflect"
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
		{"network packages", "network_import", []want{
			{"internal/gui/app/bad.go", 4, RuleNetworkImport, "crypto/tls"},
			{"internal/gui/app/bad.go", 5, RuleNetworkImport, "net/http"},
			{"internal/gui/app/bad.go", 6, RuleNetworkImport, "net/rpc"},
		}},
		{"net outside ipc", "net_outside_ipc", []want{
			{"internal/core/storage/bad.go", 3, RuleNetOutsideIPC, "net"},
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
		{"missing root", "testdata/does_not_exist", "Check", "ArchWalkFailed"},
		{"file that does not parse", brokenTree(t), "Check", "ArchParseFailed"},
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
