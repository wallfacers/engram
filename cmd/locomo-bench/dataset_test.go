package main

import (
	"encoding/json"
	"testing"
)

func TestParseConversationCapturesDiaID(t *testing.T) {
	raw := json.RawMessage(`{"session_1":[{"speaker":"A","text":"hi","dia_id":"D1:1"},{"speaker":"B","text":"yo","dia_id":"D1:2"}]}`)
	sessions, err := parseConversation(raw, false)
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

// captionRaw carries a text+image turn, an image-only turn, and a plain turn —
// the three shapes LoCoMo mixes (blip_caption describes the shared photo).
var captionRaw = json.RawMessage(`{"session_1":[
	{"speaker":"A","text":"look at this","dia_id":"D1:1","img_url":["u"],"blip_caption":"a photo of a dog"},
	{"speaker":"B","text":"","dia_id":"D1:2","img_url":["u"],"blip_caption":"a poster reading Trans Lives Matter"},
	{"speaker":"A","text":"nice","dia_id":"D1:3"}]}`)

func TestParseConversationCaptionsOffKeepsCurrentBehavior(t *testing.T) {
	sessions, err := parseConversation(captionRaw, false)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	turns := sessions[0].Turns
	if len(turns) != 2 {
		t.Fatalf("turns = %d, want 2 (image-only turn dropped when captions off)", len(turns))
	}
	if turns[0].Text != "look at this" {
		t.Fatalf("text = %q, want caption NOT folded in when captions off", turns[0].Text)
	}
}

func TestParseConversationCaptionsOnFoldsAndKeepsImageOnlyTurns(t *testing.T) {
	sessions, err := parseConversation(captionRaw, true)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	turns := sessions[0].Turns
	if len(turns) != 3 {
		t.Fatalf("turns = %d, want 3 (image-only turn kept when captions on)", len(turns))
	}
	if want := "look at this [shares a photo: a photo of a dog]"; turns[0].Text != want {
		t.Fatalf("text = %q, want %q", turns[0].Text, want)
	}
	if want := "[shares a photo: a poster reading Trans Lives Matter]"; turns[1].Text != want {
		t.Fatalf("image-only text = %q, want %q", turns[1].Text, want)
	}
	if turns[1].DiaID != "D1:2" {
		t.Fatalf("image-only DiaID = %q, want D1:2 (gold evidence references it)", turns[1].DiaID)
	}
	if turns[2].Text != "nice" {
		t.Fatalf("plain turn = %q, want untouched", turns[2].Text)
	}
}
