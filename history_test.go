package main

import (
	"testing"

	"terminalchat/anthropic"
)

func TestRollbackLastUser(t *testing.T) {
	h := []anthropic.Message{
		{Role: "user", Content: "a"},
		{Role: "assistant", Content: "b"},
		{Role: "user", Content: "c"},
	}
	got := rollbackLastUser(h)
	if len(got) != 2 || got[1].Role != "assistant" {
		t.Fatalf("got=%+v", got)
	}
	got = rollbackLastUser(got)
	if len(got) != 2 {
		t.Fatalf("unexpected rollback: %+v", got)
	}
	if rollbackLastUser(nil) != nil {
		t.Fatal("nil history")
	}
}

func TestAppendAssistantKeepsEmpty(t *testing.T) {
	h := []anthropic.Message{{Role: "user", Content: "a"}}
	h = appendAssistant(h, "")
	if len(h) != 2 || h[1].Role != "assistant" || h[1].Content != "" {
		t.Fatalf("empty assistant should still append: %+v", h)
	}
	h = appendAssistant(h[:1], "reply")
	if len(h) != 2 || h[1].Content != "reply" {
		t.Fatalf("got=%+v", h)
	}
}

func TestTrimHistory(t *testing.T) {
	var h []anthropic.Message
	for i := 0; i < maxHistoryMessages+4; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		h = append(h, anthropic.Message{Role: role, Content: "x"})
	}
	got := trimHistory(h)
	if len(got) > maxHistoryMessages {
		t.Fatalf("len=%d", len(got))
	}
	if len(got) == 0 || got[0].Role != "user" {
		t.Fatalf("should start on user: %+v", got)
	}
}
