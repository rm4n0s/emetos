// Package history is the history of the requests the HTTP client sent in a workspace (SPEC §10.33 "The side bar", "As built (step
// 27.3)"): the last httpClient.historyLimit of them with the method, the URL, the status and the time, and a copy of the request with its
// secrets and authorization values masked. It is kept in Emetos's state, never in the project, and knows nothing of the user interface.
package history
