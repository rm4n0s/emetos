package exthost

import (
	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// validateManifest checks a manifest (extension.CheckManifest, whose error is returned as it is) and that no extension
// of the host has its id already (seen). The duplicate fails with the tag ExtensionIDDuplicate.
func validateManifest(m extension.Manifest, seen map[string]bool) error {
	if err := extension.CheckManifest(m); err != nil {
		return err
	}
	if seen[m.ID] {
		return errors.New("ExtensionIDDuplicate", "two extensions have the same id", "id", m.ID)
	}
	return nil
}
