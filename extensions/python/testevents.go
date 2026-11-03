package python

import (
	"encoding/json"
	"strings"

	"github.com/rm4n0s/errors"
)

// The events of the helper scripts (helpers/emetos_pytest.py, helpers/emetos_unittest.py): JSON lines after a marker, mixed with the
// ordinary output of the run. All lines are counted from 1.

// eventMark starts an event in a line of output.
const eventMark = "@@EMETOS@@ "

// testEvent is one event of the helpers; Type says which fields are set.
type testEvent struct {
	Type string `json:"type"`
	// ID is the id the framework gives the test (pytest's node id, unittest's dotted id).
	ID string `json:"id"`
	// discovery
	Rootdir string       `json:"rootdir"`
	Tests   []testRecord `json:"tests"`
	// collect_error
	File    string `json:"file"`
	Line    int    `json:"line"`
	Message string `json:"message"`
	Text    string `json:"text"`
	// result
	Outcome  string         `json:"outcome"`
	Duration float64        `json:"duration"`
	Messages []eventMessage `json:"messages"`
	Output   string         `json:"output"`
	// Holder says that the result belongs to a class or a module whose fixture failed (setUpClass), named by the id.
	Holder bool `json:"holder"`
	// done
	ExitStatus int `json:"exitstatus"`
}

// testRecord is a test found by the discovery.
type testRecord struct {
	ID      string        `json:"id"`
	File    string        `json:"file"`
	Line    int           `json:"line"`
	Classes []classRecord `json:"classes"`
	Func    string        `json:"func"`
	Param   string        `json:"param"`
	// Module is the dotted name of the file's module (unittest).
	Module string `json:"module"`
}

// classRecord is a class a test is in.
type classRecord struct {
	Name string `json:"name"`
	Line int    `json:"line"`
}

// eventMessage is what a failure says.
type eventMessage struct {
	Message  string       `json:"message"`
	Text     string       `json:"text"`
	File     string       `json:"file"`
	Line     int          `json:"line"`
	Frames   []eventFrame `json:"frames"`
	Expected string       `json:"expected"`
	Actual   string       `json:"actual"`
}

// eventFrame is a line of the traceback of a failure, in the user's code.
type eventFrame struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Name string `json:"name"`
}

// parseEvent reads a line of output: the text before the marker (the output of the run that shares the line, when there is a marker), and
// the event after it. A line with no marker is all text and no event. A marker followed by something that is not an event fails with
// PythonTestEventInvalid.
func parseEvent(line string) (text string, ev testEvent, ok bool, err error) {
	i := strings.Index(line, eventMark)
	if i < 0 {
		return line, testEvent{}, false, nil
	}
	text = line[:i]
	if err := json.Unmarshal([]byte(line[i+len(eventMark):]), &ev); err != nil {
		return text, testEvent{}, false, errors.NewErr("PythonTestEventInvalid", err).SetMetadata("line", clip(line, 200))
	}
	if ev.Type == "" {
		return text, testEvent{}, false, errors.New("PythonTestEventInvalid", "the event of the test helper has no type", "line", clip(line, 200))
	}
	return text, ev, true, nil
}

// clip shortens s to n bytes for a message.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "") + "..."
}
