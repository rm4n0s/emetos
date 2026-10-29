package containerapi_test

import (
	"net/http"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi/containerapitest"
)

func TestImages(t *testing.T) {
	for _, kind := range kinds {
		t.Run(string(kind), func(t *testing.T) {
			f, c := engine(t, kind)
			id := f.AddImage(9<<20, "docker.io/library/alpine:3.20", "docker.io/library/alpine:latest")
			dangling := f.AddImage(1 << 20)
			f.AddContainer("user", "docker.io/library/alpine:3.20", nil, "running")

			list, err := c.ListImages(bg, containerapi.ListImagesOptions{})
			if err != nil {
				t.Fatal(err)
			}
			byID := map[string]containerapi.Image{}
			for _, im := range list {
				byID[strings.TrimPrefix(im.ID, "sha256:")] = im
			}
			alpine, dang := byID[id], byID[dangling]
			if !reflect.DeepEqual(alpine.RepoTags, []string{"docker.io/library/alpine:3.20", "docker.io/library/alpine:latest"}) || alpine.Dangling || alpine.Size != 9<<20 || alpine.Created.IsZero() {
				t.Errorf("alpine = %+v", alpine)
			}
			if !dang.Dangling || len(dang.RepoTags) != 0 {
				t.Errorf("the image with no name = %+v", dang)
			}
			if alpine.Containers != 1 || dang.Containers != 0 {
				t.Errorf("containers using them: %d and %d, want 1 and 0", alpine.Containers, dang.Containers)
			}
			if kind == containerapi.KindDocker && !strings.HasPrefix(alpine.ID, "sha256:") {
				t.Errorf("Docker's image id = %q", alpine.ID)
			}

			only, err := c.ListImages(bg, containerapi.ListImagesOptions{Filters: containerapi.Filters{"dangling": {"true"}}})
			if err != nil || len(only) != 1 || !only[0].Dangling {
				t.Errorf("dangling only = %+v, %v", only, err)
			}

			insp, err := c.InspectImage(bg, "docker.io/library/alpine:3.20")
			if err != nil {
				t.Fatal(err)
			}
			if strings.TrimPrefix(insp.ID, "sha256:") != id || insp.Architecture != "amd64" || insp.OS != "linux" || !reflect.DeepEqual(insp.Cmd, []string{"sh"}) ||
				!reflect.DeepEqual(insp.ExposedPorts, []string{"80/tcp"}) || insp.Size != 9<<20 || len(insp.Raw) == 0 || insp.Created.IsZero() {
				t.Errorf("inspect = %+v", insp)
			}
			// by id, and by a name written short
			if _, err := c.InspectImage(bg, id[:12]); err != nil {
				t.Errorf("by id: %v", err)
			}

			hist, err := c.ImageHistory(bg, "docker.io/library/alpine:3.20")
			if err != nil || len(hist) != 2 || hist[0].CreatedBy == "" || hist[1].Size != 9<<20 || hist[0].Created.IsZero() {
				t.Errorf("history = %+v, %v", hist, err)
			}
		})
	}
}

func TestTagAndRemoveImages(t *testing.T) {
	for _, kind := range kinds {
		t.Run(string(kind), func(t *testing.T) {
			f, c := engine(t, kind)
			id := f.AddImage(5, "docker.io/library/app:1")
			if err := c.TagImage(bg, "docker.io/library/app:1", "localhost/app", "stable"); err != nil {
				t.Fatal(err)
			}
			q := queryOf(t, lastRequest(t, f, http.MethodPost, "/tag"))
			if q.Get("repo") != "localhost/app" || q.Get("tag") != "stable" {
				t.Errorf("tag query = %v", q)
			}
			if err := c.TagImage(bg, "docker.io/library/app:1", "localhost/app", ""); err != nil {
				t.Fatal(err)
			}
			if q := queryOf(t, lastRequest(t, f, http.MethodPost, "/tag")); q.Has("tag") {
				t.Errorf("an empty tag was sent: %v", q)
			}
			// removing one name leaves the image
			res, err := c.RemoveImage(bg, "localhost/app:stable", containerapi.RemoveImageOptions{})
			if err != nil || !reflect.DeepEqual(res.Untagged, []string{"localhost/app:stable"}) || len(res.Deleted) != 0 {
				t.Errorf("remove a name = %+v, %v", res, err)
			}
			// a container that uses the image blocks its removal
			f.AddContainer("holder", "docker.io/library/app:1", nil, "running")
			_, err = c.RemoveImage(bg, id, containerapi.RemoveImageOptions{})
			wantRoute(t, err, "Client.RemoveImage->Client.call->Client.do.ContainerAPIConflict")
			res, err = c.RemoveImage(bg, id, containerapi.RemoveImageOptions{Force: true, NoPrune: true})
			if err != nil || len(res.Deleted) != 1 || strings.TrimPrefix(res.Deleted[0], "sha256:") != id || len(res.Untagged) == 0 {
				t.Errorf("forced removal = %+v, %v", res, err)
			}
			q = queryOf(t, lastRequest(t, f, http.MethodDelete, id))
			if q.Get("force") != "true" || q.Get("noprune") != "true" {
				t.Errorf("remove query = %v", q)
			}
			_, err = c.InspectImage(bg, id)
			wantRoute(t, err, "ContainerAPINotFound")
		})
	}
}

func TestPruneImages(t *testing.T) {
	for _, kind := range kinds {
		t.Run(string(kind), func(t *testing.T) {
			f, c := engine(t, kind)
			f.AddImage(10, "docker.io/library/used:1")
			f.AddImage(20, "docker.io/library/unused:1")
			f.AddImage(30)
			f.AddContainer("user", "docker.io/library/used:1", nil, "running")
			f.AddImage(0) // another dangling one
			res, err := c.PruneImages(bg, containerapi.PruneImagesOptions{})
			if err != nil || len(res.Deleted) != 2 {
				t.Fatalf("dangling prune = %+v, %v", res, err)
			}
			q := queryOf(t, lastRequest(t, f, http.MethodPost, "/images/prune"))
			if kind == containerapi.KindDocker && q.Get("filters") != `{"dangling":["true"]}` {
				t.Errorf("Docker prune query = %v", q)
			}
			if kind == containerapi.KindPodman && q.Has("all") {
				t.Errorf("Podman prune query = %v", q)
			}
			res, err = c.PruneImages(bg, containerapi.PruneImagesOptions{All: true})
			if err != nil || len(res.Deleted) != 2 {
				t.Fatalf("prune all = %+v, %v (want the two unused named images: the unused one and BusyBox)", res, err)
			}
			q = queryOf(t, lastRequest(t, f, http.MethodPost, "/images/prune"))
			if kind == containerapi.KindDocker && q.Get("filters") != `{"dangling":["false"]}` {
				t.Errorf("Docker prune-all query = %v", q)
			}
			if kind == containerapi.KindPodman && q.Get("all") != "true" {
				t.Errorf("Podman prune-all query = %v", q)
			}
			left, _ := c.ListImages(bg, containerapi.ListImagesOptions{})
			if len(left) != 1 || !reflect.DeepEqual(left[0].RepoTags, []string{"docker.io/library/used:1"}) {
				t.Errorf("left = %+v, want only the image a container uses", left)
			}
			// with a filter the user's own filters are kept (Docker adds dangling to them)
			if _, err := c.PruneImages(bg, containerapi.PruneImagesOptions{Filters: containerapi.Filters{"label": {"dev.emetos.test=1"}}}); err != nil {
				t.Fatal(err)
			}
			q = queryOf(t, lastRequest(t, f, http.MethodPost, "/images/prune"))
			if kind == containerapi.KindDocker && !strings.Contains(q.Get("filters"), `"label":["dev.emetos.test=1"]`) {
				t.Errorf("filters lost: %v", q)
			}
		})
	}
}

func TestPullImage(t *testing.T) {
	for _, kind := range kinds {
		t.Run(string(kind), func(t *testing.T) {
			f, c := engine(t, kind)
			var lines []containerapi.PullProgress
			if err := c.PullImage(bg, "docker.io/library/alpine:3.20", func(p containerapi.PullProgress) { lines = append(lines, p) }); err != nil {
				t.Fatal(err)
			}
			if len(lines) < 3 {
				t.Fatalf("progress = %+v", lines)
			}
			q := queryOf(t, lastRequest(t, f, http.MethodPost, "/images/"+map[containerapi.Kind]string{containerapi.KindDocker: "create", containerapi.KindPodman: "pull"}[kind]))
			if kind == containerapi.KindDocker {
				if q.Get("fromImage") != "docker.io/library/alpine" || q.Get("tag") != "3.20" {
					t.Errorf("Docker pull query = %v", q)
				}
				var sawBytes bool
				for _, l := range lines {
					if l.Total == 4096 && l.Current == 1024 && l.ID == "layer1" {
						sawBytes = true
					}
				}
				if !sawBytes {
					t.Errorf("no line with the byte counts: %+v", lines)
				}
			} else {
				if q.Get("reference") != "docker.io/library/alpine:3.20" {
					t.Errorf("Podman pull query = %v", q)
				}
				if !strings.HasPrefix(lines[0].Status, "Trying to pull") || lines[len(lines)-1].ID == "" {
					t.Errorf("Podman progress = %+v", lines)
				}
			}
			if _, err := c.InspectImage(bg, "docker.io/library/alpine:3.20"); err != nil {
				t.Errorf("the image is not there after the pull: %v", err)
			}
			// pulling what is there again, with no callback
			if err := c.PullImage(bg, "docker.io/library/alpine:3.20", nil); err != nil {
				t.Fatal(err)
			}
			// a pull by digest names the digest as the tag
			if err := c.PullImage(bg, "ghcr.io/org/app@sha256:abcd", nil); err != nil {
				t.Fatal(err)
			}
			if kind == containerapi.KindDocker {
				if q := queryOf(t, lastRequest(t, f, http.MethodPost, "/images/create")); q.Get("fromImage") != "ghcr.io/org/app" || q.Get("tag") != "sha256:abcd" {
					t.Errorf("digest pull query = %v", q)
				}
			}
		})
	}
}

func TestPullFailureIsReportedFromTheStream(t *testing.T) {
	for _, kind := range kinds {
		t.Run(string(kind), func(t *testing.T) {
			f, c := engine(t, kind)
			f.FailPull("registry.example/private:1", "pull access denied for registry.example/private, repository does not exist")
			err := c.PullImage(bg, "registry.example/private:1", nil)
			appErr := routeOf(t, err, "Client.PullImage->scanLines->Client.PullImage.func2.ContainerAPIPullFailed")
			if !strings.Contains(appErr.Message, "pull access denied") || appErr.Metadata["image"] != "registry.example/private:1" {
				t.Errorf("message %q, metadata %v", appErr.Message, appErr.Metadata)
			}
		})
	}
}

func TestPullStreamProblems(t *testing.T) {
	f, c := engine(t, containerapi.KindDocker)
	if _, err := c.Negotiate(bg); err != nil {
		t.Fatal(err)
	}
	f.Override(http.MethodPost, "/images/create", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("{\"status\":\"ok\"}\nthis is not json\n"))
	})
	wantRoute(t, c.PullImage(bg, "x:1", nil), "Client.PullImage->scanLines->Client.PullImage.func2.ContainerAPIDecodeFailed")
	f.Override(http.MethodPost, "/images/create", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"pull access denied"}`))
	})
	// the earlier override is first in line; ask for a different path
	f2, c2 := engine(t, containerapi.KindDocker)
	f2.FailNext(http.MethodPost, "/images/create", http.StatusInternalServerError, "registry unreachable")
	wantRoute(t, c2.PullImage(bg, "x:1", nil), "Client.PullImage->Client.do.ContainerAPIEngineError")
	f3, c3 := engine(t, containerapi.KindDocker)
	if _, err := c3.Negotiate(bg); err != nil {
		t.Fatal(err)
	}
	f3.Override(http.MethodPost, "/images/create", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 5<<20) + "\n"))
	})
	wantRoute(t, c3.PullImage(bg, "x:1", nil), "scanLines.ContainerAPIBodyTooLarge")
}

func TestNetworks(t *testing.T) {
	for _, kind := range kinds {
		t.Run(string(kind), func(t *testing.T) {
			f, c := engine(t, kind)
			id, err := c.CreateNetwork(bg, containerapi.NetworkSpec{Name: "back", Driver: "bridge", Labels: map[string]string{"dev.emetos.test": "1"}, Internal: true, IPv6: true})
			if err != nil {
				t.Fatal(err)
			}
			if len(id) != 64 {
				t.Errorf("id = %q", id)
			}
			if _, err := c.CreateNetwork(bg, containerapi.NetworkSpec{Name: "front"}); err != nil {
				t.Fatal(err)
			}
			all, err := c.ListNetworks(bg, nil)
			if err != nil {
				t.Fatal(err)
			}
			var names []string
			for _, n := range all {
				names = append(names, n.Name)
			}
			sort.Strings(names)
			if !reflect.DeepEqual(names, []string{"back", "bridge", "front"}) {
				t.Errorf("networks = %v", names)
			}
			mine, err := c.ListNetworks(bg, containerapitest.LabelFilter())
			if err != nil || len(mine) != 1 || mine[0].Name != "back" {
				t.Fatalf("labelled = %+v, %v", mine, err)
			}
			n := mine[0]
			if n.ID != id || n.Driver != "bridge" || !n.Internal || !n.IPv6 || n.Labels["dev.emetos.test"] != "1" || n.Created.IsZero() || len(n.Subnets) != 1 || n.Subnets[0].Subnet == "" || len(n.Raw) == 0 {
				t.Errorf("network = %+v", n)
			}
			one, err := c.InspectNetwork(bg, "back")
			if err != nil || one.ID != id || len(one.Subnets) != 1 {
				t.Errorf("inspect = %+v, %v", one, err)
			}
			// a container that uses a network keeps it
			f.AddContainer("user", containerapitest.ImageBusyBox, nil, "running")
			res, err := c.PruneNetworks(bg, containerapitest.LabelFilter())
			if err != nil || !reflect.DeepEqual(res.Deleted, []string{"back"}) {
				t.Errorf("prune = %+v, %v", res, err)
			}
			if err := c.RemoveNetwork(bg, "front"); err != nil {
				t.Fatal(err)
			}
			_, err = c.InspectNetwork(bg, "front")
			wantRoute(t, err, "ContainerAPINotFound")
		})
	}
}

func TestVolumes(t *testing.T) {
	for _, kind := range kinds {
		t.Run(string(kind), func(t *testing.T) {
			f, c := engine(t, kind)
			v, err := c.CreateVolume(bg, containerapi.VolumeSpec{Name: "data", Driver: "local", Labels: map[string]string{"dev.emetos.test": "1"}})
			if err != nil {
				t.Fatal(err)
			}
			if v.Name != "data" || v.Driver != "local" || v.Labels["dev.emetos.test"] != "1" || v.Mountpoint == "" || v.Created.IsZero() || len(v.Raw) == 0 {
				t.Errorf("created = %+v", v)
			}
			if _, err := c.CreateVolume(bg, containerapi.VolumeSpec{Name: "scratch"}); err != nil {
				t.Fatal(err)
			}
			anon, err := c.CreateVolume(bg, containerapi.VolumeSpec{})
			if err != nil || anon.Name == "" {
				t.Errorf("a volume with no name = %+v, %v", anon, err)
			}
			all, err := c.ListVolumes(bg, nil)
			if err != nil || len(all) != 3 {
				t.Errorf("all = %d, %v", len(all), err)
			}
			mine, err := c.ListVolumes(bg, containerapitest.LabelFilter())
			if err != nil || len(mine) != 1 || mine[0].Name != "data" || mine[0].Size != -1 {
				t.Errorf("labelled = %+v, %v", mine, err)
			}
			got, err := c.InspectVolume(bg, "data")
			if err != nil || got.Name != "data" {
				t.Errorf("inspect = %+v, %v", got, err)
			}
			// a volume in use needs force
			res, err := c.CreateContainer(bg, containerapi.ContainerSpec{Name: "user", Image: containerapitest.ImageBusyBox, Binds: []string{"data:/d"}})
			if err != nil {
				t.Fatal(err)
			}
			wantRoute(t, c.RemoveVolume(bg, "data", false), "Client.RemoveVolume->Client.call->Client.do.ContainerAPIConflict")
			if err := c.RemoveVolume(bg, "data", true); err != nil {
				t.Fatal(err)
			}
			if q := queryOf(t, lastRequest(t, f, http.MethodDelete, "/volumes/data")); q.Get("force") != "true" {
				t.Errorf("remove query = %v", q)
			}
			pr, err := c.PruneVolumes(bg, nil)
			if err != nil || len(pr.Deleted) != 2 {
				t.Errorf("prune = %+v, %v", pr, err)
			}
			_ = res
		})
	}
}

func TestPods(t *testing.T) {
	f, c := engine(t, containerapi.KindPodman)
	id, err := c.CreatePod(bg, containerapi.PodSpec{Name: "stack", Labels: map[string]string{"dev.emetos.test": "1"}})
	if err != nil || len(id) != 64 {
		t.Fatalf("CreatePod = %q, %v", id, err)
	}
	wantRoute(t, errOf(c.CreatePod(bg, containerapi.PodSpec{Name: "stack"})), "Client.CreatePod->Client.call->Client.do.ContainerAPIConflict")
	web, err := c.CreateContainer(bg, containerapi.ContainerSpec{Name: "web", Image: containerapitest.ImageBusyBox, Pod: "stack", Cmd: []string{"sh", "-c", "echo hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateContainer(bg, containerapi.ContainerSpec{Name: "alone", Image: containerapitest.ImageBusyBox}); err != nil {
		t.Fatal(err)
	}

	pods, err := c.ListPods(bg, nil)
	if err != nil || len(pods) != 1 {
		t.Fatalf("ListPods = %+v, %v", pods, err)
	}
	p := pods[0]
	if p.ID != id || p.Name != "stack" || p.Status != "Created" || p.Labels["dev.emetos.test"] != "1" || p.Created.IsZero() || len(p.Containers) != 1 || p.Containers[0] != (containerapi.PodContainer{ID: web.ID, Name: "web", Status: "created"}) {
		t.Errorf("pod = %+v", p)
	}
	// the container knows its pod
	list, _ := c.ListContainers(bg, containerapi.ListContainersOptions{All: true})
	for _, x := range list {
		if (x.Name() == "web") != (x.Pod == id && x.PodName == "stack") {
			t.Errorf("container %s: pod %q %q", x.Name(), x.Pod, x.PodName)
		}
	}
	if info, _ := c.InspectContainer(bg, web.ID); info == nil || info.Pod != id {
		t.Errorf("inspect pod = %+v", info)
	}

	steps := []struct {
		name   string
		do     func() error
		status string
	}{
		{"start", func() error { return c.PodDo(bg, "stack", containerapi.PodStart) }, "Running"},
		{"pause", func() error { return c.PodDo(bg, "stack", containerapi.PodPause) }, "Degraded"},
		{"unpause", func() error { return c.PodDo(bg, "stack", containerapi.PodUnpause) }, "Running"},
		{"restart", func() error { return c.PodDo(bg, "stack", containerapi.PodRestart) }, "Running"},
		{"stop with a timeout", func() error { return c.StopPod(bg, "stack", 3e9) }, "Exited"},
		{"start again", func() error { return c.PodDo(bg, id, containerapi.PodStart) }, "Running"},
		{"kill", func() error { return c.PodDo(bg, "stack", containerapi.PodKill) }, "Exited"},
	}
	for _, s := range steps {
		if err := s.do(); err != nil {
			t.Fatalf("%s: %v", s.name, err)
		}
		insp, err := c.InspectPod(bg, "stack")
		if err != nil {
			t.Fatal(err)
		}
		if insp.Status == "Degraded" && s.status == "Degraded" || insp.Status == s.status {
			continue
		}
		if s.name != "pause" {
			t.Errorf("after %s the pod is %s, want %s", s.name, insp.Status, s.status)
		}
		if len(insp.Raw) == 0 || len(insp.Containers) != 1 || insp.Containers[0].Name != "web" || insp.Containers[0].Status == "" {
			t.Errorf("inspect = %+v", insp)
		}
	}
	if q := queryOf(t, lastRequest(t, f, http.MethodPost, "/pods/"+"stack"+"/stop")); q.Get("t") != "3" {
		t.Errorf("stop query = %v", q)
	}
	if err := c.PodDo(bg, "stack", containerapi.PodStart); err != nil {
		t.Fatal(err)
	}
	wantRoute(t, c.RemovePod(bg, "stack", false), "Client.RemovePod->Client.call->Client.do.ContainerAPIConflict")
	if err := c.RemovePod(bg, "stack", true); err != nil {
		t.Fatal(err)
	}
	if left, _ := c.ListContainers(bg, containerapi.ListContainersOptions{All: true}); len(left) != 1 || left[0].Name() != "alone" {
		t.Errorf("after the pod went: %+v", left)
	}
	_, err = c.InspectPod(bg, "stack")
	wantRoute(t, err, "Client.InspectPod->Client.call->Client.do.ContainerAPINotFound")

	// prune: a stopped pod goes, a running one stays
	for _, name := range []string{"one", "two"} {
		if _, err := c.CreatePod(bg, containerapi.PodSpec{Name: name}); err != nil {
			t.Fatal(err)
		}
	}
	res, err := c.PrunePods(bg)
	if err != nil || len(res.Deleted) != 2 {
		t.Errorf("PrunePods = %+v, %v", res, err)
	}
}

// errOf drops the first result of a call that returns a value and an error.
func errOf[T any](_ T, err error) error { return err }

func TestDiskUsage(t *testing.T) {
	for _, kind := range kinds {
		t.Run(string(kind), func(t *testing.T) {
			f, c := engine(t, kind)
			used := f.AddImage(100, "docker.io/library/used:1")
			unused := f.AddImage(300, "docker.io/library/unused:1")
			f.AddContainer("run", "docker.io/library/used:1", nil, "running")
			f.AddContainer("stopped", "docker.io/library/used:1", nil, "exited")
			if _, err := c.CreateVolume(bg, containerapi.VolumeSpec{Name: "kept"}); err != nil {
				t.Fatal(err)
			}
			if _, err := c.CreateVolume(bg, containerapi.VolumeSpec{Name: "loose"}); err != nil {
				t.Fatal(err)
			}
			if _, err := c.CreateContainer(bg, containerapi.ContainerSpec{Name: "holder", Image: "docker.io/library/used:1", Binds: []string{"kept:/k"}}); err != nil {
				t.Fatal(err)
			}
			f.SetVolumeSize("kept", 4000)
			f.SetVolumeSize("loose", 6000)
			du, err := c.DiskUsage(bg)
			if err != nil {
				t.Fatal(err)
			}
			busy := int64(4 << 20)
			if du.Images.Count != 3 || du.Images.Active != 1 || du.Images.Size != busy+100+300 || du.Images.Reclaimable != busy+300 {
				t.Errorf("images = %+v", du.Images)
			}
			if du.ImageSizes[strings.TrimPrefix(used, "sha256:")] != 100 || du.ImageSizes[unused] != 300 {
				t.Errorf("image sizes = %v", du.ImageSizes)
			}
			if du.Containers.Count != 3 || du.Containers.Active != 1 || du.Containers.Size != 3000 || du.Containers.Reclaimable != 2000 {
				t.Errorf("containers = %+v", du.Containers)
			}
			if du.Volumes.Count != 2 || du.Volumes.Active != 1 || du.Volumes.Size != 10000 || du.Volumes.Reclaimable != 6000 || du.VolumeSizes["loose"] != 6000 {
				t.Errorf("volumes = %+v sizes %v", du.Volumes, du.VolumeSizes)
			}
			if kind == containerapi.KindDocker {
				if du.BuildCache.Count != 2 || du.BuildCache.Active != 1 || du.BuildCache.Size != 800 || du.BuildCache.Reclaimable != 500 {
					t.Errorf("build cache = %+v", du.BuildCache)
				}
			} else if du.BuildCache.Count != 0 {
				t.Errorf("Podman has no build cache in df: %+v", du.BuildCache)
			}
			if du.Total() != du.Images.Size+du.Containers.Size+du.Volumes.Size+du.BuildCache.Size {
				t.Error("Total")
			}
		})
	}
}
