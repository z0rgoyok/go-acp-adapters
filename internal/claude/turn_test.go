package claude

import "testing"

func TestCurrentTurnStopRequiresCurrentTurnID(t *testing.T) {
	turn := turnContext{ID: "turn-1"}
	if currentTurnStop(stopPayload{}, turn) {
		t.Fatal("payload without turn id matched")
	}
	if currentTurnStop(stopPayload{TurnID: "turn-2"}, turn) {
		t.Fatal("payload with another turn id matched")
	}
	if !currentTurnStop(stopPayload{TurnID: "turn-1"}, turn) {
		t.Fatal("payload with current turn id did not match")
	}
}
