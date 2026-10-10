package main

import (
	"errors"
	"testing"

	"terminalchat/anthropic"
)

func TestStress_HistoryRollbackAndRecovery(t *testing.T) {
	var h []anthropic.Message
	h = append(h, anthropic.Message{Role: "user", Content: "a"})
	h = finishTurn(h, "partial", errors.New("fail"))
	if len(h) != 0 {
		t.Fatalf("expected rollback, got %+v", h)
	}
	h = append(h, anthropic.Message{Role: "user", Content: "b"})
	h = finishTurn(h, "ok", nil)
	if len(h) != 2 || h[0].Content != "b" || h[1].Content != "ok" {
		t.Fatalf("got %+v", h)
	}
	// Simulate many failed turns without corrupting prior history.
	for i := 0; i < 30; i++ {
		h = append(h, anthropic.Message{Role: "user", Content: "x"})
		h = finishTurn(h, "nope", errors.New("e"))
	}
	if len(h) != 2 {
		t.Fatalf("corrupted history len=%d", len(h))
	}
	h = trimHistory(h)
	if len(h) != 2 || h[0].Role != "user" {
		t.Fatalf("trim broke history: %+v", h)
	}
}
