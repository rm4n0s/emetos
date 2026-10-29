// Package mongodb is the MongoDB extension (SPEC §10.22, D23): connections on the shared "Databases" view
// (extensions/internal/dbconn), the explorer of databases, collections, indexes and the sampled schema, and the playground language
// whose parser is the package playground.
//
// # How it fits the shared view
//
// The extension "Databases" shows the connections; this extension gives the one dbconn.Hub a Driver for the engine "mongodb". The
// hub asks the driver to connect, to list what is below a connection or a node (the nodes carry what the driver needs in their
// Data), and for the entries of a node's context menu. The servers that are open belong to one manager that every window tab's
// instance of the extension shares, as the hub is shared.
//
// # The server behind an interface
//
// Everything the explorer asks of a server goes through Backend, so that the explorer is tested against a fake. The real Backend
// is a client of the official driver (go.mongodb.org/mongo-driver/v2); a test of it against a server of its own is in the files with
// the build tag emetos_docker.
//
// # Passwords
//
// A password is never in a connection string of ours: the hub finds it (the environment, the memory, the keyring, D24) and gives it to
// Dial, which sets it on the credential of the driver's options. The password a user pastes inside a connection string moves out of
// the string to the keyring when the connection is saved (dbconn.StripURLPassword).
package mongodb
