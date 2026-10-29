package containerapi_test

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi/containerapitest"
	"github.com/rm4n0s/errors"
)

func TestEngineErrorsKeepTheEnginesMessage(t *testing.T) {
	for _, kind := range kinds {
		for _, tc := range []struct {
			name    string
			call    func(c *containerapi.Client) error
			route   string
			message string // a part of the engine's own words
			status  int
		}{
			{"a container that is not there", func(c *containerapi.Client) error { _, err := c.InspectContainer(bg, "ghost"); return err },
				"Client.InspectContainer->Client.call->Client.do.ContainerAPINotFound", "ghost", 404},
			{"starting one that is not there", func(c *containerapi.Client) error { return c.StartContainer(bg, "ghost") },
				"Client.StartContainer->Client.call->Client.do.ContainerAPINotFound", "ghost", 404},
			{"an image that is not there", func(c *containerapi.Client) error { _, err := c.InspectImage(bg, "ghost:1"); return err },
				"Client.InspectImage->Client.call->Client.do.ContainerAPINotFound", "ghost:1", 404},
			{"a network that is not there", func(c *containerapi.Client) error { return c.RemoveNetwork(bg, "ghost") },
				"Client.RemoveNetwork->Client.call->Client.do.ContainerAPINotFound", "ghost", 404},
			{"a volume that is not there", func(c *containerapi.Client) error { _, err := c.InspectVolume(bg, "ghost"); return err },
				"Client.InspectVolume->Client.call->Client.do.ContainerAPINotFound", "ghost", 404},
			{"creating from an image that is not there", func(c *containerapi.Client) error {
				_, err := c.CreateContainer(bg, containerapi.ContainerSpec{Image: "ghost:1"})
				return err
			}, "Client.CreateContainer->Client.call->Client.do.ContainerAPINotFound", "ghost:1", 404},
			{"a name that is taken", func(c *containerapi.Client) error {
				if _, err := c.CreateContainer(bg, containerapi.ContainerSpec{Name: "dup", Image: containerapitest.ImageBusyBox}); err != nil {
					return err
				}
				_, err := c.CreateContainer(bg, containerapi.ContainerSpec{Name: "dup", Image: containerapitest.ImageBusyBox})
				return err
			}, "Client.CreateContainer->Client.call->Client.do.ContainerAPIConflict", "dup", 409},
			{"a network name that is taken", func(c *containerapi.Client) error {
				_, err := c.CreateNetwork(bg, containerapi.NetworkSpec{Name: "bridge"})
				return err
			}, "Client.CreateNetwork->Client.call->Client.do.ContainerAPIConflict", "bridge", 409},
			{"a request the engine refuses", func(c *containerapi.Client) error {
				_, err := c.CreateNetwork(bg, containerapi.NetworkSpec{})
				return err
			}, "Client.CreateNetwork->Client.call->Client.do.ContainerAPIBadRequest", "name", 400},
		} {
			t.Run(string(kind)+"/"+tc.name, func(t *testing.T) {
				_, c := engine(t, kind)
				err := tc.call(c)
				appErr := routeOf(t, err, tc.route)
				if !strings.Contains(appErr.Message, tc.message) {
					t.Errorf("message = %q, want the engine's words with %q", appErr.Message, tc.message)
				}
				if appErr.Metadata["status"] != tc.status || appErr.Metadata["method"] == nil || appErr.Metadata["path"] == nil {
					t.Errorf("metadata = %v, want the status %d, the method and the path", appErr.Metadata, tc.status)
				}
			})
		}
	}
}

func TestOtherStatusesAreEngineErrors(t *testing.T) {
	for _, kind := range kinds {
		for _, status := range []int{http.StatusInternalServerError, http.StatusForbidden, http.StatusServiceUnavailable, http.StatusNotImplemented} {
			t.Run(string(kind)+"/"+http.StatusText(status), func(t *testing.T) {
				f, c := engine(t, kind)
				if _, err := c.Negotiate(bg); err != nil {
					t.Fatal(err)
				}
				f.FailNext(http.MethodGet, "/containers/json", status, "the engine says no")
				_, err := c.ListContainers(bg, containerapi.ListContainersOptions{})
				appErr := routeOf(t, err, "Client.ListContainers->Client.call->Client.do.ContainerAPIEngineError")
				if appErr.Message != "the engine says no" || appErr.Metadata["status"] != status {
					t.Errorf("message %q, metadata %v", appErr.Message, appErr.Metadata)
				}
			})
		}
	}
}

func TestAnErrorWithoutJSONKeepsItsText(t *testing.T) {
	f, c := engine(t, containerapi.KindDocker)
	if _, err := c.Negotiate(bg); err != nil {
		t.Fatal(err)
	}
	f.Override(http.MethodGet, "/containers/json", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream is down\n"))
	})
	_, err := c.ListContainers(bg, containerapi.ListContainersOptions{})
	appErr := routeOf(t, err, "Client.do.ContainerAPIEngineError")
	if appErr.Message != "upstream is down" {
		t.Errorf("message = %q", appErr.Message)
	}
}

func TestACancelledCallIsAnErrorThatKeepsTheContextError(t *testing.T) {
	_, c := engine(t, containerapi.KindDocker)
	if _, err := c.Negotiate(bg); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(bg)
	cancel()
	_, err := c.ListContainers(ctx, containerapi.ListContainersOptions{})
	wantRoute(t, err, "Client.ListContainers->Client.call->Client.do.ContainerAPIRequestFailed")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("errors.Is(err, context.Canceled) = false for %v", err)
	}
}

func TestACallThatTimesOut(t *testing.T) {
	f, c := engine(t, containerapi.KindDocker)
	if _, err := c.Negotiate(bg); err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	defer close(release)
	f.Override(http.MethodGet, "/containers/json", func(http.ResponseWriter, *http.Request) { <-release })
	ctx, cancel := context.WithTimeout(bg, 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := c.ListContainers(ctx, containerapi.ListContainersOptions{})
	wantRoute(t, err, "Client.do.ContainerAPIRequestFailed")
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 3*time.Second {
		t.Errorf("err = %v after %v", err, time.Since(start))
	}
}

func TestACallToAnEngineThatWentAway(t *testing.T) {
	f := containerapitest.NewFake(t, containerapi.KindDocker, containerapitest.FakeOptions{})
	c := f.Client()
	defer c.Close()
	if _, err := c.Negotiate(bg); err != nil {
		t.Fatal(err)
	}
	f.Close()
	_, err := c.ListContainers(bg, containerapi.ListContainersOptions{})
	wantRoute(t, err, "Client.do.ContainerAPIConnectFailed")
}

func TestBodiesAreBounded(t *testing.T) {
	f := containerapitest.NewFake(t, containerapi.KindDocker, containerapitest.FakeOptions{})
	c := containerapi.New(containerapi.KindDocker, f.Endpoint(), containerapi.Options{MaxBody: 600})
	defer c.Close()
	f.AddImage(1, "x:1")
	f.Override(http.MethodGet, "/containers/json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[" + strings.Repeat(`{"Id":"aaaaaaaa"},`, 50) + `{"Id":"b"}]`))
	})
	_, err := c.ListContainers(bg, containerapi.ListContainersOptions{})
	appErr := routeOf(t, err, "Client.ListContainers->Client.call.ContainerAPIBodyTooLarge")
	if appErr.Metadata["limit"] != int64(600) {
		t.Errorf("metadata = %v", appErr.Metadata)
	}
	// a body within the limit is fine
	f.Override(http.MethodGet, "/images/json", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`[]`)) })
	if _, err := c.ListImages(bg, containerapi.ListImagesOptions{}); err != nil {
		t.Errorf("a short answer: %v", err)
	}
}

func TestAnAnswerThatCutsOffIsAReadError(t *testing.T) {
	f, c := engine(t, containerapi.KindDocker)
	if _, err := c.Negotiate(bg); err != nil {
		t.Fatal(err)
	}
	f.Override(http.MethodGet, "/containers/json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "1000")
		_, _ = w.Write([]byte(`[{"Id":`))
	})
	_, err := c.ListContainers(bg, containerapi.ListContainersOptions{})
	wantRoute(t, err, "Client.ListContainers->Client.call.ContainerAPIReadFailed")
}

// brokenAnswers: the same call against an engine that answers with something that is not what the call decodes.
func TestAnswersThatCannotBeDecoded(t *testing.T) {
	podman := containerapi.KindPodman
	for _, tc := range []struct {
		name  string
		kind  containerapi.Kind
		path  string
		body  string
		call  func(c *containerapi.Client) error
		route string
	}{
		{"a container list that is not JSON", containerapi.KindDocker, "/containers/json", `nope`,
			func(c *containerapi.Client) error {
				_, err := c.ListContainers(bg, containerapi.ListContainersOptions{})
				return err
			},
			"Client.ListContainers->Client.call.ContainerAPIDecodeFailed"},
		{"a container list that is an object", containerapi.KindDocker, "/containers/json", `{"a":1}`,
			func(c *containerapi.Client) error {
				_, err := c.ListContainers(bg, containerapi.ListContainersOptions{})
				return err
			},
			"Client.ListContainers->Client.call.ContainerAPIDecodeFailed"},
		{"an inspect whose state is a number", containerapi.KindDocker, "/containers/ghost/json", `{"State":5}`,
			func(c *containerapi.Client) error { _, err := c.InspectContainer(bg, "ghost"); return err },
			"Client.InspectContainer.ContainerAPIDecodeFailed"},
		{"an image inspect whose size is text", containerapi.KindDocker, "/images/x:1/json", `{"Size":"big"}`,
			func(c *containerapi.Client) error { _, err := c.InspectImage(bg, "x:1"); return err },
			"Client.InspectImage.ContainerAPIDecodeFailed"},
		{"an image list that is an object", containerapi.KindDocker, "/images/json", `{}`,
			func(c *containerapi.Client) error {
				_, err := c.ListImages(bg, containerapi.ListImagesOptions{})
				return err
			},
			"Client.ListImages->Client.call.ContainerAPIDecodeFailed"},
		{"a removal report that is neither list nor object", containerapi.KindDocker, "/images/x:1", `[1]`,
			func(c *containerapi.Client) error {
				_, err := c.RemoveImage(bg, "x:1", containerapi.RemoveImageOptions{})
				return err
			},
			"Client.RemoveImage.ContainerAPIDecodeFailed"},
		{"a libpod removal report with wrong types", podman, "/images/x:1", `{"Untagged":5}`,
			func(c *containerapi.Client) error {
				_, err := c.RemoveImage(bg, "x:1", containerapi.RemoveImageOptions{})
				return err
			},
			"Client.RemoveImage.ContainerAPIDecodeFailed"},
		{"a history that is an object", containerapi.KindDocker, "/images/x:1/history", `{}`,
			func(c *containerapi.Client) error { _, err := c.ImageHistory(bg, "x:1"); return err },
			"Client.ImageHistory->Client.call.ContainerAPIDecodeFailed"},
		{"a network list with a bad entry", containerapi.KindDocker, "/networks", `[{"Internal":"yes"}]`,
			func(c *containerapi.Client) error { _, err := c.ListNetworks(bg, nil); return err },
			"Client.ListNetworks.ContainerAPIDecodeFailed"},
		{"a network inspect with a bad field", containerapi.KindDocker, "/networks/n", `{"Internal":"yes"}`,
			func(c *containerapi.Client) error { _, err := c.InspectNetwork(bg, "n"); return err },
			"Client.InspectNetwork.ContainerAPIDecodeFailed"},
		{"an older Podman's empty network list", podman, "/networks/n/json", `[]`,
			func(c *containerapi.Client) error { _, err := c.InspectNetwork(bg, "n"); return err },
			"Client.InspectNetwork.ContainerAPIDecodeFailed"},
		{"a volume list with a bad entry", containerapi.KindDocker, "/volumes", `{"Volumes":[{"Labels":5}]}`,
			func(c *containerapi.Client) error { _, err := c.ListVolumes(bg, nil); return err },
			"Client.ListVolumes.ContainerAPIDecodeFailed"},
		{"a volume inspect with a bad field", containerapi.KindDocker, "/volumes/v", `{"Labels":5}`,
			func(c *containerapi.Client) error { _, err := c.InspectVolume(bg, "v"); return err },
			"Client.InspectVolume.ContainerAPIDecodeFailed"},
		{"a volume create answer with a bad field", containerapi.KindDocker, "/volumes/create", `{"Labels":5}`,
			func(c *containerapi.Client) error {
				_, err := c.CreateVolume(bg, containerapi.VolumeSpec{Name: "v"})
				return err
			},
			"Client.CreateVolume.ContainerAPIDecodeFailed"},
		{"a pod list with a bad entry", podman, "/pods/json", `[{"Labels":5}]`,
			func(c *containerapi.Client) error { _, err := c.ListPods(bg, nil); return err },
			"Client.ListPods.ContainerAPIDecodeFailed"},
		{"a pod inspect with a bad field", podman, "/pods/p/json", `{"Labels":5}`,
			func(c *containerapi.Client) error { _, err := c.InspectPod(bg, "p"); return err },
			"Client.InspectPod.ContainerAPIDecodeFailed"},
		{"a disk usage that is a list", containerapi.KindDocker, "/system/df", `[]`,
			func(c *containerapi.Client) error { _, err := c.DiskUsage(bg); return err },
			"Client.DiskUsage->Client.call.ContainerAPIDecodeFailed"},
		{"a container prune report that is not a report", containerapi.KindDocker, "/containers/prune", `[1]`,
			func(c *containerapi.Client) error { _, err := c.PruneContainers(bg, nil); return err },
			"Client.PruneContainers->decodePrune.ContainerAPIDecodeFailed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, c := engine(t, tc.kind)
			if _, err := c.Negotiate(bg); err != nil {
				t.Fatal(err)
			}
			method := http.MethodGet
			if strings.HasSuffix(tc.path, "/create") || strings.HasSuffix(tc.path, "/prune") {
				method = http.MethodPost
			}
			if strings.HasPrefix(tc.name, "a removal") || strings.HasPrefix(tc.name, "a libpod removal") {
				method = http.MethodDelete
			}
			f.Override(method, tc.path, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			})
			wantRoute(t, tc.call(c), tc.route)
		})
	}
}

func TestPodsOnDockerAreRefused(t *testing.T) {
	f, c := engine(t, containerapi.KindDocker)
	if _, err := c.Negotiate(bg); err != nil {
		t.Fatal(err)
	}
	f.ResetRequests()
	for name, call := range map[string]func() error{
		"ListPods":   func() error { _, err := c.ListPods(bg, nil); return err },
		"InspectPod": func() error { _, err := c.InspectPod(bg, "p"); return err },
		"CreatePod":  func() error { _, err := c.CreatePod(bg, containerapi.PodSpec{Name: "p"}); return err },
		"PodDo":      func() error { return c.PodDo(bg, "p", containerapi.PodStart) },
		"StopPod":    func() error { return c.StopPod(bg, "p", 0) },
		"RemovePod":  func() error { return c.RemovePod(bg, "p", true) },
		"PrunePods":  func() error { _, err := c.PrunePods(bg); return err },
	} {
		wantRoute(t, call(), "Client."+name+"->Client.requirePodman.ContainerAPINotSupported")
	}
	if n := len(f.Requests()); n != 0 {
		t.Errorf("%d requests were sent to the engine", n)
	}
}

func TestAClientForANonexistentSocket(t *testing.T) {
	c := containerapi.New(containerapi.KindPodman, containerapi.UnixEndpoint(filepath.Join(shortSocketDir(t), "none.sock")), containerapi.Options{})
	defer c.Close()
	_, err := c.ListContainers(bg, containerapi.ListContainersOptions{})
	wantRoute(t, err, "Client.ListContainers->Client.call->Client.do->Client.prefix->Client.Negotiate->Client.do.ContainerAPIConnectFailed")
}
