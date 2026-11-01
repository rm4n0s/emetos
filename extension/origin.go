package extension

// The origin of a command (SPEC §10.18 "As built (step 24.25)", D50): a workspace that is hosted may have clients that see its
// window through a view of their own (Remote Work). A command that a client runs in its view is run by the one scope of the hosted
// workspace, so the quick picks, input boxes, dialogs, progress and notifications that the extension raises while it handles the command
// have to be shown in the view of the client, not on the screen of the host. The origin names the screen a command came from.

// Origin names a screen of a workspace: the window of the host, or the view of one client. The zero value is the window of the host,
// which is also where the questions go that no person started (an automatic fetch).
type Origin uint64

// HostOrigin is the window of the host.
const HostOrigin Origin = 0

// Origins tells an extension where a command came from and keeps the origins it needs for work that goes on after the command's handler
// has returned (a program that asks a question later).
type Origins interface {
	// Current is the origin of the command whose handler is running on the UI thread, or of the callback that continues it: the
	// function of Show that answers a question the command asked, a button of a notification the command showed, a function that
	// WithOrigin calls. It is HostOrigin where no command is running, and on any goroutine but the UI thread's.
	Current() Origin
	// Run calls f on the calling goroutine with Current returning o until f returns. It is for a callback that the extension keeps
	// for later (the end of a job on a goroutine, which a Post hands to the UI thread) so that what f asks goes to the screen the job
	// was started from. Call it on the UI thread.
	Run(o Origin, f func())
	// Remember keeps an origin under a key of the extension's own choice (the typist of a terminal, "terminal:3"), for Recall; the
	// key is shared by all the extensions of the scope, so that one extension can tell another where a request came from. The empty
	// origin is a valid one: it clears nothing.
	Remember(key string, o Origin)
	// Recall returns the origin kept under the key, or HostOrigin when there is none.
	Recall(key string) Origin
}
