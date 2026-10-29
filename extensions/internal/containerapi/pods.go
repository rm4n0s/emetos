package containerapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"github.com/rm4n0s/errors"
)

// wirePod is a pod as libpod lists and inspects it (the list says Status, the inspect State).
type wirePod struct {
	ID         string            `json:"Id"`
	Name       string            `json:"Name"`
	Status     string            `json:"Status"`
	State      string            `json:"State"`
	Created    flexTime          `json:"Created"`
	InfraID    string            `json:"InfraId"`
	InfraCtrID string            `json:"InfraContainerID"`
	Labels     map[string]string `json:"Labels"`
	Containers []struct {
		ID     string      `json:"Id"`
		Names  flexStrings `json:"Names"`
		Name   string      `json:"Name"`
		Status string      `json:"Status"`
		State  string      `json:"State"`
	} `json:"Containers"`
}

// pod converts a wire pod; raw is its JSON.
func (w wirePod) pod(raw json.RawMessage) Pod {
	p := Pod{ID: w.ID, Name: w.Name, Status: w.Status, Created: w.Created.Time, InfraID: w.InfraID, Labels: w.Labels, Raw: raw}
	if p.Status == "" {
		p.Status = w.State
	}
	if p.InfraID == "" {
		p.InfraID = w.InfraCtrID
	}
	for _, ct := range w.Containers {
		pc := PodContainer{ID: ct.ID, Name: ct.Name, Status: ct.Status}
		if pc.Name == "" && len(ct.Names) > 0 {
			pc.Name = ct.Names[0]
		}
		if pc.Status == "" {
			pc.Status = ct.State
		}
		p.Containers = append(p.Containers, pc)
	}
	return p
}

// requirePodman fails for a Docker client: pods are Podman's (SPEC §10.20 "Podman only").
func (c *Client) requirePodman(operation string) error {
	if c.kind != KindPodman {
		return errors.New("ContainerAPINotSupported", "only Podman has pods", "operation", operation, "kind", string(c.kind))
	}
	return nil
}

// ListPods lists the pods that match filters (Podman only).
func (c *Client) ListPods(ctx context.Context, filters Filters) ([]Pod, error) {
	if err := c.requirePodman("list pods"); err != nil {
		return nil, err
	}
	q := url.Values{}
	filters.addTo(q)
	var rows []json.RawMessage
	if err := c.call(ctx, request{method: http.MethodGet, path: "/pods/json", query: q}, &rows); err != nil {
		return nil, err
	}
	out := make([]Pod, 0, len(rows))
	for _, raw := range rows {
		var w wirePod
		if err := json.Unmarshal(raw, &w); err != nil {
			return nil, errors.NewErr("ContainerAPIDecodeFailed", err).SetMetadata("method", "GET", "path", "/pods/json")
		}
		out = append(out, w.pod(raw))
	}
	return out, nil
}

// InspectPod returns one pod (by id or name), with the engine's JSON in Raw (Podman only).
func (c *Client) InspectPod(ctx context.Context, id string) (*Pod, error) {
	if err := c.requirePodman("inspect a pod"); err != nil {
		return nil, err
	}
	path := "/pods/" + url.PathEscape(id) + "/json"
	var raw json.RawMessage
	if err := c.call(ctx, request{method: http.MethodGet, path: path}, &raw); err != nil {
		return nil, err
	}
	var w wirePod
	if err := json.Unmarshal(raw, &w); err != nil {
		return nil, errors.NewErr("ContainerAPIDecodeFailed", err).SetMetadata("method", "GET", "path", path)
	}
	p := w.pod(raw)
	return &p, nil
}

// CreatePod makes a pod and returns its id (Podman only).
func (c *Client) CreatePod(ctx context.Context, spec PodSpec) (string, error) {
	if err := c.requirePodman("create a pod"); err != nil {
		return "", err
	}
	body := map[string]any{}
	if spec.Name != "" {
		body["name"] = spec.Name
	}
	if len(spec.Labels) > 0 {
		body["labels"] = spec.Labels
	}
	if len(spec.Ports) > 0 {
		body["portmappings"] = portMappingsBody(spec.Ports)
	}
	var out struct {
		ID string `json:"Id"`
	}
	if err := c.call(ctx, request{method: http.MethodPost, path: "/pods/create", body: body}, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

// PodAction is something a pod can be told to do.
type PodAction string

// The actions of a pod.
const (
	PodStart   PodAction = "start"
	PodRestart PodAction = "restart"
	PodKill    PodAction = "kill"
	PodPause   PodAction = "pause"
	PodUnpause PodAction = "unpause"
)

// PodDo applies an action to a pod and its containers (Podman only). Stopping a pod that is stopped is not an error.
func (c *Client) PodDo(ctx context.Context, id string, action PodAction) error {
	if err := c.requirePodman("act on a pod"); err != nil {
		return err
	}
	return c.call(ctx, request{method: http.MethodPost, path: "/pods/" + url.PathEscape(id) + "/" + string(action)}, nil)
}

// StopPod stops a pod and its containers: SIGTERM, then SIGKILL after timeout (Podman's default, ten seconds, when timeout
// is not positive). Stopping a pod that is stopped is not an error (Podman only).
func (c *Client) StopPod(ctx context.Context, id string, timeout time.Duration) error {
	if err := c.requirePodman("stop a pod"); err != nil {
		return err
	}
	return c.call(ctx, request{method: http.MethodPost, path: "/pods/" + url.PathEscape(id) + "/stop", query: timeoutQuery("t", timeout)}, nil)
}

// RemovePod removes a pod and its containers (Podman only). A pod that runs is removed only with force.
func (c *Client) RemovePod(ctx context.Context, id string, force bool) error {
	if err := c.requirePodman("remove a pod"); err != nil {
		return err
	}
	q := url.Values{}
	if force {
		q.Set("force", "true")
	}
	return c.call(ctx, request{method: http.MethodDelete, path: "/pods/" + url.PathEscape(id), query: q}, nil)
}

// PrunePods removes the stopped pods (Podman only).
func (c *Client) PrunePods(ctx context.Context) (PruneResult, error) {
	if err := c.requirePodman("prune pods"); err != nil {
		return PruneResult{}, err
	}
	var raw json.RawMessage
	if err := c.call(ctx, request{method: http.MethodPost, path: "/pods/prune"}, &raw); err != nil {
		return PruneResult{}, err
	}
	return decodePrune(raw, "PodsDeleted")
}
