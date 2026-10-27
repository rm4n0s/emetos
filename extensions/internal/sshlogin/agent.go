package sshlogin

import (
	"context"
	"io"
	"net"
	"time"

	"github.com/rm4n0s/errors"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// agentTimeout is how long the connection to an agent and its list of keys may take.
const agentTimeout = 5 * time.Second

// AgentSocket is the socket of the ssh-agent the process runs under: the value of SSH_AUTH_SOCK, which the caller reads from
// the environment and gives as getenv (os.Getenv in the editor, a fake in a test). It is empty when there is none.
func AgentSocket(getenv func(string) string) string { return getenv("SSH_AUTH_SOCK") }

// AgentAuth connects to the ssh-agent at the socket and returns the Auth of its keys. With a fingerprint (SHA256:...) only
// that key is used, so a client with many keys does not offer them all; without one every key of the agent is offered, in
// the agent's order. Close the Auth when the connection is made. The errors have the tags SSHLoginAgentUnavailable (no
// socket, or no agent behind it), SSHLoginAgentFailed (the agent did not list its keys) and SSHLoginAgentNoKey (it has no
// key, or not the one asked for).
func AgentAuth(ctx context.Context, socket, fingerprint string) (*Auth, error) {
	if socket == "" {
		return nil, errors.New("SSHLoginAgentUnavailable", "there is no ssh-agent: SSH_AUTH_SOCK is not set")
	}
	signers, closer, err := agentSigners(ctx, socket)
	if err != nil {
		return nil, err
	}
	a := &Auth{closers: []io.Closer{closer}}
	for _, s := range signers {
		if fingerprint == "" || ssh.FingerprintSHA256(s.PublicKey()) == fingerprint {
			a.signers = append(a.signers, s)
		}
	}
	if len(a.signers) == 0 {
		_ = a.Close()
		return nil, errors.New("SSHLoginAgentNoKey", "the ssh-agent has no key, or not the one chosen", "fingerprint", fingerprint)
	}
	a.PublicKey = a.signers[0].PublicKey()
	return a, nil
}

// AgentKeys lists the public keys of the ssh-agent at the socket, for the user to choose one. It fails like AgentAuth.
func AgentKeys(ctx context.Context, socket string) ([]ssh.PublicKey, error) {
	if socket == "" {
		return nil, errors.New("SSHLoginAgentUnavailable", "there is no ssh-agent: SSH_AUTH_SOCK is not set")
	}
	signers, closer, err := agentSigners(ctx, socket)
	if err != nil {
		return nil, err
	}
	defer func() { _ = closer.Close() }()
	keys := make([]ssh.PublicKey, len(signers))
	for i, s := range signers {
		keys[i] = s.PublicKey()
	}
	return keys, nil
}

// agentSigners dials the agent and asks for its signers; the connection stays open for the signatures.
func agentSigners(ctx context.Context, socket string) ([]ssh.Signer, net.Conn, error) {
	d := net.Dialer{Timeout: agentTimeout}
	conn, err := d.DialContext(ctx, "unix", socket)
	if err != nil {
		return nil, nil, errors.NewErr("SSHLoginAgentUnavailable", err).SetMetadata("socket", socket)
	}
	_ = conn.SetDeadline(time.Now().Add(agentTimeout))
	signers, err := agent.NewClient(conn).Signers()
	if err != nil {
		_ = conn.Close()
		return nil, nil, errors.NewErr("SSHLoginAgentFailed", err).SetMetadata("socket", socket)
	}
	_ = conn.SetDeadline(time.Time{})
	return signers, conn, nil
}
