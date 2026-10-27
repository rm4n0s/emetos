//go:build !emetos_dev

package remotework

// devBuild is false without the emetos_dev build tag: only debug builds register dev commands.
const devBuild = false
