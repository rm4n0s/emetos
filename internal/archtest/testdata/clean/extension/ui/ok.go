package ui

import (
	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// extension/ui is the one bridge of the contract to the widgets of the editor (SPEC §10.15).
var _ extension.API
var _ kit.Colors
