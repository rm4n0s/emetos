package extensions

import (
	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/golang"
)

var _ extension.Factory
var _ = golang.New
