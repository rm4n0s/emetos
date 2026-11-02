// Package testing is the test model of the editor (SPEC §10.32, D42, D43): the tree of test items by controller, the run profiles, the
// run requests with their include and exclude lists, the results of the runs with VS Code's precedence of states and its durations (the
// last 16 runs, saved with the workspace's state of §10.10 with the output trimmed), the continuous runs that start again when a file
// of their items changes, and the coverage a run measured with its sums.
//
// It has no user interface and imports no Glutter, and it does not know the extension API: the host (internal/gui/exthost) turns the
// calls of an extension's controller into calls of this package. Nothing here touches the real configuration of the user.
//
// Threads. The Service is safe for use from several goroutines. Changes are announced to the subscribers (Service.Subscribe) one event
// at a time and in the order they were made, on the goroutine that made them or on the one that delivers the events of another; a
// subscriber must not block, and the window hands the work over to its UI thread (CLAUDE.md). The functions the controllers give
// (their run handlers, resolve and refresh functions) are called without any lock held, from the goroutine of the caller.
//
// Ported from VS Code: src/vs/workbench/contrib/testing/common/testService.ts (running the requests, the default profiles of a group),
// testResult.ts (the results, their tasks, the counts, the output), getComputedState.ts and testingStates.ts (the states and their
// precedence, the computed states and durations), testResultService.ts (the list of results), testResultStorage.ts (the saved
// results), testProfileService.ts (the profiles and their defaults), testCoverage.ts (the coverage and its sums) and
// testingContinuousRunService.ts (the continuous runs), and src/vs/workbench/api/common/extHostTesting.ts (the rules of a request).
package testing
