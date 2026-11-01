package app

import "encoding/json"

// extensionStateGet is the StateGet of the scope (extension.State.Get): the value an extension kept for the folder, from the state of the
// profile. A state that cannot be read has no values; it is logged by the store's own errors when it is written. It runs on any goroutine.
func (w *Workspace) extensionStateGet(extensionID, folder, key string) (json.RawMessage, bool) {
	v, ok, err := w.stateStore().ExtensionValue(w.ctx, extensionID, folder, key)
	if err != nil {
		w.Process.Logger.Warn("the state of an extension cannot be read", "extension", extensionID, "key", key, "err", err)
	}
	return v, ok
}

// extensionStateSet is the StateSet of the scope (extension.State.Set); a nil value takes the key away. It runs on any goroutine.
func (w *Workspace) extensionStateSet(extensionID, folder, key string, value json.RawMessage) error {
	return w.stateStore().SetExtensionValue(w.ctx, extensionID, folder, key, value)
}
