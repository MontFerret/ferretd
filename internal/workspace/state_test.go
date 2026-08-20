package workspace

import "testing"

func TestStateReservesZero(t *testing.T) {
	var state State
	if state == StateOpening || state == StateReady || state == StateFailed ||
		state == StateClosing || state == StateClosed {
		t.Fatalf("zero State aliases a meaningful state: %d", state)
	}
}
