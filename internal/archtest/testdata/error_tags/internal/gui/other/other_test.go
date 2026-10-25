package other

import "testing"

func TestFail(t *testing.T) {
	if Fail() == nil || "OnlyNamed" == "" {
		t.Error("a string that is not an assertion")
	}
}
