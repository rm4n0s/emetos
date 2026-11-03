package build

import "testing"

func TestBroken(t *testing.T) {
	F(undefinedName)
}

func TestOther(t *testing.T) {}
