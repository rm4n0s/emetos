package containerapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rm4n0s/errors"
)

// The default sockets (SPEC §10.20 "Which engine").
const (
	// DefaultDockerSocket is where the Docker daemon listens.
	DefaultDockerSocket = "/var/run/docker.sock"
	// RootfulPodmanSocket is the socket of Podman running as root.
	RootfulPodmanSocket = "/run/podman/podman.sock"
	// maxContextFile bounds the Docker config and context files that are read.
	maxContextFile = 1 << 20
)

// FindOptions say where to look for the engine.
type FindOptions struct {
	// Kind is the engine to find.
	Kind Kind
	// Host is the setting (containers.host, podman.host); empty is no setting.
	Host string
	// Context is the Docker context the user picked in the view (Docker only); it wins over every other source, because
	// the user chose it in the UI. Empty is none.
	Context string
	// Getenv reads the environment (DOCKER_HOST, DOCKER_CONTEXT, DOCKER_CONFIG, DOCKER_TLS_VERIFY, DOCKER_CERT_PATH,
	// CONTAINER_HOST, HOME, XDG_RUNTIME_DIR, PATH); nil is os.Getenv. A test passes a map, so that it never reads the
	// user's environment, home or sockets.
	Getenv func(string) string
	// Client tunes the Clients made.
	Client Options
	// ProbeTimeout bounds the question to each candidate; 0 is three seconds.
	ProbeTimeout time.Duration
	// DockerSocket and RootfulPodmanSocket replace the default sockets (DefaultDockerSocket, RootfulPodmanSocket); a test points
	// them at a folder of its own, so that it never asks the engines of the computer it runs on.
	DockerSocket, RootfulPodmanSocket string
	// NoService stops Find from starting a Podman service of its own when no socket answers.
	NoService bool
	// Service says how that service is started (its Getenv defaults to this Getenv).
	Service ServiceOptions
}

// Attempt is a place Find looked at.
type Attempt struct {
	// Source says why it was tried ("the setting containers.host", "DOCKER_HOST", ...), Endpoint is where, Err why it did
	// not answer ("" for the one that did).
	Source, Endpoint, Err string
}

// Found is the engine Find connected to.
type Found struct {
	// Client talks to the engine.
	Client *Client
	// Info is what the engine says about itself.
	Info ServerInfo
	// Source says why this engine was chosen.
	Source string
	// Service is the Podman service that Find started, nil when an engine answered by itself.
	Service *Service
	// Attempts are all the places looked at, the one that answered last.
	Attempts []Attempt
}

// Close stops the Podman service that Find started, if any, and closes the connections.
func (f *Found) Close() {
	f.Client.Close()
	if f.Service != nil {
		f.Service.Stop()
	}
}

// candidate is a place to ask.
type candidate struct {
	source string
	ep     Endpoint
	err    string // why the candidate could not even be made
}

// Find looks for the engine of opts.Kind and connects to it.
//
// Docker: opts.Context, else opts.Host (containers.host), else DOCKER_HOST, else the current Docker context
// (DOCKER_CONTEXT, else "currentContext" of $DOCKER_CONFIG/config.json or ~/.docker/config.json, with its host and
// certificates from contexts/), else unix:///var/run/docker.sock. The first source that is set is the only one asked: an
// engine the user named is never replaced by another one.
//
// Podman: opts.Host (podman.host), else CONTAINER_HOST, then (the user named nothing) the user's socket
// $XDG_RUNTIME_DIR/podman/podman.sock, then /run/podman/podman.sock, and when none answers a "podman system service" of
// Emetos's own (StartService), unless NoService.
//
// When nothing answers, the error is ContainerAPIEngineNotFound; its message lists what was tried and why each failed, and
// its metadata "attempts" has the same as a list.
func Find(ctx context.Context, opts FindOptions) (*Found, error) {
	getenv := opts.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	var cands []candidate
	var err error
	if opts.Kind == KindPodman {
		cands, err = podmanCandidates(opts, getenv)
	} else {
		cands, err = dockerCandidates(opts, getenv)
	}
	if err != nil {
		return nil, err
	}
	probe := opts.ProbeTimeout
	if probe <= 0 {
		probe = 3 * time.Second
	}
	var attempts []Attempt
	for _, cand := range cands {
		if cand.err != "" {
			attempts = append(attempts, Attempt{Source: cand.source, Err: cand.err})
			continue
		}
		cl := New(opts.Kind, cand.ep, opts.Client)
		pctx, cancel := context.WithTimeout(ctx, probe)
		info, perr := cl.Negotiate(pctx)
		cancel()
		if perr == nil {
			attempts = append(attempts, Attempt{Source: cand.source, Endpoint: cand.ep.String()})
			return &Found{Client: cl, Info: info, Source: cand.source, Attempts: attempts}, nil
		}
		cl.Close()
		attempts = append(attempts, Attempt{Source: cand.source, Endpoint: cand.ep.String(), Err: perr.Error()})
		if ctx.Err() != nil {
			break
		}
	}
	if opts.Kind == KindPodman && !opts.NoService && opts.Host == "" && getenv("CONTAINER_HOST") == "" && ctx.Err() == nil {
		svcOpts := opts.Service
		if svcOpts.Getenv == nil {
			svcOpts.Getenv = getenv
		}
		svc, serr := StartService(ctx, svcOpts)
		if serr == nil {
			ep := UnixEndpoint(svc.Socket)
			cl := New(KindPodman, ep, opts.Client)
			pctx, cancel := context.WithTimeout(ctx, probe)
			info, perr := cl.Negotiate(pctx)
			cancel()
			if perr == nil {
				attempts = append(attempts, Attempt{Source: "a podman system service of Emetos's own", Endpoint: ep.String()})
				return &Found{Client: cl, Info: info, Source: "a podman system service of Emetos's own", Service: svc, Attempts: attempts}, nil
			}
			cl.Close()
			svc.Stop()
			attempts = append(attempts, Attempt{Source: "a podman system service of Emetos's own", Endpoint: ep.String(), Err: perr.Error()})
		} else {
			attempts = append(attempts, Attempt{Source: "a podman system service of Emetos's own", Err: serr.Error()})
		}
	}
	lines := make([]string, 0, len(attempts))
	list := make([]string, 0, len(attempts))
	for _, a := range attempts {
		line := a.Source
		if a.Endpoint != "" {
			line += " (" + a.Endpoint + ")"
		}
		line += ": " + a.Err
		lines = append(lines, line)
		list = append(list, line)
	}
	return nil, errors.New("ContainerAPIEngineNotFound", "no "+string(opts.Kind)+" engine answers: "+strings.Join(lines, "; "), "kind", string(opts.Kind), "attempts", list)
}

// dockerCandidates lists the one place Docker is asked.
func dockerCandidates(opts FindOptions, getenv func(string) string) ([]candidate, error) {
	dockerSocket := opts.DockerSocket
	if dockerSocket == "" {
		dockerSocket = DefaultDockerSocket
	}
	tlsOpts := TLSOptions{Verify: getenv("DOCKER_TLS_VERIFY") != "", CertDir: getenv("DOCKER_CERT_PATH")}
	if tlsOpts.Verify && tlsOpts.CertDir == "" {
		if home := getenv("HOME"); home != "" {
			tlsOpts.CertDir = filepath.Join(home, ".docker")
		}
	}
	hostCandidate := func(source, host string) ([]candidate, error) {
		ep, err := ParseHost(host, tlsOpts)
		if err != nil {
			return nil, err
		}
		return []candidate{{source: source, ep: ep}}, nil
	}
	switch {
	case opts.Context != "":
		return contextCandidate(opts.Context, "the Docker context "+opts.Context, getenv, dockerSocket)
	case opts.Host != "":
		return hostCandidate("the setting containers.host", opts.Host)
	case getenv("DOCKER_HOST") != "":
		return hostCandidate("DOCKER_HOST", getenv("DOCKER_HOST"))
	}
	name := getenv("DOCKER_CONTEXT")
	if name == "" {
		cfg, err := readDockerConfig(getenv)
		if err != nil {
			return nil, err
		}
		name = cfg.CurrentContext
	}
	if name != "" && name != "default" {
		return contextCandidate(name, "the current Docker context "+name, getenv, dockerSocket)
	}
	return []candidate{{source: "the default socket", ep: UnixEndpoint(dockerSocket)}}, nil
}

// podmanCandidates lists the places Podman is asked.
func podmanCandidates(opts FindOptions, getenv func(string) string) ([]candidate, error) {
	tlsOpts := TLSOptions{Verify: getenv("DOCKER_TLS_VERIFY") != "", CertDir: getenv("DOCKER_CERT_PATH")}
	switch {
	case opts.Host != "":
		ep, err := ParseHost(opts.Host, tlsOpts)
		if err != nil {
			return nil, err
		}
		return []candidate{{source: "the setting podman.host", ep: ep}}, nil
	case getenv("CONTAINER_HOST") != "":
		ep, err := ParseHost(getenv("CONTAINER_HOST"), tlsOpts)
		if err != nil {
			return nil, err
		}
		return []candidate{{source: "CONTAINER_HOST", ep: ep}}, nil
	}
	var cands []candidate
	if rt := getenv("XDG_RUNTIME_DIR"); rt != "" {
		cands = append(cands, candidate{source: "the user's socket", ep: UnixEndpoint(filepath.Join(rt, "podman", "podman.sock"))})
	}
	rootful := opts.RootfulPodmanSocket
	if rootful == "" {
		rootful = RootfulPodmanSocket
	}
	return append(cands, candidate{source: "the rootful socket", ep: UnixEndpoint(rootful)}), nil
}

// dockerConfig is the part of ~/.docker/config.json that matters here.
type dockerConfig struct {
	CurrentContext string `json:"currentContext"`
}

// configDir returns the folder of the Docker CLI's configuration: $DOCKER_CONFIG, else ~/.docker; "" without a home.
func configDir(getenv func(string) string) string {
	if d := getenv("DOCKER_CONFIG"); d != "" {
		return d
	}
	if home := getenv("HOME"); home != "" {
		return filepath.Join(home, ".docker")
	}
	return ""
}

// readSmall reads a file of at most maxContextFile bytes; ok is false when it does not exist.
func readSmall(path string) (data []byte, ok bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, errors.NewErr("ContainerAPIContextInvalid", err).SetMetadata("file", path)
	}
	defer func() { _ = f.Close() }() // opened for reading only
	data, err = io.ReadAll(io.LimitReader(f, maxContextFile+1))
	if err != nil {
		return nil, false, errors.NewErr("ContainerAPIContextInvalid", err).SetMetadata("file", path)
	}
	if len(data) > maxContextFile {
		return nil, false, errors.New("ContainerAPIContextInvalid", path+" is longer than 1 MiB", "file", path)
	}
	return data, true, nil
}

// readDockerConfig reads config.json of the Docker CLI; a missing file is an empty configuration.
func readDockerConfig(getenv func(string) string) (dockerConfig, error) {
	dir := configDir(getenv)
	if dir == "" {
		return dockerConfig{}, nil
	}
	data, ok, err := readSmall(filepath.Join(dir, "config.json"))
	if err != nil || !ok {
		return dockerConfig{}, err
	}
	var cfg dockerConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return dockerConfig{}, errors.NewErr("ContainerAPIContextInvalid", err).SetMetadata("file", filepath.Join(dir, "config.json"))
	}
	return cfg, nil
}

// DockerContext is a context of the Docker CLI.
type DockerContext struct {
	// Name is the name of the context ("default" for the one Docker always has).
	Name string
	// Description is the description it was made with.
	Description string
	// Host is the engine's host (unix:///..., tcp://...).
	Host string
	// SkipTLSVerify is the context's setting of that name.
	SkipTLSVerify bool
	// TLSDir holds the context's certificates (ca.pem, cert.pem, key.pem) when it has any.
	TLSDir string
	// Current marks the context the Docker CLI would use now.
	Current bool
}

// contextDir returns the folder name of a context in the store: the hex SHA-256 of its name.
func contextDir(name string) string {
	sum := sha256.Sum256([]byte(name))
	return hex.EncodeToString(sum[:])
}

// metaFile is a context's meta.json.
type metaFile struct {
	Name      string
	Metadata  struct{ Description string }
	Endpoints map[string]struct {
		Host          string
		SkipTLSVerify bool
	}
}

// contextCandidate makes the candidate of a named Docker context.
func contextCandidate(name, source string, getenv func(string) string, dockerSocket string) ([]candidate, error) {
	if name == "default" {
		host := getenv("DOCKER_HOST")
		if host == "" {
			return []candidate{{source: source, ep: UnixEndpoint(dockerSocket)}}, nil
		}
		ep, err := ParseHost(host, TLSOptions{Verify: getenv("DOCKER_TLS_VERIFY") != "", CertDir: getenv("DOCKER_CERT_PATH")})
		if err != nil {
			return nil, err
		}
		return []candidate{{source: source, ep: ep}}, nil
	}
	dir := configDir(getenv)
	if dir == "" {
		return nil, errors.New("ContainerAPIContextNotFound", "the Docker context "+name+" cannot be looked up without a home folder or DOCKER_CONFIG", "context", name)
	}
	ctxs, err := readContext(dir, name)
	if err != nil {
		return nil, err
	}
	if ctxs == nil {
		return nil, errors.New("ContainerAPIContextNotFound", "there is no Docker context named "+name, "context", name, "dir", dir)
	}
	tlsOpts := TLSOptions{SkipVerify: ctxs.SkipTLSVerify}
	if ctxs.TLSDir != "" {
		tlsOpts.Verify, tlsOpts.CertDir = true, ctxs.TLSDir
	}
	ep, err := ParseHost(ctxs.Host, tlsOpts)
	if err != nil {
		return nil, err
	}
	return []candidate{{source: source, ep: ep}}, nil
}

// readContext reads the context name from the store in dir; nil when there is none.
func readContext(dir, name string) (*DockerContext, error) {
	sub := contextDir(name)
	data, ok, err := readSmall(filepath.Join(dir, "contexts", "meta", sub, "meta.json"))
	if err != nil || !ok {
		return nil, err
	}
	var m metaFile
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, errors.NewErr("ContainerAPIContextInvalid", err).SetMetadata("context", name)
	}
	ep, found := m.Endpoints["docker"]
	if !found || ep.Host == "" {
		return nil, errors.New("ContainerAPIContextInvalid", "the Docker context "+name+" has no docker endpoint", "context", name)
	}
	dc := &DockerContext{Name: m.Name, Description: m.Metadata.Description, Host: ep.Host, SkipTLSVerify: ep.SkipTLSVerify}
	if dc.Name == "" {
		dc.Name = name
	}
	tlsDir := filepath.Join(dir, "contexts", "tls", sub, "docker")
	if info, err := os.Stat(tlsDir); err == nil && info.IsDir() {
		dc.TLSDir = tlsDir
	}
	return dc, nil
}

// DockerContexts lists the contexts of the Docker CLI (the picker of the Containers view): "default" first, then the
// others by name, and marks the current one. Without a home folder or DOCKER_CONFIG only "default" is listed. getenv nil is
// os.Getenv.
func DockerContexts(getenv func(string) string) ([]DockerContext, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	defHost := getenv("DOCKER_HOST")
	if defHost == "" {
		defHost = "unix://" + DefaultDockerSocket
	}
	out := []DockerContext{{Name: "default", Description: "Current DOCKER_HOST based configuration", Host: defHost}}
	current := getenv("DOCKER_CONTEXT")
	dir := configDir(getenv)
	if dir != "" {
		if current == "" {
			cfg, err := readDockerConfig(getenv)
			if err != nil {
				return nil, err
			}
			current = cfg.CurrentContext
		}
		entries, err := os.ReadDir(filepath.Join(dir, "contexts", "meta"))
		if err != nil && !os.IsNotExist(err) {
			return nil, errors.NewErr("ContainerAPIContextInvalid", err).SetMetadata("dir", filepath.Join(dir, "contexts", "meta"))
		}
		var others []DockerContext
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			// a context that cannot be read or parsed is left out of the picker; choosing it by name reports why
			data, ok, err := readSmall(filepath.Join(dir, "contexts", "meta", e.Name(), "meta.json"))
			if err != nil || !ok {
				continue
			}
			var m metaFile
			if json.Unmarshal(data, &m) != nil || m.Name == "" {
				continue
			}
			dc, err := readContext(dir, m.Name)
			if err != nil || dc == nil {
				continue
			}
			others = append(others, *dc)
		}
		sort.Slice(others, func(i, j int) bool { return others[i].Name < others[j].Name })
		out = append(out, others...)
	}
	if current == "" {
		current = "default"
	}
	for i := range out {
		out[i].Current = out[i].Name == current
	}
	return out, nil
}
