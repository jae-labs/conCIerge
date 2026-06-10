package conversation

import (
	"testing"
)

func TestStore_Create(t *testing.T) {
	s := NewStore()

	state := s.Create("ts1", "C123", "U123")
	if state.Phase != PhaseIdle {
		t.Errorf("got phase=%v, want PhaseIdle", state.Phase)
	}
	if state.ChannelID != "C123" {
		t.Errorf("got channel=%q, want C123", state.ChannelID)
	}
	if state.ThreadTS != "ts1" {
		t.Errorf("got threadTS=%q, want ts1", state.ThreadTS)
	}
	if state.UserID != "U123" {
		t.Errorf("got userID=%q, want U123", state.UserID)
	}
	if state.Nonce == "" {
		t.Error("expected non-empty nonce")
	}
	if state.Messages != nil {
		t.Error("expected nil messages on new state")
	}
}

func TestStore_Create_UniqueNonce(t *testing.T) {
	s := NewStore()

	s1 := s.Create("ts1", "C123", "U123")
	nonce1 := s1.Nonce
	s.Delete("ts1")

	s2 := s.Create("ts1", "C123", "U123")
	if s2.Nonce == nonce1 {
		t.Error("expected different nonce on re-create")
	}
}

func TestStore_Get(t *testing.T) {
	s := NewStore()

	if got := s.Get("ts1"); got != nil {
		t.Error("expected nil for unknown thread")
	}

	created := s.Create("ts1", "C123", "U123")
	created.Phase = PhaseCategorySelected

	got := s.Get("ts1")
	if got == nil {
		t.Fatal("expected state for ts1")
		return
	}
	if got.Phase != PhaseCategorySelected {
		t.Errorf("got phase=%v, want PhaseCategorySelected", got.Phase)
	}

	if s.Get("ts2") != nil {
		t.Error("expected nil for different thread")
	}
}

func TestStore_Delete(t *testing.T) {
	s := NewStore()

	state := s.Create("ts1", "C123", "U123")
	state.Phase = PhaseCategorySelected

	s.Delete("ts1")

	if s.Get("ts1") != nil {
		t.Error("expected nil after delete")
	}
}

func TestState_TrackMessage(t *testing.T) {
	s := NewStore()
	state := s.Create("ts1", "C123", "U123")

	state.TrackMessage("1234.5678", MsgWelcome, "")
	state.TrackMessage("1234.5679", MsgCategory, "GitHub")

	if len(state.Messages) != 2 {
		t.Fatalf("got %d messages, want 2", len(state.Messages))
	}
	if state.Messages[0].Kind != MsgWelcome {
		t.Errorf("got kind=%v, want MsgWelcome", state.Messages[0].Kind)
	}
	if state.Messages[1].Label != "GitHub" {
		t.Errorf("got label=%q, want GitHub", state.Messages[1].Label)
	}
}

func TestState_HasMessage(t *testing.T) {
	s := NewStore()
	state := s.Create("ts1", "C123", "U123")

	state.TrackMessage("1234.5678", MsgWelcome, "")

	if !state.HasMessage("1234.5678") {
		t.Error("expected HasMessage to return true for tracked TS")
	}
	if state.HasMessage("9999.0000") {
		t.Error("expected HasMessage to return false for unknown TS")
	}
}
