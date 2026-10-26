package golang

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
)

// The functions go test runs and the code lenses above them (step 14.3).

const lensSource = `package sample

import (
	"testing"
)

func TestAdd(t *testing.T) {
	t.Run("sub", func(t *testing.T) {})
}

func BenchmarkAdd(b *testing.B) {
}

func ExampleAdd() {
}

func FuzzAdd(f *testing.F) {}

func helper(t *testing.T) {}

func Testlower(t *testing.T) {}

func TestWrong(t *testing.B) {}

func TestNoParam() {}

func (s *Suite) TestMethod(t *testing.T) {}

func TestGeneric[T any](t *testing.T) {}

func Test_underscore(t *testing.T) {}

func Test(t *testing.T) {}

func ExampleWithArg(x int) {}
`

func TestScanTestFileFindsTheFunctionsGoTestRuns(t *testing.T) {
	file := scanTestFile(lensSource)
	var got []string
	for _, f := range file.funcs {
		got = append(got, f.name+"/"+[]string{"test", "benchmark", "example", "fuzz"}[f.kind])
	}
	want := []string{"TestAdd/test", "BenchmarkAdd/benchmark", "ExampleAdd/example", "FuzzAdd/fuzz", "Test_underscore/test", "Test/test"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("functions = %v, want %v", got, want)
	}
	if !file.hasPkg || file.pkgLine != 0 || file.pkgCol != 0 {
		t.Errorf("package clause at %d:%d (%v)", file.pkgLine, file.pkgCol, file.hasPkg)
	}
	add := file.funcs[0]
	if add.startLine != 6 || add.endLine != 8 || add.col != 0 || add.nameEnd != len("func TestAdd") {
		t.Errorf("TestAdd = %+v", add)
	}
	if names := file.tests(); !reflect.DeepEqual(names, []string{"TestAdd", "ExampleAdd", "FuzzAdd", "Test_underscore", "Test"}) {
		t.Errorf("tests = %v", names)
	}
}

func TestScanTestFileHandlesTheImportsOfTesting(t *testing.T) {
	tests := []struct {
		name, src string
		want      []string
	}{
		{"a renamed import", "package a\nimport tt \"testing\"\nfunc TestA(t *tt.T) {}\nfunc TestB(t *testing.T) {}\n", []string{"TestA"}},
		{"a dot import", "package a\nimport . \"testing\"\nfunc TestA(t *T) {}\nfunc BenchmarkA(b *B) {}\n", []string{"TestA", "BenchmarkA"}},
		{"no import of testing", "package a\nfunc TestA(t *testing.T) {}\n", nil},
		{"a syntax error below keeps the functions above", "package a\nimport \"testing\"\nfunc TestA(t *testing.T) {}\nfunc TestB(t *testing.T) {\n", []string{"TestA", "TestB"}},
		{"not Go at all", "this is not Go", nil},
		{"an unnamed parameter", "package a\nimport \"testing\"\nfunc TestA(*testing.T) {}\nfunc TestB(a, b *testing.T) {}\n", []string{"TestA"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			for _, f := range scanTestFile(tt.src).funcs {
				got = append(got, f.name)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("functions = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAtFindsTheFunctionOfALine(t *testing.T) {
	file := scanTestFile(lensSource)
	for line, want := range map[int]string{6: "TestAdd", 7: "TestAdd", 8: "TestAdd", 11: "BenchmarkAdd", 13: "ExampleAdd", 5: "", 9: "", 100: ""} {
		fn, ok := file.at(line)
		if (want == "") == ok || fn.name != want {
			t.Errorf("at(%d) = %q, %v, want %q", line, fn.name, ok, want)
		}
	}
}

func TestRunPattern(t *testing.T) {
	for names, want := range map[string]string{"TestA": "^TestA$", "TestA,TestB": "^(TestA|TestB)$", "TestA,ExampleB,FuzzC": "^(TestA|ExampleB|FuzzC)$"} {
		if got := runPattern(strings.Split(names, ",")); got != want {
			t.Errorf("runPattern(%s) = %q, want %q", names, got, want)
		}
	}
}

// lensView is a lens as a test reads it.
type lensView struct {
	Title    string
	ID       string
	Args     string
	Line     int
	Disabled bool
}

func lensViews(t *testing.T, lenses []extension.CodeLens) []lensView {
	t.Helper()
	var out []lensView
	for _, l := range lenses {
		v := lensView{Title: l.Command.Title, ID: l.Command.ID, Line: l.Range.Start.Line, Disabled: l.Disabled}
		if len(l.Command.Arguments) > 0 {
			v.Args = string(l.Command.Arguments[0])
		}
		out = append(out, v)
	}
	return out
}

func TestTheLensesOfATestFile(t *testing.T) {
	r := newRig(t, rigOptions{})
	provider := goLenses{r.ext}
	doc := extension.TextDocument{URI: "file:///w/sample_test.go", LanguageID: "go", Text: lensSource}
	lenses, err := provider.ProvideCodeLenses(context.Background(), doc)
	if err != nil {
		t.Fatal(err)
	}
	const args = `{"uri":"file:///w/sample_test.go"`
	want := []lensView{
		{"run package tests", TestPackageCommand, args + `}`, 0, false},
		{"run file tests", TestFileCommand, args + `}`, 0, false},
		{"run test", TestFunctionCommand, args + `,"name":"TestAdd"}`, 6, false},
		{"debug test", "", "", 6, true},
		{"run benchmark", TestFunctionCommand, args + `,"name":"BenchmarkAdd"}`, 10, false},
		{"debug benchmark", "", "", 10, true},
		{"run test", TestFunctionCommand, args + `,"name":"ExampleAdd"}`, 13, false},
		{"debug test", "", "", 13, true},
		{"run test", TestFunctionCommand, args + `,"name":"FuzzAdd"}`, 16, false},
		{"debug test", "", "", 16, true},
		{"run test", TestFunctionCommand, args + `,"name":"Test_underscore"}`, 30, false},
		{"debug test", "", "", 30, true},
		{"run test", TestFunctionCommand, args + `,"name":"Test"}`, 32, false},
		{"debug test", "", "", 32, true},
	}
	if got := lensViews(t, lenses); !reflect.DeepEqual(got, want) {
		t.Errorf("lenses =\n%+v\nwant\n%+v", got, want)
	}
	// the lens sits on the keyword and the name: the range of the first test
	if rg := lenses[2].Range; rg.Start.Col != 0 || rg.End.Col != len("func TestAdd") || rg.End.Line != 6 {
		t.Errorf("range = %+v", rg)
	}
	// the arguments are what the command takes
	var a map[string]any
	if err := json.Unmarshal(lenses[2].Command.Arguments[0], &a); err != nil || a["name"] != "TestAdd" {
		t.Errorf("arguments = %v, %v", a, err)
	}
}

func TestNoLensesOutsideTestFilesOrWithTheSettingOff(t *testing.T) {
	r := newRig(t, rigOptions{})
	provider := goLenses{r.ext}
	ctx := context.Background()
	for name, doc := range map[string]extension.TextDocument{
		"a source file":             {URI: "file:///w/sample.go", Text: lensSource},
		"a test file with no tests": {URI: "file:///w/x_test.go", Text: "package a\n\nfunc helper() {}\n"},
		"an empty test file":        {URI: "file:///w/y_test.go", Text: ""},
	} {
		if lenses, err := provider.ProvideCodeLenses(ctx, doc); err != nil || len(lenses) != 0 {
			t.Errorf("%s: lenses = %v, %v", name, lenses, err)
		}
	}
	// go.enableCodeLens {"runtest": false} takes them away, and the editors are asked again
	before := r.api.CodeLensRefreshes()
	r.api.SetSetting(EnableCodeLensSetting, map[string]any{"runtest": false})
	if lenses, _ := provider.ProvideCodeLenses(ctx, extension.TextDocument{URI: "file:///w/sample_test.go", Text: lensSource}); len(lenses) != 0 {
		t.Errorf("%d lenses with runtest off", len(lenses))
	}
	if got := r.api.CodeLensRefreshes(); got != before+1 {
		t.Errorf("%d refreshes after the setting changed, want %d", got, before+1)
	}
	r.api.SetSetting(EnableCodeLensSetting, map[string]any{"runtest": true})
	if lenses, _ := provider.ProvideCodeLenses(ctx, extension.TextDocument{URI: "file:///w/sample_test.go", Text: lensSource}); len(lenses) == 0 {
		t.Error("no lenses with runtest on again")
	}
	if got := r.api.CodeLensRefreshes(); got != before+2 {
		t.Errorf("%d refreshes", got)
	}
	// a change of another setting does not refresh them
	r.api.SetSetting(TestTimeoutSetting, "1m")
	if got := r.api.CodeLensRefreshes(); got != before+2 {
		t.Errorf("%d refreshes after an unrelated setting", got)
	}
}

func TestTheExtensionRegistersItsLensProviderForGo(t *testing.T) {
	r := newRig(t, rigOptions{})
	var found bool
	for _, p := range r.api.Providers() {
		if p.Feature == "codeLens" {
			found = true
			if !reflect.DeepEqual(p.Selector.Languages, []string{"go"}) || p.Disposed {
				t.Errorf("registration = %+v", p)
			}
		}
	}
	if !found {
		t.Error("no code lens provider was registered")
	}
}
