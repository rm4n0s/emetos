package login

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"golang.org/x/crypto/ssh"

	"github.com/rm4n0s/emetos/extensions/internal/sshlogin"
)

func wantRoute(t *testing.T, err error, route string) {
	t.Helper()
	if err == nil {
		t.Fatalf("no error, want %s", route)
	}
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("error %T (%v) is not an *errors.Error", err, err)
	}
	if !appErr.HasRoute(route) {
		t.Errorf("route = %s, want %s", appErr.Route(), route)
	}
}

func okGrant() Grant {
	return Grant{Status: StatusOK, Workspace: "project", Token: bytes.Repeat([]byte{7}, TokenSize), CertSHA256: bytes.Repeat([]byte{9}, FingerprintSize)}
}

func TestRequest(t *testing.T) {
	for _, name := range []string{"", "project", "ünïcode", strings.Repeat("x", MaxWorkspaceName)} {
		b, err := Request{Workspace: name}.Encode()
		if err != nil {
			t.Fatalf("Encode(%q): %v", name, err)
		}
		if len(b) > sshlogin.MaxChannelData {
			t.Errorf("a request of %d bytes does not fit in the data of a channel (%d)", len(b), sshlogin.MaxChannelData)
		}
		got, err := ParseRequest(b)
		if err != nil || got.Workspace != name {
			t.Errorf("round trip of %q = %q, %v", name, got.Workspace, err)
		}
	}
	_, err := Request{Workspace: strings.Repeat("x", MaxWorkspaceName+1)}.Encode()
	wantRoute(t, err, "Encode.RemoteWorkRequestInvalid")
	_, err = Request{Workspace: "\xff"}.Encode()
	wantRoute(t, err, "Encode.RemoteWorkRequestInvalid")
	for name, b := range map[string][]byte{
		"empty": nil, "version 2": {2, 'a'}, "too long": append([]byte{1}, bytes.Repeat([]byte{'x'}, MaxWorkspaceName+1)...), "not utf-8": {1, 0xff},
	} {
		_, err := ParseRequest(b)
		if err == nil {
			t.Errorf("%s: no error", name)
			continue
		}
		wantRoute(t, err, "ParseRequest.RemoteWorkRequestInvalid")
	}
}

func TestGrantRoundTrip(t *testing.T) {
	tests := []Grant{
		okGrant(),
		{Status: StatusOK, Workspace: "p", Token: bytes.Repeat([]byte{1}, TokenSize)},
		{Status: StatusNone},
		{Status: StatusAmbiguous, Workspaces: []string{"a", "b"}},
		{Status: StatusUnknown, Workspaces: []string{"a"}},
	}
	for _, g := range tests {
		var buf bytes.Buffer
		if err := WriteGrant(&buf, g); err != nil {
			t.Fatalf("WriteGrant(%+v): %v", g, err)
		}
		got, err := ReadGrant(&buf)
		if err != nil {
			t.Fatalf("ReadGrant: %v", err)
		}
		if got.Status != g.Status || got.Workspace != g.Workspace || !bytes.Equal(got.Token, g.Token) || !bytes.Equal(got.CertSHA256, g.CertSHA256) || strings.Join(got.Workspaces, ",") != strings.Join(g.Workspaces, ",") {
			t.Errorf("round trip: %+v -> %+v", g, got)
		}
		if buf.Len() != 0 {
			t.Errorf("%d bytes left after the grant", buf.Len())
		}
	}
}

func TestInvalidGrantsAreNotWritten(t *testing.T) {
	long := strings.Repeat("x", MaxWorkspaceName+1)
	many := make([]string, MaxWorkspaces+1)
	tests := map[string]Grant{
		"unknown status":         {Status: "maybe"},
		"ok without a token":     {Status: StatusOK, Workspace: "p"},
		"short token":            {Status: StatusOK, Workspace: "p", Token: []byte{1}},
		"ok without a workspace": {Status: StatusOK, Token: okGrant().Token},
		"odd fingerprint":        {Status: StatusOK, Workspace: "p", Token: okGrant().Token, CertSHA256: []byte{1, 2}},
		"refusal with a token":   {Status: StatusNone, Token: okGrant().Token},
		"refusal with workspace": {Status: StatusNone, Workspace: "p"},
		"long workspace":         {Status: StatusOK, Workspace: long, Token: okGrant().Token},
		"long name in the list":  {Status: StatusAmbiguous, Workspaces: []string{long}},
		"not utf-8":              {Status: StatusAmbiguous, Workspaces: []string{"\xff"}},
		"too many names":         {Status: StatusAmbiguous, Workspaces: many},
		"bad workspace utf-8":    {Status: StatusOK, Workspace: "\xff", Token: okGrant().Token},
	}
	for name, g := range tests {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			wantRoute(t, WriteGrant(&buf, g), "check.RemoteWorkGrantInvalid")
			if buf.Len() != 0 {
				t.Error("something was written")
			}
		})
	}
}

// frame is a grant message made by hand.
func frame(body string) []byte {
	return append(binary.BigEndian.AppendUint32(nil, uint32(len(body))), body...)
}

func TestReadGrantRefusesWhatItShould(t *testing.T) {
	tok := `"` + "BwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwc=" + `"`
	tests := []struct {
		name  string
		input []byte
		route string
	}{
		{"nothing", nil, "ReadGrant.RemoteWorkGrantReadFailed"},
		{"half a length", []byte{0, 0}, "ReadGrant.RemoteWorkGrantReadFailed"},
		{"length zero", []byte{0, 0, 0, 0}, "ReadGrant.RemoteWorkGrantInvalid"},
		{"length over the limit", binary.BigEndian.AppendUint32(nil, MaxGrant+1), "ReadGrant.RemoteWorkGrantInvalid"},
		{"a short body", append(binary.BigEndian.AppendUint32(nil, 50), "{}"...), "ReadGrant.RemoteWorkGrantReadFailed"},
		{"not json", frame("nope"), "ReadGrant.RemoteWorkGrantInvalid"},
		{"another version", frame(`{"version":2,"status":"none"}`), "ReadGrant.RemoteWorkGrantInvalid"},
		{"an unknown field", frame(`{"version":1,"status":"none","extra":1}`), "ReadGrant.RemoteWorkGrantInvalid"},
		{"two values", frame(`{"version":1,"status":"none"} {"version":1,"status":"none"}`), "ReadGrant.RemoteWorkGrantInvalid"},
		{"an invalid grant", frame(`{"version":1,"status":"ok","workspace":"p"}`), "check.RemoteWorkGrantInvalid"},
		{"a token of the wrong size", frame(`{"version":1,"status":"ok","workspace":"p","token":"AAAA"}`), "check.RemoteWorkGrantInvalid"},
		{"a good one", frame(`{"version":1,"status":"ok","workspace":"p","token":` + tok + `}`), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ReadGrant(bytes.NewReader(tt.input))
			if tt.route == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			wantRoute(t, err, tt.route)
		})
	}
	// the length is checked before the body is allocated: a lying header of 8 KiB costs nothing but the read
	_, err := ReadGrant(bytes.NewReader(binary.BigEndian.AppendUint32(nil, MaxGrant)))
	wantRoute(t, err, "ReadGrant.RemoteWorkGrantReadFailed")
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestWriteGrantFailure(t *testing.T) {
	wantRoute(t, WriteGrant(failWriter{}, okGrant()), "WriteGrant.RemoteWorkGrantWriteFailed")
	big := Grant{Status: StatusAmbiguous}
	for i := 0; i < MaxWorkspaces; i++ {
		big.Workspaces = append(big.Workspaces, strings.Repeat("é", MaxWorkspaceName/2))
	}
	// 16 names of 200 bytes fit in the limit
	if err := WriteGrant(io.Discard, big); err != nil {
		t.Errorf("the largest grant does not fit: %v", err)
	}
}

// ---- Connect ----

type host struct {
	addr   string
	key    ssh.Signer
	client ssh.Signer
	hosts  *sshlogin.HostStore
	priv   ed25519.PrivateKey
}

// startHost serves the channel type with the handler; the client key is listed and the host key pinned.
func startHost(t *testing.T, channelType string, handler func(*sshlogin.Session)) *host {
	t.Helper()
	mk := func() (ed25519.PrivateKey, ssh.Signer) {
		_, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		s, err := ssh.NewSignerFromKey(priv)
		if err != nil {
			t.Fatal(err)
		}
		return priv, s
	}
	_, hostKey := mk()
	priv, clientKey := mk()
	srv, err := sshlogin.NewServer(sshlogin.ServerOptions{
		HostKey: hostKey, ChannelType: channelType, Handler: handler, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Keys: func() []sshlogin.AllowedKey {
			return []sshlogin.AllowedKey{{Key: clientKey.PublicKey(), Comment: "test"}}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { defer close(done); _ = srv.Serve(ln) }()
	t.Cleanup(func() { srv.Close(); <-done })
	hosts := sshlogin.NewHostStore(filepath.Join(t.TempDir(), "hosts.json"), nil)
	if err := hosts.Pin(ln.Addr().String(), hostKey.PublicKey()); err != nil {
		t.Fatal(err)
	}
	return &host{addr: ln.Addr().String(), key: hostKey, client: clientKey, hosts: hosts, priv: priv}
}

func (h *host) options() ConnectOptions {
	return ConnectOptions{Address: h.addr, Auth: sshlogin.SignerAuth(h.client), Hosts: h.hosts, GrantTimeout: 5 * time.Second}
}

func TestConnect(t *testing.T) {
	var seen Request
	h := startHost(t, ChannelType, func(s *sshlogin.Session) {
		seen, _ = ParseRequest(s.ExtraData)
		_ = WriteGrant(s.Channel, okGrant())
		<-s.Done()
	})
	o := h.options()
	o.Workspace = "project"
	c, err := Connect(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	if seen.Workspace != "project" || c.Grant.Workspace != "project" || len(c.Grant.Token) != TokenSize || c.Channel == nil {
		t.Errorf("seen %+v, connection %+v", seen, c.Grant)
	}
	// a context that ends later does not close a connection that was made
	ctx, cancel := context.WithCancel(context.Background())
	c2, err := Connect(ctx, h.options())
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	time.Sleep(100 * time.Millisecond)
	select {
	case <-waitDone(c2):
		t.Error("the connection was closed by the end of the context after Connect had returned")
	default:
	}
	_ = c2.Close()
}

func waitDone(c *Connection) <-chan struct{} {
	ch := make(chan struct{})
	go func() { _ = c.Client.Wait(); close(ch) }()
	return ch
}

func TestConnectDenied(t *testing.T) {
	for _, g := range []Grant{{Status: StatusNone}, {Status: StatusAmbiguous, Workspaces: []string{"a", "b"}}, {Status: StatusUnknown, Workspaces: []string{"a"}}} {
		g := g
		t.Run(g.Status, func(t *testing.T) {
			h := startHost(t, ChannelType, func(s *sshlogin.Session) { _ = WriteGrant(s.Channel, g) })
			_, err := Connect(context.Background(), h.options())
			wantRoute(t, err, "Connect.RemoteWorkLoginDenied")
			appErr, _ := errors.FromError(err)
			if appErr.Metadata["status"] != g.Status {
				t.Errorf("metadata = %v", appErr.Metadata)
			}
			if names, _ := appErr.Metadata["workspaces"].([]string); strings.Join(names, ",") != strings.Join(g.Workspaces, ",") {
				t.Errorf("workspaces = %v", appErr.Metadata["workspaces"])
			}
		})
	}
	if deniedWhy("other") != "other" {
		t.Error("an unknown status is said as it is")
	}
}

func TestConnectFailures(t *testing.T) {
	t.Run("the host never answers", func(t *testing.T) {
		h := startHost(t, ChannelType, func(s *sshlogin.Session) { <-s.Done() })
		o := h.options()
		o.GrantTimeout = 150 * time.Millisecond
		start := time.Now()
		_, err := Connect(context.Background(), o)
		wantRoute(t, err, "ReadGrant.RemoteWorkGrantReadFailed")
		if time.Since(start) > 5*time.Second {
			t.Error("the wait was not bounded")
		}
	})
	t.Run("the context ends while it waits", func(t *testing.T) {
		h := startHost(t, ChannelType, func(s *sshlogin.Session) { <-s.Done() })
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		defer cancel()
		_, err := Connect(ctx, h.options())
		wantRoute(t, err, "ReadGrant.RemoteWorkGrantReadFailed")
	})
	t.Run("a grant that is not one", func(t *testing.T) {
		h := startHost(t, ChannelType, func(s *sshlogin.Session) { _, _ = s.Channel.Write(frame("garbage")); <-s.Done() })
		_, err := Connect(context.Background(), h.options())
		wantRoute(t, err, "ReadGrant.RemoteWorkGrantInvalid")
	})
	t.Run("another channel type", func(t *testing.T) {
		h := startHost(t, "emetos-other-channel", func(s *sshlogin.Session) {})
		_, err := Connect(context.Background(), h.options())
		wantRoute(t, err, "OpenChannel.SSHLoginChannelRefused")
	})
	t.Run("a workspace name that is too long", func(t *testing.T) {
		o := ConnectOptions{Workspace: strings.Repeat("x", 300)}
		_, err := Connect(context.Background(), o)
		wantRoute(t, err, "Encode.RemoteWorkRequestInvalid")
	})
	t.Run("a host that is not there", func(t *testing.T) {
		h := startHost(t, ChannelType, func(s *sshlogin.Session) {})
		o := h.options()
		o.Address = "127.0.0.1:1"
		_, err := Connect(context.Background(), o)
		wantRoute(t, err, "dial.SSHLoginConnectFailed")
	})
}
