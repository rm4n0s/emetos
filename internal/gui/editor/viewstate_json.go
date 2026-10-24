package editor

import (
	"bytes"
	"encoding/json"
	"math"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/folding"
)

// ViewStateVersion is the version of the JSON of a ViewState, which the saved state of a window holds (SPEC
// §10.10). A state with another version is refused with the tag ViewStateVersionUnsupported.
const ViewStateVersion = 1

// viewStatePosition and viewStateSelection are the JSON of buffer.Position and buffer.Selection, with
// lowercase names and the lines and columns counted from 0 as the buffer does.
type viewStatePosition struct {
	Line int `json:"line"`
	Col  int `json:"col"`
}

type viewStateSelection struct {
	Anchor viewStatePosition `json:"anchor"`
	Active viewStatePosition `json:"active"`
}

// viewStateJSON is the JSON of a ViewState.
type viewStateJSON struct {
	Version    int                  `json:"version"`
	Selections []viewStateSelection `json:"selections,omitempty"`
	ScrollTop  float64              `json:"scrollTop,omitempty"`
	ScrollLeft float64              `json:"scrollLeft,omitempty"`
	Folds      folding.Memento      `json:"folds,omitempty"`
}

// MarshalJSON implements json.Marshaler: the view state with a version field. A scroll position that is not a
// number is written as 0.
func (s ViewState) MarshalJSON() ([]byte, error) {
	out := viewStateJSON{Version: ViewStateVersion, ScrollTop: finiteOrZero(s.ScrollTop), ScrollLeft: finiteOrZero(s.ScrollLeft), Folds: s.Folds}
	for _, sel := range s.Selections {
		out.Selections = append(out.Selections, viewStateSelection{
			Anchor: viewStatePosition{Line: sel.Anchor.Line, Col: sel.Anchor.Col},
			Active: viewStatePosition{Line: sel.Active.Line, Col: sel.Active.Col},
		})
	}
	return json.Marshal(out)
}

func finiteOrZero(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return v
}

// UnmarshalJSON implements json.Unmarshaler. A document that is not a view state has the tag
// ViewStateInvalid, one with another version ViewStateVersionUnsupported. Negative positions and scroll
// positions are made 0; RestoreViewState moves positions into the text.
func (s *ViewState) UnmarshalJSON(data []byte) error {
	var in viewStateJSON
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&in); err != nil {
		return errors.NewErr("ViewStateInvalid", err)
	}
	if in.Version != ViewStateVersion {
		return errors.New("ViewStateVersionUnsupported", "the view state has another version than this program reads",
			"version", in.Version, "supported", ViewStateVersion)
	}
	out := ViewState{ScrollTop: max(finiteOrZero(in.ScrollTop), 0), ScrollLeft: max(finiteOrZero(in.ScrollLeft), 0), Folds: in.Folds}
	for _, sel := range in.Selections {
		out.Selections = append(out.Selections, buffer.Selection{
			Anchor: buffer.Position{Line: max(sel.Anchor.Line, 0), Col: max(sel.Anchor.Col, 0)},
			Active: buffer.Position{Line: max(sel.Active.Line, 0), Col: max(sel.Active.Col, 0)},
		})
	}
	*s = out
	return nil
}
