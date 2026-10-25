package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/rm4n0s/errors"
)

// SharingVersion is the version of event-sharing.json (SPEC §10.15 "Event sharing"). A file with another version is
// ignored (SharingVersionUnsupported) and written again with this one by the next change.
const SharingVersion = 1

// EventSharingPath is event-sharing.json in the state folder, $XDG_STATE_HOME/emetos/event-sharing.json: the links
// between window tabs that the user allowed. It is the same for every profile (SPEC §10.15).
func (p Paths) EventSharingPath() string {
	return filepath.Join(p.StateHome, appDir, "event-sharing.json")
}

// SharingLink lets the events of the scope From reach the extensions of the scope To (SPEC §10.15). A scope is named
// by its id: the absolute, cleaned folder of a workspace, or "<extension id>:<tab id>" for the tab of an extension.
type SharingLink struct {
	// From is the scope the events come from, To the scope they go to; a link works in this direction only.
	From string `json:"from"`
	To   string `json:"to"`
	// Events are the topic patterns the link carries; empty carries every topic that an extension asked for.
	Events []string `json:"events,omitempty"`
}

// sharingFile is the shape of event-sharing.json.
type sharingFile struct {
	Version int           `json:"version"`
	Links   []SharingLink `json:"links"`
	Private []string      `json:"private"`
}

// SharingSnapshot is a copy of what a SharingStore holds.
type SharingSnapshot struct {
	// Links are the links saved in the file.
	Links []SharingLink
	// Session are the links that last until Emetos quits; they are never written.
	Session []SharingLink
	// Private are the scopes that send nothing to other scopes, sorted.
	Private []string
}

// SharingStoreOptions say how a SharingStore writes.
type SharingStoreOptions struct {
	// WriteFile replaces the file; WriteFileAtomic when nil.
	WriteFile WriteFileFunc
	// Logger is the log of the store; slog.Default() when nil.
	Logger *slog.Logger
}

// SharingStore holds the links between window tabs and the private workspaces (SPEC §10.15 "Event sharing"): the saved
// ones in event-sharing.json, written at once, atomically and with mode 0600 whenever the user changes them, and the
// session links, which stay in memory. Only the permission UI of Emetos calls its changing methods; no settings file
// and no extension can. It is safe for concurrent use.
type SharingStore struct {
	path   string
	write  WriteFileFunc
	logger *slog.Logger
	// marshal encodes the file; json.MarshalIndent, and a failing one in a test.
	marshal func(v any, prefix, indent string) ([]byte, error)

	mu      sync.RWMutex
	links   []SharingLink
	session []SharingLink
	private []string
}

// NewSharingStore makes the store over the file of paths. It reads nothing yet (see Load). Paths without a state
// folder (the zero value) make a store that is kept in memory only.
func NewSharingStore(paths Paths, opts SharingStoreOptions) *SharingStore {
	s := &SharingStore{write: opts.WriteFile, logger: opts.Logger, marshal: json.MarshalIndent}
	if s.write == nil {
		s.write = WriteFileAtomic
	}
	if s.logger == nil {
		s.logger = slog.Default()
	}
	if paths.StateHome != "" {
		s.path = paths.EventSharingPath()
	}
	return s
}

// Path is the file of the store, or empty for a store in memory.
func (s *SharingStore) Path() string { return s.path }

// Load reads the file. A file that is not there is an empty store. A file that cannot be used gives an empty store and an
// error, which the caller logs as a warning: SharingFileUnreadable, SharingFileCorrupt or SharingVersionUnsupported. The
// next change replaces such a file. Links that cannot be used (an empty or equal end) are dropped.
func (s *SharingStore) Load(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.links, s.private = nil, nil
	if s.path == "" {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return errors.NewErr("SharingFileUnreadable", err).SetMetadata("path", s.path)
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return errors.NewErr("SharingFileUnreadable", err).SetMetadata("path", s.path)
	}
	var f sharingFile
	if err := json.NewDecoder(bytes.NewReader(data)).Decode(&f); err != nil {
		return errors.NewErr("SharingFileCorrupt", err).SetMetadata("path", s.path)
	}
	if f.Version != SharingVersion {
		return errors.New("SharingVersionUnsupported", "event-sharing.json has another version than this program reads",
			"path", s.path, "version", f.Version, "supported", SharingVersion)
	}
	for _, l := range f.Links {
		if l.From == "" || l.To == "" || l.From == l.To {
			continue
		}
		s.links = mergeLink(s.links, l)
	}
	for _, id := range f.Private {
		if id != "" && !slices.Contains(s.private, id) {
			s.private = append(s.private, id)
		}
	}
	slices.Sort(s.private)
	return nil
}

// Snapshot is a copy of the links and the private scopes.
func (s *SharingStore) Snapshot() SharingSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return SharingSnapshot{Links: cloneLinks(s.links), Session: cloneLinks(s.session), Private: slices.Clone(s.private)}
}

// IsPrivate tells whether the scope is private: it sends nothing to other scopes.
func (s *SharingStore) IsPrivate(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return slices.Contains(s.private, id)
}

// Allows tells whether a link, saved or for the session, lets the topic go from one scope to the other, and neither the
// scope it comes from is private. match tells whether a topic matches a pattern of a link.
func (s *SharingStore) Allows(from, to, topic string, match func(pattern, topic string) bool) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if slices.Contains(s.private, from) {
		return false
	}
	covers := func(list []SharingLink) bool {
		for _, l := range list {
			if l.From == from && l.To == to && linkCovers(l, topic, match) {
				return true
			}
		}
		return false
	}
	return covers(s.links) || covers(s.session)
}

// Carries tells whether a link, saved or for the session, carries every topic that the pattern matches, and the scope it
// comes from is not private. within tells whether a pattern is within a list of patterns.
func (s *SharingStore) Carries(from, to, pattern string, within func(pattern string, list []string) bool) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if slices.Contains(s.private, from) {
		return false
	}
	carries := func(list []SharingLink) bool {
		for _, l := range list {
			if l.From == from && l.To == to && (len(l.Events) == 0 || within(pattern, l.Events)) {
				return true
			}
		}
		return false
	}
	return carries(s.links) || carries(s.session)
}

// linkCovers tells whether the link carries the topic: it has no patterns (every topic) or one of them matches.
func linkCovers(l SharingLink, topic string, match func(pattern, topic string) bool) bool {
	if len(l.Events) == 0 {
		return true
	}
	return slices.ContainsFunc(l.Events, func(p string) bool { return match(p, topic) })
}

// AddLink saves a link and writes the file. A link between the same scopes that is saved already gets the new
// patterns added (a link that carries every topic stays so). When the file cannot be written nothing changes and the
// error has the tag SharingWriteFailed (or SharingEncodeFailed); the caller keeps a session link instead.
func (s *SharingStore) AddLink(ctx context.Context, l SharingLink) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	before := cloneLinks(s.links)
	s.links = mergeLink(s.links, l)
	if err := s.save(ctx); err != nil {
		s.links = before
		return err
	}
	return nil
}

// AddSessionLink keeps a link in memory until Emetos quits.
func (s *SharingStore) AddSessionLink(l SharingLink) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.session = mergeLink(s.session, l)
}

// RemoveLink removes the link between the scopes, from the saved links or from the session links, and writes the file.
// It reports the link that was removed; ok is false when there was none. A file that cannot be written leaves the link
// removed in memory for this session and returns the error.
func (s *SharingStore) RemoveLink(ctx context.Context, from, to string, session bool) (removed SharingLink, ok bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := &s.links
	if session {
		list = &s.session
	}
	i := slices.IndexFunc(*list, func(l SharingLink) bool { return l.From == from && l.To == to })
	if i < 0 {
		return SharingLink{}, false, nil
	}
	removed = cloneLinks([]SharingLink{(*list)[i]})[0]
	*list = slices.Delete(*list, i, i+1)
	if session {
		return removed, true, nil
	}
	return removed, true, s.save(ctx)
}

// SetPrivate makes the scope private or not, and writes the file. It reports whether anything changed. A file that cannot
// be written leaves the change made for this session and returns the error.
func (s *SharingStore) SetPrivate(ctx context.Context, id string, on bool) (changed bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	has := slices.Contains(s.private, id)
	switch {
	case on && !has:
		s.private = append(s.private, id)
		slices.Sort(s.private)
	case !on && has:
		s.private = slices.DeleteFunc(s.private, func(x string) bool { return x == id })
	default:
		return false, nil
	}
	return true, s.save(ctx)
}

// save writes the saved links and the private scopes. The caller holds mu.
func (s *SharingStore) save(ctx context.Context) error {
	if s.path == "" {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return errors.NewErr("SharingWriteFailed", err).SetMetadata("path", s.path)
	}
	f := sharingFile{Version: SharingVersion, Links: cloneLinks(s.links), Private: slices.Clone(s.private)}
	if f.Links == nil {
		f.Links = []SharingLink{}
	}
	if f.Private == nil {
		f.Private = []string{}
	}
	data, err := s.marshal(f, "", "  ")
	if err != nil {
		return errors.NewErr("SharingEncodeFailed", err).SetMetadata("path", s.path)
	}
	if err := s.write(s.path, append(data, '\n')); err != nil {
		return errors.NewErr("SharingWriteFailed", err).SetMetadata("path", s.path)
	}
	return nil
}

// mergeLink adds the link to the list: the link between the same scopes gets the patterns added; a link or an addition
// without patterns carries every topic. A link with an empty end, or from a scope to itself, is not added.
func mergeLink(list []SharingLink, l SharingLink) []SharingLink {
	if l.From == "" || l.To == "" || l.From == l.To {
		return list
	}
	i := slices.IndexFunc(list, func(x SharingLink) bool { return x.From == l.From && x.To == l.To })
	if i < 0 {
		return append(list, cloneLinks([]SharingLink{l})[0])
	}
	if len(list[i].Events) == 0 {
		return list
	}
	if len(l.Events) == 0 {
		list[i].Events = nil
		return list
	}
	for _, p := range l.Events {
		if !slices.Contains(list[i].Events, p) {
			list[i].Events = append(list[i].Events, p)
		}
	}
	return list
}

// cloneLinks copies a list of links with their patterns.
func cloneLinks(list []SharingLink) []SharingLink {
	if list == nil {
		return nil
	}
	out := make([]SharingLink, len(list))
	for i, l := range list {
		out[i] = SharingLink{From: l.From, To: l.To, Events: slices.Clone(l.Events)}
	}
	return out
}
