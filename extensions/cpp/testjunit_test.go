package cpp

import (
	"reflect"
	"testing"
	"time"
)

func TestTheReportOfTheRealCTestIsRead(t *testing.T) {
	got, err := readJUnit([]byte(fixture(t, "junit_all.xml")))
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		name    string
		outcome outcome
		message string
		output  string
	}
	var rows []row
	for _, r := range got {
		if r.Duration < 0 || r.Duration > time.Minute {
			t.Errorf("%s took %v", r.Name, r.Duration)
		}
		rows = append(rows, row{r.Name, r.Outcome, r.Message, r.Output})
	}
	want := []row{
		{"adds", outcomePassed, "", "adding 40 and 2\n"},
		{"divides", outcomeFailed, "Failed", "divide(5, 2) = 2\nexpected 3 but got 2\n"},
		{"skipped", outcomeSkipped, "SKIP_RETURN_CODE=77", "no display here, skipping\n"},
		{"disabled", outcomeSkipped, "Disabled", ""},
		{"math.lists", outcomePassed, "", "adding 40 and 2\n"},
		{"odd[1]+x(2)", outcomePassed, "", "adding 40 and 2\n"},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("results:\n%+v\nwant:\n%+v", rows, want)
	}
}

func TestTheReportOfTestsThatCannotRunTimeOutOrCrashIsRead(t *testing.T) {
	got, err := readJUnit([]byte(fixture(t, "junit_errors.xml")))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("%d results", len(got))
	}
	for i, want := range []struct {
		name    string
		outcome outcome
		message string
	}{{"missing", outcomeNotRun, "Unable to find executable"}, {"slow", outcomeFailed, "Timeout"}, {"crash", outcomeFailed, "SEGFAULT"}} {
		if got[i].Name != want.name || got[i].Outcome != want.outcome || got[i].Message != want.message {
			t.Errorf("%d: %+v, want %+v", i, got[i], want)
		}
	}
	if got[1].Duration < time.Second || got[1].Duration > 2*time.Second {
		t.Errorf("slow took %v", got[1].Duration)
	}
}

func TestReportsWrittenByHandAreRead(t *testing.T) {
	for _, tc := range []struct {
		name string
		xml  string
		want []junitResult
	}{
		{"nothing", `<testsuite name="x" tests="0"/>`, []junitResult{}},
		{"an error element", `<testsuite><testcase name="a" time="1.5" status="fail"><error message="boom"/><system-err>bad</system-err></testcase></testsuite>`,
			[]junitResult{{Name: "a", Outcome: outcomeFailed, Message: "boom", Duration: 1500 * time.Millisecond, Output: "bad"}}},
		{"the output of both streams", `<testsuite><testcase name="a" status="run"><system-out>out</system-out><system-err>err</system-err></testcase></testsuite>`,
			[]junitResult{{Name: "a", Outcome: outcomePassed, Output: "outerr"}}},
		{"a skip by a regular expression", `<testsuite><testcase name="a" status="notrun"><skipped message="SKIP_REGULAR_EXPRESSION"/></testcase></testsuite>`,
			[]junitResult{{Name: "a", Outcome: outcomeSkipped, Message: "SKIP_REGULAR_EXPRESSION"}}},
		{"not run with no element", `<testsuite><testcase name="a" status="notrun"/></testsuite>`, []junitResult{{Name: "a", Outcome: outcomeNotRun}}},
		{"a disabled test", `<testsuite><testcase name="a" status="disabled"><system-out>Disabled</system-out></testcase></testsuite>`,
			[]junitResult{{Name: "a", Outcome: outcomeSkipped, Message: "Disabled"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := readJUnit([]byte(tc.xml))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("results:\n%+v\nwant:\n%+v", got, tc.want)
			}
		})
	}
}

func TestAReportThatIsNotXMLFailsWithItsTag(t *testing.T) {
	for _, text := range []string{"", "not xml", "<testsuite><testcase", "<other/>"} {
		_, err := readJUnit([]byte(text))
		wantRoute(t, err, "readJUnit.CppJUnitInvalid")
	}
}
