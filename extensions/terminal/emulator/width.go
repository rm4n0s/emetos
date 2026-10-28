package emulator

//go:generate go run gen_width.go

// runeWidth is the number of cells r takes: 0 for combining marks and format characters, 2 for the wide and fullwidth
// characters of the East Asian Width property (which include the emoji with emoji presentation), 1 for the rest. It is
// the model of wcwidth(3) that programs use to lay out their output, so Emetos's idea of a line matches theirs (a
// variation selector or a joiner does not change the width of what it follows).
func runeWidth(r rune) int {
	if r < 0x300 {
		if r < 0x20 || (r >= 0x7f && r < 0xa0) {
			return 0
		}
		return 1
	}
	if uint32(r) >= 0x110000 {
		return 1
	}
	return int(widthPages[widthIndex[r>>8]][r&0xff])
}
