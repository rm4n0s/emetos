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

// toldKey is the key under which FirstTime remembers that the user was told the thing.
func toldKey(what string) string { return "told:" + what }

// FirstTime says whether the user is told the thing named what for the first time, and remembers that it was told: the next call with
// the same name says false, in this session and in every later one, until Forget. It is for a notice that nobody asked for (a tool that
// an extension needs and that is not installed, found when a folder is opened): with the API's GlobalState it is shown once and not at
// every start of Emetos. A state that cannot keep the answer says true every time, so the notice is never lost.
func FirstTime(s State, what string) bool { return FirstTimeSaying(s, what, "") }

// FirstTimeSaying is FirstTime for a notice whose text may change (the path of a program that the user set and that is no program):
// it says true when the message is not the one last told about the thing, and remembers the message.
func FirstTimeSaying(s State, what, message string) bool {
	if last, told := StateValue[string](s, toldKey(what)); told && last == message {
		return false
	}
	_ = s.Set(toldKey(what), message) //nolint:errcheck // a state that keeps nothing tells again, which is the safe side
	return true
}

// Forget takes away what FirstTime remembers for the name: the thing is told again the next time. An extension calls it when the
// reason of the notice is gone (the tool was found), so that the user hears of it once more if it comes back.
func Forget(s State, what string) {
	if _, told := s.Get(toldKey(what)); told {
		_ = s.Set(toldKey(what), nil) //nolint:errcheck // what cannot be forgotten only keeps a notice quiet
	}
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
