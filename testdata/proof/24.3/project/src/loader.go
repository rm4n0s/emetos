// Package src loads the configuration of the sample project of the Source Control proof.
package src

import "os"

// Load reads the file at path.
func Load(path string) ([]byte, error) {
	return os.ReadFile(path)
}
