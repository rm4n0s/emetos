package testing

// NewTestCoverage makes a Coverage with the loader, for the tests of the package.
func NewTestCoverage(loader CoverageLoader) *Coverage { return newCoverage(loader) }
