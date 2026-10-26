package main

import "testing"

func TestGreet(t *testing.T) {
	if got := Greet(" x "); got != "hello, x" {
		t.Fatalf("Greet = %q", got)
	}
}
