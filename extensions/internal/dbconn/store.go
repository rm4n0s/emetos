package dbconn

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/internal/statefile"
)

// FileName is the name of the connection file in $XDG_STATE_HOME/emetos.
const FileName = "db-connections.json"

// The limits and the version of the file.
const (
	// fileVersion is the version of the format the file is written in.
	fileVersion = 1
	// maxFileSize is the largest file the store reads; it is far above what anyone writes by hand.
	maxFileSize = 4 << 20
	// MaxConnections is the most connections the store keeps.
	MaxConnections = 1000
)

// fileFormat is the content of db-connections.json. It has no field for a password, and a connection has none either.
type fileFormat struct {
	Version     int          `json:"version"`
	Connections []Connection `json:"connections"`
}

// StorePath is $XDG_STATE_HOME/emetos/db-connections.json. The error has the tag StateFileDirUnknown.
func StorePath() (string, error) {
	dir, err := statefile.DirFromEnv()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, FileName), nil
}

// Store keeps the connections of the user in db-connections.json. The file has mode 0600 in a folder of mode 0700, is replaced
// atomically, is written only by Save and Delete, and is read only when nobody else can change it. It is safe for use from several
// goroutines.
type Store struct {
	path string

	mu    sync.Mutex
	conns []Connection
	// loadErr is why the file could not be read; while it is set, nothing is written, so that a file that is not understood is
	// never overwritten.
	loadErr error
}

// NewStore makes a store over the file at path and reads it. A file that does not exist is an empty store; a file that cannot be
// used leaves the store empty and read-only, and the error is what Load returned (Err gives it again).
func NewStore(path string) *Store {
	s := &Store{path: path}
	_ = s.Load() // the error stays in s.loadErr, for Err; the store is empty and refuses to write
	return s
}

// Path is the file of the store.
func (s *Store) Path() string { return s.path }

// Err is the error of the last Load, nil when the file was read (or is missing).
func (s *Store) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadErr
}

// Load reads the file again. The errors have the tags of statefile.Read (StateFileUnsafeMode, StateFileNotRegular,
// StateFileTooLarge, StateFileReadFailed), ConnectionsFileInvalid, ConnectionsFileVersion and ConnectionsFileTooMany, and
// those of Connection.Check for a connection of the file that holds what must not be there.
func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	conns, err := s.read()
	s.loadErr = err
	if err != nil {
		s.conns = nil
		return err
	}
	s.conns = conns
	return nil
}

// read reads and checks the file.
func (s *Store) read() ([]Connection, error) {
	data, err := statefile.Read(s.path, maxFileSize, statefile.Writable)
	if err != nil {
		if statefile.Missing(err) {
			return nil, nil
		}
		return nil, err
	}
	var f fileFormat
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, errors.NewErr("ConnectionsFileInvalid", err).SetMetadata("path", s.path)
	}
	if f.Version != fileVersion {
		return nil, errors.New("ConnectionsFileVersion", "the connection file is of a version this Emetos does not know", "path", s.path, "version", f.Version)
	}
	if len(f.Connections) > MaxConnections {
		return nil, errors.New("ConnectionsFileTooMany", "the connection file lists more connections than Emetos keeps", "path", s.path, "count", len(f.Connections))
	}
	seen := map[string]bool{}
	for _, c := range f.Connections {
		if err := c.Check(); err != nil {
			return nil, err
		}
		if c.ID == "" || seen[c.ID] {
			return nil, errors.New("ConnectionsFileInvalid", "a connection of the file has no ID, or the same ID as another", "path", s.path, "id", c.ID)
		}
		seen[c.ID] = true
	}
	return f.Connections, nil
}

// List returns the connections a workspace sees (those of the user and those of the folder), in the order of the file; the folder
// "" is a workspace with no folder, which sees the user's. The connections are copies.
func (s *Store) List(folder string) []Connection {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Connection
	for _, c := range s.conns {
		if c.VisibleIn(folder) {
			out = append(out, c.clone())
		}
	}
	return out
}

// All returns every connection of the file, whatever its scope.
func (s *Store) All() []Connection {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Connection, len(s.conns))
	for i, c := range s.conns {
		out[i] = c.clone()
	}
	return out
}

// Get returns the connection with the ID.
func (s *Store) Get(id string) (Connection, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.conns {
		if c.ID == id {
			return c.clone(), true
		}
	}
	return Connection{}, false
}

// Save adds a connection, or replaces the one with the same ID, and writes the file. A connection with no ID gets one. It returns
// the connection as it was kept. It fails with the tags of Connection.Check, with ConnectionsFileTooMany, with the error that
// made the file unusable when Load failed, and with StateFileWriteFailed.
func (s *Store) Save(c Connection) (Connection, error) {
	if err := c.Check(); err != nil {
		return Connection{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loadErr != nil {
		return Connection{}, s.loadErr
	}
	c = c.clone()
	c.Name = strings.TrimSpace(c.Name)
	next := slices.Clone(s.conns)
	if c.ID == "" {
		c.ID = NewID()
		next = append(next, c)
	} else if i := slices.IndexFunc(next, func(x Connection) bool { return x.ID == c.ID }); i >= 0 {
		next[i] = c
	} else {
		next = append(next, c)
	}
	if len(next) > MaxConnections {
		return Connection{}, errors.New("ConnectionsFileTooMany", "there are more connections than Emetos keeps", "count", len(next))
	}
	if err := s.write(next); err != nil {
		return Connection{}, err
	}
	s.conns = next
	return c.clone(), nil
}

// Delete removes the connection with the ID and writes the file; it returns the connection it removed. It fails with
// ConnectionNotFound, with the error that made the file unusable when Load failed, and with StateFileWriteFailed.
func (s *Store) Delete(id string) (Connection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loadErr != nil {
		return Connection{}, s.loadErr
	}
	i := slices.IndexFunc(s.conns, func(x Connection) bool { return x.ID == id })
	if i < 0 {
		return Connection{}, errors.New("ConnectionNotFound", "there is no connection with this ID", "id", id)
	}
	gone := s.conns[i]
	next := slices.Delete(slices.Clone(s.conns), i, i+1)
	if err := s.write(next); err != nil {
		return Connection{}, err
	}
	s.conns = next
	return gone.clone(), nil
}

// write writes the file with the connections.
func (s *Store) write(conns []Connection) error {
	if conns == nil {
		conns = []Connection{}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(fileFormat{Version: fileVersion, Connections: conns}); err != nil {
		return errors.NewErr("ConnectionsFileInvalid", err).SetMetadata("path", s.path)
	}
	return statefile.Write(s.path, buf.Bytes())
}
