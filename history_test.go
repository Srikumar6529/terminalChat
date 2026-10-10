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
	// Does not drop assistant if last role is not user.
	got = rollbackLastUser(got)
	if len(got) != 2 {
		t.Fatalf("unexpected rollback: %+v", got)
	}
	if rollbackLastUser(nil) != nil {
		t.Fatal("nil history")
	}
}

func TestAppendAssistant(t *testing.T) {
	h := []anthropic.Message{{Role: "user", Content: "a"}}
	h = appendAssistant(h, "")
	if len(h) != 1 {
		t.Fatal("empty assistant should not append")
	}
	h = appendAssistant(h, "reply")
	if len(h) != 2 || h[1].Content != "reply" {
		t.Fatalf("got=%+v", h)
	}
}
