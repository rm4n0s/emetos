package icons

import (
	"embed"
	"strconv"
)

//go:embed emetos-*.png
var files embed.FS

// PNG is the picture of the application icon that is size pixels wide and high, as a PNG file, and whether there is
// one of that size.
func PNG(size int) ([]byte, bool) {
	data, err := files.ReadFile("emetos-" + strconv.Itoa(size) + ".png")
	if err != nil {
		return nil, false
	}
	return data, true
}
