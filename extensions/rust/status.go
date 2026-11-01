package rust

import (
	"encoding/json"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/langserver"
)

// The status of rust-analyzer in the status item (SPEC §10.34 "Rust"): the server tells how it does with the notification
// experimental/serverStatus, which it sends because the client asked for it in its capabilities (clientExperimental).

// statusMethod is the notification.
const statusMethod = "experimental/serverStatus"

// serverStatus follows the notification: health is ok, warning or error, quiescent says that the server is idle, and message says what
// is wrong ("Failed to discover workspace").
func (r *rust) serverStatus(params json.RawMessage) {
	var p struct {
		Health    string `json:"health"`
		Quiescent bool   `json:"quiescent"`
		Message   string `json:"message"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		r.api.Log().Warn("the status of rust-analyzer cannot be read", "err", err)
		return
	}
	if r.ctl != nil {
		r.ctl.SetHealth(langserver.Health{Level: p.Health, Busy: !p.Quiescent, Message: p.Message})
	}
	// the runnables are known once the workspace is loaded
	if p.Quiescent && !r.quiescent {
		r.api.Languages().RefreshCodeLenses()
	}
	r.quiescent = p.Quiescent
}

// serverChanged asks the editors for the lenses again when the server starts to run: the lenses come from it. A restart forgets that the
// server was quiescent.
func (r *rust) serverChanged(st extension.ServerStatus) {
	running := st.State == extension.ServerRunning
	if running && !r.wasRunning {
		r.api.Languages().RefreshCodeLenses()
	}
	if !running {
		r.quiescent = false
	}
	r.wasRunning = running
}
