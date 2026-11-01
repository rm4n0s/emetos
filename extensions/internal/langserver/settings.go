package langserver

import (
	"encoding/json"
	"sort"
	"strings"
	"sync"
)

// Settings reads the settings of the tab for the hooks of a Config and remembers which it read, so that a change of one of them
// restarts the server (Controller). It is safe for use from any goroutine.
type Settings struct {
	get  func(key string) (any, bool)
	mu   sync.Mutex
	read map[string]string
}

// NewSettings makes Settings over a function that reads a setting (extension.LaunchRequest.Setting).
func NewSettings(get func(key string) (any, bool)) *Settings {
	return &Settings{get: get, read: map[string]string{}}
}

// Value is the value of the setting and whether there is such a setting. The key is remembered either way.
func (s *Settings) Value(key string) (any, bool) {
	v, ok := s.get(key)
	s.mu.Lock()
	s.read[key] = Fingerprint(v, ok)
	s.mu.Unlock()
	return v, ok
}

// String is the value of a string setting, "" when it is not one. Surrounding spaces are removed.
func (s *Settings) String(key string) string {
	v, _ := s.Value(key)
	str, _ := v.(string)
	return strings.TrimSpace(str)
}

// Bool is the value of a boolean setting, false when it is not one.
func (s *Settings) Bool(key string) bool {
	v, _ := s.Value(key)
	b, _ := v.(bool)
	return b
}

// Float is the value of a number setting, 0 when it is not one.
func (s *Settings) Float(key string) float64 {
	v, _ := s.Value(key)
	f, _ := v.(float64)
	return f
}

// Strings is the value of an array of strings; other members and other types give nothing.
func (s *Settings) Strings(key string) []string {
	v, _ := s.Value(key)
	list, _ := v.([]any)
	var out []string
	for _, item := range list {
		if str, ok := item.(string); ok {
			out = append(out, str)
		}
	}
	return out
}

// Object is the value of an object setting, nil when it is not one.
func (s *Settings) Object(key string) map[string]any {
	v, _ := s.Value(key)
	m, _ := v.(map[string]any)
	return m
}

// Read lists what was read: each key with the Fingerprint of its value.
func (s *Settings) Read() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]string, len(s.read))
	for k, v := range s.read {
		out[k] = v
	}
	return out
}

// Keys are the keys that were read, sorted.
func (s *Settings) Keys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]string, 0, len(s.read))
	for k := range s.read {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Fingerprint is a text that is equal for equal values of a setting: its JSON, or "absent" for a setting nobody registered.
func Fingerprint(v any, ok bool) string {
	if !ok {
		return "absent"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "unencodable"
	}
	return string(b)
}
