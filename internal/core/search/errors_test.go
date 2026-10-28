package search

import (
	"context"
	"testing"

	"github.com/rm4n0s/errors"
)

// asError returns the *errors.Error of err, or fails the test.
func asError(t *testing.T, err error) *errors.Error {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	return appErr
}

func TestErrorRoutes(t *testing.T) {
	dir := tree(t, map[string]string{"a.txt": "x\n"})
	tests := []struct {
		name  string
		run   func() error
		route string
	}{
		{"an empty pattern", func() error {
			_, err := Run(context.Background(), Query{}, defaults(dir), func(FileResult) {})
			return err
		}, "Run->compile.SearchPatternEmpty"},
		{"a regular expression that does not compile", func() error {
			_, err := Run(context.Background(), Query{Pattern: "(", IsRegExp: true}, defaults(dir), func(FileResult) {})
			return err
		}, "Run->compile->Compile->compileECMAScript.RegexCompileFailed"},
		{"validate", func() error { return Query{Pattern: "[a", IsRegExp: true}.Validate() }, "Query.Validate->compile->Compile->compileECMAScript.RegexCompileFailed"},
		{"a folder that is not there", func() error {
			o := defaults(dir + "/nowhere")
			_, err := Run(context.Background(), Query{Pattern: "x"}, o, func(FileResult) {})
			return err
		}, "Run.SearchFolderUnreadable"},
		{"a replacer with an empty pattern", func() error { _, err := NewReplacer(Query{}, "x", false); return err }, "NewReplacer.SearchPatternEmpty"},
		{"a replacer with a bad expression", func() error { _, err := NewReplacer(Query{Pattern: "(", IsRegExp: true}, "x", false); return err }, "NewReplacer->Compile->compileECMAScript.RegexCompileFailed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			appErr := asError(t, tt.run())
			if !appErr.HasRoute(tt.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
}
