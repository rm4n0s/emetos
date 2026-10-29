package playground

import (
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// fixed are options with a clock and random numbers that never change.
var fixed = Options{
	Now:         func() time.Time { return time.Date(2024, 5, 6, 7, 8, 9, 123_000_000, time.UTC) },
	NewObjectID: func() bson.ObjectID { return bson.ObjectID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12} },
	NewUUID: func() [16]byte {
		return [16]byte{0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0x4d, 0xef, 0x80, 0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde}
	},
}

// render writes a statement in one canonical line, to compare in a table.
func render(s Statement) string {
	switch x := s.(type) {
	case *Use:
		return "use " + x.Database
	case *Show:
		return "show " + string(x.What)
	case *Command:
		name := "runCommand"
		if x.Admin {
			name = "adminCommand"
		}
		return "db." + name + "(" + Format(x.Document) + ")"
	case *CollectionCall:
		var b strings.Builder
		b.WriteString("db[" + Quote(x.Collection) + "]." + x.Method + "(")
		for i, a := range x.Args {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(Format(a))
		}
		b.WriteString(")")
		for _, c := range x.Chain {
			b.WriteString("." + c.Name + "(")
			for i, a := range c.Args {
				if i > 0 {
					b.WriteString(", ")
				}
				b.WriteString(Format(a))
			}
			b.WriteString(")")
		}
		return b.String()
	}
	return "?"
}

// renderAll writes every statement of a script, separated by " ; ", and the errors as "ERR(tag)".
func renderAll(s *Script) string {
	var parts []string
	for _, it := range s.Items {
		if it.Err != nil {
			e, _ := errors.FromError(it.Err)
			parts = append(parts, "ERR("+e.Tag+")")
			continue
		}
		parts = append(parts, render(it.Statement))
	}
	return strings.Join(parts, " ; ")
}

// wantErr checks the item is an error whose route has the want, at the line and column.
func wantErr(t *testing.T, it Item, route string, line, col int) {
	t.Helper()
	appErr, ok := errors.FromError(it.Err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", it.Err, it.Err)
	}
	if !appErr.HasRoute(route) {
		t.Errorf("unexpected failure path: %s (want %s)", appErr.Route(), route)
	}
	pl, ok := Place(it.Err)
	if !ok {
		t.Fatalf("the error has no place: %v", appErr.Metadata)
	}
	if pl.Start.Line != line || pl.Start.Column != col {
		t.Errorf("error at line %d column %d, want %d:%d (%s)", pl.Start.Line, pl.Start.Column, line, col, appErr.Message)
	}
}
