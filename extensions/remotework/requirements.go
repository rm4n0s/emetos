package remotework

import (
	"context"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/keyqr"
	"github.com/rm4n0s/emetos/extensions/internal/sshlogin"
)

// What Remote Work needs from the system (SPEC §10.36, step 34.3): a camera, for Scan QR Code, and the ssh-agent, for logging in with a
// key it holds. Both are optional: a key can be typed or read from a file. The camera is only asked what it is (Cameras opens each device
// for a moment and streams nothing); the agent is only asked for its public keys (nothing is signed).

// The ids of the requirements of the extension.
const (
	RequirementCamera   = "camera"
	RequirementSSHAgent = "ssh-agent"
)

// requirements are the requirements of the manifest; camera is the Source of the hub (nil: none).
func requirements(camera keyqr.Source) []extension.Requirement {
	return []extension.Requirement{
		{
			ID: RequirementCamera, Name: "Camera", Kind: extension.RequirementDevice, Optional: true,
			Purpose: "Scan QR Code reads the public key of another computer from its screen",
			Install: extension.Install{Docs: "https://www.kernel.org/doc/html/latest/userspace-api/media/v4l/v4l2.html"},
			Check: func(ctx context.Context, _ extension.RequirementEnv) extension.RequirementResult {
				return checkCamera(ctx, camera, runtime.GOOS)
			},
		},
		{
			ID: RequirementSSHAgent, Name: "ssh-agent", Kind: extension.RequirementService, Optional: true,
			Purpose: "logs in to a hosted workspace with a key the agent holds (SSH_AUTH_SOCK)",
			Install: extension.Install{Docs: "https://man.openbsd.org/ssh-agent", Commands: []extension.InstallCommand{
				{Family: extension.FamilyFedora, Command: "sudo dnf install openssh-clients"},
				{Family: extension.FamilyDebian, Command: "sudo apt install openssh-client"},
				{Family: extension.FamilyArch, Command: "sudo pacman -S openssh"},
				{Family: extension.FamilySuse, Command: "sudo zypper install openssh-clients"},
			}},
			Check: func(ctx context.Context, _ extension.RequirementEnv) extension.RequirementResult {
				return checkAgent(ctx, sshlogin.AgentSocket(os.Getenv))
			},
		},
	}
}

// checkCamera is the check of the camera. goos is runtime.GOOS: the cameras are read through V4L2, on Linux only.
func checkCamera(ctx context.Context, camera keyqr.Source, goos string) extension.RequirementResult {
	if goos != "linux" {
		return extension.RequirementResult{State: extension.RequirementUnknown, Detail: "the cameras are read through V4L2, on Linux only; paste or type the key instead"}
	}
	if camera == nil {
		return extension.RequirementResult{State: extension.RequirementUnknown, Detail: "this build has no camera source"}
	}
	cams, err := camera.Cameras(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return extension.RequirementResult{State: extension.RequirementUnknown, Detail: "the check was stopped"}
		}
		if appErr, ok := errors.FromError(err); ok && appErr.Tag == "KeyQRCameraDenied" {
			return extension.RequirementResult{State: extension.RequirementUnusable, Detail: appErr.Message}
		}
		return extension.RequirementResult{State: extension.RequirementUnknown, Detail: "the cameras could not be listed: " + err.Error()}
	}
	if len(cams) == 0 {
		return extension.RequirementResult{State: extension.RequirementMissing, Detail: "no camera that captures video was found"}
	}
	names := make([]string, len(cams))
	for i, c := range cams {
		names[i] = c.Name
	}
	return extension.RequirementResult{State: extension.RequirementFound, Path: cams[0].ID, Detail: strings.Join(names, ", ")}
}

// checkAgent is the check of the ssh-agent at socket.
func checkAgent(ctx context.Context, socket string) extension.RequirementResult {
	if socket == "" {
		return extension.RequirementResult{State: extension.RequirementMissing, Detail: "SSH_AUTH_SOCK is not set: no ssh-agent runs for this session"}
	}
	keys, err := sshlogin.AgentKeys(ctx, socket)
	if err != nil {
		if ctx.Err() != nil {
			return extension.RequirementResult{State: extension.RequirementUnknown, Detail: "the check was stopped"}
		}
		return extension.RequirementResult{State: extension.RequirementUnusable, Path: socket, Detail: "the agent at SSH_AUTH_SOCK does not answer"}
	}
	detail := strconv.Itoa(len(keys)) + " keys"
	if len(keys) == 1 {
		detail = "1 key"
	}
	return extension.RequirementResult{State: extension.RequirementFound, Path: socket, Detail: detail}
}
