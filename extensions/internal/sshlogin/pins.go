package sshlogin

import (
	"encoding/base64"
	"encoding/json"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rm4n0s/errors"
	"golang.org/x/crypto/ssh"

	"github.com/rm4n0s/emetos/extensions/internal/statefile"
)

// hostsFileVersion is the version of the file of pinned host keys, and maxHostsFile its largest size.
const (
	hostsFileVersion = 1
	maxHostsFile     = 4 << 20
)

// Pinned is the host key that a client trusts for an address.
type Pinned struct {
	// Address is host:port, with the host in lower case.
	Address string
	// Type is the algorithm of the key (ssh-ed25519) and Fingerprint its SHA-256 fingerprint (SHA256:base64).
	Type        string
	Fingerprint string
	// Key is the key.
	Key ssh.PublicKey
	// FirstSeen is when the user accepted it.
	FirstSeen time.Time
}

// Verdict is what a HostStore says about the key a server showed.
type Verdict int

// The verdicts of HostStore.Check.
const (
	// HostUnknown is an address with no pinned key: the first use.
	HostUnknown Verdict = iota
	// HostTrusted is a key equal to the pinned one.
	HostTrusted
	// HostChanged is a key different from the pinned one.
	HostChanged
)

// HostStore keeps the pinned host keys of a client in a JSON file (remote-work-hosts.json), as
// known_hosts does. The file is written atomically with mode 0600 and read only when nobody else can change it. Every call
// reads the file again, so the store never trusts a copy in memory; the calls of one store are serialized (two processes
// that pin at the same moment may lose one pin, which the next connection makes again). Safe for concurrent use.
type HostStore struct {
	path string
	now  func() time.Time
	mu   sync.Mutex
}

// NewHostStore makes the store of the file at path; now is the clock of the first-seen times (time.Now when nil).
func NewHostStore(path string, now func() time.Time) *HostStore {
	if now == nil {
		now = time.Now
	}
	return &HostStore{path: path, now: now}
}

// hostsFile and hostEntry are the JSON of the file.
type hostsFile struct {
	Version int                  `json:"version"`
	Hosts   map[string]hostEntry `json:"hosts"`
}

type hostEntry struct {
	Type        string `json:"type"`
	Fingerprint string `json:"fingerprint"`
	Key         string `json:"key"`
	FirstSeen   string `json:"firstSeen"`
}

// NormalizeAddress makes host:port with the host in lower case, the key of a pin; a port is required. It fails with
// SSHLoginAddressInvalid.
func NormalizeAddress(addr string) (string, error) {
	host, port, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil || host == "" || port == "" {
		return "", errors.New("SSHLoginAddressInvalid", "an address is host:port", "address", addr)
	}
	return net.JoinHostPort(strings.ToLower(host), port), nil
}

// load reads the file; a file that does not exist is an empty one. The caller holds mu.
func (s *HostStore) load() (hostsFile, error) {
	data, err := statefile.Read(s.path, maxHostsFile, statefile.Writable)
	if statefile.Missing(err) {
		return hostsFile{Version: hostsFileVersion, Hosts: map[string]hostEntry{}}, nil
	}
	if err != nil {
		return hostsFile{}, err
	}
	var f hostsFile
	if err := json.Unmarshal(data, &f); err != nil {
		return hostsFile{}, errors.NewErr("SSHLoginHostsFileInvalid", err).SetMetadata("path", s.path)
	}
	if f.Version != hostsFileVersion {
		return hostsFile{}, errors.New("SSHLoginHostsFileInvalid", "the file of host keys has a version this Emetos does not know", "path", s.path, "version", f.Version)
	}
	if f.Hosts == nil {
		f.Hosts = map[string]hostEntry{}
	}
	return f, nil
}

// save writes the file. The caller holds mu.
func (s *HostStore) save(f hostsFile) error {
	f.Version = hostsFileVersion
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return errors.NewErr("SSHLoginHostsFileInvalid", err).SetMetadata("path", s.path)
	}
	return statefile.Write(s.path, append(data, '\n'))
}

// entryKey decodes the key of an entry.
func (s *HostStore) entryKey(addr string, e hostEntry) (ssh.PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(e.Key)
	if err != nil {
		return nil, errors.NewErr("SSHLoginHostsFileInvalid", err).SetMetadata("path", s.path, "address", addr)
	}
	key, err := ssh.ParsePublicKey(raw)
	if err != nil {
		return nil, errors.NewErr("SSHLoginHostsFileInvalid", err).SetMetadata("path", s.path, "address", addr)
	}
	return key, nil
}

// Check compares the key a server showed with the one pinned for the address. For HostChanged it returns the pinned entry as
// well, so the warning can show both fingerprints. The errors have the tags of statefile.Read, SSHLoginHostsFileInvalid and
// SSHLoginAddressInvalid.
func (s *HostStore) Check(addr string, key ssh.PublicKey) (Verdict, Pinned, error) {
	addr, err := NormalizeAddress(addr)
	if err != nil {
		return HostUnknown, Pinned{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load()
	if err != nil {
		return HostUnknown, Pinned{}, err
	}
	e, ok := f.Hosts[addr]
	if !ok {
		return HostUnknown, Pinned{}, nil
	}
	pinned, err := s.entryKey(addr, e)
	if err != nil {
		return HostUnknown, Pinned{}, err
	}
	p := Pinned{Address: addr, Type: pinned.Type(), Fingerprint: ssh.FingerprintSHA256(pinned), Key: pinned, FirstSeen: parseTime(e.FirstSeen)}
	if SameKey(pinned, key) {
		return HostTrusted, p, nil
	}
	return HostChanged, p, nil
}

// Pin trusts the key for the address from now on, replacing a key pinned before.
func (s *HostStore) Pin(addr string, key ssh.PublicKey) error {
	addr, err := NormalizeAddress(addr)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load()
	if err != nil {
		return err
	}
	f.Hosts[addr] = hostEntry{
		Type: key.Type(), Fingerprint: ssh.FingerprintSHA256(key), Key: base64.StdEncoding.EncodeToString(key.Marshal()),
		FirstSeen: s.now().UTC().Format(time.RFC3339),
	}
	return s.save(f)
}

// Forget removes the pin of the address, so that the next connection is a first use again. It is what the user chooses after
// a changed key was explained (a server that was installed anew); nothing calls it by itself.
func (s *HostStore) Forget(addr string) error {
	addr, err := NormalizeAddress(addr)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load()
	if err != nil {
		return err
	}
	if _, ok := f.Hosts[addr]; !ok {
		return nil
	}
	delete(f.Hosts, addr)
	return s.save(f)
}

// List returns the pinned hosts, by address.
func (s *HostStore) List() ([]Pinned, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load()
	if err != nil {
		return nil, err
	}
	out := make([]Pinned, 0, len(f.Hosts))
	for addr, e := range f.Hosts {
		key, err := s.entryKey(addr, e)
		if err != nil {
			return nil, err
		}
		out = append(out, Pinned{Address: addr, Type: key.Type(), Fingerprint: ssh.FingerprintSHA256(key), Key: key, FirstSeen: parseTime(e.FirstSeen)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Address < out[j].Address })
	return out, nil
}

// parseTime reads a time of the file; the zero time when it is not one.
func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
