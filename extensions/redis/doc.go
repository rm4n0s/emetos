// Package redis is the extension "Redis" (emetos.redis, SPEC §10.23): it connects to Redis, Valkey and servers that speak RESP, lists
// their databases and keys in the Databases view, and opens a key in an editor to view and change its value.
//
// # The pieces
//
// The client is behind three small interfaces (Dialer, Server, DB; client.go) so that everything above it is tested against an
// in-memory server: GoDialer is the one over go-redis, ReadOnly wraps a server so that it refuses every write, and BuildSpec turns a
// connection of the Databases view into the Spec a Dialer needs. Driver is the dbconn.Driver of the engine: it keeps one session for
// each open connection, and the explorer (explorer.go, with extensions/redis/keytree) scans the keys with SCAN in pages and lists them
// one level of the tree at a time. The key editor (keytab.go) shows a string or a ReJSON value in a text box and the other types as
// Collections, rows loaded in pages that the user adds to, edits and removes; the New Key form (newkeytab.go) makes a key of any type.
//
// # Safety
//
// A password never reaches this package except as an argument of Connect. No error or log line carries a key or a value. The
// explorer never sends KEYS.
package redis
