// Package helper is the program git runs for passwords and editors, and the server in the extension that answers it (SPEC §10.30
// "Passwords and editors", D35). Git asks a program for secrets (GIT_ASKPASS for HTTPS, SSH_ASKPASS for ssh) and for an editor
// (GIT_EDITOR). VS Code points them at a Node script that talks to the extension over a socket; Emetos points them at three small shell
// scripts that start its own binary (emetos --git-askpass, --git-ssh-askpass, --git-editor), which talks to the extension over a Unix
// socket in a private folder.
//
// The client is Main: cmd/emetos runs it before anything else for the three flags, with no window, no single-instance lookup and no
// toolkit. The server is Server: it listens on a socket in $XDG_RUNTIME_DIR/emetos/git-<pid>/ (mode 0700, the socket 0600), checks a
// random token and the user id of the peer, reads one request with a size bound and a deadline, and hands it to a Handler.
// The scripts hold no secret; the token reaches git only through the environment of the commands (EMETOS_GIT_TOKEN).
//
// Nothing here stores or logs a secret: the reply of an askpass request goes from the Handler to the socket and to git, and the
// arguments of a request (which hold the prompt, with the name of a user or a host) are not logged.
//
// Ported in idea from VS Code: extensions/git/src/askpass.ts, askpass-main.ts, askpassManager.ts, git-editor-main.ts, gitEditor.ts and
// ipc/ (ipcServer.ts, ipcClient.ts).
package helper
