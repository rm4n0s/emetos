package emulator

import (
	"strings"

	_ "github.com/rm4n0s/emetos/extension"
	_ "github.com/rm4n0s/emetos/extensions/internal/pty"
	_ "github.com/rm4n0s/glutter/widgets"
	_ "golang.org/x/sys/unix"
	"golang.org/x/text/width"
)

var _ = strings.Repeat
var _ = width.LookupRune
