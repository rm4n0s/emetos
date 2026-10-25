package thing

import (
	"testing"

	"github.com/rm4n0s/errors"
)

const constRoute = "Do.ConstRoute"

// TestDo names OnlyInComment only here: it does not count.
func TestDo(t *testing.T) {
	appErr, _ := errors.FromError(Do(1))
	if !appErr.HasRoute("Do.LiteralRoute") {
		t.Error(appErr.Route())
	}
	for _, tt := range []struct{ route, tag string }{{"Do.TableRoute", "TableTag"}} {
		_ = tt.tag
		if !appErr.HasRoute(tt.route) {
			t.Error(appErr.Route())
		}
	}
	for _, tag := range []string{"TableTag"} {
		if appErr.Tag != tag {
			t.Error(tag)
		}
	}
	if !errors.Is(Do(4), &errors.Error{Tag: "SentinelTag"}) {
		t.Error("sentinel")
	}
	if !appErr.HasRoute(constRoute) {
		t.Error("const")
	}
	wantRoute(t, Do(6), "Do", "HelperRoute")
	// a fake error of a test is not an assertion
	_ = errors.New("OnlyAFake", "fake")
	_ = errors.New("TableTag", "fake")
}

func wantRoute(t *testing.T, err error, fn, tag string) {
	appErr, _ := errors.FromError(err)
	if !appErr.HasRoute(fn + "." + tag) {
		t.Error(appErr.Route())
	}
}
