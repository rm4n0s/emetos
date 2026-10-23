// Package inventory keeps a count of the items in a store.
package inventory

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// MaxItems is the most kinds of item the store can hold.
const MaxItems = 128

var ErrFull = errors.New("the store is full")

// Store maps the name of an item to how many of it there are.
type Store struct {
	counts map[string]int
}

/*
Add puts n more of an item in the store.
It fails when the store has no room for a new kind of item.
*/
func (s *Store) Add(name string, n int) error {
	if _, known := s.counts[name]; !known && len(s.counts) >= MaxItems {
		return fmt.Errorf("add %q: %w", name, ErrFull)
	}
	s.counts[name] += n
	return nil
}

// Report lists the items, the most plentiful first.
func (s *Store) Report() string {
	names := make([]string, 0, len(s.counts))
	for name := range s.counts {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return s.counts[names[i]] > s.counts[names[j]] })
	var b strings.Builder
	for _, name := range names {
		fmt.Fprintf(&b, "%-12s %5d\n", name, s.counts[name])
	}
	return b.String()
}
