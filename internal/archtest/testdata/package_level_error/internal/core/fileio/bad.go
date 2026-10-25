package fileio

import (
	"io"

	"github.com/rm4n0s/errors"
)

var errA = errors.New("A", "a package-level error")

var (
	errB = errors.NewErr("B", io.EOF).SetMetadata("k", "v")
	errC = errors.NewErrf("C", "formatted %d", 1)
)

var sentinel = &errors.Error{Tag: "Sentinel", Message: "a struct literal is fine"}

// run is a function literal: it makes its error when it is called, not at package level.
var run = func() error { return errors.New("D", "made when called") }

func use() error { return errors.New("E", "inside a function") }
