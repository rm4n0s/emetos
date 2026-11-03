package cpp

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"
)

// The readers of CTest's two formats (SPEC §10.32 "The providers", C and C++; "As built (step 26.9)"): `ctest --show-only=json-v1`, which
// lists the tests of a build folder, and the JUnit XML of `ctest --output-junit` (testjunit.go), which has the results of a run.

// ctestTest is a test CTest lists.
type ctestTest struct {
	// Name is the name add_test gave it and Command its command line (the program first). Dir is the folder it runs in (the
	// WORKING_DIRECTORY property), Env the variables the ENVIRONMENT property adds ("NAME=value") and Labels its LABELS.
	Name    string
	Command []string
	Dir     string
	Env     []string
	Labels  []string
	// Disabled is the DISABLED property.
	Disabled bool
	// File and Line are the place of the test in the sources (1-based; Line 0 when CTest gives none): the line of the add_test call that
	// made it, or the one a test generator such as gtest_discover_tests puts in the property DEF_SOURCE_LINE.
	File string
	Line int
}

// ctestJSON is the part of the JSON that is read.
type ctestJSON struct {
	Kind    string `json:"kind"`
	Version struct {
		Major int `json:"major"`
	} `json:"version"`
	Backtrace struct {
		Files []string `json:"files"`
		Nodes []struct {
			File int `json:"file"`
			Line int `json:"line"`
		} `json:"nodes"`
	} `json:"backtraceGraph"`
	Tests []struct {
		Name       string   `json:"name"`
		Backtrace  *int     `json:"backtrace"`
		Command    []string `json:"command"`
		Properties []struct {
			Name  string          `json:"name"`
			Value json.RawMessage `json:"value"`
		} `json:"properties"`
	} `json:"tests"`
}

// readTestList reads the output of `ctest --show-only=json-v1`: anything before the JSON (a warning CMake printed) is skipped, and
// whatever follows it is ignored. It fails with CppCTestJSONInvalid when there is no JSON, it is not CTest's list or its version is not 1.
func readTestList(text string) ([]ctestTest, error) {
	start := strings.Index(text, "{")
	if start < 0 {
		return nil, errors.New("CppCTestJSONInvalid", "ctest printed no JSON", "output", clip(strings.TrimSpace(text), 200))
	}
	var doc ctestJSON
	if err := json.NewDecoder(strings.NewReader(text[start:])).Decode(&doc); err != nil {
		return nil, errors.NewErr("CppCTestJSONInvalid", err)
	}
	if doc.Kind != "ctestInfo" || doc.Version.Major != 1 {
		return nil, errors.New("CppCTestJSONInvalid", "ctest listed its tests in a format Emetos does not read", "kind", doc.Kind, "major", doc.Version.Major)
	}
	var out []ctestTest
	for _, t := range doc.Tests {
		if t.Name == "" {
			continue
		}
		ct := ctestTest{Name: t.Name, Command: t.Command}
		if t.Backtrace != nil && *t.Backtrace >= 0 && *t.Backtrace < len(doc.Backtrace.Nodes) {
			n := doc.Backtrace.Nodes[*t.Backtrace]
			if n.File >= 0 && n.File < len(doc.Backtrace.Files) {
				ct.File, ct.Line = doc.Backtrace.Files[n.File], n.Line
			}
		}
		for _, p := range t.Properties {
			switch p.Name {
			case "WORKING_DIRECTORY":
				ct.Dir = jsonString(p.Value)
			case "ENVIRONMENT":
				ct.Env = jsonStrings(p.Value)
			case "LABELS":
				ct.Labels = jsonStrings(p.Value)
			case "DISABLED":
				ct.Disabled = jsonBool(p.Value)
			case "DEF_SOURCE_LINE":
				if file, line, ok := splitSourceLine(jsonString(p.Value)); ok {
					ct.File, ct.Line = file, line
				}
			}
		}
		out = append(out, ct)
	}
	return out, nil
}

// splitSourceLine splits "path:line" at the last colon.
func splitSourceLine(s string) (string, int, bool) {
	i := strings.LastIndex(s, ":")
	if i <= 0 {
		return "", 0, false
	}
	line, err := strconv.Atoi(strings.TrimSpace(s[i+1:]))
	if err != nil || line < 1 {
		return "", 0, false
	}
	return s[:i], line, true
}

// jsonString is the value of a JSON string; "" for any other value.
func jsonString(v json.RawMessage) string {
	var s string
	if json.Unmarshal(v, &s) != nil {
		return ""
	}
	return s
}

// jsonStrings is the strings of a JSON array, or the one string of a JSON string (a single label is not wrapped).
func jsonStrings(v json.RawMessage) []string {
	var list []string
	if json.Unmarshal(v, &list) == nil {
		return list
	}
	if s := jsonString(v); s != "" {
		return []string{s}
	}
	return nil
}

// jsonBool is a JSON true, or a string CMake reads as true.
func jsonBool(v json.RawMessage) bool {
	var b bool
	if json.Unmarshal(v, &b) == nil {
		return b
	}
	switch strings.ToUpper(jsonString(v)) {
	case "1", "ON", "YES", "TRUE", "Y":
		return true
	}
	return false
}
