package cpp

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The readers of CTest's formats (step 26.9) on the output recorded from the real ctest (testdata/ctest, CMake 4.3.0; {{dir}} is the
// folder of the project), and on lists and reports written by hand for what the recorded project does not have.

// fixture reads a recorded file with {{dir}} replaced.
func fixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "ctest", name))
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(data), "{{dir}}", "/proj")
}

func TestTheListOfTheRealCTestIsRead(t *testing.T) {
	got, err := readTestList(fixture(t, "show.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := []ctestTest{
		{Name: "adds", Command: []string{"/proj/build/adds"}, Dir: "/proj/build", File: "/proj/CMakeLists.txt", Line: 9},
		{Name: "divides", Command: []string{"/proj/build/divides", "5", "2"}, Dir: "/proj/build", File: "/proj/CMakeLists.txt", Line: 10},
		{Name: "skipped", Command: []string{"/proj/build/skipped"}, Dir: "/proj/build", File: "/proj/CMakeLists.txt", Line: 11},
		{Name: "disabled", Command: []string{"/proj/build/adds"}, Dir: "/proj/build", File: "/proj/CMakeLists.txt", Line: 12, Disabled: true},
		{Name: "math.lists", Command: []string{"/proj/build/adds"}, Dir: "/proj/src", File: "/proj/CMakeLists.txt", Line: 13, Labels: []string{"fast", "math"}},
		{Name: "odd[1]+x(2)", Command: []string{"/proj/build/adds"}, Dir: "/proj/build", File: "/proj/CMakeLists.txt", Line: 14},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tests:\n%+v\nwant:\n%+v", got, want)
	}
}

func TestListsWrittenByHandAreRead(t *testing.T) {
	const head = `{"kind":"ctestInfo","version":{"major":1,"minor":0},"backtraceGraph":{"commands":["add_test"],"files":["/p/a.cmake"],"nodes":[{"file":0},{"file":0,"line":7,"parent":0}]},"tests":`
	for _, tc := range []struct {
		name string
		text string
		want []ctestTest
	}{
		{"nothing", head + `[]}`, nil},
		{"a warning before the JSON and a line after it", "CMake Warning: x\n" + head + `[{"name":"a","backtrace":1,"command":["x"]}]}` + "\ntrailing", []ctestTest{{Name: "a", Command: []string{"x"}, File: "/p/a.cmake", Line: 7}}},
		{"the source line of a generator wins, and its path may have colons", head + `[{"name":"G.T","backtrace":1,"command":["x"],"properties":[{"name":"DEF_SOURCE_LINE","value":"C:/src/t.cpp:12"}]}]}`,
			[]ctestTest{{Name: "G.T", Command: []string{"x"}, File: "C:/src/t.cpp", Line: 12}}},
		{"a source line without a number is ignored", head + `[{"name":"G.T","backtrace":1,"properties":[{"name":"DEF_SOURCE_LINE","value":"t.cpp:x"}]}]}`, []ctestTest{{Name: "G.T", File: "/p/a.cmake", Line: 7}}},
		{"a backtrace out of range and no backtrace", head + `[{"name":"a","backtrace":9},{"name":"b"}]}`, []ctestTest{{Name: "a"}, {Name: "b"}}},
		{"a single label, the environment and disabled as text", head + `[{"name":"a","properties":[{"name":"LABELS","value":"one"},{"name":"ENVIRONMENT","value":["A=1","B=2"]},{"name":"DISABLED","value":"ON"}]}]}`,
			[]ctestTest{{Name: "a", Labels: []string{"one"}, Env: []string{"A=1", "B=2"}, Disabled: true}}},
		{"a test with no name is left out", head + `[{"command":["x"]},{"name":"a"}]}`, []ctestTest{{Name: "a"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := readTestList(tc.text)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("tests:\n%+v\nwant:\n%+v", got, tc.want)
			}
		})
	}
}

func TestAListThatIsNotCTestsFailsWithItsTag(t *testing.T) {
	for _, tc := range []struct{ name, text string }{
		{"no JSON", "Test project /x\nNo tests were found!!!\n"},
		{"cut off", `{"kind":"ctestInfo","tests":[`},
		{"another kind", `{"kind":"other","version":{"major":1}}`},
		{"another version", `{"kind":"ctestInfo","version":{"major":2}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := readTestList(tc.text)
			wantRoute(t, err, "readTestList.CppCTestJSONInvalid")
		})
	}
}
