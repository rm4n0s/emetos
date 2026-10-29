// Package podman is the Podman extension (SPEC §10.20, D22): the side bar view, the actions and the logs of the Docker extension, on
// Podman's libpod API, and what only Podman has: pods, connections, Kube YAML and Quadlet units.
//
// The views, the actions and the engine client are shared with the Docker extension: this package is the Config of Podman
// (extensions/internal/containerui) and nothing else. The commands are podman.* (podman.containers.start, podman.pods.start, ...), the
// setting podman.host names the engine, and the topic emetos.podman.changed carries what the engine reported ({kind, id, action}).
// When no socket answers, the engine client starts a `podman system service` of its own (containerapi.StartService), stopped when the
// extension ends.
//
// Ported from vscode-containers: see extensions/internal/containerui.
package podman

import (
	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
	"github.com/rm4n0s/emetos/extensions/internal/containerui"
)

// ID is the id of the extension.
const ID = "emetos.podman"

// FirstRunNote is what the view says to a user who has not seen it. Podman runs containers as the user (rootless), which is not
// root; a socket of the root's Podman is.
const FirstRunNote = "Podman runs containers as your user; a rootful Podman socket is power over this computer. " +
	"Emetos asks before it removes anything."

// Config is the configuration of the Podman extension. A test changes the Dial of a copy of it to point at a fake engine.
func Config() containerui.Config {
	return containerui.Config{
		Kind: containerapi.KindPodman, ID: ID, Name: "Podman", Engine: "Podman",
		Description: "Containers, pods, images, networks and volumes of Podman, with their logs, Kube YAML and Quadlet units",
		Prefix:      "podman", Icon: "server-environment", Order: 61, Pods: true, Connections: true, MenuTitlePrefix: "Podman: ",
		FirstRunNote: FirstRunNote,
	}
}

// New is the Factory of the extension.
func New() extension.Extension { return containerui.New(Config()) }
