package demo

import "github.com/rm4n0s/errors"

func Run(tag string) error { return errors.New(tag, "a tag that is not a literal") }
