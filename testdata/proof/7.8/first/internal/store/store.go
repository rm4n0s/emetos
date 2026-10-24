// Package store keeps the notes in memory.
package store

// Store is a list of notes.
type Store struct{ notes []string }

// New makes an empty store.
func New() *Store { return &Store{} }

// Add appends a note.
func (s *Store) Add(note string) { s.notes = append(s.notes, note) }

// All lists the notes in the order they were added.
func (s *Store) All() []string { return s.notes }
