// Package containers is the Docker extension (SPEC §10.20, D22): a side bar view of the containers, images, networks, volumes
// and Docker contexts of the engine, with the actions of VS Code's Container Tools (microsoft/vscode-containers, MIT) on them,
// the logs of a container in the panel, and the confirmations that come before anything is removed.
//
// The views, the actions and the engine client are shared with the Podman extension: this package is the Config of Docker
// (extensions/internal/containerui) and nothing else. The commands are containers.* (containers.containers.start, ...), the
// setting containers.host names the engine, and the topic emetos.containers.changed carries what the engine reported
// ({kind, id, action}).
//
// Ported from vscode-containers: see extensions/internal/containerui.
package containers

import (
	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/containerapi"
	"github.com/rm4n0s/emetos/extensions/internal/containerui"
)

// ID is the id of the extension.
const ID = "emetos.containers"

// FirstRunNote is what the view says to a user who has not seen it: access to the Docker socket is power over the computer.
const FirstRunNote = "Access to Docker is power over this computer: whoever can use its socket controls it as root. " +
	"Emetos asks before it removes anything."

// Config is the configuration of the Docker extension. A test changes the Dial of a copy of it to point at a fake engine.
func Config() containerui.Config {
	return containerui.Config{
		Kind: containerapi.KindDocker, ID: ID, Name: "Containers", Engine: "Docker",
		Description: "Containers, images, networks, volumes and contexts of Docker, with their logs and actions",
		Prefix:      "containers", Icon: "package", Order: 60, Contexts: true, FirstRunNote: FirstRunNote,
	}
}

// New is the Factory of the extension.
func New() extension.Extension { return containerui.New(Config()) }
