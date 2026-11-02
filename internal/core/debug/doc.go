// Package debug is the debug model of the editor (SPEC §10.31, D39, D41, D43): the data of Run and Debug (sessions, threads, stack
// frames loaded on demand, scopes and variables loaded lazily and in pages, watch expressions, breakpoints of every kind with their
// verification and the moves the adapter makes), the session driver that runs VS Code's sequence over the DAP client of core/dap
// (initialize, launch or attach, the initialized event, the breakpoints, configurationDone, the events, restart, stop, disconnect,
// child sessions and compounds), the launch configurations (launch.json, the launch setting, the variables and the inputs) and the
// state that is saved with the workspace (§10.10).
//
// It has no user interface and imports no Glutter. Everything that belongs to the window comes through callbacks in Options: how
// to reach the adapter of a type, the configuration providers of the debuggers, the tasks that run before and after a session, the
// prompts of the inputs, the terminal of runInTerminal, the active editor for ${file}. Nothing here touches the real configuration
// of the user.
//
// Threads. The model is safe for use from several goroutines. A session reads its adapter on a goroutine of its own and makes all
// its requests on other goroutines, never from the loop that takes the events (the event channel of core/dap is bounded and a
// request made from that loop could wait behind events nobody takes). Changes are announced to the subscribers (Service.Subscribe)
// from the goroutine that made them, after the lock is released; a subscriber must not block, and the window hands the work over
// to its UI thread (CLAUDE.md). The documents belong to the UI thread: the Tracker, which moves breakpoints with the edits of their
// documents, is called from it.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/common/debugModel.ts (the model), browser/debugSession.ts and
// browser/rawDebugSession.ts (the session), browser/debugService.ts (starting, restarting and stopping, the breakpoints),
// browser/debugConfigurationManager.ts (the launches and the selected configuration), common/debugStorage.ts (the state),
// common/debugUtils.ts (the configurations by platform, their order), and the configuration resolver of
// src/vs/workbench/services/configurationResolver/ (the variables and the inputs).
package debug
