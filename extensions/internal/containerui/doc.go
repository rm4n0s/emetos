// Package containerui is the part of the Docker and Podman extensions that is the same for both (SPEC §10.20, step 18.2): the
// side bar view with its sections (Containers grouped by Compose project, Images by repository, Networks, Volumes and, for Docker,
// Contexts), the actions of the items, the logs in a panel tab, the documents (inspect, history, files of a container) in editor
// tabs, the form that runs an image, the confirmations before anything is removed, and the view that says an engine is missing.
// It talks to the engine through containerapi and to Emetos through the extension API only.
//
// An extension is made of a Config: New(cfg) is its extension.Extension. extensions/containers (Docker) is the first user;
// Podman (step 18.4) is the second and adds its Pods and Connections through the same Config.
//
// Ported from vscode-containers (Container Tools, MIT): the view containers and the order of the sections
// (package.json contributes.views), the titles and ids of the commands (contributes.commands, with the prefix of the extension
// in place of vscode-containers), the menus of the items (menus view/item/context and view/title), the labels, descriptions and
// state icons of the tree items (src/tree/containers, images, networks, volumes and contexts) and the texts of the
// confirmations (src/commands). Not built: the registries view, the properties that configure the explorer, the tooltips, the
// outdated-image check, Azure, Copilot and the walkthrough.
//
// The threads: Activate, the command handlers, the callbacks of the API and Build run on the UI thread, where every field of
// the extension is used. The goroutines (the connection, the lists, the event stream, the logs, exec, pulls) hand their results to
// the UI thread with API.Post.
package containerui
