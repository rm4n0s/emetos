package server

import "testing"

func TestNew(t *testing.T) {
	if got := New(":1").Addr; got != ":1" {
		t.Errorf("Addr = %q", got)
	}
}
