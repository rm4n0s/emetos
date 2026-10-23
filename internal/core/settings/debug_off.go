//go:build glutter_release

package settings

// debugBuild is false in release builds: misuse of the settings gives the zero value.
const debugBuild = false
