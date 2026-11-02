//go:build !emetos_gopls && !emetos_dlv

package golang

// realTools is false: TestMain isolates the environment of the tests.
const realTools = false
