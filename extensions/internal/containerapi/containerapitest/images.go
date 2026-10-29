package containerapitest

// The images the tests against the real engines may pull, in one place (CLAUDE.md "Testing notes": only pinned image tags
// listed in one place). Each is a full reference with a version tag that does not move in practice, never "latest". The
// harness refuses to pull anything that is not listed here.
const (
	// ImageBusyBox is the small image the real-engine tests run containers from (a few megabytes). The tag is BusyBox 1.37.0.
	ImageBusyBox = "docker.io/library/busybox:1.37.0"
)

// PinnedImages lists every image the real-engine tests may pull.
var PinnedImages = []string{ImageBusyBox}

// IsPinned reports whether ref is one of PinnedImages.
func IsPinned(ref string) bool {
	for _, p := range PinnedImages {
		if p == ref {
			return true
		}
	}
	return false
}
