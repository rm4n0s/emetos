package quickinput

import "strings"

// KeyCaps splits the label of a keybinding ("Ctrl+K Ctrl+C") into chords and each chord into the
// names of its keys, which the list draws as key caps: [["Ctrl", "K"], ["Ctrl", "C"]]. A key that is
// a plus sign stays a key ("Ctrl++" is Ctrl and +).
func KeyCaps(label string) [][]string {
	var out [][]string
	for _, chord := range strings.Fields(label) {
		var keys []string
		rest := chord
		for rest != "" {
			if strings.HasPrefix(rest, "+") {
				// a "+" at the start of what is left is the key itself
				keys = append(keys, "+")
				rest = strings.TrimPrefix(rest[1:], "+")
				continue
			}
			i := strings.Index(rest, "+")
			if i < 0 {
				keys = append(keys, rest)
				break
			}
			keys = append(keys, rest[:i])
			rest = rest[i+1:]
		}
		out = append(out, keys)
	}
	return out
}
