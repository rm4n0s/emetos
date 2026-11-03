package app

import (
	"time"

	"encoding/json"
	"path/filepath"

	coretesting "github.com/rm4n0s/emetos/internal/core/testing"
	"github.com/rm4n0s/emetos/internal/core/workspace"
	"github.com/rm4n0s/emetos/internal/gui/exthost"
)

// Testing in the window (SPEC §10.32, step 26.2). A workspace has one testing host: the test model over the controllers its extensions
// make. This file makes the host, restores and saves the results with the rest of the state, and feeds the file changes of the watcher
// to the continuous runs (decision 1533). The Test Explorer, its commands and the badge are the shell's (testingview.go,
// testingcommands.go), one set for the window of the host and one for each headless view of a client, all over the host's service (D46).
//
// Ported from VS Code: src/vs/workbench/contrib/testing/common/testService.ts and testingContinuousRunService.ts

// initTesting makes the testing host of a workspace. The window tab of an extension and the headless view of a workspace make none.
func (w *Workspace) initTesting() {
	host := exthost.NewTestingHost(exthost.TestingHostOptions{Logger: w.Process.Logger, Model: coretesting.Options{Now: w.Process.testingNow}})
	w.Testing = host
	w.stops = append(w.stops, host.Close)
	w.initCoverage(host.Service())
}

// TestingService is the test service of the workspace, nil where there is none.
func (w *Workspace) TestingService() *coretesting.Service {
	if w.Testing == nil {
		return nil
	}
	return w.Testing.Service()
}

// restoreTesting puts the saved results back; a state that cannot be used is logged and changes nothing.
func (w *Workspace) restoreTesting(raw json.RawMessage) {
	svc := w.TestingService()
	if svc == nil || len(raw) == 0 {
		return
	}
	if err := svc.RestoreState(raw); err != nil {
		w.Process.Logger.Warn("the saved test results cannot be used", "err", err)
	}
}

// testingWindowState is the state of the test model to save with the window; nothing while there is none to save.
func (w *Workspace) testingWindowState() json.RawMessage {
	svc := w.TestingService()
	if svc == nil {
		return nil
	}
	data, err := svc.SaveState()
	if err != nil {
		w.Process.Logger.Warn("the test results cannot be saved", "err", err)
		return nil
	}
	return data
}

// startTesting feeds the changes of files to the continuous runs. It runs once the folder of the workspace is known.
func (w *Workspace) startTesting() {
	svc := w.TestingService()
	if svc == nil {
		return
	}
	w.stops = append(w.stops, w.OnFileEvents(func(events []workspace.Event) {
		var uris []string
		for _, e := range events {
			if e.IsDir || e.Kind == workspace.EventDeleted || e.Kind == workspace.EventRescan {
				continue
			}
			uris = append(uris, explorerURI(filepath.Clean(e.Path)))
		}
		if len(uris) > 0 {
			svc.FilesChanged(uris)
		}
	}))
}

// testingElapsed is how long a run that goes on has taken. The runs are stamped with the system clock unless a test fixed the clock of the
// model (ProcessOptions.TestingNow); then the time since the start cannot be known and the summary says none.
func (p *Process) testingElapsed(r *coretesting.Result) time.Duration {
	if p.testingNow != nil {
		return 0
	}
	return time.Since(r.StartedAt())
}
