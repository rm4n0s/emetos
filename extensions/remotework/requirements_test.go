package remotework

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/keyqr"
)

func TestTheRequirementsOfRemoteWork(t *testing.T) {
	m := FactoryOf(NewHub(HubOptions{Camera: keyqr.NewFakeSource()}))().Manifest()
	if err := extension.CheckManifest(m); err != nil {
		t.Fatal(err)
	}
	if len(m.Requirements) != 2 || m.Requirements[0].ID != RequirementCamera || m.Requirements[1].ID != RequirementSSHAgent {
		t.Fatalf("requirements %+v", m.Requirements)
	}
	if (&remoteWork{}).Manifest().Requirements == nil {
		t.Error("an instance with no hub declares nothing")
	}
}

func TestTheRequirementOfTheCamera(t *testing.T) {
	denied := keyqr.NewFakeSource()
	denied.SetListError(errors.New("KeyQRCameraDenied", "this user may not open the cameras (is it in the video group?)"))
	tests := []struct {
		name   string
		source keyqr.Source
		goos   string
		state  extension.RequirementState
		detail string
	}{
		{"a camera", keyqr.NewFakeSource(keyqr.FakeCamera{Name: "Integrated Camera"}, keyqr.FakeCamera{Name: "USB Camera"}), "linux", extension.RequirementFound, "Integrated Camera, USB Camera"},
		{"no camera", keyqr.NewFakeSource(), "linux", extension.RequirementMissing, "no camera that captures video was found"},
		{"cameras this user may not open", denied, "linux", extension.RequirementUnusable, "this user may not open the cameras (is it in the video group?)"},
		{"a Mac", keyqr.NewFakeSource(keyqr.FakeCamera{Name: "FaceTime"}), "darwin", extension.RequirementUnknown, "the cameras are read through V4L2, on Linux only; paste or type the key instead"},
		{"no source", nil, "linux", extension.RequirementUnknown, "this build has no camera source"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checkCamera(context.Background(), tt.source, tt.goos)
			if got.State != tt.state || got.Detail != tt.detail {
				t.Errorf("got %+v", got)
			}
			if f, ok := tt.source.(*keyqr.FakeSource); ok && f.Opened() != 0 {
				t.Error("the check opened a camera")
			}
		})
	}
}

func TestTheRequirementOfTheAgent(t *testing.T) {
	one := fakeAgent(t, newKey(t))
	tests := []struct {
		name   string
		socket string
		state  extension.RequirementState
		detail string
	}{
		{"an agent with a key", one, extension.RequirementFound, "1 key"},
		{"an empty agent", fakeAgent(t), extension.RequirementFound, "0 keys"},
		{"no SSH_AUTH_SOCK", "", extension.RequirementMissing, "SSH_AUTH_SOCK is not set: no ssh-agent runs for this session"},
		{"a socket nobody listens on", filepath.Join(t.TempDir(), "gone.sock"), extension.RequirementUnusable, "the agent at SSH_AUTH_SOCK does not answer"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := checkAgent(context.Background(), tt.socket); got.State != tt.state || got.Detail != tt.detail {
				t.Errorf("got %+v", got)
			}
		})
	}
}
