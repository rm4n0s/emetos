package scm

import "slices"

// historyCapacity is how many messages a repository remembers (the capacity VS Code gives its HistoryNavigator2 for the input).
const historyCapacity = 100

// History is the list of commit messages of a repository in this session, with a cursor that goes through them: VS Code's
// HistoryNavigator2. It always holds at least one value, the last one, which is the message being written; a value is in the list
// once (an older copy is dropped when the same text is added again).
//
// Ported from VS Code: src/vs/base/common/history.ts (HistoryNavigator2)
type History struct {
	items    []string
	cursor   int
	capacity int
}

// NewHistory makes a history from values (at least one; an empty list gets one empty message) and a capacity, where the last
// value is the current one.
func NewHistory(capacity int, values ...string) *History {
	if len(values) == 0 {
		values = []string{""}
	}
	h := &History{items: []string{values[0]}, capacity: max(capacity, 1)}
	for _, v := range values[1:] {
		h.Add(v)
	}
	h.cursor = len(h.items) - 1
	return h
}

// Add puts a value at the end and moves the cursor to it; an older copy of the same value is dropped, and the oldest values go
// when there are more than the capacity.
func (h *History) Add(value string) {
	h.items = append(h.items, value)
	h.cursor = len(h.items) - 1
	h.dropOlderCopies(value)
	if over := len(h.items) - h.capacity; over > 0 {
		h.items = h.items[over:]
		h.cursor = len(h.items) - 1
	}
}

// ReplaceLast replaces the last value and returns the old one. It does nothing, and returns value, when the last value is the
// same.
func (h *History) ReplaceLast(value string) string {
	last := len(h.items) - 1
	if h.items[last] == value {
		return value
	}
	old := h.items[last]
	h.items[last] = value
	h.dropOlderCopies(value)
	return old
}

// dropOlderCopies removes the values before the last one that equal value.
func (h *History) dropOlderCopies(value string) {
	last := len(h.items) - 1
	for i := last - 1; i >= 0; i-- {
		if h.items[i] != value {
			continue
		}
		h.items = slices.Delete(h.items, i, i+1)
		last--
		if h.cursor > i {
			h.cursor--
		}
	}
	h.cursor = min(h.cursor, len(h.items)-1)
}

// IsAtEnd tells whether the cursor is on the last value.
func (h *History) IsAtEnd() bool { return h.cursor == len(h.items)-1 }

// Current is the value at the cursor.
func (h *History) Current() string { return h.items[h.cursor] }

// Previous moves the cursor to the value before and returns it; at the first value it stays.
func (h *History) Previous() string {
	if h.cursor > 0 {
		h.cursor--
	}
	return h.items[h.cursor]
}

// Next moves the cursor to the value after and returns it; at the last value it stays.
func (h *History) Next() string {
	if h.cursor < len(h.items)-1 {
		h.cursor++
	}
	return h.items[h.cursor]
}

// Has tells whether the history holds the value.
func (h *History) Has(value string) bool { return slices.Contains(h.items, value) }

// ResetCursor moves the cursor to the last value and returns it.
func (h *History) ResetCursor() string {
	h.cursor = len(h.items) - 1
	return h.items[h.cursor]
}

// Values are the values from the oldest to the last.
func (h *History) Values() []string { return slices.Clone(h.items) }

// Len is the number of values.
func (h *History) Len() int { return len(h.items) }
