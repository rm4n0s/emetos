package exthost

import "testing"

func TestEveryScopeHasItsOwnInstanceNumber(t *testing.T) {
	r := newRig(t)
	a, b := r.newTab("a"), r.newTab("b")
	ia, ib := a.scope.Info().Instance, b.scope.Info().Instance
	if ia == 0 || ib == 0 || ia == ib {
		t.Errorf("instances = %d and %d, want two different numbers above 0", ia, ib)
	}
	if again := a.scope.Info().Instance; again != ia {
		t.Errorf("the instance of a scope changed from %d to %d", ia, again)
	}
}
