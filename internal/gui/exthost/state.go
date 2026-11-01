package exthost

import (
	"encoding/json"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// maxStateValue is the largest value State().Set accepts, in bytes of JSON (storage.MaxExtensionValue; the exthost package does not
// import the storage).
const maxStateValue = 64 * 1024

// stateAPI is the extension.State of an activation: the values are kept by the scope's StateGet and StateSet under the id of the
// extension and the folder of the scope.
type stateAPI struct{ inst *instance }

var _ extension.State = stateAPI{}

// State implements extension.API.
func (a *api) State() extension.State { return stateAPI{a.inst} }

// Get implements extension.State.
func (s stateAPI) Get(key string) (json.RawMessage, bool) {
	in := s.inst
	if in.isDead() || in.scope.opts.StateGet == nil || key == "" {
		return nil, false
	}
	return in.scope.opts.StateGet(in.id, in.scope.opts.Info.Folder, key)
}

// Set implements extension.State.
func (s stateAPI) Set(key string, value any) error {
	in := s.inst
	if in.isDead() {
		return errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	if in.scope.opts.StateSet == nil {
		return errors.New("ExtensionStateUnavailable", "the scope keeps no state", "extension", in.id)
	}
	if key == "" {
		return errors.New("ExtensionStateInvalid", "a kept value needs a key", "extension", in.id)
	}
	var raw json.RawMessage
	if value != nil {
		b, err := json.Marshal(value)
		if err != nil {
			return errors.NewErr("ExtensionStateInvalid", err).SetMetadata("extension", in.id, "key", key)
		}
		if len(b) > maxStateValue {
			return errors.New("ExtensionStateInvalid", "a kept value is too large", "extension", in.id, "key", key, "size", len(b), "max", maxStateValue)
		}
		if string(b) != "null" {
			raw = b
		}
	}
	return in.scope.opts.StateSet(in.id, in.scope.opts.Info.Folder, key, raw)
}
