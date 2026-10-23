package emmet

import (
	"embed"
	"encoding/json"
	"io/fs"
	"maps"
	"slices"
	"strings"

	"github.com/rm4n0s/errors"
)

// The snippets and the vocabularies of Emmet, copied by scripts/update-assets.sh
// (third_party/emmet/LICENSE is the license of Emmet).
//
//go:embed assets/*.json assets/lorem/*.json
var assetFS embed.FS

// readStringMap reads an embedded JSON object of strings.
func readStringMap(name string) (map[string]string, error) { return readStringMapFrom(assetFS, name) }

// readStringMapFrom reads a JSON object of strings from a file system.
func readStringMapFrom(fsys fs.FS, name string) (map[string]string, error) {
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, errors.NewErr("EmmetAssetMissing", err).SetMetadata("asset", name)
	}
	return parseStringMap(name, data)
}

// parseStringMap reads a JSON object of strings.
func parseStringMap(name string, data []byte) (map[string]string, error) {
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, errors.NewErr("EmmetAssetInvalid", err).SetMetadata("asset", name)
	}
	return m, nil
}

// splitSnippetKeys turns the snippets with names like "a|b" into a plain map: one entry for each name.
func splitSnippetKeys(snippets map[string]string) map[string]string {
	result := make(map[string]string, len(snippets))
	for _, k := range slices.Sorted(maps.Keys(snippets)) {
		for _, name := range strings.Split(k, "|") {
			result[name] = snippets[k]
		}
	}
	return result
}
