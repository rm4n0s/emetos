// Package containerapi is the one client of the Docker Engine API and of Podman's libpod API that the Docker and Podman
// extensions share (SPEC §10.20 "The engine client", D22). Both engines are spoken to over HTTP on a Unix socket, or on
// tcp:// with TLS, with the standard library only: no Docker SDK and no new module.
//
// A Client is made for one Kind of engine and one Endpoint. It negotiates the API version with /_ping and /version on its
// first call (Docker: at least 1.41; Podman: at least version 4, whose libpod paths are /v4.0.0/libpod/...), and it turns the
// two engines' different JSON into the same types (Container, Image, Network, Volume, Pod, Event, DiskUsage), so the views
// above it do not know which engine answers. The calls are the ones of SPEC §10.20: containers, images (with pull progress),
// networks, volumes, pods (Podman), the event stream, logs (the multiplexed stream split into stdout and stderr), exec
// (output captured, not interactive) and "system df".
//
// Every call that does I/O takes a ctx as its first parameter. A stream (Events, Logs, PullImage) ends when its ctx is
// cancelled and then returns nil, because the caller asked for it. Response bodies are limited (Options.MaxBody, and
// MaxFrame for the log stream), and a failure keeps the engine's own message. All errors have a tag that starts with
// ContainerAPI.
//
// Find looks for the engine to use: the setting, the environment, Docker's contexts and the default sockets; for Podman it
// starts a "podman system service" of its own on a private socket when none answers (StartService), stopped with Close.
//
// The package containerapitest has a fake engine that speaks both APIs, for the default tests, and a harness for tests
// against the user's real Docker and Podman that touches only things it created and labelled.
package containerapi
