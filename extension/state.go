package extension

import "encoding/json"

// State keeps small values of the extension in Emetos's state (the state folder, not the user's settings), for the folder of the scope:
// the Python pack keeps the interpreter the user chose for a folder there (SPEC §10.34 "As built (step 24b.4)"). A value is whatever
// encodes to JSON, up to 64 KiB; it survives restarts of Emetos and belongs to the extension, the folder and the profile. It is the
// extension's own memory, not something the user edits: what the user decides to write down is a setting.
//
// The methods may be called from any goroutine (a language server's launcher reads the value off the UI thread).
type State interface {
	// Get returns the value kept under the key for the folder of the scope as JSON, and whether there is one. A scope without a
	// folder has values of its own, shared by the tabs without one.
	Get(key string) (json.RawMessage, bool)
	// Set keeps the value under the key; a nil value takes the key away. It fails with ExtensionStateInvalid for an empty key, a value
	// that does not encode to JSON or is larger than 64 KiB, with ExtensionStateUnavailable when the scope keeps no state (a scope
	// that is not a workspace tab), and with ExtensionDeactivated after the activation ended.
	Set(key string, value any) error
}

// StateValue is the value kept under the key decoded into T, and whether there is one of that shape: a value that does not decode into
// T is no value.
func StateValue[T any](s State, key string) (T, bool) {
	var v T
	raw, ok := s.Get(key)
	if !ok || json.Unmarshal(raw, &v) != nil {
		var zero T
		return zero, false
	}
	return v, true
}
