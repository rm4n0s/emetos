// Package httpclient is the HTTP Client extension (SPEC §10.33, "emetos.httpclient"). Step 27.1 builds its files and its
// variables, without a user interface yet: format reads and writes the collections, folders, requests and environments of a
// project (the files under .emetos/http), store keeps the tree of them in step with the disk, and variables resolves {{name}}
// in the order of the SPEC and keeps current values and secrets away from the project. Step 27.2 adds engine, which builds and sends
// a request and captures the response (SPEC §10.33 "The engine"). Step 27.3 adds this package: the extension itself with its side bar
// (the Collections, Environments and History views), the status bar item and the picker of the active environment, and the
// environment editor tab; history keeps the requests that were sent. The request tab, the response pane, extraction and checks and the
// collection runner (rundata reads its data files) are built; OAuth 2.0 follows in a later step of M27.
package httpclient
