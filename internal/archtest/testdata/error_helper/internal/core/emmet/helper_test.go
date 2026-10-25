package emmet

import "github.com/rm4n0s/errors"

// Test files may build errors in helpers: their fakes stand for the real ones.
func fake() error { return errors.New("Fake", "fake") }

func useFake() error { return fake() }
