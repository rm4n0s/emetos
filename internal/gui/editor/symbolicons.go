package editor

import (
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// symbolIcons are the Codicon and the name of the color of each symbol kind, indexed by the LSP's SymbolKind less one.
//
// Ported from VS Code: src/vs/editor/common/languages.ts (SymbolKinds.toIcon) and
// src/vs/editor/contrib/symbolIcons/browser/symbolIcons.ts
var symbolIcons = [...]struct {
	code  int
	color string
}{
	{fonts.CodiconSymbolFile, "file"}, {fonts.CodiconSymbolNamespace, "module"}, {fonts.CodiconSymbolNamespace, "namespace"},
	{fonts.CodiconSymbolNamespace, "package"}, {fonts.CodiconSymbolClass, "class"}, {fonts.CodiconSymbolMethod, "method"},
	{fonts.CodiconSymbolProperty, "property"}, {fonts.CodiconSymbolField, "field"}, {fonts.CodiconSymbolMethod, "constructor"},
	{fonts.CodiconSymbolEnum, "enumerator"}, {fonts.CodiconSymbolInterface, "interface"}, {fonts.CodiconSymbolMethod, "function"},
	{fonts.CodiconSymbolVariable, "variable"}, {fonts.CodiconSymbolConstant, "constant"}, {fonts.CodiconSymbolString, "string"},
	{fonts.CodiconSymbolNumeric, "number"}, {fonts.CodiconSymbolBoolean, "boolean"}, {fonts.CodiconSymbolArray, "array"},
	{fonts.CodiconSymbolNamespace, "object"}, {fonts.CodiconSymbolString, "key"}, {fonts.CodiconSymbolBoolean, "null"},
	{fonts.CodiconSymbolEnumMember, "enumeratorMember"}, {fonts.CodiconSymbolStructure, "struct"}, {fonts.CodiconSymbolEvent, "event"},
	{fonts.CodiconSymbolOperator, "operator"}, {fonts.CodiconSymbolParameter, "typeParameter"},
}

// SymbolIcon is the code point of the Codicon of a symbol kind (the LSP's SymbolKind) and its color in the theme.
func SymbolIcon(colors kit.Colors, kind int) (code int, color ui.Color) {
	if kind < 1 || kind > len(symbolIcons) {
		kind = 7 // a kind that is not one is a property
	}
	ic := symbolIcons[kind-1]
	return ic.code, colors.SymbolIconColor(ic.color)
}

// SymbolIconCode is the code point of the Codicon of a symbol kind, for the lists that cannot color it.
func SymbolIconCode(kind int) int {
	if kind < 1 || kind > len(symbolIcons) {
		kind = 7
	}
	return symbolIcons[kind-1].code
}
