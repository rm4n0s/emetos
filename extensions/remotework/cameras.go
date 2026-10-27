package remotework

import "github.com/rm4n0s/emetos/extensions/internal/keyqr"

// Cameras is the Source that Scan QR Code reads, or nil when the hub has none (HubOptions.Camera).
func (h *Hub) Cameras() keyqr.Source {
	h.camMu.Lock()
	defer h.camMu.Unlock()
	return h.camera
}

// SetCameras replaces the Source of the cameras. The dev command dev.keyqr.fakeCamera uses it to put a camera that plays a picture
// of a QR code in place of the real ones (debug builds only), and the tests use it. Dialogs that are open keep the Source they
// were made with.
func (h *Hub) SetCameras(src keyqr.Source) {
	h.camMu.Lock()
	h.camera = src
	h.camMu.Unlock()
}
