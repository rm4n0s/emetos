package fail

import "testing"

func TestPasses(t *testing.T) {
	if Double(2) != 4 {
		t.Fatal("no")
	}
}

func TestWrong(t *testing.T) {
	got := Double(2)
	if got != 5 {
		t.Errorf("Double(2) = %d, want %d", got, 5)
	}
}

func TestSkipped(t *testing.T) {
	t.Skip("needs a network")
}

func TestSubs(t *testing.T) {
	t.Run("ok", func(t *testing.T) {})
	t.Run("bad", func(t *testing.T) {
		t.Error("the subtest failed")
	})
}
