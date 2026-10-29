package containerapi

import (
	"encoding/json"
	"net/url"
	"strings"
	"time"
)

// Kind is the engine a Client speaks to.
type Kind string

// The two engines.
const (
	// KindDocker is the Docker Engine API (and anything that speaks it).
	KindDocker Kind = "docker"
	// KindPodman is Podman's libpod API.
	KindPodman Kind = "podman"
)

// LabelCompose is the label that Compose puts on the containers of a project, and LabelComposeService the one with the
// name of the service.
const (
	LabelCompose        = "com.docker.compose.project"
	LabelComposeService = "com.docker.compose.service"
)

// LabelPodmanCompose is the label that podman-compose puts on the containers of a project, next to the one of Docker's Compose when
// its version sets that too (SPEC §10.20 "Podman only").
const LabelPodmanCompose = "io.podman.compose.project"

// Filters are the filters of a list or prune call, as the engines take them: a name and the values that match, for
// example {"label": {"dev.emetos.test=1"}}.
type Filters map[string][]string

// encode returns the filters as the JSON the engines take in the "filters" parameter, or "" when there are none.
func (f Filters) encode() string {
	if len(f) == 0 {
		return ""
	}
	b, err := json.Marshal(map[string][]string(f))
	if err != nil {
		return "" // a map of strings always encodes
	}
	return string(b)
}

// addTo sets the "filters" parameter of q when there are filters.
func (f Filters) addTo(q url.Values) {
	if s := f.encode(); s != "" {
		q.Set("filters", s)
	}
}

// Port is a port a container publishes or exposes.
type Port struct {
	// IP is the host address the port is published on; empty when it is only exposed.
	IP string
	// PrivatePort is the port in the container, PublicPort the one on the host (0 when only exposed).
	PrivatePort, PublicPort uint16
	// Type is "tcp", "udp" or "sctp".
	Type string
}

// Container is one row of the container list.
type Container struct {
	// ID is the full container id.
	ID string
	// Names are the names of the container, without the leading slash Docker adds.
	Names []string
	// Image is the image the container was made from, ImageID its id.
	Image, ImageID string
	// Command is the command the container runs, as one line.
	Command string
	// Created is when the container was made. StartedAt is when it last started; Docker's list does not say, so it is
	// the zero time there.
	Created, StartedAt time.Time
	// State is one of created, running, paused, restarting, removing, exited, dead and stopped; Status is the engine's
	// sentence ("Up 2 hours"; empty from Podman).
	State, Status string
	// Ports are the published and exposed ports.
	Ports []Port
	// Labels are the labels of the container.
	Labels map[string]string
	// Pod and PodName are the id and the name of the pod of the container (Podman; empty otherwise).
	Pod, PodName string
	// Networks are the names of the networks the container is connected to.
	Networks []string
}

// Name returns the first name of the container, or its short id when it has none.
func (c Container) Name() string {
	if len(c.Names) > 0 {
		return c.Names[0]
	}
	return ShortID(c.ID)
}

// ComposeProject returns the Compose project of the container, or "" when it was not made by Compose (Docker's label, else the one
// of podman-compose).
func (c Container) ComposeProject() string {
	if p := c.Labels[LabelCompose]; p != "" {
		return p
	}
	return c.Labels[LabelPodmanCompose]
}

// Running reports whether the container is running (or paused or restarting, which still hold a process).
func (c Container) Running() bool {
	switch c.State {
	case "running", "paused", "restarting":
		return true
	}
	return false
}

// ShortID returns the first 12 characters of an id, without a "sha256:" prefix.
func ShortID(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// PortBinding is a published port of a container as inspect reports it.
type PortBinding struct {
	// HostIP and HostPort are where the port is published on the host.
	HostIP, HostPort string
}

// ContainerState is the state part of an inspected container.
type ContainerState struct {
	// Status is created, running, paused, restarting, removing, exited or dead.
	Status string
	// Running, Paused, Restarting, OOMKilled and Dead are the flags of the state.
	Running, Paused, Restarting, OOMKilled, Dead bool
	// Pid is the process id on the host (0 when stopped).
	Pid int
	// ExitCode is the code the last run ended with.
	ExitCode int
	// Error is the engine's error text of the last start.
	Error string
	// StartedAt and FinishedAt are the times of the last start and stop; the zero time when it never did.
	StartedAt, FinishedAt time.Time
	// Health is the status of the health check ("healthy", "unhealthy", "starting"), empty without one.
	Health string
}

// Mount is a mount of a container.
type Mount struct {
	// Type is bind, volume or tmpfs; Name is the volume's name, Source the path on the host, Destination the path in the
	// container.
	Type, Name, Source, Destination string
	// RW reports whether the mount is writable.
	RW bool
}

// ContainerInspect is the part of an inspected container that the views use, with the whole JSON in Raw.
type ContainerInspect struct {
	// ID is the full id, Name the name without the slash, Image the image id and ImageName the name the container was
	// made from.
	ID, Name, Image, ImageName string
	// Created is when the container was made.
	Created time.Time
	// State is the state.
	State ContainerState
	// Env, Cmd, Entrypoint, WorkingDir, Hostname, User, Labels and Tty are the container's configuration.
	Env, Cmd, Entrypoint []string
	WorkingDir, Hostname string
	User                 string
	Labels               map[string]string
	Tty                  bool
	// Ports are the published ports, keyed by "80/tcp".
	Ports map[string][]PortBinding
	// Networks are the names of the networks the container is connected to.
	Networks []string
	// Mounts are the mounts.
	Mounts []Mount
	// Pod is the id of the pod of the container (Podman).
	Pod string
	// Raw is the JSON the engine sent, for the Inspect action.
	Raw json.RawMessage
}

// PortMapping publishes a port of a container.
type PortMapping struct {
	// HostIP is the address on the host; empty is every interface (the test harness insists on 127.0.0.1).
	HostIP string
	// HostPort is the port on the host; 0 lets the engine pick one.
	HostPort uint16
	// ContainerPort is the port in the container.
	ContainerPort uint16
	// Protocol is "tcp" (the default) or "udp".
	Protocol string
}

// ContainerSpec says what CreateContainer makes.
type ContainerSpec struct {
	// Name is the container's name; empty lets the engine pick one.
	Name string
	// Image is the image to run; it has to be there already, create does not pull.
	Image string
	// Cmd and Entrypoint replace the image's command and entrypoint when set.
	Cmd, Entrypoint []string
	// Env is the environment as "NAME=value".
	Env []string
	// Labels are put on the container.
	Labels map[string]string
	// Ports are published.
	Ports []PortMapping
	// Binds are the mounts as "source:destination[:options]": a source that starts with "/" or "." is a path on the
	// host, any other source is the name of a volume.
	Binds []string
	// AutoRemove removes the container when it stops (docker run --rm).
	AutoRemove bool
	// Network is the network to connect to; empty is the engine's default.
	Network string
	// Pod is the pod to put the container in (Podman only).
	Pod string
	// WorkingDir and User override the image's.
	WorkingDir, User string
	// Tty gives the container a terminal.
	Tty bool
}

// CreateResult is what CreateContainer returns.
type CreateResult struct {
	// ID is the id of the new container.
	ID string
	// Warnings are the engine's warnings.
	Warnings []string
}

// ListContainersOptions are the options of ListContainers.
type ListContainersOptions struct {
	// All lists stopped containers too.
	All bool
	// Filters are the engine's filters.
	Filters Filters
}

// RemoveOptions are the options of the remove calls.
type RemoveOptions struct {
	// Force removes a running container (kills it first).
	Force bool
	// Volumes removes the anonymous volumes of the container too.
	Volumes bool
}

// PruneResult is what a prune call reports.
type PruneResult struct {
	// Deleted are the ids or names of what went.
	Deleted []string
	// SpaceReclaimed is the number of bytes freed, when the engine says.
	SpaceReclaimed int64
}

// Image is one row of the image list.
type Image struct {
	// ID is the image id, with the "sha256:" prefix when the engine gives it.
	ID string
	// RepoTags are the names of the image ("repository:tag"); empty for a dangling image.
	RepoTags []string
	// RepoDigests are the digests of the image ("repository@sha256:...").
	RepoDigests []string
	// Created is when the image was built.
	Created time.Time
	// Size is the size in bytes.
	Size int64
	// Containers is the number of containers that use the image; -1 when the engine did not count.
	Containers int
	// Labels are the labels of the image.
	Labels map[string]string
	// Dangling reports an image with no name.
	Dangling bool
}

// ListImagesOptions are the options of ListImages.
type ListImagesOptions struct {
	// All lists the intermediate images too.
	All bool
	// Filters are the engine's filters.
	Filters Filters
}

// ImageInspect is the part of an inspected image that the views use, with the whole JSON in Raw.
type ImageInspect struct {
	// ID is the image id, RepoTags and RepoDigests its names.
	ID                    string
	RepoTags, RepoDigests []string
	// Created is when the image was built, Size its size in bytes.
	Created time.Time
	Size    int64
	// Architecture and OS are the platform of the image.
	Architecture, OS string
	// Env, Cmd, Entrypoint, WorkingDir, User, Labels and ExposedPorts are the image's configuration.
	Env, Cmd, Entrypoint []string
	WorkingDir, User     string
	Labels               map[string]string
	ExposedPorts         []string
	// Raw is the JSON the engine sent.
	Raw json.RawMessage
}

// HistoryEntry is a layer of an image's history.
type HistoryEntry struct {
	// ID is the id of the layer ("<missing>" for one that is not kept).
	ID string
	// Created is when the layer was made.
	Created time.Time
	// CreatedBy is the command that made it.
	CreatedBy string
	// Tags are the names of the image at this layer.
	Tags []string
	// Size is the size of the layer in bytes.
	Size int64
	// Comment is the comment of the layer.
	Comment string
}

// RemoveImageOptions are the options of RemoveImage.
type RemoveImageOptions struct {
	// Force removes the image even when containers use it or it has several names.
	Force bool
	// NoPrune keeps the untagged parents (Docker).
	NoPrune bool
}

// RemoveImageResult says what RemoveImage removed.
type RemoveImageResult struct {
	// Untagged are the names that were taken off, Deleted the image ids that went.
	Untagged, Deleted []string
}

// PruneImagesOptions are the options of PruneImages.
type PruneImagesOptions struct {
	// All removes every unused image, not only the dangling ones.
	All bool
	// Filters are the engine's filters.
	Filters Filters
}

// PullProgress is one line of the progress of a pull.
type PullProgress struct {
	// ID is the layer the line is about (empty for the whole image).
	ID string
	// Status is the text of the line ("Downloading", "Pull complete", ...).
	Status string
	// Current and Total are bytes, when the line has them.
	Current, Total int64
}

// Subnet is a subnet of a network.
type Subnet struct {
	// Subnet is the range in CIDR form and Gateway the gateway address.
	Subnet, Gateway string
}

// Network is a network.
type Network struct {
	// ID and Name identify the network.
	ID, Name string
	// Driver is bridge, host, overlay, macvlan, ...; Scope is local or swarm.
	Driver, Scope string
	// Created is when the network was made.
	Created time.Time
	// Internal reports a network with no outside access, IPv6 one with IPv6.
	Internal, IPv6 bool
	// Subnets are the address ranges.
	Subnets []Subnet
	// Labels are the labels of the network.
	Labels map[string]string
	// Raw is the JSON the engine sent for this network.
	Raw json.RawMessage
}

// NetworkSpec says what CreateNetwork makes.
type NetworkSpec struct {
	// Name is the name of the network.
	Name string
	// Driver is the driver; empty is bridge.
	Driver string
	// Labels are put on the network.
	Labels map[string]string
	// Internal makes a network with no outside access.
	Internal bool
	// IPv6 turns IPv6 on.
	IPv6 bool
}

// Volume is a volume.
type Volume struct {
	// Name identifies the volume.
	Name string
	// Driver is the volume driver, Mountpoint the path on the host and Scope local or global.
	Driver, Mountpoint, Scope string
	// Created is when the volume was made.
	Created time.Time
	// Labels and Options are the volume's.
	Labels, Options map[string]string
	// Size is the bytes the volume uses when the engine counted them (Docker, with "system df"), else -1.
	Size int64
	// Raw is the JSON the engine sent for this volume.
	Raw json.RawMessage
}

// VolumeSpec says what CreateVolume makes.
type VolumeSpec struct {
	// Name is the name of the volume; empty lets the engine pick one.
	Name string
	// Driver is the driver; empty is local.
	Driver string
	// Labels are put on the volume.
	Labels map[string]string
}

// PodContainer is a container of a pod.
type PodContainer struct {
	// ID, Name and Status identify the container and say how it is.
	ID, Name, Status string
}

// Pod is a Podman pod.
type Pod struct {
	// ID and Name identify the pod.
	ID, Name string
	// Status is Created, Running, Stopped, Exited, Paused or Degraded.
	Status string
	// Created is when the pod was made.
	Created time.Time
	// InfraID is the id of the pod's infra container.
	InfraID string
	// Labels are the labels of the pod.
	Labels map[string]string
	// Containers are the containers of the pod, the infra container among them.
	Containers []PodContainer
	// Raw is the JSON the engine sent for this pod.
	Raw json.RawMessage
}

// PodSpec says what CreatePod makes.
type PodSpec struct {
	// Name is the name of the pod; empty lets Podman pick one.
	Name string
	// Labels are put on the pod.
	Labels map[string]string
	// Ports are published by the pod: the containers of a pod share one network, so a container cannot publish a port of its own.
	Ports []PortMapping
}

// Event is an event of the engine's event stream.
type Event struct {
	// Type is container, image, network, volume, pod, daemon, ...; Action is start, die, create, pull, ...
	Type, Action string
	// ID is the id (or name) of what the event is about.
	ID string
	// Attributes are the actor's attributes: its name, its image and its labels.
	Attributes map[string]string
	// Scope is local or swarm.
	Scope string
	// Time is when it happened.
	Time time.Time
}

// ServerInfo is what the engine says about itself.
type ServerInfo struct {
	// Kind is the engine's kind.
	Kind Kind
	// Engine is the engine's name ("Docker Engine - Community", "Podman Engine").
	Engine string
	// Version is the engine's own version ("29.9.2", "5.8.9").
	Version string
	// ServerAPIVersion is the newest API version the engine speaks, MinAPIVersion the oldest.
	ServerAPIVersion, MinAPIVersion string
	// APIVersion is the version the Client uses in its paths: the newest both sides speak ("1.47"; "4.0.0" for libpod).
	APIVersion string
	// OS and Arch are the engine's platform.
	OS, Arch string
}

// Usage is the disk usage of one kind of object.
type Usage struct {
	// Count is how many there are, Active how many are in use, Size the bytes they use and Reclaimable the bytes that
	// removing the unused ones would free.
	Count, Active int
	Size          int64
	Reclaimable   int64
}

// DiskUsage is the result of "system df".
type DiskUsage struct {
	// Images, Containers, Volumes and BuildCache are the usage of each kind (the build cache is Docker's).
	Images, Containers, Volumes, BuildCache Usage
	// ImageSizes and VolumeSizes are the bytes of each image (by id) and volume (by name) where the engine says, for the
	// confirmation of a remove.
	ImageSizes, VolumeSizes map[string]int64
}

// Total returns the bytes of every kind together.
func (d DiskUsage) Total() int64 {
	return d.Images.Size + d.Containers.Size + d.Volumes.Size + d.BuildCache.Size
}
