package main

import "sort"

// Names keeps a sorted list of names.
type Names struct {
	list []string
}

// Add puts a name in its place.
func (n *Names) Add(name string) {
	n.list = append(n.list, name)
	sort.Strings(n.list)
}

// Len is the number of names.
func (n *Names) Len() int { return len(n.list) }
