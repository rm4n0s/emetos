package golang

import (
	"fmt"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/internal/engine"
	"github.com/rm4n0s/glutter/widgets"
	"github.com/someone/lib"
	"golang.org/x/text/unicode/norm"
	"gopkg.in/yaml.v3"
)

var _ = fmt.Sprint
var _ = errors.New
var _ = engine.Run
var _ widgets.Widget
var _ = lib.Do
var _ = norm.NFC
var _ = yaml.Marshal
