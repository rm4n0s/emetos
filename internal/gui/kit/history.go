package kit

import "slices"

// DefaultHistoryLimit is the number of values an input box keeps in its history (VS Code's
// HistoryInputBox makes its navigator with a limit of 100).
const DefaultHistoryLimit = 100

// History is the list of values an input box remembers, with a position for going back and forth
// through them with the Up and Down keys. It keeps each value once (adding a value again moves it
// to the end), at most Limit values (the oldest go first), and the position starts, and goes back to
// after every Add, past the last value ("nowhere"), where Current has nothing to give. It is plain
// Go and is used from the UI thread only.
//
// Ported from VS Code: src/vs/base/common/history.ts (HistoryNavigator) and src/vs/base/common/navigator.ts
// (ArrayNavigator)
type History struct {
	items []string
	limit int
	// index is the position: -1 before the first value, len(items) past the last one.
	index int
}

// NewHistory makes a history with some values, oldest first, and a limit (the default is
// DefaultHistoryLimit; a limit below 1 is the default too). A repeated value is kept once, at its
// last place, and values past the limit are dropped, oldest first.
func NewHistory(values []string, limit ...int) *History {
	h := &History{limit: DefaultHistoryLimit}
	if len(limit) > 0 && limit[0] > 0 {
		h.limit = limit[0]
	}
	for _, v := range values {
		h.put(v)
	}
	h.reset()
	return h
}

// put adds a value at the end, taking an earlier copy out, and drops the oldest values over the limit.
func (h *History) put(v string) {
	if i := slices.Index(h.items, v); i >= 0 {
		h.items = slices.Delete(h.items, i, i+1)
	}
	h.items = append(h.items, v)
	if over := len(h.items) - h.limit; over > 0 {
		h.items = slices.Delete(h.items, 0, over)
	}
}

func (h *History) reset() { h.index = len(h.items) }

// Limit is the number of values the history keeps.
func (h *History) Limit() int { return h.limit }

// Items are the values, oldest first. The slice is a copy.
func (h *History) Items() []string { return slices.Clone(h.items) }

// Len is the number of values.
func (h *History) Len() int { return len(h.items) }

// Has says whether the value is in the history.
func (h *History) Has(v string) bool { return slices.Contains(h.items, v) }

// Add puts a value at the end of the history (taking an earlier copy out) and moves the position
// past it.
func (h *History) Add(v string) {
	h.put(v)
	h.reset()
}

// Clear forgets every value.
func (h *History) Clear() {
	h.items = nil
	h.reset()
}

// Current is the value at the position; it has none (false) before the first and past the last value.
func (h *History) Current() (string, bool) {
	if h.index < 0 || h.index >= len(h.items) {
		return "", false
	}
	return h.items[h.index], true
}

// Next moves to the next value and returns it. Past the last value it returns false and stays there,
// which an input box shows as an empty box.
func (h *History) Next() (string, bool) {
	h.index = min(h.index+1, len(h.items))
	return h.Current()
}

// Previous moves to the previous value and returns it. At the first value it stays and returns
// false.
func (h *History) Previous() (string, bool) {
	if h.IsFirst() {
		return "", false
	}
	h.index = max(h.index-1, -1)
	return h.Current()
}

// First moves to the first value and returns it.
func (h *History) First() (string, bool) {
	h.index = 0
	return h.Current()
}

// Last moves to the last value and returns it.
func (h *History) Last() (string, bool) {
	h.index = len(h.items) - 1
	return h.Current()
}

// IsFirst says whether the position is at the first value.
func (h *History) IsFirst() bool { return h.index == 0 && len(h.items) > 0 }

// IsLast says whether the position is at the last value, or past it.
func (h *History) IsLast() bool { return h.position() >= len(h.items)-1 }

// IsNowhere says whether the position is on no value.
func (h *History) IsNowhere() bool {
	_, ok := h.Current()
	return !ok
}

// position is the index of the current value, or -1 when there is none.
func (h *History) position() int {
	if _, ok := h.Current(); !ok {
		return -1
	}
	return h.index
}
