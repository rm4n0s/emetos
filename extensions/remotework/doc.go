// Package remotework is the Remote Work extension (SPEC §10.18): one workspace of an Emetos on laptop A can be used from an
// Emetos on laptop B. This package is the part that runs on A, with the login of step 15.2: the page "Remote Work" (the
// per-workspace switch, the port, the idle timeout, the allowed keys, the host fingerprint and the security notes), the
// listener that exists only while a workspace has Remote Work on, the SSH server of extensions/internal/sshlogin that lets
// the listed keys in, the session token and the banner "Remote Work: <key comment> connected" with its Disconnect button.
// The conversation on the channel is package login, the messages are package protocol and the pictures are package tiles.
//
// The extension is made once for the process (NewFactory) and activated in every workspace; the instances share one Hub,
// which holds the listener, the sessions and remote-work.json.
package remotework
