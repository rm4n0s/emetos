package devtesting

import (
	"context"
	"encoding/json"
	"path/filepath"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The names of the extension.
const (
	// ID is the id of the extension and ControllerID that of the controller it makes.
	ID           = "dev.testing"
	ControllerID = "dev.testing.fake"
	// ScriptFile is the file of the folder the controller reads, by its path relative to the folder.
	ScriptFile = "fake-tests.json"
)

// Script is the file ScriptFile: the tests, and what a run of each does.
type Script struct {
	// Label is the name of the controller; "Fake Tests (dev)" when empty.
	Label string `json:"label"`
	// DelayMs is how long a test runs before it reports, 150 when 0.
	DelayMs int `json:"delayMs"`
	// Tests are the top items.
	Tests []Test `json:"tests"`
}

// Test is an item and the result a run gives it: Result is "passed", "failed", "errored" or "skipped" (a test with children has none).
type Test struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	// Result and Ms are the result and the duration a run reports; Message is the message of a failure.
	Result  string  `json:"result,omitempty"`
	Ms      float64 `json:"ms,omitempty"`
	Message string  `json:"message,omitempty"`
	// Expected and Actual are shown as a diff with a failure.
	Expected string `json:"expected,omitempty"`
	Actual   string `json:"actual,omitempty"`
	// FailLine is the line (counted from 1) of the file of the test that the message of a failure is at; 0 gives the message no place of its
	// own. Stack are the frames of the failure and Output is the text the test prints, with ANSI sequences.
	FailLine int     `json:"failLine,omitempty"`
	Stack    []Frame `json:"stack,omitempty"`
	Output   string  `json:"output,omitempty"`
	// File is the file of the test, by its path relative to the folder; Line and EndLine are the lines it covers (counted from 1) when it has a
	// file. A test with no file has no place in an editor.
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	EndLine  int    `json:"endLine,omitempty"`
	Children []Test `json:"children,omitempty"`
}

// Frame is a line of the stack trace of a failure: a function, and the file (by its path relative to the folder) and line (counted from 1) it
// was at.
type Frame struct {
	Label string `json:"label"`
	File  string `json:"file,omitempty"`
	Line  int    `json:"line,omitempty"`
}

// New makes the extension. It is a factory of extension.Factory.
func New() extension.Extension { return &devTesting{} }

type devTesting struct{}

// Manifest implements extension.Extension.
func (*devTesting) Manifest() extension.Manifest {
	return extension.Manifest{
		ID: ID, Name: "Fake Test Controller (development)", Version: "0.1.0",
		Description:      "A test controller that plays the script " + ScriptFile + " of the folder, for the proofs of the Test Explorer.",
		ActivationEvents: []string{extension.OnStartup},
	}
}

// Activate implements extension.Extension: it reads the script and makes the controller, its tree and its profiles. A folder with no
// script gets no controller.
func (*devTesting) Activate(ctx context.Context, api extension.API) error {
	text, err := api.Workspace().ReadFile(ctx, ScriptFile)
	if err != nil {
		// no script: nothing to show
		return nil
	}
	var script Script
	if err := json.Unmarshal([]byte(text), &script); err != nil {
		return errors.NewErr("DevTestingScriptInvalid", err).SetMetadata("file", ScriptFile)
	}
	label := script.Label
	if label == "" {
		label = "Fake Tests (dev)"
	}
	ctl, err := api.Tests().CreateTestController(ControllerID, label)
	if err != nil {
		return err
	}
	folder := api.Scope().Folder
	byID := map[string]Test{}
	var add func(parent *extension.TestItemCollection, ts []Test, path string) error
	add = func(parent *extension.TestItemCollection, ts []Test, path string) error {
		for _, t := range ts {
			item := ctl.CreateTestItem(t.ID, t.Label, "")
			if t.File != "" && folder != "" {
				uri := fileURI(folder, t.File)
				t := t
				item.Update(func(d *extension.TestItemData) {
					d.URI = uri
					if t.Line > 0 {
						d.Range = &extension.Range{Start: extension.Position{Line: t.Line - 1}, End: extension.Position{Line: max(t.EndLine, t.Line) - 1, Col: 1}}
					}
				})
			}
			if t.Description != "" {
				item.Update(func(d *extension.TestItemData) { d.Description = t.Description })
			}
			if err := parent.Add(item); err != nil {
				return err
			}
			byID[path+"/"+t.ID] = t
			if err := add(item.Children(), t.Children, path+"/"+t.ID); err != nil {
				return err
			}
		}
		return nil
	}
	if err := add(ctl.Items(), script.Tests, ""); err != nil {
		return err
	}
	run := runner(ctl, script, byID, folder)
	for _, p := range []struct {
		label string
		kind  extension.TestRunKind
	}{{"Run Tests", extension.TestKindRun}, {"Debug Tests", extension.TestKindDebug}, {"Run Tests with Coverage", extension.TestKindCoverage}} {
		if _, err := ctl.CreateRunProfile(p.label, p.kind, run, extension.TestRunProfileOptions{IsDefault: true, SupportsContinuousRun: p.kind == extension.TestKindRun}); err != nil {
			return err
		}
	}
	return nil
}

// Deactivate implements extension.Extension.
func (*devTesting) Deactivate(context.Context) error { return nil }

// pathOf is the key of an item in the script's index: the ids from the top.
func pathOf(it *extension.TestItem) string {
	if p := it.Parent(); p != nil {
		return pathOf(p) + "/" + it.ID()
	}
	return "/" + it.ID()
}

// runner is the handler of the profiles: every leaf of the included items is queued, then started and reported in order, each after the
// delay of the script. The run ends early when the context ends.
func runner(ctl extension.TestController, script Script, byID map[string]Test, folder string) extension.TestRunHandler {
	delay := time.Duration(script.DelayMs) * time.Millisecond
	if delay <= 0 {
		delay = 150 * time.Millisecond
	}
	return func(ctx context.Context, req *extension.TestRunRequest) error {
		tr, err := ctl.CreateTestRun(req, "Fake Tests", true)
		if err != nil {
			return err
		}
		defer tr.End()
		roots := req.Include
		if roots == nil {
			roots = ctl.Items().All()
		}
		excluded := map[*extension.TestItem]bool{}
		for _, e := range req.Exclude {
			excluded[e] = true
		}
		var leaves []*extension.TestItem
		var collect func(it *extension.TestItem)
		collect = func(it *extension.TestItem) {
			if excluded[it] {
				return
			}
			if kids := it.Children().All(); len(kids) > 0 {
				for _, k := range kids {
					collect(k)
				}
				return
			}
			leaves = append(leaves, it)
		}
		for _, r := range roots {
			collect(r)
		}
		for _, l := range leaves {
			tr.Enqueued(l)
		}
		for _, l := range leaves {
			t := byID[pathOf(l)]
			tr.Started(l)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil
			}
			d := time.Duration(t.Ms * float64(time.Millisecond))
			if t.Output != "" {
				tr.AppendOutput(t.Output, nil, l)
			}
			switch t.Result {
			case "failed", "errored":
				msg := extension.TestMessage{Message: t.Message, ExpectedOutput: t.Expected, ActualOutput: t.Actual}
				if t.FailLine > 0 && t.File != "" && folder != "" {
					at := extension.Position{Line: t.FailLine - 1}
					msg.Location = &extension.Location{URI: fileURI(folder, t.File), Range: extension.Range{Start: at, End: extension.Position{Line: at.Line, Col: 1}}}
				}
				for _, fr := range t.Stack {
					frame := extension.TestStackFrame{Label: fr.Label}
					if fr.File != "" && folder != "" {
						frame.URI = fileURI(folder, fr.File)
						if fr.Line > 0 {
							frame.Position = &extension.Position{Line: fr.Line - 1}
						}
					}
					msg.StackTrace = append(msg.StackTrace, frame)
				}
				if t.Result == "errored" {
					tr.Errored(l, []extension.TestMessage{msg}, d)
				} else {
					tr.Failed(l, []extension.TestMessage{msg}, d)
				}
			case "skipped":
				tr.Skipped(l)
			default:
				tr.Passed(l, d)
			}
		}
		return nil
	}
}

// fileURI is the file:// URI of a file of the folder.
func fileURI(folder, file string) string {
	return "file://" + filepath.ToSlash(filepath.Join(folder, file))
}
