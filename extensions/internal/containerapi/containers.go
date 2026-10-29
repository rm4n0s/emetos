package containerapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/errors"
)

// maxPortRange bounds the ports one libpod port range expands to.
const maxPortRange = 256

// wirePort is a port in either engine's list: Docker's IP/PrivatePort/PublicPort/Type, or libpod's
// host_ip/container_port/host_port/range/protocol.
type wirePort struct {
	IP          string `json:"IP"`
	PrivatePort uint16 `json:"PrivatePort"`
	PublicPort  uint16 `json:"PublicPort"`
	Type        string `json:"Type"`

	HostIP        string `json:"host_ip"`
	ContainerPort uint16 `json:"container_port"`
	HostPort      uint16 `json:"host_port"`
	Range         uint16 `json:"range"`
	Protocol      string `json:"protocol"`
}

// portsOf turns the wire ports into Ports, expanding libpod's ranges.
func portsOf(in []wirePort) []Port {
	var out []Port
	for _, p := range in {
		if p.ContainerPort == 0 && p.PrivatePort != 0 {
			out = append(out, Port{IP: p.IP, PrivatePort: p.PrivatePort, PublicPort: p.PublicPort, Type: p.Type})
			continue
		}
		n := int(p.Range)
		if n < 1 {
			n = 1
		}
		if n > maxPortRange {
			n = maxPortRange
		}
		for i := 0; i < n; i++ {
			port := Port{IP: p.HostIP, PrivatePort: p.ContainerPort + uint16(i), Type: p.Protocol}
			if p.HostPort != 0 {
				port.PublicPort = p.HostPort + uint16(i)
			}
			out = append(out, port)
		}
	}
	return out
}

// wireContainer is a row of either engine's container list.
type wireContainer struct {
	ID         string            `json:"Id"`
	Names      []string          `json:"Names"`
	Image      string            `json:"Image"`
	ImageID    string            `json:"ImageID"`
	Command    json.RawMessage   `json:"Command"`
	Created    flexTime          `json:"Created"`
	StartedAt  flexTime          `json:"StartedAt"`
	State      string            `json:"State"`
	Status     string            `json:"Status"`
	Ports      []wirePort        `json:"Ports"`
	Labels     map[string]string `json:"Labels"`
	Pod        string            `json:"Pod"`
	PodName    string            `json:"PodName"`
	Networks   []string          `json:"Networks"`
	NetworkSet struct {
		Networks map[string]json.RawMessage `json:"Networks"`
	} `json:"NetworkSettings"`
}

// container converts a wire row.
func (w wireContainer) container() Container {
	c := Container{
		ID: w.ID, Image: w.Image, ImageID: w.ImageID, Command: commandLine(w.Command),
		Created: w.Created.Time, StartedAt: w.StartedAt.Time, State: w.State, Status: w.Status,
		Ports: portsOf(w.Ports), Labels: w.Labels, Pod: w.Pod, PodName: w.PodName, Networks: w.Networks,
	}
	for _, n := range w.Names {
		c.Names = append(c.Names, trimSlash(n))
	}
	if len(c.Networks) == 0 {
		for name := range w.NetworkSet.Networks {
			c.Networks = append(c.Networks, name)
		}
		sort.Strings(c.Networks)
	}
	return c
}

// ListContainers lists the containers. The names have no leading slash and the ports are in one form for both engines.
func (c *Client) ListContainers(ctx context.Context, opts ListContainersOptions) ([]Container, error) {
	q := url.Values{}
	if opts.All {
		q.Set("all", "true")
	}
	opts.Filters.addTo(q)
	var rows []wireContainer
	if err := c.call(ctx, request{method: http.MethodGet, path: "/containers/json", query: q}, &rows); err != nil {
		return nil, err
	}
	out := make([]Container, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.container())
	}
	return out, nil
}

// wireInspect is an inspected container of either engine.
type wireInspect struct {
	ID        string   `json:"Id"`
	Name      string   `json:"Name"`
	Created   flexTime `json:"Created"`
	Image     string   `json:"Image"`
	ImageName string   `json:"ImageName"`
	Pod       string   `json:"Pod"`
	State     struct {
		Status     string   `json:"Status"`
		Running    bool     `json:"Running"`
		Paused     bool     `json:"Paused"`
		Restarting bool     `json:"Restarting"`
		OOMKilled  bool     `json:"OOMKilled"`
		Dead       bool     `json:"Dead"`
		Pid        int      `json:"Pid"`
		ExitCode   int      `json:"ExitCode"`
		Error      string   `json:"Error"`
		StartedAt  flexTime `json:"StartedAt"`
		FinishedAt flexTime `json:"FinishedAt"`
		Health     *struct {
			Status string `json:"Status"`
		} `json:"Health"`
	} `json:"State"`
	Config struct {
		Env        []string          `json:"Env"`
		Cmd        flexStrings       `json:"Cmd"`
		Entrypoint flexStrings       `json:"Entrypoint"`
		WorkingDir string            `json:"WorkingDir"`
		Hostname   string            `json:"Hostname"`
		User       string            `json:"User"`
		Labels     map[string]string `json:"Labels"`
		Tty        bool              `json:"Tty"`
		Image      string            `json:"Image"`
	} `json:"Config"`
	NetworkSettings struct {
		Ports    map[string][]PortBinding   `json:"Ports"`
		Networks map[string]json.RawMessage `json:"Networks"`
	} `json:"NetworkSettings"`
	Mounts []struct {
		Type        string `json:"Type"`
		Name        string `json:"Name"`
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
		RW          bool   `json:"RW"`
	} `json:"Mounts"`
}

// InspectContainer returns the details of a container (by id or name), with the engine's JSON in Raw.
func (c *Client) InspectContainer(ctx context.Context, id string) (*ContainerInspect, error) {
	var raw json.RawMessage
	if err := c.call(ctx, request{method: http.MethodGet, path: "/containers/" + url.PathEscape(id) + "/json"}, &raw); err != nil {
		return nil, err
	}
	var w wireInspect
	if err := json.Unmarshal(raw, &w); err != nil {
		return nil, errors.NewErr("ContainerAPIDecodeFailed", err).SetMetadata("method", "GET", "path", "/containers/"+id+"/json")
	}
	return inspectOf(&w, raw), nil
}

// ParseInspect reads the JSON an engine answers to the inspect of a container (Docker's or Podman's) into a ContainerInspect with the
// JSON in Raw. It fails with ContainerAPIInspectInvalid.
func ParseInspect(raw []byte) (*ContainerInspect, error) {
	var w wireInspect
	if err := json.Unmarshal(raw, &w); err != nil {
		return nil, errors.NewErr("ContainerAPIInspectInvalid", err)
	}
	return inspectOf(&w, raw), nil
}

// inspectOf makes the ContainerInspect of a decoded answer.
func inspectOf(w *wireInspect, raw json.RawMessage) *ContainerInspect {
	out := &ContainerInspect{
		ID: w.ID, Name: trimSlash(w.Name), Image: w.Image, ImageName: w.ImageName, Created: w.Created.Time, Pod: w.Pod,
		State: ContainerState{
			Status: w.State.Status, Running: w.State.Running, Paused: w.State.Paused, Restarting: w.State.Restarting,
			OOMKilled: w.State.OOMKilled, Dead: w.State.Dead, Pid: w.State.Pid, ExitCode: w.State.ExitCode, Error: w.State.Error,
			StartedAt: w.State.StartedAt.Time, FinishedAt: w.State.FinishedAt.Time,
		},
		Env: w.Config.Env, Cmd: w.Config.Cmd, Entrypoint: w.Config.Entrypoint, WorkingDir: w.Config.WorkingDir,
		Hostname: w.Config.Hostname, User: w.Config.User, Labels: w.Config.Labels, Tty: w.Config.Tty,
		Ports: w.NetworkSettings.Ports, Raw: raw,
	}
	if out.ImageName == "" {
		out.ImageName = w.Config.Image
	}
	if w.State.Health != nil {
		out.State.Health = w.State.Health.Status
	}
	for name := range w.NetworkSettings.Networks {
		out.Networks = append(out.Networks, name)
	}
	sort.Strings(out.Networks)
	for _, m := range w.Mounts {
		out.Mounts = append(out.Mounts, Mount{Type: m.Type, Name: m.Name, Source: m.Source, Destination: m.Destination, RW: m.RW})
	}
	return out
}

// CreateContainer makes a container from spec, without starting it. The image has to be there; create does not pull it.
// A spec with a Pod is for Podman only (ContainerAPINotSupported for Docker).
func (c *Client) CreateContainer(ctx context.Context, spec ContainerSpec) (CreateResult, error) {
	if spec.Pod != "" && c.kind != KindPodman {
		return CreateResult{}, errors.New("ContainerAPINotSupported", "only Podman has pods", "operation", "create a container in a pod", "kind", string(c.kind))
	}
	var body any
	q := url.Values{}
	path := "/containers/create"
	if c.kind == KindPodman {
		body = libpodSpec(spec)
	} else {
		body = dockerSpec(spec)
		if spec.Name != "" {
			q.Set("name", spec.Name)
		}
	}
	var out struct {
		ID       string   `json:"Id"`
		Warnings []string `json:"Warnings"`
	}
	if err := c.call(ctx, request{method: http.MethodPost, path: path, query: q, body: body}, &out); err != nil {
		return CreateResult{}, err
	}
	return CreateResult{ID: out.ID, Warnings: out.Warnings}, nil
}

// dockerSpec is the body of Docker's container create.
func dockerSpec(s ContainerSpec) map[string]any {
	host := map[string]any{}
	body := map[string]any{"Image": s.Image, "HostConfig": host}
	if len(s.Cmd) > 0 {
		body["Cmd"] = s.Cmd
	}
	if len(s.Entrypoint) > 0 {
		body["Entrypoint"] = s.Entrypoint
	}
	if len(s.Env) > 0 {
		body["Env"] = s.Env
	}
	if len(s.Labels) > 0 {
		body["Labels"] = s.Labels
	}
	if s.WorkingDir != "" {
		body["WorkingDir"] = s.WorkingDir
	}
	if s.User != "" {
		body["User"] = s.User
	}
	if s.Tty {
		body["Tty"] = true
		body["OpenStdin"] = true
	}
	if len(s.Ports) > 0 {
		exposed := map[string]struct{}{}
		bindings := map[string][]map[string]string{}
		for _, p := range s.Ports {
			proto := p.Protocol
			if proto == "" {
				proto = "tcp"
			}
			key := strconv.Itoa(int(p.ContainerPort)) + "/" + proto
			exposed[key] = struct{}{}
			b := map[string]string{"HostIp": p.HostIP, "HostPort": ""}
			if p.HostPort != 0 {
				b["HostPort"] = strconv.Itoa(int(p.HostPort))
			}
			bindings[key] = append(bindings[key], b)
		}
		body["ExposedPorts"] = exposed
		host["PortBindings"] = bindings
	}
	if len(s.Binds) > 0 {
		host["Binds"] = s.Binds
	}
	if s.AutoRemove {
		host["AutoRemove"] = true
	}
	if s.Network != "" {
		host["NetworkMode"] = s.Network
	}
	return body
}

// libpodSpec is the body of libpod's container create (its SpecGenerator).
func libpodSpec(s ContainerSpec) map[string]any {
	body := map[string]any{"image": s.Image}
	if s.Name != "" {
		body["name"] = s.Name
	}
	if len(s.Cmd) > 0 {
		body["command"] = s.Cmd
	}
	if len(s.Entrypoint) > 0 {
		body["entrypoint"] = s.Entrypoint
	}
	if len(s.Env) > 0 {
		env := map[string]string{}
		for _, kv := range s.Env {
			k, v, _ := strings.Cut(kv, "=")
			env[k] = v
		}
		body["env"] = env
	}
	if len(s.Labels) > 0 {
		body["labels"] = s.Labels
	}
	if s.WorkingDir != "" {
		body["work_dir"] = s.WorkingDir
	}
	if s.User != "" {
		body["user"] = s.User
	}
	if s.Tty {
		body["terminal"] = true
		body["stdin"] = true
	}
	if len(s.Ports) > 0 {
		body["portmappings"] = portMappingsBody(s.Ports)
	}
	var mounts []map[string]any
	var volumes []map[string]any
	for _, b := range s.Binds {
		parts := strings.Split(b, ":")
		if len(parts) < 2 {
			continue
		}
		var opts []string
		if len(parts) > 2 && parts[2] != "" {
			opts = strings.Split(parts[2], ",")
		}
		if strings.HasPrefix(parts[0], "/") || strings.HasPrefix(parts[0], ".") {
			m := map[string]any{"type": "bind", "source": parts[0], "destination": parts[1]}
			if len(opts) > 0 {
				m["options"] = opts
			}
			mounts = append(mounts, m)
			continue
		}
		v := map[string]any{"Name": parts[0], "Dest": parts[1]}
		if len(opts) > 0 {
			v["Options"] = opts
		}
		volumes = append(volumes, v)
	}
	if len(mounts) > 0 {
		body["mounts"] = mounts
	}
	if len(volumes) > 0 {
		body["volumes"] = volumes
	}
	if s.AutoRemove {
		body["remove"] = true
	}
	if s.Network != "" {
		body["networks"] = map[string]any{s.Network: map[string]any{}}
	}
	if s.Pod != "" {
		body["pod"] = s.Pod
	}
	return body
}

// StartContainer starts a container. Starting one that runs already is not an error.
func (c *Client) StartContainer(ctx context.Context, id string) error {
	return c.call(ctx, request{method: http.MethodPost, path: "/containers/" + url.PathEscape(id) + "/start"}, nil)
}

// timeoutQuery returns the query that carries a stop timeout under the name the engine wants; none when timeout is not
// positive.
func timeoutQuery(name string, timeout time.Duration) url.Values {
	q := url.Values{}
	if timeout > 0 {
		secs := int(timeout / time.Second)
		if secs < 1 {
			secs = 1
		}
		q.Set(name, strconv.Itoa(secs))
	}
	return q
}

// StopContainer stops a container: SIGTERM, then SIGKILL after timeout (the engine's default when timeout is not positive,
// and at least one second otherwise). Stopping one that is stopped already is not an error.
func (c *Client) StopContainer(ctx context.Context, id string, timeout time.Duration) error {
	name := "t"
	if c.kind == KindPodman {
		name = "timeout"
	}
	return c.call(ctx, request{method: http.MethodPost, path: "/containers/" + url.PathEscape(id) + "/stop", query: timeoutQuery(name, timeout)}, nil)
}

// RestartContainer stops and starts a container; timeout is as for StopContainer.
func (c *Client) RestartContainer(ctx context.Context, id string, timeout time.Duration) error {
	return c.call(ctx, request{method: http.MethodPost, path: "/containers/" + url.PathEscape(id) + "/restart", query: timeoutQuery("t", timeout)}, nil)
}

// PauseContainer freezes the processes of a container.
func (c *Client) PauseContainer(ctx context.Context, id string) error {
	return c.call(ctx, request{method: http.MethodPost, path: "/containers/" + url.PathEscape(id) + "/pause"}, nil)
}

// UnpauseContainer thaws a paused container.
func (c *Client) UnpauseContainer(ctx context.Context, id string) error {
	return c.call(ctx, request{method: http.MethodPost, path: "/containers/" + url.PathEscape(id) + "/unpause"}, nil)
}

// KillContainer sends a signal ("SIGKILL" when signal is empty) to a container's main process.
func (c *Client) KillContainer(ctx context.Context, id, signal string) error {
	q := url.Values{}
	if signal != "" {
		q.Set("signal", signal)
	}
	return c.call(ctx, request{method: http.MethodPost, path: "/containers/" + url.PathEscape(id) + "/kill", query: q}, nil)
}

// RenameContainer gives a container a new name.
func (c *Client) RenameContainer(ctx context.Context, id, name string) error {
	return c.call(ctx, request{method: http.MethodPost, path: "/containers/" + url.PathEscape(id) + "/rename", query: url.Values{"name": {name}}}, nil)
}

// RemoveContainer removes a container. A running one is removed only with opts.Force.
func (c *Client) RemoveContainer(ctx context.Context, id string, opts RemoveOptions) error {
	q := url.Values{}
	if opts.Force {
		q.Set("force", "true")
	}
	if opts.Volumes {
		q.Set("v", "true")
	}
	return c.call(ctx, request{method: http.MethodDelete, path: "/containers/" + url.PathEscape(id), query: q}, nil)
}

// PruneContainers removes the stopped containers that match filters (all of them without filters).
func (c *Client) PruneContainers(ctx context.Context, filters Filters) (PruneResult, error) {
	q := url.Values{}
	filters.addTo(q)
	var raw json.RawMessage
	if err := c.call(ctx, request{method: http.MethodPost, path: "/containers/prune", query: q}, &raw); err != nil {
		return PruneResult{}, err
	}
	return decodePrune(raw, "ContainersDeleted")
}

// portMappingsBody writes published ports as libpod's SpecGenerator (and the pod's) takes them: "portmappings".
func portMappingsBody(ports []PortMapping) []map[string]any {
	var maps []map[string]any
	for _, p := range ports {
		proto := p.Protocol
		if proto == "" {
			proto = "tcp"
		}
		m := map[string]any{"container_port": p.ContainerPort, "protocol": proto}
		if p.HostIP != "" {
			m["host_ip"] = p.HostIP
		}
		if p.HostPort != 0 {
			m["host_port"] = p.HostPort
		}
		maps = append(maps, m)
	}
	return maps
}
