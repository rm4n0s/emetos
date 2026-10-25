package other

import "github.com/rm4n0s/errors"

func Fail() error { return errors.New("OnlyNamed", "the test of this folder asserts nothing") }
