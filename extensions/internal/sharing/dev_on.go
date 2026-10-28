//go:build emetos_dev

package sharing

// devBuild is set by the emetos_dev build tag: the dev knob of the interfaces is read even in a release build (SPEC D15).
const devBuild = true
