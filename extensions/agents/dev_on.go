//go:build emetos_dev

package agents

// devBuild is set by the emetos_dev build tag: dev commands are registered even in a release build (SPEC D15).
const devBuild = true
