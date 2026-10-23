//go:build !glutter_release

package settings

// debugBuild is true in debug builds, where misusing the settings (reading a key that
// is not registered, asking for a type the setting does not have) panics.
const debugBuild = true
