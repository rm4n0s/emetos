// Package sshlogin is the login that Remote Work (SPEC §10.18) and Relay (SPEC §10.19) share: an SSH server for the
// machine that is controlled (A), and an SSH client for the machine that controls (B), built on golang.org/x/crypto/ssh.
//
// The server lets in the public keys of a list and nothing else: there is no password, no keyboard-interactive and no
// "none" login; the keys are compared by their wire bytes; the only thing an authenticated client may do is open one
// channel of the type the server was made for, and every other channel type, every request on the channel, and every global
// request is refused; the handshake, the wait for the channel and the number of unauthenticated connections are bounded;
// and failed attempts are rate-limited by address (5 a minute) before any work for a connection is done. The host key is an
// Ed25519 key made the first time and kept with mode 0600.
//
// The client proves itself with a key file in OpenSSH format (a passphrase asked through a function, which is never stored) or
// with the keys of an ssh-agent, whose socket the caller names; it never reads ~/.ssh or the environment itself. The host
// key of the server is pinned on first use in a file of the caller's (remote-work-hosts.json), and a key that changed is
// refused.
package sshlogin
