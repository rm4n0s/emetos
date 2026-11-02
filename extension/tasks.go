package extension

// The request to the Tasks extension (SPEC §10.15 "As built (step 11.5)", §10.31 "Launch configurations"): run a task and tell how it
// ended. The window asks it for preLaunchTask and postDebugTask (the window is the requester, Scope.Ask of the host), and any
// extension may ask it with Events().Request.

// TopicTasksRun is the request that runs a task of the Tasks extension (emetos.tasks): it takes TasksRunRequest and answers
// TasksRunReply when the task has ended. The reply takes as long as the task does, so the requester gives the request a context with a
// deadline that fits (a request without one is answered with EventTimeout after 10 seconds); cancelling the context stops the task. It
// fails with EventNoHandler when the Tasks extension is not running, and the handler's own errors are the tags TaskNotFound,
// TaskBusy, TaskNoFolder, TaskStartFailed and TaskRequestInvalid.
const TopicTasksRun = "emetos.tasks.runTask"

// TasksRunRequest is the payload of TopicTasksRun. A task is named by its label: the command line of an entry of the setting
// emetos.tasks.commands, or the label of an entry that has one.
type TasksRunRequest struct {
	// Label is the label of the task.
	Label string `json:"label,omitempty"`
	// Identifier names the task by an object, as a launch configuration's preLaunchTask may (VS Code's task identifier): its field
	// label (else task) is the label. It is used when Label is empty.
	Identifier map[string]any `json:"identifier,omitempty"`
}

// TasksRunReply is the answer to TopicTasksRun: how the task ended.
type TasksRunReply struct {
	// ExitCode is the exit code of the command; -1 when it did not end by itself (a signal).
	ExitCode int `json:"exitCode"`
	// Cancelled says that the task was stopped (by the user, or because the request was cancelled) and did not end by itself.
	Cancelled bool `json:"cancelled,omitempty"`
	// Errors is how many problems of the error severity the run put in the Problems panel.
	Errors int `json:"errors,omitempty"`
}

// TasksID is the id of the Tasks extension. It is activated by its commands and its tab, so a requester that is not the user (the window,
// for a debug session) activates it first (exthost.Scope.Activate).
const TasksID = "emetos.tasks"
