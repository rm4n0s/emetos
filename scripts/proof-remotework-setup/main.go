// Command proof-remotework-setup prepares the folders of scripts/proof-remotework.sh: a fresh client key, the state
// folder of the host instance (A: remote-work.json with the key allowed and the workspace on, and its host key) and the
// state folder of the client instance (B: remote-work-hosts.json with A's host key pinned, so that the proof does not
// stop at the first-use dialog). Everything is written under the folder given as the argument and nothing is kept
// in the repository: the keys are made on every run.
//
// Usage: go run ./scripts/proof-remotework-setup <out dir> <workspace folder>
//
// It prints the TCP and UDP port it picked on its last line.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "proof-remotework-setup:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) != 2 {
		return problem("usage: proof-remotework-setup <out dir> <workspace folder>")
	}
	out, err := filepath.Abs(args[0])
	if err != nil {
		return err
	}
	workspace, err := filepath.Abs(args[1])
	if err != nil {
		return err
	}
	port, err := freePort()
	if err != nil {
		return err
	}

	clientPub, clientPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	hostPub, hostPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}

	clientDir := filepath.Join(out, "client")
	aState := filepath.Join(out, "a-state")
	bState := filepath.Join(out, "b-state")
	for _, d := range []string{clientDir, aState, bState} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
	}

	if err := writePrivate(filepath.Join(clientDir, "id_ed25519"), clientPriv, "laptop B (proof)"); err != nil {
		return err
	}
	if err := writePrivate(filepath.Join(aState, "remote-work-host-key"), hostPriv, "emetos host key"); err != nil {
		return err
	}

	sshClient, err := ssh.NewPublicKey(clientPub)
	if err != nil {
		return err
	}
	sshHost, err := ssh.NewPublicKey(hostPub)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)

	cfg := map[string]any{
		"version": 1, "port": port, "idleMinutes": 30,
		"workspaces": []string{workspace},
		"keys": []map[string]string{{
			"line":  strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshClient))) + " laptop B (proof)",
			"added": now,
		}},
	}
	if err := writeJSON(filepath.Join(aState, "remote-work.json"), cfg); err != nil {
		return err
	}

	hosts := map[string]any{
		"version": 1,
		"hosts": map[string]any{
			net.JoinHostPort("127.0.0.1", fmt.Sprint(port)): map[string]string{
				"type":        sshHost.Type(),
				"fingerprint": ssh.FingerprintSHA256(sshHost),
				"key":         base64.StdEncoding.EncodeToString(sshHost.Marshal()),
				"firstSeen":   now,
			},
		},
	}
	if err := writeJSON(filepath.Join(bState, "remote-work-hosts.json"), hosts); err != nil {
		return err
	}
	fmt.Println(port)
	return nil
}

// problem is a message that ends the program.
type problem string

// Error returns the message.
func (p problem) Error() string { return string(p) }

// freePort finds a port that is free for both TCP and UDP on every interface.
func freePort() (int, error) {
	for range 50 {
		u, err := net.ListenPacket("udp", ":0")
		if err != nil {
			return 0, err
		}
		port := u.LocalAddr().(*net.UDPAddr).Port
		t, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		_ = u.Close()
		if err != nil {
			continue
		}
		_ = t.Close()
		return port, nil
	}
	return 0, problem("no port is free for both TCP and UDP")
}

func writePrivate(path string, key ed25519.PrivateKey, comment string) error {
	block, err := ssh.MarshalPrivateKey(key, comment)
	if err != nil {
		return err
	}
	return writeFile(path, pem.EncodeToMemory(block))
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(path, append(b, '\n'))
}

func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o600)
}
