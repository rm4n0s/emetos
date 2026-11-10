package exthost

import (
	"encoding/json"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// maxStateValue is the largest value State().Set accepts, in bytes of JSON (storage.MaxExtensionValue; the exthost package does not
// import the storage).
const maxStateValue = 64 * 1024

// globalStateFolder is the folder under which the values of every folder are kept (storage.GlobalExtensionFolder; the exthost package
// does not import the storage).
const globalStateFolder = "*"

// stateAPI is the extension.State of an activation: the values are kept by the scope's StateGet and StateSet under the id of the
// extension and the folder of the scope, or, for the global state, under globalStateFolder.
type stateAPI struct {
	inst   *instance
	global bool
}

var _ extension.State = stateAPI{}

// State implements extension.API.
func (a *api) State() extension.State { return stateAPI{inst: a.inst} }

// GlobalState implements extension.API.
func (a *api) GlobalState() extension.State { return stateAPI{inst: a.inst, global: true} }

// folder is the folder the values are kept under.
func (s stateAPI) folder() string {
	if s.global {
		return globalStateFolder
	}
	return s.inst.scope.opts.Info.Folder
}

// Get implements extension.State.
func (s stateAPI) Get(key string) (json.RawMessage, bool) {
	in := s.inst
	if in.isDead() || in.scope.opts.StateGet == nil || key == "" {
		return nil, false
	}
	return in.scope.opts.StateGet(in.id, s.folder(), key)
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
	return in.scope.opts.StateSet(in.id, s.folder(), key, raw)
}
