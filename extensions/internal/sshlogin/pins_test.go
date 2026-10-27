package sshlogin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHostStore(t *testing.T) {
	clock := newClock()
	path := filepath.Join(t.TempDir(), "state", "hosts.json")
	s := NewHostStore(path, clock.Now)
	a, b := newKey(t).signer.PublicKey(), newKey(t).signer.PublicKey()

	if v, _, err := s.Check("Host.Example:47123", a); err != nil || v != HostUnknown {
		t.Fatalf("Check on an empty store = %v, %v", v, err)
	}
	if err := s.Pin("Host.Example:47123", a); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("hosts file: %v, %v", info, err)
	}
	// the host name is not case sensitive, the port is part of the address
	tests := []struct {
		addr string
		key  [2]bool // key a, key b
		want Verdict
	}{
		{"host.example:47123", [2]bool{true, false}, HostTrusted},
		{"HOST.EXAMPLE:47123", [2]bool{false, true}, HostChanged},
		{"host.example:47124", [2]bool{true, false}, HostUnknown},
		{"other.example:47123", [2]bool{true, false}, HostUnknown},
	}
	for _, tt := range tests {
		key := a
		if tt.key[1] {
			key = b
		}
		v, p, err := s.Check(tt.addr, key)
		if err != nil || v != tt.want {
			t.Errorf("Check(%s) = %v, %v; want %v", tt.addr, v, err, tt.want)
		}
		if v != HostUnknown && (p.Address != "host.example:47123" || !SameKey(p.Key, a) || !p.FirstSeen.Equal(clock.Now())) {
			t.Errorf("pinned = %+v", p)
		}
	}
	// a second store over the same file sees the pin; List is sorted
	other := NewHostStore(path, nil)
	if err := other.Pin("a.example:1", b); err != nil {
		t.Fatal(err)
	}
	list, err := s.List()
	if err != nil || len(list) != 2 || list[0].Address != "a.example:1" || list[1].Address != "host.example:47123" {
		t.Fatalf("List = %+v, %v", list, err)
	}
	if err := s.Forget("nothing.example:1"); err != nil {
		t.Errorf("Forget of an unknown address: %v", err)
	}
	if err := s.Forget("a.example:1"); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.List(); len(list) != 1 {
		t.Errorf("List after Forget = %+v", list)
	}
}

func TestHostStoreErrors(t *testing.T) {
	a := newKey(t).signer.PublicKey()
	write := func(t *testing.T, content string, mode os.FileMode) *HostStore {
		t.Helper()
		p := filepath.Join(t.TempDir(), "hosts.json")
		if err := os.WriteFile(p, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, mode); err != nil {
			t.Fatal(err)
		}
		return NewHostStore(p, nil)
	}
	tests := []struct {
		name  string
		store *HostStore
		run   func(*HostStore) error
		route string
	}{
		{"bad address on Check", NewHostStore("x", nil), func(s *HostStore) error { _, _, err := s.Check("nope", a); return err }, "NormalizeAddress.SSHLoginAddressInvalid"},
		{"bad address on Pin", NewHostStore("x", nil), func(s *HostStore) error { return s.Pin("nope", a) }, "NormalizeAddress.SSHLoginAddressInvalid"},
		{"bad address on Forget", NewHostStore("x", nil), func(s *HostStore) error { return s.Forget(":1") }, "NormalizeAddress.SSHLoginAddressInvalid"},
		{"junk file", write(t, "junk", 0o600), func(s *HostStore) error { _, err := s.List(); return err }, "load.SSHLoginHostsFileInvalid"},
		{"other version", write(t, `{"version":9,"hosts":{}}`, 0o600), func(s *HostStore) error { return s.Pin("h:1", a) }, "load.SSHLoginHostsFileInvalid"},
		{"writable by others", write(t, `{"version":1,"hosts":{}}`, 0o666), func(s *HostStore) error { _, _, err := s.Check("h:1", a); return err }, "Read.StateFileUnsafeMode"},
		{"bad base64 key", write(t, `{"version":1,"hosts":{"h:1":{"key":"!!"}}}`, 0o600), func(s *HostStore) error { _, _, err := s.Check("h:1", a); return err }, "entryKey.SSHLoginHostsFileInvalid"},
		{"not a key", write(t, `{"version":1,"hosts":{"h:1":{"key":"AAAA"}}}`, 0o600), func(s *HostStore) error { _, err := s.List(); return err }, "entryKey.SSHLoginHostsFileInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { wantRoute(t, tt.run(tt.store), tt.route) })
	}
	// a file that cannot be saved
	s := NewHostStore(filepath.Join(readOnlyDir(t), "sub", "hosts.json"), nil)
	wantRoute(t, s.Pin("h:1", a), "Write.StateFileWriteFailed")
	// a file in the state that is readable by others is fine (it holds public keys)
	if _, _, err := write(t, `{"version":1,"hosts":{}}`, 0o644).Check("h:1", a); err != nil {
		t.Errorf("a readable file was refused: %v", err)
	}
}
