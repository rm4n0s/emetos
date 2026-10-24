// Package ipc hands the command line of a second launch over to the running instance through a Unix socket (SPEC §10.11).
//
// The first Emetos process of a user listens on a socket (Locations); a later launch connects, sends one
// versioned JSON message that holds its working directory and what it was asked to open, and exits with
// the exit code of the reply, which with --wait comes only when the files are closed. Join decides which of
// the two a process is, under a lock file, so that two launches at once end as one instance and one client.
// This is the only package that may import net (SPEC §12), and it opens Unix sockets only.
package ipc
