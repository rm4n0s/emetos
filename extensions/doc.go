// Package extensions lists the extensions that are compiled into Emetos (SPEC §10.15, D18). Every extension is a
// folder below this one with a function of type extension.Factory, and one line in All. A package under extensions
// imports only the public package extension, the standard library, its own subpackages and the dependencies CLAUDE.md
// allows; internal/archtest checks it.
package extensions
