// Package engine sends the requests of the HTTP client and captures everything about the response (SPEC §10.33 "The engine").
//
// Prepare builds a request from the model of package format and the variables of package variables: the URL with its
// parameters, the headers (those a collection and its folders pass down, then the request's), the authorization and the body of
// every kind of the SPEC. Send does Prepare and then sends: it follows or keeps the redirects, answers a Digest challenge,
// keeps the cookies of the environment in a jar kept in the state, decodes gzip and deflate, takes the timings from
// net/http/httptrace, and keeps the body in memory up to a limit (50 MiB) and in a file of the cache beyond it. The context
// cancels a request at any moment.
//
// Nothing here knows a user interface, and nothing reads the user's configuration: everything comes in through Config and
// Input. Only the standard library is used.
package engine
