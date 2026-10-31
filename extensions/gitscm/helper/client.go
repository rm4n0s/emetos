package helper

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rm4n0s/errors"
)

// DialTimeout is how long the client waits for the socket of the server.
const DialTimeout = 5 * time.Second

// IsHelperFlag says whether arg is one of the three flags Main serves; cmd/emetos calls Main before anything else when its first
// argument is one.
func IsHelperFlag(arg string) bool {
	return arg == FlagAskpass || arg == FlagSSHAskpass || arg == FlagEditor
}

// Main is the program git runs: args are the arguments of the process (the flag first, then what git or ssh gave), env its
// environment ("KEY=VALUE"), stdout where an askpass answer goes, and stderr where a failure is explained. It sends one request to the
// socket named by EMETOS_GIT_SOCKET with the token of EMETOS_GIT_TOKEN, prints the answer (askpass) and returns the exit status: 0
// when the request was answered, 1 when it was cancelled or could not be made. stdin is not read; git gives the askpass program none.
//
// Ported in idea from VS Code: extensions/git/src/askpass-main.ts and git-editor-main.ts.
func Main(args, env []string, stdin io.Reader, stdout, stderr io.Writer) int {
	_ = stdin
	if len(args) == 0 || !IsHelperFlag(args[0]) {
		say(stderr, "emetos: the helper needs --git-askpass, --git-ssh-askpass or --git-editor\n")
		return 1
	}
	kind := KindAskpass
	switch args[0] {
	case FlagSSHAskpass:
		kind = KindSSHAskpass
	case FlagEditor:
		kind = KindEditor
	}
	vars := environment(env)
	req := Request{
		Token: vars[EnvToken], Kind: kind, Argv: args[1:], Command: vars[EnvCommand],
		Silent: vars[EnvFetchSilent] != "", SSHPrompt: vars[EnvSSHPrompt],
	}
	if kind != KindEditor && req.Command == "fetch" && req.Silent {
		// a fetch the user did not ask for never asks for a secret (VS Code: "Skip silent fetch commands")
		say(stderr, "emetos: a silent fetch does not ask for credentials\n")
		return 1
	}
	if wd, err := os.Getwd(); err == nil {
		req.Dir = wd
	}
	reply, err := Call(context.Background(), vars[EnvSocket], req)
	if err != nil {
		say(stderr, "emetos: missing or invalid credentials: "+describe(err)+"\n")
		return 1
	}
	if !reply.OK {
		if reply.Error != "" {
			say(stderr, "emetos: "+reply.Error+"\n")
		}
		return 1
	}
	if kind != KindEditor {
		if _, err := io.WriteString(stdout, reply.Value+"\n"); err != nil {
			return 1
		}
	}
	return 0
}

// environment turns "KEY=VALUE" entries into a map; a later entry wins.
func environment(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok {
			m[k] = v
		}
	}
	return m
}

// say writes a message; a stderr that is broken leaves nowhere to say so, and the exit status tells.
func say(w io.Writer, msg string) { _, _ = io.WriteString(w, msg) }

// describe is the message of an error without its stack.
func describe(err error) string {
	if appErr, ok := errors.FromError(err); ok && appErr.Message != "" {
		return appErr.Message
	}
	return err.Error()
}

// Call sends the request to the server at socket and waits for its reply, for as long as the user takes to answer or until ctx ends.
// The error has the tags GitHelperSocketMissing, GitHelperDialFailed, GitHelperEncodeFailed, GitHelperWriteFailed,
// GitHelperLineTooLong, GitHelperLineUnreadable and GitHelperReplyInvalid.
func Call(ctx context.Context, socket string, req Request) (Reply, error) {
	if socket == "" {
		return Reply{}, errors.New("GitHelperSocketMissing", "the environment has no "+EnvSocket)
	}
	dialer := net.Dialer{Timeout: DialTimeout}
	conn, err := dialer.DialContext(ctx, "unix", socket)
	if err != nil {
		return Reply{}, errors.NewErr("GitHelperDialFailed", err).SetMetadata("socket", filepath.Base(socket))
	}
	defer func() { _ = conn.Close() }()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	if err := conn.SetWriteDeadline(time.Now().Add(DialTimeout)); err != nil {
		return Reply{}, errors.NewErr("GitHelperWriteFailed", err)
	}
	if err := writeJSON(conn, req); err != nil {
		return Reply{}, err
	}
	line, err := readLine(conn, MaxReplyBytes)
	if err != nil {
		return Reply{}, err
	}
	var reply Reply
	if err := json.Unmarshal(line, &reply); err != nil {
		return Reply{}, errors.NewErr("GitHelperReplyInvalid", err)
	}
	return reply, nil
}
