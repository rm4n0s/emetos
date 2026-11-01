package extension

import "strings"

// The Terminal Environment API (SPEC §10.30 "Passwords and editors", step 24.23; after VS Code's `ExtensionContext.environmentVariableCollection`
// and the TerminalEnvironmentManager of its git extension). An extension adds variables to the environment of the shells that the Terminal
// extension starts in the window tab: Git adds `GIT_ASKPASS` so that `git push` typed in a terminal asks for its password in the window.
//
// The Terminal extension reads the contributions when it starts a shell (Contributions), so a terminal that is running keeps the
// environment it started with, as in VS Code. The variables are the extension's to give lazily: the source is called then, and not when it
// is registered, so that it can start what the variables point at (a socket) only when a terminal needs it.
//
// Everything here may be called from any goroutine.

// TerminalVariables is a source of variables for the terminals: it returns the variables to set (nil for none) and the description of what
// they enable, which the terminal's tab shows ("Enables the following features: git auth provider"). It is called on the UI thread, each time
// a terminal starts, and replaces a variable of the same name that the environment has. A panic in it fails the extension.
type TerminalVariables func() (variables map[string]string, description string)

// TerminalContribution is what one source contributes to a terminal that starts now.
type TerminalContribution struct {
	// Extension is the id of the extension that contributes, Name its name as the user reads it ("Git").
	Extension, Name string
	// Description is what the source said its variables enable; empty for none.
	Description string
	// Variables are the variables, valid ones only (ValidTerminalVariable), by name.
	Variables map[string]string
}

// TerminalEnvironment adds variables to the environment of terminals.
type TerminalEnvironment interface {
	// Contribute registers a source of variables. dispose takes it away (it is called by itself when the activation ends, which is how
	// the variables of an extension that is turned off stop reaching new terminals). It fails with ExtensionTerminalEnvironmentInvalid
	// for a nil source and ExtensionDeactivated after the activation ended.
	Contribute(source TerminalVariables) (dispose func(), err error)
	// Contributions asks every source, in the order they were registered, and returns what they give now, leaving out the sources that
	// give no variable. A variable that two sources give is the later one's when a terminal applies them in this order. It is for the
	// Terminal extension, and works for any extension.
	Contributions() []TerminalContribution
}

// ValidTerminalVariable tells whether name=value can be put in an environment: the name is not empty, has no "=" and no NUL, and the value
// has no NUL.
func ValidTerminalVariable(name, value string) bool {
	return name != "" && !strings.ContainsAny(name, "=\x00") && !strings.Contains(value, "\x00")
}
