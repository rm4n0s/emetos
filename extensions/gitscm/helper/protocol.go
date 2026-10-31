package helper

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"time"

	"github.com/rm4n0s/errors"
)

// The flags of the binary that cmd/emetos hands to Main.
const (
	// FlagAskpass is GIT_ASKPASS: the prompt of git for an HTTPS user name or password.
	FlagAskpass = "--git-askpass"
	// FlagSSHAskpass is SSH_ASKPASS: the passphrase or host key question of ssh.
	FlagSSHAskpass = "--git-ssh-askpass"
	// FlagEditor is GIT_EDITOR: the editor of a commit message or a rebase todo list.
	FlagEditor = "--git-editor"
)

// The environment variables of the helper.
const (
	// EnvSocket is the path of the socket of the server; the extension gives it to every command that may prompt.
	EnvSocket = "EMETOS_GIT_SOCKET"
	// EnvToken is the token the server wants in every request (32 random bytes, hexadecimal).
	EnvToken = "EMETOS_GIT_TOKEN"
	// EnvCommand is the git command that is running (EMETOS_GIT_COMMAND, VS Code's VSCODE_GIT_COMMAND).
	EnvCommand = "EMETOS_GIT_COMMAND"
	// EnvFetchSilent, with EMETOS_GIT_COMMAND=fetch, marks a fetch the user did not ask for (autofetch): it never prompts (VS Code's
	// VSCODE_GIT_FETCH_SILENT).
	EnvFetchSilent = "EMETOS_GIT_FETCH_SILENT"
	// EnvSSHPrompt is the type of prompt that ssh (OpenSSH 8.4 and newer) puts in the environment of the askpass program: "confirm" for a
	// yes or no question, "none" for a message, empty for an input.
	EnvSSHPrompt = "SSH_ASKPASS_PROMPT"
)

// Kind says what a request is.
type Kind string

// The kinds of request.
const (
	// KindAskpass is a prompt of git (GIT_ASKPASS): Username for 'https://host': .
	KindAskpass Kind = "askpass"
	// KindSSHAskpass is a prompt of ssh (SSH_ASKPASS): a passphrase, a password or the question about a host key.
	KindSSHAskpass Kind = "ssh-askpass"
	// KindEditor is a file git wants edited (GIT_EDITOR).
	KindEditor Kind = "editor"
)

// The bounds of the protocol.
const (
	// MaxRequestBytes is the most a request may have: 64 KiB, the line of JSON. A longer request is refused.
	MaxRequestBytes = 64 << 10
	// MaxReplyBytes is the most a reply may have: 1 MiB.
	MaxReplyBytes = 1 << 20
	// DefaultRequestTimeout is how long a connection has to send its request.
	DefaultRequestTimeout = 5 * time.Second
	// DefaultReplyTimeout is how long the server has to write a reply to a client that does not read.
	DefaultReplyTimeout = 10 * time.Second
	// MaxConnections is the most connections the server serves at the same time.
	MaxConnections = 32
)

// Request is what the client sends: one line of JSON.
type Request struct {
	// Token is the token of the server (EnvToken).
	Token string `json:"token"`
	// Kind says what the request is.
	Kind Kind `json:"kind"`
	// Argv are the arguments git or ssh gave the program: the prompt for the askpass kinds, the options and the file for the editor.
	Argv []string `json:"argv,omitempty"`
	// Dir is the working directory of the client: git runs the editor in the repository with a path relative to it (.git/COMMIT_EDITMSG).
	Dir string `json:"dir,omitempty"`
	// Command is the git command that runs (EnvCommand).
	Command string `json:"command,omitempty"`
	// Silent says the command is a silent fetch (EnvFetchSilent).
	Silent bool `json:"silent,omitempty"`
	// SSHPrompt is the type of prompt that ssh gave (EnvSSHPrompt).
	SSHPrompt string `json:"sshPrompt,omitempty"`
}

// Reply is the answer: one line of JSON.
type Reply struct {
	// OK says the request was answered: the client prints Value and exits with 0. False is a cancel: it exits with 1.
	OK bool `json:"ok"`
	// Value is the text of an askpass answer. The editor kinds leave it empty.
	Value string `json:"value,omitempty"`
	// Error says why a request was refused, for the user; it never holds a secret.
	Error string `json:"error,omitempty"`
}

// readLine reads one line of at most max bytes (without the newline). A longer line is not read to its end: the caller closes the
// connection.
func readLine(r io.Reader, max int) ([]byte, error) {
	br := bufio.NewReaderSize(io.LimitReader(r, int64(max)+1), 4096)
	line, err := br.ReadBytes('\n')
	if len(line) > max {
		return nil, errors.New("GitHelperLineTooLong", "a line of the helper protocol is longer than the bound", "max", max)
	}
	if err != nil && (!errors.Is(err, io.EOF) || len(bytes.TrimSpace(line)) == 0) {
		return nil, errors.NewErr("GitHelperLineUnreadable", err)
	}
	return bytes.TrimRight(line, "\r\n"), nil
}

// writeJSON writes a value as one line of JSON.
func writeJSON(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return errors.NewErr("GitHelperEncodeFailed", err)
	}
	if _, err := w.Write(append(data, '\n')); err != nil {
		return errors.NewErr("GitHelperWriteFailed", err)
	}
	return nil
}
