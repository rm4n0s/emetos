//go:build !debug_mode

package settings

// debugBuild is false in release builds: misuse of the settings gives the zero value.
const debugBuild = false
