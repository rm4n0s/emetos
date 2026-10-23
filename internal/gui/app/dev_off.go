//go:build !emetos_dev

package app

// devBuild is false without the emetos_dev build tag: only debug builds run dev commands.
const devBuild = false
