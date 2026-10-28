//go:build !emetos_dev

package sharing

// devBuild is false without the emetos_dev build tag: only debug builds read the dev knob of the interfaces.
const devBuild = false
