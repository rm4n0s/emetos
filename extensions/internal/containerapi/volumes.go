package containerapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/rm4n0s/errors"
)

// wireVolume is a volume of either engine.
type wireVolume struct {
	Name       string            `json:"Name"`
	Driver     string            `json:"Driver"`
	Mountpoint string            `json:"Mountpoint"`
	Scope      string            `json:"Scope"`
	CreatedAt  flexTime          `json:"CreatedAt"`
	Labels     map[string]string `json:"Labels"`
	Options    map[string]string `json:"Options"`
	UsageData  *struct {
		Size int64 `json:"Size"`
	} `json:"UsageData"`
}

// volume converts a wire volume; raw is its JSON.
func (w wireVolume) volume(raw json.RawMessage) Volume {
	v := Volume{
		Name: w.Name, Driver: w.Driver, Mountpoint: w.Mountpoint, Scope: w.Scope, Created: w.CreatedAt.Time,
		Labels: w.Labels, Options: w.Options, Size: -1, Raw: raw,
	}
	if w.UsageData != nil {
		v.Size = w.UsageData.Size
	}
	return v
}

// volumesPath is the path of the volume list, which differs between the engines.
func (c *Client) volumesPath() string {
	if c.kind == KindPodman {
		return "/volumes/json"
	}
	return "/volumes"
}

// ListVolumes lists the volumes that match filters (all of them without filters).
func (c *Client) ListVolumes(ctx context.Context, filters Filters) ([]Volume, error) {
	q := url.Values{}
	filters.addTo(q)
	var rows []json.RawMessage
	if c.kind == KindPodman {
		if err := c.call(ctx, request{method: http.MethodGet, path: c.volumesPath(), query: q}, &rows); err != nil {
			return nil, err
		}
	} else {
		var wrapped struct {
			Volumes []json.RawMessage `json:"Volumes"`
		}
		if err := c.call(ctx, request{method: http.MethodGet, path: c.volumesPath(), query: q}, &wrapped); err != nil {
			return nil, err
		}
		rows = wrapped.Volumes
	}
	out := make([]Volume, 0, len(rows))
	for _, raw := range rows {
		var w wireVolume
		if err := json.Unmarshal(raw, &w); err != nil {
			return nil, errors.NewErr("ContainerAPIDecodeFailed", err).SetMetadata("method", "GET", "path", c.volumesPath())
		}
		out = append(out, w.volume(raw))
	}
	return out, nil
}

// InspectVolume returns one volume (by name), with the engine's JSON in Raw.
func (c *Client) InspectVolume(ctx context.Context, name string) (*Volume, error) {
	path := "/volumes/" + url.PathEscape(name)
	if c.kind == KindPodman {
		path += "/json"
	}
	var raw json.RawMessage
	if err := c.call(ctx, request{method: http.MethodGet, path: path}, &raw); err != nil {
		return nil, err
	}
	var w wireVolume
	if err := json.Unmarshal(raw, &w); err != nil {
		return nil, errors.NewErr("ContainerAPIDecodeFailed", err).SetMetadata("method", "GET", "path", path)
	}
	v := w.volume(raw)
	return &v, nil
}

// CreateVolume makes a volume.
func (c *Client) CreateVolume(ctx context.Context, spec VolumeSpec) (*Volume, error) {
	body := map[string]any{}
	if spec.Name != "" {
		body["Name"] = spec.Name
	}
	if spec.Driver != "" {
		body["Driver"] = spec.Driver
	}
	if len(spec.Labels) > 0 {
		if c.kind == KindPodman {
			body["Label"] = spec.Labels
		} else {
			body["Labels"] = spec.Labels
		}
	}
	var raw json.RawMessage
	if err := c.call(ctx, request{method: http.MethodPost, path: "/volumes/create", body: body}, &raw); err != nil {
		return nil, err
	}
	var w wireVolume
	if err := json.Unmarshal(raw, &w); err != nil {
		return nil, errors.NewErr("ContainerAPIDecodeFailed", err).SetMetadata("method", "POST", "path", "/volumes/create")
	}
	v := w.volume(raw)
	return &v, nil
}

// RemoveVolume removes a volume. A volume that a container uses is removed only with force.
func (c *Client) RemoveVolume(ctx context.Context, name string, force bool) error {
	q := url.Values{}
	if force {
		q.Set("force", "true")
	}
	return c.call(ctx, request{method: http.MethodDelete, path: "/volumes/" + url.PathEscape(name), query: q}, nil)
}

// PruneVolumes removes the unused volumes that match filters.
func (c *Client) PruneVolumes(ctx context.Context, filters Filters) (PruneResult, error) {
	q := url.Values{}
	filters.addTo(q)
	var raw json.RawMessage
	if err := c.call(ctx, request{method: http.MethodPost, path: "/volumes/prune", query: q}, &raw); err != nil {
		return PruneResult{}, err
	}
	return decodePrune(raw, "VolumesDeleted")
}
