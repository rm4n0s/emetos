package containerui

import "github.com/rm4n0s/emetos/extension"

// ContainersListed tells whether the extension made by New has read the list of containers from its engine. It is for the tests of
// other packages that need the extension to know its containers before they run a command on one (the tests of Connect to
// Database, SPEC §10.24); call it on the UI thread, which in a test is the goroutine that pumps the fake API.
func ContainersListed(x extension.Extension) bool {
	e, ok := x.(*ext)
	return ok && e.loads[resContainers].loaded
}
