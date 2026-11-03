// Package checks reads a response of the HTTP client: it takes values out of it for extraction rules and tests it with checks (SPEC
// §10.33 "Extraction and checks"). There are no scripts (D44): a rule or a check is data, and what it does is fixed here. The package
// does no I/O of its own beyond reading the body of the response, and it writes no variable: the caller stores what Extract found.
//
// A rule or a check that cannot be applied does not stop the others: its result says why (Problem), with the text of an error whose
// tag is listed in the function that makes it.
package checks
