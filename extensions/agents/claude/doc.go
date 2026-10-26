// Package claude is the adapter of the Agents extension (SPEC §10.16) for the user's own claude program: a Go client of its
// stream-json mode. It has no UI and imports nothing of Emetos but the error package; the session tab, the permissions
// dialogs and the rest of the extension (steps 13.2 and 13.3) are built on it.
//
// The adapter never runs a copy of the program of its own, never passes --bare, never sets CLAUDE_CONFIG_DIR, and never
// reads, stores, passes or asks for a token or any file of the program's configuration: it only runs the program, which
// finds the user's login itself. The environment of the program is the environment of Emetos with one added variable
// (see Environment).
//
// The protocol code is in this package only and is split by what it does, so that a change of the program touches one
// place: command.go (finding the program, the command line, the environment), auth.go (`claude auth status`), lines.go
// (reading the output), messages.go (the typed messages), partial.go (joining partial text), control.go (the control
// channel and permissions), process.go (the process and its group) and session.go (the Session). The wire shapes come from
// the documentation of the Claude Agent SDK (the stream-json input and output, the control requests, the types of
// SDKSystemMessage, SDKResultMessage, SDKPartialAssistantMessage, PermissionUpdate) and are listed in SPEC §10.16 "As
// built (step 13.1)".
package claude
