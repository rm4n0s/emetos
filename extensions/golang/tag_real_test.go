//go:build emetos_gopls || emetos_dlv

package golang

// realTools is true (the tests of the real tools: gopls, dlv): TestMain leaves PATH and the environment alone so that the tests of the real tools can find them.
const realTools = true
