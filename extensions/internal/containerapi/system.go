package containerapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/rm4n0s/errors"
)

// decodePrune reads the answer of a prune call. Docker answers with an object that names what was deleted under dockerKey
// ("ContainersDeleted", ...) and the bytes freed; libpod answers with a list of reports ({"Id": ..., "Err": ..., "Size":
// ...}, or "Name" and "Error" for networks), of which the ones with an error are not counted.
func decodePrune(raw json.RawMessage, dockerKey string) (PruneResult, error) {
	var res PruneResult
	raw = bytes.TrimSpace(raw)
	switch {
	case bytes.HasPrefix(raw, []byte("[")):
		var reports []struct {
			ID    string          `json:"Id"`
			Name  string          `json:"Name"`
			Err   json.RawMessage `json:"Err"`
			Error json.RawMessage `json:"Error"`
			Size  int64           `json:"Size"`
		}
		if err := json.Unmarshal(raw, &reports); err != nil {
			return PruneResult{}, errors.NewErr("ContainerAPIDecodeFailed", err).SetMetadata("what", "prune report")
		}
		for _, r := range reports {
			if isSet(r.Err) || isSet(r.Error) {
				continue
			}
			name := r.ID
			if name == "" {
				name = r.Name
			}
			res.Deleted = append(res.Deleted, name)
			res.SpaceReclaimed += r.Size
		}
	case bytes.HasPrefix(raw, []byte("{")):
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return PruneResult{}, errors.NewErr("ContainerAPIDecodeFailed", err).SetMetadata("what", "prune report")
		}
		if v, ok := fields["SpaceReclaimed"]; ok {
			if err := json.Unmarshal(v, &res.SpaceReclaimed); err != nil {
				return PruneResult{}, errors.NewErr("ContainerAPIDecodeFailed", err).SetMetadata("what", "prune report")
			}
		}
		var items []json.RawMessage
		if v, ok := fields[dockerKey]; ok && isSet(v) {
			if err := json.Unmarshal(v, &items); err != nil {
				return PruneResult{}, errors.NewErr("ContainerAPIDecodeFailed", err).SetMetadata("what", "prune report")
			}
		}
		for _, it := range items {
			var name string
			if json.Unmarshal(it, &name) == nil {
				res.Deleted = append(res.Deleted, name)
				continue
			}
			var img struct{ Untagged, Deleted string }
			if err := json.Unmarshal(it, &img); err != nil {
				return PruneResult{}, errors.NewErr("ContainerAPIDecodeFailed", err).SetMetadata("what", "prune report")
			}
			if img.Deleted != "" {
				res.Deleted = append(res.Deleted, img.Deleted)
			} else if img.Untagged != "" {
				res.Deleted = append(res.Deleted, img.Untagged)
			}
		}
	}
	return res, nil
}

// isSet reports whether a JSON value is present and not null or an empty string.
func isSet(v json.RawMessage) bool {
	s := strings.TrimSpace(string(v))
	return s != "" && s != "null" && s != `""`
}

// trimSHA removes the "sha256:" that Docker puts in front of an image id.
func trimSHA(id string) string { return strings.TrimPrefix(id, "sha256:") }

// DiskUsage returns what the images, containers, volumes and build cache use ("system df"). The size maps are keyed by the
// image id without "sha256:" and by the volume name.
func (c *Client) DiskUsage(ctx context.Context) (DiskUsage, error) {
	if c.kind == KindPodman {
		return c.libpodDiskUsage(ctx)
	}
	var df struct {
		LayersSize int64 `json:"LayersSize"`
		Images     []struct {
			ID         string `json:"Id"`
			Size       int64  `json:"Size"`
			SharedSize int64  `json:"SharedSize"`
			Containers int    `json:"Containers"`
		} `json:"Images"`
		Containers []struct {
			State  string `json:"State"`
			SizeRw int64  `json:"SizeRw"`
		} `json:"Containers"`
		Volumes []struct {
			Name      string `json:"Name"`
			UsageData *struct {
				Size     int64 `json:"Size"`
				RefCount int   `json:"RefCount"`
			} `json:"UsageData"`
		} `json:"Volumes"`
		BuildCache []struct {
			Size  int64 `json:"Size"`
			InUse bool  `json:"InUse"`
		} `json:"BuildCache"`
	}
	if err := c.call(ctx, request{method: http.MethodGet, path: "/system/df"}, &df); err != nil {
		return DiskUsage{}, err
	}
	du := DiskUsage{ImageSizes: map[string]int64{}, VolumeSizes: map[string]int64{}}
	var imagesSum int64
	for _, im := range df.Images {
		du.Images.Count++
		imagesSum += im.Size
		du.ImageSizes[trimSHA(im.ID)] = im.Size
		if im.Containers > 0 {
			du.Images.Active++
		} else {
			unique := im.Size - im.SharedSize
			if im.SharedSize <= 0 || unique < 0 {
				unique = im.Size
			}
			du.Images.Reclaimable += unique
		}
	}
	du.Images.Size = df.LayersSize
	if du.Images.Size <= 0 {
		du.Images.Size = imagesSum
	}
	for _, ct := range df.Containers {
		du.Containers.Count++
		if ct.SizeRw > 0 {
			du.Containers.Size += ct.SizeRw
		}
		if ct.State == "running" {
			du.Containers.Active++
		} else if ct.SizeRw > 0 {
			du.Containers.Reclaimable += ct.SizeRw
		}
	}
	for _, v := range df.Volumes {
		du.Volumes.Count++
		if v.UsageData == nil || v.UsageData.Size < 0 {
			if v.UsageData != nil && v.UsageData.RefCount > 0 {
				du.Volumes.Active++
			}
			continue
		}
		du.Volumes.Size += v.UsageData.Size
		du.VolumeSizes[v.Name] = v.UsageData.Size
		if v.UsageData.RefCount > 0 {
			du.Volumes.Active++
		} else {
			du.Volumes.Reclaimable += v.UsageData.Size
		}
	}
	for _, b := range df.BuildCache {
		du.BuildCache.Count++
		du.BuildCache.Size += b.Size
		if b.InUse {
			du.BuildCache.Active++
		} else {
			du.BuildCache.Reclaimable += b.Size
		}
	}
	return du, nil
}

// libpodDiskUsage is DiskUsage for Podman, whose "system df" has its own shape.
func (c *Client) libpodDiskUsage(ctx context.Context) (DiskUsage, error) {
	var df struct {
		ImagesSize int64 `json:"ImagesSize"`
		Images     []struct {
			ImageID    string `json:"ImageID"`
			Size       int64  `json:"Size"`
			UniqueSize int64  `json:"UniqueSize"`
			Containers int    `json:"Containers"`
		} `json:"Images"`
		Containers []struct {
			Status string `json:"Status"`
			RWSize int64  `json:"RWSize"`
		} `json:"Containers"`
		Volumes []struct {
			VolumeName      string `json:"VolumeName"`
			Links           int    `json:"Links"`
			Size            int64  `json:"Size"`
			ReclaimableSize int64  `json:"ReclaimableSize"`
		} `json:"Volumes"`
	}
	if err := c.call(ctx, request{method: http.MethodGet, path: "/system/df"}, &df); err != nil {
		return DiskUsage{}, err
	}
	du := DiskUsage{ImageSizes: map[string]int64{}, VolumeSizes: map[string]int64{}}
	var imagesSum int64
	for _, im := range df.Images {
		du.Images.Count++
		imagesSum += im.Size
		du.ImageSizes[trimSHA(im.ImageID)] = im.Size
		if im.Containers > 0 {
			du.Images.Active++
		} else {
			du.Images.Reclaimable += im.UniqueSize
		}
	}
	du.Images.Size = df.ImagesSize
	if du.Images.Size <= 0 {
		du.Images.Size = imagesSum
	}
	for _, ct := range df.Containers {
		du.Containers.Count++
		if ct.RWSize > 0 {
			du.Containers.Size += ct.RWSize
		}
		if ct.Status == "running" {
			du.Containers.Active++
		} else if ct.RWSize > 0 {
			du.Containers.Reclaimable += ct.RWSize
		}
	}
	for _, v := range df.Volumes {
		du.Volumes.Count++
		du.Volumes.Size += v.Size
		du.Volumes.Reclaimable += v.ReclaimableSize
		du.VolumeSizes[v.VolumeName] = v.Size
		if v.Links > 0 {
			du.Volumes.Active++
		}
	}
	return du, nil
}
