package main

import (
	"encoding/json"
	"testing"
)

func TestParseConversationCapturesDiaID(t *testing.T) {
	raw := json.RawMessage(`{"session_1":[{"speaker":"A","text":"hi","dia_id":"D1:1"},{"speaker":"B","text":"yo","dia_id":"D1:2"}]}`)
	sessions, err := parseConversation(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(sessions) != 1 || len(sessions[0].Turns) != 2 {
		t.Fatalf("sessions = %+v, want one session with two turns", sessions)
	}
	if sessions[0].Turns[0].DiaID != "D1:1" || sessions[0].Turns[1].DiaID != "D1:2" {
		t.Fatalf("dialogue ids not captured: %+v", sessions[0].Turns)
	}
}
