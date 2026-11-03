package checks

import (
	"encoding/json"
	"maps"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"
)

// Values are those encoding/json decodes into any, with numbers as json.Number (jsonpath.Decode) or float64 (a value of a check read
// from a file).

// number reads a value as a number: a number, or a string that is one.
func number(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return f, err == nil && !math.IsNaN(f)
	}
	return 0, false
}

// isNumber says that a value is a number itself (not a string that looks like one).
func isNumber(v any) bool {
	switch v.(type) {
	case float64, int, int64, json.Number:
		return true
	}
	return false
}

// text is the value as text: a string as it is, a number as it was written, null as "null" and a container as compact JSON.
func text(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case bool:
		return strconv.FormatBool(x)
	case nil:
		return "null"
	}
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(data)
}

// kind is the name isType uses for a value.
func kind(v any) string {
	switch v.(type) {
	case string:
		return "string"
	case float64, int, int64, json.Number:
		return "number"
	case bool:
		return "boolean"
	case nil:
		return "null"
	case map[string]any:
		return "object"
	case []any:
		return "array"
	}
	return "unknown"
}

// normalize makes numbers float64 all the way down, so that two values from different sources can be compared.
func normalize(v any) any {
	switch x := v.(type) {
	case json.Number, int, int64:
		f, _ := number(x)
		return f
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = normalize(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = normalize(e)
		}
		return out
	}
	return v
}

// equal compares two values: numbers by value (a string that is a number equals the number), the rest by type and content.
func equal(a, b any) bool {
	if isNumber(a) || isNumber(b) {
		fa, oka := number(a)
		fb, okb := number(b)
		return oka && okb && fa == fb
	}
	return reflect.DeepEqual(normalize(a), normalize(b))
}

// clip cuts a text shown in a result.
func clip(s string) string {
	const limit = 200
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && s[cut]&0xC0 == 0x80 { // do not cut a character in two
		cut--
	}
	return s[:cut] + "..."
}

// ProblemText is the text of an error for a result: its message and its metadata, "the path matches nothing in the body (path=$.id)".
func ProblemText(err error) string {
	appErr, ok := errors.FromError(err)
	if !ok {
		return err.Error()
	}
	if len(appErr.Metadata) == 0 {
		return appErr.Message
	}
	keys := slices.Sorted(maps.Keys(appErr.Metadata))
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + text(appErr.Metadata[k])
	}
	return appErr.Message + " (" + strings.Join(parts, ", ") + ")"
}
