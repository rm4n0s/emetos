package containerapi_test

import (
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
)

// ParseInspect reads Docker's and Podman's answers the way InspectContainer does, keeping the JSON.
func TestParseInspect(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want containerapi.ContainerInspect
	}{
		{"docker", `{"Id":"abc","Name":"/web","State":{"Running":true},"Config":{"Env":["A=1"],"Image":"nginx:1.27"},"NetworkSettings":{"Ports":{"80/tcp":[{"HostIp":"127.0.0.1","HostPort":"8080"}]}}}`,
			containerapi.ContainerInspect{ID: "abc", Name: "web", ImageName: "nginx:1.27", Env: []string{"A=1"}}},
		{"podman", `{"Id":"abc","Name":"web","ImageName":"docker.io/library/nginx:1.27","State":{"Running":false},"Config":{"Image":"ignored"}}`,
			containerapi.ContainerInspect{ID: "abc", Name: "web", ImageName: "docker.io/library/nginx:1.27"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := containerapi.ParseInspect([]byte(tt.raw))
			if err != nil {
				t.Fatal(err)
			}
			if got.ID != tt.want.ID || got.Name != tt.want.Name || got.ImageName != tt.want.ImageName || len(got.Env) != len(tt.want.Env) || string(got.Raw) != tt.raw {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParseInspectFails(t *testing.T) {
	_, err := containerapi.ParseInspect([]byte(`{"State":"running"}`))
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T", err)
	}
	if !appErr.HasRoute("ParseInspect.ContainerAPIInspectInvalid") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}
