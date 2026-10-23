package emmet

import (
	"math"
	"math/big"
	"strconv"
	"strings"
)

// Ported from Emmet: src/stylesheet/color.ts

// cssColor writes a color token: `transparent`, a hex color, or rgba().
func cssColor(tok *CSSToken, shortHex bool) string {
	if tok.R == 0 && tok.G == 0 && tok.B == 0 && tok.A == 0 {
		return "transparent"
	}
	if tok.A == 1 {
		return cssAsHex(tok, shortHex)
	}
	return cssAsRGB(tok)
}

// cssAsHex writes a color as a hex value; short writes #fff for #ffffff where it can.
func cssAsHex(tok *CSSToken, short bool) string {
	if short && tok.R%17 == 0 && tok.G%17 == 0 && tok.B%17 == 0 {
		return "#" + strconv.FormatInt(int64(tok.R>>4), 16) + strconv.FormatInt(int64(tok.G>>4), 16) + strconv.FormatInt(int64(tok.B>>4), 16)
	}
	return "#" + hex2(tok.R) + hex2(tok.G) + hex2(tok.B)
}

func hex2(n int) string {
	s := strconv.FormatInt(int64(n), 16)
	if len(s) < 2 {
		s = "0" + s
	}
	return s
}

// cssAsRGB writes a color as rgb() or rgba().
func cssAsRGB(tok *CSSToken) string {
	parts := []string{strconv.Itoa(tok.R), strconv.Itoa(tok.G), strconv.Itoa(tok.B)}
	name := "rgb"
	if tok.A != 1 {
		parts = append(parts, frac(tok.A, 8))
		name = "rgba"
	}
	return name + "(" + strings.Join(parts, ", ") + ")"
}

// frac writes a number with at most digits decimals and no zeros at the end.
func frac(num float64, digits int) string {
	s := jsToFixed(num, digits)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		s = strings.TrimSuffix(s, ".")
	}
	return s
}

// jsToFixed writes a number with a fixed count of decimals as JavaScript's toFixed() does: the
// exact value of the number is rounded, and half goes away from zero.
func jsToFixed(x float64, digits int) string {
	switch {
	case math.IsNaN(x):
		return "NaN"
	case math.IsInf(x, 1):
		return "Infinity"
	case math.IsInf(x, -1):
		return "-Infinity"
	}
	sign := ""
	if x < 0 {
		sign = "-"
		x = -x
	}
	if x >= 1e21 {
		return sign + strconv.FormatFloat(x, 'f', -1, 64)
	}
	r := new(big.Rat).SetFloat64(x)
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(digits)), nil)
	r.Mul(r, new(big.Rat).SetInt(scale))
	r.Add(r, big.NewRat(1, 2))
	n := new(big.Int).Quo(r.Num(), r.Denom())
	s := n.String()
	for len(s) <= digits {
		s = "0" + s
	}
	if digits == 0 {
		return sign + s
	}
	return sign + s[:len(s)-digits] + "." + s[len(s)-digits:]
}

// jsNumber writes a number as JavaScript's String() does for the numbers of an abbreviation.
func jsNumber(x float64) string {
	switch {
	case math.IsNaN(x):
		return "NaN"
	case math.IsInf(x, 1):
		return "Infinity"
	case math.IsInf(x, -1):
		return "-Infinity"
	case x == 0:
		return "0"
	}
	return strconv.FormatFloat(x, 'f', -1, 64)
}
