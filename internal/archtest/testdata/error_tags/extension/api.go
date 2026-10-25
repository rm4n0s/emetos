package extension

import "github.com/rm4n0s/errors"

func Call() error { return errors.New("ExtensionCallFailed", "in the public contract") }
