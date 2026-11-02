package app

import (
	"context"
	"strconv"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/debug"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/views/problems"
)

// The tasks before and after a debug session (SPEC §10.31 "Launch configurations", step 25.11): preLaunchTask, postDebugTask,
// preRestartTask and postRestartTask run through the Tasks extension (extension.TopicTasksRun), and debug.onTaskErrors decides what a
// task that failed does to the launch.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugTaskRunner.ts

// debugTaskTimeout is how long the window waits for a task: a task takes as long as it takes (a request without a deadline would be
// answered with EventTimeout after 10 seconds), and the user stops it by stopping the start.
const debugTaskTimeout = 6 * time.Hour

// TaskSummary is how a task ended (VS Code's ITaskSummary).
type TaskSummary struct {
	// ExitCode is the exit code of the task's process; nil when the task was stopped before it ended by itself.
	ExitCode *int
	// Cancelled says that the task was stopped, by the user or because the start was.
	Cancelled bool
}

// TaskRunner runs the task of a configuration (preLaunchTask, postDebugTask) and tells how it ended.
type TaskRunner func(ctx context.Context, folder string, task debug.TaskRef) (TaskSummary, error)

// debugRunTask runs the task of a configuration and tells whether to go on. A task that is checked (checkErrors: preLaunchTask and
// postRestartTask) that ended with a failure asks what to do as debug.onTaskErrors says; the others (postDebugTask, preRestartTask)
// never stop anything.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugTaskRunner.ts (DebugTaskRunner.runTaskAndCheckErrors)
func (w *Workspace) debugRunTask(ctx context.Context, folder string, task debug.TaskRef, checkErrors bool) (debug.TaskResult, error) {
	run := w.taskRunner
	if run == nil {
		run = w.runTaskThroughExtension
	}
	summary, err := run(ctx, folder, task)
	if err != nil {
		return debug.TaskFailure, err
	}
	if !checkErrors {
		return debug.TaskSuccess, nil
	}
	if summary.ExitCode == nil || summary.Cancelled {
		return debug.TaskFailure, nil // the user cancelled, either the debugging or the task
	}
	host := w
	if w.isView() {
		host = w.viewOf
	}
	errorCount := len(host.Markers.Read(problems.ReadFilter{Severities: problems.Severities(problems.SeverityError), Take: 2}))
	exit := *summary.ExitCode
	onTaskErrors := settings.Get[string](host.Settings, "debug.onTaskErrors", "")
	// the markers decide nothing here: a task that ended with 0 passes and one that did not is asked about, as VS Code does (its extra
	// condition, no errors and no failing exit code, is the first one again); they only change the words of the question
	if exit == 0 || onTaskErrors == "debugAnyway" {
		return debug.TaskSuccess, nil
	}
	switch onTaskErrors {
	case "showErrors":
		screen := w.debugScreen(ctx)
		screen.onUI(screen.showProblems)
		return debug.TaskFailure, nil
	case "abort":
		return debug.TaskFailure, nil
	}
	if w.debugAskTaskErrors(ctx, task.String(), errorCount, exit) {
		return debug.TaskSuccess, nil
	}
	return debug.TaskFailure, nil
}

// taskErrorsMessage is the question of a preLaunchTask that failed: VS Code's three texts.
func taskErrorsMessage(label string, errorCount, exitCode int) string {
	switch {
	case errorCount > 1:
		return "Errors exist after running preLaunchTask '" + label + "'."
	case errorCount == 1:
		return "Error exists after running preLaunchTask '" + label + "'."
	}
	return "The preLaunchTask '" + label + "' terminated with exit code " + strconv.Itoa(exitCode) + "."
}

// The buttons of the question about a task that failed, in VS Code's order; Abort is the cancel button.
const (
	taskDebugAnyway = iota
	taskShowErrors
	taskAbort
)

// debugAskTaskErrors asks, on the screen that asked, what to do about a task that failed (debug.onTaskErrors "prompt"): Debug Anyway,
// Show Errors or Abort, with a checkbox that keeps the choice in the user settings. It reports whether to go on with the launch.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugTaskRunner.ts (DebugTaskRunner.runTaskAndCheckErrors)
func (w *Workspace) debugAskTaskErrors(ctx context.Context, label string, errorCount, exitCode int) bool {
	screen := w.debugScreen(ctx)
	ch := make(chan kit.DialogResult, 1)
	screen.onUI(func() {
		screen.Dialogs.Show(kit.DialogSpec{
			Type:    kit.DialogWarning,
			Message: taskErrorsMessage(label, errorCount, exitCode),
			Buttons: []string{"Debug Anyway", "Show Errors", "Abort"}, CancelButton: foundation.Some(taskAbort),
			CheckboxLabel: "Remember my choice in user settings",
			OnAnswer:      func(r kit.DialogResult) { ch <- r },
		})
	})
	var r kit.DialogResult
	select {
	case r = <-ch:
	case <-ctx.Done():
		return false
	}
	choice := r.Button
	if r.Cancelled {
		choice = taskAbort
	}
	if r.Checked {
		remembered := map[int]string{taskDebugAnyway: "debugAnyway", taskShowErrors: "showErrors", taskAbort: "abort"}[choice]
		host := w
		if w.isView() {
			host = w.viewOf
		}
		if err := host.Settings.Update(ctx, "debug.onTaskErrors", remembered, settings.TargetUser); err != nil {
			w.Process.Logger.Warn("the choice about the errors of a task cannot be kept", "err", err)
		}
	}
	switch choice {
	case taskDebugAnyway:
		return true
	case taskShowErrors:
		screen.onUI(screen.showProblems)
	}
	return false
}

// runTaskThroughExtension asks the Tasks extension to run the task and waits for the answer. A task that cannot be found, or cannot
// start, is the extension's error. It gives up, with a task that was cancelled, when ctx ends.
func (w *Workspace) runTaskThroughExtension(ctx context.Context, _ string, task debug.TaskRef) (TaskSummary, error) {
	host := w
	if w.isView() {
		host = w.viewOf // the session is the host's, and so is its task (D46)
	}
	scope := host.Extensions()
	if scope == nil {
		return TaskSummary{}, errors.New("DebugTaskNoScope", "the task '"+task.String()+"' cannot run: this window runs no extensions", "task", task.String())
	}
	// the Tasks extension starts with its first use: a session that runs a task is such a use
	if _, err := uiValue(ctx, host, func() struct{} { scope.Activate(extension.TasksID); return struct{}{} }); err != nil {
		return TaskSummary{Cancelled: true}, nil
	}
	type answer struct {
		ev  extension.Event
		err error
	}
	ch := make(chan answer, 1)
	rctx, cancel := context.WithTimeout(ctx, debugTaskTimeout)
	defer cancel()
	req := extension.TasksRunRequest{Label: task.Label, Identifier: task.Identifier}
	if err := scope.Ask(rctx, extension.TopicTasksRun, req, func(ev extension.Event, err error) { ch <- answer{ev, err} }); err != nil {
		return TaskSummary{}, err
	}
	select {
	case a := <-ch:
		if a.err != nil {
			if appErr, ok := errors.FromError(a.err); ok && appErr.Tag == "EventNoHandler" {
				return TaskSummary{}, errors.New("DebugTaskUnavailable", "the task '"+task.String()+"' cannot run: the Tasks extension is not running", "task", task.String())
			}
			if ctx.Err() != nil {
				return TaskSummary{Cancelled: true}, nil
			}
			if appErr, ok := errors.FromError(a.err); ok && appErr.Tag == "EventHandlerFailed" {
				// the task's own failure (TaskNotFound, TaskBusy, ...) with its tag and message, not the bus's wrapper
				if inner, ok := errors.FromError(errors.Unwrap(a.err)); ok {
					return TaskSummary{}, inner
				}
			}
			return TaskSummary{}, a.err
		}
		reply, err := extension.EventValue[extension.TasksRunReply](a.ev)
		if err != nil {
			return TaskSummary{}, err
		}
		if reply.Cancelled {
			return TaskSummary{Cancelled: true}, nil
		}
		return TaskSummary{ExitCode: &reply.ExitCode}, nil
	case <-ctx.Done():
		return TaskSummary{Cancelled: true}, nil
	}
}
