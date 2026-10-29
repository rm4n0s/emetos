// Package dbconn is what the three database extensions (SQL, MongoDB and Redis, SPEC §10.21–§10.23) share: the connection
// model and its file, the passwords and the desktop keyring, the connection form, and the "Databases" side bar view whose
// connections the extensions give their engines to (SPEC §10.24, D24).
//
// # Passwords
//
// A password is never written to a file of Emetos (D24). The connection file (db-connections.json) has no field for one and
// refuses a connection whose URL carries one; "Save password" keeps it in the desktop's keyring through the Secret Service
// D-Bus API (org.freedesktop.secrets), and a password that is not saved is asked when the connection is made and kept in
// memory until it is closed. A password may also come from an environment variable named in the form (${env:PGPASSWORD}).
// When no keyring answers, the password is held in memory for the session and the user is told so.
//
// # The pieces
//
// Connection and Store are the model and db-connections.json (mode 0600, written atomically, read only when nobody else can
// change it). Keyring is where a saved password goes: SecretService speaks the Secret Service API, and Passwords decides
// between the environment, the memory of the session and the keyring. Hub is the one object the extensions of every window
// tab share: the store, the passwords, the engines the database extensions registered (Driver) and the state of each
// connection. FormModel is the connection form without its widgets, and Extension is the extension "Databases" that shows the
// view and the form tab.
package dbconn
