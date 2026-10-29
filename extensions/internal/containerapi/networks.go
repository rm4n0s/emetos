package containerapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/rm4n0s/errors"
)

// wireNetwork is a network of either engine: Docker's Name/Id/IPAM.Config or libpod's name/id/subnets.
type wireNetwork struct {
	ID      string   `json:"Id"`
	Name    string   `json:"Name"`
	Driver  string   `json:"Driver"`
	Scope   string   `json:"Scope"`
	Created flexTime `json:"Created"`

	Internal   bool `json:"Internal"`
	EnableIPv6 bool `json:"EnableIPv6"`
	IPv6       bool `json:"ipv6_enabled"`

	IPAM struct {
		Config []Subnet `json:"Config"`
	} `json:"IPAM"`
	Subnets []Subnet `json:"subnets"`

	Labels map[string]string `json:"Labels"`
}

// network converts a wire network; raw is its JSON.
func (w wireNetwork) network(raw json.RawMessage) Network {
	n := Network{
		ID: w.ID, Name: w.Name, Driver: w.Driver, Scope: w.Scope, Created: w.Created.Time, Internal: w.Internal,
		IPv6: w.EnableIPv6 || w.IPv6, Subnets: w.Subnets, Labels: w.Labels, Raw: raw,
	}
	if len(n.Subnets) == 0 {
		n.Subnets = w.IPAM.Config
	}
	return n
}

// networksPath is the path of the network list, which differs between the engines.
func (c *Client) networksPath() string {
	if c.kind == KindPodman {
		return "/networks/json"
	}
	return "/networks"
}

// ListNetworks lists the networks that match filters (all of them without filters).
func (c *Client) ListNetworks(ctx context.Context, filters Filters) ([]Network, error) {
	q := url.Values{}
	filters.addTo(q)
	var rows []json.RawMessage
	if err := c.call(ctx, request{method: http.MethodGet, path: c.networksPath(), query: q}, &rows); err != nil {
		return nil, err
	}
	out := make([]Network, 0, len(rows))
	for _, raw := range rows {
		var w wireNetwork
		if err := json.Unmarshal(raw, &w); err != nil {
			return nil, errors.NewErr("ContainerAPIDecodeFailed", err).SetMetadata("method", "GET", "path", c.networksPath())
		}
		out = append(out, w.network(raw))
	}
	return out, nil
}

// InspectNetwork returns one network (by id or name), with the engine's JSON in Raw.
func (c *Client) InspectNetwork(ctx context.Context, id string) (*Network, error) {
	path := "/networks/" + url.PathEscape(id)
	if c.kind == KindPodman {
		path += "/json"
	}
	var raw json.RawMessage
	if err := c.call(ctx, request{method: http.MethodGet, path: path}, &raw); err != nil {
		return nil, err
	}
	if trimmed := bytes.TrimSpace(raw); bytes.HasPrefix(trimmed, []byte("[")) { // older Podman wraps it in a list
		var list []json.RawMessage
		if err := json.Unmarshal(trimmed, &list); err != nil || len(list) == 0 {
			return nil, errors.New("ContainerAPIDecodeFailed", "the engine's answer for the network is an empty list", "method", "GET", "path", path)
		}
		raw = list[0]
	}
	var w wireNetwork
	if err := json.Unmarshal(raw, &w); err != nil {
		return nil, errors.NewErr("ContainerAPIDecodeFailed", err).SetMetadata("method", "GET", "path", path)
	}
	n := w.network(raw)
	return &n, nil
}

// CreateNetwork makes a network and returns its id.
func (c *Client) CreateNetwork(ctx context.Context, spec NetworkSpec) (string, error) {
	var body map[string]any
	if c.kind == KindPodman {
		body = map[string]any{"name": spec.Name}
		if spec.Driver != "" {
			body["driver"] = spec.Driver
		}
		if len(spec.Labels) > 0 {
			body["labels"] = spec.Labels
		}
		if spec.Internal {
			body["internal"] = true
		}
		if spec.IPv6 {
			body["ipv6_enabled"] = true
		}
	} else {
		body = map[string]any{"Name": spec.Name, "CheckDuplicate": true}
		if spec.Driver != "" {
			body["Driver"] = spec.Driver
		}
		if len(spec.Labels) > 0 {
			body["Labels"] = spec.Labels
		}
		if spec.Internal {
			body["Internal"] = true
		}
		if spec.IPv6 {
			body["EnableIPv6"] = true
		}
	}
	var out struct {
		ID string `json:"Id"`
	}
	if err := c.call(ctx, request{method: http.MethodPost, path: "/networks/create", body: body}, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

// RemoveNetwork removes a network (by id or name).
func (c *Client) RemoveNetwork(ctx context.Context, id string) error {
	return c.call(ctx, request{method: http.MethodDelete, path: "/networks/" + url.PathEscape(id)}, nil)
}

// PruneNetworks removes the unused networks that match filters.
func (c *Client) PruneNetworks(ctx context.Context, filters Filters) (PruneResult, error) {
	q := url.Values{}
	filters.addTo(q)
	var raw json.RawMessage
	if err := c.call(ctx, request{method: http.MethodPost, path: "/networks/prune", query: q}, &raw); err != nil {
		return PruneResult{}, err
	}
	return decodePrune(raw, "NetworksDeleted")
}
