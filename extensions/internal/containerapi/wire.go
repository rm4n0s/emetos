package containerapi

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"
)

// flexTime reads a time that one engine writes as RFC 3339 text and another as Unix seconds: Docker's container and image
// lists have numbers, libpod's container list has text. A null, an empty string and the zero time are the zero Time.
type flexTime struct{ time.Time }

// UnmarshalJSON reads a number of seconds or a quoted RFC 3339 time. Anything else is the zero time rather than an error,
// so that one odd field does not hide a whole list.
func (t *flexTime) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	t.Time = time.Time{}
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil || s == "" {
			return nil
		}
		if parsed, err := time.Parse(time.RFC3339Nano, s); err == nil {
			t.Time = parsed
		}
		return nil
	}
	var secs float64
	if err := json.Unmarshal(b, &secs); err != nil {
		return nil
	}
	if secs > 0 {
		t.Time = time.Unix(int64(secs), 0)
	}
	return nil
}

// flexStrings reads a list of strings that one engine writes as an array and another as one string: Docker's entrypoint is
// an array, Podman's inspect has a string (split at white space). A null is the empty list.
type flexStrings []string

// UnmarshalJSON reads an array of strings or a string.
func (s *flexStrings) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	*s = nil
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	if b[0] == '"' {
		var one string
		if err := json.Unmarshal(b, &one); err != nil {
			return nil
		}
		if fields := strings.Fields(one); len(fields) > 0 {
			*s = fields
		}
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		return nil
	}
	*s = many
	return nil
}

// commandLine turns a container's command, which Docker's list writes as a line and libpod's as an array, into a line.
func commandLine(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return ""
	}
	if raw[0] == '"' {
		var line string
		if err := json.Unmarshal(raw, &line); err != nil {
			return ""
		}
		return line
	}
	var parts []string
	if err := json.Unmarshal(raw, &parts); err != nil {
		return ""
	}
	return strings.Join(parts, " ")
}

// trimSlash removes the slash that Docker puts in front of a container's name.
func trimSlash(name string) string { return strings.TrimPrefix(name, "/") }
