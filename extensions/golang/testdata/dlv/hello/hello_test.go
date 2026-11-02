package main

import (
	"fmt"
	"testing"
)

func TestCompute(t *testing.T) {
	want := 14
	got := compute(3)
	if got != want {
		t.Fatalf("compute(3) = %d, want %d", got, want)
	}
	fmt.Println("TestCompute ran")
}

func TestOther(t *testing.T) {
	other := compute(1)
	fmt.Println("TestOther ran", other)
}
