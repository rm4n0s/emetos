package markdown

import (
	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// extension/ui/markdown is a package of the bridge: it may use the kit as extension/ui does (SPEC §10.15 "As built (step 11.6)").
var _ extension.Token
var _ kit.Colors
var _ = fonts.CodiconCheck
