// Package protocol is the message format of Remote Work (SPEC §10.18): the messages the host and the client send
// each other once the login is done, in a binary form that is versioned, length-prefixed and limited in size.
//
// A message is a type byte, a length (4 bytes, big endian) and a payload of exactly that length:
//
//	type:1 | length:4 | payload:length
//
// The length is checked against the limit of the type before anything is read or allocated, so a peer that lies about
// it costs nothing: no decoder allocates more than the limit of the type it was told (MaxPayload). A message is
// refused, never repaired, when its type is unknown, its length is over the limit, it ends early, it has bytes left
// over, or a field holds a value the format does not allow (an unknown enum, a string that is not UTF-8, a number that
// is not finite, a size of zero). Every accepted message encodes back to the bytes it was decoded from.
//
// The package imports nothing of Emetos but the errors package, so the Relay protocol (SPEC §10.19) and the tests of
// the transports can use it. The pixels travel as Tile messages, the pictures themselves are made by package tiles. A Missing
// lists the tiles of the newest picture a client did not receive on the datagrams of package transport (step 15.3).
package protocol
