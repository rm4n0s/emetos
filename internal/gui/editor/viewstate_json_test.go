package editor

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/folding"
)

func TestViewStateJSONRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		in   ViewState
	}{
		{"empty", ViewState{}},
		{"a cursor", ViewState{Selections: []buffer.Selection{buffer.NewCursor(buffer.Position{Line: 4, Col: 7})}}},
		{"several selections", ViewState{Selections: []buffer.Selection{
			{Anchor: buffer.Position{Line: 1, Col: 2}, Active: buffer.Position{Line: 3, Col: 0}},
			buffer.NewCursor(buffer.Position{Line: 9, Col: 9}),
		}}},
		{"scroll positions", ViewState{ScrollTop: 1234.5, ScrollLeft: 17}},
		{"folds", ViewState{Folds: folding.Memento{
			{Start: 3, End: 9, Collapsed: true, Checksum: 123456},
			{Start: 12, End: 14, Collapsed: false, Source: folding.UserDefined},
		}}},
		{"all of it", ViewState{
			Selections: []buffer.Selection{buffer.NewCursor(buffer.Position{Line: 2, Col: 1})},
			ScrollTop:  80, ScrollLeft: 4,
			Folds: folding.Memento{{Start: 0, End: 5, Collapsed: true}},
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(data), `"version":1`) {
				t.Errorf("no version in %s", data)
			}
			var got ViewState
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.in) {
				t.Errorf("got %+v, want %+v (%s)", got, tc.in, data)
			}
		})
	}
}

func TestViewStateJSONCleansWhatItReads(t *testing.T) {
	var got ViewState
	err := json.Unmarshal([]byte(`{"version":1,"selections":[{"anchor":{"line":-3,"col":-1},"active":{"line":2,"col":5}}],"scrollTop":-4,"scrollLeft":9}`), &got)
	if err != nil {
		t.Fatal(err)
	}
	want := ViewState{Selections: []buffer.Selection{{Anchor: buffer.Position{}, Active: buffer.Position{Line: 2, Col: 5}}}, ScrollLeft: 9}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
	data, err := json.Marshal(ViewState{ScrollTop: math.NaN(), ScrollLeft: math.Inf(1)})
	if err != nil || strings.Contains(string(data), "scroll") {
		t.Errorf("a scroll that is not a number: %s, %v", data, err)
	}
}

func TestViewStateJSONFailures(t *testing.T) {
	tests := []struct {
		name, in, route string
	}{
		{"not JSON", `{`, "ViewState.UnmarshalJSON.ViewStateInvalid"},
		{"not an object", `[1]`, "ViewState.UnmarshalJSON.ViewStateInvalid"},
		{"a number where a position is", `{"version":1,"selections":[{"anchor":1}]}`, "ViewState.UnmarshalJSON.ViewStateInvalid"},
		{"no version", `{"scrollTop":3}`, "ViewState.UnmarshalJSON.ViewStateVersionUnsupported"},
		{"a newer version", `{"version":2}`, "ViewState.UnmarshalJSON.ViewStateVersionUnsupported"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var s ViewState
			err := s.UnmarshalJSON([]byte(tc.in))
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tc.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
}
