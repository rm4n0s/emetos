package theme

import "strings"

// isValidHexColor tells whether s is #rgb, #rgba, #rrggbb or #rrggbbaa.
//
// Ported from vscode-textmate: src/utils.ts
func isValidHexColor(s string) bool {
	if len(s) == 0 || s[0] != '#' {
		return false
	}
	switch len(s) {
	case 4, 5, 7, 9:
	default:
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

// ColorMap numbers the colors of a theme. Id 0 is no color; the colors are kept in upper case.
//
// Ported from vscode-textmate: src/theme.ts (ColorMap)
type ColorMap struct {
	id2color []string
	color2id map[string]int
}

// NewColorMap makes an empty color map.
func NewColorMap() *ColorMap {
	return &ColorMap{id2color: []string{""}, color2id: map[string]int{}}
}

// ID gives the number of a color, adding the color when it is new. The empty color is 0.
func (m *ColorMap) ID(color string) int {
	if color == "" {
		return 0
	}
	color = strings.ToUpper(color)
	if id, ok := m.color2id[color]; ok {
		return id
	}
	id := len(m.id2color)
	m.color2id[color] = id
	m.id2color = append(m.id2color, color)
	return id
}

// Colors are the colors by number; the one at 0 is empty.
func (m *ColorMap) Colors() []string { return append([]string(nil), m.id2color...) }

// Color gives the color with a number, or "" for 0 or a number that is not used.
func (m *ColorMap) Color(id int) string {
	if id <= 0 || id >= len(m.id2color) {
		return ""
	}
	return m.id2color[id]
}
