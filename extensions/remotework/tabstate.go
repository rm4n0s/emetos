package remotework

import (
	"encoding/json"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/internal/sshlogin"
	"github.com/rm4n0s/emetos/extensions/remotework/protocol"
	"github.com/rm4n0s/emetos/extensions/remotework/transport"
)

// TabKindID is the kind of the window tab "Remote Work..." (SPEC §10.18 "As built (step 15.4)").
const TabKindID = "emetos.remotework.tab"

// The ways a tab chooses its key.
const (
	// keyFile is the private key file named by the tab, and keyAgent the ssh-agent of SSH_AUTH_SOCK.
	keyFile  = "file"
	keyAgent = "agent"
)

// tabState is what a tab keeps across restarts (the state of the window tab): where to connect and which key to use. It never
// holds a passphrase, a token or a key; the path of a key file is a name, and the agent's key is named by its fingerprint.
type tabState struct {
	// Host and Port are the computer to connect to; Port 0 is the default port.
	Host string `json:"host"`
	Port int    `json:"port"`
	// Key is "file" or "agent". KeyFile is the path of the private key file, and Fingerprint the SHA256:... fingerprint of the
	// agent's key to use (empty: every key of the agent is offered).
	Key         string `json:"key"`
	KeyFile     string `json:"keyFile,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	// Workspace is the workspace of the host that was used last, so that a host with several asks only once.
	Workspace string `json:"workspace,omitempty"`
}

// parseTabState reads the state a tab saved; nothing is a new tab. It fails with RemoteWorkTabStateInvalid.
func parseTabState(raw json.RawMessage) (tabState, error) {
	st := tabState{Port: DefaultPort, Key: keyFile}
	if len(raw) == 0 {
		return st, nil
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&st); err != nil {
		return tabState{}, errors.NewErr("RemoteWorkTabStateInvalid", err)
	}
	if st.Port == 0 {
		st.Port = DefaultPort
	}
	if err := CheckPort(st.Port); err != nil || (st.Key != keyFile && st.Key != keyAgent) {
		return tabState{}, errors.New("RemoteWorkTabStateInvalid", "the saved state of the tab has a port or a way of choosing the key that is not allowed", "port", st.Port, "key", st.Key)
	}
	return st, nil
}

// connectParams are what a connection needs, read from the form of the tab.
type connectParams struct {
	// Host is the name or address typed, Port the port, and Addr both as host:port.
	Host string
	Port int
	Addr string
	// KeyFile is the private key file; empty means the ssh-agent, and Fingerprint then names its key (empty: any).
	KeyFile     string
	Fingerprint string
	// Workspace is the workspace to use; empty for the only one, or to be asked.
	Workspace string
}

// params reads the connection from the state of the form. It fails with RemoteWorkHostInvalid, RemoteWorkPortInvalid and
// RemoteWorkKeyChoiceInvalid.
func (s tabState) params() (connectParams, error) {
	host := strings.TrimSpace(s.Host)
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	if host == "" || strings.ContainsAny(host, " \t/@\\") {
		return connectParams{}, errors.New("RemoteWorkHostInvalid", "the host is the name or the address of the other computer", "host", s.Host)
	}
	port := s.Port
	if err := CheckPort(port); err != nil {
		return connectParams{}, errors.New("RemoteWorkPortInvalid", "the port is a number from 1 to 65535", "port", port)
	}
	p := connectParams{Host: host, Port: port, Addr: net.JoinHostPort(strings.ToLower(host), strconv.Itoa(port)), Workspace: s.Workspace}
	switch s.Key {
	case keyAgent:
		p.Fingerprint = s.Fingerprint
	case keyFile:
		p.KeyFile = strings.TrimSpace(s.KeyFile)
		if p.KeyFile == "" {
			return connectParams{}, errors.New("RemoteWorkKeyChoiceInvalid", "choose the private key file, or the ssh-agent")
		}
	default:
		return connectParams{}, errors.New("RemoteWorkKeyChoiceInvalid", "the key is a file or the ssh-agent", "key", s.Key)
	}
	return p, nil
}

// TabOptions are the knobs of the remote window tab; every field has a default, and the tests set their own.
type TabOptions struct {
	// Backoff is how long to wait before the reconnection attempt n (counted from 1) after a connection dropped; the default is 1 s,
	// doubling to 30 s.
	Backoff func(attempt int) time.Duration
	// AgentSocket gives the socket of the ssh-agent; the default is the value of SSH_AUTH_SOCK.
	AgentSocket func() string
	// ResizeDelay is how long the size of the tab must stay as it is before the new size is sent (150 ms by default).
	ResizeDelay time.Duration
	// Transport, if not nil, changes the options of the client's transport before it connects (tests shorten its timeouts).
	Transport func(*transport.ClientOptions)
	// HandshakeTimeout bounds the connection and the login (the default of sshlogin).
	HandshakeTimeout time.Duration
}

// The defaults of TabOptions.
const (
	defaultResizeDelay = 150 * time.Millisecond
	maxBackoff         = 30 * time.Second
)

func (o TabOptions) backoff(attempt int) time.Duration {
	if o.Backoff != nil {
		return o.Backoff(attempt)
	}
	d := time.Second << min(max(attempt-1, 0), 5)
	return min(d, maxBackoff)
}

func (o TabOptions) resizeDelay() time.Duration {
	if o.ResizeDelay > 0 {
		return o.ResizeDelay
	}
	return defaultResizeDelay
}

func (o TabOptions) agentSocket() string {
	if o.AgentSocket != nil {
		return o.AgentSocket()
	}
	return sshlogin.AgentSocket(os.Getenv)
}

// fitSize makes the size of the tab a size the protocol allows: at least 1 pixel, at most MaxDimension a side and MaxPixels in
// all (a larger tab shows a picture smaller than itself).
func fitSize(w, h int) (int, int) {
	w, h = min(max(w, 1), protocol.MaxDimension), min(max(h, 1), protocol.MaxDimension)
	for w*h > protocol.MaxPixels {
		w, h = w*9/10, h*9/10
	}
	return max(w, 1), max(h, 1)
}
