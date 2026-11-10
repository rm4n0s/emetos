package storage

import (
	"context"
	"encoding/json"
	"maps"
	"path/filepath"

	"github.com/rm4n0s/errors"
)

// MaxExtensionValue is the largest value, in bytes of JSON, an extension may keep under one key (extension.State): the state file is
// read at every start, so it is for small values, not for data.
const MaxExtensionValue = 64 * 1024

// GlobalExtensionFolder is the folder key of the values an extension keeps for every folder (extension.API.GlobalState): no folder has
// this name, since the folders of the state are absolute paths or empty.
const GlobalExtensionFolder = "*"

// ExtensionValue returns the value the extension kept for the folder under the key, as the JSON it gave, and whether there is one. The
// folder is cleaned (an empty one is the tab without a folder). The error has the tags of LoadGlobal, which are not fatal (the value is
// not there then).
func (s *StateStore) ExtensionValue(ctx context.Context, extension, folder, key string) (json.RawMessage, bool, error) {
	g, err := s.LoadGlobal(ctx)
	v, ok := g.Extensions[extension][cleanFolderKey(folder)][key]
	if !ok {
		return nil, false, err
	}
	return v, true, err
}

// SetExtensionValue keeps the value (JSON) for the extension and the folder under the key; a nil or empty value takes the key away. It
// fails with ExtensionStateInvalid for an empty extension id or key, a value that is not JSON or is larger than MaxExtensionValue; the
// rest are the tags of UpdateGlobal. The value is written after the delay of the store.
func (s *StateStore) SetExtensionValue(ctx context.Context, extension, folder, key string, value json.RawMessage) error {
	switch {
	case extension == "" || key == "":
		return errors.New("ExtensionStateInvalid", "a kept value needs an extension and a key", "extension", extension, "key", key)
	case len(value) > MaxExtensionValue:
		return errors.New("ExtensionStateInvalid", "a kept value is too large", "extension", extension, "key", key, "size", len(value), "max", MaxExtensionValue)
	case len(value) > 0 && !json.Valid(value):
		return errors.New("ExtensionStateInvalid", "a kept value is not JSON", "extension", extension, "key", key)
	}
	folder = cleanFolderKey(folder)
	return s.UpdateGlobal(ctx, func(g *GlobalState) bool {
		cur, had := g.Extensions[extension][folder][key]
		if len(value) == 0 {
			if !had {
				return false
			}
			delete(g.Extensions[extension][folder], key)
			if len(g.Extensions[extension][folder]) == 0 {
				delete(g.Extensions[extension], folder)
			}
			if len(g.Extensions[extension]) == 0 {
				delete(g.Extensions, extension)
			}
			return true
		}
		if had && string(cur) == string(value) {
			return false
		}
		if g.Extensions == nil {
			g.Extensions = map[string]map[string]map[string]json.RawMessage{}
		}
		if g.Extensions[extension] == nil {
			g.Extensions[extension] = map[string]map[string]json.RawMessage{}
		}
		if g.Extensions[extension][folder] == nil {
			g.Extensions[extension][folder] = map[string]json.RawMessage{}
		}
		g.Extensions[extension][folder][key] = append(json.RawMessage(nil), value...)
		return true
	})
}

// cleanFolderKey is the folder as the state keeps it: cleaned, or empty, or GlobalExtensionFolder.
func cleanFolderKey(folder string) string {
	if folder == "" || folder == GlobalExtensionFolder {
		return folder
	}
	return filepath.Clean(folder)
}

// cleanExtensionState drops what a state file holds that cannot be used: an empty id or key, a folder that is not absolute (and not
// empty or GlobalExtensionFolder) and a value that is not JSON.
func cleanExtensionState(in map[string]map[string]map[string]json.RawMessage) map[string]map[string]map[string]json.RawMessage {
	var out map[string]map[string]map[string]json.RawMessage
	for ext, folders := range in {
		for folder, keys := range folders {
			if ext == "" || (folder != "" && folder != GlobalExtensionFolder && !filepath.IsAbs(folder)) {
				continue
			}
			for key, v := range keys {
				if key == "" || len(v) == 0 || len(v) > MaxExtensionValue || !json.Valid(v) {
					continue
				}
				if out == nil {
					out = map[string]map[string]map[string]json.RawMessage{}
				}
				if out[ext] == nil {
					out[ext] = map[string]map[string]json.RawMessage{}
				}
				f := cleanFolderKey(folder)
				if out[ext][f] == nil {
					out[ext][f] = map[string]json.RawMessage{}
				}
				out[ext][f][key] = v
			}
		}
	}
	return out
}

// cloneExtensionState copies the three levels of the map.
func cloneExtensionState(in map[string]map[string]map[string]json.RawMessage) map[string]map[string]map[string]json.RawMessage {
	if in == nil {
		return nil
	}
	out := make(map[string]map[string]map[string]json.RawMessage, len(in))
	for ext, folders := range in {
		out[ext] = make(map[string]map[string]json.RawMessage, len(folders))
		for folder, keys := range folders {
			out[ext][folder] = maps.Clone(keys)
		}
	}
	return out
}
