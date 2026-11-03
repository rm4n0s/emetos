// Package variables resolves the {{name}} of the HTTP client (SPEC §10.33 "Variables"): the order in which the places that define
// a name are looked at, the dynamic variables ({{$guid}}, {{$processEnv NAME}}), the current values that only this computer has
// (kept in Emetos's state, never in the project), the globals of the user, and the secrets, which are in the desktop keyring and
// never in a file (D45).
//
// The order, from the strongest: the values extracted by the request in this run (locals), the current and then the initial
// values of the environment, the folders of the request (the innermost first), the current and then the initial values of the
// collection, and the globals. A name that is found nowhere stays as it is in the text and is reported.
package variables
