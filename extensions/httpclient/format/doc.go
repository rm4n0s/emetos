// Package format is the file format of the HTTP client, version 1 (SPEC §10.33 "The format"): the types of a collection, a
// folder, a request and an environment, reading them from text with comments, and writing them.
//
// Reading accepts comments and trailing commas (extension/jsonc). Writing a file that exists goes through the edits of the
// comment-keeping JSON editor, so the comments, the layout and the keys a user added by hand stay; a new file is written with the
// keys in the fixed order of the SPEC, two spaces and a final line break. A file of a newer version is read, and never written.
// A secret variable never has its value in text (D45).
//
// Nothing here knows the workspace except ResolvePath and the file helpers, which keep every path inside the workspace folder.
package format
