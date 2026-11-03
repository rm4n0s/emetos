package cover

import "testing"

func TestSign(t *testing.T) {
	if Sign(-1) != "neg" || Sign(1) != "pos" {
		t.Fatal("wrong sign")
	}
}
