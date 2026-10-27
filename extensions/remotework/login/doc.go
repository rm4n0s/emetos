// Package login is the conversation that Remote Work holds on the SSH channel after the key has been accepted (SPEC §10.18
// "As built (step 15.2)"): the client opens the channel "emetos-remotework" with a request (the workspace it wants), and the
// host answers with one grant: the 256-bit session token and the SHA-256 fingerprint of the QUIC certificate (step 15.3), or
// the reason there is none. After the grant the channel carries the messages of package protocol (the TCP fallback of 15.3).
// Connect is the client's side: key file or agent, pinned host key, channel and grant in one call, for the tab of step 15.4.
//
// The grant is a 4-byte big endian length (at most MaxGrant) and a strict JSON object; the request is one version byte and
// the workspace name, in the data of the channel open message.
package login
