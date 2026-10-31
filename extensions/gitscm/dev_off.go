//go:build !emetos_dev

package gitscm

// devBuild is false without the emetos_dev build tag: only debug builds read the dev knobs.
const devBuild = false
