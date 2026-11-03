// Package httpclient is the HTTP Client extension (SPEC §10.33, "emetos.httpclient"). Step 27.1 builds its files and its
// variables, without a user interface yet: format reads and writes the collections, folders, requests and environments of a
// project (the files under .emetos/http), store keeps the tree of them in step with the disk, and variables resolves {{name}}
// in the order of the SPEC and keeps current values and secrets away from the project. The extension itself (the side bar, the
// request tab) follows in the later steps of M27. Step 27.2 adds engine, which builds and sends a request and captures the
// response (SPEC §10.33 "The engine").
package httpclient
