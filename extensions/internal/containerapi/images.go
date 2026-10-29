package containerapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/rm4n0s/errors"
)

// imagePath returns the path of an image by name or id, each segment escaped: a name like docker.io/library/busybox:1.37
// keeps its slashes, which both engines' routes take.
func imagePath(name, suffix string) string {
	parts := strings.Split(name, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return "/images/" + strings.Join(parts, "/") + suffix
}

// noneTag is the name Docker gives an image with no name.
const noneTag = "<none>:<none>"

// wireImage is a row of either engine's image list.
type wireImage struct {
	ID          string            `json:"Id"`
	RepoTags    []string          `json:"RepoTags"`
	RepoDigests []string          `json:"RepoDigests"`
	Created     flexTime          `json:"Created"`
	Size        int64             `json:"Size"`
	Containers  *int              `json:"Containers"`
	Labels      map[string]string `json:"Labels"`
	Dangling    bool              `json:"Dangling"`
}

// ListImages lists the images. An image with no name has Dangling set and no RepoTags.
func (c *Client) ListImages(ctx context.Context, opts ListImagesOptions) ([]Image, error) {
	q := url.Values{}
	if opts.All {
		q.Set("all", "true")
	}
	opts.Filters.addTo(q)
	var rows []wireImage
	if err := c.call(ctx, request{method: http.MethodGet, path: "/images/json", query: q}, &rows); err != nil {
		return nil, err
	}
	out := make([]Image, 0, len(rows))
	for _, r := range rows {
		img := Image{
			ID: r.ID, RepoDigests: r.RepoDigests, Created: r.Created.Time, Size: r.Size, Labels: r.Labels,
			Containers: -1, Dangling: r.Dangling,
		}
		for _, t := range r.RepoTags {
			if t != noneTag {
				img.RepoTags = append(img.RepoTags, t)
			}
		}
		if r.Containers != nil {
			img.Containers = *r.Containers
		}
		if len(img.RepoTags) == 0 {
			img.Dangling = true
		}
		out = append(out, img)
	}
	return out, nil
}

// InspectImage returns the details of an image (by name or id), with the engine's JSON in Raw.
func (c *Client) InspectImage(ctx context.Context, name string) (*ImageInspect, error) {
	var raw json.RawMessage
	if err := c.call(ctx, request{method: http.MethodGet, path: imagePath(name, "/json")}, &raw); err != nil {
		return nil, err
	}
	var w struct {
		ID           string   `json:"Id"`
		RepoTags     []string `json:"RepoTags"`
		RepoDigests  []string `json:"RepoDigests"`
		Created      flexTime `json:"Created"`
		Size         int64    `json:"Size"`
		Architecture string   `json:"Architecture"`
		OS           string   `json:"Os"`
		Config       struct {
			Env          []string            `json:"Env"`
			Cmd          flexStrings         `json:"Cmd"`
			Entrypoint   flexStrings         `json:"Entrypoint"`
			WorkingDir   string              `json:"WorkingDir"`
			User         string              `json:"User"`
			Labels       map[string]string   `json:"Labels"`
			ExposedPorts map[string]struct{} `json:"ExposedPorts"`
		} `json:"Config"`
	}
	if err := json.Unmarshal(raw, &w); err != nil {
		return nil, errors.NewErr("ContainerAPIDecodeFailed", err).SetMetadata("method", "GET", "path", imagePath(name, "/json"))
	}
	out := &ImageInspect{
		ID: w.ID, RepoTags: w.RepoTags, RepoDigests: w.RepoDigests, Created: w.Created.Time, Size: w.Size,
		Architecture: w.Architecture, OS: w.OS, Env: w.Config.Env, Cmd: w.Config.Cmd, Entrypoint: w.Config.Entrypoint,
		WorkingDir: w.Config.WorkingDir, User: w.Config.User, Labels: w.Config.Labels, Raw: raw,
	}
	for p := range w.Config.ExposedPorts {
		out.ExposedPorts = append(out.ExposedPorts, p)
	}
	sort.Strings(out.ExposedPorts)
	return out, nil
}

// splitImageRef splits a reference into its repository and its tag or digest: "busybox:1.37" into "busybox" and "1.37",
// "busybox@sha256:ab" into "busybox" and "sha256:ab", "localhost:5000/app" into itself and "latest".
func splitImageRef(ref string) (repo, tag string) {
	if i := strings.Index(ref, "@"); i >= 0 {
		return ref[:i], ref[i+1:]
	}
	slash := strings.LastIndex(ref, "/")
	if colon := strings.LastIndex(ref, ":"); colon > slash {
		return ref[:colon], ref[colon+1:]
	}
	return ref, "latest"
}

// PullImage pulls an image by reference ("busybox:1.37", "ghcr.io/org/app@sha256:..."), calling progress (when it is not
// nil) for each line the engine sends. It returns when the pull is complete. A failure the engine reports in the stream
// is a ContainerAPIPullFailed error.
func (c *Client) PullImage(ctx context.Context, ref string, progress func(PullProgress)) error {
	q := url.Values{}
	path := "/images/pull"
	if c.kind == KindPodman {
		q.Set("reference", ref)
	} else {
		repo, tag := splitImageRef(ref)
		path = "/images/create"
		q.Set("fromImage", repo)
		q.Set("tag", tag)
	}
	resp, err := c.do(ctx, request{method: http.MethodPost, path: path, query: q})
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }() // a body that was read has nothing to report on closing
	return scanLines(ctx, resp.Body, func(line []byte) error {
		var l struct {
			Status         string `json:"status"`
			ID             string `json:"id"`
			Stream         string `json:"stream"`
			Error          string `json:"error"`
			ProgressDetail struct {
				Current int64 `json:"current"`
				Total   int64 `json:"total"`
			} `json:"progressDetail"`
			Images []string `json:"images"`
		}
		if err := json.Unmarshal(line, &l); err != nil {
			return errors.NewErr("ContainerAPIDecodeFailed", err).SetMetadata("method", "POST", "path", path)
		}
		if l.Error != "" {
			return errors.New("ContainerAPIPullFailed", l.Error, "image", ref)
		}
		if progress == nil {
			return nil
		}
		p := PullProgress{ID: l.ID, Status: l.Status, Current: l.ProgressDetail.Current, Total: l.ProgressDetail.Total}
		if l.Stream != "" {
			p.Status = strings.TrimSpace(l.Stream)
			p.ID = ""
		}
		if len(l.Images) > 0 && p.ID == "" {
			p.ID = l.Images[0]
		}
		progress(p)
		return nil
	})
}

// TagImage gives an image another name: repo and tag ("tag" empty is latest).
func (c *Client) TagImage(ctx context.Context, name, repo, tag string) error {
	q := url.Values{"repo": {repo}}
	if tag != "" {
		q.Set("tag", tag)
	}
	return c.call(ctx, request{method: http.MethodPost, path: imagePath(name, "/tag"), query: q}, nil)
}

// RemoveImage removes an image (or one of its names), and says which names were taken off and which images went.
func (c *Client) RemoveImage(ctx context.Context, name string, opts RemoveImageOptions) (RemoveImageResult, error) {
	q := url.Values{}
	if opts.Force {
		q.Set("force", "true")
	}
	if opts.NoPrune {
		q.Set("noprune", "true")
	}
	var raw json.RawMessage
	if err := c.call(ctx, request{method: http.MethodDelete, path: imagePath(name, ""), query: q}, &raw); err != nil {
		return RemoveImageResult{}, err
	}
	var res RemoveImageResult
	trimmed := strings.TrimSpace(string(raw))
	switch {
	case strings.HasPrefix(trimmed, "["): // Docker: [{"Untagged": ...}, {"Deleted": ...}]
		var items []struct {
			Untagged string `json:"Untagged"`
			Deleted  string `json:"Deleted"`
		}
		if err := json.Unmarshal(raw, &items); err != nil {
			return RemoveImageResult{}, errors.NewErr("ContainerAPIDecodeFailed", err).SetMetadata("method", "DELETE", "path", imagePath(name, ""))
		}
		for _, it := range items {
			if it.Untagged != "" {
				res.Untagged = append(res.Untagged, it.Untagged)
			}
			if it.Deleted != "" {
				res.Deleted = append(res.Deleted, it.Deleted)
			}
		}
	case strings.HasPrefix(trimmed, "{"): // libpod: {"Untagged": [...], "Deleted": [...]}
		var v struct {
			Untagged []string `json:"Untagged"`
			Deleted  []string `json:"Deleted"`
		}
		if err := json.Unmarshal(raw, &v); err != nil {
			return RemoveImageResult{}, errors.NewErr("ContainerAPIDecodeFailed", err).SetMetadata("method", "DELETE", "path", imagePath(name, ""))
		}
		res.Untagged, res.Deleted = v.Untagged, v.Deleted
	}
	return res, nil
}

// ImageHistory returns the layers of an image, newest first.
func (c *Client) ImageHistory(ctx context.Context, name string) ([]HistoryEntry, error) {
	var rows []struct {
		ID        string   `json:"Id"`
		Created   flexTime `json:"Created"`
		CreatedBy string   `json:"CreatedBy"`
		Tags      []string `json:"Tags"`
		Size      int64    `json:"Size"`
		Comment   string   `json:"Comment"`
	}
	if err := c.call(ctx, request{method: http.MethodGet, path: imagePath(name, "/history")}, &rows); err != nil {
		return nil, err
	}
	out := make([]HistoryEntry, 0, len(rows))
	for _, r := range rows {
		out = append(out, HistoryEntry{ID: r.ID, Created: r.Created.Time, CreatedBy: r.CreatedBy, Tags: r.Tags, Size: r.Size, Comment: r.Comment})
	}
	return out, nil
}

// PruneImages removes the unused images that match opts.Filters: the dangling ones, or every unused one with opts.All.
func (c *Client) PruneImages(ctx context.Context, opts PruneImagesOptions) (PruneResult, error) {
	q := url.Values{}
	filters := Filters{}
	for k, v := range opts.Filters {
		filters[k] = v
	}
	if c.kind == KindPodman {
		if opts.All {
			q.Set("all", "true")
		}
	} else if _, set := filters["dangling"]; !set {
		filters["dangling"] = []string{boolText(!opts.All)}
	}
	filters.addTo(q)
	var raw json.RawMessage
	if err := c.call(ctx, request{method: http.MethodPost, path: "/images/prune", query: q}, &raw); err != nil {
		return PruneResult{}, err
	}
	return decodePrune(raw, "ImagesDeleted")
}

// boolText returns "true" or "false".
func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
