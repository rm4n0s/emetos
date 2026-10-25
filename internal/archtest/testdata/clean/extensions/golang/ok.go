package golang

import (
	"fmt"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/golang/grammar"
	"github.com/rm4n0s/emetos/extensions/internal/testext"
	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/widgets"
	"golang.org/x/text/unicode/norm"
)

// New is the Factory.
func New() extension.Extension { return nil }

var _ = fmt.Sprint
var _ = grammar.Rules
var _ = testext.New
var _ = errors.New
var _ widgets.Widget
var _ = norm.NFC
