package regex

import "github.com/rm4n0s/errors"

// The tag of a fake error in a test may be a variable.
func fakeOf(tag string) error { return errors.New(tag, "fake") }
