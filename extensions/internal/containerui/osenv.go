package containerui

import "os"

// osGetenv reads the process environment; the engine search takes it by default.
func osGetenv(name string) string { return os.Getenv(name) }
