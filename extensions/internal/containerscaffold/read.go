package containerscaffold

import "os"

// readOS reads a file from disk.
func readOS(p string) ([]byte, error) { return os.ReadFile(p) }
