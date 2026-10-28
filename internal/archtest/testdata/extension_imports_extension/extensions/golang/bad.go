package golang

import (
	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/golang/grammar"
	"github.com/rm4n0s/emetos/extensions/internal/testext"
	"github.com/rm4n0s/emetos/extensions/markdown"
	"github.com/rm4n0s/emetos/extensions/terminal/emulator"
)

var _ extension.API
var _ = grammar.Rules
var _ = testext.New
var _ = markdown.New
var _ = emulator.New
