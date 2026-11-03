package variables

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/internal/statefile"
)

// GlobalsFileName is the file of the globals in $XDG_STATE_HOME/emetos.
const GlobalsFileName = "httpclient-globals.json"

const (
	globalsVersion  = 1
	maxGlobalsBytes = 1 << 20
)

// Entry is a global variable. The value of a secret is in the keyring.
type Entry struct {
	Name    string
	Value   string
	Enabled bool
	Secret  bool
}

type entryJSON struct {
	Name    string `json:"name"`
	Value   string `json:"value,omitempty"`
	Enabled bool   `json:"enabled"`
	Secret  bool   `json:"secret,omitempty"`
}

type globalsFile struct {
	Version   int         `json:"version"`
	Variables []entryJSON `json:"variables"`
}

// GlobalsPath is $XDG_STATE_HOME/emetos/httpclient-globals.json. The error has the tag StateFileDirUnknown.
func GlobalsPath() (string, error) {
	dir, err := statefile.DirFromEnv()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, GlobalsFileName), nil
}

// Globals are the variables shared between projects (SPEC §10.33): they are in the state of the user, not in any project. The
// file has mode 0600 and no secret value.
type Globals struct {
	path    string
	secrets Secrets

	mu    sync.Mutex
	known map[string]bool // the secrets of the last Load or Save
	// encode writes the file; json.MarshalIndent, replaced by a test to make it fail.
	encode func(v any) ([]byte, error)
}

// NewGlobals makes the globals kept in the file at path, with the secrets in secrets (nil: no keyring).
func NewGlobals(path string, secrets Secrets) *Globals {
	return &Globals{path: path, secrets: secrets, known: map[string]bool{}, encode: func(v any) ([]byte, error) { return json.MarshalIndent(v, "", "  ") }}
}

// Load reads the globals. The value of a secret comes from the keyring; missing lists the names of secrets it has none for. A
// file that does not exist is no globals. The errors have the tags of statefile.Read, GlobalsFileInvalid and GlobalsFileVersion,
// and are those of the keyring.
func (g *Globals) Load(ctx context.Context) (entries []Entry, missing []string, err error) {
	data, err := statefile.Read(g.path, maxGlobalsBytes, statefile.Writable)
	if err != nil {
		if statefile.Missing(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	var f globalsFile
	if jerr := json.Unmarshal(data, &f); jerr != nil {
		return nil, nil, errors.NewErr("GlobalsFileInvalid", jerr).SetMetadata("path", g.path)
	}
	if f.Version != globalsVersion {
		return nil, nil, errors.New("GlobalsFileVersion", "the globals file is of another version", "version", f.Version, "supported", globalsVersion)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.known = map[string]bool{}
	for _, e := range f.Variables {
		entry := Entry(e)
		if e.Secret {
			g.known[e.Name] = true
			entry.Value = ""
			if g.secrets != nil {
				secret, ok, serr := g.secrets.Lookup(ctx, SecretKey("", GlobalOwner, e.Name))
				if serr != nil {
					return nil, nil, serr
				}
				if ok {
					entry.Value = secret
				} else {
					missing = append(missing, e.Name)
				}
			} else {
				missing = append(missing, e.Name)
			}
		}
		entries = append(entries, entry)
	}
	return entries, missing, nil
}

// Save writes the globals. The value of a secret goes to the keyring (and the secrets that are no longer globals are removed
// from it); the file has none. It fails with HttpSecretsUnavailable for a secret when there is no keyring, and with the tag
// StateFileWriteFailed.
func (g *Globals) Save(ctx context.Context, entries []Entry) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	f := globalsFile{Version: globalsVersion, Variables: []entryJSON{}}
	now := map[string]bool{}
	for _, e := range entries {
		j := entryJSON(e)
		if e.Secret {
			if g.secrets == nil {
				return errors.New("HttpSecretsUnavailable", "there is no keyring for the secret", "name", e.Name)
			}
			if err := g.secrets.Store(ctx, SecretKey("", GlobalOwner, e.Name), secretLabel(GlobalOwner, e.Name), e.Value); err != nil {
				return err
			}
			j.Value = ""
			now[e.Name] = true
		}
		f.Variables = append(f.Variables, j)
	}
	data, err := g.encode(f)
	if err != nil {
		return errors.NewErr("GlobalsEncodeFailed", err)
	}
	if err := statefile.Write(g.path, append(data, '\n')); err != nil {
		return err
	}
	if g.secrets != nil {
		for name := range g.known {
			if !now[name] {
				if err := g.secrets.Delete(ctx, SecretKey("", GlobalOwner, name)); err != nil {
					return err
				}
			}
		}
	}
	g.known = now
	return nil
}
